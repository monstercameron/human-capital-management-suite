package chatui_test

import (
	"fmt"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_098_Browser reads the whole Chat page as a tree at 1280,
// 768 and 390 px: with search, a row menu, Chat preferences and the Archived
// section in play, Archived is the last section of the scrolling list, every
// conversation row sits above it, and no footer band element exists anywhere
// to be drawn over a row. Live geometry (no two boxes overlap) is specified
// in tools/uxqual/browser/chatbug098_archived_sidebar.spec.mjs, which needs a
// running server and browser and is not executed here.
func TestTodo_CHATBUG_098_Browser(t *testing.T) {
	setup := func(menu, search string) func(*chatui.Model) {
		return func(m *chatui.Model) {
			m.RailMenuID = menu
			m.Search = search
			m.Callbacks.SavePreferences = func(chatui.Preferences) {}
			m.ChatFeatures = &chatui.ChatFeatures{Status: true}
			m.ChannelStatuses = map[string]chatui.ChannelStatusView{
				"old-ideas": {Status: chat.ChannelStatus{ConversationID: "old-ideas", Name: "old-ideas", Status: chatpolicy.StatusArchived}},
			}
		}
	}
	for _, locale := range w7Locales {
		for _, width := range []int{1280, 768, 390} {
			for _, state := range []struct {
				name         string
				menu, search string
			}{
				{"plain", "", ""},
				{"menu", "general", ""},
				{"menu on archived row", "old-ideas", ""},
				{"searching", "", "holiday"},
				{"searching with menu", "general", "holiday"},
			} {
				name := fmt.Sprintf("%s/%d/%s", locale, width, state.name)
				page := w7Parse(t, w7Page(t, locale, setup(state.menu, state.search)))
				rail := w7One(t, name+" sidebar", page, func(n *xhtml.Node) bool { return w7Class(n, "chat-rail") })
				if len(w7All(rail, func(n *xhtml.Node) bool { return w7Class(n, "chatstate-archived") })) != 0 {
					t.Errorf("%s: the fixed footer band is back in the rail", name)
				}
				scroll := w7One(t, name+" scrolling list", rail, func(n *xhtml.Node) bool { return w7Class(n, "rail-scroll") })
				archived := w7One(t, name+" Archived section", scroll, func(n *xhtml.Node) bool { return w7Attr(n, "data-section-id") == "archived" })
				sections := w7All(scroll, func(n *xhtml.Node) bool { return w7Class(n, "sidebar-section") })
				if archived.Parent != scroll || len(sections) == 0 || sections[len(sections)-1] != archived {
					t.Errorf("%s: Archived is not the last section in the scrolling list", name)
				}
				rows := w7All(scroll, func(n *xhtml.Node) bool { return w7Class(n, "chat-rail-row") })
				if len(rows) == 0 {
					t.Errorf("%s: the list has no conversation rows", name)
				}
				seen := map[*xhtml.Node]bool{}
				var walk func(*xhtml.Node)
				walk = func(n *xhtml.Node) {
					seen[n] = true
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						walk(c)
					}
				}
				walk(archived)
				for _, row := range rows {
					if seen[row] {
						t.Errorf("%s: a conversation row is inside Archived", name)
					}
				}
				// Chat preferences rides the rail head as a top-layer popover.
				prefs := w7One(t, name+" Chat preferences", rail, func(n *xhtml.Node) bool { return w7Attr(n, "data-chat-layer") == "chat-prefs" })
				if w7Attr(prefs, "popover") != "manual" {
					t.Errorf("%s: Chat preferences is not a top-layer popover", name)
				}
				var underScroll func(*xhtml.Node) bool
				underScroll = func(n *xhtml.Node) bool {
					if n == nil {
						return false
					}
					if n == scroll {
						return true
					}
					return underScroll(n.Parent)
				}
				if underScroll(prefs) {
					t.Errorf("%s: Chat preferences sits inside the scrolling list", name)
				}
			}
		}
	}
}
