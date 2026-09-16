package workflowcontrol

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/intervention"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// InterventionSpec is the typed workflow intervention (WF-RUN-015) a
// [Command] carries. It never names a target state: the transition follows
// from the kind, the durable instance and its pinned plan.
type InterventionSpec struct {
	Kind intervention.Kind
	// NodeID addresses RETRY, SKIP, SATISFY, OVERRIDE and RECONCILE;
	// ExpectedAttempt fences RETRY.
	NodeID          string
	ExpectedAttempt int
	// Route is the declared route a SATISFY or OVERRIDE takes.
	Route string
	// TargetNodeID is the earlier node a REWIND returns control to.
	TargetNodeID string
	// Replacement is the instance a SUPERSEDE links to.
	Replacement uuid.UUID
	// Observation is what a RECONCILE observed.
	Observation intervention.Observation
	// EvidenceRefs are the references the intervention cites. At least one
	// is required.
	EvidenceRefs []string
}

// OperatorKindFor returns the operator gateway kind -- and so the grant
// capability and policy -- an intervention kind is submitted under.
func OperatorKindFor(k intervention.Kind) (operator.Kind, bool) {
	switch k {
	case intervention.Retry:
		return operator.KindWorkflowRetryNode, true
	case intervention.Resume:
		return operator.KindWorkflowResume, true
	case intervention.Skip:
		return operator.KindWorkflowSkip, true
	case intervention.Satisfy:
		return operator.KindWorkflowSatisfy, true
	case intervention.Override:
		return operator.KindWorkflowOverride, true
	case intervention.Rewind:
		return operator.KindWorkflowRewind, true
	case intervention.Compensate:
		return operator.KindWorkflowCompensate, true
	case intervention.Supersede:
		return operator.KindWorkflowSupersede, true
	case intervention.Reconcile:
		return operator.KindWorkflowReconcile, true
	case intervention.Cancel:
		return operator.KindWorkflowCancel, true
	}
	return "", false
}

// interventionKinds are the operator kinds whose executor exists only to run
// an intervention. RETRY and RESUME reuse the retry and resume controls.
var interventionKinds = []operator.Kind{
	operator.KindWorkflowSkip, operator.KindWorkflowSatisfy, operator.KindWorkflowOverride,
	operator.KindWorkflowRewind, operator.KindWorkflowSupersede, operator.KindWorkflowReconcile,
}

// authorityCodes are the gateway refusals an intervention reports as
// INTERVENTION_UNAUTHORIZED, keeping the gateway's code as the cause.
var authorityCodes = []string{
	operator.CodeAuthorityRequired, operator.CodeAuthorityMismatch, operator.CodeAuthorityInactive,
	operator.CodeDualControlRequired, operator.CodeSimulationRequired, operator.CodeBypassReason,
}

