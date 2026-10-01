package agentrun

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTodo_AGENT2_025_TaskEventsTrackRuntimeReplansAndApproval(t *testing.T) {
	plan := planForTest(t, step("read", StepRead, TierRead))
	runtime, task := newTestRuntime(t, plan)
	store := runtime.store.(*MemoryStore)
	ctx := context.Background()

	task, err := runtime.ConfirmPlan(ctx, task.ID, task.UserID, task.Version, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	changed := planForTest(t, step("submit", StepSubmit, TierSubmitGoverned))
	task, err = runtime.Replan(ctx, task.ID, task.Version, changed, testNow.Add(2*time.Minute))
	if err != nil || task.State != StateAwaitingPlanConfirmation {
		t.Fatalf("Replan = state %s, error %v", task.State, err)
	}
	task, err = runtime.ConfirmPlan(ctx, task.ID, task.UserID, task.Version, testNow.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	awaiting, err := runtime.ExecuteNext(ctx, task.ID, task.Version, &fakeExecutor{}, nil, testNow.Add(4*time.Minute))
	if !errors.Is(err, ErrApprovalRequired) || awaiting.State != StateAwaitingApproval {
		t.Fatalf("ExecuteNext = state %s, error %v", awaiting.State, err)
	}
	approved, err := runtime.ApproveStep(ctx, task.ID, "submit", awaiting.Plan.Steps[0].ApprovalDigest, awaiting.Version, testNow.Add(5*time.Minute))
	if err != nil || approved.State != StateRunning {
		t.Fatalf("ApproveStep = state %s, error %v", approved.State, err)
	}

	events, err := store.ListEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 6 {
		t.Fatalf("event count = %d, want 6: %+v", len(events), events)
	}
	for i, event := range events {
		if event.Sequence != uint64(i+1) || event.TaskID != task.ID {
			t.Fatalf("event %d identity = %+v", i, event)
		}
	}
	if events[0].Type != TaskEventPlanRevision || events[0].PlanRevision != 1 || events[0].Outcome != "AWAITING_CONFIRMATION" ||
		events[1].Type != TaskEventPlanConfirmation || events[1].Outcome != "CONFIRMED" || events[1].ActorID != task.UserID ||
		events[2].Type != TaskEventPlanRevision || events[2].PlanRevision != 2 || events[2].Outcome != "AWAITING_CONFIRMATION" ||
		events[3].Type != TaskEventPlanConfirmation || events[3].PlanRevision != 2 ||
		events[4].Type != TaskEventApprovalRequest || events[4].Outcome != "PENDING" || events[4].ApprovalDigest != events[5].ApprovalDigest ||
		events[5].Type != TaskEventApprovalOutcome || events[5].Outcome != "APPROVED" {
		t.Fatalf("observed plan and approval events = %+v", events)
	}
}

func TestTodo_AGENT2_025_TaskEventSaveIsAtomicAndReadCopyIsSafe(t *testing.T) {
	runtime, task := newTestRuntime(t, planForTest(t, step("read", StepRead, TierRead)))
	store := runtime.store.(*MemoryStore)
	ctx := context.Background()
	snapshot, err := SnapshotPlan(task.Plan)
	if err != nil {
		t.Fatal(err)
	}
	event := TaskEvent{TaskID: task.ID, Type: TaskEventPlanConfirmation, PlanRevision: task.Plan.Revision,
		PlanDigest: task.Plan.Digest, PlanSnapshot: &snapshot, Outcome: "CONFIRMED", OccurredAt: testNow}
	stale := task
	if err := store.SaveWithEvents(ctx, stale, task.Version-1, event); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale save with event = %v, want conflict", err)
	}
	events, err := store.ListEvents(ctx, task.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("events after failed save = %+v, %v", events, err)
	}
	events[0].Outcome = "MUTATED"
	events[0].PlanSnapshot.Steps[0].ID = "MUTATED"
	stored, err := store.ListEvents(ctx, task.ID)
	if err != nil || stored[0].Outcome != "AWAITING_CONFIRMATION" || stored[0].PlanSnapshot.Steps[0].ID == "MUTATED" {
		t.Fatalf("caller changed stored event: %+v, %v", stored, err)
	}
	tampered := *stored[0].PlanSnapshot
	tampered.Steps = append([]PlanStepIdentity(nil), stored[0].PlanSnapshot.Steps...)
	tampered.Steps[0].Tier = TierExternalWrite
	if err := tampered.Verify(); err == nil {
		t.Fatal("historical plan snapshot accepted a tier change without a matching digest")
	}
}

func TestTodo_AGENT2_025_HistoricalPlanAndExecutionEvidence(t *testing.T) {
	verify := step("verify-owner", StepVerify, TierRead)
	verify.Inputs = []InputRef{{Name: "owner-state", Ref: "owner:worker-1"}}
	runtime, task := newTestRuntime(t, planForTest(t, verify, step("read-more", StepRead, TierRead)))
	ctx := context.Background()
	task, err := runtime.ConfirmPlan(ctx, task.ID, task.UserID, task.Version, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	verifier := &fakeVerifier{}
	task, err = runtime.ExecuteNext(ctx, task.ID, task.Version, &fakeExecutor{}, verifier, testNow.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("execute VERIFY: %v", err)
	}
	retired, err := NewPlan([]PlanStep{{ID: "submit", Type: StepSubmit, SkillID: "skill.read", SkillVersion: 1,
		ExpectedOutput: "submitted", Tier: TierSubmitGoverned}})
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.Replan(ctx, task.ID, task.Version, retired, testNow.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.ConfirmPlan(ctx, task.ID, task.UserID, task.Version, testNow.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	task, err = runtime.ExecuteNext(ctx, task.ID, task.Version, &fakeExecutor{}, nil, testNow.Add(5*time.Minute))
	if !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("request approval error = %v", err)
	}
	task, err = runtime.ApproveStep(ctx, task.ID, "submit", task.Plan.Steps[0].ApprovalDigest, task.Version, testNow.Add(6*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ExecuteNext(ctx, task.ID, task.Version, &fakeExecutor{}, nil, testNow.Add(7*time.Minute)); err != nil {
		t.Fatalf("execute approved step: %v", err)
	}
	events, err := runtime.TaskEvents(ctx, task.ID)
	if err != nil {
		t.Fatalf("TaskEvents: %v", err)
	}
	var retiredVerify, stepRun, verifyResult, approval, effect bool
	for _, event := range events {
		switch event.Type {
		case TaskEventPlanRevision:
			if event.PlanRevision == 1 {
				if event.PlanSnapshot == nil || event.PlanSnapshot.Verify() != nil || len(event.PlanSnapshot.Steps) != 2 || event.PlanSnapshot.Steps[0].Type != StepVerify {
					t.Fatalf("retired plan snapshot = %+v", event.PlanSnapshot)
				}
				retiredVerify = true
			}
		case TaskEventStepExecution:
			stepRun = stepRun || event.StepID == "verify-owner" && event.Outcome == "SUCCEEDED"
		case TaskEventStepVerification:
			verifyResult = verifyResult || event.StepID == "verify-owner" && event.Outcome == "VERIFIED"
		case TaskEventApprovalRequest:
			approval = approval || event.StepID == "submit" && event.StepType == StepSubmit && event.Tier == TierSubmitGoverned && event.PlanRevision == 2
		case TaskEventStepEffect:
			effect = effect || event.StepID == "submit" && event.Outcome == "SUCCEEDED" && event.ApprovalDigest != ""
		}
	}
	if !retiredVerify || !stepRun || !verifyResult || !approval || !effect {
		t.Fatalf("historical/effect evidence flags: plan=%t execution=%t verify=%t approval=%t effect=%t events=%+v", retiredVerify, stepRun, verifyResult, approval, effect, events)
	}
}
