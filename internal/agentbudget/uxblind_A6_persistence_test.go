package agentbudget

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type recordingPersister struct {
	seen []Transition
	fail func(Transition) error
}

func (r *recordingPersister) Persist(t Transition) error {
	if r.fail != nil {
		if err := r.fail(t); err != nil {
			return err
		}
	}
	r.seen = append(r.seen, t)
	return nil
}

func (r *recordingPersister) last(t *testing.T) Transition {
	t.Helper()
	if len(r.seen) == 0 {
		t.Fatal("no transition was persisted")
	}
	return r.seen[len(r.seen)-1]
}

func persistedLedger(t *testing.T, policy Policy, p Persister) (*Ledger, *time.Time) {
	t.Helper()
	now := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)
	ledger, err := NewWithPersistence(policy, func() time.Time { return now }, p)
	if err != nil {
		t.Fatalf("NewWithPersistence: %v", err)
	}
	return ledger, &now
}

func reserveFor(t *testing.T, l *Ledger, task, step, fp string) *Reservation {
	t.Helper()
	r, err := l.Reserve(context.Background(), Request{TaskID: task, StepID: step, Fingerprint: fp, Estimate: callEstimate()})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	return r
}

// TestTodo_AGENT2_012_PersisterSeesEveryTransition checks what the ledger
// hands the persister at each transition, including that the reserved amount
// never reaches it.
func TestTodo_AGENT2_012_PersisterSeesEveryTransition(t *testing.T) {
	rec := &recordingPersister{}
	policy := budgetPolicy()
	policy.MaxRetries, policy.LoopThreshold = 5, 2
	ledger, _ := persistedLedger(t, policy, rec)
	limit := Limits{Steps: 2, Tokens: 1_000, WallClock: time.Hour, SpendMicros: 1_000}
	openBudgetTask(t, ledger, "task-a", "tenant-a", "user-a", limit)
	if got := rec.last(t); got.Kind != TransitionOpenTask || got.Task.Spec.ID != "task-a" || got.Task.Spec.Limit != limit || got.At.IsZero() {
		t.Fatalf("OpenTask transition = %+v", got)
	}

	first := reserveFor(t, ledger, "task-a", "step-1", "fp-1")
	got := rec.last(t)
	if got.Kind != TransitionReserve || got.Task.Attempts["step-1"] != 1 || got.Task.Used.any() || got.StepID != "step-1" || len(got.Periods) != 0 {
		t.Fatalf("Reserve transition = %+v", got)
	}
	actual := Usage{Steps: 1, Tokens: 40, WallClock: time.Second, SpendMicros: 60}
	if err := first.Settle(actual); err != nil {
		t.Fatal(err)
	}
	got = rec.last(t)
	if got.Kind != TransitionSettle || got.Task.Used != actual || got.Actual != actual || got.ReservationID != first.ID || len(got.Periods) != 2 {
		t.Fatalf("Settle transition = %+v", got)
	}
	user, tenant := got.Periods[0], got.Periods[1]
	if user.Scope != ScopeUser || user.Key != "user-a|2026-01-15" || user.Used != actual || !user.Start.Equal(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("user period = %+v", user)
	}
	if tenant.Scope != ScopeTenant || tenant.Key != "tenant-a|2026-01" || tenant.Used != actual || !tenant.Start.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("tenant period = %+v", tenant)
	}

	second := reserveFor(t, ledger, "task-a", "step-2", "fp-2")
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
	if got := rec.last(t); got.Kind != TransitionRelease || got.ReservationID != second.ID {
		t.Fatalf("Release transition = %+v", got)
	}
	third := reserveFor(t, ledger, "task-a", "step-3", "fp-3")
	if err := third.Fail(); err != nil {
		t.Fatal(err)
	}
	got = rec.last(t)
	if got.Kind != TransitionFail || got.Task.Failures["step-3\x00fp-3"] != 1 || got.Task.Paused != nil {
		t.Fatalf("Fail transition = %+v", got)
	}

	// The task ceiling (2 steps) is now reached: refusal is persisted as a PAUSE.
	if _, err := ledger.Reserve(context.Background(), Request{TaskID: "task-a", StepID: "step-4", Fingerprint: "fp-4", Estimate: Usage{Steps: 2, Tokens: 1, WallClock: 1, SpendMicros: 1}}); !errors.Is(err, ErrPaused) {
		t.Fatalf("over-ceiling Reserve = %v", err)
	}
	got = rec.last(t)
	if got.Kind != TransitionPause || got.Task.Paused == nil || got.Task.Paused.Reason != PauseTaskSteps {
		t.Fatalf("Pause transition = %+v", got)
	}
	if err := ledger.AcceptExtension(ExtensionRequest{TaskID: "task-a", RequestID: "extend-a", ExpectedRevision: got.Task.Revision, Additional: Limits{Steps: 3}}); err != nil {
		t.Fatal(err)
	}
	got = rec.last(t)
	if got.Kind != TransitionExtension || got.Task.Paused != nil || got.Task.Spec.Limit.Steps != 5 || got.Extension.Steps != 3 {
		t.Fatalf("Extension transition = %+v", got)
	}
}

