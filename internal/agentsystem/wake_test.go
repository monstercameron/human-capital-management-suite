package agentsystem

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

func timerWait(id string, due time.Time) agentrun.PlanStep {
	step := planStep(id, agentrun.StepWait, "skill.lookup", agentrun.TierRead)
	step.Wait = &agentrun.WakeCondition{Kind: agentrun.WakeTimer, Key: "promotion-due", DueAt: due}
	return step
}

func signalWait(id, key, correlation string, stale time.Time) agentrun.PlanStep {
	step := planStep(id, agentrun.StepWait, "skill.lookup", agentrun.TierRead)
	step.Wait = &agentrun.WakeCondition{Kind: agentrun.WakeSignal, Key: key, Correlation: correlation, StaleAfter: stale}
	return step
}

// startParked starts a task and drives it to its first park.
func (f *fixture) startParked(t *testing.T, id string, steps ...agentrun.PlanStep) agentrun.AgentTask {
	t.Helper()
	task := f.start(t, id, steps...)
	task, err := f.runner.Drive(context.Background(), task.ID, ModeOnBehalfOf)
	if err != nil {
		t.Fatalf("Drive: %v", err)
	}
	if task.State != agentrun.StateWaiting || task.Wake == nil {
		t.Fatalf("task = %s wake=%v, want WAITING with a wake condition", task.State, task.Wake)
	}
	return task
}

func (f *fixture) get(t *testing.T, id string) agentrun.AgentTask {
	t.Helper()
	task, err := f.runner.Runtime.GetTask(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func stepResults(task agentrun.AgentTask) int {
	n := 0
	for _, e := range task.Ledger.Entries {
		if e.Kind == "STEP_RESULT" {
			n++
		}
	}
	return n
}

// TestTodo_AGENT2_011_Delivery proves a due timer resumes a parked task
// exactly once over concurrent ticks with a deterministic event id, that a
// timer that is not due is untouched, and that the parked task holds no lease.
func TestTodo_AGENT2_011_Delivery(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	due := fixedNow.Add(time.Hour)
	parked := f.startParked(t, "task-timer", timerWait("wait", due), planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead))
	if parked.WorkerLease != "" || parked.ModelSession != "" || parked.Plan.Steps[0].State != agentrun.StepWaiting || parked.Wake.Kind != agentrun.WakeTimer || !parked.Wake.DueAt.Equal(due) {
		t.Fatalf("parked task = %+v, want a lease-free WAITING task on the timer", parked)
	}
	if parked.Wake.StaleAfter.IsZero() {
		t.Fatal("timer wake has no stale deadline for the sweeper")
	}

	moved, err := f.platform.TickTenant(context.Background(), tenantKey, fixedNow)
	if err != nil || moved != 0 {
		t.Fatalf("not-due tick = %d, %v, want 0 moved", moved, err)
	}
	if got := f.get(t, "task-timer"); got.Version != parked.Version || got.State != agentrun.StateWaiting || f.owner.calls() != 0 {
		t.Fatalf("not-due timer touched the task: state %s version %d->%d calls %d", got.State, parked.Version, got.Version, f.owner.calls())
	}

	const ticks = 8
	var wg sync.WaitGroup
	errs := make(chan error, ticks)
	total := make(chan int, ticks)
	for range ticks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := f.platform.TickTenant(context.Background(), tenantKey, due.Add(time.Minute))
			errs <- err
			total <- n
		}()
	}
	wg.Wait()
	close(errs)
	close(total)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent tick: %v", err)
		}
	}
	sum := 0
	for n := range total {
		sum += n
	}
	if sum < 1 {
		t.Fatalf("ticks moved %d tasks, want at least the resumed one", sum)
	}
	got := f.get(t, "task-timer")
	wantRef := fmt.Sprintf("wake:timer:task-timer:%d:%d", parked.Version, due.UnixNano())
	// A concurrent drive rebuilds the same result from the durable wake receipt.
	if ref := got.Plan.Steps[0].ResultRef; got.State != agentrun.StateCompleted || ref != wantRef {
		t.Fatalf("task = %s wait result %q, want COMPLETED with deterministic event %q", got.State, got.Plan.Steps[0].ResultRef, wantRef)
	}
	if f.owner.calls() != 1 || got.Plan.Steps[1].Attempt != 1 || stepResults(got) != 2 {
		t.Fatalf("read ran %d times (attempt %d, results %d), want exactly once", f.owner.calls(), got.Plan.Steps[1].Attempt, stepResults(got))
	}
	if n, err := f.platform.TickTenant(context.Background(), tenantKey, due.Add(2*time.Minute)); err != nil || n != 0 {
		t.Fatalf("tick after completion = %d, %v, want 0", n, err)
	}
}

