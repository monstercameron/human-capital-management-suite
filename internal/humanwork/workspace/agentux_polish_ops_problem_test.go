package workspace

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_AGENTUX_010_BrandFirstPaint(t *testing.T) {
	h, _ := newShellHandler(t, false)
	request := httptest.NewRequest(http.MethodGet, "http://cell.test/workspace/app/admin/agents?locale=en-US", nil)
	request.Header.Set("Referer", "http://cell.test/workspace/app/chat/agents")
	recorder := httptest.NewRecorder()
	theme := productui.DefaultCustomerTheme()
	theme.BrandName = "Ironridge Builders"
	theme.BrandMark = "IB"
	theme.BrandLogoURL = "/workspace/assets/ironridge-logo.svg"
	theme.Palette = "copper"
	stylesheet, err := productStylesheetForTheme(theme)
	if err != nil {
		t.Fatal(err)
	}
	h.writeProductProblemWithAppearance(recorder, request, http.StatusForbidden, productui.ResolveProductLocale("en-US"), productui.PageAgentOperations, JourneyConfig{Tenant: "ironridge-demo", TenantName: theme.BrandName, TenantMark: theme.BrandMark, TenantLogo: theme.BrandLogoURL}, theme, productui.DefaultAccessibilityPreferences())
	body := recorder.Body.String()
	for _, want := range []string{`data-hcm-palette="copper"`, `src="/workspace/assets/ironridge-logo.svg"`, `<span class="sr-only" data-hcm-brand-name="">Ironridge Builders</span>`, `class="button primary" href="/workspace/app/chat/agents"`, `class="button secondary" href="/workspace/app/chat/agents"`, "Go to Agents"} {
		if !strings.Contains(body, want) {
			t.Errorf("branded refusal missing %q: %s", want, body)
		}
	}
	problem := body[strings.Index(body, `class="surface empty-state workspace-page-problem"`):]
	problem = problem[:strings.Index(problem, "</section>")]
	if !strings.Contains(stylesheet, "--hcm-color-brand-primary") || strings.Contains(problem, ">Home<") || strings.Contains(problem, ">Try again<") {
		t.Fatalf("refusal lost theme tokens or offered a dead-end recovery: %s", body)
	}
}

func TestTodo_AGENTUX_010_SecurityProjection(t *testing.T) {
	h, _ := newShellHandler(t, false)
	request := httptest.NewRequest(http.MethodGet, "http://cell.test/workspace/app/admin/agents", nil)
	recorder := httptest.NewRecorder()
	config := JourneyConfig{Tenant: "ironridge-demo", Agents: &AgentsConfig{}}
	h.writeProductProblem(recorder, request, http.StatusForbidden, productui.ResolveProductLocale("en-US"), productui.PageAgentOperations, config)
	body := recorder.Body.String()
	for _, forbidden := range []string{"Running agents", "agent-controls", "data-owner-action", "persona_admin_snapshot", "task-secret"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("refusal leaked protected data or controls %q: %s", forbidden, body)
		}
	}
}
