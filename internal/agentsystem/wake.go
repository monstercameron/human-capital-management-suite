package agentsystem

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	// tickMaxDrive bounds how many runnable tasks one tick drives, and
	// tickMaxSteps how many steps it runs for one task, so a large backlog
	// is spread over ticks instead of holding the scheduler.
	tickMaxDrive = 64
	tickMaxSteps = 32
	// timerStaleGrace is how long past its due time a timer may go undelivered
	// before the stale-task sweeper resolves it as a lost wake.
	timerStaleGrace = 24 * time.Hour
	// A worker is bounded to this lease interval. Recovery never retries an
	// externally visible effect without an owning capability receipt.
	stepLeaseTimeout = 5 * time.Minute
)

// TickTenant is one scheduler tick for one tenant. It is the
// internal/intent/app.AgentWaker port and runs from the workflow scheduler's
// recovery seam, so there is no second scheduler. In order it delivers due
// TIMER and POLLING wakes, settles stale tasks (lost wakes, expiry) to typed
// outcomes, and drives RUNNING tasks that hold no worker lease. It returns the
// number of tasks moved.
//
// Every replica may tick at once: delivered wakes carry deterministic event
// ids that the store's dedupe inbox collapses, and every task transition is
// version-checked, so a concurrent tick moves a task at most once.
func (p *Platform) TickTenant(ctx context.Context, tenant string, now time.Time) (int, error) {
	if p == nil {
		return 0, ErrNotConfigured
	}
	if now.IsZero() {
		return 0, fmt.Errorf("%w: tick time is required", ErrInvalid)
	}
	runner, err := p.ForTenant(ctx, values.TenantId(tenant))
	if err != nil {
		return 0, err
	}
	if gate := p.cfg.WakeGate; gate != nil && !gate(ctx, tenant) {
		tasks, err := runner.tasks.List(ctx)
		if err != nil {
			return 0, err
		}
		moved := 0
		var errs []error
		for _, task := range tasks {
			if task.State != agentrun.StateRunning && task.State != agentrun.StateWaiting && task.State != agentrun.StateAwaitingApproval {
				continue
			}
			paused, err := runner.Runtime.PauseAuthority(ctx, task.ID, task.Version, agentrun.PauseTenantDisabled, now)
			if err == nil && paused.Version != task.Version {
				moved++
			}
			if err != nil && !errors.Is(err, agentrun.ErrConflict) && !errors.Is(err, agentrun.ErrTerminal) {
				errs = append(errs, err)
			}
		}
		return moved, errors.Join(errs...)
	}
	return runner.tick(ctx, now)
}

func (r *Runner) tick(ctx context.Context, now time.Time) (int, error) {
	moved := 0
	var errs []error
	tasks, err := r.tasks.List(ctx)
	if err != nil {
		return 0, err
	}
	for _, task := range tasks {
		// Lease age uses the worker clock that claimed the step. The scheduler's
		// due-time argument can differ when replaying historical timer firings.
		if task.State == agentrun.StateRunning && task.WorkerLease != "" && !task.UpdatedAt.Add(stepLeaseTimeout).After(r.p.cfg.Clock()) {
			recovered, recoverErr := r.Runtime.RecoverStale(ctx, task.ID, task.Version, now)
			if recoverErr != nil && !errors.Is(recoverErr, agentrun.ErrConflict) && !errors.Is(recoverErr, agentrun.ErrTerminal) {
				errs = append(errs, fmt.Errorf("recover %s: %w", task.ID, recoverErr))
			}
			if recovered.ID != "" && recovered.Version != task.Version {
				moved++
			}
			continue
		}
		if !task.ExpiresAt.After(now) {
			continue
		}
		event, due := dueWake(task, now)
		if !due {
			continue
		}
		res, err := r.deliver(ctx, task.ID, event)
		switch {
		case err == nil && res.Accepted:
			moved++
		case err == nil || errors.Is(err, ErrDenied) || errors.Is(err, agentrun.ErrConflict):
			if res.Task.ID != "" && res.Task.Version != task.Version {
				moved++
			}
		default:
			errs = append(errs, fmt.Errorf("deliver wake to %s: %w", task.ID, err))
		}
	}
	settled, err := r.Runtime.SweepStaleTasks(ctx, now)
	if err != nil {
		errs = append(errs, err)
	}
	moved += len(settled)

	tasks, err = r.tasks.List(ctx)
	if err != nil {
		return moved, errors.Join(append(errs, err)...)
	}
	driven := 0
	for _, task := range tasks {
		if driven >= tickMaxDrive {
			break
		}
		if !runnable(task) || !task.ExpiresAt.After(now) {
			continue
		}
		driven++
		next, err := r.drive(ctx, task.ID, ModeOnBehalfOf, tickMaxSteps)
		if err != nil {
			errs = append(errs, fmt.Errorf("drive %s: %w", task.ID, err))
		}
		if next.Version != task.Version {
			moved++
		}
	}
	return moved, errors.Join(errs...)
}

