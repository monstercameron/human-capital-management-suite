// Package cancellation is the served governed workflow cancellation
// (WF-RUN-010): it builds the cancellation facts of one workflow instance
// from durable state, asks the pure policy [workflow.DecideCancellation] for
// the verdict and acts on it inside the caller's transaction, recording one
// append-only decision row (migration 00304) with its phase and effect
// evidence.
//
// The facts are exactly what the runtime has durably recorded:
//
//   - every node execution, judged against the pinned plan's declared
//     cancellation semantics ([workflow.CancellationSemanticsOf]): a
//     succeeded write is a produced effect (compensable when the node binds a
//     published compensation, irreversible otherwise); a write still in
//     flight, or a finished attempt that recorded an effect reference without
//     succeeding, is unconfirmed, so the request's observer resolves it
//     before the verdict when one is composed (confirmed produced is judged
//     by its declared undo contract, confirmed absent needs no verdict,
//     and only what stays unknown is ambiguous); a node the plan does not
//     declare is ambiguous;
//   - every child workflow instance linked to the instance
//     (workflow_child_link), judged recursively the same way and reported
//     through subworkflow.PropagateCancellation.
//
// The verdict is acted on, never merely reported:
//
//   - CANCELLED: live children are cancelled first (each with its own row),
//     pending timers are cancelled, and the instance moves CANCELLING ->
//     CANCELLED;
//   - COMPENSATION_REQUIRED: the instance moves to CANCELLING -- it stops
//     advancing but is not declared cancelled -- and the row carries the
//     compensation obligation (the published compensation references);
//   - CANNOT_CANCEL: the refusal and its reasons are recorded and nothing
//     else changes;
//   - REPAIR_REQUIRED: the instance moves CANCELLING -> REPAIR_REQUIRED.
//
// The instance row is locked (SELECT ... FOR UPDATE) before any fact is read,
// so advancement and a second cancellation serialize behind it; a
// cancellation that finds a decision already recorded for the instance's
// current version returns that decision instead of deciding again, so
// concurrent cancels converge on one decision. Nothing is deleted: node
// executions, frontier history and earlier decisions all stay.
package cancellation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/subworkflow"
)

// Refusals.
var (
	// ErrInvalid reports a request missing a required field.
	ErrInvalid = errors.New("workflow cancellation: invalid request")
	// ErrTerminal reports an instance that already ended with no decision
	// recorded for its current state: history is never rewritten.
	ErrTerminal = errors.New("workflow cancellation: terminal instance cannot be cancelled")
	// ErrStale reports a caller whose expected instance version was overtaken.
	ErrStale = errors.New("workflow cancellation: stale instance version")
	// ErrPlanMismatch reports a plan that is not the one the instance is pinned
	// to: cancellation semantics are facts of exactly that plan.
	ErrPlanMismatch = errors.New("workflow cancellation: plan does not match the instance")
)

// Reason codes a decision records.
const (
	ReasonEffectCompensation  = "EFFECT_COMPENSATION"
	ReasonEffectIrreversible  = "EFFECT_IRREVERSIBLE"
	ReasonEffectCorrection    = "EFFECT_CORRECTION"
	ReasonEffectAmbiguous     = "EFFECT_AMBIGUOUS"
	ReasonNodeUndeclared      = "NODE_UNDECLARED"
	ReasonChildNotCancellable = "CHILD_NOT_CANCELLABLE"
	ReasonChildRepair         = "CHILD_REPAIR_REQUIRED"
)

// ObservedEffect is what bounded observation of one unconfirmed effect
// found. Produced reports the effect was confirmed produced (it is judged
// by its declared undo contract) or confirmed never produced (it needs no
// verdict: nothing committed).
type ObservedEffect struct {
	Produced bool
}

// ObserveEffectRequest asks the observer to resolve one unconfirmed effect.
type ObserveEffectRequest struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	NodeID     string
	Attempt    int
	// EffectID is the judged effect, "nodeID#attempt".
	EffectID string
	Status   runtime.NodeStatus
	// EffectRefs are the effect references the unconfirmed attempt recorded.
	EffectRefs []string
}

