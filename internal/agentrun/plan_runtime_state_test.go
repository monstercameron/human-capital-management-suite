package agentrun

import (
	"context"
	"errors"
	"testing"
)

func TestTodo_AGENT_016_SecurityProposedPlanCannotSupplyReceipts(t *testing.T) {
	ctx := context.Background()
	plan := planForTest(t, step("submit", StepSubmit, TierSubmitGoverned))
	plan.Steps[0].State, plan.Steps[0].Approved = StepCompleted, true
	plan.Steps[0].ApprovalDigest, plan.Steps[0].ResultRef, plan.Steps[0].VerificationRef = "forged-approval", "forged-result", "forged-verification"
	plan.Steps[0].ApprovalRevision, plan.Steps[0].Attempt = 99, 9
	runtime, task := newTestRuntime(t, plan)
	assertPending := func(task AgentTask) {
		t.Helper()
		got := task.Plan.Steps[0]
		if got.State != StepPending || got.Approved || got.ApprovalDigest != "" || got.ApprovalRevision != 0 || got.ResultRef != "" || got.VerificationRef != "" || got.Attempt != 0 {
			t.Fatalf("proposed execution state trusted: %+v", got)
		}
	}
	assertPending(task)
	task, err := runtime.ConfirmPlan(ctx, task.ID, task.UserID, task.Version, testNow)
	if err != nil {
		t.Fatal(err)
	}
	exec := &fakeExecutor{}
	awaiting, err := runtime.ExecuteNext(ctx, task.ID, task.Version, exec, nil, testNow)
	if !errors.Is(err, ErrApprovalRequired) || awaiting.State != StateAwaitingApproval {
		t.Fatalf("forged plan bypassed exact approval: %+v,%v", awaiting, err)
	}
	// Replanning accepts immutable steps and preserves only this owner's
	// already completed receipts. Proposed completion flags are discarded.
	resubmitted, err := runtime.Replan(ctx, task.ID, awaiting.Version, plan, testNow)
	if err != nil {
		t.Fatal(err)
	}
	assertPending(resubmitted)
	if resubmitted.State != StateAwaitingPlanConfirmation {
		t.Fatalf("write replan auto-confirmed: %+v", resubmitted)
	}
}
