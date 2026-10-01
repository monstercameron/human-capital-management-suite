package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/resources"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

// AgentTaskStepAdmission joins the durable task credential boundary to worker
// resources. Long tasks use the autonomous lane even when user started.
type AgentTaskStepAdmission struct {
	Runtime    *AgentResourceRuntime
	ProviderID string
}

func (a AgentTaskStepAdmission) AcquireTaskStep(ctx context.Context, task agentsystem.TaskWorkIdentity) (context.Context, func(), error) {
	if task.Mode != agentsystem.ModeOnBehalfOf && task.Mode != agentsystem.ModeSponsored {
		return nil, nil, ErrAgentResourceIdentity
	}
	provider := ""
	if task.StepType == agentrun.StepAnalyze || task.StepType == agentrun.StepDraft {
		provider = a.ProviderID
	}
	lease, err := a.Runtime.Acquire(ctx, AgentResourceIdentity{TenantID: task.TenantID, UserID: task.UserID, TaskID: task.TaskID, Lane: resources.LaneAutonomous}, provider)
	if err != nil {
		return nil, nil, err
	}
	return lease.Context(ctx), lease.Release, nil
}

// CommonAgentResourceIdentityResolver reloads the exact accepted inbox and
// execution pins. It never treats client identity fields as resource authority.
func CommonAgentResourceIdentityResolver(runtime *CommonAgentRuntime) func(context.Context, string, string) (AgentResourceIdentity, error) {
	return func(ctx context.Context, tenant, task string) (AgentResourceIdentity, error) {
		if runtime == nil {
			return AgentResourceIdentity{}, ErrAgentResourceIdentity
		}
		admission, err := runtime.GetAdmission(ctx, tenant, task)
		if err != nil {
			return AgentResourceIdentity{}, err
		}
		run, err := runtime.GetRun(ctx, tenant, task)
		if err != nil {
			return AgentResourceIdentity{}, err
		}
		if admission.Decision != agentrun.DecisionAccepted || run.ActorID == "" || run.CancelRequested || run.ExpireRequested || run.State != runstate.StateRunning {
			return AgentResourceIdentity{}, ErrAgentResourceIdentity
		}
		lane := resources.LaneAutonomous
		switch admission.Request.Source.Kind {
		case agentrun.SourceChat, agentrun.SourcePersonaMention, agentrun.SourceAPI:
			lane = resources.LaneInteractive
		}
		return AgentResourceIdentity{TenantID: tenant, UserID: run.ActorID, TaskID: task, Lane: lane}, nil
	}
}

var _ agentsystem.StepAdmission = AgentTaskStepAdmission{}
var _ AgentModelResourceAdmission = (*AgentResourceRuntime)(nil)
