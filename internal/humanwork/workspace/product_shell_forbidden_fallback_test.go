package workspace

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// Issue #56: a generic 403 (an unrelated denied route such as
// /admin/assistive-tech) must not offer "Go to Agents" as its recovery
// action. That CTA is only the recovery for the agent-operations denial,
// whose permitted fallback is the personal Agents page. A generic denial
// recovers to Home instead, keeping its Back secondary action.
func TestProductProblemForbiddenFallbackIsPageSpecific(t *testing.T) {
	h, _ := newShellHandler(t, false)
	const backHref = "/workspace/app/people"
	config := JourneyConfig{Tenant: string(shellTenant), Subject: uxblind122Worker.subject, Roles: uxblind122Worker.roles}

	render := func(t *testing.T, localeID string, page productui.PageID) (string, productui.LocaleContext) {
		t.Helper()
		locale := productui.ResolveProductLocale(localeID)
		request := httptest.NewRequest(http.MethodGet, "http://cell.test"+productui.Path(page)+"?locale="+localeID, nil)
		request.Header.Set("Referer", "http://cell.test"+backHref)
		recorder := httptest.NewRecorder()
		h.writeProductProblem(recorder, request, http.StatusForbidden, locale, page, config)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s %s: status = %d, want %d", localeID, page, recorder.Code, http.StatusForbidden)
		}
		body := recorder.Body.String()
		if !strings.Contains(body, `dir="`+string(locale.Direction)+`"`) {
			t.Fatalf("%s %s: document direction is not %q: %s", localeID, page, locale.Direction, body)
		}
		start := strings.Index(body, `class="surface empty-state workspace-page-problem"`)
		if start < 0 {
			t.Fatalf("%s %s: problem card missing: %s", localeID, page, body)
		}
		problem := body[start:]
		end := strings.Index(problem, "</section>")
		if end < 0 {
			t.Fatalf("%s %s: problem card is not closed: %s", localeID, page, body)
		}
		return problem[:end], locale
	}

	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		// Generic denied route: primary recovery is Home, not Agents.
		generic, locale := render(t, localeID, productui.PageAssistiveTech)
		definition, ok := productui.LookupPage(productui.PageAssistiveTech)
		if !ok || definition.TitleKey == "" {
			t.Fatalf("%s: assistive-tech definition missing title key", localeID)
		}
		for _, want := range []string{
			locale.Text(definition.TitleKey),
			locale.Text("agents.page_access_denied"),
			`class="button primary" href="` + PathProductHome + `"`,
			">" + locale.Text("agents.home") + "<",
			`class="button secondary" href="` + backHref + `"`,
			">" + locale.Text("agents.back") + "<",
		} {
			if !strings.Contains(generic, want) {
				t.Errorf("%s generic forbidden card missing %q: %s", localeID, want, generic)
			}
		}
		if strings.Contains(generic, agentUXProblemText(locale, "go_to_agents")) {
			t.Errorf("%s generic forbidden card still offers the Agents CTA %q: %s", localeID, agentUXProblemText(locale, "go_to_agents"), generic)
		}
		if strings.Contains(generic, `href="`+productui.Path(productui.PageAgents)+`"`) {
			t.Errorf("%s generic forbidden card links to the Agents page %q: %s", localeID, productui.Path(productui.PageAgents), generic)
		}
		if strings.Count(generic, `class="button primary"`) != 1 || strings.Count(generic, `class="button secondary"`) != 1 {
			t.Errorf("%s generic forbidden card is not one primary and one secondary action: %s", localeID, generic)
		}

		// Agent-operations denied route: primary recovery stays Agents.
		agentOps, _ := render(t, localeID, productui.PageAgentOperations)
		for _, want := range []string{
			locale.Text("page.agent_operations.title"),
			`class="button primary" href="` + productui.Path(productui.PageAgents) + `"`,
			">" + agentUXProblemText(locale, "go_to_agents") + "<",
			`class="button secondary" href="` + backHref + `"`,
			">" + locale.Text("agents.back") + "<",
		} {
			if !strings.Contains(agentOps, want) {
				t.Errorf("%s agent-operations forbidden card missing %q: %s", localeID, want, agentOps)
			}
		}
		if strings.Contains(agentOps, `class="button primary" href="`+PathProductHome+`"`) {
			t.Errorf("%s agent-operations forbidden card lost its Agents primary recovery: %s", localeID, agentOps)
		}
		if strings.Count(agentOps, `class="button primary"`) != 1 || strings.Count(agentOps, `class="button secondary"`) != 1 {
			t.Errorf("%s agent-operations forbidden card is not one primary and one secondary action: %s", localeID, agentOps)
		}
	}
}
