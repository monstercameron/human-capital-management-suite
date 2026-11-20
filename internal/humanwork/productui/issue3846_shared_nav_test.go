package productui

import (
	"strings"
	"testing"
)

// #3846: Conversations and Agents are two pages of the Chat section and share
// one navigation state: the rail by default, then whatever the person chose.
func TestIssue3846_ChatAndAgentsShareOneNavigationState(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, page := range []PageID{PageChat, PageAgents} {
			view := testView(page)
			view.Locale = ResolveProductLocalePreference(locale, "")
			document, class, toggle := chatDesignShell(t, view)
			if !strings.Contains(class, " nav-collapsed ") || strings.Contains(class, " nav-expanded ") {
				t.Errorf("%s %s: shell is %q, want the collapsed rail", locale, page, class)
			}
			if !strings.Contains(toggle, `aria-expanded="false"`) || !strings.Contains(toggle, "nav=expanded") {
				t.Errorf("%s %s: toggle does not offer to expand: %s", locale, page, toggle)
			}
			if strings.Contains(document, "nav=collapsed") {
				t.Errorf("%s %s: the default rail leaks into links as a collapse choice", locale, page)
			}
		}
	}
	// The person's choice made on one page is the other's too.
	for _, page := range []PageID{PageChat, PageAgents} {
		view := testView(page)
		view.NavExpanded = true
		if _, class, _ := chatDesignShell(t, view); strings.Contains(class, " nav-collapsed ") || !strings.Contains(class, " nav-expanded ") {
			t.Errorf("%s: an explicit expand renders %q", page, class)
		}
		if got := ChatNavigationChoice(view); got != "expanded" {
			t.Errorf("%s: stored choice %q, want expanded", page, got)
		}
		if !ChatNavigationStoredExpanded(page, false, map[string]string{ChatNavigationFilter: "expanded"}) {
			t.Errorf("%s: a stored expand is not read back", page)
		}
	}
	if ChatRailDefault(PageHome) || ChatRailDefault(PagePeople) {
		t.Error("pages outside the Chat section open as a rail")
	}
}