// EffectObserver resolves one unconfirmed effect before the cancellation
// verdict. It is called at most once per unconfirmed effect; any error,
// including a timeout, leaves exactly that effect unresolved. It must bound
// itself (the caller's context carries the deadline): cancellation never
// waits out an observer, it repairs the effect the observer could not read.
type EffectObserver interface {
	ObserveEffect(ctx context.Context, ex Executor, req ObserveEffectRequest) (ObservedEffect, error)
}

// maxChildDepth bounds child recursion; a deeper tree is not guessed at.
const maxChildDepth = 8

// Executor is the transaction the caller began and will finish.
type Executor = runtime.Executor

// PlanResolver returns the exact compiled plan an instance is pinned to. It
// is consulted for child instances; the decided instance's plan is the
// request's own.
type PlanResolver interface {
	ResolvePlan(ctx context.Context, ex runtime.Executor, inst runtime.Instance) (*workflow.CompiledWorkflow, error)
}

// Request asks for one governed cancellation.
type Request struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	// ExpectedInstanceVersion fences the caller's view; zero accepts the
	// version read under the instance lock (a caller fenced elsewhere, such as
	// an intent's own revision compare-and-swap).
	ExpectedInstanceVersion int64
	Plan                    *workflow.CompiledWorkflow
	// Plans resolves child plans; nil resolves only children pinned to Plan.
	Plans PlanResolver
	// Observer resolves every unconfirmed effect before the verdict. Nil
	// observes nothing: unconfirmed effects stay ambiguous and need repair,
	// as before. When composed, an effect the observer confirms produced is
	// judged by its declared undo contract, one confirmed never produced
	// needs no verdict, and only an effect the observer cannot resolve
	// stays unresolved: its siblings are still decided.
	Observer    EffectObserver
	Reason      string
	RequestedBy string
	RecordedAt  time.Time
}

// Reason is one fact that shaped a decision.
type Reason struct {
	Code   string `json:"code"`
	NodeID string `json:"node_id,omitempty"`
	Ref    string `json:"ref"`
}

func (r Reason) encode() string { return r.Code + "|" + r.NodeID + "|" + r.Ref }

func decodeReason(s string) Reason {
	parts := strings.SplitN(s, "|", 3)
	for len(parts) < 3 {
		parts = append(parts, "")
	}
	return Reason{Code: parts[0], NodeID: parts[1], Ref: parts[2]}
}

// ChildDecision names a child decision a parent decision propagated.
type ChildDecision struct {
	InstanceID uuid.UUID                     `json:"instance_id"`
	DecisionID uuid.UUID                     `json:"decision_id"`
	Decision   workflow.CancellationDecision `json:"decision"`
}

// Outcome is one recorded governed cancellation decision.
type Outcome struct {
	DecisionID       uuid.UUID
	Decision         workflow.CancellationDecision
	Evidence         workflow.CancellationOutcome
	Reasons          []Reason
	CompensationRefs []string
	Children         []ChildDecision
	StatusBefore     runtime.InstanceStatus
	VersionBefore    int64
	// Instance is the instance after the decision was acted on.
	Instance runtime.Instance
	// Replayed reports a decision already recorded for the instance's current
	// state, returned instead of deciding again.
	Replayed bool
}

// Blocking returns the recorded reason that produced the decision, in
// priority order: the ambiguity behind REPAIR_REQUIRED, the uncancellable
// child (whose lifted effect is also recorded) or irreversible effect behind
// CANNOT_CANCEL, the compensation behind COMPENSATION_REQUIRED.
func (o Outcome) Blocking() (Reason, bool) {
	var codes []string
	switch o.Decision {
	case workflow.RepairRequired:
		codes = []string{ReasonEffectAmbiguous, ReasonNodeUndeclared, ReasonChildRepair}
	case workflow.CannotCancel:
		codes = []string{ReasonChildNotCancellable, ReasonEffectIrreversible, ReasonEffectCorrection}
	case workflow.CompensationRequired:
		codes = []string{ReasonEffectCompensation}
	}
	for _, code := range codes {
		for _, r := range o.Reasons {
			if r.Code == code {
				return r, true
			}
		}
	}
	return Reason{}, false
}

