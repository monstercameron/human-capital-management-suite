package chatui

import (
	"strings"
	"testing"
)

// TestIssue3696_RecentIconGutter: the search box's magnifier is absolutely
// placed over its input (".rail-search .chat-icon"), and each recent entry sits
// inside that box. The entry's own icon must reset that placement, or it is
// drawn over the first characters of the entry text.
func TestIssue3696_RecentIconGutter(t *testing.T) {
	const selector = ".chat-workspace .rail-search-recent-item .chat-icon{"
	at := strings.Index(chatlane9Styles, selector)
	if at < 0 {
		t.Fatalf("the recent entry icon has no rule of its own: %s", chatlane9Styles)
	}
	decl := chatlane9Styles[at+len(selector):]
	decl = decl[:strings.Index(decl, "}")]
	for _, want := range []string{"position:static", "transform:none"} {
		if !strings.Contains(decl, want) {
			t.Errorf("the recent entry icon rule lacks %q, so the box's magnifier placement reaches it: %s", want, decl)
		}
	}
	// The entry's icon is rendered inside the recent list, which sits under the box.
	markup := renderNode(t, rail(func() Model {
		m := chatux002Model("en-US")
		m.SearchRecent = []string{"open enrollment is:mine"}
		return m
	}(), handlers{}))
	list := markup[strings.Index(markup, `class="rail-search-recent"`):]
	if !strings.Contains(list, `class="chat-icon icon-search"`) {
		t.Fatalf("the recent entry has no icon to place: %s", list)
	}
}
