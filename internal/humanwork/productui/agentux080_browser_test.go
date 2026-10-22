package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_AGENTUX_080_Browser re-checks the composed Agents pages: the top
// bar trigger reads the same on every page, the Ask view carries one switch
// with the quiet access link under it, and the access view has its single way
// back, in every locale.
func TestTodo_AGENTUX_080_Browser(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		trigger, navTrigger := locale.Text("action_launcher.trigger"), locale.Text("action_launcher.navigation_trigger")
		if trigger == "" || trigger != navTrigger {
			t.Fatalf("%s: the top bar trigger reads %q on one page and %q on another", localeID, trigger, navTrigger)
		}
		admin := renderAgentUXPage5(t, localeID, AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true}, true)
		if strings.Count(admin, "agents-page-nav-item") != 3 {
			t.Fatalf("%s: the composed Ask view lost its one switch", localeID)
		}
		if !strings.Contains(admin, `class="agents-access-link"`) || strings.Count(admin, `class="agents-access-link"`) != 1 {
			t.Fatalf("%s: the composed Ask view does not carry exactly one quiet access link", localeID)
		}
		view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", "view=access"), locale)
		markup, err := ui.RenderToString(BuildAgentAccessPage(view))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, ">"+agentUX080Text(locale, "back_to_agents")+"<") {
			t.Fatalf("%s: the composed access view lost its way back: %s", localeID, markup)
		}
	}
}