// TestTodo_AGENT2_011_DeliveryRace hammers the tick, direct wake and signal
// paths at once: the resumed step must run once.
func TestTodo_AGENT2_011_DeliveryRace(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	due := fixedNow.Add(time.Hour)
	f.startParked(t, "task-race", timerWait("wait", due), planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead))
	f.startParked(t, "task-race-sig", signalWait("wait", "sig", "corr", time.Time{}), planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead))

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := range 12 {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_, err := f.platform.TickTenant(context.Background(), tenantKey, due.Add(time.Minute))
			errs <- err
		}()
		go func() {
			defer wg.Done()
			_, err := f.runner.DeliverSignal(context.Background(), "task-race-sig", "sig", "corr", "sig-event")
			errs <- err
		}()
		go func() {
			defer wg.Done()
			_, err := f.runner.DeliverSignal(context.Background(), "task-race-sig", "sig", "corr", fmt.Sprintf("sig-retry-%d", i))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("racing delivery: %v", err)
		}
	}
	for _, id := range []string{"task-race", "task-race-sig"} {
		got := f.get(t, id)
		if got.State != agentrun.StateCompleted || got.Plan.Steps[1].Attempt != 1 || stepResults(got) != 2 {
			t.Fatalf("%s = %s attempt %d results %d, want COMPLETED with the step run once", id, got.State, got.Plan.Steps[1].Attempt, stepResults(got))
		}
	}
	if f.owner.calls() != 2 {
		t.Fatalf("owner calls = %d, want one per task", f.owner.calls())
	}
}