// Decide builds the instance's cancellation facts from durable state, decides
// and acts on the verdict in ex, and records the decision.
func Decide(ctx context.Context, ex Executor, req Request) (ret0 Outcome, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.cancellation.decide",
		observe.Attrs{observe.KeyTenant: req.TenantID.String()}, observe.Attrs{observe.KeyInstance: req.InstanceID.String()})
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if ex == nil || req.TenantID == uuid.Nil || req.InstanceID == uuid.Nil || req.Plan == nil ||
		strings.TrimSpace(req.Reason) == "" || strings.TrimSpace(req.RequestedBy) == "" || req.RecordedAt.IsZero() ||
		req.ExpectedInstanceVersion < 0 {
		return Outcome{}, fmt.Errorf("%w: tenant, instance, plan, reason, actor and RecordedAt are required", ErrInvalid)
	}
	inst, err := lockInstance(ctx, ex, req.TenantID, req.InstanceID)
	if err != nil {
		return Outcome{}, err
	}
	if prior, found, err := loadCurrentDecision(ctx, ex, inst); err != nil || found {
		return prior, err
	}
	if inst.RuntimeStatus.Terminal() {
		return Outcome{}, fmt.Errorf("%w: instance %s is %s", ErrTerminal, inst.InstanceID, inst.RuntimeStatus)
	}
	if req.ExpectedInstanceVersion > 0 && req.ExpectedInstanceVersion != inst.InstanceVersion {
		return Outcome{}, fmt.Errorf("%w: expected %d, stored %d", ErrStale, req.ExpectedInstanceVersion, inst.InstanceVersion)
	}
	if req.Plan.Digest() != inst.CompiledPlanHash {
		return Outcome{}, fmt.Errorf("%w: instance %s is pinned to %s", ErrPlanMismatch, inst.InstanceID, inst.CompiledPlanHash)
	}
	d := decider{req: req}
	j, err := d.judge(ctx, ex, inst, req.Plan, 0)
	if err != nil {
		return Outcome{}, err
	}
	return d.apply(ctx, ex, j, uuid.Nil)
}

type decider struct{ req Request }

// pendingObservation is one unconfirmed effect awaiting the verdict: the
// observer request that resolves it and the ambiguous record carrying its
// declared undo contract.
type pendingObservation struct {
	req ObserveEffectRequest
	rec workflow.EffectRecord
}

// judged is one instance's facts and verdict, before anything is written.
type judged struct {
	inst         runtime.Instance
	plan         *workflow.CompiledWorkflow
	outcome      workflow.CancellationOutcome
	reasons      []Reason
	compensation []string
	// children are live children whose own verdict is acted on when the
	// parent's is CANCELLED or COMPENSATION_REQUIRED.
	children []*judged
}

