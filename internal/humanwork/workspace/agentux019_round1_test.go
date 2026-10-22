package workspace

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_AGENTUX_019 pins the round-1 refused-page findings: a refused
// agent surface keeps the tenant shell and brand, renders its message in the
// request locale, and always offers a permitted destination.
func TestTodo_AGENTUX_019(t *testing.T) {
	t.Run("F33 F34 refusal keeps the shell brand and a way out", func(t *testing.T) {
		h, _ := newShellHandler(t, false)
		for _, localeID := range []string{"en-US", "de-DE", "ar"} {
			locale := productui.ResolveProductLocale(localeID)
			request := httptest.NewRequest(http.MethodGet, "http://cell.test/workspace/app/admin/agents?locale="+localeID, nil)
			recorder := httptest.NewRecorder()
			h.writeProductProblem(recorder, request, http.StatusForbidden, locale, productui.PageAgentOperations, JourneyConfig{Tenant: string(shellTenant), Subject: uxblind122Worker.subject, Roles: uxblind122Worker.roles})
			body := recorder.Body.String()
			for _, want := range []string{`class="app-shell`, `id="workspace-navigation"`, `dir="` + string(locale.Direction) + `"`, locale.Text("agents.page_access_denied"), `href="/workspace/app/chat/agents"`} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s refusal missing %q", localeID, want)
				}
			}
		}
	})
}