// TestTodo_AGENT2_011_DeliveryFault proves a wake that fails the authority
// recheck, arrives after cancel or expiry, or belongs to a gated-off tenant
// runs no step.
func TestTodo_AGENT2_011_DeliveryFault(t *testing.T) {
	due := fixedNow.Add(time.Hour)
	tickAfter := due.Add(time.Minute)
	setup := func(t *testing.T, id string) *fixture {
		f := newFixture(t, nil)
		f.defaultOwner(t)
		f.startParked(t, id, timerWait("wait", due), planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead))
		return f
	}
	t.Run("deactivated user", func(t *testing.T) {
		f := setup(t, "task-d")
		f.resolver.deactivate()
		task := f.get(t, "task-d")
		_, err := f.runner.Wake(context.Background(), "task-d", agentrun.WakeEvent{ID: "x", Kind: agentrun.WakeTimer, Key: task.Wake.Key, OccurredAt: tickAfter})
		if !errors.Is(err, ErrDenied) || !errors.Is(err, agentdelegation.ErrUserInactive) {
			t.Fatalf("direct wake = %v, want ErrDenied wrapping ErrUserInactive", err)
		}
		paused := f.get(t, "task-d")
		if paused.State != agentrun.StatePaused || paused.FailureCode != string(agentrun.PauseUserInactive) || f.owner.calls() != 0 {
			t.Fatalf("revoked wake did not pause: %+v", paused)
		}
		if n, err := f.platform.TickTenant(context.Background(), tenantKey, tickAfter); err != nil || n != 0 {
			t.Fatalf("paused tick = %d, %v", n, err)
		}
	})
	t.Run("revoked grant", func(t *testing.T) {
		f := setup(t, "task-r")
		if err := f.runner.Delegation.RevokeGrant(GrantID("task-r"), "user cancelled"); err != nil {
			t.Fatal(err)
		}
		if n, err := f.platform.TickTenant(context.Background(), tenantKey, tickAfter); err != nil || n != 1 {
			t.Fatalf("tick = %d, %v", n, err)
		}
		paused := f.get(t, "task-r")
		if paused.State != agentrun.StatePaused || paused.FailureCode != string(agentrun.PauseGrantRevoked) || f.owner.calls() != 0 {
			t.Fatalf("grant revocation did not pause: %+v", paused)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		f := setup(t, "task-c")
		task := f.get(t, "task-c")
		if _, err := f.runner.Runtime.Cancel(context.Background(), "task-c", task.Version, fixedNow); err != nil {
			t.Fatal(err)
		}
		if n, err := f.platform.TickTenant(context.Background(), tenantKey, tickAfter); err != nil || n != 0 {
			t.Fatalf("tick = %d, %v", n, err)
		}
		res, err := f.runner.DeliverSignal(context.Background(), "task-c", "promotion-due", "", "late")
		if err != nil || res.Accepted {
			t.Fatalf("wake after cancel = %+v, %v, want ignored", res, err)
		}
		if got := f.get(t, "task-c"); got.State != agentrun.StateCancelled || f.owner.calls() != 0 {
			t.Fatalf("task = %s calls=%d, want CANCELLED and no step", got.State, f.owner.calls())
		}
	})
	t.Run("paused", func(t *testing.T) {
		f := setup(t, "task-p")
		task := f.get(t, "task-p")
		if _, err := f.runner.Runtime.Pause(context.Background(), "task-p", task.Version, fixedNow); err != nil {
			t.Fatal(err)
		}
		if n, err := f.platform.TickTenant(context.Background(), tenantKey, tickAfter); err != nil || n != 0 {
			t.Fatalf("tick = %d, %v", n, err)
		}
		if got := f.get(t, "task-p"); got.State != agentrun.StatePaused || f.owner.calls() != 0 {
			t.Fatalf("task = %s calls=%d, want PAUSED and no step", got.State, f.owner.calls())
		}
	})
	t.Run("expired", func(t *testing.T) {
		f := setup(t, "task-e")
		n, err := f.platform.TickTenant(context.Background(), tenantKey, fixedNow.Add(25*time.Hour))
		got := f.get(t, "task-e")
		if err != nil || n != 1 || got.State != agentrun.StateExpired || got.FailureCode != "TASK_EXPIRED" || f.owner.calls() != 0 {
			t.Fatalf("expired tick = %d, %v; task %s %s calls=%d, want EXPIRED and no step", n, err, got.State, got.FailureCode, f.owner.calls())
		}
	})
	t.Run("gate off", func(t *testing.T) {
		f := setup(t, "task-g")
		cfg := f.platform.cfg
		var asked []string
		var mu sync.Mutex
		cfg.WakeGate = func(_ context.Context, tenant string) bool {
			mu.Lock()
			asked = append(asked, tenant)
			mu.Unlock()
			return false
		}
		gated, err := NewPlatform(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := gated.TickTenant(context.Background(), tenantKey, tickAfter); err != nil || n != 1 {
			t.Fatalf("gated tick = %d, %v, want one paused task", n, err)
		}
		paused := f.get(t, "task-g")
		if paused.State != agentrun.StatePaused || paused.FailureCode != string(agentrun.PauseTenantDisabled) || f.owner.calls() != 0 {
			t.Fatalf("disabled task=%+v", paused)
		}
		if len(asked) != 1 || asked[0] != tenantKey {
			t.Fatalf("gate asked %v, want the ticked tenant", asked)
		}
		cfg.WakeGate = func(context.Context, string) bool { return true }
		open, err := NewPlatform(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if n, err := open.TickTenant(context.Background(), tenantKey, tickAfter); err != nil || n != 0 {
			t.Fatalf("enable silently resumed paused task: %d,%v", n, err)
		}
		if _, err := f.runner.Runtime.Resume(context.Background(), paused.ID, paused.Version, fixedNow); err != nil {
			t.Fatal(err)
		}
		if n, err := open.TickTenant(context.Background(), tenantKey, tickAfter); err != nil || n != 1 {
			t.Fatalf("open tick = %d, %v, want the task delivered", n, err)
		}
		if got := f.get(t, "task-g"); got.State != agentrun.StateCompleted {
			t.Fatalf("task = %s, want COMPLETED once the gate opens", got.State)
		}
	})
	t.Run("bad input", func(t *testing.T) {
		f := setup(t, "task-b")
		if _, err := f.platform.TickTenant(context.Background(), tenantKey, time.Time{}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("zero time = %v, want ErrInvalid", err)
		}
		if _, err := f.platform.TickTenant(context.Background(), "", tickAfter); !errors.Is(err, ErrInvalid) {
			t.Fatalf("empty tenant = %v, want ErrInvalid", err)
		}
		var nilPlatform *Platform
		if _, err := nilPlatform.TickTenant(context.Background(), tenantKey, tickAfter); !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("nil platform = %v, want ErrNotConfigured", err)
		}
		if _, err := f.runner.DeliverSignal(context.Background(), "task-b", "k", "", " "); !errors.Is(err, ErrInvalid) {
			t.Fatalf("blank event id = %v, want ErrInvalid", err)
		}
		if _, err := f.runner.Drive(context.Background(), "task-b", Mode("BOGUS")); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bogus mode = %v, want ErrInvalid", err)
		}
		if _, err := f.runner.Drive(context.Background(), "no-such-task", ModeOnBehalfOf); !errors.Is(err, agentrun.ErrNotFound) {
			t.Fatalf("missing task = %v, want ErrNotFound", err)
		}
	})
}

