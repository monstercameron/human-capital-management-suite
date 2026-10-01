package agentbudget

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/quick"
	"time"
)

func budgetPolicy() Policy {
	return Policy{
		TaskDefault:     Limits{Steps: 20, Tokens: 10_000, WallClock: time.Hour, SpendMicros: 10_000},
		UserDaily:       Limits{Steps: 100, Tokens: 100_000, WallClock: 24 * time.Hour, SpendMicros: 100_000},
		TenantMonthly:   Limits{Steps: 1_000, Tokens: 1_000_000, WallClock: 31 * 24 * time.Hour, SpendMicros: 1_000_000},
		ExtensionPolicy: ExtensionPolicy{MaxAdditional: Limits{Steps: 1_000, Tokens: 1_000_000, WallClock: 31 * 24 * time.Hour, SpendMicros: 1_000_000}},
	}
}

func newBudgetTestLedger(t *testing.T, policy Policy) (*Ledger, *time.Time) {
	t.Helper()
	now := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)
	ledger, err := NewWithClock(policy, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewWithClock: %v", err)
	}
	return ledger, &now
}

func openBudgetTask(t *testing.T, ledger *Ledger, id, tenant, user string, limit Limits) {
	t.Helper()
	if err := ledger.OpenTask(TaskSpec{ID: id, TenantID: tenant, UserID: user, Limit: limit}); err != nil {
		t.Fatalf("OpenTask: %v", err)
	}
}

func callEstimate() Usage {
	return Usage{Steps: 1, Tokens: 100, WallClock: time.Second, SpendMicros: 100}
}

