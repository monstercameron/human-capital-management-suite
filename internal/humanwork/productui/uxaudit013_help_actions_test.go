package productui

import (
	"strings"
	"testing"
)

// TestTodo_UXAUDIT_013_Regression keeps Help's support actions task-oriented.
// A generic "Open" label is not enough context when several destinations are
// adjacent, particularly for keyboard and assistive-technology users.
func TestTodo_UXAUDIT_013_Regression(t *testing.T) {
	doc, err := Render(testView(PageHelp))
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"Find an answer", "Request HR support"} {
		if !strings.Contains(doc, ">"+action+"</a>") {
			t.Fatalf("Help missing outcome-oriented action %q", action)
		}
	}
	if strings.Contains(doc, `class="button secondary compact">Open</a>`) {
		t.Fatal("Help uses an ambiguous generic support action")
	}
}

// TestTodo_UXAUDIT_013_SecurityEmpty proves a view with no admitted support
// destinations remains useful without leaking a denied route or pretending a
// request/article exists.
func TestTodo_UXAUDIT_013_SecurityEmpty(t *testing.T) {
	view := testView(PageHelp)
	view.EffectivePermissions = []RolePagePermission{{Page: PageHelp, View: true}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/help/knowledge-search", "/help/hr-service-request", "/help/confidential-case", "/help/case-status"} {
		if strings.Contains(doc, route) {
			t.Fatalf("empty Help leaked denied route %q", route)
		}
	}
	if !strings.Contains(doc, "HR administrator") {
		t.Fatal("empty Help omitted safe escalation guidance")
	}
	if strings.Contains(doc, "article 1") || strings.Contains(doc, "request 4471") {
		t.Fatal("empty Help fabricated governed data")
	}
}

// TestTodo_UXAUDIT_013_SearchBoundary proves that an empty or denied search
// request stays a safe progressive-enhancement shell. Article results are
// owned by the governed help service; SSR must not manufacture or disclose
// them while that service is unavailable.
func TestTodo_UXAUDIT_013_SearchBoundary(t *testing.T) {
	for _, query := range []string{"", "pay statement"} {
		view := testView(PageKnowledgeSearch)
		view.Query = query
		view.EffectivePermissions = []RolePagePermission{{Page: PageKnowledgeSearch, View: false}}
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		for _, forbidden := range []string{"article 1", "restricted article shown", "9 results for leave", "request 4471"} {
			if strings.Contains(doc, forbidden) {
				t.Fatalf("query %q disclosed fabricated governed data %q", query, forbidden)
			}
		}
		if strings.Contains(doc, `href="/workspace/app/help/knowledge-search?q=`) {
			t.Fatalf("query %q disclosed search terms in a destination link", query)
		}
	}
}

// TestTodo_UXAUDIT_013_GlobalSearchAuthorization proves Help relies on the
// existing authorized global-search projection rather than adding an
// ungoverned Help-side index.
func TestTodo_UXAUDIT_013_GlobalSearchAuthorization(t *testing.T) {
	view := testView(PageHelp)
	view.EffectivePermissions = []RolePagePermission{{Page: PageHelp, View: true}}
	items := authorizedGlobalSearchItems(view, globalSearchItems(view))
	for _, item := range items {
		if strings.Contains(item.Href, "/help/knowledge-search") || strings.Contains(item.Href, "/help/hr-service-request") {
			t.Fatalf("denied Help service route entered global search: %s", item.Href)
		}
	}
	if got := SearchGlobalItems(items, "", 10); len(got) != 0 {
		t.Fatalf("empty global search query returned %d results", len(got))
	}
}