// TestTodo_AGENT2_011_DeliveryRecovery proves a wake that never lands is
// resolved by the sweeper to a typed LOST_WAKE outcome, and that a polling
// wake is delivered once its interval elapses.
func TestTodo_AGENT2_011_DeliveryRecovery(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	stale := fixedNow.Add(2 * time.Hour)
	f.startParked(t, "task-lost", signalWait("wait", "never", "", stale), planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead))
	if n, err := f.platform.TickTenant(context.Background(), tenantKey, fixedNow.Add(time.Hour)); err != nil || n != 0 {
		t.Fatalf("early tick = %d, %v", n, err)
	}
	n, err := f.platform.TickTenant(context.Background(), tenantKey, stale.Add(time.Second))
	got := f.get(t, "task-lost")
	if err != nil || n != 1 || got.State != agentrun.StateFailed || got.FailureCode != "LOST_WAKE" || got.Wake != nil || f.owner.calls() != 0 {
		t.Fatalf("lost wake tick = %d, %v; task %s %s wake=%v", n, err, got.State, got.FailureCode, got.Wake)
	}
	if late, err := f.runner.DeliverSignal(context.Background(), "task-lost", "never", "", "too-late"); err != nil || late.Accepted {
		t.Fatalf("wake after LOST_WAKE = %+v, %v, want ignored", late, err)
	}

	poll := planStep("poll", agentrun.StepWait, "skill.lookup", agentrun.TierRead)
	poll.Wait = &agentrun.WakeCondition{Kind: agentrun.WakePolling, Key: "poll:status", PollAfter: 10 * time.Minute}
	parked := f.startParked(t, "task-poll", poll, planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead))
	if n, err := f.platform.TickTenant(context.Background(), tenantKey, parked.UpdatedAt.Add(5*time.Minute)); err != nil || n != 0 {
		t.Fatalf("polling tick before its interval = %d, %v", n, err)
	}
	if n, err := f.platform.TickTenant(context.Background(), tenantKey, parked.UpdatedAt.Add(11*time.Minute)); err != nil || n != 1 {
		t.Fatalf("polling tick after its interval = %d, %v", n, err)
	}
	if got := f.get(t, "task-poll"); got.State != agentrun.StateCompleted || got.Plan.Steps[1].Attempt != 1 {
		t.Fatalf("polled task = %s, want COMPLETED once", got.State)
	}
}

