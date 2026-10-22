package productui

import (
	"html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func agentUX080Render(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// TestTodo_AGENTUX_080: a refused or failed read on the Agents pages is the
// shared loading placeholder with Try again, Agent setup opens in plain words,
// the page head has one switch and one quiet link, the page has one link style
// and each button carries its explanation on one line when there is room.
func TestTodo_AGENTUX_080(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		retry := AgentLoadingText(locale, "retry")

		// 1. Every failed read is the shared placeholder, never an inline error.
		for name, test := range map[string]struct {
			node   ui.Node
			action string
			status string
		}{
			"spend limits": {AgentSpendLimitsFailed(locale), `data-agent-spend-action="retry"`, agentCostText(locale, "limits_failed")},
			"cost":         {AgentCostPanel(AgentCostPanelProps{I18nProps: I18nProps{Locale: locale}, State: AgentAccessStateUnavailable}), `data-agent-cost-action="retry"`, agentCostText(locale, "failed")},
			"my access":    {AgentAccessPage(AgentAccessPageProps{I18nProps: I18nProps{Locale: locale}, State: AgentAccessStateUnavailable}), `data-agent-access-action="retry"`, agentAccessText(locale, "unavailable_title")},
			"connections":  {AgentAdminAccessPage(AgentAdminAccessPageProps{I18nProps: I18nProps{Locale: locale}, State: AgentAccessStateUnavailable}), `data-agent-admin-action="retry"`, agentAdminText(locale, "unavailable_title")},
		} {
			markup := agentUX080Render(t, test.node)
			for _, want := range []string{`data-chat-loading="`, `chatux037-loading`, `chatux037-retry`, test.action, ">" + retry + "<", html.EscapeString(test.status)} {
				if !strings.Contains(markup, want) {
					t.Fatalf("%s %s: a failed read is not the loading-then-retry state, missing %q: %s", localeID, name, want, markup)
				}
			}
			if strings.Contains(markup, `role="alert"`) || strings.Contains(markup, "action-row") || strings.Contains(markup, "empty-state") {
				t.Fatalf("%s %s: a failed read still draws an inline error: %s", localeID, name, markup)
			}
		}

		// 2. Agent setup opens with what an administrator can do, in plain words.
		var opening []string
		for _, key := range []string{"no_template", "no_template_next", "named_template_manager"} {
			opening = append(opening, personaAdminR5SetupText(locale, key))
		}
		opening = append(opening, personaAdminText(locale, "catalog_detail"))
		for _, text := range opening {
			if strings.TrimSpace(text) == "" {
				t.Fatalf("%s: an opening sentence is missing from the copy table", localeID)
			}
			for _, banned := range []string{"build", "Build", "template", "Template", "Vorlage", "Produktupdate", "product update", "available", "Verfügbarkeit", "قالب", "تحديث المنتج", "متاحاً"} {
				if strings.Contains(text, banned) {
					t.Fatalf("%s: the opening of Agent setup still talks about %q: %s", localeID, banned, text)
				}
			}
		}
		notice := personaAdminRender(t, personaAdminNoStarterNotice(locale, nil, ""))
		if !strings.Contains(notice, personaAdminR5SetupText(locale, "no_template")) || !strings.Contains(notice, personaAdminR5SetupText(locale, "no_template_next")) {
			t.Fatalf("%s: the opening notice does not say what an administrator can do: %s", localeID, notice)
		}

		// 3. One navigation switch; My agent access is a link, not a second tab strip.
		admin := renderAgentUXPage5(t, localeID, AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true}, true)
		if strings.Count(admin, `<nav aria-label="`+locale.Text("agents.admin_navigation")+`" class="agents-page-nav">`) != 1 || strings.Count(admin, "agents-page-nav-item") != 3 {
			t.Fatalf("%s: the head does not carry exactly one Ask / Setup / Operations switch: %s", localeID, admin)
		}
		if strings.Contains(admin, "agents-view-tab") {
			t.Fatalf("%s: a second tab strip is still drawn: %s", localeID, admin)
		}
		if !strings.Contains(admin, `class="agents-access-link"`) || !strings.Contains(admin, "view=access") || !strings.Contains(admin, ">"+agentAccessText(locale, "tab_access")+"<") {
			t.Fatalf("%s: the access link is missing from the Ask view: %s", localeID, admin)
		}
		person := renderAgentUXPage5(t, localeID, AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true}, false)
		if strings.Contains(person, "agents-page-nav-item") || !strings.Contains(person, `class="agents-access-link"`) {
			t.Fatalf("%s: a person who does not administer agents sees the wrong head: %s", localeID, person)
		}
		view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", "view=access"), locale)
		access := agentUX080Render(t, BuildAgentAccessPage(view))
		if !strings.Contains(access, agentUX080Text(locale, "back_to_agents")) || strings.Contains(access, "agents-view-tab") || strings.Contains(access, `view=access"`) {
			t.Fatalf("%s: the access view has no single way back to Ask: %s", localeID, access)
		}
	}

	// 4. One link style on the agent pages, and the button explanations are not
	// held to a narrow measure.
	css := agentUX073Stylesheet()
	for _, want := range []string{
		".agent-page-frame :is(.persona-admin-page,.agents-page,.agent-operations-page",
		"text-decoration:underline",
		".agents-composer-action-hint{max-inline-size:none}",
		".agents-composer-action{flex:0 1 auto}",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("the agent stylesheet is missing %q", want)
		}
	}
	if narrow, wide := strings.Index(css, "max-inline-size:22rem}"), strings.LastIndex(css, ".agents-composer-action-hint{max-inline-size:none}"); narrow < 0 || wide < narrow {
		t.Fatalf("the one-line explanation rule does not follow the 22rem cap it overrides (%d, %d)", narrow, wide)
	}
}
