package chatui_test

import (
	"fmt"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_092_Browser reads the whole Chat page as a tree at 1280,
// 768 and 390 px: in every sidebar state (menu closed or open on a row, the
// section collapsed or open) the Archived section is the last section of the
// scrolling list and every conversation row sits above it, so the footer can
// never be drawn over a row.
func TestTodo_CHATBUG_092_Browser(t *testing.T) {
	setup := func(menu string) func(*chatui.Model) {
		return func(m *chatui.Model) {
			m.RailMenuID = menu
			m.ChatFeatures = &chatui.ChatFeatures{Status: true}
			m.ChannelStatuses = map[string]chatui.ChannelStatusView{
				"old-ideas": {Status: chat.ChannelStatus{ConversationID: "old-ideas", Name: "old-ideas", Status: chatpolicy.StatusArchived}},
			}
		}
	}
	for _, locale := range w7Locales {
		for _, width := range []int{1280, 768, 390} {
			for _, menu := range []string{"", "general"} {
				name := fmt.Sprintf("%s/%d/menu=%q", locale, width, menu)
				page := w7Parse(t, w7Page(t, locale, setup(menu)))
				rail := w7One(t, name+" sidebar", page, func(n *xhtml.Node) bool { return w7Class(n, "chat-rail") })
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
				// Every row is outside Archived (the section starts collapsed)
				// and above it in the tree.
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
				if w7Inside(archived, func(n *xhtml.Node) bool { return w7Class(n, "rail-row-menu") }) {
					t.Errorf("%s: Archived moved into the open row menu", name)
				}
			}
		}
	}
}
