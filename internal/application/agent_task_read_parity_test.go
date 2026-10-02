package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentTaskParityExecutor struct{ err error }

func (e agentTaskParityExecutor) Execute(context.Context, agentrun.AgentTask, agentrun.PlanStep) (agentrun.StepResult, error) {
	return agentrun.StepResult{}, e.err
}

// TestTodo_AGENTUX_020_Integration stores one task in every state the runtime
// can leave behind, some started through the served surface and some written
// by the runtime alone as an older build stored them, then reads them through
// the served gRPC service: every listed task opens for its owner with the same
// projection, and every state is filed under exactly one tab of the page.
func TestTodo_AGENTUX_020_Integration(t *testing.T) {
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	ctx, client := newServedAgentClient(t, f)
	background := context.Background()
	runner, err := f.runtime.Platform.ForTenant(background, values.TenantId(f.tenant))
	if err != nil {
		t.Fatal(err)
	}
	runtime := runner.Runtime
	now := time.Now().UTC()
	want := map[string]agentrun.TaskState{}

	// Started through the served surface: these rows carry the answering agent,
	// step times and the document usage state.
	answered, err := client.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "What is my job title?", Mode: agentv1.AgentStartMode_AGENT_START_MODE_QUICK_ANSWER})
	if err != nil || answered.GetState() != string(agentrun.StateCompleted) {
		t.Fatalf("quick answer = %+v, %v", answered, err)
	}
	want[answered.GetTaskId()] = agentrun.StateCompleted
	planned, err := client.StartAgentTask(ctx, &agentv1.StartAgentTaskRequest{Prompt: "Plan my first week", Mode: agentv1.AgentStartMode_AGENT_START_MODE_LONG_TASK})
	if err != nil || planned.GetState() != string(agentrun.StateAwaitingPlanConfirmation) {
		t.Fatalf("long task = %+v, %v", planned, err)
	}
	want[planned.GetTaskId()] = agentrun.StateAwaitingPlanConfirmation

	// Written by the runtime alone: no answering agent, no step times and no
	// usage marker, as a task stored before the projection carried them.
	seed := func(id string, created time.Time, steps []agentrun.PlanStep) agentrun.AgentTask {
		t.Helper()
		plan, err := agentrun.NewPlan(steps)
		if err != nil {
			t.Fatal(err)
		}
		task, err := runtime.CreateTask(background, agentrun.CreateRequest{ID: id, TenantID: f.tenant, UserID: agentTestWorker, Goal: "Older request " + id, Plan: plan, Now: created, ExpiresAt: created.Add(time.Hour)})
		if err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		return task
	}
	confirmed := func(id string, steps []agentrun.PlanStep) agentrun.AgentTask {
		t.Helper()
		task := seed(id, now, steps)
		task, err := runtime.ConfirmPlan(background, task.ID, agentTestWorker, task.Version, now)
		if err != nil {
			t.Fatalf("confirm %s: %v", id, err)
		}
		return task
	}
	readThenAnswer := agentTaskPlanSteps(AgentTaskPlanningOutput{NeedsOwnRecord: true})
	governed := []agentrun.PlanStep{{ID: "submit_request", Type: agentrun.StepSubmit, SkillID: "agent.submit_request", SkillVersion: 1, ExpectedOutput: "a submitted request", Tier: agentrun.TierSubmitGoverned}}

	seed("older-awaiting-plan", now, readThenAnswer)
	want["older-awaiting-plan"] = agentrun.StateAwaitingPlanConfirmation

	confirmed("older-running", readThenAnswer)
	want["older-running"] = agentrun.StateRunning

	task := confirmed("older-paused", readThenAnswer)
	if _, err := runtime.Pause(background, task.ID, task.Version, now); err != nil {
		t.Fatal(err)
	}
	want["older-paused"] = agentrun.StatePaused

	task = confirmed("older-waiting", readThenAnswer)
	if _, err := runtime.Park(background, task.ID, task.Version, agentrun.WakeCondition{Kind: agentrun.WakeSignal, Key: "signal:older-waiting"}, now); err != nil {
		t.Fatal(err)
	}
	want["older-waiting"] = agentrun.StateWaiting

	task = confirmed("older-awaiting-approval", governed)
	if parked, err := runtime.ExecuteNext(background, task.ID, task.Version, agentTaskParityExecutor{}, nil, now); !errors.Is(err, agentrun.ErrApprovalRequired) || parked.State != agentrun.StateAwaitingApproval {
		t.Fatalf("governed step = %+v, %v", parked, err)
	}
	want["older-awaiting-approval"] = agentrun.StateAwaitingApproval

	task = confirmed("older-failed", readThenAnswer)
	stepFailure := errors.New("the step could not run")
	if failed, err := runtime.ExecuteNext(background, task.ID, task.Version, agentTaskParityExecutor{err: stepFailure}, nil, now); !errors.Is(err, stepFailure) || failed.State != agentrun.StateFailed {
		t.Fatalf("failed step = %+v, %v", failed, err)
	}
	want["older-failed"] = agentrun.StateFailed

	task = seed("older-cancelled", now, readThenAnswer)
	if _, err := runtime.Cancel(background, task.ID, task.Version, now); err != nil {
		t.Fatal(err)
	}
	want["older-cancelled"] = agentrun.StateCancelled

	// Created two hours ago with an hour to live, so the sweep at this instant
	// expires this task and no other.
	seed("older-expired", now.Add(-2*time.Hour), readThenAnswer)
	if settled, err := runtime.SweepStaleTasks(background, now); err != nil || len(settled) != 1 || settled[0].ID != "older-expired" {
		t.Fatalf("sweep = %+v, %v", settled, err)
	}
	want["older-expired"] = agentrun.StateExpired

	groupOf := map[agentrun.TaskState]string{
		agentrun.StateAwaitingPlanConfirmation: "active", agentrun.StateRunning: "active", agentrun.StateWaiting: "active",
		agentrun.StateAwaitingApproval: "active", agentrun.StatePaused: "active", agentrun.StateCompleted: "completed",
		agentrun.StateFailed: "failed", agentrun.StateCancelled: "failed", agentrun.StateExpired: "failed",
	}
	listed, err := client.ListAgentTasks(ctx, &agentv1.ListAgentTasksRequest{})
	if err != nil || len(listed.GetTasks()) != len(want) {
		t.Fatalf("listed %d tasks, %v; want %d", len(listed.GetTasks()), err, len(want))
	}
	states := map[agentrun.TaskState]bool{}
	groups := map[string]int{}
	for _, row := range listed.GetTasks() {
		state, ok := want[row.GetTaskId()]
		if !ok || row.GetState() != string(state) {
			t.Fatalf("listed task %s is %q; want %q", row.GetTaskId(), row.GetState(), state)
		}
		opened, err := client.GetAgentTask(ctx, &agentv1.GetAgentTaskRequest{TaskId: row.GetTaskId()})
		if err != nil {
			t.Fatalf("listed task %s (%s) does not open: %v", row.GetTaskId(), row.GetState(), err)
		}
		if !proto.Equal(opened.GetTask(), row) {
			t.Fatalf("task %s opens as %+v but is listed as %+v", row.GetTaskId(), opened.GetTask(), row)
		}
		if row.GetAnsweringAgentDisplayName() == "" || row.GetPrompt() == "" || row.GetCreatedAt() == nil {
			t.Fatalf("task %s projection is incomplete: %+v", row.GetTaskId(), row)
		}
		group := productui.AgentTaskGroup(productui.AgentTask{State: productui.AgentTaskState(row.GetState()), ResultPreview: row.GetResultPreview(), FailureReason: row.GetFailureSummary()})
		if group != groupOf[state] {
			t.Fatalf("task %s in state %s is filed under %q; want %q", row.GetTaskId(), state, group, groupOf[state])
		}
		states[state] = true
		groups[group]++
		delete(want, row.GetTaskId())
	}
	if len(want) != 0 || len(states) != len(groupOf) {
		t.Fatalf("not every stored state was listed: missing tasks %v, states seen %v", want, states)
	}
	if groups["active"] != 6 || groups["completed"] != 1 || groups["failed"] != 3 {
		t.Fatalf("tab counts = %v; want 6 active, 1 completed, 3 failed", groups)
	}

	// Another person's listed id opens for nobody else, and an id that was
	// never stored is not found rather than an error.
	admin := agentTestPrincipal(t, f.tenant, agentTestAdmin)
	if _, err := f.runtime.Starter.GetAgentTask(trust.WithPrincipal(background, admin), admin, answered.GetTaskId()); !errors.Is(err, agentrun.ErrNotFound) {
		t.Fatalf("another user's read = %v; want not found", err)
	}
	if others, err := f.runtime.Starter.ListAgentTasks(trust.WithPrincipal(background, admin), admin); err != nil || len(others) != 0 {
		t.Fatalf("another user's list = %+v, %v; want empty", others, err)
	}
	if _, err := client.GetAgentTask(ctx, &agentv1.GetAgentTaskRequest{TaskId: "agt_never_stored"}); status.Code(err) != codes.NotFound {
		t.Fatalf("unknown task = %v; want NotFound", err)
	}
}
