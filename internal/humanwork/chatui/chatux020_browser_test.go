package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATUX_020_Browser: the sidebar's row menu, draft pencil and collapsed
// section with the product's own catalog in each language: the menu's words,
// the heading over the notification choices, and no copy key.
func TestTodo_CHATUX_020_Browser(t *testing.T) {
	type words struct{ unread, group, leave, move, draft string }
	for locale, w := range map[string]words{
		"en-US": {"Mark as unread", "Notify me about", "Leave channel", "Move to Projects", "Unsent draft"},
		"de-DE": {"Als ungelesen markieren", "Benachrichtigen bei", "Kanal verlassen", "Nach Projects verschieben", "Nicht gesendeter Entwurf"},
		"ar":    {"تحديد كغير مقروءة", "أبلغني عن", "مغادرة القناة", "نقل إلى Projects", "مسودة غير مرسلة"},
	} {
		page := lane3Page(t, locale, func(m *chatui.Model) {
			sales := chatui.Conversation{ID: "sales", Name: "sales", Kind: chatui.PublicChannel, Joined: true}
			general := m.Conversations[0]
			m.Sections = []chatui.SidebarSection{{ID: "channels", Name: "Channels", Chats: []chatui.Conversation{general}, Collapsed: true}, {ID: "custom-1", Name: "Projects", Chats: []chatui.Conversation{sales}}}
			m.RailMenuID = "general"
			m.Preferences.Drafts = map[string]string{"sales": "half a thought"}
			m.Callbacks.MarkConversationUnread, m.Callbacks.LeaveConversation = func(string) {}, func(string) {}
			m.Callbacks.MoveConversationSection, m.Callbacks.SetConversationNotification = func(string, string) {}, func(string, chatui.NotificationMode) {}
		})
		lane3NoLeaks(t, locale+" sidebar", page)
		for _, want := range []string{w.unread, w.group, w.leave, w.move, w.draft, `role="menuitemradio"`, `id="rail-menu-notify-heading"`, `class="rail-fixed"`} {
			if !strings.Contains(page, want) {
				t.Errorf("%s: the sidebar misses %q", locale, want)
			}
		}
		// The collapsed Channels section still shows the open conversation.
		channels := page[strings.Index(page, `data-section-id="channels"`):]
		channels = channels[:strings.Index(channels, `data-section-id="custom-1"`)]
		if !strings.Contains(channels, `data-id="general"`) {
			t.Errorf("%s: the collapsed section hides the open conversation", locale)
		}
	}
}
