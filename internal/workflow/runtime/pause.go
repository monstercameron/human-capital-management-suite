package runtime

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Stable refusal codes this file introduces for WF-RUN-008.
const (
	// CodeInstancePaused reports an advancement refused because the instance
	// is PAUSED, or because it is PAUSE_REQUESTED and this boundary is the
	// eligible safe point the pause was waiting for. It is not a failure of
	// the advancement: it is the pause taking effect.
	CodeInstancePaused = "INSTANCE_PAUSED"
	// CodeNotPaused reports a resume-from-pause on an instance that is not
	// PAUSED. Resuming an instance nobody paused is a caller error, not a
	// no-op, because it would otherwise hide a lost pause request.
	CodeNotPaused = "NOT_PAUSED"
)

const pauseReceiptDigestProfile = "hcmnext.workflow.runtime.PauseReceipt/v1"

// PauseRequest is one caller's request to pause a running instance.
//
// There is no scheduler behind it. A pause is recorded as PAUSE_REQUESTED and
// becomes PAUSED at an eligible safe point, and the only thing that ever
// evaluates "are we at a safe point now" is a caller's own next call into
// this package: [RequestPause] itself, [ApplyPause], or the pause gate
// [Advance] runs before it does any work. Nothing here reads a clock, claims
// a lease or wakes an instance up -- WF-RUN-000's prototype ruling admits
// caller-driven Start/Advance/commit fenced by the instance version and
// nothing more, and pause is built inside that same envelope.
type PauseRequest struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	// ExpectedInstanceVersion fences the request the same way every other
	// write in this package is fenced. It is required only when the request
	// actually transitions the instance; a repeat request against an
	// instance already PAUSE_REQUESTED or PAUSED is answered from the stored
	// row without a write, so a caller replaying a pause never has to guess
	// the version its own earlier call produced.
	ExpectedInstanceVersion int64

	// Plan is the exact compiled plan the instance is pinned to. It is
	// required because safe-point eligibility is a compiled fact
	// (WF-COMP-004's atomic regions), never an operator's judgement about
	// where it looks safe to stop.
	Plan *workflow.CompiledWorkflow

	// Reason and RequestedBy are recorded on the receipt so an operator
	// reading the pause can tell a governed intervention from an accident.
	Reason      string
	RequestedBy string
	RequestedAt time.Time
}

func (r PauseRequest) validate() error {
	id := r.InstanceID.String()
	switch {
	case r.TenantID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "", "tenant id must not be the nil UUID")
	case r.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "", "instance id must not be the nil UUID")
	case r.Plan == nil:
		return refuse(CodeInvalidRecord, id, "", "no compiled plan supplied; safe points are read from the plan")
	case r.Reason == "":
		return refuse(CodeInvalidRecord, id, "", "a pause records why it was requested")
	case r.RequestedBy == "":
		return refuse(CodeInvalidRecord, id, "", "a pause records who requested it")
	case r.RequestedAt.IsZero():
		return refuse(CodeInvalidRecord, id, "",
			"requested_at must be supplied; this package never reads a wall clock")
	}
	return nil
}

// ResumeRequest is one caller's request to take a PAUSED instance back to
// RUNNING.
//
// Resuming revalidates the context the instance would resume into, rather
// than assuming the world stood still while it was paused: the pinned plan
// must still be the plan the instance carries, and every required context the
// frontier's nodes declare must be presented again, resolved now. A resume
// that could not name today's LegalContext is exactly the resume that must
// not happen.
type ResumeRequest struct {
	TenantID                uuid.UUID
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	Plan                    *workflow.CompiledWorkflow

	// ResolvedContext supplies, keyed by [workflow.ContextRequirement.Kind],
	// the reference proving one required-context read was resolved at resume
	// time. It is the same shape [StartRequest.ResolvedContext] carries, and
	// it is checked against every node on the frontier, not only the start.
	ResolvedContext map[string]string

	Reason    string
	ResumedBy string
	ResumedAt time.Time
}

func (r ResumeRequest) validate() error {
	id := r.InstanceID.String()
	switch {
	case r.TenantID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "", "tenant id must not be the nil UUID")
	case r.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRecord, id, "", "instance id must not be the nil UUID")
	case r.ExpectedInstanceVersion < 1:
		return refuse(CodeInvalidRecord, id, "", "expected instance version must be at least 1")
	case r.Plan == nil:
		return refuse(CodeInvalidRecord, id, "", "no compiled plan supplied")
	case r.ResumedBy == "":
		return refuse(CodeInvalidRecord, id, "", "a resume records who resumed it")
	case r.ResumedAt.IsZero():
		return refuse(CodeInvalidRecord, id, "",
			"resumed_at must be supplied; this package never reads a wall clock")
	}
	return nil
}