// TestTodo_AGENT2_012_PersistFailureRollsBack proves every transition fails
// closed: a persister error is returned as ErrPersistence and leaves the
// in-memory ledger exactly as it was, so the same call can be retried.
func TestTodo_AGENT2_012_PersistFailureRollsBack(t *testing.T) {
	boom := errors.New("disk full")
	failing := false
	rec := &recordingPersister{fail: func(Transition) error {
		if failing {
			return boom
		}
		return nil
	}}
	policy := budgetPolicy()
	policy.MaxRetries = 1
	ledger, _ := persistedLedger(t, policy, rec)
	limit := Limits{Steps: 2, Tokens: 1_000, WallClock: time.Hour, SpendMicros: 1_000}

	failing = true
	if err := ledger.OpenTask(TaskSpec{ID: "task-a", TenantID: "tenant-a", UserID: "user-a", Limit: limit}); !errors.Is(err, ErrPersistence) || !errors.Is(err, boom) {
		t.Fatalf("OpenTask persist failure = %v", err)
	}
	if len(ledger.Snapshot().Tasks) != 0 {
		t.Fatal("task exists after failed OpenTask")
	}
	failing = false
	openBudgetTask(t, ledger, "task-a", "tenant-a", "user-a", limit)

	failing = true
	if _, err := ledger.Reserve(context.Background(), Request{TaskID: "task-a", StepID: "s1", Fingerprint: "f", Estimate: callEstimate()}); !errors.Is(err, ErrPersistence) {
		t.Fatalf("Reserve persist failure = %v (no model work may start)", err)
	}
	if snap := ledger.Snapshot(); snap.Tasks[0].Reserved.any() || snap.Tasks[0].Attempts != 0 || snap.UserPeriodTotals[0].any() {
		t.Fatalf("failed Reserve left state: %+v", snap)
	}
	failing = false
	res := reserveFor(t, ledger, "task-a", "s1", "f")
	if res.Attempt != 1 || res.ID != "reservation-00000001" {
		t.Fatalf("retry after rolled-back Reserve = attempt %d id %s", res.Attempt, res.ID)
	}

	usage := Usage{Steps: 1, Tokens: 10, WallClock: time.Second, SpendMicros: 5}
	failing = true
	if err := res.Settle(usage); !errors.Is(err, ErrPersistence) {
		t.Fatalf("Settle persist failure = %v", err)
	}
	snap := ledger.Snapshot()
	if snap.Tasks[0].Used.any() || snap.Tasks[0].Reserved != callEstimate() {
		t.Fatalf("failed Settle changed state: %+v", snap.Tasks[0])
	}
	if err := res.Release(); !errors.Is(err, ErrPersistence) {
		t.Fatalf("Release persist failure = %v", err)
	}
	if err := res.Fail(); !errors.Is(err, ErrPersistence) {
		t.Fatalf("Fail persist failure = %v", err)
	}
	if snap := ledger.Snapshot(); snap.Tasks[0].Reserved != callEstimate() || snap.Tasks[0].Used.any() {
		t.Fatalf("failed Release/Fail changed state: %+v", snap.Tasks[0])
	}
	failing = false
	if err := res.Settle(usage); err != nil {
		t.Fatalf("Settle retry after rollback: %v", err)
	}
	if err := res.Settle(usage); !errors.Is(err, ErrReservationClosed) {
		t.Fatalf("second Settle = %v", err)
	}
	if snap := ledger.Snapshot(); snap.Tasks[0].Used != usage || snap.Tasks[0].Reserved.any() {
		t.Fatalf("settled state = %+v", snap.Tasks[0])
	}

	// A failed Fail must not count the failure or pause the task.
	again := reserveFor(t, ledger, "task-a", "s2", "g")
	failing = true
	if err := again.Fail(); !errors.Is(err, ErrPersistence) {
		t.Fatal(err)
	}
	failing = false
	if err := again.Fail(); err != nil {
		t.Fatalf("Fail retry: %v", err)
	}

	// A refused extension write leaves the task paused with the same card.
	if _, err := ledger.Reserve(context.Background(), Request{TaskID: "task-a", StepID: "s3", Fingerprint: "h", Estimate: Usage{Steps: 5, Tokens: 1, WallClock: 1, SpendMicros: 1}}); !errors.Is(err, ErrPaused) {
		t.Fatalf("over-ceiling Reserve = %v", err)
	}
	failing = true
	if err := ledger.AcceptExtension(ExtensionRequest{TaskID: "task-a", RequestID: "extend-failure", ExpectedRevision: ledger.Snapshot().Tasks[0].Revision, Additional: Limits{Steps: 5}}); !errors.Is(err, ErrPersistence) {
		t.Fatalf("AcceptExtension persist failure = %v", err)
	}
	failing = false
	if snap := ledger.Snapshot(); snap.Tasks[0].Paused != PauseTaskSteps || snap.Tasks[0].Limit.Steps != 2 {
		t.Fatalf("failed extension changed state: %+v", snap.Tasks[0])
	}
	// A lost PAUSE write keeps the pause in memory and still denies.
	openBudgetTask(t, ledger, "task-b", "tenant-a", "user-a", limit)
	failing = true
	var pause *PauseError
	if _, err := ledger.Reserve(context.Background(), Request{TaskID: "task-b", StepID: "s4", Fingerprint: "i", Estimate: Usage{Steps: 9, Tokens: 1, WallClock: 1, SpendMicros: 1}}); !errors.As(err, &pause) || pause.Reason != PauseTaskSteps {
		t.Fatalf("Reserve with a lost PAUSE write = %v, want the pause", err)
	}
	failing = false
	for _, task := range ledger.Snapshot().Tasks {
		if task.ID == "task-b" && task.Paused != PauseTaskSteps {
			t.Fatalf("lost PAUSE write dropped the in-memory pause: %+v", task)
		}
	}
}