// runnable reports whether a task is RUNNING with no worker holding it and a
// step ready to run (or a resumed wait step waiting to be completed).
func runnable(task agentrun.AgentTask) bool {
	if task.State != agentrun.StateRunning || !task.Plan.Confirmed || task.WorkerLease != "" || task.CurrentStep >= len(task.Plan.Steps) {
		return false
	}
	step := task.Plan.Steps[task.CurrentStep]
	return step.State == agentrun.StepPending || (step.State == agentrun.StepWaiting && agentrun.WaitLike(step.Type))
}

// dueWake derives the wake a scheduler owes a parked task, if any. Event ids
// are a function of the task, its version and the condition, so every tick and
// replica that observes the same parked state produces the same id.
func dueWake(task agentrun.AgentTask, now time.Time) (agentrun.WakeEvent, bool) {
	w := task.Wake
	if w == nil || task.State != agentrun.StateWaiting {
		return agentrun.WakeEvent{}, false
	}
	switch w.Kind {
	case agentrun.WakeTimer:
		if w.DueAt.After(now) {
			return agentrun.WakeEvent{}, false
		}
		id := fmt.Sprintf("timer:%s:%d:%d", task.ID, task.Version, w.DueAt.UnixNano())
		return agentrun.WakeEvent{ID: id, Kind: w.Kind, Key: w.Key, Correlation: w.Correlation, OccurredAt: now}, true
	case agentrun.WakePolling:
		if task.UpdatedAt.Add(w.PollAfter).After(now) {
			return agentrun.WakeEvent{}, false
		}
		id := fmt.Sprintf("poll:%s:%d", task.ID, task.Version)
		return agentrun.WakeEvent{ID: id, Kind: w.Kind, Key: w.Key, Correlation: w.Correlation, OccurredAt: now}, true
	}
	return agentrun.WakeEvent{}, false
}

// deliver runs a wake through the authority recheck and the dedupe inbox and,
// when it is accepted, completes the resumed wait step and drives the task on.
func (r *Runner) deliver(ctx context.Context, taskID string, event agentrun.WakeEvent) (agentrun.WakeResult, error) {
	res, err := r.Wake(ctx, taskID, event)
	if err != nil || !res.Accepted {
		return res, err
	}
	task := res.Task
	if task.CurrentStep < len(task.Plan.Steps) {
		step := task.Plan.Steps[task.CurrentStep]
		if agentrun.WaitLike(step.Type) && step.State == agentrun.StepWaiting {
			done, err := r.Runtime.CompleteWait(ctx, taskID, task.Version, wakeStepResult(event), r.p.cfg.Clock())
			if err != nil && !errors.Is(err, agentrun.ErrConflict) && !errors.Is(err, agentrun.ErrStepNotReady) {
				return res, err
			}
			if err == nil {
				task = done
			}
		}
	}
	driven, err := r.drive(ctx, taskID, ModeOnBehalfOf, tickMaxSteps)
	if driven.ID != "" {
		task = driven
	}
	res.Task = task
	return res, err
}

// Drive runs a confirmed task forward, one gated step at a time, until it is
// no longer RUNNING: it parks on a WAIT or ASK_USER step, waits for approval,
// completes or fails. A step that fails the task returns the FAILED task with
// a nil error (the failure code and detail are on the task); an error is
// returned only when the task could not be advanced for a reason that left it
// runnable, such as a store outage. If another worker holds the step, Drive
// returns the task as it stands.
func (r *Runner) Drive(ctx context.Context, taskID string, mode Mode) (agentrun.AgentTask, error) {
	return r.drive(ctx, taskID, mode, agentrun.MaxPlanSteps*2)
}