// PauseReceipt is everything a pause, an apply or a resume produced.
type PauseReceipt struct {
	TenantID   uuid.UUID
	InstanceID uuid.UUID
	// Status is the instance's status after the call: PAUSE_REQUESTED when
	// the pause is recorded but the instance is not at a safe point yet,
	// PAUSED when it took effect, RUNNING after a resume.
	Status          InstanceStatus
	InstanceVersion int64
	Frontier        []string

	// Eligible reports whether the instance was at a safe point when this
	// call evaluated it, and BlockingNodeID names the frontier node that was
	// not, when it was not.
	Eligible       bool
	BlockingNodeID string

	Reason     string
	Actor      string
	RecordedAt time.Time
	SafePoints []string
	// Replay reports that this receipt describes a state the call found
	// rather than one it wrote: a second pause request against an already
	// paused instance, or an apply that had nothing to apply.
	Replay bool

	digest string
}

// Digest is the receipt's content identity.
func (r PauseReceipt) Digest() string { return r.digest }

type pauseReceiptIdentity struct {
	TenantID        string
	InstanceID      string
	Status          string
	InstanceVersion int64
	Frontier        []string
	Eligible        bool
	BlockingNodeID  string
	Reason          string
	Actor           string
}

func newPauseReceipt(inst Instance, eligible bool, blocking, reason, actor string, at time.Time, replay bool, safePoints []string) PauseReceipt {
	rec := PauseReceipt{
		TenantID:        inst.TenantID,
		InstanceID:      inst.InstanceID,
		Status:          inst.RuntimeStatus,
		InstanceVersion: inst.InstanceVersion,
		Frontier:        append([]string(nil), inst.CurrentNodeIDs...),
		Eligible:        eligible,
		BlockingNodeID:  blocking,
		Reason:          reason,
		Actor:           actor,
		RecordedAt:      at,
		SafePoints:      append([]string(nil), safePoints...),
		Replay:          replay,
	}
	rec.digest = canonicalDigest(pauseReceiptDigestProfile, pauseReceiptIdentity{
		TenantID:        rec.TenantID.String(),
		InstanceID:      rec.InstanceID.String(),
		Status:          string(rec.Status),
		InstanceVersion: rec.InstanceVersion,
		Frontier:        rec.Frontier,
		Eligible:        rec.Eligible,
		BlockingNodeID:  rec.BlockingNodeID,
		Reason:          rec.Reason,
		Actor:           rec.Actor,
	})
	return rec
}

// SafePointEligibility reports whether an instance sitting at its current
// frontier may be paused, cancelled or migrated right now, and names the node
// that forbids it when it may not.
//
// Two things make a boundary unsafe, and only two:
//
//   - A node execution is RUNNING. The caller's own step handler is inside a
//     node; pausing there would claim a clean stop in the middle of one.
//   - A frontier node is inside a compiled atomic region (WF-COMP-004): the
//     instance holds an external effect no OBSERVE has confirmed yet, and an
//     intervention there abandons it.
//
// Everything else is a safe point. That is deliberately the compiled
// [workflow.CompiledWorkflow.InterventionEligible] fact plus one durable
// runtime fact, and nothing else -- no operator judgement, no heuristics
// about node type.
func SafePointEligibility(
	ctx context.Context, ex Executor, inst Instance, plan *workflow.CompiledWorkflow,
) (bool, string, error) {
	rows, err := (Store{}).LoadNodeExecutions(ctx, ex, inst.TenantID, inst.InstanceID)
	if err != nil {
		return false, "", err
	}
	latest := make(map[string]NodeExecution, len(rows))
	for _, r := range rows {
		if cur, ok := latest[r.NodeID]; !ok || r.Attempt > cur.Attempt {
			latest[r.NodeID] = r
		}
	}
	ids := make([]string, 0, len(latest))
	for id := range latest {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if latest[id].Status == NodeRunning {
			return false, id, nil
		}
	}

	frontier := append([]string(nil), inst.CurrentNodeIDs...)
	sort.Strings(frontier)
	for _, id := range frontier {
		if !plan.InterventionEligible(id) {
			return false, id, nil
		}
	}
	return true, "", nil
}

// planSafePoints lists the plan's compiled safe points, sorted. It is carried
// on every [PauseReceipt] so an operator reading a PAUSE_REQUESTED can see
// where the pause will be able to take effect without re-deriving it.
func planSafePoints(plan *workflow.CompiledWorkflow) []string {
	var out []string
	for _, n := range plan.Nodes {
		if n.SafePoint {
			out = append(out, n.ID)
		}
	}
	sort.Strings(out)
	return out
}