func TestTodo_AGENT2_012_RetryPausePersistsAndSurvivesFailure(t *testing.T) {
	rec := &recordingPersister{}
	policy := budgetPolicy()
	policy.MaxRetries, policy.LoopThreshold = 1, 5
	ledger, _ := persistedLedger(t, policy, rec)
	openBudgetTask(t, ledger, "task-a", "tenant-a", "user-a", Limits{})
	for i := 0; i < 2; i++ {
		res := reserveFor(t, ledger, "task-a", "s1", "fp"+string(rune('a'+i)))
		err := res.Fail()
		if i == 0 && err != nil {
			t.Fatalf("first Fail = %v", err)
		}
		if i == 1 {
			var pause *PauseError
			if !errors.As(err, &pause) || pause.Reason != PauseRetryLimit {
				t.Fatalf("second Fail = %v, want retry-limit pause", err)
			}
		}
	}
	got := rec.last(t)
	if got.Kind != TransitionFail || got.Task.Paused == nil || got.Task.Paused.Reason != PauseRetryLimit {
		t.Fatalf("retry-limit Fail transition = %+v", got)
	}
	// Refused Reserve while paused does not persist again.
	before := len(rec.seen)
	if _, err := ledger.Reserve(context.Background(), Request{TaskID: "task-a", StepID: "s1", Fingerprint: "z", Estimate: callEstimate()}); !errors.Is(err, ErrPaused) {
		t.Fatal(err)
	}
	if len(rec.seen) != before {
		t.Fatalf("sticky pause re-persisted: %d -> %d", before, len(rec.seen))
	}

	// Loop detection: identical failures pause with the loop reason, persisted on Reserve refusal.
	policy2 := budgetPolicy()
	policy2.LoopThreshold, policy2.MaxRetries = 2, 9
	rec2 := &recordingPersister{}
	ledger2, _ := persistedLedger(t, policy2, rec2)
	openBudgetTask(t, ledger2, "task-b", "tenant-a", "user-a", Limits{})
	for i := 0; i < 2; i++ {
		_ = reserveFor(t, ledger2, "task-b", "s1", "same").Fail()
	}
	if got := rec2.last(t); got.Task.Paused == nil || got.Task.Paused.Reason != PauseLoopDetected {
		t.Fatalf("loop Fail transition = %+v", got)
	}
}

