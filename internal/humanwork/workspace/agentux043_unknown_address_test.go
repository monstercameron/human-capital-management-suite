package workspace

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func agentUX043Get(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	h, token := newShellHandler(t, false)
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+target, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	return recorder
}

// An unknown address under the workspace belongs to the workspace: the shell,
// "This page does not exist", the closest page when there is one, and Home.
func TestTodo_AGENTUX_043(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := productui.ResolveProductLocale(language)
		response := agentUX043Get(t, "/workspace/app/nowhere/agents?locale="+language)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s: unknown address answered %d, want 404", language, response.Code)
		}
		body := response.Body.String()
		for _, want := range []string{`class="app-shell`, `<header class="topbar"`, `class="primary-nav"`, `class="surface empty-state workspace-page-problem"`} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s: the unknown-address page is missing %q", language, want)
			}
		}
		title, message, home := productUnknownText(locale, "title"), productUnknownText(locale, "message"), productUnknownText(locale, "home")
		problem := body[strings.Index(body, `class="surface empty-state workspace-page-problem"`):]
		problem = problem[:strings.Index(problem, "</section>")]
		for _, want := range []string{title, message, `href="` + productui.Path(productui.PageHome) + `"`, home} {
			if want == "" || !strings.Contains(problem, want) {
				t.Fatalf("%s: the card does not carry %q: %s", language, want, problem)
			}
		}
		// The closest page by its last segment is offered: "agents" offers Agents.
		agents, _ := productui.LookupPage(productui.PageAgents)
		if !strings.Contains(problem, `href="`+productui.Path(productui.PageAgents)+`"`) || !strings.Contains(problem, locale.Text(agents.LabelKey)) {
			t.Fatalf("%s: the closest page is not offered: %s", language, problem)
		}
		if !strings.Contains(body, `lang="`+locale.Resolved+`"`) || !strings.Contains(body, `dir="`+string(locale.Direction)+`"`) {
			t.Fatalf("%s: the page is not in the reader's locale", language)
		}
		for _, internal := range []string{"No such workspace route", "This cell serves", "Promotion workspace", "?worker=", " cell "} {
			if strings.Contains(body, internal) {
				t.Fatalf("%s: the page shows the developer message %q", language, internal)
			}
		}
	}
	if productUnknownText(productui.ResolveProductLocale("en-US"), "title") != "This page does not exist" {
		t.Fatal("the unknown-address title changed")
	}
	if productUnknownText(productui.ResolveProductLocale("de-DE"), "title") == "This page does not exist" || productUnknownText(productui.ResolveProductLocale("ar"), "title") == "This page does not exist" {
		t.Fatal("the unknown-address title is not localized")
	}

	// An address with no near match offers Home alone.
	lost := agentUX043Get(t, "/workspace/app/nothing-like-this").Body.String()
	card := lost[strings.Index(lost, `class="surface empty-state workspace-page-problem"`):]
	card = card[:strings.Index(card, "</section>")]
	if strings.Count(card, "<a ") != 1 || !strings.Contains(card, `href="`+productui.Path(productui.PageHome)+`"`) {
		t.Fatalf("an address with no near match offers something other than Home: %s", card)
	}

	// The old Agents address goes to the Agents page and keeps its query.
	for _, old := range []string{"/workspace/app/agents", "/workspace/app/agents/"} {
		redirect := agentUX043Get(t, old+"?locale=de-DE&task=task-7")
		if redirect.Code != http.StatusSeeOther || redirect.Header().Get("Location") != productui.Path(productui.PageAgents)+"?locale=de-DE&task=task-7" {
			t.Fatalf("%s answered %d to %q", old, redirect.Code, redirect.Header().Get("Location"))
		}
	}
}

// The unknown-address page is styled by the stylesheet its own policy admits.
func TestTodo_AGENTUX_043_Browser(t *testing.T) {
	response := agentUX043Get(t, "/workspace/app/nowhere/agents")
	body := response.Body.String()
	open, end := strings.Index(body, "<style>"), strings.Index(body, "</style>")
	if open < 0 || end < open {
		t.Fatal("the unknown-address page inlines no stylesheet")
	}
	sheet := body[open+len("<style>") : end]
	if !strings.Contains(response.Header().Get("Content-Security-Policy"), sha256Source(sheet)) {
		t.Fatal("the unknown-address page's stylesheet is not admitted by its policy; it would render as bare text")
	}
	if !strings.Contains(sheet, `.workspace-page-problem .action-row{display:flex`) || !strings.Contains(sheet, "--hcm-color-brand-primary") {
		t.Fatal("the page is served without the workspace theme or the card's layout")
	}
}
