package chatui_test

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatbug090Rooms is a sidebar long enough to scroll: enough rows that the first,
// a middle and the last row are three different rows.
func chatbug090Rooms() []chatui.Conversation {
	rooms := []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true, MemberCount: 18}}
	for _, name := range []string{"random", "people-ops", "announcements", "design", "payroll", "recruiting", "safety"} {
		rooms = append(rooms, chatui.Conversation{ID: name, Name: name, Kind: chatui.PublicChannel, Joined: true, MemberCount: 5})
	}
	return append(rooms,
		chatui.Conversation{ID: "loretta", Name: "Loretta Haynes", Kind: chatui.DirectMessage, Joined: true},
		chatui.Conversation{ID: "huddle", Name: "Q4 hiring huddle", Kind: chatui.GroupChat, Joined: true, MemberCount: 4})
}

// TestTodo_CHATBUG_090_Browser opens the sidebar row menu from the first, a
// middle and the last row of a long list in each language: the menu is drawn
// from the row's own button (a labelled, expanded menu button), as a menu layer
// with its items, and Archived is the last collapsed section in the scrolling
// list rather than a band positioned over its rows.
func TestTodo_CHATBUG_090_Browser(t *testing.T) {
	rooms := chatbug090Rooms()
	setup := func(open string) func(*chatui.Model) {
		return func(m *chatui.Model) {
			m.Conversations = rooms
			m.RailMenuID = open
			m.ChatFeatures = &chatui.ChatFeatures{Status: true}
			m.ChannelStatuses = map[string]chatui.ChannelStatusView{
				"old-ideas": {Status: chat.ChannelStatus{ConversationID: "old-ideas", Name: "old-ideas", Status: chatpolicy.StatusArchived}},
			}
			m.Callbacks.OpenConversationDetails = func(string) {}
			m.Callbacks.LeaveConversation = func(string) {}
		}
	}
	for _, locale := range w7Locales {
		// Closed: every row has its own menu button, collapsed.
		closed := w7Parse(t, w7Page(t, locale, setup("")))
		rail := w7One(t, locale+" sidebar", closed, func(n *xhtml.Node) bool { return w7Class(n, "chat-rail") })
		scroll := w7One(t, locale+" scrolling list", rail, func(n *xhtml.Node) bool { return w7Class(n, "rail-scroll") })
		more := w7All(scroll, func(n *xhtml.Node) bool {
			return w7Class(n, "rail-row-more") && w7Attr(n, "data-action") == "rail-menu" && !w7Class(n, "section-more")
		})
		if len(more) != len(rooms) {
			t.Fatalf("%s: %d row menu buttons for %d rows", locale, len(more), len(rooms))
		}
		for _, button := range more {
			if w7Attr(button, "aria-haspopup") != "menu" || w7Attr(button, "aria-expanded") != "false" || w7Attr(button, "aria-label") == "" || w7Attr(button, "type") != "button" || w7Has(button, "disabled") {
				t.Errorf("%s: the menu button of %s is not an enabled, labelled, collapsed menu button", locale, w7Attr(button, "data-id"))
			}
		}
		if len(w7All(rail, func(n *xhtml.Node) bool { return w7Attr(n, "role") == "menu" && w7Class(n, "rail-row-menu") })) != 0 {
			t.Errorf("%s: a row menu is drawn while none is open", locale)
		}

		// Archived is a normal last section in the list and starts collapsed.
		archived := w7One(t, locale+" Archived section", scroll, func(n *xhtml.Node) bool { return w7Attr(n, "data-section-id") == "archived" })
		sections := w7All(scroll, func(n *xhtml.Node) bool { return w7Class(n, "sidebar-section") })
		if archived.Parent != scroll || len(sections) == 0 || sections[len(sections)-1] != archived {
			t.Errorf("%s: Archived is not the last section in the scrolling list", locale)
		}
		if w7Inside(archived, func(n *xhtml.Node) bool { return w7Class(n, "chat-rail-row") }) {
			t.Errorf("%s: the collapsed Archived section draws conversation rows", locale)
		}
		toggle := w7One(t, locale+" Archived toggle", archived, func(n *xhtml.Node) bool { return n.Data == "button" && w7Class(n, "section-title") })
		if w7Text(toggle) == "" || w7Attr(toggle, "aria-expanded") != "false" || w7Attr(toggle, "data-action") != "toggle-archived" {
			t.Errorf("%s: Archived has no labelled, collapsed section toggle", locale)
		}

		// Open from the first, a middle and the last row.
		for name, id := range map[string]string{"first": rooms[0].ID, "middle": rooms[len(rooms)/2].ID, "last": rooms[len(rooms)-1].ID} {
			page := w7Parse(t, w7Page(t, locale, setup(id)))
			button := w7One(t, locale+" "+name+" row button", page, func(n *xhtml.Node) bool {
				return w7Class(n, "rail-row-more") && w7Attr(n, "data-action") == "rail-menu" && w7Attr(n, "data-id") == id
			})
			if w7Attr(button, "aria-expanded") != "true" {
				t.Errorf("%s: the %s row's button does not say its menu is open", locale, name)
			}
			menu := w7One(t, locale+" "+name+" row menu", page, func(n *xhtml.Node) bool { return w7Attr(n, "role") == "menu" && w7Class(n, "rail-row-menu") })
			if w7Attr(menu, "data-chat-layer") != "rail-menu" || w7Attr(menu, "popover") != "manual" {
				t.Errorf("%s: the %s row's menu is not the shared anchored layer", locale, name)
			}
			if label := w7Attr(menu, "aria-label"); label == "" || label != w7Attr(button, "aria-label") {
				t.Errorf("%s: the menu is named %q but its button %q", locale, label, w7Attr(button, "aria-label"))
			}
			items := w7All(menu, func(n *xhtml.Node) bool {
				return n.Data == "button" && (w7Attr(n, "role") == "menuitem" || w7Attr(n, "role") == "menuitemradio")
			})
			if len(items) < 4 {
				t.Errorf("%s: the %s row's menu holds only %d items", locale, name, len(items))
			}
			for _, item := range items {
				if strings.TrimSpace(w7Text(item)) == "" && w7Attr(item, "aria-label") == "" {
					t.Errorf("%s: a menu item of the %s row has no name", locale, name)
				}
			}
			// The menu is a sibling of the list inside the sidebar, never a row of it.
			if w7Inside(menu, func(n *xhtml.Node) bool { return w7Class(n, "rail-scroll") }) {
				t.Errorf("%s: the %s row's menu is drawn inside the scrolling list, where its scroll can move it", locale, name)
			}
		}
	}
}