// TestTodo_AGENT2_011_DeliveryDrive proves the tick drives a runnable task
// that no worker holds, that a failing step ends the task in FAILED instead
// of being retried, and that Drive stops at an approval.
func TestTodo_AGENT2_011_DeliveryDrive(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	f.start(t, "task-run", planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead), planStep("analyze", agentrun.StepAnalyze, "skill.summarize", agentrun.TierPrivateDraft))
	n, err := f.platform.TickTenant(context.Background(), tenantKey, fixedNow)
	if got := f.get(t, "task-run"); err != nil || n != 1 || got.State != agentrun.StateCompleted {
		t.Fatalf("tick drove %d tasks (%v), task %s, want the runnable task completed", n, err, got.State)
	}

	base := f.owner.result
	f.owner.result = func(call Invocation) (Result, error) {
		if call.Task.ID == "task-fail" {
			return Result{}, errors.New("owner exploded")
		}
		return base(call)
	}
	f.start(t, "task-fail", planStep("act", agentrun.StepRead, "skill.lookup", agentrun.TierRead))
	before := f.owner.calls()
	failed, err := f.runner.Drive(context.Background(), "task-fail", ModeOnBehalfOf)
	if err != nil || failed.State != agentrun.StateFailed || failed.FailureCode != "STEP_FAILED" {
		t.Fatalf("Drive of a failing step = %s %s, %v, want the FAILED task and a nil error", failed.State, failed.FailureCode, err)
	}
	for range 3 {
		if _, err := f.platform.TickTenant(context.Background(), tenantKey, fixedNow); err != nil {
			t.Fatal(err)
		}
	}
	if f.owner.calls() != before+1 {
		t.Fatalf("failed step ran %d times, want once", f.owner.calls()-before)
	}

	f.start(t, "task-appr", planStep("update", agentrun.StepSubmit, "skill.update", agentrun.TierSubmitGoverned))
	parked, err := f.runner.Drive(context.Background(), "task-appr", ModeOnBehalfOf)
	if err != nil || parked.State != agentrun.StateAwaitingApproval || parked.WorkerLease != "" || parked.Wake == nil || parked.Wake.Kind != agentrun.WakeApproval {
		t.Fatalf("Drive at T3 = %s %v, want AWAITING_APPROVAL with an approval wake and a nil error", parked.State, err)
	}
	if n, err := f.platform.TickTenant(context.Background(), tenantKey, fixedNow); err != nil || n != 0 {
		t.Fatalf("tick over an approval wait = %d, %v, want untouched", n, err)
	}
}

