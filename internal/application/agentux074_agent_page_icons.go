package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agentUX074AttachStoredIcons gives each agent already admitted to the viewer's
// Agents page the icon stored for it. The catalog decided which agents the
// viewer may see; this only decorates them and never adds one. An agent whose
// icon cannot be read keeps the zero icon, which the page draws as a fallback:
// a missing picture must not take the list of agents away.
func agentUX074AttachStoredIcons(ctx context.Context, icons AgentIconProjection, principal *trust.Principal, agents []productui.AgentSummary) {
	if icons.Store == nil || principal == nil {
		return
	}
	for index := range agents {
		decorated := agents[index]
		if err := icons.Agent(ctx, principal.Tenant(), decorated.ID, &decorated); err == nil {
			agents[index].Icon, agents[index].IconRevision = decorated.Icon, decorated.IconRevision
		}
	}
}
