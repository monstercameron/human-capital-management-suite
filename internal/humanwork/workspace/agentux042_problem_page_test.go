package workspace

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func agentUX042Problem(t *testing.T, status int, page productui.PageID, language string) (string, string) {
	t.Helper()
	h, _ := newShellHandler(t, false)
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+productui.Path(page)+"?locale="+language, nil)
	request.Header.Set("Referer", "http://cell.test/workspace/app/chat/agents")
	recorder := httptest.NewRecorder()
	config := JourneyConfig{Tenant: "ironridge-demo", Subject: "ir-021-dana-lee", Roles: []string{"employees"}}
	h.writeProductProblem(recorder, request, status, productui.ResolveProductLocale(language), page, config)
	if recorder.Code != status {
		t.Fatalf("problem page status = %d, want %d", recorder.Code, status)
	}
	return recorder.Body.String(), recorder.Header().Get("Content-Security-Policy")
}

// A refused or failed page is drawn inside the same shell as every page: top
// bar, navigation and account control, with the message first in the content
// area and its two actions set apart.
func TestTodo_AGENTUX_042(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{{"no access", http.StatusForbidden}, {"could not load", http.StatusInternalServerError}} {
		for _, language := range []string{"en-US", "de-DE", "ar"} {
			body, _ := agentUX042Problem(t, tc.status, productui.PageAgentOperations, language)
			locale := productui.ResolveProductLocale(language)
			for _, want := range []string{`class="app-shell`, `<header class="topbar"`, `class="shell-grid"`, `<aside`, `class="primary-nav"`, `class="brand-cluster"`, `account-popover`, `<main`} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s (%s): the shell is missing %q", tc.name, language, want)
				}
			}
			if strings.Contains(body, "workspace-problem-shell") {
				t.Fatalf("%s (%s): the problem page uses a stripped shell", tc.name, language)
			}
			// The message is the first thing in the content area.
			content := regexp.MustCompile(`<div class="main[^"]*"[^>]*>\s*<section[^>]*class="surface empty-state workspace-page-problem"`)
			if !content.MatchString(body) {
				t.Fatalf("%s (%s): the message card is not first in the content area", tc.name, language)
			}
			// It carries one heading, the sentence, and exactly two actions in
			// the row that spaces them.
			problem := body[strings.Index(body, `class="surface empty-state workspace-page-problem"`):]
			problem = problem[:strings.Index(problem, "</section>")]
			if strings.Count(problem, "<h1") != 1 || !strings.Contains(problem, `class="action-row"`) || strings.Count(problem, `class="button primary"`) != 1 || strings.Count(problem, `class="button secondary"`) != 1 {
				t.Fatalf("%s (%s): the card is not one heading with a primary and a secondary action: %s", tc.name, language, problem)
			}
			message := locale.Text("agents.page_load_failed")
			if tc.status == http.StatusForbidden {
				message = locale.Text("agents.page_access_denied")
			}
			if message == "" || !strings.Contains(problem, message) {
				t.Fatalf("%s (%s): the card does not say %q: %s", tc.name, language, message, problem)
			}
			if !strings.Contains(body, `dir="`+string(locale.Direction)+`"`) {
				t.Fatalf("%s (%s): the page direction is not the locale's", tc.name, language)
			}
		}
	}
	// "No access" and "could not load" are different sentences.
	english := productui.ResolveProductLocale("en-US")
	if english.Text("agents.page_access_denied") == english.Text("agents.page_load_failed") {
		t.Fatal("a refusal and a failed load say the same thing")
	}
}

// The spacing and the rail are rules in the stylesheet the page is served
// with, and that stylesheet is the one the page's own policy admits.
func TestTodo_AGENTUX_042_Browser(t *testing.T) {
	body, policy := agentUX042Problem(t, http.StatusForbidden, productui.PageAgentOperations, "en-US")
	open, end := strings.Index(body, "<style>"), strings.Index(body, "</style>")
	if open < 0 || end < open || strings.Count(body, "<style") != 1 {
		t.Fatal("the problem page does not inline exactly one stylesheet")
	}
	sheet := body[open+len("<style>") : end]
	if !strings.Contains(policy, sha256Source(sheet)) {
		t.Fatal("the problem page's stylesheet is not admitted by its own policy, so it would render unstyled")
	}
	for _, want := range []string{
		// The two actions are separated by the standard gap.
		`.workspace-page-problem .action-row{display:flex;flex-wrap:wrap;align-items:center;gap:var(--hcm-space-2,.5rem)`,
		`.workspace-page-problem .action-row .button{margin:0}`,
		// The card starts at the top of the content area and reads at a measure.
		`.workspace-page-problem{max-inline-size:44rem;gap:12px}`,
		// The rail keeps a column of its own from tablet width up.
		`.app-shell:not(.nav-expanded) .shell-grid { grid-template-columns:72px minmax(0,1fr); }`,
	} {
		if !strings.Contains(sheet, want) && !strings.Contains(strings.ReplaceAll(sheet, " ", ""), strings.ReplaceAll(want, " ", "")) {
			t.Fatalf("the served stylesheet is missing %q", want)
		}
	}
	// Nothing in the card's own rules pushes it down the page.
	for _, rule := range regexp.MustCompile(`\.workspace-page-problem[^{}]*\{[^{}]*\}`).FindAllString(sheet, -1) {
		for _, pushing := range []string{"margin-top:auto", "margin-block-start:auto", "align-self:end", "place-self:end", "position:absolute", "position:fixed"} {
			if strings.Contains(strings.ReplaceAll(rule, " ", ""), pushing) {
				t.Fatalf("a rule moves the message away from the top of the content area: %s", rule)
			}
		}
	}
	if strings.Contains(body, ` style="`) || strings.Contains(body, "<script") {
		t.Fatal("the problem page carries an inline style or a script")
	}
}