// Intervene runs one typed workflow intervention through the governed
// operator gateway. A request that is malformed, lacks a reason, evidence or
// requester, or names an action with no executable path is denied before the
// gateway and records nothing. Otherwise the gateway demands the kind's
// authority (JIT grant, dual control, simulation) and journals a receipt, and
// the executor -- in one tenant transaction -- re-evaluates the intervention
// against the durable instance, performs an accepted plan only through the
// runtime's own transitions, reads the transition back and records the
// immutable decision. A no-op or failed precondition is a DENIED outcome that
// changes no workflow state and records no decision.
func (c *Controller) Intervene(ctx context.Context, cmd Command) (ret0 Result, retErr error) {
	ctx, obsOp := observe.Begin(c.observed(ctx), "workflow.control.intervene", cmd)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if cmd.Intervention == nil {
		return Result{}, fmt.Errorf("%w: intervention", ErrInvalidCommand)
	}
	spec := *cmd.Intervention
	if err := interventionRequest(cmd, c.clock().UTC()).Validate(); err != nil {
		code := intervention.CodeOf(err)
		if code == "" {
			return Result{}, err
		}
		return Result{Outcome: OutcomeDenied, Code: code, InstanceID: cmd.InstanceID, NodeID: spec.NodeID}, nil
	}
	kind, _ := OperatorKindFor(spec.Kind)
	cmd.NodeID, cmd.ExpectedAttempt = spec.NodeID, 0
	if spec.Kind == intervention.Retry {
		cmd.ExpectedAttempt = spec.ExpectedAttempt
	}
	if err := cmd.validate(kind); err != nil {
		return Result{}, err
	}
	// A key with a recorded receipt replays through the gateway. A new one is
	// judged against the durable instance first, in a transaction that always
	// rolls back, so a no-op or failed precondition writes nothing at all --
	// no receipt, no grant use, no decision. The executor judges it again
	// inside its own transaction, so a concurrent change is still caught.
	if _, recorded, err := c.journalRef.Lookup(ctx, cmd.Tenant, cmd.IdempotencyKey); err != nil {
		return Result{}, fmt.Errorf("workflowcontrol: look up intervention receipt: %w", err)
	} else if !recorded {
		pre, err := c.run(ctx, cmd, c.intervened(ctx, kind, "", nil), false)
		if err != nil {
			return Result{}, err
		}
		if pre.Outcome == OutcomeDenied {
			return pre, nil
		}
	}
	res, err := c.submit(ctx, kind, cmd)
	if err == nil && res.Outcome == OutcomeDenied && slices.Contains(authorityCodes, res.Code) {
		res.Cause, res.Code = res.Code, intervention.CodeUnauthorized
	}
	return res, err
}

// interventionRequest is the typed request a command's intervention makes.
func interventionRequest(cmd Command, at time.Time) intervention.Request {
	spec := InterventionSpec{}
	if cmd.Intervention != nil {
		spec = *cmd.Intervention
	}
	return intervention.Request{
		Kind: spec.Kind, InstanceID: cmd.InstanceID, ExpectedVersion: cmd.ExpectedVersion,
		NodeID: spec.NodeID, ExpectedAttempt: spec.ExpectedAttempt, Route: spec.Route,
		TargetNodeID: spec.TargetNodeID, Replacement: spec.Replacement, Observation: spec.Observation,
		Reason: cmd.ReasonRef, EvidenceRefs: slices.Clone(spec.EvidenceRefs), RequestedBy: cmd.Operator, RequestedAt: at,
	}
}

// interventionPayload pins the intervention's typed parameters into the
// gateway's request digest, so an idempotency key cannot be replayed with a
// different route, target, replacement, observation or evidence.
func interventionPayload(spec *InterventionSpec) string {
	if spec == nil {
		return ""
	}
	evidence := slices.Clone(spec.EvidenceRefs)
	slices.Sort(evidence)
	return strings.Join([]string{"|intervention", string(spec.Kind), spec.NodeID, strconv.Itoa(spec.ExpectedAttempt),
		spec.Route, spec.TargetNodeID, spec.Replacement.String(), string(spec.Observation), strings.Join(evidence, ",")}, "|")
}

// planStep performs an accepted intervention plan inside the tenant
// transaction.
type planStep func(tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow,
	req intervention.Request, p intervention.Plan, now time.Time) (Result, error)

// applyIntervention is the gateway executor of an intervention-only kind.
func (c *Controller) applyIntervention(kind operator.Kind) operator.ExecutorFunc {
	return func(ctx context.Context, auth operator.Authorization, _ operator.Request) (string, error) {
		return c.transact(ctx, auth, kind, nil)
	}
}

// decorate returns the step a control kind runs. A command carrying an
// intervention is evaluated, performed and recorded as one; a command without
// one runs the plain control, and an intervention-only kind has none.
func (c *Controller) decorate(ctx context.Context, kind operator.Kind, cmd Command, intentID string, base stepFunc) (stepFunc, error) {
	if cmd.Intervention == nil {
		if base == nil {
			return nil, fmt.Errorf("%w: %s runs only as a typed intervention", ErrInvalidCommand, kind)
		}
		return base, nil
	}
	if want, _ := OperatorKindFor(cmd.Intervention.Kind); want != kind {
		return nil, fmt.Errorf("%w: intervention %s is not submitted as %s", ErrInvalidCommand, cmd.Intervention.Kind, kind)
	}
	inner := c.executePlan(ctx)
	if base != nil {
		inner = func(tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, _ intervention.Request, _ intervention.Plan, now time.Time) (Result, error) {
			return base(tx, cmd, inst, plan, now)
		}
	}
	return c.intervened(ctx, kind, intentID, inner), nil
}