func (d decider) judge(ctx context.Context, ex Executor, inst runtime.Instance, plan *workflow.CompiledWorkflow, depth int) (*judged, error) {
	j := &judged{inst: inst, plan: plan}
	nodes, err := (runtime.Store{}).LoadNodeExecutions(ctx, ex, inst.TenantID, inst.InstanceID)
	if err != nil {
		return nil, err
	}
	var effects []workflow.EffectRecord
	// observable names every unconfirmed effect whose undo contract the
	// plan declares, so the observer resolves exactly what a confirmed
	// produced verdict can judge. An effect no node declares stays
	// ambiguous without observation: producing it cannot be judged.
	// Reasons for observed effects are recorded after the verdict (below),
	// never before: only the observed outcome tells whether the effect
	// compensates, keeps, or needs no verdict at all.
	observable := make(map[string]pendingObservation)
	for _, n := range nodes {
		id := n.NodeID + "#" + strconv.Itoa(n.Attempt)
		cn, ok := plan.Node(n.NodeID)
		var sem workflow.CancellationSemantics
		if ok {
			sem, err = workflow.CancellationSemanticsOf(cn)
		}
		if !ok || err != nil {
			effects = append(effects, workflow.EffectRecord{ID: id, Ambiguous: true})
			j.reasons = append(j.reasons, Reason{Code: ReasonNodeUndeclared, NodeID: n.NodeID, Ref: id})
			continue
		}
		settled, has := effectState(n)
		if !has {
			continue
		}
		rec, ok := sem.Effect(id, settled)
		if !ok {
			continue
		}
		effects = append(effects, rec)
		if !settled && d.req.Observer != nil {
			observable[id] = pendingObservation{
				req: ObserveEffectRequest{TenantID: inst.TenantID, InstanceID: inst.InstanceID,
					NodeID: n.NodeID, Attempt: n.Attempt, EffectID: id, Status: n.Status, EffectRefs: n.Refs.EffectRefs},
				rec: rec,
			}
			continue
		}
		j.addEffectReason(rec, n.NodeID, "")
	}

	links, err := (runtimestate.ChildLinkStore{}).LinksForParent(ctx, ex, inst.TenantID, inst.InstanceID)
	if err != nil {
		return nil, err
	}
	var children []workflow.CancellableNode
	for _, link := range links {
		node, childEffects, err := d.judgeChild(ctx, ex, j, link, depth)
		if err != nil {
			return nil, err
		}
		children = append(children, node)
		effects = append(effects, childEffects...)
	}

	var observe workflow.ObserveEffect
	if d.req.Observer != nil {
		observer := d.req.Observer
		observe = func(id string) (workflow.EffectObservation, error) {
			target, ok := observable[id]
			if !ok {
				return workflow.EffectObservation{}, fmt.Errorf("workflow cancellation: no declared execution for unconfirmed effect %s", id)
			}
			obs, err := observer.ObserveEffect(ctx, ex, target.req)
			if err != nil {
				return workflow.EffectObservation{}, err
			}
			return workflow.EffectObservation{Produced: obs.Produced}, nil
		}
	}
	j.outcome, err = workflow.DecideCancellation(workflow.CancellationRequest{
		RunID:    inst.InstanceID.String(),
		Revision: plan.Digest() + "#v" + strconv.FormatInt(inst.InstanceVersion, 10),
		Phase:    phaseOf(inst),
		Children: children,
		Effects:  effects,
		Observe:  observe,
	})
	if err != nil {
		return nil, err
	}
	j.observeReasons(observable)
	return j, nil
}

// observeReasons records the reasons for effects the observer resolved,
// from the verdict each received: a confirmed produced effect is judged by
// its declared contract exactly as a settled one, a confirmed absent
// effect committed nothing and needs no reason, and only an effect the
// observer could not resolve keeps its ambiguity.
func (j *judged) observeReasons(observable map[string]pendingObservation) {
	for _, e := range j.outcome.Effects {
		pending, ok := observable[e.ID]
		if !ok {
			continue
		}
		switch {
		case e.Verdict == "":
			// Confirmed never produced: nothing committed, nothing owed.
		case e.Verdict == workflow.EffectUnresolved:
			j.addEffectReason(pending.rec, pending.req.NodeID, "")
		default:
			resolved := pending.rec
			resolved.Ambiguous = false
			j.addEffectReason(resolved, pending.req.NodeID, "")
		}
	}
}

func (j *judged) addEffectReason(rec workflow.EffectRecord, nodeID, prefix string) {
	switch {
	case rec.Ambiguous:
		j.reasons = append(j.reasons, Reason{Code: ReasonEffectAmbiguous, NodeID: nodeID, Ref: prefix + rec.ID})
	case rec.Reversible:
	case rec.Compensation != "":
		j.reasons = append(j.reasons, Reason{Code: ReasonEffectCompensation, NodeID: nodeID, Ref: prefix + rec.ID})
		j.compensation = append(j.compensation, prefix+rec.ID+"="+rec.Compensation)
	case rec.Correction != "":
		// Kept with a forward correction path: the verdict carries the
		// correction for its owner to drive, and the run is refused while
		// the effect stands. No compensation obligation: discharge owns
		// only COMPENSATE verdicts. (No compiled node binds a correction
		// yet; the reversal contracts of WF-REV-006 will.)
		j.reasons = append(j.reasons, Reason{Code: ReasonEffectCorrection, NodeID: nodeID, Ref: prefix + rec.ID})
	default:
		j.reasons = append(j.reasons, Reason{Code: ReasonEffectIrreversible, NodeID: nodeID, Ref: prefix + rec.ID})
	}
}