// RequestPause records a pause request against one instance and applies it
// immediately when the instance is already at a safe point.
//
// It is one atomic unit of work through tx, which the caller began and will
// commit. A request against an instance that is already PAUSE_REQUESTED
// re-evaluates eligibility (and may therefore complete the pause); a request
// against one already PAUSED is answered from the stored row and writes
// nothing. A terminal instance is [CodeIllegalTransition]: history is not
// pausable.
func RequestPause(ctx context.Context, tx Executor, req PauseRequest) (ret0 PauseReceipt, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.request_pause", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := req.validate(); err != nil {
		return PauseReceipt{}, err
	}
	store := Store{}
	inst, err := store.LoadInstance(ctx, tx, req.TenantID, req.InstanceID)
	if err != nil {
		return PauseReceipt{}, err
	}
	if err := checkPinnedPlan(inst, req.Plan); err != nil {
		return PauseReceipt{}, err
	}
	if inst.RuntimeStatus.Terminal() {
		return PauseReceipt{}, refuse(CodeIllegalTransition, req.InstanceID.String(), "",
			"instance is %s; a finished instance is not pausable", string(inst.RuntimeStatus))
	}
	if inst.RuntimeStatus == InstancePaused {
		return newPauseReceipt(inst, true, "", req.Reason, req.RequestedBy, req.RequestedAt, true,
			planSafePoints(req.Plan)), nil
	}

	if inst.RuntimeStatus != InstancePauseRequested {
		if req.ExpectedInstanceVersion < 1 {
			return PauseReceipt{}, refuse(CodeInvalidRecord, req.InstanceID.String(), "",
				"expected instance version must be at least 1 to record a new pause request")
		}
		if inst.InstanceVersion != req.ExpectedInstanceVersion {
			return PauseReceipt{}, staleError(req.InstanceID, "", req.ExpectedInstanceVersion, inst.InstanceVersion)
		}
		inst, err = recordInstanceStatus(ctx, tx, store, inst, InstancePauseRequested)
		if err != nil {
			return PauseReceipt{}, err
		}
	}

	return applyPauseTo(ctx, tx, store, inst, req.Plan, req.Reason, req.RequestedBy, req.RequestedAt)
}

// ApplyPause is the caller-driven "are we at a safe point yet" step: it moves
// a PAUSE_REQUESTED instance to PAUSED when the current boundary is eligible
// and leaves it alone when it is not.
//
// It exists as its own entry point because nothing in this runtime wakes an
// instance up. A caller that requested a pause while an atomic region was in
// flight finishes that region with ordinary [Advance] calls and then calls
// this; a caller that would rather not think about it calls [RequestPause]
// again, which does the same thing.
func ApplyPause(ctx context.Context, tx Executor, req PauseRequest) (ret0 PauseReceipt, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.apply_pause", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := req.validate(); err != nil {
		return PauseReceipt{}, err
	}
	store := Store{}
	inst, err := store.LoadInstance(ctx, tx, req.TenantID, req.InstanceID)
	if err != nil {
		return PauseReceipt{}, err
	}
	if err := checkPinnedPlan(inst, req.Plan); err != nil {
		return PauseReceipt{}, err
	}
	if inst.RuntimeStatus == InstancePaused {
		return newPauseReceipt(inst, true, "", req.Reason, req.RequestedBy, req.RequestedAt, true,
			planSafePoints(req.Plan)), nil
	}
	if inst.RuntimeStatus != InstancePauseRequested {
		return newPauseReceipt(inst, false, "", req.Reason, req.RequestedBy, req.RequestedAt, true,
			planSafePoints(req.Plan)), nil
	}
	return applyPauseTo(ctx, tx, store, inst, req.Plan, req.Reason, req.RequestedBy, req.RequestedAt)
}

// applyPauseTo evaluates eligibility for a PAUSE_REQUESTED instance and
// records PAUSED when the boundary is safe.
func applyPauseTo(
	ctx context.Context, tx Executor, store Store, inst Instance,
	plan *workflow.CompiledWorkflow, reason, actor string, at time.Time,
) (PauseReceipt, error) {
	eligible, blocking, err := SafePointEligibility(ctx, tx, inst, plan)
	if err != nil {
		return PauseReceipt{}, err
	}
	if !eligible {
		return newPauseReceipt(inst, false, blocking, reason, actor, at, false, planSafePoints(plan)), nil
	}
	paused, err := recordInstanceStatus(ctx, tx, store, inst, InstancePaused)
	if err != nil {
		return PauseReceipt{}, err
	}
	return newPauseReceipt(paused, true, "", reason, actor, at, false, planSafePoints(plan)), nil
}

