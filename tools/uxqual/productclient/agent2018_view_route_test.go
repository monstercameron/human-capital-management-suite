package productclient

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_AGENT2_018_AgentsViewRoute: the Agents page keeps its second view,
// My agent access, in the address the loader writes back. The loader replaces
// the address with CanonicalHref before the page reads it, so a view the route
// profile did not name was dropped on every load and the Ask page showed
// whatever was linked (reproduced on the review server: ?view=access loaded
// the Ask page and the address lost the key).
func TestTodo_AGENT2_018_AgentsViewRoute(t *testing.T) {
	state, err := ParseState("/workspace/app/chat/agents", "view=access")
	if err != nil {
		t.Fatal(err)
	}
	if want := "/workspace/app/chat/agents?view=access"; CanonicalHref(state) != want {
		t.Fatalf("the view did not survive the loader's address rewrite: %q, want %q", CanonicalHref(state), want)
	}
	// The view and a task selector are independent keys of the same page.
	both, err := ParseState("/workspace/app/chat/agents", "task=task-1&view=access")
	if err != nil || !strings.Contains(CanonicalHref(both), "view=access") || !strings.Contains(CanonicalHref(both), "task=task-1") {
		t.Fatalf("view and task = %q, %v", CanonicalHref(both), err)
	}
	if _, err := ParseState("/workspace/app/chat/agents", "view=bogus"); err == nil {
		t.Fatal("an unknown Agents view was accepted")
	}
	plain, err := ParseState("/workspace/app/chat/agents", "")
	if err != nil || CanonicalHref(plain) != "/workspace/app/chat/agents" {
		t.Fatalf("an address without a view gained one: %v %q", err, CanonicalHref(plain))
	}
	// Another page does not carry the Agents view.
	if other, err := ParseState("/workspace/app/admin/agents", "view=access"); err == nil && strings.Contains(CanonicalHref(other), "view=") {
		t.Fatalf("Agent operations kept the Agents view: %q", CanonicalHref(other))
	}
}

// TestTodo_AGENT2_018_AgentsViewRendered: the view named by the address is the
// one the page draws, from the state the client parsed.
func TestTodo_AGENT2_018_AgentsViewRendered(t *testing.T) {
	state, err := ParseState("/workspace/app/chat/agents", "view=access")
	if err != nil {
		t.Fatal(err)
	}
	view := productui.ApplyRequest(productui.NewView(productui.PageAgents, "tenant", "person", ""), state.Request)
	if !productui.AgentAccessViewRequested(view.Query) {
		t.Fatalf("the access view was not requested by its address: query %q", view.Query)
	}
	markup, err := ui.RenderToString(productui.BuildAgentAccessPage(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `id="agent-access-mount"`) {
		t.Fatalf("the access view has no place for the script to fill: %s", markup)
	}
	ask := productui.ApplyRequest(productui.NewView(productui.PageAgents, "tenant", "person", ""), productui.PageRequest{Page: productui.PageAgents})
	if productui.AgentAccessViewRequested(ask.Query) {
		t.Fatal("the Ask view was drawn as the access view")
	}
}

// TestTodo_AGENT2_019_ConsoleTabsRoute: the Connections and Cost tabs of Agent
// operations keep their address through the loader's rewrite. They were left out
// of the tab values the route profile accepts, so ?tab=connections was refused
// as malformed and the page loaded Activity (reproduced on the review server).
func TestTodo_AGENT2_019_ConsoleTabsRoute(t *testing.T) {
	for _, tab := range []string{"connections", "cost"} {
		state, err := ParseState("/workspace/app/admin/agents", "tab="+tab)
		if err != nil {
			t.Fatalf("%s: %v", tab, err)
		}
		if want := "/workspace/app/admin/agents?tab=" + tab; CanonicalHref(state) != want {
			t.Fatalf("%s: the tab did not survive the loader's address rewrite: %q", tab, CanonicalHref(state))
		}
		view := productui.ApplyRequest(productui.NewView(productui.PageAgentOperations, "tenant", "owner", ""), state.Request)
		view.AgentsProjection = &productui.AgentsAvailabilityProjection{ViewerIsAdmin: true}
		markup, err := ui.RenderToString(productui.BuildAgentOperationsPage(view))
		if err != nil {
			t.Fatal(err)
		}
		at := strings.Index(markup, `id="agent-operations-tab-`+tab+`"`)
		if at < 0 {
			t.Fatalf("no %s tab: %s", tab, markup)
		}
		anchor := markup[strings.LastIndex(markup[:at], "<a "):]
		if anchor = anchor[:strings.Index(anchor, ">")]; !strings.Contains(anchor, `aria-selected="true"`) {
			t.Fatalf("the %s tab is not selected by its address: %s", tab, anchor)
		}
	}
}
