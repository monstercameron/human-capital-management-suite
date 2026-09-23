// Package workflowcontrol turns the operator workflow interventions -- Pause,
// Resume, Cancel and RetryNode (EP-WF-002) plus WF-RUN-015's skip, satisfy,
// override, rewind, supersede and reconcile (REV-009-02) -- into governed
// operator actions.
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
//
// The same door carries WF-RUN-015's typed interventions
// ([Controller.Intervene]): a command with an [InterventionSpec] is evaluated
// by internal/workflow/intervention against the durable instance, performed
// only through the runtime's own transitions, and -- when accepted -- recorded
// as an immutable decision in the same transaction. There is no generic force
// operation, and an action with no executable path is a typed denial.
package workflowcontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
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
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/cancellation"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/intervention"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
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
	// CodeCompensationRequired marks an APPLIED cancel that left the
	// instance CANCELLING with a recorded compensation obligation.
	CodeCompensationRequired = "COMPENSATION_REQUIRED"
	// CodeChildNotCancellable marks a TOO_LATE cancel refused by a child
	// workflow instance that cannot stop.
	CodeChildNotCancellable = "CHILD_NOT_CANCELLABLE"
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
	// Intervention, when set, makes the command a typed workflow intervention
	// (WF-RUN-015); see [Controller.Intervene].
	Intervention *InterventionSpec
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
	// DecisionID and DecisionDigest identify the immutable decision an
	// accepted intervention recorded; Cause is the gateway refusal code an
	// INTERVENTION_UNAUTHORIZED denial carries.
	DecisionID     string
	DecisionDigest string
	Cause          string
	// durable marks a result whose writes must commit even though it is not
	// a state change (a recorded CANNOT_CANCEL decision).
	durable bool
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
	// preflight dry-runs simulation-required controls; see
	// WithPreflightSimulation.
	preflight bool
	// recorder receives the controller's operations when the caller's
	// context carries none; see WithRecorder.
	recorder observe.Recorder
}

// observed returns ctx carrying the controller's recorder unless the caller
// already supplied one, so controls arriving over a transport that does not
// thread a recorder still reach spans and logs.
func (c *Controller) observed(ctx context.Context) context.Context {
	if c == nil || c.recorder == nil || observe.RecorderFrom(ctx) != nil {
		return ctx
	}
	return observe.WithRecorder(ctx, c.recorder)
}

