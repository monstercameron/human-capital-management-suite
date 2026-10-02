package chatui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATUX_019_Browser: the notification radios, the pinned item's
// time and Unpin, and a reaction's name, with the product's own catalog in
// each language and no copy key on the page.
func TestTodo_CHATUX_019_Browser(t *testing.T) {
	type words struct{ notifications, jump, unpin, pinnedBy, eyes string }
	for locale, w := range map[string]words{
		"en-US": {"Notifications for me", "Jump to message", "Unpin", "Pinned by Walt Brennan", "You and Jake Sullivan reacted with eyes"},
		"de-DE": {"Benachrichtigungen für mich", "Zur Nachricht springen", "Lösen", "Angeheftet von Walt Brennan", "Sie und Jake Sullivan haben mit Augen reagiert"},
		"ar":    {"الإشعارات الخاصة بي", "الانتقال إلى الرسالة", "إلغاء التثبيت", "ثبّتها Walt Brennan", "تفاعل أنت وJake Sullivan بـ عينان"},
	} {
		page := lane3Page(t, locale, func(m *chatui.Model) {
			m.ShowDetails = true
			m.Callbacks.SetConversationNotification = func(string, chatui.NotificationMode) {}
			m.Callbacks.JumpToPin, m.Callbacks.CopyPinReference, m.Callbacks.Unpin = func(string, uint64) {}, func(string) {}, func(string) {}
			m.ChannelPins = []chatui.ChannelPin{{PostID: "p1", Author: "Jake Sullivan", Body: "**Policy** note", Sequence: 3, PinnedBy: "Walt Brennan", PinnedAt: time.Now()}}
			m.Messages = []chatui.Message{{ID: "msg", AuthorID: "jake", Author: "Jake Sullivan", Body: "Seen", Chips: []chatui.ReactionChip{{Emoji: "👀", Count: 2, Mine: true, PeopleIDs: []string{"jake"}}}}}
			m.Preferences.Notifications = map[string]chatui.NotificationMode{"general": chatui.NotifyMention}
		})
		lane3NoLeaks(t, locale+" details", page)
		for _, want := range []string{w.notifications, `type="radio"`, `role="radiogroup"`, w.jump, w.unpin, w.pinnedBy, "<strong>", w.eyes} {
			if !strings.Contains(page, want) {
				t.Errorf("%s: the page misses %q", locale, want)
			}
		}
		if strings.Contains(page, "**Policy**") {
			t.Errorf("%s: the pinned preview shows raw markdown", locale)
		}
	}
}
