package workspace

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gwchtml "github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestAgentUXR4_ProblemPagesRenderInsideProductShell(t *testing.T) {
	locale := productui.ResolveProductLocale("en-US")
	theme := productui.DefaultCustomerTheme()
	theme.BrandName = "Ironridge Builders"
	theme.BrandMark = "IB"
	theme.BrandLogoURL = "/workspace/assets/ironridge-logo.svg"
	stylesheet, err := productStylesheetForTheme(theme)
	if err != nil {
		t.Fatal(err)
	}
	problem := gwchtml.Section(gwchtml.Props{Class: "surface empty-state workspace-page-problem", Role: "alert"},
		gwchtml.H1(gwchtml.Props{ID: "workspace-problem-title"}, ui.Text("You do not have access to this page.")),
		gwchtml.P(gwchtml.Props{}, ui.Text("Ask your workspace administrator to grant access.")),
		gwchtml.Div(gwchtml.Props{Class: "action-row"},
			gwchtml.A(gwchtml.Props{Class: "button primary", Href: productui.Path(productui.PageAgents)}, ui.Text("Go to Agents")),
			gwchtml.A(gwchtml.Props{Class: "button secondary", Href: productui.Path(productui.PageHome)}, ui.Text("Home")),
		),
	)
	doc, err := productShellProblemDocument(JourneyConfig{Tenant: "ironridge-demo", TenantName: theme.BrandName, TenantMark: theme.BrandMark, TenantLogo: theme.BrandLogoURL}, locale, productui.PageAgentOperations, problem, theme, productui.DefaultAccessibilityPreferences(), stylesheet)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="app-shell`, `class="shell-grid"`, `id="workspace-navigation"`, `class="topbar"`, `class="popover-root account-menu`, `id="main-content"`,
		`class="surface empty-state workspace-page-problem"`, `class="action-row"`, `class="button primary" href="/workspace/app/chat/agents"`, `class="button secondary" href="/workspace/app/home"`,
		`<style>`, `--hcm-color-brand-primary`, `src="/workspace/assets/ironridge-logo.svg"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("problem shell missing %q: %s", want, doc)
		}
	}
	if strings.Contains(doc, `workspace-problem-shell`) {
		t.Fatalf("problem page still uses stripped shell wrapper: %s", doc)
	}
}

func TestAgentUXR4_UnknownProductRoutesUseShellCopyAndLegacyAgentsRedirect(t *testing.T) {
	h := &Handler{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://cell.test/workspace/app/agents?locale=de-DE&filter=open", nil)
	h.serveProduct(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("legacy agents status = %d, want 303", recorder.Code)
	}
	if got := recorder.Header().Get("Location"); got != "/workspace/app/chat/agents?locale=de-DE&filter=open" {
		t.Fatalf("legacy agents location = %q", got)
	}

	for _, code := range []string{"en-US", "de-DE", "ar"} {
		locale := productui.ResolveProductLocale(code)
		title := productUnknownText(locale, "title")
		message := productUnknownText(locale, "message")
		home := productUnknownText(locale, "home")
		if title == "" || message == "" || home == "" {
			t.Fatalf("%s unknown route copy is incomplete", code)
		}
		if code == "en-US" && title != "This page does not exist" {
			t.Fatalf("unknown route title = %q", title)
		}
	}
	if page, ok := closestProductRouteByLastSegment("/workspace/app/unknown/agents"); !ok || page.ID != productui.PageAgents {
		t.Fatalf("closest route for unknown/agents = %+v, %t", page, ok)
	}
}
