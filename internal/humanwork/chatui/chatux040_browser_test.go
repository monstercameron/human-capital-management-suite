package chatui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	xhtml "golang.org/x/net/html"
)

// TestTodo_CHATUX_040_Browser: with the product's own catalog in each language,
// every item of a row menu and of a section menu leads with a glyph, the menu
// words are not copy keys, and the Saved panel says "Done yesterday" in its own
// words with the note that explains why All counts more than To do plus Done.
func TestTodo_CHATUX_040_Browser(t *testing.T) {
	for locale, done := range map[string]string{"en-US": "Done yesterday", "de-DE": "Erledigt gestern", "ar": "تم أمس"} {
		for _, menuID := range []string{"general", "section:custom-1"} {
			page := lane3Page(t, locale, func(m *chatui.Model) {
				sales := chatui.Conversation{ID: "sales", Name: "sales", Kind: chatui.PublicChannel, Joined: true}
				general := m.Conversations[0]
				m.Sections = []chatui.SidebarSection{{ID: "channels", Name: "Channels", Chats: []chatui.Conversation{general}}, {ID: "custom-1", Name: "Projects", Chats: []chatui.Conversation{sales}}, {ID: "custom-2", Name: "Reading", Chats: nil}}
				m.RailMenuID = menuID
				m.Callbacks.MarkConversationUnread, m.Callbacks.LeaveConversation, m.Callbacks.SetFavorite = func(string) {}, func(string) {}, func(string, bool) {}
				m.Callbacks.MoveConversationSection, m.Callbacks.SetConversationNotification, m.Callbacks.CreateSectionFor = func(string, string) {}, func(string, chatui.NotificationMode) {}, func(string, string) {}
				m.Callbacks.MoveConversationOrder, m.Callbacks.RenameSection, m.Callbacks.ReorderSection, m.Callbacks.RemoveSection = func(string, int) {}, func(string, string) {}, func(string, int) {}, func(string) {}
			})
			lane3NoLeaks(t, locale+" "+menuID, page)
			root, err := xhtml.Parse(strings.NewReader(page))
			if err != nil {
				t.Fatal(err)
			}
			items, withGlyph := 0, 0
			var walk func(n *xhtml.Node, inMenu bool)
			walk = func(n *xhtml.Node, inMenu bool) {
				if n.Type == xhtml.ElementNode {
					for _, a := range n.Attr {
						if a.Key == "class" && strings.Contains(" "+a.Val+" ", " rail-row-menu ") {
							inMenu = true
						}
					}
					if inMenu && n.Data == "button" {
						items++
						for c := n.FirstChild; c != nil; c = c.NextSibling {
							if c.Type != xhtml.ElementNode {
								continue
							}
							class := ""
							for _, a := range c.Attr {
								if a.Key == "class" {
									class = a.Val
								}
							}
							if strings.Contains(class, "chat-icon") || strings.Contains(class, "rail-menu-check") {
								withGlyph++
							}
							break
						}
					}
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c, inMenu)
				}
			}
			walk(root, false)
			if items < 3 {
				t.Errorf("%s %s: the menu holds %d items", locale, menuID, items)
			}
			if items != withGlyph {
				t.Errorf("%s %s: %d of %d menu items have no glyph", locale, menuID, items-withGlyph, items)
			}
		}

		ctx := productui.ResolveProductLocale(locale)
		now := time.Date(2026, time.October, 2, 11, 0, 0, 0, time.UTC)
		view := chatui.SavedMessagesView{Locale: ctx.Resolved, Tab: "all", Now: now, TodoCount: 0, DoneCount: 1, AllCount: 2,
			Rows: []chatui.SavedMessageRow{
				{TenantID: "t", ConversationID: "random", PostID: "p1", Author: "Walt Brennan", Channel: "random", InChannel: true, SentAt: now.Add(-48 * time.Hour), Body: "Can someone take a look?", Availability: "readable", Sequence: 1, Done: true, DoneAt: now.Add(-24 * time.Hour)},
				{TenantID: "t", ConversationID: "gone", PostID: "p2", Availability: "deleted"},
			}}
		markup, err := ui.RenderToString(chatui.RenderSavedMessages(view))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, done) {
			t.Errorf("%s: the Saved panel does not say %q", locale, done)
		}
		if strings.Contains(markup, "Done Yesterday") || strings.Contains(markup, "Erledigt Gestern") {
			t.Errorf("%s: a day word is capitalised inside a sentence", locale)
		}
		if !strings.Contains(markup, `data-saved-all-note="true"`) {
			t.Errorf("%s: All 2 beside To do 0 and Done 1 is not explained", locale)
		}
		lane3NoLeaks(t, locale+" saved", markup)
	}
}