// judgeChild reports one linked child and lifts its produced effects into the
// parent's facts, so a child's compensation or irreversible effect decides
// the parent exactly as the parent's own would.
func (d decider) judgeChild(ctx context.Context, ex Executor, parent *judged, link runtimestate.ChildLink, depth int) (workflow.CancellableNode, []workflow.EffectRecord, error) {
	child, err := lockInstance(ctx, ex, link.TenantID, link.Child)
	if err != nil {
		return workflow.CancellableNode{}, nil, err
	}
	node := workflow.CancellableNode{Ref: subworkflow.ChildRef{
		Workflow: child.WorkflowID + "/" + child.InstanceID.String(), Version: strconv.FormatUint(uint64(child.WorkflowVersion), 10),
	}}
	ref := "child:" + child.InstanceID.String()
	notCancellable := func() {
		node.State, node.Cancellable = subworkflow.StateRunning, false
		parent.reasons = append(parent.reasons, Reason{Code: ReasonChildNotCancellable, NodeID: link.ParentNodeID, Ref: ref})
	}
	repair := func(state string) {
		node.State = subworkflow.ChildState(state)
		parent.reasons = append(parent.reasons, Reason{Code: ReasonChildRepair, NodeID: link.ParentNodeID, Ref: ref})
	}
	switch child.RuntimeStatus {
	case runtime.InstanceCompleted, runtime.InstanceSuperseded:
		node.State = subworkflow.StateSucceeded
		return node, nil, nil
	case runtime.InstanceCancelled:
		node.State, node.Cancellable = subworkflow.StateRunning, true
		return node, nil, nil
	case runtime.InstanceRepairRequired:
		repair(string(child.RuntimeStatus))
		return node, nil, nil
	case runtime.InstanceQuarantined:
		notCancellable()
		return node, nil, nil
	}
	if prior, found, err := loadCurrentDecision(ctx, ex, child); err != nil {
		return workflow.CancellableNode{}, nil, err
	} else if found {
		// A decision already stands for this child's current state (for
		// example a compensation it is CANCELLING for): report it, act on
		// nothing again.
		switch prior.Decision {
		case workflow.CannotCancel:
			notCancellable()
		case workflow.RepairRequired:
			repair("DECIDED_REPAIR")
		default:
			node.State, node.Cancellable = subworkflow.StateRunning, true
		}
		for _, c := range prior.CompensationRefs {
			parent.compensation = append(parent.compensation, ref+"/"+c)
		}
		var lifted []workflow.EffectRecord
		for _, r := range prior.Reasons {
			if r.Code == ReasonEffectCompensation {
				lifted = append(lifted, workflow.EffectRecord{ID: ref + "/" + r.Ref, Compensation: "decided:" + prior.DecisionID.String()})
				parent.reasons = append(parent.reasons, Reason{Code: r.Code, NodeID: link.ParentNodeID, Ref: ref + "/" + r.Ref})
			}
		}
		return node, lifted, nil
	}
	if depth+1 > maxChildDepth {
		repair("DEPTH_EXCEEDED")
		return node, nil, nil
	}
	plan, err := d.childPlan(ctx, ex, child)
	if err != nil || plan == nil || plan.Digest() != child.CompiledPlanHash {
		repair("PLAN_UNRESOLVED")
		return node, nil, nil
	}
	cj, err := d.judge(ctx, ex, child, plan, depth+1)
	if err != nil {
		return workflow.CancellableNode{}, nil, err
	}
	var lifted []workflow.EffectRecord
	for _, r := range cj.reasons {
		if r.Code == ReasonEffectCompensation || r.Code == ReasonEffectIrreversible {
			rec := workflow.EffectRecord{ID: ref + "/" + r.Ref}
			if r.Code == ReasonEffectCompensation {
				rec.Compensation = compensationFor(cj.compensation, r.Ref)
			}
			lifted = append(lifted, rec)
			parent.addEffectReason(rec, link.ParentNodeID, "")
		}
	}
	switch cj.outcome.Decision {
	case workflow.Cancelled, workflow.CompensationRequired:
		node.State, node.Cancellable = subworkflow.StateRunning, true
		parent.children = append(parent.children, cj)
	case workflow.CannotCancel:
		notCancellable()
	default:
		repair("DECIDED_REPAIR")
	}
	return node, lifted, nil
}

func compensationFor(refs []string, effectID string) string {
	for _, c := range refs {
		if strings.HasPrefix(c, effectID+"=") {
			return strings.TrimPrefix(c, effectID+"=")
		}
	}
	return "unnamed"
}