// intervened evaluates the intervention against the durable facts, performs
// an accepted plan, reads the transition back and records the decision.
func (c *Controller) intervened(ctx context.Context, kind operator.Kind, intentID string, inner planStep) stepFunc {
	return func(tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, now time.Time) (Result, error) {
		req := interventionRequest(cmd, now)
		facts, err := loadFacts(ctx, tx, cmd, inst, plan, req)
		if err != nil {
			return Result{}, err
		}
		p, err := intervention.Evaluate(req, facts)
		if err != nil {
			code := intervention.CodeOf(err)
			if code == "" {
				return Result{}, err
			}
			r := denied(inst, code)
			r.NodeID = req.NodeID
			return r, nil
		}
		if inner == nil {
			// A precheck: the plan is acceptable, nothing is performed.
			return Result{Outcome: OutcomeApplied, InstanceStatus: inst.RuntimeStatus, InstanceVersion: inst.InstanceVersion}, nil
		}
		res, err := inner(tx, cmd, inst, plan, req, p, now)
		if err != nil || res.Outcome != OutcomeApplied {
			return res, err
		}
		observed, err := readBack(ctx, tx, cmd, p)
		if err != nil {
			return Result{}, err
		}
		d, err := intervention.NewDecision(req, p, intervention.Binding{TenantID: cmd.TenantID, OperatorKind: string(kind),
			IntentInstanceID: intentID, IdempotencyKey: cmd.IdempotencyKey}, observed)
		if err == nil {
			err = intervention.DecisionStore{}.Record(ctx, tx, d)
		}
		switch code := intervention.CodeOf(err); {
		case err == nil:
		case code == intervention.CodeStaleVersion || code == intervention.CodeNoOp:
			// Nothing commits: a concurrent intervention already decided this
			// version, or the runtime moved nothing.
			return denied(inst, code), nil
		default:
			return Result{}, err
		}
		res.DecisionID, res.DecisionDigest = d.DecisionID.String(), d.Digest
		return res, nil
	}
}

func loadFacts(ctx context.Context, tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, req intervention.Request) (intervention.Facts, error) {
	store := runtime.Store{}
	nodes, err := store.LoadNodeExecutions(ctx, tx, cmd.TenantID, cmd.InstanceID)
	if err != nil {
		return intervention.Facts{}, err
	}
	facts := intervention.Facts{Plan: plan, Instance: inst, Nodes: nodes}
	if req.Kind == intervention.Supersede && req.Replacement != uuid.Nil {
		rep, err := store.LoadInstance(ctx, tx, cmd.TenantID, req.Replacement)
		switch {
		case err == nil:
			facts.Replacement = &rep
		case runtime.CodeOf(err) != runtime.CodeInstanceNotFound:
			return intervention.Facts{}, err
		}
	}
	return facts, nil
}

// readBack reads the durable state the transition left: the instance and the
// latest attempt of the node the plan acted on.
func readBack(ctx context.Context, tx dbport.Tx, cmd Command, p intervention.Plan) (intervention.Observed, error) {
	store := runtime.Store{}
	inst, err := store.LoadInstance(ctx, tx, cmd.TenantID, cmd.InstanceID)
	if err != nil {
		return intervention.Observed{}, err
	}
	out := intervention.Observed{InstanceStatus: inst.RuntimeStatus, InstanceVersion: inst.InstanceVersion}
	nodeID := p.Node.NodeID
	if p.Target.NodeID != "" {
		nodeID = p.Target.NodeID
	}
	if nodeID == "" {
		return out, nil
	}
	nodes, err := store.LoadNodeExecutions(ctx, tx, cmd.TenantID, cmd.InstanceID)
	if err != nil {
		return intervention.Observed{}, err
	}
	for _, n := range nodes {
		if n.NodeID == nodeID && n.Attempt >= out.Node.Attempt {
			out.Node = intervention.NodeRef{NodeID: n.NodeID, Attempt: n.Attempt, Status: n.Status}
		}
	}
	return out, nil
}

