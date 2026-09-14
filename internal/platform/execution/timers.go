package execution

import (
	"context"
	"fmt"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepswait "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// The production adapter between internal/workflow/execute's TIMER_REQUIRED
// port and internal/workflow/timer's durable scheduler.
//
// It lives here, and not in either of those packages, because neither should
// depend on the other: execute drives a workflow and states what it needs as a
// port, timer owns the promise's storage, and a composition root is what wires
// one to the other. Until now the only implementation was a fixture in
// test/workflow, which meant no shipped binary could park an instance on a
// durable timer at all.
//
// The counterpart TimerReader -- the port [execute.Driver.ResumeTimer] loads a
// fired timer through -- is deliberately absent from this package, and the
// reason is placement rather than oversight. Its interface method spells
// google/uuid.UUID in its own signature, so any type implementing it must
// import that module directly, and
// definitions/architecture/dependency-roles.yaml does not list
// internal/platform among the roots allowed to. A production reader therefore
// belongs to a root that is listed (internal/workflow, which already owns both
// halves of the conversation, is the natural one); widening the allowlist to
// put it here would be fixing a placement problem by moving the fence.
// This factory is unaffected: CreateTimer's signature names no identifier
// type, so the adapter that actually needs one is the only half that moves.

// TimerFactoryConfig is what [NewTimerFactory] needs.
type TimerFactoryConfig struct {
	// Scheduler is the durable timer owner. Its zero value is usable;
	// setting Attempts makes a fired timer resolve the node attempt it wakes
	// from durable node executions rather than settling into attempt 1.
	Scheduler timer.Scheduler

	// Dataset is the tzdb and business-calendar release the wake condition is
	// resolved against. It is supplied by the composition root, never read
	// from the process environment: the whole point of the WAIT node's pinned
	// reference-update policy is that the release is a declared input.
	Dataset values.DatasetVersions

	// EffectiveDates carries the immutable proposal effective date observed by
	// the execute step runner. The workflow plan deliberately names this as a
	// workflow-input binding; the execute port does not carry the full proposal
	// on TimerRequest, so the composition shares this short-lived identity map
	// between the two ports.
	EffectiveDates *sync.Map
}

// TimerFactory creates the durable timer a TIMER_REQUIRED continuation
// describes.
type TimerFactory struct {
	scheduler      timer.Scheduler
	dataset        values.DatasetVersions
	effectiveDates *sync.Map
}

var _ execute.TimerFactory = TimerFactory{}

// NewTimerFactory validates the wiring and returns the factory an
// execute.Options may carry as Timers.
//
// The dataset is validated here rather than at the first WAIT node, so a cell
// configured against no tzdb release fails at composition instead of when the
// first instance happens to reach a wait.
func NewTimerFactory(cfg TimerFactoryConfig) (TimerFactory, error) {
	if err := cfg.Dataset.Validate(); err != nil {
		return TimerFactory{}, fmt.Errorf("platform execution: timer factory dataset: %w", err)
	}
	return TimerFactory{scheduler: cfg.Scheduler, dataset: cfg.Dataset, effectiveDates: cfg.EffectiveDates}, nil
}

// CreateTimer resolves the compiled WAIT node's wake requirement purely
// (internal/workflow/steps/wait), then writes the durable promise through
// internal/workflow/timer, inside the advancement's own transaction.
//
// The requirement's content digest becomes the timer's durable key, which is
// what makes a replayed advancement address the promise it already made: the
// second call reports Replay and writes nothing.
func (f TimerFactory) CreateTimer(ctx context.Context, ex runtime.Executor, req execute.TimerRequest) (ret0 execute.TimerHandle, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execution.create_timer", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if req.Plan == nil {
		return execute.TimerHandle{}, fmt.Errorf("platform execution: create timer: continuation carries no pinned plan")
	}
	bound, err := f.bindWaitNode(req)
	if err != nil {
		return execute.TimerHandle{}, err
	}
	node, err := stepswait.FromCompiled(&bound)
	if err != nil {
		return execute.TimerHandle{}, fmt.Errorf("platform execution: bind the compiled WAIT node: %w", err)
	}
	node.WorkflowID, node.WorkflowVersion = req.Plan.WorkflowID, req.Plan.Version

	requirement, err := stepswait.ComputeTimerRequirement(node, f.dataset)
	if err != nil {
		return execute.TimerHandle{}, fmt.Errorf("platform execution: compute the wake requirement: %w", err)
	}
	// The WAIT node's activation was recorded by the advancement that raised
	// this continuation; a re-entered WAIT (a re-approval loop) is a later
	// attempt and must be promised under its own timer identity and key.
	scheduled, err := f.scheduler.Schedule(ctx, ex, timer.Request{
		TenantID:   req.Continuation.TenantID,
		InstanceID: req.Continuation.InstanceID,
		NodeID:     req.Continuation.TargetNodeID,
		Kind:       timer.KindUntil,

		Requirement: requirement,
		CreatedAt:   req.CreatedAt,
		Attempt:     waitAttempt(req.Continuation.TargetAttempt),
		Causal:      req.Continuation.Causal,
	})
	if err != nil {
		return execute.TimerHandle{}, err
	}
	return execute.TimerHandle{
		TimerID: scheduled.Timer.TimerID,
		NodeID:  scheduled.Timer.NodeID,
		Key:     scheduled.Timer.Key,
		FiresAt: scheduled.Timer.FiresAt,
		Replay:  scheduled.Replay,
	}, nil
}

