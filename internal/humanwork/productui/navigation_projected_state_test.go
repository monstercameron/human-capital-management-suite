package productui

import (
	"net/url"
	"testing"
)

func TestTodo_UIPOLISH_004_ProjectedNavigationKeepsCollapseState(t *testing.T) {
	view := testView(PageHelp)
	view.NavCollapsed = true
	item := NavItem{Page: PagePeople, Href: "/workspace/app/people?team=Finance"}
	href := navigationHrefForItem(view, item)
	parsed, err := url.Parse(href)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/workspace/app/people" || parsed.Query().Get("team") != "Finance" || parsed.Query().Get("nav") != "collapsed" {
		t.Fatalf("projected destination lost route or collapsed sidebar state: %q", href)
	}

	view.NavCollapsed = false
	item.Href = "/workspace/app/people?nav=collapsed"
	href = navigationHrefForItem(view, item)
	parsed, err = url.Parse(href)
	if err != nil {
		t.Fatal(err)
	}
	if _, stale := parsed.Query()["nav"]; stale {
		t.Fatalf("expanded shell inherited stale destination collapse state: %q", href)
	}
}
