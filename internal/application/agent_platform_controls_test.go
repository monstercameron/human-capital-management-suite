package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func waitForAgentTaskState(t *testing.T, fixture *agentFixture, taskID string, want agentrun.TaskState) agentrun.AgentTask {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		task := fixture.task(t, taskID)
		if task.State == want {
			return task
		}
		if task.State == agentrun.StateFailed || task.State == agentrun.StateCancelled || task.State == agentrun.StateExpired {
			t.Fatalf("task reached %s, want %s: code=%q", task.State, want, task.FailureCode)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("task did not reach %s before deadline; current state is %s", want, task.State)
		case <-ticker.C:
		}
	}
}

func TestTodo_UXBLIND_122_ControlProjectionAndLongTaskLifecycle(t *testing.T) {
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	ctx := context.Background()
	worker := agentTestPrincipal(t, f.tenant, agentTestWorker)
	admin := agentTestPrincipal(t, f.tenant, agentTestAdmin)
	client := agentclient.FromPlatformWithControls(f.runtime.Platform, f.cell.AgentSettings, f.runtime.Controller)

	long, err := f.runtime.Starter.StartTaskMode(ctx, worker, "summarise my job", agentclient.StartLongTask)
	if err != nil || long.State != string(agentrun.StateAwaitingPlanConfirmation) {
		t.Fatalf("long start = %+v, %v, want awaiting plan confirmation", long, err)
	}
	if calls := f.runtime.Model.Fake.CallCount(); calls != 0 {
		t.Fatalf("long start ran the model before confirmation: %d calls", calls)
	}
	snapshot, err := client.Snapshot(ctx, productui.AgentSnapshotRequest{TenantID: f.tenant, Principal: agentTestWorker})
	if err != nil || len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Version != long.Version ||
		!snapshot.Tasks[0].Actions.ConfirmPlan || !snapshot.Tasks[0].Actions.Cancel {
		t.Fatalf("awaiting-plan projection = %+v, %v", snapshot, err)
	}
	if got := client.TaskPolicy(ctx, f.tenant, agentTestWorker, long.ID); got != (agentclient.ActionPolicy{ConfirmPlan: true, Cancel: true}) {
		t.Fatalf("current plan policy = %+v", got)
	}

	// A different valid principal in the same tenant cannot confirm another
	// person's task. The denied call leaves the original revision untouched.
	if _, err := f.runtime.Controller.ControlTask(ctx, admin, agentclient.TaskControl{TaskID: long.ID, ExpectedVersion: long.Version, Action: agentclient.TaskConfirmPlan}); !errors.Is(err, agentsystem.ErrDenied) {
		t.Fatalf("other owner confirmation = %v, want ErrDenied", err)
	}
	if task := f.task(t, long.ID); task.State != agentrun.StateAwaitingPlanConfirmation || task.Version != long.Version {
		t.Fatalf("denied confirmation changed task = %+v", task)
	}

	requestCtx, cancelRequest := context.WithCancel(ctx)
	confirmed, err := f.runtime.Controller.ControlTask(requestCtx, worker, agentclient.TaskControl{
		TaskID: long.ID, ExpectedVersion: long.Version, Action: agentclient.TaskConfirmPlan,
	})
	if err != nil || confirmed.State != string(agentrun.StateRunning) {
		cancelRequest()
		t.Fatalf("confirm = %+v, %v, want RUNNING", confirmed, err)
	}
	// End the request lifetime as soon as the control RPC responds. The bounded
	// task drive and tenant-bound grant adapter must survive it.
	cancelRequest()
	done := waitForAgentTaskState(t, f, long.ID, agentrun.StateCompleted)
	if !done.Plan.Confirmed || done.Plan.ConfirmedBy != agentTestWorker || f.runtime.Model.Fake.CallCount() != 1 {
		t.Fatalf("confirmed long task = state %s, confirmed=%v by=%q, model calls=%d", done.State, done.Plan.Confirmed, done.Plan.ConfirmedBy, f.runtime.Model.Fake.CallCount())
	}
	if _, err := f.runtime.Controller.ControlTask(ctx, worker, agentclient.TaskControl{TaskID: long.ID, ExpectedVersion: long.Version, Action: agentclient.TaskConfirmPlan}); !errors.Is(err, agentrun.ErrConflict) {
		t.Fatalf("confirmation replay = %v, want ErrConflict", err)
	}
	if got := client.TaskPolicy(ctx, f.tenant, agentTestWorker, long.ID); got != (agentclient.ActionPolicy{}) {
		t.Fatalf("terminal task policy = %+v, want no controls", got)
	}

	// Resume takes the same detached, bounded runner path. Prepare a durable
	// paused task without starting a worker, then resume through the controller.
	resumable, err := f.runtime.Starter.StartTaskMode(ctx, worker, "summarise my role", agentclient.StartLongTask)
	if err != nil {
		t.Fatalf("second long start: %v", err)
	}
	runner, err := f.runtime.Platform.ForTenant(ctx, worker.Tenant())
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := runner.Runtime.ConfirmPlan(ctx, resumable.ID, agentTestWorker, resumable.Version, time.Now().UTC())
	if err != nil {
		t.Fatalf("prepare second plan: %v", err)
	}
	paused, err := runner.Runtime.Pause(ctx, resumable.ID, prepared.Version, time.Now().UTC())
	if err != nil || paused.State != agentrun.StatePaused {
		t.Fatalf("prepare paused task = %+v, %v", paused, err)
	}
	if got := client.TaskPolicy(ctx, f.tenant, agentTestWorker, resumable.ID); got != (agentclient.ActionPolicy{Resume: true, Cancel: true}) {
		t.Fatalf("paused task policy = %+v", got)
	}
	resumeCtx, cancelResume := context.WithCancel(ctx)
	resumed, err := f.runtime.Controller.ControlTask(resumeCtx, worker, agentclient.TaskControl{
		TaskID: resumable.ID, ExpectedVersion: paused.Version, Action: agentclient.TaskResume,
	})
	if err != nil || resumed.State != string(agentrun.StateRunning) {
		cancelResume()
		t.Fatalf("resume = %+v, %v, want RUNNING", resumed, err)
	}
	cancelResume()
	resumedDone := waitForAgentTaskState(t, f, resumable.ID, agentrun.StateCompleted)
	if !resumedDone.Plan.Confirmed || resumedDone.Plan.ConfirmedBy != agentTestWorker || f.runtime.Model.Fake.CallCount() != 2 {
		t.Fatalf("resumed task = state %s, confirmed=%v by=%q, model calls=%d", resumedDone.State, resumedDone.Plan.Confirmed, resumedDone.Plan.ConfirmedBy, f.runtime.Model.Fake.CallCount())
	}
}

