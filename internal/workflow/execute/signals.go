package execute

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// WF-RUN-005's driver-facing half, added additively and shaped exactly like
// WF-RUN-004's timer ports: a port through which a SIGNAL_SUBSCRIPTION_REQUIRED
// continuation parks the instance on a durable subscription, and a port
// through which [Driver.ResumeSignal] reloads the committed, matched receipt
// it advances from. Neither is a dependency on the signal store itself; a
// composition root wires the adapters. A driver with no [SignalSubscriber]
// behaves exactly as before WF-RUN-005: the continuation is
// [ErrUnsupportedContinuation].

// ErrSignalDrift reports a resumed SIGNAL node whose reloaded receipt is not
// an ACCEPTED match against a satisfied subscription, is bound to another
// instance, or names a node that is not a SIGNAL in the pinned plan. It is
// [ErrTimerDrift]'s counterpart for durable signals.
var ErrSignalDrift = errors.New("workflow execute: signal drift")

// SignalSubscriptionRequest asks the signal adapter to open the durable
// subscription a SIGNAL_SUBSCRIPTION_REQUIRED continuation describes.
//
// It carries the pinned plan and compiled SIGNAL node because the event type,
// schema, accepted sources, ordering and close window live on the node, and
// it carries the instance's own correlation facts because a definition cannot
// know the correlation value a particular instance waits under.
type SignalSubscriptionRequest struct {
	Continuation  runtime.ContinuationRecord
	Plan          *workflow.CompiledWorkflow
	Node          workflow.CompiledNode
	Proposal      runtime.ProposalBinding
	CorrelationID string
	SubjectRefs   []string
	CreatedAt     time.Time
}

// SignalHandle is the durable subscription the adapter opened (or
// recognised on a replayed advancement).
type SignalHandle struct {
	SubscriptionID uuid.UUID
	NodeID         string
	Attempt        int
	Replay         bool
}

// SignalSubscriber opens the durable subscription for a
// SIGNAL_SUBSCRIPTION_REQUIRED continuation inside the same transaction as the
// advancement that raised it.
type SignalSubscriber interface {
	CreateSubscription(ctx context.Context, ex runtime.Executor, req SignalSubscriptionRequest) (SignalHandle, error)
}

// MatchedSignalQuery names the committed receipt a resume advances from.
type MatchedSignalQuery struct {
	TenantID       uuid.UUID
	SignalID       uuid.UUID
	SubscriptionID uuid.UUID
}

// MatchedSignal is the committed receipt [SignalReader] hands back. It
// carries the continuation reference, never the signal payload.
type MatchedSignal struct {
	SignalID       uuid.UUID
	SubscriptionID uuid.UUID
	InstanceID     uuid.UUID
	NodeID         string
	NodeAttempt    int
	// Settled reports that the receipt is an ACCEPTED disposition against a
	// SATISFIED subscription whose continuation is the reference to this
	// exact signal.
	Settled         bool
	ContinuationRef string
	// Causal is the subscription's stored causal identity. It links the
	// resume span and never governs the advancement.
	Causal *runtime.CausalMetadata
}

// SignalReader loads the matched receipt [Driver.ResumeSignal] advances from,
// inside the advancement's own transaction.
type SignalReader interface {
	LoadMatchedSignal(ctx context.Context, ex runtime.Executor, q MatchedSignalQuery) (MatchedSignal, error)
}

// ExpiredSubscriptionQuery names the committed EXPIRED wait a timeout resume
// advances from.
type ExpiredSubscriptionQuery struct {
	TenantID       uuid.UUID
	SubscriptionID uuid.UUID
}

// ExpiredSubscription is the committed EXPIRED wait
// [SignalTimeoutReader] hands back. It carries the timeout continuation
// reference, never any signal payload: an expiry has no signal.
type ExpiredSubscription struct {
	SubscriptionID uuid.UUID
	InstanceID     uuid.UUID
	NodeID         string
	NodeAttempt    int
	// Settled reports that the wait is EXPIRED with the timeout continuation
	// referencing this exact subscription.
	Settled         bool
	ContinuationRef string
	// Causal is the wait's stored causal identity. It links the resume span
	// and never governs the advancement.
	Causal *runtime.CausalMetadata
}

// SignalTimeoutReader loads the expired wait [Driver.ResumeSignalTimeout]
// advances from, inside the advancement's own transaction.
type SignalTimeoutReader interface {
	LoadExpiredSubscription(ctx context.Context, ex runtime.Executor, q ExpiredSubscriptionQuery) (ExpiredSubscription, error)
}