func (r *Runner) drive(ctx context.Context, taskID string, mode Mode, limit int) (agentrun.AgentTask, error) {
	if !mode.valid() {
		return agentrun.AgentTask{}, fmt.Errorf("%w: unknown run mode %q", ErrInvalid, mode)
	}
	var task agentrun.AgentTask
	for range limit {
		var err error
		task, err = r.Runtime.GetTask(ctx, taskID)
		if err != nil {
			return task, err
		}
		if task.State != agentrun.StateRunning || !task.Plan.Confirmed || task.CurrentStep >= len(task.Plan.Steps) {
			return task, nil
		}
		step := task.Plan.Steps[task.CurrentStep]
		if step.State == agentrun.StepWaiting && agentrun.WaitLike(step.Type) && task.WorkerLease == "" {
			// A wake resumed the task but the worker died before completing the
			// wait step: complete it from the durable resume.
			if task.LastWake == nil {
				return task, fmt.Errorf("%w: resumed wait has no durable wake receipt", agentrun.ErrReconciliationRequired)
			}
			if _, err := r.Runtime.CompleteWait(ctx, taskID, task.Version, wakeStepResult(*task.LastWake), r.p.cfg.Clock()); err != nil && !errors.Is(err, agentrun.ErrConflict) && !errors.Is(err, agentrun.ErrStepNotReady) {
				return task, err
			}
			continue
		}
		if task.WorkerLease != "" || step.State != agentrun.StepPending {
			return task, nil
		}
		next, err := r.Step(ctx, taskID, mode)
		switch {
		case err == nil:
		case errors.Is(err, agentrun.ErrApprovalRequired):
			return next, nil
		case errors.Is(err, agentrun.ErrStepPaused):
			return next, nil
		case errors.Is(err, agentrun.ErrConflict), errors.Is(err, agentrun.ErrStepNotReady):
			return r.Runtime.GetTask(ctx, taskID)
		default:
			latest, getErr := r.Runtime.GetTask(ctx, taskID)
			if getErr == nil && latest.State == agentrun.StateFailed {
				return latest, nil
			}
			return next, err
		}
	}
	return r.Runtime.GetTask(ctx, taskID)
}

func wakeStepResult(event agentrun.WakeEvent) agentrun.StepResult {
	ref := event.PayloadRef
	if ref == "" {
		ref = "wake:" + event.ID
	}
	var taint []string
	if event.Kind == agentrun.WakeUserReply {
		taint = []string{string(agentsecurity.TaintHuman)}
	}
	return agentrun.StepResult{Ref: ref, Digest: digestOf("wake", event.ID, string(event.Kind), event.Key, event.PayloadRef), Taint: taint}
}

// parkIfWait parks the task when its current step is a WAIT or ASK_USER step
// ready to run. ok reports that the step was one of those and was handled. A
// WAIT step with no wait condition is not parkable and falls through to the
// executor, which fails it with ErrUnsupported instead of stranding the task.
func (r *Runner) parkIfWait(ctx context.Context, task agentrun.AgentTask) (agentrun.AgentTask, bool, error) {
	if task.State != agentrun.StateRunning || !task.Plan.Confirmed || task.CurrentStep >= len(task.Plan.Steps) {
		return task, false, nil
	}
	step := task.Plan.Steps[task.CurrentStep]
	if !agentrun.WaitLike(step.Type) || step.State != agentrun.StepPending || (step.Type == agentrun.StepWait && step.Wait == nil) {
		return task, false, nil
	}
	now := r.p.cfg.Clock().UTC()
	parked, err := r.Runtime.ParkStep(ctx, task.ID, task.Version, waitCondition(step, now), now)
	return parked, true, err
}

// waitCondition resolves a wait step's declared condition: a relative timer
// becomes an absolute due time, keys default to a per-step key, and a timer
// gets a stale deadline so a lost wake resolves to a typed outcome.
func waitCondition(step agentrun.PlanStep, now time.Time) agentrun.WakeCondition {
	var c agentrun.WakeCondition
	if step.Wait != nil {
		c = *step.Wait
	}
	if step.Type == agentrun.StepAskUser {
		c.Kind = agentrun.WakeUserReply
	}
	if strings.TrimSpace(c.Key) == "" {
		switch c.Kind {
		case agentrun.WakeTimer:
			c.Key = "timer:" + step.ID
		case agentrun.WakePolling:
			c.Key = "poll:" + step.ID
		default:
			c.Key = "reply:" + step.ID
		}
	}
	if c.Kind == agentrun.WakeTimer {
		if c.DueAt.IsZero() {
			c.DueAt = now.Add(c.PollAfter)
		}
		if c.StaleAfter.IsZero() {
			c.StaleAfter = c.DueAt.Add(timerStaleGrace)
		}
	}
	return c
}

