package agenteval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type runtimeEvidenceStub struct {
	task   agentrun.AgentTask
	events []agentrun.TaskEvent
	err    error
}

func (r runtimeEvidenceStub) GetTask(context.Context, string) (agentrun.AgentTask, error) {
	return r.task, r.err
}

func (r runtimeEvidenceStub) TaskEvents(context.Context, string) ([]agentrun.TaskEvent, error) {
	return append([]agentrun.TaskEvent(nil), r.events...), r.err
}

func TestObserveRuntimeOutcomeUsesDurableTaskEventsAndSettledUsage(t *testing.T) {
	tenant, task, runtime, usage := runtimeOutcomeFixture(t)
	got, err := ObserveRuntimeOutcome(context.Background(), tenant, task.ID, runtime, usage)
	if err != nil {
		t.Fatalf("ObserveRuntimeOutcome() error = %v", err)
	}
	if !got.Completed || got.VerifyPassed != 1 || got.VerifyTotal != 1 || got.PlanRevisions != 0 || got.ApprovalsRequested["T3"] != 1 || got.Steps != 3 || got.WallClock != 42*time.Second || got.CostMicros != 321 {
		t.Fatalf("runtime outcome = %+v", got)
	}
}

func runtimeOutcomeFixture(t *testing.T) (values.TenantId, agentrun.AgentTask, runtimeEvidenceStub, *usageReaderStub) {
	t.Helper()
	tenant := values.TenantId("synthetic-test")
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	plan, err := agentrun.NewPlan([]agentrun.PlanStep{
		{ID: "analyze", Type: agentrun.StepAnalyze, SkillID: "policy.read", SkillVersion: 1, ExpectedOutput: "answer", Tier: agentrun.TierRead},
		{ID: "verify", Type: agentrun.StepVerify, SkillID: "policy.read", SkillVersion: 1, ExpectedOutput: "verified citations", Tier: agentrun.TierRead, Inputs: []agentrun.InputRef{{Name: "source", Ref: "fixture:policy"}}},
		{ID: "submit", Type: agentrun.StepSubmit, SkillID: "promotion.submit", SkillVersion: 1, ExpectedOutput: "submitted intent", Tier: agentrun.TierSubmitGoverned},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := agentrun.SnapshotPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	stepEvent := func(sequence uint64, kind agentrun.TaskEventType, stepID string, stepType agentrun.StepType, tier agentrun.Tier, outcome string) agentrun.TaskEvent {
		return agentrun.TaskEvent{Sequence: sequence, TaskID: "task-1", Type: kind, PlanRevision: plan.Revision,
			PlanDigest: plan.Digest, StepID: stepID, StepType: stepType, Tier: tier, Outcome: outcome,
			ApprovalDigest: "approval:submit", EvidenceRef: "evidence:" + stepID, EvidenceDigest: "sha256:" + stepID, OccurredAt: now.Add(time.Duration(sequence) * time.Second)}
	}
	events := []agentrun.TaskEvent{
		{Sequence: 1, TaskID: "task-1", Type: agentrun.TaskEventPlanRevision, PlanRevision: plan.Revision, PlanDigest: plan.Digest, PlanSnapshot: &snapshot, Outcome: "AWAITING_CONFIRMATION", OccurredAt: now},
		{Sequence: 2, TaskID: "task-1", Type: agentrun.TaskEventPlanConfirmation, PlanRevision: plan.Revision, PlanDigest: plan.Digest, PlanSnapshot: &snapshot, Outcome: "CONFIRMED", OccurredAt: now.Add(time.Second)},
		stepEvent(3, agentrun.TaskEventApprovalRequest, "submit", agentrun.StepSubmit, agentrun.TierSubmitGoverned, "PENDING"),
		stepEvent(4, agentrun.TaskEventApprovalOutcome, "submit", agentrun.StepSubmit, agentrun.TierSubmitGoverned, "APPROVED"),
		stepEvent(5, agentrun.TaskEventStepExecution, "analyze", agentrun.StepAnalyze, agentrun.TierRead, "SUCCEEDED"),
		stepEvent(6, agentrun.TaskEventStepExecution, "verify", agentrun.StepVerify, agentrun.TierRead, "SUCCEEDED"),
		stepEvent(7, agentrun.TaskEventStepVerification, "verify", agentrun.StepVerify, agentrun.TierRead, "VERIFIED"),
		stepEvent(8, agentrun.TaskEventStepExecution, "submit", agentrun.StepSubmit, agentrun.TierSubmitGoverned, "SUCCEEDED"),
		stepEvent(9, agentrun.TaskEventStepEffect, "submit", agentrun.StepSubmit, agentrun.TierSubmitGoverned, "SUCCEEDED"),
	}
	task := agentrun.AgentTask{ID: "task-1", TenantID: tenant.String(), State: agentrun.StateCompleted, Plan: plan,
		CurrentStep: len(plan.Steps), CreatedAt: now, UpdatedAt: now.Add(42 * time.Second)}
	runtime := runtimeEvidenceStub{task: task, events: events}
	usage := &usageReaderStub{result: agentbudget.SettledTaskUsage{TenantID: tenant.String(), TaskID: task.ID, Usage: agentbudget.Usage{SpendMicros: 321}}}

	return tenant, task, runtime, usage
}

func TestTodo_AGENT2_025_Security_RejectsNoncausalRuntimeEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func([]agentrun.TaskEvent, agentrun.AgentTask)
	}{
		{"approval after execution", func(events []agentrun.TaskEvent, _ agentrun.AgentTask) { events[3], events[7] = events[7], events[3] }},
		{"approval before request", func(events []agentrun.TaskEvent, _ agentrun.AgentTask) { events[2], events[3] = events[3], events[2] }},
		{"verification before execution", func(events []agentrun.TaskEvent, _ agentrun.AgentTask) { events[5], events[6] = events[6], events[5] }},
		{"effect before execution", func(events []agentrun.TaskEvent, _ agentrun.AgentTask) { events[7], events[8] = events[8], events[7] }},
		{"step before confirmation", func(events []agentrun.TaskEvent, _ agentrun.AgentTask) { events[1], events[4] = events[4], events[1] }},
		{"failed completed step", func(events []agentrun.TaskEvent, _ agentrun.AgentTask) { events[4].Outcome = "FAILED" }},
		{"event after terminal", func(events []agentrun.TaskEvent, task agentrun.AgentTask) {
			events[8].OccurredAt = task.UpdatedAt.Add(time.Second)
		}},
		{"event before creation", func(events []agentrun.TaskEvent, task agentrun.AgentTask) {
			events[0].OccurredAt = task.CreatedAt.Add(-time.Second)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenant, task, runtime, usage := runtimeOutcomeFixture(t)
			tc.mutate(runtime.events, task)
			for i := range runtime.events {
				runtime.events[i].Sequence = uint64(i + 1)
				if tc.name != "event after terminal" && tc.name != "event before creation" {
					runtime.events[i].OccurredAt = task.CreatedAt.Add(time.Duration(i) * time.Second)
				}
			}
			if _, err := ObserveRuntimeOutcome(context.Background(), tenant, task.ID, runtime, usage); !errors.Is(err, ErrIncompleteRuntimeEvidence) {
				t.Fatalf("accepted inconsistent event history: %v", err)
			}
		})
	}
}

