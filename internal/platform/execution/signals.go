package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

// The production adapter between internal/workflow/execute's signal ports and
// internal/data/signals' durable receive path (WF-RUN-005). It lives here for
// the same reason [TimerFactory] does: execute states what it needs, the data
// package owns the rows, and a composition root wires one to the other.
//
// Neither method signature names an identifier type, so this adapter needs no
// direct github.com/google/uuid import (dependency-roles.yaml does not list
// internal/platform among that module's importers): identities cross as the
// fields of execute's own request structs and are derived by the data
// package.

// CorrelationResolver evaluates a SIGNAL node's declared correlation key
// expression for one instance and returns the value an inbound signal must
// carry to match it.
type CorrelationResolver func(req execute.SignalSubscriptionRequest) (string, error)

// ErrUnresolvedCorrelation reports a correlation key expression the resolver
// cannot evaluate for the instance. The subscription is refused rather than
// opened under a guessed value, because a wrong value is a wait that either
// never wakes or wakes for someone else's signal.
var ErrUnresolvedCorrelation = errors.New("platform execution: unresolved signal correlation")

// SignalSubscriptions is the durable adapter for both
// [execute.SignalSubscriber] and [execute.SignalReader].
type SignalSubscriptions struct {
	// Store is the durable owner. Its zero value is usable.
	Store signals.Store
	// Correlate evaluates correlation key expressions. Nil means
	// [DefaultCorrelation].
	Correlate CorrelationResolver
}

var (
	_ execute.SignalSubscriber    = SignalSubscriptions{}
	_ execute.SignalReader        = SignalSubscriptions{}
	_ execute.SignalTimeoutReader = SignalSubscriptions{}
)

// DefaultCorrelation is the closed correlation vocabulary a composition gets
// when it names no resolver of its own: the instance's own start facts, never
// payload data. "workflow.correlation_id", "workflow.instance_id",
// "proposal.intent_id" and "proposal.revision_id" name those facts directly;
// "subject:<kind>" selects the one business subject reference of that kind
// ("subject:employment" selects "employment:jane"). Anything else is
// [ErrUnresolvedCorrelation].
func DefaultCorrelation(req execute.SignalSubscriptionRequest) (string, error) {
	expression := ""
	if req.Node.Signal != nil {
		expression = req.Node.Signal.CorrelationKeyExpression
	}
	value := ""
	switch {
	case expression == "workflow.correlation_id":
		value = req.CorrelationID
	case expression == "workflow.instance_id":
		value = req.Continuation.InstanceID.String()
	case expression == "proposal.intent_id":
		value = req.Proposal.Revision.IntentID
	case expression == "proposal.revision_id":
		value = req.Proposal.Revision.ProposalRevisionID
	case strings.HasPrefix(expression, "subject:") && len(expression) > len("subject:"):
		prefix := strings.TrimPrefix(expression, "subject:") + ":"
		for _, ref := range req.SubjectRefs {
			if strings.HasPrefix(ref, prefix) {
				if value != "" {
					return "", fmt.Errorf("%w: %q selects more than one business subject", ErrUnresolvedCorrelation, expression)
				}
				value = ref
			}
		}
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%w: node %s expression %q has no value for instance %s",
			ErrUnresolvedCorrelation, req.Node.ID, expression, req.Continuation.InstanceID)
	}
	return value, nil
}

// CreateSubscription opens the durable subscription the compiled SIGNAL node
// declares, inside the advancement's own transaction. A replayed advancement
// addresses the same deterministic subscription and reports Replay.
func (a SignalSubscriptions) CreateSubscription(ctx context.Context, ex runtime.Executor, req execute.SignalSubscriptionRequest) (ret0 execute.SignalHandle, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execution.create_signal_subscription", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if req.Node.Type != workflow.StepSignal || req.Node.Signal == nil {
		return execute.SignalHandle{}, fmt.Errorf("platform execution: node %s is not a compiled SIGNAL", req.Node.ID)
	}
	correlate := a.Correlate
	if correlate == nil {
		correlate = DefaultCorrelation
	}
	value, err := correlate(req)
	if err != nil {
		return execute.SignalHandle{}, err
	}
	rec := req.Continuation
	attempt := waitAttempt(rec.TargetAttempt)
	created := req.CreatedAt.UTC()
	instanceCtx := stepSignal.InstanceContext{
		Tenant: values.TenantId(rec.TenantID.String()), WorkflowInstanceID: rec.InstanceID.String(), CorrelationValue: value,
	}
	if seconds := req.Node.Signal.CloseAfterSeconds; seconds > 0 {
		instanceCtx.ClosesAt = values.NewInstant(created.Add(time.Duration(seconds) * time.Second))
	}
	// internal/workflow/steps/signal.FromCompiled is the one translation of a
	// compiled SIGNAL node into the subscription Accept evaluates; the durable
	// row is written from its output so the two can never disagree.
	bound := req.Node
	sub, err := stepSignal.FromCompiled(&bound, instanceCtx)
	if err != nil {
		return execute.SignalHandle{}, fmt.Errorf("platform execution: bind the compiled SIGNAL node: %w", err)
	}
	var closes time.Time
	if sub.ClosesAt.IsSet() {
		closes = sub.ClosesAt.Time()
	}
	subscriptionID := signals.SubscriptionIDFor(rec.TenantID, rec.InstanceID, sub.NodeID, attempt)
	replay, err := a.Store.SubscribeReplay(ctx, ex, signals.Subscription{
		TenantID: rec.TenantID, SubscriptionID: subscriptionID, InstanceID: rec.InstanceID,
		NodeID: sub.NodeID, NodeAttempt: attempt, EventType: sub.EventType,
		CorrelationKey: sub.CorrelationKey, CorrelationValue: sub.CorrelationValue,
		ExpectedSchemaRef: sub.ExpectedSchemaRef, AcceptedSources: sub.AcceptedSources,
		Ordering: sub.Ordering, ClosesAt: closes, CreatedAt: created,
		Causal: stateCausal(rec.Causal),
	})
	if err != nil {
		return execute.SignalHandle{}, err
	}
	return execute.SignalHandle{SubscriptionID: subscriptionID, NodeID: req.Node.ID, Attempt: attempt, Replay: replay}, nil
}