func (d decider) childPlan(ctx context.Context, ex Executor, child runtime.Instance) (*workflow.CompiledWorkflow, error) {
	if d.req.Plans != nil {
		return d.req.Plans.ResolvePlan(ctx, ex, child)
	}
	if d.req.Plan.Digest() == child.CompiledPlanHash {
		return d.req.Plan, nil
	}
	return nil, fmt.Errorf("workflow cancellation: no plan %s for child %s", child.CompiledPlanHash, child.InstanceID)
}

// effectState reports whether a node execution carries an effect and whether
// that effect is settled (known produced) or ambiguous.
func effectState(n runtime.NodeExecution) (settled, has bool) {
	switch n.Status {
	case runtime.NodeSucceeded:
		return true, true
	case runtime.NodeRunning, runtime.NodeWaiting:
		return false, true
	case runtime.NodeFailed, runtime.NodeCancelled, runtime.NodeOverridden, runtime.NodeRetrying, runtime.NodeSkipped:
		return false, len(n.Refs.EffectRefs) > 0
	default: // READY has not run; COMPENSATED already released its effect.
		return false, false
	}
}

func phaseOf(inst runtime.Instance) string {
	frontier := append([]string(nil), inst.CurrentNodeIDs...)
	sort.Strings(frontier)
	if len(frontier) == 0 {
		return string(inst.RuntimeStatus)
	}
	return string(inst.RuntimeStatus) + "@" + strings.Join(frontier, ",")
}

// apply acts on j's verdict and records it (children first, so a parent is
// never declared cancelled over a live child).
func (d decider) apply(ctx context.Context, ex Executor, j *judged, parentID uuid.UUID) (Outcome, error) {
	before := j.inst
	out := Outcome{
		DecisionID: decisionID(before), Decision: j.outcome.Decision, Evidence: j.outcome,
		Reasons: j.reasons, CompensationRefs: j.compensation,
		StatusBefore: before.RuntimeStatus, VersionBefore: before.InstanceVersion, Instance: before,
	}
	if out.Decision == workflow.Cancelled || out.Decision == workflow.CompensationRequired {
		for _, child := range j.children {
			co, err := d.apply(ctx, ex, child, out.DecisionID)
			if err != nil {
				return Outcome{}, err
			}
			out.Children = append(out.Children, ChildDecision{InstanceID: co.Instance.InstanceID, DecisionID: co.DecisionID, Decision: co.Decision})
		}
	}
	var err error
	switch out.Decision {
	case workflow.Cancelled:
		out.Instance, err = d.cancel(ctx, ex, j)
	case workflow.CompensationRequired:
		out.Instance, err = d.cancelling(ctx, ex, j.inst, j.plan)
	case workflow.RepairRequired:
		out.Instance, err = d.repair(ctx, ex, j)
	}
	if err != nil {
		return Outcome{}, err
	}
	if err := recordDecision(ctx, ex, out, j.plan.Digest(), parentID, d.req); err != nil {
		return Outcome{}, err
	}
	return out, nil
}

func transitionOf(inst runtime.Instance, status runtime.InstanceStatus) runtime.InstanceTransition {
	return runtime.InstanceTransition{
		TenantID: inst.TenantID, InstanceID: inst.InstanceID, ExpectedVersion: inst.InstanceVersion,
		Status: status, CurrentNodeIDs: append([]string(nil), inst.CurrentNodeIDs...),
		VariableRevisionHead: inst.VariableRevisionHead, EffectiveContextRef: inst.EffectiveContextRef,
		LastCheckpointRef: inst.LastCheckpointRef, CompletionDimensions: inst.CompletionDimensions,
	}
}

func (d decider) cancelling(ctx context.Context, ex Executor, inst runtime.Instance, plan *workflow.CompiledWorkflow) (runtime.Instance, error) {
	if inst.RuntimeStatus == runtime.InstanceCancelling {
		return inst, nil
	}
	t := transitionOf(inst, runtime.InstanceCancelling)
	if len(t.CurrentNodeIDs) == 0 {
		// A CREATED instance has no frontier yet; CANCELLING must carry one.
		t.CurrentNodeIDs = []string{plan.StartNodeID}
	}
	return (runtime.Store{}).RecordInstanceState(ctx, ex, t)
}

