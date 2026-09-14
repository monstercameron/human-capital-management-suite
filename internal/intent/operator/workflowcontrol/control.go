// Package workflowcontrol turns the four operator workflow interventions --
// Pause, Resume, Cancel and RetryNode (EP-WF-002) -- into governed operator
// actions.
//
// Every command is submitted through an [operator.Gateway], so it resolves a
// registered operational intent under current JIT authority, dual control or
// simulation where its kind demands, and an idempotency key, before its
// executor runs. The executor never edits a status column: it calls the
// runtime's own transitions (RequestPause, ResumeFromPause, the cancellation
// boundary, the node retry state machine) inside one tenant transaction, fenced
// by the caller's expected instance version or attempt, and reports exactly one
// [Outcome]:
//
//   - APPLIED: the runtime transition happened.
//   - PENDING_SAFE_POINT: a pause is recorded and takes effect at the next
//     compiled safe point.
//   - DENIED: authority, separation of duties, a stale version or attempt, a
//     leased or successful node, or a failed revalidation refused it.
//   - TOO_LATE: the instance is terminal, or an effect it would reverse already
//     succeeded; nothing is claimed reversed.
//   - REPAIR_REQUIRED: an effect is ambiguous (in flight, or not idempotent
//     to retry), so the instance is routed to repair rather than re-run.
//
// A repeated command under the same idempotency key returns the recorded
// outcome and never executes twice. Operators cannot name a target state.
package workflowcontrol

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// Outcome is the governed result of one workflow control.
type Outcome string

// Outcomes.
const (
	OutcomeApplied          Outcome = "APPLIED"
	OutcomePendingSafePoint Outcome = "PENDING_SAFE_POINT"
	OutcomeDenied           Outcome = "DENIED"
	OutcomeTooLate          Outcome = "TOO_LATE"
	OutcomeRepairRequired   Outcome = "REPAIR_REQUIRED"
)

// Denial and result codes this package introduces.
const (
	CodeNodeNotFailed      = "NODE_NOT_FAILED"
	CodeAttemptStale       = "ATTEMPT_STALE"
	CodeNodeLeased         = "NODE_LEASED"
	CodeNonIdempotentRetry = "NON_IDEMPOTENT_RETRY"
	CodeEffectCommitted    = "EFFECT_COMMITTED"
	CodeEffectInFlight     = "EFFECT_IN_FLIGHT"
	CodeInstanceTerminal   = "INSTANCE_TERMINAL"
	CodeInvalidCommand     = "INVALID_COMMAND"
)

// ErrInvalidCommand reports a command missing a required field.
var ErrInvalidCommand = errors.New("workflowcontrol: invalid command")

// Authority is what an operator presents for one control.
type Authority struct {
	JIT            *jit.Grant
	SecondApprover string
	Simulation     *operator.Simulation
	Emergency      *operator.Emergency
}

// AuthorityResolver finds the current authority an operator holds for a
// control. It is a port: grants live in the trust store, not in a request.
type AuthorityResolver interface {
	ResolveAuthority(ctx context.Context, tenant values.TenantId, operatorID string, kind operator.Kind, instanceID string) (Authority, error)
}

// PlanResolver returns the exact compiled plan an instance is pinned to.
type PlanResolver interface {
	ResolvePlan(ctx context.Context, ex runtime.Executor, inst runtime.Instance) (*workflow.CompiledWorkflow, error)
}

// Command is one control request.
type Command struct {
	TenantID        uuid.UUID
	Tenant          values.TenantId
	InstanceID      uuid.UUID
	ExpectedVersion int64
	// NodeID and ExpectedAttempt address RetryNode.
	NodeID          string
	ExpectedAttempt int
	IdempotencyKey  string
	ReasonRef       string
	Operator        string
	// ResolvedContext is the resume-time revalidation evidence.
	ResolvedContext map[string]string
}

// Result is what a control produced.
type Result struct {
	Outcome          Outcome
	Code             string
	InstanceID       uuid.UUID
	InstanceStatus   runtime.InstanceStatus
	InstanceVersion  int64
	NodeID           string
	Attempt          int
	IntentInstanceID string
	ReceiptDigest    string
	Replayed         bool
}