// TestTodo_AGENT2_011_WaitStep proves WAIT and ASK_USER steps park with a
// typed wake condition, a wake completes the step (the event is its result)
// and the task advances.
func TestTodo_AGENT2_011_WaitStep(t *testing.T) {
	t.Run("signal wait", func(t *testing.T) {
		f := newFixture(t, nil)
		f.defaultOwner(t)
		parked := f.startParked(t, "task-sig", signalWait("wait", "promotion-complete", "promotion/42", fixedNow.Add(time.Hour)), planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead))
		if w := parked.Wake; w.Kind != agentrun.WakeSignal || w.Key != "promotion-complete" || w.Correlation != "promotion/42" {
			t.Fatalf("wake = %+v", w)
		}
		if res, err := f.runner.DeliverSignal(context.Background(), "task-sig", "other-signal", "promotion/42", "e0"); err != nil || res.Accepted {
			t.Fatalf("wrong signal = %+v, %v, want ignored", res, err)
		}
		if res, err := f.runner.DeliverSignal(context.Background(), "task-sig", "promotion-complete", "promotion/99", "e1"); err != nil || res.Accepted {
			t.Fatalf("wrong correlation = %+v, %v, want ignored", res, err)
		}
		res, err := f.runner.DeliverSignal(context.Background(), "task-sig", "promotion-complete", "promotion/42", "e2")
		if err != nil || !res.Accepted || res.Task.State != agentrun.StateCompleted || res.Task.Plan.Steps[0].ResultRef != "wake:e2" {
			t.Fatalf("signal = %+v, %v, want accepted and the task completed with the event as the wait result", res, err)
		}
		if dup, err := f.runner.DeliverSignal(context.Background(), "task-sig", "promotion-complete", "promotion/42", "e2"); err != nil || !dup.Duplicate || dup.Accepted {
			t.Fatalf("duplicate signal = %+v, %v", dup, err)
		}
		if f.owner.calls() != 1 {
			t.Fatalf("read ran %d times, want once", f.owner.calls())
		}
	})
	t.Run("relative timer and defaults", func(t *testing.T) {
		f := newFixture(t, nil)
		f.defaultOwner(t)
		step := planStep("wait", agentrun.StepWait, "skill.lookup", agentrun.TierRead)
		step.Wait = &agentrun.WakeCondition{Kind: agentrun.WakeTimer, PollAfter: 3 * time.Hour}
		parked := f.startParked(t, "task-rel", step)
		if w := parked.Wake; !w.DueAt.Equal(fixedNow.Add(3*time.Hour)) || w.Key != "timer:wait" || !w.StaleAfter.Equal(w.DueAt.Add(timerStaleGrace)) {
			t.Fatalf("resolved wake = %+v, want an absolute due time, a step key and a stale deadline", w)
		}
		if n, err := f.platform.TickTenant(context.Background(), tenantKey, fixedNow.Add(4*time.Hour)); err != nil || n != 1 {
			t.Fatalf("tick = %d, %v", n, err)
		}
		if got := f.get(t, "task-rel"); got.State != agentrun.StateCompleted {
			t.Fatalf("task = %s, want COMPLETED (a final wait step ends the task)", got.State)
		}
	})
	t.Run("ask user", func(t *testing.T) {
		f := newFixture(t, nil)
		f.defaultOwner(t)
		parked := f.startParked(t, "task-ask", planStep("ask", agentrun.StepAskUser, "skill.lookup", agentrun.TierRead), planStep("read", agentrun.StepRead, "skill.lookup", agentrun.TierRead))
		if w := parked.Wake; w.Kind != agentrun.WakeUserReply || w.Key != "reply:ask" || parked.WorkerLease != "" {
			t.Fatalf("ASK_USER wake = %+v", w)
		}
		if n, err := f.platform.TickTenant(context.Background(), tenantKey, fixedNow.Add(10*time.Hour)); err != nil || n != 0 {
			t.Fatalf("a user-reply wait must not be woken by the scheduler: %d, %v", n, err)
		}
		if _, err := f.runner.DeliverUserReply(context.Background(), "task-ask", "user-other", "", "r0", "reply:1"); !errors.Is(err, ErrDenied) {
			t.Fatalf("non-owner reply = %v, want ErrDenied", err)
		}
		if got := f.get(t, "task-ask"); got.State != agentrun.StateWaiting || f.owner.calls() != 0 {
			t.Fatalf("non-owner reply moved the task: %s", got.State)
		}
		res, err := f.runner.DeliverUserReply(context.Background(), "task-ask", "user-42", "", "r1", "reply:thread/1")
		if err != nil || !res.Accepted || res.Task.State != agentrun.StateCompleted {
			t.Fatalf("owner reply = %+v, %v", res, err)
		}
		got := f.get(t, "task-ask")
		var entry agentrun.LedgerEntry
		for _, e := range got.Ledger.Entries {
			if e.Ref == "reply:thread/1" {
				entry = e
			}
		}
		if got.Plan.Steps[0].ResultRef != "reply:thread/1" || len(entry.Taint) != 1 || entry.Taint[0] != string(agentsecurity.TaintHuman) || f.owner.calls() != 1 {
			t.Fatalf("reply result %q ledger %+v calls %d, want the payload ref as a human-asserted result and the next step run once", got.Plan.Steps[0].ResultRef, entry, f.owner.calls())
		}
		if dup, err := f.runner.DeliverUserReply(context.Background(), "task-ask", "user-42", "", "r1", "reply:thread/1"); err != nil || dup.Accepted {
			t.Fatalf("duplicate reply = %+v, %v", dup, err)
		}
	})
	t.Run("wait with no condition is not executable", func(t *testing.T) {
		f := newFixture(t, nil)
		f.defaultOwner(t)
		task := f.start(t, "task-bare", planStep("wait", agentrun.StepWait, "skill.lookup", agentrun.TierRead))
		got, err := f.runner.Drive(context.Background(), task.ID, ModeOnBehalfOf)
		if err != nil || got.State != agentrun.StateFailed {
			t.Fatalf("bare WAIT = %s, %v, want a typed FAILED task rather than a stranded one", got.State, err)
		}
	})
	t.Run("paused waiting task resumes parked", func(t *testing.T) {
		f := newFixture(t, nil)
		f.defaultOwner(t)
		parked := f.startParked(t, "task-pr", timerWait("wait", fixedNow.Add(time.Hour)))
		paused, err := f.runner.Runtime.Pause(context.Background(), "task-pr", parked.Version, fixedNow)
		if err != nil {
			t.Fatal(err)
		}
		resumed, err := f.runner.Runtime.Resume(context.Background(), "task-pr", paused.Version, fixedNow)
		if err != nil || resumed.State != agentrun.StateWaiting || resumed.Wake == nil || resumed.Wake.Kind != agentrun.WakeTimer {
			t.Fatalf("resumed = %s %+v %v, want the WAITING task and its timer back", resumed.State, resumed.Wake, err)
		}
		if n, err := f.platform.TickTenant(context.Background(), tenantKey, fixedNow.Add(2*time.Hour)); err != nil || n != 1 {
			t.Fatalf("tick after resume = %d, %v", n, err)
		}
	})
}

