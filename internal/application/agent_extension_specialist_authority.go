package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
)

// SpecialistChildAuthority is the durable current-source check used by the
// delegation core before child creation and at every child checkpoint.
// CommonAdmissionID is persisted in the child grant, so a restart rechecks the
// exact accepted common request rather than trusting the original page call.
type SpecialistChildAuthority struct {
	Runtime   *CommonAgentRuntime
	Authority *CommonAgentAuthority
}

func NewAgentSpecialistChildTaskAuthority(runtime *CommonAgentRuntime, authority *CommonAgentAuthority) *SpecialistChildAuthority {
	if runtime == nil || authority == nil {
		return nil
	}
	return &SpecialistChildAuthority{Runtime: runtime, Authority: authority}
}

var _ interface {
	CheckChildTask(context.Context, agentrun.AgentTask, agentdelegation.Grant) error
} = (*SpecialistChildAuthority)(nil)

func (a *SpecialistChildAuthority) CheckChildTask(ctx context.Context, task agentrun.AgentTask, grant agentdelegation.Grant) error {
	if a == nil || a.Runtime == nil || a.Authority == nil || ctx == nil || grant.CommonAdmissionID == "" {
		return ErrSpecialistDenied
	}
	record, err := a.Runtime.GetAdmission(ctx, task.TenantID, grant.CommonAdmissionID)
	if err != nil || record.Decision != agentrun.DecisionAccepted {
		return fmt.Errorf("%w: child common admission unavailable", ErrSpecialistDenied)
	}
	if err := a.Runtime.Recheck(ctx, task.TenantID, grant.CommonAdmissionID); err != nil {
		return fmt.Errorf("%w: child common admission is stale: %v", ErrSpecialistDenied, err)
	}
	request := record.Request
	if request.Principal.Mode != agentrun.ModeOnBehalfOf || request.Principal.InvokerID != task.UserID || request.Principal.DelegatedCredentialRef != task.ID || request.InstallationID != grant.InstallationID || request.Agent.AgentID != grant.TargetAgentID || request.Purpose != grant.Purpose || !request.Deadline.Equal(task.ExpiresAt) || !grant.ExpiresAt.Equal(task.ExpiresAt) || request.Source.TenantID != task.TenantID {
		return fmt.Errorf("%w: child task does not match common admission", ErrSpecialistDenied)
	}
	if grant.ParentGrantID != agentsystem.GrantID(task.ParentTaskID) || grant.ParentActor == nil || grant.ParentActor.RunID != task.ParentTaskID {
		return fmt.Errorf("%w: child parent lineage does not match admission", ErrSpecialistDenied)
	}
	if at := strings.LastIndexByte(grant.AgentVersion, '@'); at <= 0 || grant.AgentVersion[at+1:] != request.Agent.Version || grant.AgentVersion[:at] != request.Agent.AgentID {
		return fmt.Errorf("%w: child version attribution does not match admission", ErrSpecialistDenied)
	}
	return a.Authority.ValidateSpecialistPlan(ctx, request, task.Plan.Steps)
}
