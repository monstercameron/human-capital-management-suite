package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_CHATUX_045_Integration: the served Agents and chat pages carry the
// tablet and phone fixes: the tab strips scroll with an edge fade and open on
// the chosen tab, cost tables become one card per row with labelled cells, the
// brand mark keeps its slot at tablet widths, and the phone list bar keeps the
// bell and the account menu.
func TestTodo_CHATUX_045_Integration(t *testing.T) {
	sheet := agentsStylesheet()
	for _, want := range []string{
		`overflow-x:auto`,
		`scroll-snap-type:inline proximity`,
		`mask-image:linear-gradient(90deg,transparent,#000 var(--agents-fade-start)`,
		`[aria-selected="true"]){scroll-snap-align:center}`,
		`@media(max-width:560px){`,
		`.agent-cost-table tr{display:grid`,
		`.agent-cost-table td::before{content:attr(data-label)`,
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("agents sheet missing %.70q", want)
		}
	}

	locale := ResolveProductLocale("en-US")
	markup, err := ui.RenderToString(AgentCostPanel(AgentCostPanelProps{
		I18nProps: I18nProps{Locale: locale},
		Report: AgentCostReport{
			PerAgentDay: []AgentCostAgentDay{{Agent: "Policy Helper", Day: "2026-10-01", Micros: 1200000}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="agent-cost-table"`) || !strings.Contains(markup, `data-label="`) {
		t.Error("cost table cells do not name their column for the card layout")
	}
	if strings.Count(markup, "<td") != strings.Count(markup, "data-label=") {
		t.Error("a cost cell has no data-label")
	}

	shell := chatShellStylesheet()
	for _, want := range []string{
		`.brand-logo-slot{min-inline-size:34px;flex:none}`,
		`.app-shell:has(.chat-workspace[data-sidebar-open="true"]) .topbar{display:flex`,
		`.topbar>:not(.notifications,.account-menu){display:none}`,
	} {
		if !strings.Contains(shell, want) {
			t.Errorf("chat shell sheet missing %.70q", want)
		}
	}
}