func TestTodo_AGENT2_012_RestoreRebuildsLedger(t *testing.T) {
	rec := &recordingPersister{}
	ledger, now := persistedLedger(t, budgetPolicy(), rec)
	limit := Limits{Steps: 3, Tokens: 1_000, WallClock: time.Hour, SpendMicros: 1_000}
	openBudgetTask(t, ledger, "task-a", "tenant-a", "user-a", limit)
	r1 := reserveFor(t, ledger, "task-a", "s1", "f1")
	if err := r1.Settle(callEstimate()); err != nil {
		t.Fatal(err)
	}
	_ = reserveFor(t, ledger, "task-a", "s2", "f2") // in flight: must not survive a restart
	_, _ = ledger.Reserve(context.Background(), Request{TaskID: "task-a", StepID: "s3", Fingerprint: "f3", Estimate: Usage{Steps: 9, Tokens: 1, WallClock: 1, SpendMicros: 1}})

	// Assemble the restored state from what the persister saw last.
	var state RestoredState
	byTask := map[string]DurableTask{}
	periods := map[string]DurablePeriod{}
	for _, tr := range rec.seen {
		byTask[tr.Task.Spec.ID] = tr.Task
		for _, p := range tr.Periods {
			periods[string(p.Scope)+p.Key] = p
		}
	}
	for _, tk := range byTask {
		state.Tasks = append(state.Tasks, tk)
	}
	for _, p := range periods {
		state.Periods = append(state.Periods, p)
	}

	clock := *now
	restored, err := NewWithClock(budgetPolicy(), func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Restore(state); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	snap := restored.Snapshot()
	if len(snap.Tasks) != 1 || snap.Tasks[0].Used != callEstimate() || snap.Tasks[0].Reserved.any() || snap.Tasks[0].Paused != PauseTaskSteps || snap.Tasks[0].Attempts != 2 {
		t.Fatalf("restored task = %+v", snap.Tasks)
	}
	if !reflect.DeepEqual(snap.UserPeriodTotals, []Usage{callEstimate()}) || !reflect.DeepEqual(snap.TenantPeriodTotals, []Usage{callEstimate()}) {
		t.Fatalf("restored periods = %+v / %+v", snap.UserPeriodTotals, snap.TenantPeriodTotals)
	}
	// The paused card survives verbatim and the extension still works.
	_, err = restored.Reserve(context.Background(), Request{TaskID: "task-a", StepID: "s9", Fingerprint: "x", Estimate: callEstimate()})
	var pause *PauseError
	if !errors.As(err, &pause) || pause.Card.Reason != PauseTaskSteps {
		t.Fatalf("restored paused Reserve = %v", err)
	}
	if err := restored.AcceptExtension(ExtensionRequest{TaskID: "task-a", RequestID: "extend-restored", ExpectedRevision: snap.Tasks[0].Revision, Additional: Limits{Steps: 5}}); err != nil {
		t.Fatalf("restored extension: %v", err)
	}
	if _, err := restored.Reserve(context.Background(), Request{TaskID: "task-a", StepID: "s9", Fingerprint: "x", Estimate: callEstimate()}); err != nil {
		t.Fatalf("Reserve after restored extension: %v", err)
	}

	// Restore is all-or-nothing and refuses bad rows.
	empty, _ := NewWithClock(budgetPolicy(), func() time.Time { return clock })
	good := state.Tasks[0]
	bad := good
	bad.Spec.ID = "task-bad"
	bad.Spec.Limit = Limits{}
	for name, s := range map[string]RestoredState{
		"invalid limit":   {Tasks: []DurableTask{good, bad}},
		"missing ids":     {Tasks: []DurableTask{{Spec: TaskSpec{ID: "x", Limit: limit}}}},
		"duplicate":       {Tasks: []DurableTask{good, good}},
		"bad scope":       {Periods: []DurablePeriod{{Scope: ScopeTask, Key: "k"}}},
		"empty key":       {Periods: []DurablePeriod{{Scope: ScopeUser}}},
		"negative period": {Periods: []DurablePeriod{{Scope: ScopeUser, Key: "k", Used: Usage{Steps: -1}}}},
	} {
		if err := empty.Restore(s); !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrTaskExists) {
			t.Fatalf("%s: Restore = %v", name, err)
		}
		if len(empty.Snapshot().Tasks) != 0 {
			t.Fatalf("%s: refused Restore applied rows", name)
		}
	}
	if err := restored.Restore(RestoredState{Tasks: []DurableTask{good}}); !errors.Is(err, ErrTaskExists) {
		t.Fatalf("Restore over a live task = %v", err)
	}
	var nilLedger *Ledger
	if err := nilLedger.Restore(RestoredState{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil Restore = %v", err)
	}
	if _, err := NewWithPersistence(budgetPolicy(), nil, rec); !errors.Is(err, ErrInvalid) {
		t.Fatalf("NewWithPersistence nil clock = %v", err)
	}
}

func TestTodo_AGENT2_012_PeriodStartParsing(t *testing.T) {
	if got := periodStart(ScopeUser, "u|with|pipes|2026-02-03"); !got.Equal(time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("user start = %v", got)
	}
	if got := periodStart(ScopeTenant, "t|2026-12"); !got.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("tenant start = %v", got)
	}
	if got := periodStart(ScopeUser, "not-a-period"); !got.IsZero() {
		t.Fatalf("unparseable key start = %v, want zero", got)
	}
}