// LoadMatchedSignal loads the committed ACCEPTED receipt a resume advances
// from. A receipt that does not exist is [execute.ErrSignalDrift]: the
// request names evidence that was never committed.
func (a SignalSubscriptions) LoadMatchedSignal(ctx context.Context, ex runtime.Executor, q execute.MatchedSignalQuery) (ret0 execute.MatchedSignal, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execution.load_matched_signal", q)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	row, err := a.Store.LoadMatchedReceipt(ctx, ex, q.TenantID, q.SignalID, q.SubscriptionID)
	if errors.Is(err, signals.ErrNoMatchedReceipt) {
		return execute.MatchedSignal{}, fmt.Errorf("%w: %w", execute.ErrSignalDrift, err)
	}
	if err != nil {
		return execute.MatchedSignal{}, err
	}
	return execute.MatchedSignal{
		SignalID: row.SignalID, SubscriptionID: row.SubscriptionID, InstanceID: row.InstanceID,
		NodeID: row.NodeID, NodeAttempt: row.NodeAttempt, Settled: row.Settled(),
		ContinuationRef: row.ContinuationRef, Causal: runtimeCausal(row.Causal),
	}, nil
}

// LoadExpiredSubscription loads the committed EXPIRED wait a timeout resume
// advances from. Anything but an EXPIRED row is [execute.ErrSignalDrift]:
// the request names evidence that was never committed, or a wait that is
// still receivable. Settled additionally proves the durable timeout
// reference agrees with the driver's: the advancement records it as the
// SIGNAL node's output, so the two namespaces must be one.
func (a SignalSubscriptions) LoadExpiredSubscription(ctx context.Context, ex runtime.Executor, q execute.ExpiredSubscriptionQuery) (ret0 execute.ExpiredSubscription, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execution.load_expired_subscription", q)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	row, err := a.Store.LoadExpiredSubscription(ctx, ex, q.TenantID, q.SubscriptionID)
	if errors.Is(err, signals.ErrNoExpiredSubscription) {
		return execute.ExpiredSubscription{}, fmt.Errorf("%w: %w", execute.ErrSignalDrift, err)
	}
	if err != nil {
		return execute.ExpiredSubscription{}, err
	}
	return execute.ExpiredSubscription{
		SubscriptionID: row.SubscriptionID, InstanceID: row.InstanceID,
		NodeID: row.NodeID, NodeAttempt: row.NodeAttempt,
		Settled:         true,
		ContinuationRef: signals.ExpiryContinuationRef(row.SubscriptionID),
		Causal:          runtimeCausal(row.Causal),
	}, nil
}

func stateCausal(c *runtime.CausalMetadata) *runtimestate.CausalMetadata {
	if c == nil {
		return nil
	}
	out := &runtimestate.CausalMetadata{CorrelationID: c.CorrelationID, CausationID: c.CausationID,
		LogicalOperationID: c.LogicalOperationID, AttemptID: c.AttemptID}
	if c.TraceLink != nil {
		out.TraceLink = &runtimestate.TraceLinkMetadata{TraceID: c.TraceLink.TraceID, SpanID: c.TraceLink.SpanID,
			TraceFlags: c.TraceLink.TraceFlags, TraceState: c.TraceLink.TraceState, ExpiresAt: c.TraceLink.ExpiresAt}
	}
	return out
}

func runtimeCausal(c *runtimestate.CausalMetadata) *runtime.CausalMetadata {
	if c == nil {
		return nil
	}
	out := &runtime.CausalMetadata{CorrelationID: c.CorrelationID, CausationID: c.CausationID,
		LogicalOperationID: c.LogicalOperationID, AttemptID: c.AttemptID}
	if c.TraceLink != nil {
		out.TraceLink = &runtime.TraceLinkMetadata{TraceID: c.TraceLink.TraceID, SpanID: c.TraceLink.SpanID,
			TraceFlags: c.TraceLink.TraceFlags, TraceState: c.TraceLink.TraceState, ExpiresAt: c.TraceLink.ExpiresAt}
	}
	return out
}
