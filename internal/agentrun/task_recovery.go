package agentrun

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrStepPaused = errors.New("agent step paused before execution")

// StepPauseError is permitted only before an owner effect starts. It allows
// resource admission to park the checkpoint for an explicit user resumption.
type StepPauseError struct {
	Reason string
	Cause  error
}

func (e *StepPauseError) Error() string { return "agent step paused: " + e.Reason }
func (e *StepPauseError) Unwrap() error { return errors.Join(ErrStepPaused, e.Cause) }

func (e *StepPauseError) valid() bool {
	if e == nil || len(e.Reason) == 0 || len(e.Reason) > 64 || strings.TrimSpace(e.Reason) != e.Reason {
		return false
	}
	for _, ch := range e.Reason {
		if !(ch >= 'A' && ch <= 'Z' || ch == '_' || ch >= '0' && ch <= '9') {
			return false
		}
	}
	return true
}

// expireBeforeWork fences work even when the periodic sweeper has not run.
func (r *Runtime) expireBeforeWork(ctx context.Context, task AgentTask, now time.Time) (AgentTask, error) {
	if task.State.terminal() || task.ExpiresAt.After(now) {
		return task, nil
	}
	prior := task.Version
	task.State, task.Wake, task.PausedWake, task.LastWake = StateExpired, nil, nil, nil
	task.WorkerLease, task.ModelSession, task.PausedState = "", "", ""
	task.FailureCode, task.FailureDetail = "TASK_EXPIRED", "maximum task lifetime elapsed"
	task.Version++
	task.UpdatedAt = now.UTC()
	if err := r.store.Save(ctx, task, prior); err != nil {
		return AgentTask{}, err
	}
	return task, ErrTerminal
}

// EffectObservation is an owning capability's durable receipt, never a model
// inference. Applied receipts need a retained reference and digest.
type EffectObservation struct {
	Status EffectStatus
	Result StepResult
}

type EffectStatus string

const (
	EffectApplied    EffectStatus = "APPLIED"
	EffectNotApplied EffectStatus = "NOT_APPLIED"
)

// AuthorityPauseReason is a stable stop reason owned by current authority.
type AuthorityPauseReason string

const (
	PauseUserInactive   AuthorityPauseReason = "AUTHORITY_REVOKED_USER_DEACTIVATED"
	PauseGrantRevoked   AuthorityPauseReason = "AUTHORITY_REVOKED_GRANT"
	PauseGrantExpired   AuthorityPauseReason = "AUTHORITY_EXPIRED_GRANT"
	PauseTenantDisabled AuthorityPauseReason = "AUTHORITY_REVOKED_TENANT_DISABLED"
)

// PauseAuthority stops a task and voids every approved but unsubmitted step.
// Approval outcomes and state change share the same compare-and-swap commit.
func (r *Runtime) PauseAuthority(ctx context.Context, id string, expected uint64, reason AuthorityPauseReason, now time.Time) (AgentTask, error) {
	if now.IsZero() || (reason != PauseUserInactive && reason != PauseGrantRevoked && reason != PauseGrantExpired && reason != PauseTenantDisabled) {
		return AgentTask{}, fmt.Errorf("%w: authority stop reason and time required", ErrInvalid)
	}
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Version != expected {
		return AgentTask{}, ErrConflict
	}
	if task.State.terminal() {
		return task, ErrTerminal
	}
	if task.State != StatePaused {
		task.PausedState, task.PausedWake = task.State, cloneWake(task.Wake)
	}
	var events []TaskEvent
	for i := range task.Plan.Steps {
		step := &task.Plan.Steps[i]
		if step.State == StepCompleted || step.Tier < TierSubmitGoverned {
			continue
		}
		if step.ApprovalDigest != "" {
			events = append(events, TaskEvent{TaskID: task.ID, Type: TaskEventApprovalOutcome, PlanRevision: task.Plan.Revision, PlanDigest: task.Plan.Digest, StepID: step.ID, StepType: step.Type, Tier: step.Tier, ApprovalDigest: step.ApprovalDigest, Outcome: "VOIDED_AUTHORITY", OccurredAt: now.UTC()})
		}
		step.Approved, step.ApprovalDigest = false, ""
		if step.State == StepAwaitingApproval {
			step.State = StepPending
			step.StartedAt, step.FinishedAt = time.Time{}, time.Time{}
			task.PausedState, task.PausedWake = StateRunning, nil
		}
	}
	task.State, task.Wake, task.WorkerLease, task.ModelSession = StatePaused, nil, "", ""
	task.FailureCode, task.FailureDetail = string(reason), "current delegated authority cannot resume this task"
	task.Version++
	task.UpdatedAt = now.UTC()
	if err := saveTaskWithEvents(ctx, r.store, task, expected, events...); err != nil {
		return AgentTask{}, err
	}
	return task, nil
}

// EffectReconciler queries the system that owns a potentially applied effect.
// A transport cannot substitute agent text for this owner observation.
type EffectReconciler interface {
	Reconcile(context.Context, AgentTask, PlanStep) (EffectObservation, error)
}

// ReconcileEffect resolves an abandoned externally visible step without
// invoking it again. Proven absence permits a new admitted attempt; proven
// application completes the checkpoint from the owner's receipt.
func (r *Runtime) ReconcileEffect(ctx context.Context, id string, expected uint64, owner EffectReconciler, now time.Time) (AgentTask, error) {
	if owner == nil || now.IsZero() {
		return AgentTask{}, fmt.Errorf("%w: owner reconciler and time are required", ErrInvalid)
	}
	task, err := r.store.Get(ctx, id)
	if err != nil {
		return AgentTask{}, err
	}
	if task.Version != expected {
		return AgentTask{}, ErrConflict
	}
	if task.State != StatePaused || task.CurrentStep >= len(task.Plan.Steps) || (task.FailureCode != "AMBIGUOUS_EFFECT" && task.FailureCode != string(PauseUserInactive) && task.FailureCode != string(PauseGrantRevoked) && task.FailureCode != string(PauseGrantExpired) && task.FailureCode != string(PauseTenantDisabled)) {
		return task, ErrStepNotReady
	}
	step := task.Plan.Steps[task.CurrentStep]
	if step.State != StepRunning || step.Tier < TierCommunicate {
		return task, ErrStepNotReady
	}
	observation, err := owner.Reconcile(ctx, cloneTask(task), step)
	if err != nil {
		return task, err
	}
	if observation.Status != EffectApplied && observation.Status != EffectNotApplied {
		return task, ErrReconciliationRequired
	}
	if observation.Status == EffectApplied && (observation.Result.Ref == "" || observation.Result.Digest == "") {
		return task, fmt.Errorf("%w: applied effect requires owner receipt", ErrInvalid)
	}
	if observation.Status == EffectApplied {
		// Keep the task paused while recording the receipt. Resumption remains
		// explicit and gets a current authority check at the platform boundary.
		completed, err := r.finishStep(ctx, id, expected, now, step.ID, observation.Result, nil, true, nil)
		if err != nil {
			return completed, err
		}
		return completed, nil
	}
	task.Plan.Steps[task.CurrentStep].State = StepPending
	task.Plan.Steps[task.CurrentStep].StartedAt, task.Plan.Steps[task.CurrentStep].FinishedAt = time.Time{}, time.Time{}
	task.FailureCode, task.FailureDetail = "", ""
	task.Version++
	task.UpdatedAt = now.UTC()
	if err := r.store.Save(ctx, task, expected); err != nil {
		return AgentTask{}, err
	}
	return task, nil
}
