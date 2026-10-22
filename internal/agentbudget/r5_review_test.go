package agentbudget

import (
	"context"
	"errors"
	"testing"
	"time"
)

func r5ExtensionPolicy() Policy {
	policy := budgetPolicy()
	policy.TenantMonthly.Steps = 30
	policy.ExtensionPolicy.MaxAdditional = Limits{Steps: 20, Tokens: 10_000, WallClock: time.Hour, SpendMicros: 10_000}
	return policy
}

func r5PauseAtTaskCeiling(t *testing.T, ledger *Ledger, taskID, step string) *PauseError {
	t.Helper()
	_, err := ledger.Reserve(context.Background(), Request{TaskID: taskID, StepID: step, Fingerprint: step, Estimate: Usage{Steps: 100, Tokens: 1, WallClock: 1, SpendMicros: 1}})
	var pause *PauseError
	if !errors.As(err, &pause) || pause.Scope != ScopeTask {
		t.Fatalf("Reserve = %v, want a task-scope pause", err)
	}
	return pause
}

func r5Revision(t *testing.T, ledger *Ledger, taskID string) uint64 {
	t.Helper()
	for _, task := range ledger.Snapshot().Tasks {
		if task.ID == taskID {
			return task.Revision
		}
	}
	t.Fatalf("no task %s", taskID)
	return 0
}

// A task that has been extended once is above the extension ceiling, and the
// ledger refuses a second extension. The pause card and the allowance used to
// offer one anyway, so the task view showed an Extend budget control whose click
// always answered "conflict".
func TestR5_ExtensionNotOfferedWhenTheLedgerWouldRefuseIt(t *testing.T) {
	ledger, _ := newBudgetTestLedger(t, r5ExtensionPolicy())
	openBudgetTask(t, ledger, "task-a", "tenant-a", "user-a", Limits{Steps: 2, Tokens: 1_000, WallClock: time.Hour, SpendMicros: 1_000})
	pause := r5PauseAtTaskCeiling(t, ledger, "task-a", "s1")
	if !pause.Card.CanAccept || pause.Card.MaxAdditional.Steps != 20 {
		t.Fatalf("first pause card = %+v, want an offer of 20 steps", pause.Card)
	}
	if allowance := ledger.ExtensionAllowance("task-a"); allowance.Steps != 20 {
		t.Fatalf("allowance = %+v", allowance)
	}
	if _, err := ledger.AcceptExtensionCAS(ExtensionRequest{TaskID: "task-a", RequestID: "x1", ExpectedRevision: r5Revision(t, ledger, "task-a"), Additional: Limits{Steps: 19}}); err != nil {
		t.Fatalf("first extension: %v", err)
	}
	second := r5PauseAtTaskCeiling(t, ledger, "task-a", "s2")
	if second.Card.CanAccept {
		t.Fatalf("the card of a task already past the extension ceiling offers %+v", second.Card.MaxAdditional)
	}
	if allowance := ledger.ExtensionAllowance("task-a"); allowance != (Limits{}) {
		t.Fatalf("allowance after the first extension = %+v, want none", allowance)
	}
	if _, err := ledger.AcceptExtensionCAS(ExtensionRequest{TaskID: "task-a", RequestID: "x2", ExpectedRevision: r5Revision(t, ledger, "task-a"), Additional: Limits{Steps: 1}}); !errors.Is(err, ErrExtensionUnavailable) {
		t.Fatalf("second extension = %v, want ErrExtensionUnavailable", err)
	}
}

// What is offered is what the ledger accepts: with the tenant's month nearly
// used, the allowance is held to what is left, so the click does not fail.
func TestR5_ExtensionAllowanceIsHeldToWhatTheTenantHasLeft(t *testing.T) {
	ledger, _ := newBudgetTestLedger(t, r5ExtensionPolicy())
	openBudgetTask(t, ledger, "other", "tenant-a", "user-b", Limits{Steps: 25, Tokens: 1_000, WallClock: time.Hour, SpendMicros: 1_000})
	used, err := ledger.Reserve(context.Background(), Request{TaskID: "other", StepID: "s1", Fingerprint: "s1", Estimate: Usage{Steps: 25, Tokens: 1, WallClock: 1, SpendMicros: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := used.Settle(Usage{Steps: 25, Tokens: 1, WallClock: 1, SpendMicros: 1}); err != nil {
		t.Fatal(err)
	}
	openBudgetTask(t, ledger, "task-a", "tenant-a", "user-a", Limits{Steps: 2, Tokens: 1_000, WallClock: time.Hour, SpendMicros: 1_000})
	pause := r5PauseAtTaskCeiling(t, ledger, "task-a", "s1")
	allowance := ledger.ExtensionAllowance("task-a")
	if allowance.Steps != 5 || pause.Card.MaxAdditional.Steps != 5 {
		t.Fatalf("allowance %+v, card %+v: want 5 steps, what the tenant's month has left", allowance, pause.Card.MaxAdditional)
	}
	if _, err := ledger.AcceptExtensionCAS(ExtensionRequest{TaskID: "task-a", RequestID: "x1", ExpectedRevision: r5Revision(t, ledger, "task-a"), Additional: allowance}); err != nil {
		t.Fatalf("the offered allowance was refused: %v", err)
	}
}

// A task that is not paused, or is paused at a shared ceiling, has no allowance.
func TestR5_NoAllowanceForATaskThatIsNotPausedAtItsOwnCeiling(t *testing.T) {
	ledger, _ := newBudgetTestLedger(t, r5ExtensionPolicy())
	openBudgetTask(t, ledger, "task-a", "tenant-a", "user-a", Limits{Steps: 2, Tokens: 1_000, WallClock: time.Hour, SpendMicros: 1_000})
	if allowance := ledger.ExtensionAllowance("task-a"); allowance != (Limits{}) {
		t.Fatalf("a running task is offered %+v", allowance)
	}
}