// ResumeSignalTimeoutRequest names the expired wait a WAITING SIGNAL node
// times out from. It carries no outcome and no payload: the node's outcome is
// always TIMED_OUT, derived from the committed expiry alone, and the
// advancement records the wait's timeout continuation reference as the node's
// output.
type ResumeSignalTimeoutRequest struct {
	Start                   runtime.StartRequest
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	SubscriptionID          uuid.UUID
	Refs                    runtime.GovernanceRefs
	RecordedAt              time.Time
}

// ResumeSignalRequest names the matched receipt a WAITING SIGNAL node resumes
// from. It carries no outcome and no payload: the node's outcome is derived
// from the committed receipt alone, and the advancement records the
// receipt's continuation reference as the node's output.
type ResumeSignalRequest struct {
	Start                   runtime.StartRequest
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	SignalID                uuid.UUID
	SubscriptionID          uuid.UUID
	Refs                    runtime.GovernanceRefs
	RecordedAt              time.Time
}

// ResumeSignal advances a WAITING SIGNAL node from the matched receipt
// [SignalReader] loads, then drains any READY successors exactly as Execute
// does. It is fenced and leased like [Driver.ResumeTimer]; the instance
// version compare-and-swap and the node's own settled state make a second
// resume from the same receipt a refusal rather than a second advancement.
func (d *Driver) ResumeSignal(ctx context.Context, req ResumeSignalRequest) (ret0 Result, retErr error) {
	ctx, obsOp := observe.Begin(d.observed(ctx), "workflow.execute.resume_signal", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	selection, err := validateResumeSignalConfig(ctx, req, d.opts.SignalReader)
	if err != nil {
		return Result{}, err
	}
	run := runContext{
		start: req.Start, selection: selection, instanceID: req.InstanceID,
		traceID: d.opts.Instrumentation.TraceID(ctx),
	}
	at := req.RecordedAt.UTC()
	if req.RecordedAt.IsZero() {
		at = d.opts.Clock().UTC()
	}
	ctx, release, err := d.acquireInstanceLease(ctx, req.Start.TenantID, req.InstanceID)
	if err != nil {
		return Result{}, err
	}
	defer func() { retErr = releasing(retErr, release) }()

	advanced, created, evidenceIDs, timers, err := d.advanceOnce(ctx, run, req.ExpectedInstanceVersion, at, 1,
		func(ctx context.Context, ex runtime.Executor) (frontier.NodeOutcome, runtime.GovernanceRefs, *runtime.CausalMetadata, error) {
			row, loadErr := d.opts.SignalReader.LoadMatchedSignal(ctx, ex, MatchedSignalQuery{
				TenantID: req.Start.TenantID, SignalID: req.SignalID, SubscriptionID: req.SubscriptionID,
			})
			if loadErr != nil {
				return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, nil, loadErr
			}
			outcome, driftErr := checkSignalDrift(req, selection, row)
			return outcome, req.Refs, nil, driftErr
		}, nil)
	if err != nil {
		settled := d.settlePause(ctx, run, at, err)
		if paused, ok := pausedResult(settled, Result{}); ok {
			return paused, nil
		}
		return Result{}, settled
	}

	result := Result{
		Advances:        []runtime.AdvanceReceipt{advanced},
		WorkItems:       created,
		Timers:          timers,
		InstanceVersion: advanced.NewInstanceVersion,
		Frontier:        append([]string(nil), advanced.Frontier...),
		EvidenceIDs:     evidenceIDs,
	}
	return d.continueAfterAdvance(ctx, run, result, advanced)
}

// validateResumeSignalConfig checks wiring, request shape and the pinned
// plan's identity before any transaction opens. It never reads the receipt.
func validateResumeSignalConfig(ctx context.Context, req ResumeSignalRequest, reader SignalReader) (runtime.WorkflowSelection, error) {
	if reader == nil {
		return runtime.WorkflowSelection{}, invalid("resume from a signal has no SignalReader")
	}
	if req.Start.TenantID == uuid.Nil || req.InstanceID == uuid.Nil || req.SignalID == uuid.Nil ||
		req.SubscriptionID == uuid.Nil || req.ExpectedInstanceVersion < 1 {
		return runtime.WorkflowSelection{}, invalid(
			"resume from a signal requires tenant, instance, signal, subscription and a positive expected instance version")
	}
	return resolvePinnedPlan(ctx, req.Start, "resume from a signal")
}

// checkSignalDrift compares the committed receipt against the request and the
// pinned plan. The node, and the reference the node records as its output,
// are always taken from the stored row.
func checkSignalDrift(req ResumeSignalRequest, selection runtime.WorkflowSelection, row MatchedSignal) (frontier.NodeOutcome, error) {
	if row.SignalID != req.SignalID || row.SubscriptionID != req.SubscriptionID {
		return frontier.NodeOutcome{}, signalDrift("loaded receipt %s/%s is not the requested %s/%s",
			row.SignalID, row.SubscriptionID, req.SignalID, req.SubscriptionID)
	}
	if !row.Settled {
		return frontier.NodeOutcome{}, signalDrift(
			"signal %s did not settle subscription %s; only an ACCEPTED match against a satisfied subscription resumes a node",
			row.SignalID, row.SubscriptionID)
	}
	if row.ContinuationRef != "signal:"+row.SignalID.String() {
		return frontier.NodeOutcome{}, signalDrift("receipt continuation %q is not the reference to signal %s", row.ContinuationRef, row.SignalID)
	}
	if row.InstanceID != req.InstanceID {
		return frontier.NodeOutcome{}, signalDrift("signal %s settled instance %s, not %s", row.SignalID, row.InstanceID, req.InstanceID)
	}
	node, ok := selection.Plan.Node(row.NodeID)
	if !ok || node.Type != workflow.StepSignal {
		return frontier.NodeOutcome{}, signalDrift("signal %s settled node %s, which is not a SIGNAL in the pinned plan", row.SignalID, row.NodeID)
	}
	return frontier.NodeOutcome{NodeID: row.NodeID, Outcome: workflow.OutcomeSucceeded, OutputDigest: row.ContinuationRef}, nil
}

// ResumeSignalTimeout advances a WAITING SIGNAL node with the TIMED_OUT
// outcome from the expired wait [SignalTimeoutReader] loads, then drains any
// READY successors exactly as Execute does. It is fenced and leased like
// [Driver.ResumeSignal]; the instance version compare-and-swap and the node's
// own settled state make a second timeout resume from the same wait a refusal
// rather than a second advancement. The outcome routes through the node's
// declared TIMED_OUT edge: a wait that closed with no signal repairs instead
// of completing silently.
func (d *Driver) ResumeSignalTimeout(ctx context.Context, req ResumeSignalTimeoutRequest) (ret0 Result, retErr error) {
	ctx, obsOp := observe.Begin(d.observed(ctx), "workflow.execute.resume_signal_timeout", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	selection, err := validateResumeSignalTimeoutConfig(ctx, req, d.opts.SignalTimeoutReader)
	if err != nil {
		return Result{}, err
	}
	run := runContext{
		start: req.Start, selection: selection, instanceID: req.InstanceID,
		traceID: d.opts.Instrumentation.TraceID(ctx),
	}
	at := req.RecordedAt.UTC()
	if req.RecordedAt.IsZero() {
		at = d.opts.Clock().UTC()
	}
	ctx, release, err := d.acquireInstanceLease(ctx, req.Start.TenantID, req.InstanceID)
	if err != nil {
		return Result{}, err
	}
	defer func() { retErr = releasing(retErr, release) }()

	advanced, created, evidenceIDs, timers, err := d.advanceOnce(ctx, run, req.ExpectedInstanceVersion, at, 1,
		func(ctx context.Context, ex runtime.Executor) (frontier.NodeOutcome, runtime.GovernanceRefs, *runtime.CausalMetadata, error) {
			row, loadErr := d.opts.SignalTimeoutReader.LoadExpiredSubscription(ctx, ex, ExpiredSubscriptionQuery{
				TenantID: req.Start.TenantID, SubscriptionID: req.SubscriptionID,
			})
			if loadErr != nil {
				return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, nil, loadErr
			}
			outcome, driftErr := checkSignalTimeout(req, selection, row)
			return outcome, req.Refs, nil, driftErr
		}, nil)
	if err != nil {
		settled := d.settlePause(ctx, run, at, err)
		if paused, ok := pausedResult(settled, Result{}); ok {
			return paused, nil
		}
		return Result{}, settled
	}

	result := Result{
		Advances:        []runtime.AdvanceReceipt{advanced},
		WorkItems:       created,
		Timers:          timers,
		InstanceVersion: advanced.NewInstanceVersion,
		Frontier:        append([]string(nil), advanced.Frontier...),
		EvidenceIDs:     evidenceIDs,
	}
	return d.continueAfterAdvance(ctx, run, result, advanced)
}

// validateResumeSignalTimeoutConfig checks wiring, request shape and the
// pinned plan's identity before any transaction opens. It never reads the
// expired wait.
func validateResumeSignalTimeoutConfig(ctx context.Context, req ResumeSignalTimeoutRequest, reader SignalTimeoutReader) (runtime.WorkflowSelection, error) {
	if reader == nil {
		return runtime.WorkflowSelection{}, invalid("resume from a signal timeout has no SignalTimeoutReader")
	}
	if req.Start.TenantID == uuid.Nil || req.InstanceID == uuid.Nil ||
		req.SubscriptionID == uuid.Nil || req.ExpectedInstanceVersion < 1 {
		return runtime.WorkflowSelection{}, invalid(
			"resume from a signal timeout requires tenant, instance, subscription and a positive expected instance version")
	}
	return resolvePinnedPlan(ctx, req.Start, "resume from a signal timeout")
}

// checkSignalTimeout compares the committed expiry against the request and
// the pinned plan. The outcome is always TIMED_OUT: only an EXPIRED wait
// whose timeout continuation references this exact subscription advances a
// node, and the node, and the reference the node records as its output, are
// always taken from the stored row.
func checkSignalTimeout(req ResumeSignalTimeoutRequest, selection runtime.WorkflowSelection, row ExpiredSubscription) (frontier.NodeOutcome, error) {
	if row.SubscriptionID != req.SubscriptionID {
		return frontier.NodeOutcome{}, signalDrift("loaded expiry %s is not the requested %s",
			row.SubscriptionID, req.SubscriptionID)
	}
	if !row.Settled {
		return frontier.NodeOutcome{}, signalDrift(
			"subscription %s is not an expired wait; only an EXPIRED wait with its timeout continuation resumes a node as TIMED_OUT",
			row.SubscriptionID)
	}
	if row.ContinuationRef != timeoutContinuationRef(row.SubscriptionID) {
		return frontier.NodeOutcome{}, signalDrift("expiry continuation %q is not the timeout reference to subscription %s",
			row.ContinuationRef, row.SubscriptionID)
	}
	if row.InstanceID != req.InstanceID {
		return frontier.NodeOutcome{}, signalDrift("expiry %s settled instance %s, not %s", row.SubscriptionID, row.InstanceID, req.InstanceID)
	}
	node, ok := selection.Plan.Node(row.NodeID)
	if !ok || node.Type != workflow.StepSignal {
		return frontier.NodeOutcome{}, signalDrift("expiry %s settled node %s, which is not a SIGNAL in the pinned plan", row.SubscriptionID, row.NodeID)
	}
	return frontier.NodeOutcome{NodeID: row.NodeID, Outcome: workflow.Outcome("TIMED_OUT"), OutputDigest: row.ContinuationRef}, nil
}

// requireSignalSubscription is the sink half: it opens the durable
// subscription through the configured port, or refuses exactly as the driver
// did before WF-RUN-005 when none is configured.
func (s *continuationSink) requireSignalSubscription(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) error {
	if s.signals == nil {
		return unsupported("SIGNAL_SUBSCRIPTION_REQUIRED", rec.TargetNodeID)
	}
	if s.plan == nil {
		return invalid("SIGNAL_SUBSCRIPTION_REQUIRED for node %s but the sink was built without a pinned plan", rec.TargetNodeID)
	}
	node, ok := s.plan.Node(rec.TargetNodeID)
	if !ok || node.Type != workflow.StepSignal {
		return invalid("SIGNAL_SUBSCRIPTION_REQUIRED names node %s, which is not a SIGNAL in the pinned plan", rec.TargetNodeID)
	}
	handle, err := s.signals.CreateSubscription(ctx, ex, SignalSubscriptionRequest{
		Continuation: rec, Plan: s.plan, Node: node, Proposal: s.proposal,
		CorrelationID: s.correlationID, SubjectRefs: append([]string(nil), s.subjectRefs...), CreatedAt: rec.RecordedAt,
	})
	if err != nil {
		return err
	}
	if handle.NodeID != "" && handle.NodeID != rec.TargetNodeID {
		return invalid("SignalSubscriber returned a subscription for node %s while parking %s", handle.NodeID, rec.TargetNodeID)
	}
	return nil
}

func signalDrift(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrSignalDrift, fmt.Sprintf(format, args...))
}

// timeoutContinuationRef is the reference an expired wait's continuation
// carries: the timeout namespace apart from signal receipts, naming the
// expired subscription. internal/data/signals.ExpiryContinuationRef is the
// durable source of this reference; the adapter proves they agree, and this
// check keeps a row that names anything else from advancing a node as a
// timeout.
func timeoutContinuationRef(subscriptionID uuid.UUID) string {
	return "signal-timeout:" + subscriptionID.String()
}
