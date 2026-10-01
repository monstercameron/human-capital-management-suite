package productclient

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func agentsNavListed(items []productui.NavItem) bool {
	for _, item := range items {
		if item.Page == productui.PageAgents || agentsNavListed(item.Children) {
			return true
		}
	}
	return false
}

// The browser client's view composition applies the island's agents
// projection to both the menu and the Agents page, and fails closed when a
// composed session carries none.
func TestTodo_UXBLIND_122_LoadingView(t *testing.T) {
	permissions := []productui.RolePagePermission{
		{RoleID: "r", Page: productui.PageHome, View: true}, {RoleID: "r", Page: productui.PageChat, View: true}, {RoleID: "r", Page: productui.PageAgents, View: true},
	}
	state := State{Page: productui.PageAgents, Request: productui.PageRequest{Page: productui.PageAgents}}
	base := Session{Tenant: "tenant", Principal: "worker", Roles: []string{"worker_self"}, Permissions: permissions, EnforceRoleVisibility: true}

	missing := LoadingView(base, state)
	if agentsNavListed(missing.Navigation) || productui.ResolveAgentsSurface(missing.AgentsProjection).State != productui.AgentsSurfaceDisabledHidden {
		t.Fatal("a composed session without an agents projection did not fail closed")
	}

	enabled := base
	enabled.Agents = &productui.AgentsAvailabilityProjection{Enabled: true, Snapshot: productui.AgentSnapshot{
		Availability: productui.AgentsAvailable, Tasks: []productui.AgentTask{{ID: "t1", Title: "Collect my evidence", State: productui.AgentTaskAwaitingInput}},
	}}
	view := LoadingView(enabled, state)
	if !agentsNavListed(view.Navigation) {
		t.Fatal("an enabled projection did not advertise Agents")
	}
	markup, err := ui.RenderToString(productui.BuildAgentsSurface(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Collect my evidence") {
		t.Fatalf("enabled page = %s", markup)
	}
}
