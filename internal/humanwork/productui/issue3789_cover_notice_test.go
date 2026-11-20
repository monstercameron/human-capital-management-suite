package productui

import (
	"strings"
	"testing"
	"time"
)

func TestIssue3789_BellShowsACoverNoticeWithoutText(t *testing.T) {
	at := time.Date(2026, 3, 4, 9, 30, 0, 0, time.UTC)
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := testView(PageChat)
		view.Locale = ResolveProductLocale(locale)
		view.ChatNotifications = []ChatNotification{
			{ID: "c1", Kind: ChatNotificationCover, ConversationID: "c-ops", ConversationName: "ops", PostID: "c1", Sequence: 12, AuthorName: "Dana Cole", CoveringID: "bob", CoveringName: "Bob Ray", Body: "private figures", CreatedAt: at},
			{ID: "c2", Kind: ChatNotificationCover, ConversationID: "c-ops", ConversationName: "ops", PostID: "c2", Sequence: 13, AuthorName: "Dana Cole", CoveringID: "x", CreatedAt: at.Add(time.Hour)},
		}
		view.MarkChatNotificationRead = func(string, func(error)) {}
		view.MarkAllChatNotificationsRead = func(func(error)) {}
		doc := renderNotificationSubtree(t, view)
		want := view.Locale.Text("chat_notifications.cover", map[string]string{"actor": "Dana Cole", "place": "#ops", "covered": "Bob Ray"})
		anon := view.Locale.Text("chat_notifications.cover", map[string]string{"actor": "Dana Cole", "place": "#ops", "covered": view.Locale.Text("chat_notifications.someone_covered")})
		for _, s := range []string{want, anon, "channel=c-ops&amp;at=12&amp;message=c1"} {
			if !strings.Contains(doc, s) {
				t.Errorf("%s: missing %q in %s", locale, s, doc)
			}
		}
		if strings.Contains(doc, "private figures") {
			t.Fatalf("%s: a cover notice carried message text: %s", locale, doc)
		}
		if strings.Contains(doc, "bob\"") || strings.Contains(doc, ">bob<") {
			t.Fatalf("%s: an identifier was shown: %s", locale, doc)
		}
	}
	if view := ResolveProductLocale("de-DE"); view.Text("chat_notifications.cover") == ResolveProductLocale("en-US").Text("chat_notifications.cover") {
		t.Fatal("German copy missing for the cover notice")
	}
	if ResolveProductLocale("ar").Text("chat_notifications.cover") == ResolveProductLocale("en-US").Text("chat_notifications.cover") {
		t.Fatal("Arabic copy missing for the cover notice")
	}
}
