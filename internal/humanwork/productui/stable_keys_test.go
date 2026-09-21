package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func nodeKey(node ui.Node) string {
	if node.Key != "" {
		return node.Key
	}
	if key, ok := node.Props["key"].(string); ok {
		return key
	}
	return ""
}

// TestShellChildrenAreKeyedSoRefreshDoesNotRemount pins the UXLIVE-028 live
// defect: the refresh progress bar arriving at the front of the page frame
// shifted the page head and the page, re-mounting both. Every frame child
// and both page-head children now carry stable keys, and a key the caller
// set on the page (the journey content's address key) is kept.
func TestShellChildrenAreKeyedSoRefreshDoesNotRemount(t *testing.T) {
	page := html.Div(html.Props{Class: "people-page"})
	if got := nodeKey(keyUnlessKeyed(page, "page-content")); got != "page-content" {
		t.Fatalf("unkeyed page key = %q", got)
	}
	journey := html.Div(html.Props{Key: "/workspace/app/journeys?journey=a"})
	if got := nodeKey(keyUnlessKeyed(journey, "page-content")); got != "/workspace/app/journeys?journey=a" {
		t.Fatalf("caller key replaced: %q", got)
	}
	props := ui.CreateElement(ui.Fragment)
	props.Props = map[string]any{"key": "own"}
	if got := nodeKey(keyUnlessKeyed(props, "page-content")); got != "own" {
		t.Fatalf("props key replaced: %q", got)
	}
	if keyUnlessKeyed(nil, "x") != nil {
		t.Fatal("nil node gained a value")
	}

	for _, refreshing := range []bool{false, true} {
		view := testView(PagePeople)
		view.Refreshing = refreshing
		frame := pageFrame(view, html.Div(html.Props{Class: "people-page"}), true)
		markup, err := ui.RenderToString(frame)
		if err != nil {
			t.Fatal(err)
		}
		if refreshing != strings.Contains(markup, "network-progress") {
			t.Fatalf("refreshing=%v progress bar presence wrong", refreshing)
		}
	}
	heading := PageHeading(PageHeadingProps{Identity: PageIdentity{Page: PagePerson, Title: "Amara"}})
	if len(heading.Children) < 2 {
		t.Fatalf("page head children = %d", len(heading.Children))
	}
	keys := []string{}
	for _, child := range heading.Children {
		if node, ok := child.(ui.Node); ok && node != nil {
			keys = append(keys, nodeKey(node))
		}
	}
	if strings.Join(keys, ",") != "page-title-block" {
		t.Fatalf("page head keys without a trail = %v, want only the keyed title block", keys)
	}
}
