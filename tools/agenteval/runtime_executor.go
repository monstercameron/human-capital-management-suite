package agenteval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	runtimeeval "github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrRuntimeExecutorConfig = errors.New("agenteval: invalid runtime executor configuration")
	ErrSyntheticTaskPending  = errors.New("agenteval: synthetic task did not reach a terminal state")
)

// SyntheticTaskRequest is the isolated composition needed to start one fresh
// scenario. The executor calls StartSyntheticTask itself, so the tenant
// authority is checked before stores are scoped or a task is created.
type SyntheticTaskRequest struct {
	Platform  *agentsystem.Platform
	Authority agentsystem.SyntheticTenantAuthority
	Start     agentsystem.StartRequest
	Usage     runtimeeval.SettledUsageReader
	Now       func() time.Time
}

// SyntheticTaskSource resolves an isolated test tenant's trusted authority,
// evaluator-only platform and scenario request. Implementations must never use
// a production tenant or return model-reported measurements.
type SyntheticTaskSource interface {
	Prepare(context.Context, TaskCase) (SyntheticTaskRequest, error)
}

// RuntimeTaskExecutor drives the real agentsystem.Runner and projects only
// terminal durable runtime evidence into the AGENT2-025 outcome schema.
type RuntimeTaskExecutor struct{ source SyntheticTaskSource }

// NewRuntimeTaskExecutor requires a synthetic-tenant task source.
func NewRuntimeTaskExecutor(source SyntheticTaskSource) (*RuntimeTaskExecutor, error) {
	if source == nil {
		return nil, ErrRuntimeExecutorConfig
	}
	return &RuntimeTaskExecutor{source: source}, nil
}

// Execute starts, confirms and drives one isolated task. It performs only an
// explicit approval declared by this suite case and routes that approval
// through Runner.DeliverApproval, which rechecks tenant authority. Parked
// waits/manual replies remain incomplete until a real synthetic wake fixture
// is supplied by the source.
func (e *RuntimeTaskExecutor) Execute(ctx context.Context, taskCase TaskCase) (TaskOutcome, error) {
	if e == nil || e.source == nil || ctx == nil || strings.TrimSpace(taskCase.TenantID) == "" || strings.TrimSpace(taskCase.ID) == "" {
		return TaskOutcome{}, ErrRuntimeExecutorConfig
	}
	scenario, err := e.source.Prepare(ctx, taskCase)
	if err != nil {
		return TaskOutcome{}, err
	}
	if scenario.Platform == nil || scenario.Authority == nil || scenario.Usage == nil || scenario.Now == nil || scenario.Start.TaskID == "" ||
		scenario.Start.Goal != taskCase.Goal || scenario.Start.UserID == "" {
		return TaskOutcome{}, fmt.Errorf("%w: source returned an incomplete or mismatched task request", ErrRuntimeExecutorConfig)
	}
	tenant := values.TenantId(taskCase.TenantID)
	runner, task, err := scenario.Platform.StartSyntheticTask(ctx, tenant, scenario.Authority, scenario.Start)
	if err != nil {
		return TaskOutcome{}, err
	}
	if runner == nil || runner.TenantID() != tenant || task.TenantID != tenant.String() || task.Goal != taskCase.Goal {
		return TaskOutcome{}, fmt.Errorf("%w: synthetic runner/task binding mismatch", ErrRuntimeExecutorConfig)
	}
	return runSyntheticTask(ctx, taskCase, runnerControl{runner: runner}, task, scenario.Usage, scenario.Now)
}

type runnerControl struct{ runner *agentsystem.Runner }

func (r runnerControl) confirm(ctx context.Context, task agentrun.AgentTask, now time.Time) (agentrun.AgentTask, error) {
	return r.runner.Runtime.ConfirmPlan(ctx, task.ID, task.UserID, task.Version, now)
}

func (r runnerControl) drive(ctx context.Context, taskID string) (agentrun.AgentTask, error) {
	return r.runner.Drive(ctx, taskID, agentsystem.ModeOnBehalfOf)
}

func (r runnerControl) approve(ctx context.Context, task agentrun.AgentTask) (agentrun.AgentTask, error) {
	if task.CurrentStep < 0 || task.CurrentStep >= len(task.Plan.Steps) {
		return agentrun.AgentTask{}, ErrSyntheticTaskPending
	}
	step := task.Plan.Steps[task.CurrentStep]
	return r.runner.DeliverApproval(ctx, task.ID, step.ID, step.ApprovalDigest, task.UserID)
}

func (r runnerControl) evidence() runtimeeval.RuntimeEvidenceReader { return r.runner.Runtime }

type taskDriver interface {
	confirm(context.Context, agentrun.AgentTask, time.Time) (agentrun.AgentTask, error)
	drive(context.Context, string) (agentrun.AgentTask, error)
	approve(context.Context, agentrun.AgentTask) (agentrun.AgentTask, error)
	evidence() runtimeeval.RuntimeEvidenceReader
}

func runSyntheticTask(ctx context.Context, taskCase TaskCase, driver taskDriver, task agentrun.AgentTask, usage runtimeeval.SettledUsageReader, now func() time.Time) (TaskOutcome, error) {
	if task.ID == "" || task.TenantID != taskCase.TenantID || task.Goal != taskCase.Goal || task.UserID == "" || usage == nil || now == nil {
		return TaskOutcome{}, ErrRuntimeExecutorConfig
	}
	confirmed, err := driver.confirm(ctx, task, now().UTC())
	if err != nil {
		return TaskOutcome{}, err
	}
	task = confirmed
	for range agentrun.MaxPlanSteps {
		if terminalTask(task.State) {
			observed, err := runtimeeval.ObserveRuntimeOutcome(ctx, values.TenantId(taskCase.TenantID), task.ID, driver.evidence(), usage)
			if err != nil {
				return TaskOutcome{}, err
			}
			return TaskOutcome{
				Completed: observed.Completed, VerifyPassed: observed.VerifyPassed, VerifyTotal: observed.VerifyTotal,
				PlanRevisions: observed.PlanRevisions, ApprovalsRequested: observed.ApprovalsRequested,
				Steps: observed.Steps, WallClock: observed.WallClock, CostMicros: observed.CostMicros,
			}, nil
		}
		if task.State == agentrun.StateAwaitingApproval {
			if task.CurrentStep < 0 || task.CurrentStep >= len(task.Plan.Steps) || taskCase.ApprovalEffect != fmt.Sprintf("T%d", task.Plan.Steps[task.CurrentStep].Tier) {
				return TaskOutcome{}, fmt.Errorf("%w: approval was not declared for this scenario effect", ErrSyntheticTaskPending)
			}
			task, err = driver.approve(ctx, task)
		} else {
			task, err = driver.drive(ctx, task.ID)
		}
		if err != nil {
			return TaskOutcome{}, err
		}
		if task.State == agentrun.StateWaiting || task.State == agentrun.StatePaused || task.State == agentrun.StateAwaitingPlanConfirmation {
			return TaskOutcome{}, fmt.Errorf("%w: task is %s", ErrSyntheticTaskPending, task.State)
		}
	}
	return TaskOutcome{}, fmt.Errorf("%w: task exceeded the bounded driver loop", ErrSyntheticTaskPending)
}

func terminalTask(state agentrun.TaskState) bool {
	switch state {
	case agentrun.StateCompleted, agentrun.StateFailed, agentrun.StateCancelled, agentrun.StateExpired:
		return true
	default:
		return false
	}
}