// executePlan performs the intervention-only kinds through the runtime's own
// transitions. RETRY and RESUME run the retry and resume controls instead.
func (c *Controller) executePlan(ctx context.Context) planStep {
	return func(tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, req intervention.Request, p intervention.Plan, now time.Time) (Result, error) {
		ref := "intervention:" + intervention.DecisionID(cmd.TenantID, req, cmd.IdempotencyKey).String()
		switch p.Kind {
		case intervention.Skip, intervention.Satisfy, intervention.Override:
			return routeNode(ctx, tx, cmd, inst, plan, p, ref, now)
		case intervention.Rewind:
			return rewind(ctx, tx, cmd, inst, plan, p, now)
		case intervention.Supersede:
			return supersede(ctx, tx, cmd, inst, p, now)
		case intervention.Reconcile:
			return reconcile(ctx, tx, cmd, inst, req, p, ref, now)
		}
		return denied(inst, intervention.CodeNotSupported), nil
	}
}

// routeNode settles the addressed node along its declared route through
// runtime.Advance, cancelling the node's own outstanding timers.
func routeNode(ctx context.Context, tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, p intervention.Plan, ref string, now time.Time) (Result, error) {
	receipt, err := runtime.Advance(ctx, tx, runtime.AdvanceRequest{
		TenantID: cmd.TenantID, InstanceID: cmd.InstanceID, ExpectedInstanceVersion: inst.InstanceVersion,
		Attempt: p.Node.Attempt, Plan: plan, SettleAs: p.Settle, RecordedAt: now, Sink: readySink{},
		Outcome: frontier.NodeOutcome{NodeID: p.Node.NodeID, Outcome: workflow.Outcome(p.Route), OutputDigest: p.OutputDigest},
		Refs:    runtime.GovernanceRefs{RepairRef: ref},
	})
	if err != nil {
		return refusalOf(inst, err)
	}
	if err := cancelTimers(ctx, tx, cmd, []string{p.Node.NodeID}, now); err != nil {
		return Result{}, err
	}
	return Result{Outcome: OutcomeApplied, InstanceStatus: runtime.InstanceRunning, InstanceVersion: receipt.NewInstanceVersion,
		NodeID: p.Node.NodeID, Attempt: p.Node.Attempt}, nil
}

// rewind cancels the frontier's live attempts, restages the target as a new
// attempt and moves the paused instance's frontier to it. Every earlier
// attempt stays recorded.
func rewind(ctx context.Context, tx dbport.Tx, cmd Command, inst runtime.Instance, plan *workflow.CompiledWorkflow, p intervention.Plan, now time.Time) (Result, error) {
	store := runtime.Store{}
	version, err := cancelAttempts(ctx, tx, cmd, inst.InstanceVersion, p.Cancels, now)
	if err != nil {
		return refusalOf(inst, err)
	}
	cancelled := make([]string, 0, len(p.Cancels))
	for _, n := range p.Cancels {
		cancelled = append(cancelled, n.NodeID)
	}
	if err := cancelTimers(ctx, tx, cmd, cancelled, now); err != nil {
		return Result{}, err
	}
	target, _ := plan.Node(p.Target.NodeID)
	next := runtime.NewNodeExecution(cmd.TenantID, cmd.InstanceID, target.ID, p.Target.Attempt, target.Type, runtime.NodeReady)
	next.RecordedAt = now
	if _, version, err = store.RecordNodeExecution(ctx, tx, next, version); err != nil {
		return refusalOf(inst, err)
	}
	t := transitionOf(inst, runtime.InstancePaused)
	t.ExpectedVersion, t.CurrentNodeIDs = version, []string{target.ID}
	moved, err := store.RecordInstanceState(ctx, tx, t)
	if err != nil {
		return refusalOf(inst, err)
	}
	if err := (runtimestate.ReadyWorkStore{}).Enqueue(ctx, tx, runtimestate.ReadyWork{
		TenantID: cmd.TenantID, ReadyWorkID: uuid.New(), InstanceID: cmd.InstanceID, NodeID: target.ID,
		Attempt: p.Target.Attempt, EligibleAt: now, EnqueuedAt: now,
	}); err != nil {
		return Result{}, err
	}
	return Result{Outcome: OutcomeApplied, InstanceStatus: moved.RuntimeStatus, InstanceVersion: moved.InstanceVersion,
		NodeID: target.ID, Attempt: p.Target.Attempt}, nil
}

