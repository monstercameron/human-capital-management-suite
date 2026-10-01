package agentrunstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

func TestTodo_AGENT2_025_PersistedTaskEventsAreTenantScopedAndImmutable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	store := e.tenantStore(t, "run-one")
	runtime, task := newRuntimeTask(t, store, "run-one", "event-task")
	confirmed, err := runtime.ConfirmPlan(ctx, task.ID, task.UserID, task.Version, t0.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	changed, err := agentrun.NewPlan([]agentrun.PlanStep{{ID: "write", Type: agentrun.StepSubmit, SkillID: "skill.write", SkillVersion: 1,
		ExpectedOutput: "submitted", Tier: agentrun.TierSubmitGoverned}})
	if err != nil {
		t.Fatal(err)
	}
	awaitingPlan, err := runtime.Replan(ctx, confirmed.ID, confirmed.Version, changed, t0.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	awaitingPlan, err = runtime.ConfirmPlan(ctx, awaitingPlan.ID, awaitingPlan.UserID, awaitingPlan.Version, t0.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	awaitingApproval, err := runtime.ExecuteNext(ctx, awaitingPlan.ID, awaitingPlan.Version, &eventTestExecutor{}, nil, t0.Add(4*time.Minute))
	if !errors.Is(err, agentrun.ErrApprovalRequired) {
		t.Fatalf("ExecuteNext error = %v", err)
	}
	approved, err := runtime.ApproveStep(ctx, awaitingApproval.ID, "write", awaitingApproval.Plan.Steps[0].ApprovalDigest, awaitingApproval.Version, t0.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.ExecuteNext(ctx, approved.ID, approved.Version, &eventTestExecutor{}, nil, t0.Add(6*time.Minute)); err != nil {
		t.Fatalf("execute approved effect: %v", err)
	}
	events, err := store.ListEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 8 || events[0].Sequence != 1 || events[2].PlanRevision != 2 || events[2].PlanSnapshot == nil ||
		events[2].PlanSnapshot.Verify() != nil || events[2].PlanSnapshot.Steps[0].Tier != agentrun.TierSubmitGoverned ||
		events[4].Type != agentrun.TaskEventApprovalRequest || events[5].Outcome != "APPROVED" ||
		events[6].Type != agentrun.TaskEventStepExecution || events[7].Type != agentrun.TaskEventStepEffect || events[7].Outcome != "SUCCEEDED" {
		t.Fatalf("persisted events = %+v", events)
	}
	otherTenant := e.tenantStore(t, "run-two")
	if _, err := otherTenant.ListEvents(ctx, task.ID); !errors.Is(err, agentrun.ErrNotFound) {
		t.Fatalf("cross-tenant ListEvents error = %v", err)
	}
	if err := e.db.ExecErr(`UPDATE agent_task_event SET outcome='FORGED' WHERE task_id='event-task'`); err == nil {
		t.Fatal("event update succeeded")
	}
	if err := e.db.ExecErr(`DELETE FROM agent_task_event WHERE task_id='event-task'`); err == nil {
		t.Fatal("event delete succeeded")
	}
}

type eventTestExecutor struct{}

func TestTodo_AGENT_016_RecoveryPreCallPauseEvidence(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	store := e.tenantStore(t, "run-one")
	runtime, task := newRuntimeTask(t, store, "run-one", "paused-event-task")
	confirmed, err := runtime.ConfirmPlan(ctx, task.ID, task.UserID, task.Version, t0.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	paused, err := runtime.ExecuteNext(ctx, task.ID, confirmed.Version, eventPauseExecutor{}, nil, t0.Add(2*time.Minute))
	if !errors.Is(err, agentrun.ErrStepPaused) || paused.State != agentrun.StatePaused || paused.FailureCode != "AUTHORITY_CHANGED" {
		t.Fatalf("pre-call pause=%+v error=%v", paused, err)
	}
	reopened := e.tenantStore(t, "run-one")
	reloaded, err := reopened.Get(ctx, task.ID)
	if err != nil || reloaded.State != agentrun.StatePaused || reloaded.CurrentStep != 0 {
		t.Fatalf("pause restart=%+v error=%v", reloaded, err)
	}
	events, err := reopened.ListEvents(ctx, task.ID)
	if err != nil || len(events) < 1 {
		t.Fatalf("pause events=%+v error=%v", events, err)
	}
	last := events[len(events)-1]
	if last.Type != agentrun.TaskEventStepExecution || last.Outcome != "PAUSED" || last.EvidenceRef != "" || last.EvidenceDigest != "" {
		t.Fatalf("pause evidence=%+v", last)
	}
	if err := e.db.ExecErr(`INSERT INTO agent_task_event(tenant_id,task_id,event_sequence,event_type,plan_revision,plan_digest,step_id,step_type,side_effect_tier,outcome,evidence_ref,occurred_at) SELECT tenant_id,task_id,event_sequence+1,event_type,plan_revision,plan_digest,step_id,step_type,side_effect_tier,outcome,'forged:effect',occurred_at FROM agent_task_event WHERE task_id='paused-event-task' AND outcome='PAUSED'`); err == nil {
		t.Fatal("paused event accepted owner effect evidence")
	}
}

type eventPauseExecutor struct{}

func (eventPauseExecutor) Execute(context.Context, agentrun.AgentTask, agentrun.PlanStep) (agentrun.StepResult, error) {
	return agentrun.StepResult{}, &agentrun.StepPauseError{Reason: "AUTHORITY_CHANGED"}
}

func (*eventTestExecutor) Execute(context.Context, agentrun.AgentTask, agentrun.PlanStep) (agentrun.StepResult, error) {
	return agentrun.StepResult{Ref: "result:write", Digest: "sha256:write"}, nil
}