// Controller runs governed workflow controls.
type Controller struct {
	db        dbport.Beginner
	gateway   *operator.Gateway
	plans     PlanResolver
	authority AuthorityResolver
	clock     func() time.Time
	// journalRef is the journal the gateway records into.
	journalRef operator.Journal
}

// New composes a controller over journal: it builds the operator gateway with
// this package's four executors registered.
func New(db dbport.Beginner, journal operator.Journal, plans PlanResolver, authority AuthorityResolver, clock func() time.Time) (*Controller, error) {
	if db == nil || plans == nil || authority == nil {
		return nil, fmt.Errorf("%w: database, plan resolver and authority resolver are required", ErrInvalidCommand)
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	c := &Controller{db: db, plans: plans, authority: authority, clock: clock, journalRef: journal}
	gw, err := operator.NewGateway(journal, map[operator.Kind]operator.Executor{
		operator.KindWorkflowPause:     operator.ExecutorFunc(c.applyPause),
		operator.KindWorkflowResume:    operator.ExecutorFunc(c.applyResume),
		operator.KindWorkflowCancel:    operator.ExecutorFunc(c.applyCancel),
		operator.KindWorkflowRetryNode: operator.ExecutorFunc(c.applyRetry),
	}, clock)
	if err != nil {
		return nil, err
	}
	c.gateway = gw
	return c, nil
}

// Pause requests a pause.
func (c *Controller) Pause(ctx context.Context, cmd Command) (Result, error) {
	return c.submit(ctx, operator.KindWorkflowPause, cmd)
}

// Resume resumes a paused instance after revalidation.
func (c *Controller) Resume(ctx context.Context, cmd Command) (Result, error) {
	return c.submit(ctx, operator.KindWorkflowResume, cmd)
}

// Cancel cancels an instance at its cancellation boundary.
func (c *Controller) Cancel(ctx context.Context, cmd Command) (Result, error) {
	return c.submit(ctx, operator.KindWorkflowCancel, cmd)
}

// RetryNode retries exactly one failed attempt.
func (c *Controller) RetryNode(ctx context.Context, cmd Command) (Result, error) {
	return c.submit(ctx, operator.KindWorkflowRetryNode, cmd)
}

func (cmd Command) validate(kind operator.Kind) error {
	switch {
	case cmd.TenantID == uuid.Nil || cmd.Tenant.Validate() != nil:
		return fmt.Errorf("%w: tenant", ErrInvalidCommand)
	case cmd.InstanceID == uuid.Nil:
		return fmt.Errorf("%w: instance_id", ErrInvalidCommand)
	case strings.TrimSpace(cmd.IdempotencyKey) == "":
		return fmt.Errorf("%w: idempotency_key", ErrInvalidCommand)
	case strings.TrimSpace(cmd.ReasonRef) == "":
		return fmt.Errorf("%w: reason_ref", ErrInvalidCommand)
	case strings.TrimSpace(cmd.Operator) == "":
		return fmt.Errorf("%w: operator", ErrInvalidCommand)
	}
	if kind == operator.KindWorkflowRetryNode {
		if strings.TrimSpace(cmd.NodeID) == "" || cmd.ExpectedAttempt < 1 {
			return fmt.Errorf("%w: node_id and expected_attempt", ErrInvalidCommand)
		}
	} else if cmd.ExpectedVersion < 1 {
		return fmt.Errorf("%w: expected_instance_version", ErrInvalidCommand)
	}
	return nil
}

// Scope is the exact operator scope a command targets; a simulation must
// cover exactly this.
func (cmd Command) Scope(kind operator.Kind) operator.Scope {
	ids := []string{cmd.InstanceID.String()}
	if kind == operator.KindWorkflowRetryNode {
		ids = []string{cmd.InstanceID.String() + "/" + cmd.NodeID + "#" + strconv.Itoa(cmd.ExpectedAttempt)}
	}
	return operator.Scope{Resource: "workflow_instance", IDs: ids}
}

func (c *Controller) submit(ctx context.Context, kind operator.Kind, cmd Command) (Result, error) {
	if err := cmd.validate(kind); err != nil {
		return Result{}, err
	}
	auth, err := c.authority.ResolveAuthority(ctx, cmd.Tenant, cmd.Operator, kind, cmd.InstanceID.String())
	if err != nil {
		return Result{}, fmt.Errorf("workflowcontrol: resolve authority: %w", err)
	}
	req := operator.Request{
		Kind: kind, Tenant: cmd.Tenant, Operator: cmd.Operator, Scope: cmd.Scope(kind),
		ExpectedVersion: strconv.FormatInt(cmd.ExpectedVersion, 10) + "/" + strconv.Itoa(cmd.ExpectedAttempt),
		PayloadDigest:   payloadDigest(cmd), IdempotencyKey: cmd.IdempotencyKey,
		Reason: cmd.ReasonRef, TicketRef: cmd.ReasonRef,
		JIT: auth.JIT, SecondApprover: auth.SecondApprover, Simulation: auth.Simulation, Emergency: auth.Emergency,
	}
	ctx = withCommand(ctx, cmd)
	receipt, err := c.gateway.Submit(ctx, req)
	switch code := operator.CodeOf(err); {
	case err == nil:
	case code == operator.CodeRepairRequired:
		return Result{Outcome: OutcomeRepairRequired, Code: code, InstanceID: cmd.InstanceID,
			IntentInstanceID: receipt.IntentInstanceID, ReceiptDigest: receipt.Digest}, nil
	case code == operator.CodeJournal || code == "":
		return Result{}, err
	default:
		// Authority, separation of duties, simulation and idempotency
		// refusals are governed denials, not failures.
		return Result{Outcome: OutcomeDenied, Code: code, InstanceID: cmd.InstanceID, NodeID: cmd.NodeID}, nil
	}
	res, parseErr := decodeEffect(receipt.EffectRef)
	if parseErr != nil {
		return Result{}, parseErr
	}
	res.IntentInstanceID, res.ReceiptDigest = receipt.IntentInstanceID, receipt.Digest
	res.Replayed = receipt.Outcome == operator.OutcomeDuplicate
	return res, nil
}

// payloadDigest pins the resolved context a resume revalidates with, so the
// same key cannot be replayed against different evidence.
func payloadDigest(cmd Command) string {
	keys := make([]string, 0, len(cmd.ResolvedContext))
	for k := range cmd.ResolvedContext {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("workflowcontrol/v1")
	for _, k := range keys {
		b.WriteString("|" + k + "=" + cmd.ResolvedContext[k])
	}
	return b.String()
}

type commandKey struct{}

func withCommand(ctx context.Context, cmd Command) context.Context {
	return context.WithValue(ctx, commandKey{}, cmd)
}

func commandFrom(ctx context.Context) (Command, bool) {
	cmd, ok := ctx.Value(commandKey{}).(Command)
	return cmd, ok
}

// effectPrefix versions the effect reference the gateway journals, so a
// replayed receipt decodes to exactly the original result.
const effectPrefix = "workflowcontrol/v1"

func encodeEffect(r Result) string {
	return strings.Join([]string{effectPrefix, string(r.Outcome), r.Code, r.InstanceID.String(),
		string(r.InstanceStatus), strconv.FormatInt(r.InstanceVersion, 10), r.NodeID, strconv.Itoa(r.Attempt)}, "|")
}

func decodeEffect(ref string) (Result, error) {
	parts := strings.Split(ref, "|")
	if len(parts) != 8 || parts[0] != effectPrefix {
		return Result{}, fmt.Errorf("workflowcontrol: unreadable recorded effect %q", ref)
	}
	id, err := uuid.Parse(parts[3])
	if err != nil {
		return Result{}, fmt.Errorf("workflowcontrol: recorded effect instance: %w", err)
	}
	version, _ := strconv.ParseInt(parts[5], 10, 64)
	attempt, _ := strconv.Atoi(parts[7])
	return Result{Outcome: Outcome(parts[1]), Code: parts[2], InstanceID: id, InstanceStatus: runtime.InstanceStatus(parts[4]),
		InstanceVersion: version, NodeID: parts[6], Attempt: attempt}, nil
}

// transact runs fn in one tenant transaction, committing only when the
// result is a state change (APPLIED, PENDING_SAFE_POINT or REPAIR_REQUIRED).
func (c *Controller) transact(ctx context.Context, auth operator.Authorization, kind operator.Kind, fn func(tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, now time.Time) (Result, error)) (string, error) {
	cmd, ok := commandFrom(ctx)
	if !ok {
		return "", fmt.Errorf("%w: executor invoked without a command", ErrInvalidCommand)
	}
	if err := auth.Require(kind, cmd.Tenant); err != nil {
		return "", err
	}
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return "", noEffect("begin", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, cmd.TenantID); err != nil {
		return "", noEffect("bind tenant", err)
	}
	inst, err := (runtime.Store{}).LoadInstance(ctx, tx, cmd.TenantID, cmd.InstanceID)
	if err != nil {
		if runtime.CodeOf(err) == runtime.CodeInstanceNotFound {
			return encodeEffect(Result{Outcome: OutcomeDenied, Code: runtime.CodeInstanceNotFound, InstanceID: cmd.InstanceID}), nil
		}
		return "", noEffect("load instance", err)
	}
	plan, err := c.plans.ResolvePlan(ctx, tx, inst)
	if err != nil {
		return "", noEffect("resolve plan", err)
	}
	if plan == nil || plan.Digest() != inst.CompiledPlanHash {
		// Safe points, effect classes and idempotency are compiled facts of
		// the one plan the instance is pinned to.
		return encodeEffect(Result{Outcome: OutcomeDenied, Code: runtime.CodeAdvancePlanMismatch, InstanceID: cmd.InstanceID,
			InstanceStatus: inst.RuntimeStatus, InstanceVersion: inst.InstanceVersion}), nil
	}
	res, err := fn(tx, cmd, inst, plan, c.clock().UTC())
	if err != nil {
		return "", noEffect("apply", err)
	}
	res.InstanceID = cmd.InstanceID
	switch res.Outcome {
	case OutcomeApplied, OutcomePendingSafePoint, OutcomeRepairRequired:
		if err := tx.Commit(ctx); err != nil {
			return "", fmt.Errorf("workflowcontrol: commit: %w", err)
		}
	}
	return encodeEffect(res), nil
}

// noEffect marks a failure that happened before commit: the transaction rolls
// back, so the gateway may abort the pending receipt and the key stays
// retryable.
func noEffect(step string, err error) error {
	return fmt.Errorf("workflowcontrol: %s: %w: %w", step, operator.ErrNoEffect, err)
}

func current(inst runtime.Instance) Result {
	return Result{InstanceStatus: inst.RuntimeStatus, InstanceVersion: inst.InstanceVersion}
}

func denied(inst runtime.Instance, code string) Result {
	r := current(inst)
	r.Outcome, r.Code = OutcomeDenied, code
	return r
}

// runtimeRefusal maps a runtime refusal onto a governed outcome; any other
// error is returned as a failure.
func runtimeRefusal(inst runtime.Instance, err error) (Result, error) {
	switch code := runtime.CodeOf(err); code {
	case "", runtime.CodeStorageFailed:
		return Result{}, err
	case runtime.CodeIllegalTransition:
		if inst.RuntimeStatus.Terminal() {
			r := current(inst)
			r.Outcome, r.Code = OutcomeTooLate, CodeInstanceTerminal
			return r, nil
		}
		return denied(inst, code), nil
	default:
		return denied(inst, code), nil
	}
}

func (c *Controller) applyPause(ctx context.Context, auth operator.Authorization, _ operator.Request) (string, error) {
	return c.transact(ctx, auth, operator.KindWorkflowPause, func(tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, now time.Time) (Result, error) {
		if inst.RuntimeStatus.Terminal() {
			r := current(inst)
			r.Outcome, r.Code = OutcomeTooLate, CodeInstanceTerminal
			return r, nil
		}
		receipt, err := runtime.RequestPause(ctx, tx, runtime.PauseRequest{
			TenantID: cmd.TenantID, InstanceID: cmd.InstanceID, ExpectedInstanceVersion: cmd.ExpectedVersion,
			Plan: plan, Reason: cmd.ReasonRef, RequestedBy: cmd.Operator, RequestedAt: now,
		})
		if err != nil {
			return runtimeRefusal(inst, err)
		}
		r := Result{InstanceStatus: receipt.Status, InstanceVersion: receipt.InstanceVersion, Outcome: OutcomeApplied}
		if receipt.Status != runtime.InstancePaused {
			r.Outcome, r.NodeID = OutcomePendingSafePoint, receipt.BlockingNodeID
		}
		return r, nil
	})
}

func (c *Controller) applyResume(ctx context.Context, auth operator.Authorization, _ operator.Request) (string, error) {
	return c.transact(ctx, auth, operator.KindWorkflowResume, func(tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, now time.Time) (Result, error) {
		if inst.RuntimeStatus.Terminal() {
			r := current(inst)
			r.Outcome, r.Code = OutcomeTooLate, CodeInstanceTerminal
			return r, nil
		}
		receipt, err := runtime.ResumeFromPause(ctx, tx, runtime.ResumeRequest{
			TenantID: cmd.TenantID, InstanceID: cmd.InstanceID, ExpectedInstanceVersion: cmd.ExpectedVersion,
			Plan: plan, ResolvedContext: cmd.ResolvedContext, Reason: cmd.ReasonRef, ResumedBy: cmd.Operator, ResumedAt: now,
		})
		if err != nil {
			return runtimeRefusal(inst, err)
		}
		return Result{Outcome: OutcomeApplied, InstanceStatus: receipt.Status, InstanceVersion: receipt.InstanceVersion}, nil
	})
}

func (c *Controller) applyCancel(ctx context.Context, auth operator.Authorization, _ operator.Request) (string, error) {
	return c.transact(ctx, auth, operator.KindWorkflowCancel, func(tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, now time.Time) (Result, error) {
		if inst.RuntimeStatus.Terminal() {
			r := current(inst)
			r.Outcome, r.Code = OutcomeTooLate, CodeInstanceTerminal
			return r, nil
		}
		if inst.InstanceVersion != cmd.ExpectedVersion {
			return denied(inst, runtime.CodeStaleInstance), nil
		}
		store := runtime.Store{}
		nodes, err := store.LoadNodeExecutions(ctx, tx, cmd.TenantID, cmd.InstanceID)
		if err != nil {
			return Result{}, err
		}
		inFlight := ""
		for _, n := range nodes {
			node, ok := plan.Node(n.NodeID)
			if !ok || !node.EffectClass.IsWrite() {
				continue
			}
			switch n.Status {
			case runtime.NodeSucceeded:
				// The effect is done; cancelling now would claim a reversal
				// nothing performed.
				r := current(inst)
				r.Outcome, r.Code, r.NodeID, r.Attempt = OutcomeTooLate, CodeEffectCommitted, n.NodeID, n.Attempt
				return r, nil
			case runtime.NodeRunning, runtime.NodeWaiting:
				if inFlight == "" {
					inFlight = n.NodeID
				}
			}
		}
		cancelling, err := store.RecordInstanceState(ctx, tx, transitionOf(inst, runtime.InstanceCancelling))
		if err != nil {
			return runtimeRefusal(inst, err)
		}
		if inFlight != "" {
			repair := transitionOf(cancelling, runtime.InstanceRepairRequired)
			repair.CompletionDimensions = runtime.Dimensions{RequestState: "CANCELLED", ExecutionState: "UNKNOWN",
				BusinessState: "UNKNOWN", ConsistencyState: "REPAIR_REQUIRED", ObligationState: "NOT_APPLICABLE"}
			repair.CompletedAt = &now
			routed, err := store.RecordInstanceState(ctx, tx, repair)
			if err != nil {
				return runtimeRefusal(inst, err)
			}
			return Result{Outcome: OutcomeRepairRequired, Code: CodeEffectInFlight, InstanceStatus: routed.RuntimeStatus,
				InstanceVersion: routed.InstanceVersion, NodeID: inFlight}, nil
		}
		if _, err := (timer.Scheduler{}).CancelInstance(ctx, tx, cmd.TenantID, cmd.InstanceID, now, "workflow cancelled: "+cmd.ReasonRef); err != nil {
			return Result{}, err
		}
		done := transitionOf(cancelling, runtime.InstanceCancelled)
		done.CurrentNodeIDs = nil
		done.CompletionDimensions = runtime.Dimensions{RequestState: "CANCELLED", ExecutionState: "NOT_PLANNED",
			BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"}
		done.CompletedAt = &now
		cancelled, err := store.RecordInstanceState(ctx, tx, done)
		if err != nil {
			return runtimeRefusal(inst, err)
		}
		return Result{Outcome: OutcomeApplied, InstanceStatus: cancelled.RuntimeStatus, InstanceVersion: cancelled.InstanceVersion}, nil
	})
}

func transitionOf(inst runtime.Instance, status runtime.InstanceStatus) runtime.InstanceTransition {
	return runtime.InstanceTransition{
		TenantID: inst.TenantID, InstanceID: inst.InstanceID, ExpectedVersion: inst.InstanceVersion,
		Status: status, CurrentNodeIDs: append([]string(nil), inst.CurrentNodeIDs...),
		VariableRevisionHead: inst.VariableRevisionHead, EffectiveContextRef: inst.EffectiveContextRef,
		LastCheckpointRef: inst.LastCheckpointRef, CompletionDimensions: inst.CompletionDimensions,
	}
}

// retrySafe reports whether a node may be re-run without duplicating an
// effect: it has no write effect, or its write is idempotent and not
// irreversible.
func retrySafe(node workflow.CompiledNode) bool {
	if !node.EffectClass.IsWrite() {
		return true
	}
	if node.Capability == nil || node.EffectClass == "IRREVERSIBLE_EXTERNAL_MUTATION" {
		return false
	}
	return node.Capability.IdempotencyPolicyRef != "" && node.Capability.IdempotencyKeyMapping != ""
}

func (c *Controller) applyRetry(ctx context.Context, auth operator.Authorization, _ operator.Request) (string, error) {
	return c.transact(ctx, auth, operator.KindWorkflowRetryNode, func(tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, now time.Time) (Result, error) {
		base := current(inst)
		base.NodeID, base.Attempt = cmd.NodeID, cmd.ExpectedAttempt
		if inst.RuntimeStatus.Terminal() {
			base.Outcome, base.Code = OutcomeTooLate, CodeInstanceTerminal
			return base, nil
		}
		node, ok := plan.Node(cmd.NodeID)
		if !ok {
			base.Outcome, base.Code = OutcomeDenied, runtime.CodeNodeExecutionNotFound
			return base, nil
		}
		store := runtime.Store{}
		nodes, err := store.LoadNodeExecutions(ctx, tx, cmd.TenantID, cmd.InstanceID)
		if err != nil {
			return Result{}, err
		}
		var latest *runtime.NodeExecution
		for i := range nodes {
			if nodes[i].NodeID == cmd.NodeID && (latest == nil || nodes[i].Attempt > latest.Attempt) {
				latest = &nodes[i]
			}
		}
		switch {
		case latest == nil:
			base.Outcome, base.Code = OutcomeDenied, runtime.CodeNodeExecutionNotFound
			return base, nil
		case latest.Attempt != cmd.ExpectedAttempt:
			base.Outcome, base.Code = OutcomeDenied, CodeAttemptStale
			return base, nil
		case latest.Status != runtime.NodeFailed:
			base.Outcome, base.Code = OutcomeDenied, CodeNodeNotFailed
			return base, nil
		}
		observed, err := (lease.Manager{}).Observe(ctx, tx, cmd.TenantID,
			lease.Resource{Kind: lease.ResourceNodeExecution, ID: cmd.InstanceID.String() + "/" + cmd.NodeID}, now)
		if err != nil {
			return Result{}, err
		}
		if observed.Held && !observed.Expired {
			base.Outcome, base.Code = OutcomeDenied, CodeNodeLeased
			return base, nil
		}
		if !retrySafe(node) {
			base.Outcome, base.Code = OutcomeRepairRequired, CodeNonIdempotentRetry
			// Nothing is written: the attempt stays FAILED and is routed to a
			// repair plan rather than re-run.
			return base, nil
		}
		_, version, err := store.RecordNodeTransition(ctx, tx, runtime.NodeTransition{
			TenantID: cmd.TenantID, InstanceID: cmd.InstanceID, NodeID: cmd.NodeID, Attempt: latest.Attempt,
			ExpectedInstanceVersion: inst.InstanceVersion, Status: runtime.NodeRetrying,
		})
		if err != nil {
			return runtimeRefusal(inst, err)
		}
		next := runtime.NewNodeExecution(cmd.TenantID, cmd.InstanceID, cmd.NodeID, latest.Attempt+1, latest.StepType, runtime.NodeReady)
		next.InputSnapshotRef = latest.InputSnapshotRef
		next.RecordedAt = now
		if _, version, err = store.RecordNodeExecution(ctx, tx, next, version); err != nil {
			return runtimeRefusal(inst, err)
		}
		if err := (runtimestate.ReadyWorkStore{}).Enqueue(ctx, tx, runtimestate.ReadyWork{
			TenantID: cmd.TenantID, ReadyWorkID: uuid.New(), InstanceID: cmd.InstanceID, NodeID: cmd.NodeID,
			Attempt: latest.Attempt + 1, EligibleAt: now, EnqueuedAt: now,
		}); err != nil {
			return Result{}, err
		}
		return Result{Outcome: OutcomeApplied, InstanceStatus: inst.RuntimeStatus, InstanceVersion: version,
			NodeID: cmd.NodeID, Attempt: latest.Attempt + 1}, nil
	})
}

// Request is a transport-shaped control: identifiers are strings, so a
// transport adapter needs no identifier library of its own.
type Request struct {
	Kind            operator.Kind
	Tenant          values.TenantId
	InstanceID      string
	ExpectedVersion uint64
	NodeID          string
	ExpectedAttempt uint32
	IdempotencyKey  string
	ReasonRef       string
	Operator        string
}

// Response is a transport-shaped control result.
type Response struct {
	Outcome          Outcome
	Code             string
	InstanceID       string
	InstanceStatus   string
	InstanceVersion  uint64
	NodeID           string
	Attempt          uint32
	IntentInstanceID string
	ReceiptDigest    string
	Replayed         bool
}

// TenantIDs maps a tenant key to its storage identity.
type TenantIDs func(values.TenantId) (uuid.UUID, error)

// Handle runs one transport-shaped control.
func (c *Controller) Handle(ctx context.Context, tenantIDs TenantIDs, req Request) (Response, error) {
	if tenantIDs == nil {
		return Response{}, fmt.Errorf("%w: tenant mapping", ErrInvalidCommand)
	}
	tenantID, err := tenantIDs(req.Tenant)
	if err != nil {
		return Response{}, fmt.Errorf("%w: tenant: %w", ErrInvalidCommand, err)
	}
	instanceID, err := uuid.Parse(req.InstanceID)
	if err != nil {
		return Response{}, fmt.Errorf("%w: instance_id", ErrInvalidCommand)
	}
	cmd := Command{
		TenantID: tenantID, Tenant: req.Tenant, InstanceID: instanceID, ExpectedVersion: int64(req.ExpectedVersion),
		NodeID: req.NodeID, ExpectedAttempt: int(req.ExpectedAttempt), IdempotencyKey: req.IdempotencyKey,
		ReasonRef: req.ReasonRef, Operator: req.Operator,
	}
	var res Result
	switch req.Kind {
	case operator.KindWorkflowPause:
		res, err = c.Pause(ctx, cmd)
	case operator.KindWorkflowResume:
		res, err = c.Resume(ctx, cmd)
	case operator.KindWorkflowCancel:
		res, err = c.Cancel(ctx, cmd)
	case operator.KindWorkflowRetryNode:
		res, err = c.RetryNode(ctx, cmd)
	default:
		return Response{}, fmt.Errorf("%w: kind %q is not a workflow control", ErrInvalidCommand, req.Kind)
	}
	if err != nil {
		return Response{}, err
	}
	out := Response{Outcome: res.Outcome, Code: res.Code, InstanceStatus: string(res.InstanceStatus), NodeID: res.NodeID,
		IntentInstanceID: res.IntentInstanceID, ReceiptDigest: res.ReceiptDigest, Replayed: res.Replayed}
	if res.InstanceID != uuid.Nil {
		out.InstanceID = res.InstanceID.String()
	}
	if res.InstanceVersion > 0 {
		out.InstanceVersion = uint64(res.InstanceVersion)
	}
	if res.Attempt > 0 {
		out.Attempt = uint32(res.Attempt)
	}
	return out, nil
}
