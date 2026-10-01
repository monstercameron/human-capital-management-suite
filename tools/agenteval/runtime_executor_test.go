package agenteval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	runtimeeval "github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type taskDriverProbe struct {
	task         agentrun.AgentTask
	approved     int
	driven       int
	confirmed    int
	parkApproval bool
}

func (d *taskDriverProbe) confirm(_ context.Context, task agentrun.AgentTask, _ time.Time) (agentrun.AgentTask, error) {
	d.confirmed++
	task.State = agentrun.StateRunning
	return task, nil
}

func (d *taskDriverProbe) drive(context.Context, string) (agentrun.AgentTask, error) {
	d.driven++
	if d.parkApproval {
		d.task.State = agentrun.StateAwaitingApproval
	} else {
		d.task.State = agentrun.StateCompleted
	}
	return d.task, nil
}

func (d *taskDriverProbe) approve(context.Context, agentrun.AgentTask) (agentrun.AgentTask, error) {
	d.approved++
	d.task.State = agentrun.StateCompleted
	return d.task, nil
}

func (d *taskDriverProbe) evidence() runtimeeval.RuntimeEvidenceReader {
	return runtimeEvidenceProbe{task: d.task}
}

type runtimeEvidenceProbe struct{ task agentrun.AgentTask }

func (r runtimeEvidenceProbe) GetTask(context.Context, string) (agentrun.AgentTask, error) {
	return r.task, nil
}

func (runtimeEvidenceProbe) TaskEvents(context.Context, string) ([]agentrun.TaskEvent, error) {
	return nil, nil
}

type settledUsageProbe struct{}

func (settledUsageProbe) SettledTaskUsage(_ context.Context, tenant values.TenantId, taskID string) (agentbudget.SettledTaskUsage, error) {
	return agentbudget.SettledTaskUsage{TenantID: tenant.String(), TaskID: taskID}, nil
}

type syntheticSourceProbe struct {
	scenario SyntheticTaskRequest
	err      error
}

func (s syntheticSourceProbe) Prepare(context.Context, TaskCase) (SyntheticTaskRequest, error) {
	return s.scenario, s.err
}

func TestRuntimeTaskExecutorRequiresSyntheticSource(t *testing.T) {
	if _, err := NewRuntimeTaskExecutor(nil); !errors.Is(err, ErrRuntimeExecutorConfig) {
		t.Fatalf("NewRuntimeTaskExecutor(nil) error = %v, want config error", err)
	}
}

func TestRuntimeTaskExecutorRejectsUnboundScenario(t *testing.T) {
	executor, err := NewRuntimeTaskExecutor(syntheticSourceProbe{})
	if err != nil {
		t.Fatalf("NewRuntimeTaskExecutor() error = %v", err)
	}
	_, err = executor.Execute(context.Background(), TaskCase{ID: "case-1", TenantID: "synthetic-test", Goal: "goal"})
	if !errors.Is(err, ErrRuntimeExecutorConfig) {
		t.Fatalf("Execute() error = %v, want config error", err)
	}
	if _, err := executor.Execute(context.Background(), TaskCase{}); !errors.Is(err, ErrRuntimeExecutorConfig) {
		t.Fatalf("Execute(invalid case) error = %v, want config error", err)
	}
}

func TestRuntimeExecutorDoesNotAcceptTerminalStateWithoutRuntimeEvidence(t *testing.T) {
	case0 := TaskCase{ID: "case-1", TenantID: "synthetic-test", Goal: "goal"}
	start := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	task := agentrun.AgentTask{ID: case0.ID, TenantID: case0.TenantID, UserID: "synthetic-user", Goal: case0.Goal,
		State: agentrun.StateAwaitingPlanConfirmation, CreatedAt: start, UpdatedAt: start}
	driver := &taskDriverProbe{task: task}
	_, err := runSyntheticTask(context.Background(), case0, driver, task, settledUsageProbe{}, func() time.Time { return start })
	if !errors.Is(err, runtimeeval.ErrIncompleteRuntimeEvidence) {
		t.Fatalf("runSyntheticTask() error = %v, want incomplete runtime evidence", err)
	}
	if driver.confirmed != 1 || driver.driven != 1 || driver.approved != 0 {
		t.Fatalf("driver calls confirm=%d drive=%d approve=%d; want 1/1/0", driver.confirmed, driver.driven, driver.approved)
	}
}

func TestRuntimeExecutorRequiresDeclaredApprovalEffect(t *testing.T) {
	case0 := TaskCase{ID: "case-2", TenantID: "synthetic-test", Goal: "goal"}
	start := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	task := agentrun.AgentTask{ID: case0.ID, TenantID: case0.TenantID, UserID: "synthetic-user", Goal: case0.Goal,
		State: agentrun.StateAwaitingPlanConfirmation, CreatedAt: start, UpdatedAt: start, CurrentStep: 0,
		Plan: agentrun.AgentPlan{Steps: []agentrun.PlanStep{{ID: "submit", Type: agentrun.StepSubmit, Tier: agentrun.TierSubmitGoverned}}}}
	driver := &taskDriverProbe{task: task, parkApproval: true}
	_, err := runSyntheticTask(context.Background(), case0, driver, task, settledUsageProbe{}, func() time.Time { return start })
	if !errors.Is(err, ErrSyntheticTaskPending) {
		t.Fatalf("runSyntheticTask() error = %v, want pending approval", err)
	}
	if driver.approved != 0 {
		t.Fatalf("approval calls = %d, want none without case authorization", driver.approved)
	}
}
