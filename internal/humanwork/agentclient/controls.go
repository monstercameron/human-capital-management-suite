package agentclient

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ActionPolicy is the fail-closed server projection of legal task actions.
type ActionPolicy struct {
	ConfirmPlan  bool
	Pause        bool
	Resume       bool
	Cancel       bool
	ExtendBudget bool
}

// PolicyReader is implemented by the composed client for workspace
// projections. The control RPC remains the authoritative recheck.
type PolicyReader interface {
	TaskPolicy(context.Context, string, string, string) ActionPolicy
}

type policyReader interface {
	TaskPolicy(context.Context, agentrun.AgentTask, string) agentsystem.TaskActionPolicy
}

// TaskPolicy computes one task's current action affordances. Errors and
// unavailable authority deliberately return the zero policy.
func (c *Client) TaskPolicy(ctx context.Context, tenantID, principal, taskID string) ActionPolicy {
	if c == nil || c.runners == nil || c.settings == nil || c.controller == nil || tenantID == "" || principal == "" || taskID == "" {
		return ActionPolicy{}
	}
	tenant := values.TenantId(tenantID)
	enabled, err := c.settings.AgentsEnabled(ctx, tenant)
	if err != nil || !enabled {
		return ActionPolicy{}
	}
	runner, err := c.runners.Runner(ctx, tenant)
	if err != nil || runner == nil {
		return ActionPolicy{}
	}
	_, ok := runner.(policyReader)
	if !ok {
		return ActionPolicy{}
	}
	tasks, err := runner.UserTasks(ctx, principal)
	if err != nil {
		return ActionPolicy{}
	}
	for _, task := range tasks {
		if task.ID != taskID || task.UserID != principal || task.TenantID != tenantID {
			continue
		}
		return c.policyForTask(ctx, runner, task, principal)
	}
	return ActionPolicy{}
}

func (c *Client) policyForTask(ctx context.Context, runner TaskReader, task agentrun.AgentTask, principal string) ActionPolicy {
	if c == nil || c.settings == nil || c.controller == nil || runner == nil || task.UserID != principal {
		return ActionPolicy{}
	}
	reader, ok := runner.(policyReader)
	if !ok {
		return ActionPolicy{}
	}
	policy := reader.TaskPolicy(ctx, task, principal)
	return ActionPolicy{ConfirmPlan: policy.ConfirmPlan, Pause: policy.Pause, Resume: policy.Resume, Cancel: policy.Cancel, ExtendBudget: policy.ExtendBudget}
}

// StartMode chooses the server-owned policy for a new task.
type StartMode string

const (
	StartQuickAnswer StartMode = "QUICK_ANSWER"
	StartLongTask    StartMode = "LONG_TASK"
)

// TaskAction is a user control over the owner's durable task.
type TaskAction string

const (
	TaskConfirmPlan TaskAction = "CONFIRM_PLAN"
	TaskPause       TaskAction = "PAUSE"
	TaskResume      TaskAction = "RESUME"
	TaskCancel      TaskAction = "CANCEL"
)

// TaskControl is the minimal CAS request for a task action. Tenant and user
// are derived from the verified principal and are never client supplied.
type TaskControl struct {
	TaskID          string
	ExpectedVersion uint64
	Action          TaskAction
}

// ControlledTask is the safe result returned after a state transition.
type ControlledTask struct {
	ID      string
	State   string
	Version uint64
}

// Controller performs owner-scoped task actions through the composed runtime.
type Controller interface {
	ControlTask(context.Context, *trust.Principal, TaskControl) (ControlledTask, error)
}

// ModeStarter is implemented by production starters that apply the explicit
// quick-answer versus long-task policy. Starter remains compatible for older
// callers; production transport prefers this interface when available.
type ModeStarter interface {
	StartTaskMode(context.Context, *trust.Principal, string, StartMode) (StartedTask, error)
}