func (d decider) cancel(ctx context.Context, ex Executor, j *judged) (runtime.Instance, error) {
	cancelling, err := d.cancelling(ctx, ex, j.inst, j.plan)
	if err != nil {
		return runtime.Instance{}, err
	}
	at := d.req.RecordedAt.UTC()
	if err := cancelTimers(ctx, ex, j.inst, at); err != nil {
		return runtime.Instance{}, err
	}
	if err := cancelOpenWorkItems(ctx, ex, j.inst, d.req, at); err != nil {
		return runtime.Instance{}, err
	}
	done := transitionOf(cancelling, runtime.InstanceCancelled)
	done.CurrentNodeIDs = nil
	done.CompletionDimensions = runtime.Dimensions{RequestState: "CANCELLED", ExecutionState: "NOT_PLANNED",
		BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"}
	done.CompletedAt = &at
	return (runtime.Store{}).RecordInstanceState(ctx, ex, done)
}

// WorkItemCancelledReason is the transition reason an open work item of a
// cancelled instance is closed with.
const WorkItemCancelledReason = "workflow.cancellation.cancelled"

// cancelOpenWorkItems closes every human work item the cancelled instance
// still has open, so an approver is never left holding work for a workflow
// that will not advance. Each close is an ordinary governed transition (the
// item's own history keeps who asked and why); an item another caller closed
// first is skipped.
func cancelOpenWorkItems(ctx context.Context, ex Executor, inst runtime.Instance, req Request, at time.Time) error {
	store := workitem.Store{}
	items, err := store.ListForInstance(ctx, ex, inst.TenantID, inst.InstanceID)
	if err != nil {
		return fmt.Errorf("workflow cancellation: read open work items of %s: %w", inst.InstanceID, err)
	}
	actor := req.RequestedBy
	if actor == "" {
		actor = "system:workflow-cancellation"
	}
	for _, item := range items {
		if item.Status.Terminal() {
			continue
		}
		if _, err := store.Cancel(ctx, ex, item.TenantID, item.WorkItemID, item.ItemVersion, workitem.TransitionMeta{
			ActorPrincipalID: actor, Reason: WorkItemCancelledReason, Detail: req.Reason, At: at,
		}); err != nil && workitem.CodeOf(err) != workitem.CodeStaleItem && workitem.CodeOf(err) != workitem.CodeIllegalTransition {
			return fmt.Errorf("workflow cancellation: cancel work item %s: %w", item.WorkItemID, err)
		}
	}
	return nil
}

// cancelTimers settles every timer the instance still has pending as
// cancelled, so a cancelled instance is never woken by a promise it made on
// the way. A timer another caller settled first is skipped (the same rule as
// timer.Scheduler.CancelInstance, which this package cannot import: the timer
// package depends on execute, and execute depends on this package).
func cancelTimers(ctx context.Context, ex Executor, inst runtime.Instance, at time.Time) error {
	store := runtimestate.TimerStore{}
	pending, err := store.PendingForInstance(ctx, ex, inst.TenantID, inst.InstanceID)
	if err != nil {
		return fmt.Errorf("workflow cancellation: read pending timers of %s: %w", inst.InstanceID, err)
	}
	for _, t := range pending {
		err := store.Cancel(ctx, ex, t.TenantID, t.TimerID, t.Version, at)
		switch {
		case err == nil, errors.Is(err, runtimestate.ErrIllegalTransition), errors.Is(err, runtimestate.ErrVersionConflict):
		default:
			return fmt.Errorf("workflow cancellation: cancel timer %s: %w", t.TimerID, err)
		}
	}
	return nil
}

func (d decider) repair(ctx context.Context, ex Executor, j *judged) (runtime.Instance, error) {
	cancelling, err := d.cancelling(ctx, ex, j.inst, j.plan)
	if err != nil {
		return runtime.Instance{}, err
	}
	at := d.req.RecordedAt.UTC()
	repair := transitionOf(cancelling, runtime.InstanceRepairRequired)
	repair.CompletionDimensions = runtime.Dimensions{RequestState: "CANCELLED", ExecutionState: "UNKNOWN",
		BusinessState: "UNKNOWN", ConsistencyState: "REPAIR_REQUIRED", ObligationState: "NOT_APPLICABLE"}
	repair.CompletedAt = &at
	return (runtime.Store{}).RecordInstanceState(ctx, ex, repair)
}