// ResumeFromPause takes a PAUSED instance back to RUNNING after revalidating
// the context it is resuming into.
func ResumeFromPause(ctx context.Context, tx Executor, req ResumeRequest) (ret0 PauseReceipt, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.resume_from_pause", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := req.validate(); err != nil {
		return PauseReceipt{}, err
	}
	store := Store{}
	inst, err := store.LoadInstance(ctx, tx, req.TenantID, req.InstanceID)
	if err != nil {
		return PauseReceipt{}, err
	}
	if err := checkPinnedPlan(inst, req.Plan); err != nil {
		return PauseReceipt{}, err
	}
	if inst.RuntimeStatus != InstancePaused {
		return PauseReceipt{}, refuse(CodeNotPaused, req.InstanceID.String(), "",
			"instance is %s, not PAUSED; there is nothing to resume", string(inst.RuntimeStatus))
	}
	if inst.InstanceVersion != req.ExpectedInstanceVersion {
		return PauseReceipt{}, staleError(req.InstanceID, "", req.ExpectedInstanceVersion, inst.InstanceVersion)
	}
	// Revalidate the context every node on the frontier declares it needs.
	// The instance has been sitting still; the world has not.
	for _, id := range inst.CurrentNodeIDs {
		node, ok := req.Plan.Node(id)
		if !ok {
			return PauseReceipt{}, refuse(CodeInvalidRecord, req.InstanceID.String(), id,
				"frontier names a node the pinned plan does not declare")
		}
		if err := checkResolvedContext(node, req.ResolvedContext); err != nil {
			return PauseReceipt{}, err
		}
	}
	running, err := recordInstanceStatus(ctx, tx, store, inst, InstanceRunning)
	if err != nil {
		return PauseReceipt{}, err
	}
	return newPauseReceipt(running, true, "", req.Reason, req.ResumedBy, req.ResumedAt, false,
		planSafePoints(req.Plan)), nil
}

// recordInstanceStatus writes one status change, carrying every other field
// of the instance forward unchanged. A pause changes the instance's status
// and nothing else: not its frontier, not its variables, not its dimensions.
func recordInstanceStatus(
	ctx context.Context, tx Executor, store Store, inst Instance, status InstanceStatus,
) (Instance, error) {
	return store.RecordInstanceState(ctx, tx, InstanceTransition{
		TenantID:             inst.TenantID,
		InstanceID:           inst.InstanceID,
		ExpectedVersion:      inst.InstanceVersion,
		Status:               status,
		CurrentNodeIDs:       append([]string(nil), inst.CurrentNodeIDs...),
		VariableRevisionHead: inst.VariableRevisionHead,
		EffectiveContextRef:  inst.EffectiveContextRef,
		LastCheckpointRef:    inst.LastCheckpointRef,
		CompletionDimensions: inst.CompletionDimensions,
	})
}

// checkPinnedPlan refuses a call presenting a plan the instance is not pinned
// to. Safe points are compiled facts of one exact plan, so reading them out
// of a different one would be reading somebody else's map.
func checkPinnedPlan(inst Instance, plan *workflow.CompiledWorkflow) error {
	if inst.CompiledPlanHash != plan.Digest() {
		return refuse(CodeAdvancePlanMismatch, inst.InstanceID.String(), "",
			"instance pins compiled plan %s; this call presented a plan digesting to %s",
			inst.CompiledPlanHash, plan.Digest())
	}
	return nil
}

// checkPauseGate is [Advance]'s pause gate. It refuses an advancement that a
// pause forbids, and it never writes: a PAUSE_REQUESTED instance standing at
// an eligible safe point is told to stop here, and the transition to PAUSED
// is made by the caller's own [ApplyPause] (or [RequestPause]) call, in a
// transaction that call owns.
//
// A PAUSE_REQUESTED instance that is *not* at a safe point advances normally:
// that is the whole point of the two-step status. The atomic region it is
// inside has to finish before there is anywhere clean to stop.
func checkPauseGate(ctx context.Context, ex Executor, inst Instance, plan *workflow.CompiledWorkflow) error {
	switch inst.RuntimeStatus {
	case InstancePaused:
		return refuse(CodeInstancePaused, inst.InstanceID.String(), "",
			"instance is PAUSED; resume it before advancing")
	case InstancePauseRequested:
		eligible, _, err := SafePointEligibility(ctx, ex, inst, plan)
		if err != nil {
			return err
		}
		if eligible {
			return refuse(CodeInstancePaused, inst.InstanceID.String(), "",
				"a pause was requested and this boundary is a safe point; apply the pause instead of advancing")
		}
		return nil
	default:
		return nil
	}
}