// New composes a controller over journal: it builds the operator gateway with
// this package's four executors registered.
func New(db dbport.Beginner, journal operator.Journal, plans PlanResolver, authority AuthorityResolver, clock func() time.Time, opts ...Option) (*Controller, error) {
	if db == nil || plans == nil || authority == nil {
		return nil, fmt.Errorf("%w: database, plan resolver and authority resolver are required", ErrInvalidCommand)
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	c := &Controller{db: db, plans: plans, authority: authority, clock: clock, journalRef: journal}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	executors := map[operator.Kind]operator.Executor{
		operator.KindWorkflowPause:     operator.ExecutorFunc(c.applyPause),
		operator.KindWorkflowResume:    operator.ExecutorFunc(c.applyResume),
		operator.KindWorkflowCancel:    operator.ExecutorFunc(c.applyCancel),
		operator.KindWorkflowRetryNode: operator.ExecutorFunc(c.applyRetry),
	}
	// WORKFLOW_COMPENSATE has no executor: Intervene denies it before the
	// gateway because no compensation runner is composed.
	for _, k := range interventionKinds {
		executors[k] = c.applyIntervention(k)
	}
	gw, err := operator.NewGateway(journal, executors, clock)
	if err != nil {
		return nil, err
	}
	c.gateway = gw
	return c, nil
}

// Pause requests a pause.
func (c *Controller) Pause(ctx context.Context, cmd Command) (ret0 Result, retErr error) {
	ctx, obsOp := observe.Begin(c.observed(ctx), "workflow.control.pause", cmd)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	return c.submit(ctx, operator.KindWorkflowPause, cmd)
}

// Resume resumes a paused instance after revalidation.
func (c *Controller) Resume(ctx context.Context, cmd Command) (ret0 Result, retErr error) {
	ctx, obsOp := observe.Begin(c.observed(ctx), "workflow.control.resume", cmd)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	return c.submit(ctx, operator.KindWorkflowResume, cmd)
}

// Cancel cancels an instance at its cancellation boundary.
func (c *Controller) Cancel(ctx context.Context, cmd Command) (ret0 Result, retErr error) {
	ctx, obsOp := observe.Begin(c.observed(ctx), "workflow.control.cancel", cmd)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	return c.submit(ctx, operator.KindWorkflowCancel, cmd)
}

// RetryNode retries exactly one failed attempt.
func (c *Controller) RetryNode(ctx context.Context, cmd Command) (ret0 Result, retErr error) {
	ctx, obsOp := observe.Begin(c.observed(ctx), "workflow.control.retry_node", cmd)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
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
	if policy, _ := operator.PolicyFor(kind); c.preflight && policy.SimulationRequired && auth.Simulation == nil {
		sim, _, err := c.Simulate(ctx, kind, cmd)
		if err != nil {
			return Result{}, fmt.Errorf("workflowcontrol: preflight simulation: %w", err)
		}
		auth.Simulation = sim
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
	b.WriteString(interventionPayload(cmd.Intervention))
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
	parts := []string{effectPrefix, string(r.Outcome), r.Code, r.InstanceID.String(),
		string(r.InstanceStatus), strconv.FormatInt(r.InstanceVersion, 10), r.NodeID, strconv.Itoa(r.Attempt)}
	if r.DecisionID != "" {
		// An accepted intervention also journals the decision it recorded.
		parts = append(parts, r.DecisionID, r.DecisionDigest)
	}
	return strings.Join(parts, "|")
}

func decodeEffect(ref string) (Result, error) {
	parts := strings.Split(ref, "|")
	if (len(parts) != 8 && len(parts) != 10) || parts[0] != effectPrefix {
		return Result{}, fmt.Errorf("workflowcontrol: unreadable recorded effect %q", ref)
	}
	decisionID, decisionDigest := "", ""
	if len(parts) == 10 {
		decisionID, decisionDigest = parts[8], parts[9]
	}
	id, err := uuid.Parse(parts[3])
	if err != nil {
		return Result{}, fmt.Errorf("workflowcontrol: recorded effect instance: %w", err)
	}
	version, _ := strconv.ParseInt(parts[5], 10, 64)
	attempt, _ := strconv.Atoi(parts[7])
	return Result{Outcome: Outcome(parts[1]), Code: parts[2], InstanceID: id, InstanceStatus: runtime.InstanceStatus(parts[4]),
		InstanceVersion: version, NodeID: parts[6], Attempt: attempt, DecisionID: decisionID, DecisionDigest: decisionDigest}, nil
}

// stepFunc is one control's decision and writes inside the tenant
// transaction.
type stepFunc func(tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, now time.Time) (Result, error)

// transact runs fn in one tenant transaction, committing only when the
// result is a state change (APPLIED, PENDING_SAFE_POINT or REPAIR_REQUIRED).
func (c *Controller) transact(ctx context.Context, auth operator.Authorization, kind operator.Kind, fn stepFunc) (string, error) {
	cmd, ok := commandFrom(ctx)
	if !ok {
		return "", fmt.Errorf("%w: executor invoked without a command", ErrInvalidCommand)
	}
	if err := auth.Require(kind, cmd.Tenant); err != nil {
		return "", err
	}
	fn, err := c.decorate(ctx, kind, cmd, auth.IntentInstanceID(), fn)
	if err != nil {
		return "", noEffect("intervention", err)
	}
	res, err := c.run(ctx, cmd, fn, true)
	if err != nil {
		return "", err
	}
	return encodeEffect(res), nil
}

// run executes fn against the instance's pinned plan. With commit false the
// transaction always rolls back: the result is a dry run over exactly the
// state the real control would see.
func (c *Controller) run(ctx context.Context, cmd Command, fn stepFunc, commit bool) (Result, error) {
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return Result{}, noEffect("begin", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, cmd.TenantID); err != nil {
		return Result{}, noEffect("bind tenant", err)
	}
	inst, err := (runtime.Store{}).LoadInstance(ctx, tx, cmd.TenantID, cmd.InstanceID)
	if err != nil {
		if runtime.CodeOf(err) == runtime.CodeInstanceNotFound {
			return Result{Outcome: OutcomeDenied, Code: runtime.CodeInstanceNotFound, InstanceID: cmd.InstanceID}, nil
		}
		return Result{}, noEffect("load instance", err)
	}
	plan, err := c.plans.ResolvePlan(ctx, tx, inst)
	if err != nil {
		return Result{}, noEffect("resolve plan", err)
	}
	if plan == nil || plan.Digest() != inst.CompiledPlanHash {
		// Safe points, effect classes and idempotency are compiled facts of
		// the one plan the instance is pinned to.
		return Result{Outcome: OutcomeDenied, Code: runtime.CodeAdvancePlanMismatch, InstanceID: cmd.InstanceID,
			InstanceStatus: inst.RuntimeStatus, InstanceVersion: inst.InstanceVersion}, nil
	}
	res, err := fn(tx, cmd, inst, plan, c.clock().UTC())
	if err != nil {
		return Result{}, noEffect("apply", err)
	}
	res.InstanceID = cmd.InstanceID
	if !commit {
		return res, nil
	}
	switch {
	case res.durable, res.Outcome == OutcomeApplied, res.Outcome == OutcomePendingSafePoint, res.Outcome == OutcomeRepairRequired:
		if err := tx.Commit(ctx); err != nil {
			return Result{}, fmt.Errorf("workflowcontrol: commit: %w", err)
		}
	}
	res.durable = false
	return res, nil
}

// step returns the decision function of a control kind.
func (c *Controller) step(ctx context.Context, kind operator.Kind) (stepFunc, bool) {
	switch kind {
	case operator.KindWorkflowPause:
		return c.pauseStep(ctx), true
	case operator.KindWorkflowResume:
		return c.resumeStep(ctx), true
	case operator.KindWorkflowCancel:
		return c.cancelStep(ctx), true
	case operator.KindWorkflowRetryNode:
		return c.retryStep(ctx), true
	}
	if slices.Contains(interventionKinds, kind) {
		// An intervention-only kind has no plain step; decorate supplies it.
		return nil, true
	}
	return nil, false
}

// Simulate dry-runs a control: the exact decision and writes run against the
// instance's current state and pinned plan in a transaction that always rolls
// back. The returned simulation covers exactly the command's scope and seals
// the predicted result, so the gateway can require it and the receipt names
// what was predicted.
func (c *Controller) Simulate(ctx context.Context, kind operator.Kind, cmd Command) (ret0 *operator.Simulation, ret1 Result, retErr error) {
	ctx, obsOp := observe.Begin(c.observed(ctx), "workflow.control.simulate", cmd)
	defer func() { observe.DoneWith(obsOp, retErr, ret1) }()
	if err := cmd.validate(kind); err != nil {
		return nil, Result{}, err
	}
	fn, ok := c.step(ctx, kind)
	if !ok {
		return nil, Result{}, fmt.Errorf("%w: kind %s is not a workflow control", ErrInvalidCommand, kind)
	}
	fn, err := c.decorate(ctx, kind, cmd, "simulation:"+cmd.IdempotencyKey, fn)
	if err != nil {
		return nil, Result{}, err
	}
	at := c.clock().UTC()
	res, err := c.run(ctx, cmd, fn, false)
	if err != nil {
		return nil, Result{}, err
	}
	scope := cmd.Scope(kind)
	sum := sha256.Sum256([]byte("workflowcontrol/simulation/v1|" + string(kind) + "|" + strings.Join(scope.IDs, ",") + "|" + encodeEffect(res)))
	return &operator.Simulation{Digest: "sha256:" + hex.EncodeToString(sum[:]), Scope: scope, At: at}, res, nil
}

// Option configures a Controller.
type Option func(*Controller)

// WithPreflightSimulation makes the controller dry-run a control whose policy
// requires simulation when the operator presents none, and submit that
// simulation as the evidence. The prediction is sealed into the receipt; a
// simulation the operator already holds is never replaced.
func WithPreflightSimulation() Option {
	return func(c *Controller) { c.preflight = true }
}

// WithRecorder records every control, simulation, operator submission and
// runtime operation beneath them on r (the production recorder turns each
// into a span and a structured log line). A caller context that already
// carries a recorder keeps it.
func WithRecorder(r observe.Recorder) Option {
	return func(c *Controller) { c.recorder = r }
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
	return c.transact(ctx, auth, operator.KindWorkflowPause, c.pauseStep(ctx))
}

func (c *Controller) pauseStep(ctx context.Context) stepFunc {
	return func(tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, now time.Time) (Result, error) {
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
	}
}

func (c *Controller) applyResume(ctx context.Context, auth operator.Authorization, _ operator.Request) (string, error) {
	return c.transact(ctx, auth, operator.KindWorkflowResume, c.resumeStep(ctx))
}

func (c *Controller) resumeStep(ctx context.Context) stepFunc {
	return func(tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, now time.Time) (Result, error) {
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
	}
}

func (c *Controller) applyCancel(ctx context.Context, auth operator.Authorization, _ operator.Request) (string, error) {
	return c.transact(ctx, auth, operator.KindWorkflowCancel, c.cancelStep(ctx))
}

func (c *Controller) cancelStep(ctx context.Context) stepFunc {
	return func(tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, now time.Time) (Result, error) {
		if inst.RuntimeStatus.Terminal() {
			r := current(inst)
			r.Outcome, r.Code = OutcomeTooLate, CodeInstanceTerminal
			return r, nil
		}
		if inst.InstanceVersion != cmd.ExpectedVersion {
			return denied(inst, runtime.CodeStaleInstance), nil
		}
		// WF-RUN-010: the governed decision over the instance's durable
		// facts (node executions against the plan's cancellation semantics,
		// recorded effects, child instances), acted on and recorded in this
		// transaction.
		out, err := cancellation.Decide(ctx, tx, cancellation.Request{
			TenantID: cmd.TenantID, InstanceID: cmd.InstanceID, ExpectedInstanceVersion: cmd.ExpectedVersion,
			Plan: plan, Plans: c.plans, Reason: cmd.ReasonRef, RequestedBy: cmd.Operator, RecordedAt: now,
		})
		switch {
		case errors.Is(err, cancellation.ErrStale):
			return denied(inst, runtime.CodeStaleInstance), nil
		case errors.Is(err, cancellation.ErrPlanMismatch):
			return denied(inst, runtime.CodeAdvancePlanMismatch), nil
		case errors.Is(err, cancellation.ErrTerminal):
			r := current(inst)
			r.Outcome, r.Code = OutcomeTooLate, CodeInstanceTerminal
			return r, nil
		case err != nil:
			return runtimeRefusal(inst, err)
		}
		return cancelResult(out), nil
	}
}

// cancelResult projects a governed cancellation decision onto a control
// result. Every decision is durable (its evidence row is committed), so a
// CANNOT_CANCEL refusal is kept as well.
func cancelResult(out cancellation.Outcome) Result {
	reason, _ := out.Blocking()
	r := Result{InstanceStatus: out.Instance.RuntimeStatus, InstanceVersion: out.Instance.InstanceVersion,
		NodeID: reason.NodeID, durable: true}
	switch out.Decision {
	case workflow.Cancelled:
		r.Outcome = OutcomeApplied
	case workflow.CompensationRequired:
		// The instance is CANCELLING with the compensation obligation
		// recorded; it is not reported cancelled.
		r.Outcome, r.Code = OutcomeApplied, CodeCompensationRequired
	case workflow.CannotCancel:
		// A produced effect cannot be released, or a child cannot stop;
		// nothing is claimed reversed.
		r.Outcome, r.Code = OutcomeTooLate, CodeEffectCommitted
		if reason.Code == cancellation.ReasonChildNotCancellable {
			r.Code = CodeChildNotCancellable
		}
	default:
		r.Outcome, r.Code = OutcomeRepairRequired, CodeEffectInFlight
		if reason.Code != cancellation.ReasonEffectAmbiguous && reason.Code != "" {
			r.Code = reason.Code
		}
	}
	return r
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
	return c.transact(ctx, auth, operator.KindWorkflowRetryNode, c.retryStep(ctx))
}

func (c *Controller) retryStep(ctx context.Context) stepFunc {
	return func(tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, now time.Time) (Result, error) {
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
	}
}

// Request is a transport-shaped control: identifiers are strings, so a
// transport adapter needs no identifier library of its own. Route,
// TargetNodeID, Replacement, Observation and EvidenceRefs carry a WF-RUN-015
// typed intervention (REV-009-02): the skip, satisfy, override, rewind,
// supersede and reconcile kinds run through Controller.Intervene from this
// same request, so one endpoint covers all ten intervention kinds.
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
	// Route is the declared route a SATISFY or OVERRIDE takes.
	Route string
	// TargetNodeID is the earlier node a REWIND returns control to.
	TargetNodeID string
	// Replacement is the instance UUID a SUPERSEDE links to, empty for
	// every other kind.
	Replacement string
	// Observation is what a RECONCILE observed: EFFECT_APPLIED or
	// EFFECT_NOT_APPLIED, empty for every other kind.
	Observation string
	// EvidenceRefs are the references the intervention cites. At least one
	// is required.
	EvidenceRefs []string
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
	// DecisionID and DecisionDigest identify the immutable decision an
	// accepted intervention recorded; Cause is the gateway refusal code an
	// INTERVENTION_UNAUTHORIZED denial carries. All three are empty for
	// the pause, resume, cancel and retry controls.
	DecisionID     string
	DecisionDigest string
	Cause          string
}

// interventionSpec translates a transport-shaped request into the typed
// intervention the command carries. A replacement that is not an instance
// UUID is refused before the gateway; every other malformed field is denied
// by the intervention contract itself, recording nothing.
func interventionSpec(req Request) (InterventionSpec, error) {
	var kind intervention.Kind
	switch req.Kind {
	case operator.KindWorkflowSkip:
		kind = intervention.Skip
	case operator.KindWorkflowSatisfy:
		kind = intervention.Satisfy
	case operator.KindWorkflowOverride:
		kind = intervention.Override
	case operator.KindWorkflowRewind:
		kind = intervention.Rewind
	case operator.KindWorkflowSupersede:
		kind = intervention.Supersede
	case operator.KindWorkflowReconcile:
		kind = intervention.Reconcile
	default:
		return InterventionSpec{}, fmt.Errorf("%w: kind %q is not a workflow intervention", ErrInvalidCommand, req.Kind)
	}
	spec := InterventionSpec{Kind: kind, NodeID: req.NodeID, Route: req.Route, TargetNodeID: req.TargetNodeID,
		Observation: intervention.Observation(req.Observation), EvidenceRefs: req.EvidenceRefs}
	if strings.TrimSpace(req.Replacement) != "" {
		replacement, err := uuid.Parse(strings.TrimSpace(req.Replacement))
		if err != nil {
			return InterventionSpec{}, fmt.Errorf("%w: replacement: %w", ErrInvalidCommand, err)
		}
		spec.Replacement = replacement
	}
	return spec, nil
}

// TenantIDs maps a tenant key to its storage identity.
type TenantIDs func(values.TenantId) (uuid.UUID, error)

// Handle runs one transport-shaped control.
func (c *Controller) Handle(ctx context.Context, tenantIDs TenantIDs, req Request) (ret0 Response, retErr error) {
	ctx, obsOp := observe.Begin(c.observed(ctx), "workflow.control.handle", tenantIDs, req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
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
	case operator.KindWorkflowSkip, operator.KindWorkflowSatisfy, operator.KindWorkflowOverride,
		operator.KindWorkflowRewind, operator.KindWorkflowSupersede, operator.KindWorkflowReconcile:
		spec, specErr := interventionSpec(req)
		if specErr != nil {
			return Response{}, specErr
		}
		cmd.Intervention = &spec
		res, err = c.Intervene(ctx, cmd)
	default:
		return Response{}, fmt.Errorf("%w: kind %q is not a workflow control", ErrInvalidCommand, req.Kind)
	}
	if err != nil {
		return Response{}, err
	}
	out := Response{Outcome: res.Outcome, Code: res.Code, InstanceStatus: string(res.InstanceStatus), NodeID: res.NodeID,
		IntentInstanceID: res.IntentInstanceID, ReceiptDigest: res.ReceiptDigest, Replayed: res.Replayed,
		DecisionID: res.DecisionID, DecisionDigest: res.DecisionDigest, Cause: res.Cause}
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
