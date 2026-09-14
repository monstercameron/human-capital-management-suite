package execute

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// WF-RUN-004's driver-facing half, added additively: the ports through which
// the bounded driver creates a durable timer for a TIMER_REQUIRED
// continuation and resumes a WAIT node from a timer that has actually fired.
//
// Both are ports rather than a direct dependency on internal/workflow/timer,
// for the same reason [WorkItemFactory] is one: this package drives a
// workflow, it does not own the timer's storage. A composition root wires the
// adapters. A driver with neither port configured behaves exactly as it did
// before WF-RUN-004 -- a TIMER_REQUIRED continuation is
// [ErrUnsupportedContinuation].

// TimerRequest asks the timer adapter to create the durable timer a
// TIMER_REQUIRED continuation describes.
//
// It carries the pinned plan and the compiled WAIT node rather than a
// pre-computed instant, because the wake condition -- its zone, tzdb release,
// business calendar and reference-update policy -- lives on the node, and
// resolving it is internal/workflow/steps/wait's pure job, not this driver's.
type TimerRequest struct {
	Continuation runtime.ContinuationRecord
	Plan         *workflow.CompiledWorkflow
	Node         workflow.CompiledNode
	CreatedAt    time.Time
	// Proposal is the binding the instance was started (and is resumed)
	// with. A WAIT node whose wake date is declared relative to the
	// proposal ("FROM_WORKFLOW_INPUT:effective_date") is resolved from it,
	// so the promise is derived from durable, digest-bound facts rather than
	// from anything a process remembered.
	Proposal runtime.ProposalBinding
}

// TimerHandle is the durable timer the adapter created (or recognised).
type TimerHandle struct {
	TimerID uuid.UUID
	NodeID  string
	// Key is the timer's durable key: the wake requirement's content digest.
	Key     string
	FiresAt time.Time
	// Replay reports that this promise was already on the table, which is
	// what a replayed advancement produces.
	Replay bool
}

// TimerFactory creates the durable timer for a TIMER_REQUIRED continuation,
// inside the same transaction as the advancement that raised it.
type TimerFactory interface {
	CreateTimer(ctx context.Context, ex runtime.Executor, req TimerRequest) (TimerHandle, error)
}

// FiredTimer is the durable timer row [TimerReader] hands back, as this
// package needs to see it.
type FiredTimer struct {
	TimerID    uuid.UUID
	InstanceID uuid.UUID
	NodeID     string
	Key        string
	State      string
	FiresAt    time.Time
	// Causal is the timer's stored correlation/causation identity plus its
	// optional diagnostic trace link (OBS-013). It is populated from the
	// committed row and is used only to link the resume span; it never
	// governs the advancement, which rests on the drift-checked row alone.
	Causal *runtime.CausalMetadata
}

// TimerReader loads the durable timer a [Driver.ResumeTimer] advances from,
// inside the same transaction as the advancement it feeds. It is
// [WorkItemReader]'s exact counterpart for WAIT nodes, and it exists for the
// same reason (WF-RUN-028): a resumed instance must advance on evidence that
// is actually committed, never on a struct the caller assembled.
type TimerReader interface {
	LoadTimer(ctx context.Context, ex runtime.Executor, tenantID, timerID uuid.UUID) (FiredTimer, error)
}

// TimerStateFired is the durable state a timer must be in before a WAIT node
// may advance from it. It matches migration 00026's own timer_state value.
const TimerStateFired = "FIRED"

// ResumeTimerRequest presents the typed WAIT resolution a fired timer
// produced, plus the identity of the timer it came from.
//
// Like [ResumeRequest], it names the durable row rather than carrying it:
// ResumeTimer loads the timer itself, inside the advancement transaction, and
// refuses [ErrTimerDrift] the instant the stored row disagrees with what this
// request or the pinned plan expects.
type ResumeTimerRequest struct {
	Start                   runtime.StartRequest
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	TimerID                 uuid.UUID
	Outcome                 frontier.NodeOutcome
	Refs                    runtime.GovernanceRefs
	RecordedAt              time.Time
}

