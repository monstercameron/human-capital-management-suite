package agentjourney

import (
	"context"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// RenderAgentsPage renders the agents surface in one locale.
func (w *World) RenderAgentsPage(locale string) string {
	w.T.Helper()
	ctx := context.Background()
	reader := agentclient.New(journeyRunners{w})
	snapshot, err := reader.Snapshot(ctx, productui.AgentSnapshotRequest{TenantID: journeyTenant, Principal: journeyOwner})
	if err != nil {
		w.T.Fatal(err)
	}
	view := productui.NewView(productui.PageAgents, journeyTenant, journeyOwner, "")
	view = productui.ApplyAgentsAvailability(view, productui.AgentsAvailabilityProjection{Enabled: true, Snapshot: snapshot})
	view = productui.ApplyLocale(productui.ApplyRequest(view, productui.PageRequest{Page: productui.PageAgents, AgentTaskID: journeyTask}), productui.ResolveProductLocale(locale))
	markup, err := ui.RenderToString(productui.BuildAgentsSurface(view))
	if err != nil {
		w.T.Fatal(err)
	}
	return strings.NewReplacer("&#39;", "'", "&amp;", "&", "&quot;", `"`).Replace(markup)
}