type changingRuntimeReader struct {
	runtimeEvidenceStub
	next  agentrun.AgentTask
	reads int
}

func (r *changingRuntimeReader) GetTask(context.Context, string) (agentrun.AgentTask, error) {
	r.reads++
	if r.reads == 1 {
		return r.task, nil
	}
	return r.next, nil
}

func TestTodo_AGENT2_025_Security_RejectsSecondReadTenantSubstitution(t *testing.T) {
	tenant, task, runtime, usage := runtimeOutcomeFixture(t)
	foreign := task
	foreign.TenantID = "foreign-tenant"
	reader := &changingRuntimeReader{runtimeEvidenceStub: runtime, next: foreign}
	if _, err := ObserveRuntimeOutcome(context.Background(), tenant, task.ID, reader, usage); !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("second-read tenant substitution error = %v", err)
	}
}

func TestObserveRuntimeOutcomeRejectsUnboundStepEvidence(t *testing.T) {
	tenant := values.TenantId("synthetic-test")
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	plan, err := agentrun.NewPlan([]agentrun.PlanStep{{ID: "verify", Type: agentrun.StepVerify, SkillID: "policy.read", SkillVersion: 1, ExpectedOutput: "verified citations", Tier: agentrun.TierRead, Inputs: []agentrun.InputRef{{Name: "source", Ref: "fixture:policy"}}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := agentrun.SnapshotPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	events := []agentrun.TaskEvent{
		{Sequence: 1, TaskID: "task-1", Type: agentrun.TaskEventPlanRevision, PlanRevision: plan.Revision, PlanDigest: plan.Digest, PlanSnapshot: &snapshot, Outcome: "AWAITING_CONFIRMATION", OccurredAt: now},
		{Sequence: 2, TaskID: "task-1", Type: agentrun.TaskEventPlanConfirmation, PlanRevision: plan.Revision, PlanDigest: plan.Digest, PlanSnapshot: &snapshot, Outcome: "CONFIRMED", OccurredAt: now.Add(time.Second)},
		{Sequence: 3, TaskID: "task-1", Type: agentrun.TaskEventStepExecution, PlanRevision: plan.Revision, PlanDigest: "sha256:foreign", StepID: "verify", StepType: agentrun.StepVerify, Tier: agentrun.TierRead, Outcome: "SUCCEEDED", OccurredAt: now.Add(2 * time.Second)},
	}
	task := agentrun.AgentTask{ID: "task-1", TenantID: tenant.String(), State: agentrun.StateCompleted, Plan: plan, CurrentStep: 1, CreatedAt: now, UpdatedAt: now.Add(3 * time.Second)}
	usage := &usageReaderStub{result: agentbudget.SettledTaskUsage{TenantID: tenant.String(), TaskID: task.ID}}
	_, err = ObserveRuntimeOutcome(context.Background(), tenant, task.ID, runtimeEvidenceStub{task: task, events: events}, usage)
	if !errors.Is(err, ErrIncompleteRuntimeEvidence) {
		t.Fatalf("ObserveRuntimeOutcome() error = %v, want incomplete evidence", err)
	}
}