// WakeFromProposalEffectiveDate is the wake-date placeholder a compiled WAIT
// node carries when its date is the proposal's own effective date rather than
// a literal in the definition (internal/workflow/promotionexec declares its
// effective-date WAIT this way).
const WakeFromProposalEffectiveDate = "FROM_WORKFLOW_INPUT:effective_date"

// bindWaitNode resolves a WAIT node's placeholder wake date before the wait
// step reads it. The proposal binding the driver presents is the source: it
// is what Start bound the instance to and what every Resume re-presents, so
// the promise is a function of digest-bound facts. The composition-local
// effective-date map remains only as a fallback for a caller that supplied
// no proposal; a placeholder neither source can fill is refused with the
// node named, never silently parsed.
func (f TimerFactory) bindWaitNode(req execute.TimerRequest) (workflow.CompiledNode, error) {
	node := req.Node
	if node.Wait == nil || node.Wait.WakeLocalDate != WakeFromProposalEffectiveDate {
		return node, nil
	}
	if date, ok := req.Proposal.Revision.EffectiveTime.StartDate(); ok {
		wait := *node.Wait
		wait.WakeLocalDate = date.String()
		node.Wait = &wait
		return node, nil
	}
	// internal/intent/app mints RequestedEffectiveAt as the requested date's
	// midnight in UTC (the date is the whole of what the request carried), so
	// an INSTANT interval yields that UTC calendar date; the wait step then
	// resolves the local start of that date in the node's declared zone.
	if start, ok := req.Proposal.Revision.EffectiveTime.StartInstant(); ok {
		at := start.Time().UTC()
		date, dateErr := values.NewLocalDate(at.Year(), at.Month(), at.Day())
		if dateErr != nil {
			return workflow.CompiledNode{}, fmt.Errorf("platform execution: WAIT node %s: effective instant is not a calendar date: %w", node.ID, dateErr)
		}
		wait := *node.Wait
		wait.WakeLocalDate = date.String()
		node.Wait = &wait
		return node, nil
	}
	if f.effectiveDates != nil {
		if stored, ok := f.effectiveDates.Load(req.Continuation.InstanceID.String()); ok {
			if date, valid := stored.(values.LocalDate); valid {
				wait := *node.Wait
				wait.WakeLocalDate = date.String()
				node.Wait = &wait
				return node, nil
			}
		}
	}
	return workflow.CompiledNode{}, fmt.Errorf("platform execution: WAIT node %s wakes on the proposal effective date, but the proposal binding carries no effective interval", node.ID)
}

// waitAttempt normalises the continuation's target activation: a zero value
// (a continuation recorded before activations were tracked, or a test double)
// is the first activation.
func waitAttempt(targetAttempt int) int {
	if targetAttempt < 1 {
		return 1
	}
	return targetAttempt
}