// DeliverApproval is the approval wake: the task owner approves the exact
// step digest a T3 or T4 step parked on, the delegation grant is re-exchanged
// and authority rechecked, and the task is driven on. A repeated delivery of
// an approval already applied returns the task without a second run.
func (r *Runner) DeliverApproval(ctx context.Context, taskID, stepID, digest, userID string) (agentrun.AgentTask, error) {
	task, err := r.ownedTask(ctx, taskID, userID)
	if err != nil {
		return task, err
	}
	for _, step := range task.Plan.Steps {
		if step.ID == stepID && step.Approved && step.ApprovalDigest == digest && digest != "" && task.State != agentrun.StateAwaitingApproval {
			return task, nil
		}
	}
	if task.State == agentrun.StateAwaitingApproval {
		if err := (wakeRechecker{runner: r}).RecheckWake(ctx, task, agentrun.WakeEvent{Kind: agentrun.WakeApproval, Key: digest}); err != nil {
			return r.pauseAfterAuthorityDenial(ctx, task, err)
		}
	}
	approved, err := r.Runtime.ApproveStep(ctx, taskID, stepID, digest, task.Version, r.p.cfg.Clock())
	if err != nil {
		if errors.Is(err, agentrun.ErrConflict) {
			// A concurrent delivery won; the task moved on.
			return r.Runtime.GetTask(ctx, taskID)
		}
		return task, err
	}
	driven, err := r.drive(ctx, taskID, ModeOnBehalfOf, tickMaxSteps)
	if driven.ID == "" {
		driven = approved
	}
	return driven, err
}

// DeliverSignal is the workflow-signal wake: a signal named key (with the
// task's correlation, when it declared one) resumes a task parked on it. The
// event id dedupes redelivery; a signal that matches no parked task is
// reported Ignored.
func (r *Runner) DeliverSignal(ctx context.Context, taskID, key, correlation, eventID string) (agentrun.WakeResult, error) {
	if strings.TrimSpace(eventID) == "" {
		return agentrun.WakeResult{}, fmt.Errorf("%w: a signal event id is required", ErrInvalid)
	}
	return r.deliver(ctx, taskID, agentrun.WakeEvent{
		ID: eventID, Kind: agentrun.WakeSignal, Key: key, Correlation: correlation, OccurredAt: r.p.cfg.Clock().UTC(),
	})
}

// DeliverUserReply is the user-reply wake for an ASK_USER (or USER_REPLY) wait.
// Only the task owner may reply. key may be empty, meaning the reply the task
// is parked on; payloadRef is a durable reference to the reply, never the text.
func (r *Runner) DeliverUserReply(ctx context.Context, taskID, userID, key, eventID, payloadRef string) (agentrun.WakeResult, error) {
	if strings.TrimSpace(eventID) == "" {
		return agentrun.WakeResult{}, fmt.Errorf("%w: a reply event id is required", ErrInvalid)
	}
	task, err := r.ownedTask(ctx, taskID, userID)
	if err != nil {
		return agentrun.WakeResult{Task: task}, err
	}
	if strings.TrimSpace(key) == "" && task.Wake != nil && task.Wake.Kind == agentrun.WakeUserReply {
		key = task.Wake.Key
	}
	if strings.TrimSpace(key) == "" {
		return agentrun.WakeResult{Ignored: true, Task: task}, nil
	}
	return r.deliver(ctx, taskID, agentrun.WakeEvent{
		ID: eventID, Kind: agentrun.WakeUserReply, Key: key, PayloadRef: payloadRef, OccurredAt: r.p.cfg.Clock().UTC(),
	})
}

// ownedTask loads a task and refuses anyone but its owner.
func (r *Runner) ownedTask(ctx context.Context, taskID, userID string) (agentrun.AgentTask, error) {
	task, err := r.Runtime.GetTask(ctx, taskID)
	if err != nil {
		return task, err
	}
	if strings.TrimSpace(userID) == "" || task.UserID != userID {
		return task, fmt.Errorf("%w: only the task owner may deliver this wake", ErrDenied)
	}
	return task, nil
}