func TestTodo_AGENT2_012_Property_ReservationsCannotHideIntegerOverflow(t *testing.T) {
	policy := budgetPolicy()
	policy.TaskDefault.SpendMicros = math.MaxInt64
	policy.UserDaily.SpendMicros = math.MaxInt64
	policy.TenantMonthly.SpendMicros = math.MaxInt64
	ledger, _ := newBudgetTestLedger(t, policy)
	openBudgetTask(t, ledger, "overflow", "tenant-a", "user-a", policy.TaskDefault)
	first, err := ledger.Reserve(context.Background(), Request{TaskID: "overflow", StepID: "first", Fingerprint: "first", Estimate: Usage{Steps: 1, SpendMicros: math.MaxInt64}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ledger.Reserve(context.Background(), Request{TaskID: "overflow", StepID: "second", Fingerprint: "second", Estimate: Usage{Steps: 1, SpendMicros: 1}})
	var pause *PauseError
	if !errors.As(err, &pause) || pause.Reason != PauseTaskSpend {
		t.Fatalf("overflowed reservation admitted: %v", err)
	}
	if err := first.Settle(Usage{Steps: 1, SpendMicros: math.MaxInt64}); err != nil {
		t.Fatal(err)
	}
	openBudgetTask(t, ledger, "other-task", "tenant-a", "user-a", policy.TaskDefault)
	_, err = ledger.Reserve(context.Background(), Request{TaskID: "other-task", StepID: "first", Fingerprint: "first", Estimate: Usage{Steps: 1, SpendMicros: 1}})
	if !errors.As(err, &pause) || pause.Reason != PauseUserSpend {
		t.Fatalf("overflowed daily ceiling admitted: %v", err)
	}
	openBudgetTask(t, ledger, "other-user", "tenant-a", "user-b", policy.TaskDefault)
	_, err = ledger.Reserve(context.Background(), Request{TaskID: "other-user", StepID: "first", Fingerprint: "first", Estimate: Usage{Steps: 1, SpendMicros: 1}})
	if !errors.As(err, &pause) || pause.Reason != PauseTenantSpend {
		t.Fatalf("overflowed tenant ceiling admitted: %v", err)
	}
}

// TestTodo_AGENT2_012 proves reservation-before-work, hierarchy fairness,
// typed pauses, bounded identical-failure loops, and explicit extension.
func TestTodo_AGENT2_012(t *testing.T) {
	policy := budgetPolicy()
	policy.UserDaily.Steps = 2
	ledger, _ := newBudgetTestLedger(t, policy)
	taskLimit := Limits{Steps: 4, Tokens: 1_000, WallClock: time.Hour, SpendMicros: 1_000}
	openBudgetTask(t, ledger, "task-a", "tenant-a", "user-a", taskLimit)

	first, err := ledger.Reserve(context.Background(), Request{TaskID: "task-a", StepID: "step-1", Fingerprint: "digest-1", Estimate: callEstimate()})
	if err != nil {
		t.Fatalf("first Reserve: %v", err)
	}
	if first.Attempt != 1 || first.Backoff != 0 {
		t.Fatalf("first attempt metadata = %+v", first)
	}
	if err := first.Settle(Usage{Steps: 1, Tokens: 80, WallClock: 500 * time.Millisecond, SpendMicros: 90}); err != nil {
		t.Fatalf("first Settle: %v", err)
	}

	second, err := ledger.Reserve(context.Background(), Request{TaskID: "task-a", StepID: "step-2", Fingerprint: "digest-2", Estimate: callEstimate()})
	if err != nil {
		t.Fatalf("second Reserve: %v", err)
	}
	if err := second.Settle(callEstimate()); err != nil {
		t.Fatalf("second Settle: %v", err)
	}

	openBudgetTask(t, ledger, "task-b", "tenant-a", "user-a", taskLimit)
	_, err = ledger.Reserve(context.Background(), Request{TaskID: "task-b", StepID: "step-1", Fingerprint: "digest-3", Estimate: callEstimate()})
	var userPause *PauseError
	if !errors.As(err, &userPause) || userPause.Reason != PauseUserSteps || userPause.Scope != ScopeUser {
		t.Fatalf("user ceiling error = %v, pause = %+v", err, userPause)
	}
	if userPause.Card.Reason != PauseUserSteps || userPause.Card.CanAccept {
		t.Fatalf("user ceiling card = %+v", userPause.Card)
	}

	openBudgetTask(t, ledger, "task-c", "tenant-b", "user-c", Limits{Steps: 1, Tokens: 1_000, WallClock: time.Hour, SpendMicros: 1_000})
	reservation, err := ledger.Reserve(context.Background(), Request{TaskID: "task-c", StepID: "step-1", Fingerprint: "digest-4", Estimate: callEstimate()})
	if err != nil {
		t.Fatalf("task ceiling initial Reserve: %v", err)
	}
	if err := reservation.Settle(callEstimate()); err != nil {
		t.Fatalf("task ceiling initial Settle: %v", err)
	}
	_, err = ledger.Reserve(context.Background(), Request{TaskID: "task-c", StepID: "step-2", Fingerprint: "digest-5", Estimate: callEstimate()})
	var taskPause *PauseError
	if !errors.As(err, &taskPause) || taskPause.Reason != PauseTaskSteps || !taskPause.Card.CanAccept {
		t.Fatalf("task ceiling error = %v, pause = %+v", err, taskPause)
	}
	if err := ledger.AcceptExtension(ExtensionRequest{TaskID: "task-c", RequestID: "extend-c", ExpectedRevision: 4, Additional: Limits{Steps: 1}}); err != nil {
		t.Fatalf("AcceptExtension: %v", err)
	}
	resumed, err := ledger.Reserve(context.Background(), Request{TaskID: "task-c", StepID: "step-2", Fingerprint: "digest-5", Estimate: callEstimate()})
	if err != nil {
		t.Fatalf("Reserve after extension: %v", err)
	}
	if err := resumed.Settle(callEstimate()); err != nil {
		t.Fatalf("Settle after extension: %v", err)
	}

	openBudgetTask(t, ledger, "task-loop", "tenant-c", "user-loop", taskLimit)
	for attempt := 1; attempt <= 3; attempt++ {
		res, reserveErr := ledger.Reserve(context.Background(), Request{TaskID: "task-loop", StepID: "same-step", Fingerprint: "same-failure", Estimate: callEstimate()})
		if reserveErr != nil {
			t.Fatalf("loop Reserve %d: %v", attempt, reserveErr)
		}
		failureErr := res.Fail()
		if attempt < 3 && failureErr != nil {
			t.Fatalf("unexpected loop pause %d: %v", attempt, failureErr)
		}
		if attempt == 3 {
			var loopPause *PauseError
			if !errors.As(failureErr, &loopPause) || loopPause.Reason != PauseLoopDetected {
				t.Fatalf("loop pause = %v", failureErr)
			}
		}
	}
	_, err = ledger.Reserve(context.Background(), Request{TaskID: "task-loop", StepID: "same-step", Fingerprint: "same-failure", Estimate: callEstimate()})
	if !errors.Is(err, ErrPaused) {
		t.Fatalf("paused task admitted after loop: %v", err)
	}
	if err := ledger.AcceptExtension(ExtensionRequest{TaskID: "task-loop", RequestID: "extend-loop", ExpectedRevision: 7, Additional: Limits{Steps: 1}}); !errors.Is(err, ErrExtensionUnavailable) {
		t.Fatalf("loop pause accepted a budget extension: %v", err)
	}
}

func TestTodo_AGENT2_012_InvalidPolicyAndContext(t *testing.T) {
	if _, err := New(Policy{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero policy error = %v", err)
	}
	policy := budgetPolicy()
	if _, err := NewWithClock(policy, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil clock error = %v", err)
	}
	ledger, _ := newBudgetTestLedger(t, policy)
	if err := ledger.OpenTask(TaskSpec{ID: "missing-user", TenantID: "tenant"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing identity error = %v", err)
	}
	if _, err := ledger.Reserve(context.Background(), Request{TaskID: "unknown", StepID: "step", Fingerprint: "digest", Estimate: callEstimate()}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("unknown task error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ledger.Reserve(ctx, Request{TaskID: "unknown", StepID: "step", Fingerprint: "digest", Estimate: callEstimate()}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Reserve error = %v", err)
	}
}

// TestTodo_AGENT2_012_Golden pins the audit-safe projection and proves that
// fingerprints and raw call content cannot appear in it.
func TestTodo_AGENT2_012_Golden(t *testing.T) {
	ledger, _ := newBudgetTestLedger(t, budgetPolicy())
	openBudgetTask(t, ledger, "task-golden", "tenant-golden", "user-golden", Limits{Steps: 3, Tokens: 300, WallClock: time.Hour, SpendMicros: 3_000})
	res, err := ledger.Reserve(context.Background(), Request{TaskID: "task-golden", StepID: "step", Fingerprint: "sha256:secret-input-must-not-appear", Estimate: callEstimate()})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := res.Settle(Usage{Steps: 1, Tokens: 80, WallClock: 500 * time.Millisecond, SpendMicros: 90}); err != nil {
		t.Fatalf("Settle: %v", err)
	}
	got, err := ledger.MarshalSnapshot()
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}
	want := `{"tasks":[{"id":"task-golden","tenant_id":"tenant-golden","user_id":"user-golden","revision":3,"limit":{"steps":3,"tokens":300,"wall_clock_ns":3600000000000,"spend_micros":3000},"used":{"steps":1,"tokens":80,"wall_clock_ns":500000000,"spend_micros":90},"reserved":{"steps":0,"tokens":0,"wall_clock_ns":0,"spend_micros":0},"attempts":1}],"user_period_totals":[{"steps":1,"tokens":80,"wall_clock_ns":500000000,"spend_micros":90}],"tenant_period_totals":[{"steps":1,"tokens":80,"wall_clock_ns":500000000,"spend_micros":90}]}`
	if string(got) != want {
		t.Fatalf("snapshot golden mismatch\nwant %s\n got %s", want, got)
	}
	if string(got) == "" || strings.Contains(string(got), "secret-input") {
		t.Fatalf("snapshot leaked fingerprint: %s", got)
	}
}

// TestTodo_AGENT2_012_Property proves every accepted settlement is bounded by
// its reservation and that no reservation remains after a completed call.
func TestTodo_AGENT2_012_Property(t *testing.T) {
	property := func(seed uint64) bool {
		policy := budgetPolicy()
		policy.TaskDefault = Limits{Steps: 500, Tokens: 100_000, WallClock: 100 * time.Hour, SpendMicros: 100_000}
		ledger, _ := newBudgetTestLedger(t, policy)
		openBudgetTask(t, ledger, "property", "tenant-property", "user-property", policy.TaskDefault)
		var recorded Usage
		for i := 0; i < 32; i++ {
			n := seed + uint64(i)
			estimate := Usage{Steps: 1, Tokens: 100 + int64(n%100), WallClock: time.Second + time.Duration(n%10)*time.Millisecond, SpendMicros: 100 + int64(n%100)}
			res, err := ledger.Reserve(context.Background(), Request{TaskID: "property", StepID: "step-" + strconv.Itoa(i), Fingerprint: "digest-" + strconv.Itoa(i), Estimate: estimate})
			if err != nil {
				return false
			}
			actual := Usage{Steps: 1, Tokens: estimate.Tokens - int64(n%20), WallClock: estimate.WallClock - time.Millisecond, SpendMicros: estimate.SpendMicros - int64(n%20)}
			if actual.SpendMicros < 0 || res.Settle(actual) != nil {
				return false
			}
			recorded = recorded.add(actual)
		}
		snapshot := ledger.Snapshot()
		return len(snapshot.Tasks) == 1 && snapshot.Tasks[0].Reserved == (Usage{}) && snapshot.Tasks[0].Used == recorded && snapshot.Tasks[0].Used.SpendMicros <= 32*199
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 40}); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_AGENT2_012_Race drives independent steps through one shared ledger.
// The same test is race-detector-ready on CI; local Windows validation runs it
// without -race per the repository standing rules.
func TestTodo_AGENT2_012_Race(t *testing.T) {
	policy := budgetPolicy()
	policy.TaskDefault.Steps = 200
	policy.UserDaily.Steps = 200
	policy.TenantMonthly.Steps = 200
	ledger, _ := newBudgetTestLedger(t, policy)
	openBudgetTask(t, ledger, "race", "tenant-race", "user-race", policy.TaskDefault)
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := ledger.Reserve(context.Background(), Request{TaskID: "race", StepID: "step-" + strconv.Itoa(i), Fingerprint: "digest-" + strconv.Itoa(i), Estimate: callEstimate()})
			if err != nil {
				errs <- err
				return
			}
			if err := res.Settle(callEstimate()); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent reservation failed: %v", err)
	}
	if got := ledger.Snapshot().Tasks[0].Used.Steps; got != 100 {
		t.Fatalf("concurrent steps = %d, want 100", got)
	}
}

func TestTodo_AGENT2_012_ReservationBoundsAndIdempotence(t *testing.T) {
	ledger, _ := newBudgetTestLedger(t, budgetPolicy())
	openBudgetTask(t, ledger, "bounds", "tenant-bounds", "user-bounds", budgetPolicy().TaskDefault)
	res, err := ledger.Reserve(context.Background(), Request{TaskID: "bounds", StepID: "step", Fingerprint: "digest", Estimate: callEstimate()})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Settle(Usage{Steps: 1, Tokens: 101, WallClock: time.Second, SpendMicros: 100}); !errors.Is(err, ErrReservationExceeded) {
		t.Fatalf("over-settlement error = %v", err)
	}
	if err := res.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := res.Release(); !errors.Is(err, ErrReservationClosed) {
		t.Fatalf("double release = %v", err)
	}
	if got := ledger.Snapshot().Tasks[0].Used; got != (Usage{}) {
		t.Fatalf("recorded usage after release = %+v", got)
	}
}

func TestTodo_AGENT2_012_PeriodBoundaryKeepsReservationBucket(t *testing.T) {
	policy := budgetPolicy()
	ledger, now := newBudgetTestLedger(t, policy)
	openBudgetTask(t, ledger, "boundary", "tenant-boundary", "user-boundary", policy.TaskDefault)
	res, err := ledger.Reserve(context.Background(), Request{TaskID: "boundary", StepID: "step", Fingerprint: "digest", Estimate: callEstimate()})
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(32 * 24 * time.Hour)
	if err := res.Settle(callEstimate()); err != nil {
		t.Fatal(err)
	}
	resumed, err := ledger.Reserve(context.Background(), Request{TaskID: "boundary", StepID: "step-2", Fingerprint: "digest-2", Estimate: callEstimate()})
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.Release(); err != nil {
		t.Fatal(err)
	}
	snapshot := ledger.Snapshot()
	if len(snapshot.UserPeriodTotals) != 2 || len(snapshot.TenantPeriodTotals) != 2 {
		t.Fatalf("period totals collapsed across rollover: %+v", snapshot)
	}
	for _, total := range snapshot.UserPeriodTotals {
		if total.Steps != 0 && total.Steps != 1 {
			t.Fatalf("unexpected user period total: %+v", total)
		}
	}
}

func TestTodo_AGENT2_012_ExtensionCASReplayAndPolicy(t *testing.T) {
	policy := budgetPolicy()
	policy.ExtensionPolicy.MaxAdditional = Limits{Steps: 2, Tokens: 200, WallClock: time.Minute, SpendMicros: 200}
	ledger, _ := newBudgetTestLedger(t, policy)
	openBudgetTask(t, ledger, "extension", "tenant-extension", "user-extension", Limits{Steps: 1, Tokens: 100, WallClock: time.Minute, SpendMicros: 100})
	res, err := ledger.Reserve(context.Background(), Request{TaskID: "extension", StepID: "s1", Fingerprint: "f1", Estimate: callEstimate()})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Settle(callEstimate()); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Reserve(context.Background(), Request{TaskID: "extension", StepID: "s2", Fingerprint: "f2", Estimate: callEstimate()}); !errors.Is(err, ErrPaused) {
		t.Fatalf("ceiling reserve = %v", err)
	}
	revision := ledger.Snapshot().Tasks[0].Revision
	first, err := ledger.AcceptExtensionCAS(ExtensionRequest{TaskID: "extension", RequestID: "request-1", ExpectedRevision: revision, Additional: Limits{Steps: 1}})
	if err != nil || first.Revision != revision+1 || first.Limit.Steps != 2 {
		t.Fatalf("first extension = %+v, %v", first, err)
	}
	replay, err := ledger.AcceptExtensionCAS(ExtensionRequest{TaskID: "extension", RequestID: "request-1", ExpectedRevision: revision, Additional: Limits{Steps: 1}})
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || ledger.Snapshot().Tasks[0].Revision != first.Revision {
		t.Fatalf("replay = %+v, %v", replay, err)
	}
	if _, err := ledger.AcceptExtensionCAS(ExtensionRequest{TaskID: "extension", RequestID: "request-1", ExpectedRevision: revision, Additional: Limits{Steps: 2}}); !errors.Is(err, ErrExtensionConflict) {
		t.Fatalf("changed replay = %v, want conflict", err)
	}

	disabledPolicy := budgetPolicy()
	disabledPolicy.ExtensionPolicy = ExtensionPolicy{}
	disabled, _ := newBudgetTestLedger(t, disabledPolicy)
	if disabled.ExtensionEnabled() {
		t.Fatal("zero extension policy was advertised as enabled")
	}
	openBudgetTask(t, disabled, "disabled", "tenant-disabled", "user-disabled", Limits{Steps: 1, Tokens: 100, WallClock: time.Minute, SpendMicros: 100})
	r, _ := disabled.Reserve(context.Background(), Request{TaskID: "disabled", StepID: "s1", Fingerprint: "f", Estimate: callEstimate()})
	_ = r.Settle(callEstimate())
	_, reserveErr := disabled.Reserve(context.Background(), Request{TaskID: "disabled", StepID: "s2", Fingerprint: "f2", Estimate: callEstimate()})
	var disabledPause *PauseError
	if !errors.As(reserveErr, &disabledPause) || !errors.Is(reserveErr, ErrPaused) {
		t.Fatalf("disabled extension pause = %v", reserveErr)
	}
	if disabledPause.Card.CanAccept {
		t.Fatalf("disabled extension card advertised acceptance: %+v", disabledPause.Card)
	}
	if err := disabled.AcceptExtension(ExtensionRequest{TaskID: "disabled", RequestID: "request-disabled", ExpectedRevision: disabled.Snapshot().Tasks[0].Revision, Additional: Limits{Steps: 1}}); !errors.Is(err, ErrExtensionUnavailable) {
		t.Fatalf("default policy extension = %v, want disabled", err)
	}
}

func TestTodo_AGENT2_012_ExtensionRejectsActiveReservationAndRaces(t *testing.T) {
	policy := budgetPolicy()
	policy.ExtensionPolicy.MaxAdditional = Limits{Steps: 2, Tokens: 200, WallClock: time.Minute, SpendMicros: 200}
	ledger, _ := newBudgetTestLedger(t, policy)
	openBudgetTask(t, ledger, "reserved", "tenant-reserved", "user-reserved", Limits{Steps: 1, Tokens: 100, WallClock: time.Minute, SpendMicros: 100})
	active, err := ledger.Reserve(context.Background(), Request{TaskID: "reserved", StepID: "s1", Fingerprint: "f1", Estimate: callEstimate()})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = ledger.Reserve(context.Background(), Request{TaskID: "reserved", StepID: "s2", Fingerprint: "f2", Estimate: callEstimate()})
	revision := ledger.Snapshot().Tasks[0].Revision
	if err := ledger.AcceptExtension(ExtensionRequest{TaskID: "reserved", RequestID: "request-reserved", ExpectedRevision: revision, Additional: Limits{Steps: 1}}); !errors.Is(err, ErrExtensionReserved) {
		t.Fatalf("active reservation extension = %v", err)
	}
	if err := active.Release(); err != nil {
		t.Fatal(err)
	}
	revision = ledger.Snapshot().Tasks[0].Revision
	var wg sync.WaitGroup
	results := make(chan ExtensionResult, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := ledger.AcceptExtensionCAS(ExtensionRequest{TaskID: "reserved", RequestID: "request-race", ExpectedRevision: revision, Additional: Limits{Steps: 1}})
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	wins := 0
	for err := range errs {
		if err != nil {
			t.Fatalf("raced extension = %v", err)
		}
		wins++
	}
	if wins != 8 || ledger.Snapshot().Tasks[0].Limit.Steps != 2 || ledger.Snapshot().Tasks[0].Revision != revision+1 {
		t.Fatalf("raced extension state = wins %d snapshot %+v", wins, ledger.Snapshot().Tasks[0])
	}
}