func TestTodo_UXBLIND_122_ControlFailsClosedForDisabledAndInvalidRequests(t *testing.T) {
	f := newAgentFixture(t)
	f.setEnabled(t, true)
	ctx := context.Background()
	worker := agentTestPrincipal(t, f.tenant, agentTestWorker)
	started, err := f.runtime.Starter.StartTaskMode(ctx, worker, "summarise my job", agentclient.StartLongTask)
	if err != nil {
		t.Fatal(err)
	}
	f.setEnabled(t, false)
	client := agentclient.FromPlatformWithControls(f.runtime.Platform, f.cell.AgentSettings, f.runtime.Controller)
	if got := client.TaskPolicy(ctx, f.tenant, agentTestWorker, started.ID); got != (agentclient.ActionPolicy{}) {
		t.Fatalf("disabled tenant still exposes task controls: %+v", got)
	}
	if _, err := f.runtime.Controller.ControlTask(ctx, worker, agentclient.TaskControl{TaskID: started.ID, ExpectedVersion: started.Version, Action: agentclient.TaskConfirmPlan}); !errors.Is(err, agentclient.ErrDisabled) {
		t.Fatalf("control after disable = %v, want ErrDisabled", err)
	}
	if task := f.task(t, started.ID); task.State != agentrun.StateAwaitingPlanConfirmation || task.Version != started.Version {
		t.Fatalf("disabled control changed task = %+v", task)
	}
	if _, err := f.runtime.Starter.StartTaskMode(ctx, worker, "summarise my job", agentclient.StartMode("UNRECOGNIZED")); !errors.Is(err, agentclient.ErrInvalidPrompt) {
		t.Fatalf("invalid start mode = %v, want ErrInvalidPrompt", err)
	}
	if tasks := f.snapshot(t, f.tenant, agentTestWorker).Tasks; len(tasks) != 1 || tasks[0].ID != started.ID {
		t.Fatalf("invalid mode changed the owner's task set: %+v", tasks)
	}
}