// TestTodo_AGENT2_011_ApprovalDelivery proves a T3 step parks for approval,
// only the owner can approve, the exact digest is required, the delegated
// authority is rechecked, and the approved step runs exactly once.
func TestTodo_AGENT2_011_ApprovalDelivery(t *testing.T) {
	f := newFixture(t, nil)
	f.defaultOwner(t)
	task := f.start(t, "task-appr", planStep("update", agentrun.StepSubmit, "skill.update", agentrun.TierSubmitGoverned))
	parked, err := f.runner.Drive(context.Background(), task.ID, ModeOnBehalfOf)
	if err != nil || parked.State != agentrun.StateAwaitingApproval {
		t.Fatalf("Drive = %s, %v, want AWAITING_APPROVAL", parked.State, err)
	}
	digest := parked.Plan.Steps[0].ApprovalDigest
	if _, err := f.runner.DeliverApproval(context.Background(), "task-appr", "update", digest, "user-other"); !errors.Is(err, ErrDenied) {
		t.Fatalf("non-owner approval = %v, want ErrDenied", err)
	}
	if _, err := f.runner.DeliverApproval(context.Background(), "task-appr", "update", "sha256:wrong", "user-42"); !errors.Is(err, agentrun.ErrApprovalRequired) {
		t.Fatalf("wrong digest = %v, want ErrApprovalRequired", err)
	}
	if got := f.get(t, "task-appr"); got.State != agentrun.StateAwaitingApproval || f.owner.calls() != 0 {
		t.Fatalf("refused approvals moved the task: %s calls=%d", got.State, f.owner.calls())
	}

	f.resolver.deactivate()
	if _, err := f.runner.DeliverApproval(context.Background(), "task-appr", "update", digest, "user-42"); !errors.Is(err, ErrDenied) {
		t.Fatalf("approval by a deactivated user = %v, want the authority recheck to refuse it", err)
	}
	if got := f.get(t, "task-appr"); got.State != agentrun.StatePaused || got.FailureCode != string(agentrun.PauseUserInactive) || got.Plan.Steps[0].Approved || got.Plan.Steps[0].ApprovalDigest != "" || f.owner.calls() != 0 {
		t.Fatalf("deactivated approval did not pause and void: %+v", got)
	}
	f.resolver.mu.Lock()
	f.resolver.active = true
	f.resolver.mu.Unlock()
	paused := f.get(t, "task-appr")
	if _, err := f.runner.Runtime.Resume(context.Background(), paused.ID, paused.Version, fixedNow); err != nil {
		t.Fatal(err)
	}
	requested, err := f.runner.Drive(context.Background(), paused.ID, ModeOnBehalfOf)
	if err != nil || requested.State != agentrun.StateAwaitingApproval {
		t.Fatalf("fresh approval request = %+v, %v", requested, err)
	}
	digest = requested.Plan.Steps[0].ApprovalDigest

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.runner.DeliverApproval(context.Background(), "task-appr", "update", digest, "user-42")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent approval: %v", err)
		}
	}
	got := f.get(t, "task-appr")
	if got.State != agentrun.StateCompleted || !got.Plan.Steps[0].Approved || got.Plan.Steps[0].Attempt != 1 || f.owner.calls() != 1 {
		t.Fatalf("task = %s approved=%v attempt=%d calls=%d, want COMPLETED with one run", got.State, got.Plan.Steps[0].Approved, got.Plan.Steps[0].Attempt, f.owner.calls())
	}
	again, err := f.runner.DeliverApproval(context.Background(), "task-appr", "update", digest, "user-42")
	if err != nil || again.State != agentrun.StateCompleted || f.owner.calls() != 1 {
		t.Fatalf("repeat approval = %s, %v, calls=%d, want a no-op", again.State, err, f.owner.calls())
	}
}
