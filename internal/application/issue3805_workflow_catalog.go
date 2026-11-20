package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// agentUXAmbientWorkflowStarts is the workflow catalog Chat offers from: the
// published workflows the shell's own start page lists, narrowed to those an
// administrator tagged "chat-offerable" (with the phrases that name them) and to
// the ones the person has the grants to start. It reads the same durable role
// facts the start page does and fails closed: a person with no assigned role,
// an unreadable policy or an entry that is not available is offered nothing.
type agentUXAmbientWorkflowStarts struct {
	source workspace.WorkflowStartSource
	roles  agentRoleFacts
}

// NewAgentUXAmbientWorkflowCatalog composes the catalog; it returns nil, which
// leaves workflow offers off, when the cell has no listable catalog or no role
// facts.
func NewAgentUXAmbientWorkflowCatalog(source workspace.WorkflowStartSource, roles agentRoleFacts) AgentUXAmbientWorkflowCatalog {
	if source == nil || roles == nil {
		return nil
	}
	return agentUXAmbientWorkflowStarts{source: source, roles: roles}
}

func (c agentUXAmbientWorkflowStarts) OfferableWorkflows(ctx context.Context, tenant, person string) ([]AgentUXAmbientWorkflow, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(person) == "" {
		return nil, nil
	}
	snapshot, err := c.roles.Load(ctx, values.TenantId(tenant), agentOrgScope(values.TenantId(tenant)))
	if err != nil {
		return nil, err
	}
	permissions := roleaccess.EffectivePagePermissions(snapshot, roleaccess.AssignedRoles(snapshot, person, nil))
	if !roleaccess.CanPageAction(permissions, string(productui.PageWorkflowStart), roleaccess.ActionView) || !roleaccess.CanPageAction(permissions, string(productui.PageJourneys), roleaccess.ActionCreate) {
		return nil, nil
	}
	entries, err := c.source(ctx, values.TenantId(tenant), true)
	if err != nil {
		return nil, err
	}
	var result []AgentUXAmbientWorkflow
	for _, entry := range entries {
		if entry.Availability != string(productui.WorkflowStartAvailable) {
			continue
		}
		if offerable, ok := AgentUXAmbientWorkflowFromCatalog(entry.WorkflowID, entry.Name, entry.Keywords); ok {
			result = append(result, offerable)
		}
	}
	return result, nil
}
