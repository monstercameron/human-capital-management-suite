package productui

import (
	"strings"
	"testing"
	"time"
)

func issue3733Items() []ChatNotification {
	at := time.Date(2026, 3, 4, 9, 30, 0, 0, time.UTC)
	return []ChatNotification{
		{ID: "m1", Kind: ChatNotificationMention, ConversationID: "c-ops", ConversationName: "ops", PostID: "m1", Sequence: 12, AuthorName: "Dana <b>Cole</b>", Body: "can you look at <i>this</i>?", CreatedAt: at},
		{ID: "m2", Kind: ChatNotificationReply, ConversationID: "c-ops", ConversationName: "ops", PostID: "m2", Sequence: 15, ThreadRootID: "m0", ThreadRootSequence: 3, AuthorName: "Lee Park", CreatedAt: at.Add(time.Hour)},
		{ID: "m3", Kind: ChatNotificationDirect, ConversationID: "dm-1", PostID: "m3", Sequence: 2, CreatedAt: at.Add(2 * time.Hour), Read: true},
	}
}

func issue3733View(t *testing.T, locale string) View {
	t.Helper()
	view := testView(PageChat)
	view.Locale = ResolveProductLocale(locale)
	view.ChatNotifications = issue3733Items()
	view.MarkChatNotificationRead = func(string, func(error)) {}
	view.MarkAllChatNotificationsRead = func(func(error)) {}
	return view
}

func TestIssue3733_BellListsChatNotifications(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := issue3733View(t, locale)
		doc := renderNotificationSubtree(t, view)
		for _, want := range []string{
			"chat-notifications",
			view.Locale.Text("chat_notifications.heading"),
			view.Locale.Text("chat_notifications.mention", map[string]string{"actor": "Dana <b>Cole</b>", "place": "#ops"}),
			view.Locale.Text("chat_notifications.reply", map[string]string{"actor": "Lee Park", "place": "#ops"}),
			view.Locale.Text("chat_notifications.direct", map[string]string{"actor": view.Locale.Text("chat_notifications.someone"), "place": ""}),
			view.Locale.Text("chat_notifications.mark_all"),
			view.Locale.Text("chat_notifications.open_chat"),
			view.Locale.Text("notifications.unread"), view.Locale.Text("notifications.read"),
		} {
			escaped := strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace(want)
			if !strings.Contains(doc, escaped) {
				t.Errorf("%s: missing %q in %s", locale, want, doc)
			}
		}
		if strings.Contains(doc, "<b>Cole") || strings.Contains(doc, "<i>this") {
			t.Fatalf("%s: chat text was not escaped: %s", locale, doc)
		}
		// Items open the message: a message by its place, a reply by its thread.
		for _, href := range []string{"channel=c-ops&amp;at=12&amp;message=m1", "channel=c-ops&amp;at=3&amp;message=m0&amp;thread=1", "channel=dm-1&amp;at=2&amp;message=m3"} {
			if !strings.Contains(doc, href) {
				t.Errorf("%s: missing link %q in %s", locale, href, doc)
			}
		}
	}
}

func TestIssue3733_ChatNotificationHref(t *testing.T) {
	cases := map[string]ChatNotification{
		"/workspace/app/chat#channel=c%2F1":                          {ConversationID: "c/1"},
		"/workspace/app/chat#channel=c-1&at=5&message=p+1":           {ConversationID: "c-1", PostID: "p 1", Sequence: 5},
		"/workspace/app/chat#channel=c-1&at=2&message=root&thread=1": {ConversationID: "c-1", PostID: "r", Sequence: 9, ThreadRootID: "root", ThreadRootSequence: 2},
		"/workspace/app/chat#channel=c-1&at=9&message=r":             {ConversationID: "c-1", PostID: "r", Sequence: 9, ThreadRootID: "root"},
		"/workspace/app/chat#channel=c-1":                            {ConversationID: "c-1", PostID: "r"},
	}
	for want, item := range cases {
		if got := ChatNotificationHref(item); got != want {
			t.Errorf("href = %q, want %q", got, want)
		}
	}
}

func TestIssue3733_BadgeCountsChatAndWorkflowUnread(t *testing.T) {
	view := issue3733View(t, "en-US")
	view.WorkflowNotifications = []WorkflowNotification{{ID: "n1", JourneyID: "j1", WorkerName: "A", Purpose: "TASK", Status: "ASSIGNED"}}
	doc := renderNotificationSubtree(t, view)
	// One workflow item and two unread chat items; the read direct message is not counted.
	if !strings.Contains(doc, "3 unread notifications") {
		t.Fatalf("badge does not add chat and workflow unread: %s", doc)
	}
	view.ChatNotifications = nil
	if doc = renderNotificationSubtree(t, view); !strings.Contains(doc, "1 unread notification") || !strings.Contains(doc, "No chat notifications yet.") {
		t.Fatalf("empty chat state: %s", doc)
	}
}

func TestIssue3733_ChatNotificationStates(t *testing.T) {
	view := issue3733View(t, "en-US")
	view.ChatNotificationsUnavailable = true
	doc := renderNotificationSubtree(t, view)
	if !strings.Contains(doc, "Chat notifications are temporarily unavailable.") {
		t.Fatalf("unavailable state missing: %s", doc)
	}
	if strings.Contains(doc, "Dana") || strings.Contains(doc, "Mark all as read") {
		t.Fatalf("an unavailable feed rendered stale items or actions: %s", doc)
	}
	// With everything read there is nothing to mark.
	view = issue3733View(t, "en-US")
	for i := range view.ChatNotifications {
		view.ChatNotifications[i].Read = true
	}
	if doc = renderNotificationSubtree(t, view); strings.Contains(doc, "Mark all as read") || strings.Contains(doc, "notification-unread-count") {
		t.Fatalf("all read, but the bell still offers work: %s", doc)
	}
	// An author the page cannot name reads as "Someone", never an identifier.
	view = issue3733View(t, "en-US")
	if doc = renderNotificationSubtree(t, view); !strings.Contains(doc, "Someone sent you a direct message") {
		t.Fatalf("unknown author: %s", doc)
	}
}

func TestIssue3733_NoChatSectionWithoutChatAccess_Security(t *testing.T) {
	view := issue3733View(t, "en-US")
	view.Page = PageWork
	view.Roles = nil
	view.EffectivePermissions = nil
	view.NavigationProjection = &AuthorizedNavigationProjection{}
	doc := renderNotificationSubtree(t, view)
	if strings.Contains(doc, "Dana") || strings.Contains(doc, "chat-notifications") {
		t.Fatalf("a person without Chat saw chat notifications: %s", doc)
	}
}
