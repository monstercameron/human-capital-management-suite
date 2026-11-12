package productui

import (
	"regexp"
	"strings"
	"testing"
)

var (
	chatDesignShellClass = regexp.MustCompile(`class="(app-shell[^"]*)"`)
	chatDesignNavToggle  = regexp.MustCompile(`<a[^>]*class="header-nav-toggle"[^>]*>`)
)

func chatDesignShell(t *testing.T, view View) (document, class, toggle string) {
	t.Helper()
	document, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	found := chatDesignShellClass.FindStringSubmatch(document)
	if found == nil {
		t.Fatal("the document has no application shell")
	}
	toggle = chatDesignNavToggle.FindString(document)
	if toggle == "" {
		t.Fatal("the shell has no navigation toggle")
	}
	return document, " " + found[1] + " ", toggle
}

// #3771: Chat opens with the product navigation as its icon rail. The toggle
// offers to expand it, in every locale, and links that leave Chat do not carry
// the rail with them. Other pages are unchanged.
func TestChatDesign_3771_ChatOpensWithTheNavigationRail(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := testView(PageChat)
		view.Locale = ResolveProductLocalePreference(locale, "")
		document, class, toggle := chatDesignShell(t, view)
		if !strings.Contains(class, " nav-collapsed ") || strings.Contains(class, " nav-expanded ") {
			t.Fatalf("%s: Chat's shell is %q, want the collapsed rail", locale, class)
		}
		for _, want := range []string{`aria-expanded="false"`, `aria-label="` + view.Locale.Text("nav.expand") + `"`, `nav=expanded`, `aria-controls="workspace-navigation"`} {
			if !strings.Contains(toggle, want) {
				t.Errorf("%s: the toggle on Chat's rail lacks %s: %s", locale, want, toggle)
			}
		}
		// The rail is Chat's default, not the person's choice: nothing links
		// onward with nav=collapsed, and no form posts it.
		if strings.Contains(document, "nav=collapsed") || strings.Contains(document, `name="nav" value="collapsed"`) {
			t.Errorf("%s: Chat's default rail leaks into its links as a collapse choice", locale)
		}
	}

	// The person's own choice wins, both ways.
	expanded := testView(PageChat)
	expanded.NavExpanded = true
	_, class, toggle := chatDesignShell(t, expanded)
	if strings.Contains(class, " nav-collapsed ") || !strings.Contains(class, " nav-expanded ") {
		t.Fatalf("an explicit expand on Chat renders %q", class)
	}
	if !strings.Contains(toggle, `aria-expanded="true"`) || !strings.Contains(toggle, "nav=collapsed") || !strings.Contains(toggle, `aria-label="`+expanded.Locale.Text("nav.collapse")+`"`) {
		t.Errorf("the toggle of an expanded Chat does not offer to collapse: %s", toggle)
	}
	collapsed := testView(PageChat)
	collapsed.NavCollapsed = true
	if _, class, _ := chatDesignShell(t, collapsed); !strings.Contains(class, " nav-collapsed ") {
		t.Fatalf("an explicit collapse on Chat renders %q", class)
	}

	// No change on other pages, the Agents page under Chat included.
	for _, page := range []PageID{PageHome, PagePeople, PageAgents} {
		_, class, toggle := chatDesignShell(t, testView(page))
		if strings.Contains(class, " nav-collapsed ") || !strings.Contains(toggle, `aria-expanded="true"`) {
			t.Errorf("%s: the navigation is collapsed by default: %q %s", page, class, toggle)
		}
		if ChatRailDefault(page) {
			t.Errorf("%s is treated as a Chat route", page)
		}
	}
}

// What is stored for Chat is only what the person chose, and a stored choice
// is read back only for Chat and only under an expanded workspace preference.
func TestChatDesign_3771_OnlyAChoiceIsStoredForChat(t *testing.T) {
	for _, tc := range []struct {
		name                string
		page                PageID
		collapsed, expanded bool
		want                string
	}{
		{"chat default", PageChat, false, false, ""},
		{"chat expanded", PageChat, false, true, "expanded"},
		{"chat collapsed", PageChat, true, false, "collapsed"},
		{"home collapsed", PageHome, true, false, ""},
		{"agents expanded", PageAgents, false, true, ""},
	} {
		view := testView(tc.page)
		view.NavCollapsed, view.NavExpanded = tc.collapsed, tc.expanded
		if got := ChatNavigationChoice(view); got != tc.want {
			t.Errorf("%s: stored choice %q, want %q", tc.name, got, tc.want)
		}
	}
	expanded := map[string]string{ChatNavigationFilter: "expanded"}
	if !ChatNavigationStoredExpanded(PageChat, false, expanded) {
		t.Error("a stored expand for Chat is not read back on Chat")
	}
	for name, got := range map[string]bool{
		"another page":            ChatNavigationStoredExpanded(PageHome, false, expanded),
		"workspace collapse wins": ChatNavigationStoredExpanded(PageChat, true, expanded),
		"stored collapse":         ChatNavigationStoredExpanded(PageChat, false, map[string]string{ChatNavigationFilter: "collapsed"}),
		"nothing stored":          ChatNavigationStoredExpanded(PageChat, false, nil),
	} {
		if got {
			t.Errorf("%s: Chat opens expanded", name)
		}
	}
	// A request's explicit expand reaches the view; a collapse overrides it.
	view := ApplyRequest(testView(PageChat), PageRequest{Page: PageChat, NavExpanded: true})
	if !view.NavExpanded || view.navRailCollapsed() {
		t.Error("a request's explicit expand did not reach the Chat view")
	}
	view = ApplyRequest(testView(PageChat), PageRequest{Page: PageChat, NavExpanded: true, NavCollapsed: true})
	if view.NavExpanded || !view.navRailCollapsed() {
		t.Error("a request that collapses still renders expanded")
	}
}