// ResumeTimer advances a WAITING WAIT node from the durable timer
// [TimerReader] loads for req.TimerID, then drains any ordinary READY
// successors exactly as Execute does.
//
// It never polls and it never fires the timer: settling a promise is
// internal/workflow/timer's job, done by a caller with its own clock reading
// and its own lease fence. By the time this method runs, the timer row must
// already say FIRED -- that is the evidence the advancement rests on.
func (d *Driver) ResumeTimer(ctx context.Context, req ResumeTimerRequest) (ret0 Result, retErr error) {
	ctx, obsOp := observe.Begin(d.observed(ctx), "workflow.execute.resume_timer", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	selection, err := validateResumeTimerConfig(ctx, req, d.opts.TimerReader)
	if err != nil {
		return Result{}, err
	}
	run := runContext{
		start: req.Start, selection: selection, instanceID: req.InstanceID,
		traceID: d.opts.Instrumentation.TraceID(ctx),
		// OBS-013: the fired timer's identity selects the timer_id
		// attribute on the resume span its stored causal identity links.
		timerID: req.TimerID,
	}
	at := req.RecordedAt.UTC()
	if req.RecordedAt.IsZero() {
		at = d.opts.Clock().UTC()
	}

	advanced, created, evidenceIDs, timers, err := d.advanceOnce(ctx, run, req.ExpectedInstanceVersion, at, 1,
		func(ctx context.Context, ex runtime.Executor) (frontier.NodeOutcome, runtime.GovernanceRefs, *runtime.CausalMetadata, error) {
			row, loadErr := d.opts.TimerReader.LoadTimer(ctx, ex, req.Start.TenantID, req.TimerID)
			if loadErr != nil {
				return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, nil, loadErr
			}
			outcome, refs, driftErr := checkTimerDrift(req, selection, row)
			if driftErr != nil {
				return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, nil, driftErr
			}
			// OBS-013: the drift-checked row's stored causal identity
			// rides out for the resume span link. It never governs: the
			// drift check above already refused any row this request may
			// not advance on, and a nil (pre-causal timer) advances
			// unlinked with identical business behavior.
			return outcome, refs, row.Causal, nil
		})
	if err != nil {
		return Result{}, err
	}

	result := Result{
		Advances:        []runtime.AdvanceReceipt{advanced},
		WorkItems:       created,
		Timers:          timers,
		InstanceVersion: advanced.NewInstanceVersion,
		Frontier:        append([]string(nil), advanced.Frontier...),
		EvidenceIDs:     evidenceIDs,
	}
	if advanced.Complete {
		result.Status = StatusComplete
		return result, nil
	}
	ready, parked := readyAndParked(advanced.Continuations)
	if parked {
		result.Status = StatusParked
		return result, nil
	}
	if len(ready) == 0 {
		return Result{}, fmt.Errorf("%w: resumed instance %s has no READY continuation", ErrNoProgress, req.InstanceID)
	}
	return d.drainReady(ctx, run, result, ready)
}

// validateResumeTimerConfig checks everything ResumeTimer can check before it
// opens a transaction: wiring, request shape and the pinned plan's identity.
// It never touches the timer row -- that load happens inside the advancement
// transaction, in [checkTimerDrift], for the same reason WF-RUN-028 gives for
// work items.
func validateResumeTimerConfig(ctx context.Context, req ResumeTimerRequest, timers TimerReader) (runtime.WorkflowSelection, error) {
	if timers == nil {
		return runtime.WorkflowSelection{}, invalid("resume from a timer has no TimerReader")
	}
	if req.Start.TenantID == uuid.Nil || req.InstanceID == uuid.Nil || req.TimerID == uuid.Nil ||
		req.ExpectedInstanceVersion < 1 {
		return runtime.WorkflowSelection{}, invalid(
			"resume from a timer requires tenant, instance, timer and a positive expected instance version")
	}
	return resolvePinnedPlan(ctx, req.Start, "resume from a timer")
}

// checkTimerDrift compares the durable timer row [TimerReader] loaded against
// the request and the pinned plan, and refuses [ErrTimerDrift] on any
// difference. The node the outcome names is always taken from the stored row.
func checkTimerDrift(
	req ResumeTimerRequest, selection runtime.WorkflowSelection, row FiredTimer,
) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if row.TimerID != req.TimerID {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, timerDrift(
			"loaded timer %s is not the requested %s", row.TimerID, req.TimerID)
	}
	if row.State != TimerStateFired {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, timerDrift(
			"timer %s is %s, not %s; a WAIT node advances on a settled promise, never on a pending one",
			row.TimerID, row.State, TimerStateFired)
	}
	if row.InstanceID != req.InstanceID {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, timerDrift(
			"timer %s is bound to instance %s, not %s", row.TimerID, row.InstanceID, req.InstanceID)
	}
	node, ok := selection.Plan.Node(row.NodeID)
	if !ok || node.Type != workflow.StepWait {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, timerDrift(
			"timer %s names node %s, which is not a WAIT in the pinned plan", row.TimerID, row.NodeID)
	}

	outcome := req.Outcome
	if outcome.NodeID == "" {
		outcome.NodeID = row.NodeID
	}
	if outcome.NodeID != row.NodeID {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, timerDrift(
			"typed outcome names node %s but timer %s woke %s", outcome.NodeID, row.TimerID, row.NodeID)
	}
	if outcome.Await != frontier.AwaitNone {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid(
			"resume from a timer requires a resolution that completes or fails the WAIT node, not one still awaiting")
	}
	if outcome.Outcome == "" && !outcome.Failed {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid(
			"resume from a timer requires a typed outcome for WAIT node %s", row.NodeID)
	}
	return outcome, req.Refs, nil
}
