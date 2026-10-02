package productclient

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_CHATBUG_069_TabRoute: Agent operations keeps its tab in the address
// the loader writes back. The loader replaces the address with CanonicalHref
// before the page reads it, so a tab the route profile did not name was dropped
// on every load and the page showed Activity whatever was linked.
func TestTodo_CHATBUG_069_TabRoute(t *testing.T) {
	for _, tab := range []string{"running", "rollout", "move", "announcements"} {
		state, err := ParseState("/workspace/app/admin/agents", "tab="+tab)
		if err != nil {
			t.Fatalf("%s: %v", tab, err)
		}
		if want := "/workspace/app/admin/agents?tab=" + tab; CanonicalHref(state) != want {
			t.Fatalf("%s: the tab did not survive the loader's address rewrite: %q", tab, CanonicalHref(state))
		}
	}
	if _, err := ParseState("/workspace/app/admin/agents", "tab=bogus"); err == nil {
		t.Fatal("an unknown tab was accepted")
	}
	state, err := ParseState("/workspace/app/admin/agents", "")
	if err != nil || CanonicalHref(state) != "/workspace/app/admin/agents" {
		t.Fatalf("an address without a tab gained one: %v %q", err, CanonicalHref(state))
	}
}

// TestTodo_CHATBUG_069_TabRendered: the tab named by the address is the one the
// page draws selected, in the page the client builds from the parsed state.
func TestTodo_CHATBUG_069_TabRendered(t *testing.T) {
	state, err := ParseState("/workspace/app/admin/agents", "tab=announcements")
	if err != nil {
		t.Fatal(err)
	}
	view := productui.ApplyRequest(productui.NewView(productui.PageAgentOperations, "tenant", "owner", ""), state.Request)
	view.AgentsProjection = &productui.AgentsAvailabilityProjection{ViewerIsAdmin: true}
	markup, err := ui.RenderToString(productui.BuildAgentOperationsPage(view))
	if err != nil {
		t.Fatal(err)
	}
	at := strings.Index(markup, `id="agent-operations-tab-announcements"`)
	if at < 0 {
		t.Fatalf("no announcements tab: %s", markup)
	}
	tab := markup[strings.LastIndex(markup[:at], "<a "):]
	if tab = tab[:strings.Index(tab, ">")]; !strings.Contains(tab, `aria-selected="true"`) {
		t.Fatalf("the announcements tab is not selected by its address: %s", tab)
	}
}
