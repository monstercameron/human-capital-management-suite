package application

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AgentBudgetExtender adds budget to a paused task for its owner. The proto
// task-action enum has no extend value, so the action travels over the JSON
// overlay below and reaches the same controller the other task actions use.
type AgentBudgetExtender interface {
	ExtendTaskBudget(ctx context.Context, principal *trust.Principal, taskID, requestID string) error
}

var _ AgentBudgetExtender = (*agentController)(nil)

// ExtendTaskBudget rechecks the tenant setting and the person's current task
// authority, exactly as ControlTask does, and then extends the task's budget by
// the policy's allowance. The runner repeats the owner, tenant, paused-state and
// budget-revision checks; the task stays paused until the person resumes it.
func (c *agentController) ExtendTaskBudget(ctx context.Context, principal *trust.Principal, taskID, requestID string) error {
	if c == nil || c.platform == nil || c.authority == nil || c.now == nil || principal == nil || strings.TrimSpace(taskID) == "" || strings.TrimSpace(requestID) == "" {
		return agentclient.ErrNotAuthorized
	}
	if principal.SubjectKind() != trust.SubjectKindHuman || !principal.Assurance().AtLeast(trust.AssuranceLow) {
		return agentclient.ErrNotAuthorized
	}
	tenant, user := principal.Tenant(), principal.Subject()
	if tenant.Validate() != nil || strings.TrimSpace(user) == "" || c.settings == nil {
		return agentclient.ErrNotAuthorized
	}
	enabled, err := c.settings.AgentsEnabled(ctx, tenant)
	if err != nil {
		return fmt.Errorf("agent settings: %w", err)
	}
	if !enabled {
		return agentclient.ErrDisabled
	}
	now := c.now().UTC()
	decision, err := c.authority.Resolve(user, tenant, agentPurpose, now)
	if err != nil || !decision.Active || decision.UserID != user || decision.Authority.Tenant != tenant || !slices.Contains(decision.Authority.Capabilities, agentReadCapabilityID) || !slices.Contains(decision.Authority.Purposes, agentPurpose) {
		return agentclient.ErrNotAuthorized
	}
	runner, err := c.platform.ForTenant(ctx, tenant)
	if err != nil {
		return err
	}
	if _, err := runner.ExtendBudgetByAllowance(ctx, taskID, user, requestID, now); err != nil {
		return err
	}
	return nil
}