// supersede cancels the instance's live attempts and timers and ends it
// SUPERSEDED; the replacement link is the recorded decision.
func supersede(ctx context.Context, tx dbport.Tx, cmd Command, inst runtime.Instance, p intervention.Plan, now time.Time) (Result, error) {
	version, err := cancelAttempts(ctx, tx, cmd, inst.InstanceVersion, p.Cancels, now)
	if err != nil {
		return refusalOf(inst, err)
	}
	if _, err := (timer.Scheduler{}).CancelInstance(ctx, tx, cmd.TenantID, cmd.InstanceID, now, "workflow superseded by "+p.Replacement.String()); err != nil {
		return Result{}, err
	}
	t := transitionOf(inst, runtime.InstanceSuperseded)
	t.ExpectedVersion, t.CurrentNodeIDs, t.CompletedAt = version, nil, &now
	t.CompletionDimensions = runtime.Dimensions{RequestState: "SUPERSEDED", ExecutionState: "NOT_PLANNED",
		BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"}
	ended, err := (runtime.Store{}).RecordInstanceState(ctx, tx, t)
	if err != nil {
		return refusalOf(inst, err)
	}
	return Result{Outcome: OutcomeApplied, InstanceStatus: ended.RuntimeStatus, InstanceVersion: ended.InstanceVersion}, nil
}

// reconcile records the observed outcome of an in-flight effect on its
// attempt: SUCCEEDED with the evidence as its effect references when the
// effect applied, FAILED when it did not. The instance stays in repair.
func reconcile(ctx context.Context, tx dbport.Tx, cmd Command, inst runtime.Instance, req intervention.Request, p intervention.Plan, ref string, now time.Time) (Result, error) {
	path := []runtime.NodeStatus{p.NodeTo}
	if !runtime.LegalNodeTransition(p.Node.Status, p.NodeTo) {
		path = []runtime.NodeStatus{runtime.NodeRunning, p.NodeTo}
	}
	version := inst.InstanceVersion
	for i, status := range path {
		t := runtime.NodeTransition{TenantID: cmd.TenantID, InstanceID: cmd.InstanceID, NodeID: p.Node.NodeID,
			Attempt: p.Node.Attempt, ExpectedInstanceVersion: version, Status: status}
		if i == len(path)-1 {
			t.CompletedAt, t.Refs.RepairRef = &now, ref
			if p.Observation == intervention.ObservedApplied {
				t.Refs.EffectRefs = slices.Clone(req.EvidenceRefs)
			} else {
				t.ErrorClass = "RECONCILED_" + string(p.Observation)
			}
		}
		var err error
		if _, version, err = (runtime.Store{}).RecordNodeTransition(ctx, tx, t); err != nil {
			return refusalOf(inst, err)
		}
	}
	return Result{Outcome: OutcomeApplied, InstanceStatus: inst.RuntimeStatus, InstanceVersion: version,
		NodeID: p.Node.NodeID, Attempt: p.Node.Attempt}, nil
}

// cancelAttempts moves each live attempt to CANCELLED, returning the instance
// version the next write must hold.
func cancelAttempts(ctx context.Context, tx dbport.Tx, cmd Command, version int64, attempts []intervention.NodeRef, now time.Time) (int64, error) {
	for _, n := range attempts {
		var err error
		if _, version, err = (runtime.Store{}).RecordNodeTransition(ctx, tx, runtime.NodeTransition{
			TenantID: cmd.TenantID, InstanceID: cmd.InstanceID, NodeID: n.NodeID, Attempt: n.Attempt,
			ExpectedInstanceVersion: version, Status: runtime.NodeCancelled, CompletedAt: &now,
		}); err != nil {
			return 0, err
		}
	}
	return version, nil
}

// cancelTimers cancels the outstanding timers of the named nodes only.
func cancelTimers(ctx context.Context, tx dbport.Tx, cmd Command, nodeIDs []string, now time.Time) error {
	if len(nodeIDs) == 0 {
		return nil
	}
	sched := timer.Scheduler{}
	pending, err := sched.Pending(ctx, tx, cmd.TenantID, cmd.InstanceID)
	if err != nil {
		return err
	}
	for _, row := range pending {
		if !slices.Contains(nodeIDs, row.NodeID) {
			continue
		}
		if _, err := sched.Cancel(ctx, tx, cmd.TenantID, row.TimerID, now, "workflow intervention: "+cmd.ReasonRef); err != nil && !errors.Is(err, timer.ErrAlreadySettled) {
			return err
		}
	}
	return nil
}

// refusalOf maps a runtime or frontier refusal onto a governed outcome.
func refusalOf(inst runtime.Instance, err error) (Result, error) {
	switch {
	case errors.Is(err, errUnmaterialized):
		return denied(inst, intervention.CodeNotSupported), nil
	case frontier.CodeOf(err) != "":
		return denied(inst, frontier.CodeOf(err)), nil
	}
	return runtimeRefusal(inst, err)
}

// errUnmaterialized refuses a continuation the operator gateway cannot
// materialize: only ready work is. The advancement's transaction rolls back.
var errUnmaterialized = errors.New("workflowcontrol: continuation needs a work item, timer, subscription or terminal writer the operator gateway does not compose")

// readySink persists an intervention's continuations: a READY successor is
// recorded in the continuation ledger and enqueued as ready work so the
// scheduler runs it; any other intent is refused.
type readySink struct{}

func (readySink) MarkReady(ctx context.Context, ex runtime.Executor, rec runtime.ContinuationRecord) (retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.control.intervention_ready")
	defer func() { observe.DoneWith(obsOp, retErr) }()
	if err := (runtime.ContinuationStore{}).MarkReady(ctx, ex, rec); err != nil {
		return err
	}
	return (runtimestate.ReadyWorkStore{}).Enqueue(ctx, ex, runtimestate.ReadyWork{
		TenantID: rec.TenantID, ReadyWorkID: uuid.New(), InstanceID: rec.InstanceID, NodeID: rec.TargetNodeID,
		Attempt: rec.TargetAttempt, EligibleAt: rec.RecordedAt, EnqueuedAt: rec.RecordedAt,
	})
}

func (readySink) RequireWorkItem(context.Context, runtime.Executor, runtime.ContinuationRecord) error {
	return errUnmaterialized
}

func (readySink) RequireSignalSubscription(context.Context, runtime.Executor, runtime.ContinuationRecord) error {
	return errUnmaterialized
}

func (readySink) RequireTimer(context.Context, runtime.Executor, runtime.ContinuationRecord) error {
	return errUnmaterialized
}

func (readySink) Complete(context.Context, runtime.Executor, runtime.ContinuationRecord) error {
	return errUnmaterialized
}

// transitionOf carries an instance's current shape into a state write that
// changes only its status. The governed cancellation rewrite (WF-RUN-010)
// dropped the copy this file's rewind and supersede plans still need.
func transitionOf(inst runtime.Instance, status runtime.InstanceStatus) runtime.InstanceTransition {
	return runtime.InstanceTransition{
		TenantID: inst.TenantID, InstanceID: inst.InstanceID, ExpectedVersion: inst.InstanceVersion,
		Status: status, CurrentNodeIDs: append([]string(nil), inst.CurrentNodeIDs...),
		VariableRevisionHead: inst.VariableRevisionHead, EffectiveContextRef: inst.EffectiveContextRef,
		LastCheckpointRef: inst.LastCheckpointRef, CompletionDimensions: inst.CompletionDimensions,
	}
}
