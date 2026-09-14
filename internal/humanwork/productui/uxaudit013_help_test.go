package productui

import (
	"strings"
	"testing"
)

// TestTodo_UXAUDIT_013 proves that Help is a usable destination rather than a
// collection of unconnected navigation shortcuts. The assertions intentionally
// inspect the rendered contract so a future refactor cannot silently remove a
// support path while the lower-level destination components still compile.
func TestTodo_UXAUDIT_013(t *testing.T) {
	doc, err := Render(testView(PageHelp))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="help-page `,
		`hcm-help-action="knowledge-search"`,
		`/workspace/app/help/knowledge-search`,
		`/workspace/app/help/hr-service-request`,
		`Find an answer`,
		`Request HR support`,
		`Promotion setup and support`,
		`HR administrator`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("Help missing %q", want)
		}
	}
}

// TestTodo_UXAUDIT_013_SSRDeterminism covers the deterministic, progressive-enhancement
// surface used by the browser journey without claiming SSR is browser coverage
// articles or support cases exist before their governed service publishes.
func TestTodo_UXAUDIT_013_SSRDeterminism(t *testing.T) {
	first, err := Render(testView(PageHelp))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageHelp))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("Help rendered nondeterministically")
	}
	if strings.Contains(first, "article 1") || strings.Contains(first, "request 4471") {
		t.Fatal("Help invented governed support data")
	}
}

// TestTodo_UXAUDIT_013_Accessibility checks names, semantic search/action
// affordances and status-safe copy in the rendered component contract.
func TestTodo_UXAUDIT_013_Accessibility(t *testing.T) {
	doc, err := Render(testView(PageHelp))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`aria-labelledby="help-search-title"`,
		`id="help-search-title"`,
		`class="button primary"`,
		`role="note"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("Help accessibility contract missing %q", want)
		}
	}
}

// TestTodo_UXAUDIT_013_Security proves that Help follows the resolved
// permission projection and does not disclose denied support destinations.
func TestTodo_UXAUDIT_013_Security(t *testing.T) {
	view := testView(PageHelp)
	view.EffectivePermissions = []RolePagePermission{
		{Page: PageHelp, View: true},
		{Page: PageKnowledgeSearch, View: false},
		{Page: PageHRServiceRequest, View: false},
		{Page: PageConfidentialCase, View: false},
		{Page: PageCaseStatus, View: false},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, denied := range []string{"/help/knowledge-search", "/help/hr-service-request", "/help/confidential-case", "/help/case-status"} {
		if strings.Contains(doc, denied) {
			t.Fatalf("denied Help destination leaked: %s", denied)
		}
	}
	if !strings.Contains(doc, "HR administrator") {
		t.Fatal("denied Help state lost non-disclosing escalation guidance")
	}
}

// TestTodo_UXAUDIT_013_I18N ensures Help remains complete in every supported
// locale and never emits an unresolved translation key.
func TestTodo_UXAUDIT_013_I18N(t *testing.T) {
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageHelp), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("locale %s: %v", code, err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("locale %s emitted an unresolved translation key", code)
		}
		if !strings.Contains(doc, statefulHref(view, PageKnowledgeSearch)) {
			t.Fatalf("locale %s lost the authorized search destination", code)
		}
	}
}
