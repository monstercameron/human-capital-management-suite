package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agentController is the application authorization seam for task controls.
// It rechecks the tenant setting and current durable authority immediately
// before delegating to agentsystem's owner and CAS checks.
type agentController struct {
	platform  *agentsystem.Platform
	settings  workspace.AgentSettings
	authority agentdelegation.AuthorityResolver
	now       func() time.Time
}

func (c *agentController) ControlTask(ctx context.Context, principal *trust.Principal, req agentclient.TaskControl) (agentclient.ControlledTask, error) {
	if c == nil || c.platform == nil || c.authority == nil || c.now == nil || principal == nil || strings.TrimSpace(req.TaskID) == "" || req.ExpectedVersion == 0 {
		return agentclient.ControlledTask{}, agentclient.ErrNotAuthorized
	}
	if principal.SubjectKind() != trust.SubjectKindHuman || !principal.Assurance().AtLeast(trust.AssuranceLow) {
		return agentclient.ControlledTask{}, agentclient.ErrNotAuthorized
	}
	tenant, user := principal.Tenant(), principal.Subject()
	if tenant.Validate() != nil || strings.TrimSpace(user) == "" || c.settings == nil {
		return agentclient.ControlledTask{}, agentclient.ErrNotAuthorized
	}
	enabled, err := c.settings.AgentsEnabled(ctx, tenant)
	if err != nil {
		return agentclient.ControlledTask{}, fmt.Errorf("agent settings: %w", err)
	}
	if !enabled {
		return agentclient.ControlledTask{}, agentclient.ErrDisabled
	}
	now := c.now().UTC()
	decision, err := c.authority.Resolve(user, tenant, agentPurpose, now)
	if err != nil || !decision.Active || decision.UserID != user || decision.Authority.Tenant != tenant || !slices.Contains(decision.Authority.Capabilities, agentReadCapabilityID) || !slices.Contains(decision.Authority.Purposes, agentPurpose) {
		return agentclient.ControlledTask{}, agentclient.ErrNotAuthorized
	}
	action := agentsystem.TaskAction(req.Action)
	// Confirm and resume can start work after this RPC returns. The grant
	// adapter is context-bound when ForTenant is called, so bind it to the
	// bounded detached lifetime before creating the runner. Authorization and
	// the state transition still use the live request context.
	runnerCtx := ctx
	var cancelDrive context.CancelFunc
	if action == agentsystem.ActionConfirmPlan || action == agentsystem.ActionResume {
		runnerCtx, cancelDrive = context.WithTimeout(context.WithoutCancel(ctx), agentDriveTimeout)
	}
	runner, err := c.platform.ForTenant(runnerCtx, tenant)
	if err != nil {
		if cancelDrive != nil {
			cancelDrive()
		}
		return agentclient.ControlledTask{}, err
	}
	task, err := runner.ControlTask(ctx, req.TaskID, user, action, req.ExpectedVersion, now)
	if err != nil {
		if cancelDrive != nil {
			cancelDrive()
		}
		return agentclient.ControlledTask{}, err
	}
	if cancelDrive != nil && task.State == agentrun.StateRunning {
		go func() {
			defer cancelDrive()
			_, _ = runner.Drive(runnerCtx, task.ID, agentsystem.ModeOnBehalfOf)
		}()
	} else if cancelDrive != nil {
		cancelDrive()
	}
	return agentclient.ControlledTask{ID: task.ID, State: string(task.State), Version: task.Version}, nil
}
