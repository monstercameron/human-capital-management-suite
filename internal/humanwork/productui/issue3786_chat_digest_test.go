package productui

import (
	"strings"
	"testing"
	"time"
)

func issue3786View(locale string, digest *ChatDigest) View {
	view := testView(PageChat)
	view.Locale = ResolveProductLocale(locale)
	view.ChatDigest = digest
	view.SaveChatDigestPref = func(string, int, func(error)) {}
	view.ChatDigestSeen = func(time.Time, func(error)) {}
	return view
}

func issue3786Digest() *ChatDigest {
	at := time.Date(2026, 3, 10, 2, 0, 0, 0, time.UTC)
	return &ChatDigest{Cadence: ChatDigestDaily, Minute: 480, Timezone: "UTC", Due: true, Through: at.Add(6 * time.Hour), Items: []ChatDigestItem{
		{Why: ChatDigestWhyQuietHours, ConversationID: "c-ops", ConversationName: "ops", PostID: "m1", Sequence: 12, AuthorName: "Dana <b>Cole</b>", At: at},
		{Why: ChatDigestWhyMuted, ConversationID: "c-quiet", ConversationName: "quiet", PostID: "m2", Sequence: 3, At: at},
		{Why: ChatDigestWhyFollowed, ConversationID: "c-ops", ConversationName: "ops", PostID: "m4", Sequence: 15, ThreadRootID: "m0", ThreadRootSequence: 3, AuthorName: "Lee Park", At: at, Count: 3},
	}}
}

func TestIssue3786_BellShowsTheDigestWhenDue(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := issue3786View(locale, issue3786Digest())
		doc := renderNotificationSubtree(t, view)
		for _, want := range []string{
			view.Locale.Text("chat_digest.heading"),
			view.Locale.Text("chat_digest.item_quiet_hours", map[string]string{"actor": "Dana <b>Cole</b>", "place": "#ops"}),
			view.Locale.Text("chat_digest.item_muted", map[string]string{"actor": view.Locale.Text("chat_notifications.someone"), "place": "#quiet"}),
			view.Locale.Text("chat_digest.item_followed_many", map[string]string{"place": "#ops", "count": "3"}),
			view.Locale.Text("chat_digest.mark_seen"),
			view.Locale.Text("chat_digest.cadence_daily"),
			view.Locale.Text("chat_digest.cadence_quiet_end"),
			view.Locale.Text("chat_digest.cadence_off"),
		} {
			escaped := strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace(want)
			if !strings.Contains(doc, escaped) {
				t.Errorf("%s: missing %q in %s", locale, want, doc)
			}
		}
		if strings.Contains(doc, "<b>Cole") {
			t.Fatalf("%s: text was not escaped", locale)
		}
		// Each item opens its message; a followed one opens its thread.
		for _, href := range []string{"channel=c-ops&amp;at=12&amp;message=m1", "channel=c-ops&amp;at=3&amp;message=m0&amp;thread=1"} {
			if !strings.Contains(doc, href) {
				t.Errorf("%s: missing link %q", locale, href)
			}
		}
		// The saved daily choice is the checked radio, with its time.
		if !strings.Contains(doc, `chat-digest-time-480`) {
			t.Errorf("%s: the daily time choices are missing", locale)
		}
	}
}

func TestIssue3786_FollowedThreadSaysHowManyReplies(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		digest := issue3786Digest()
		digest.Items = []ChatDigestItem{
			{Why: ChatDigestWhyFollowed, ConversationID: "c-ops", ConversationName: "ops", PostID: "m4", Sequence: 15, ThreadRootID: "m0", ThreadRootSequence: 3, Count: 1, At: digest.Through},
			{Why: ChatDigestWhyFollowed, ConversationID: "c-ops", ConversationName: "ops", PostID: "m9", Sequence: 19, ThreadRootID: "m5", ThreadRootSequence: 7, Count: 4, At: digest.Through},
		}
		view := issue3786View(locale, digest)
		doc := renderNotificationSubtree(t, view)
		for key, count := range map[string]string{"chat_digest.item_followed_one": "1", "chat_digest.item_followed_many": "4"} {
			want := view.Locale.Text(key, map[string]string{"place": "#ops", "count": count})
			if !strings.Contains(doc, want) {
				t.Errorf("%s: missing %q", locale, want)
			}
		}
	}
}

func TestIssue3786_BellDigestStates(t *testing.T) {
	// Not due: no list and no mark-seen, only the choice.
	view := issue3786View("en-US", &ChatDigest{Cadence: ChatDigestQuietEnd, Minute: 480})
	doc := renderNotificationSubtree(t, view)
	if strings.Contains(doc, view.Locale.Text("chat_digest.mark_seen")) || !strings.Contains(doc, view.Locale.Text("chat_digest.nothing_waiting")) || !strings.Contains(doc, view.Locale.Text("chat_digest.quiet_end_hint")) {
		t.Fatalf("not due: %s", doc)
	}
	// Off explains itself.
	view = issue3786View("en-US", &ChatDigest{Cadence: ChatDigestOff, Minute: 480})
	if doc = renderNotificationSubtree(t, view); !strings.Contains(doc, view.Locale.Text("chat_digest.off_hint")) {
		t.Fatalf("off: %s", doc)
	}
	// Unavailable says so, and offers no choice that cannot be saved.
	view = issue3786View("en-US", nil)
	view.ChatDigestUnavailable = true
	if doc = renderNotificationSubtree(t, view); !strings.Contains(doc, view.Locale.Text("chat_digest.unavailable")) || strings.Contains(doc, "chat-digest-cadence-daily") {
		t.Fatalf("unavailable: %s", doc)
	}
	// No digest read at all (no feed): nothing is drawn.
	view = issue3786View("en-US", nil)
	if doc = renderNotificationSubtree(t, view); strings.Contains(doc, view.Locale.Text("chat_digest.heading")) {
		t.Fatalf("no feed: %s", doc)
	}
}

func TestIssue3786_NoDigestWithoutChatAccess_Security(t *testing.T) {
	view := issue3786View("en-US", issue3786Digest())
	view.Page = PageWork
	view.Roles = nil
	view.EffectivePermissions = nil
	view.NavigationProjection = &AuthorizedNavigationProjection{}
	doc := renderNotificationSubtree(t, view)
	if strings.Contains(doc, view.Locale.Text("chat_digest.heading")) || strings.Contains(doc, "channel=c-ops") {
		t.Fatalf("a person without Chat access saw the digest: %s", doc)
	}
}

func TestIssue3786_DigestCopyInEveryLanguage(t *testing.T) {
	en := chatDigestTranslations("en-US")
	for _, locale := range []string{"de-DE", "ar"} {
		other := chatDigestTranslations(locale)
		for key, text := range en {
			if strings.TrimSpace(other[key]) == "" || (other[key] == text && locale == "ar") {
				t.Errorf("%s: %s is missing or untranslated", locale, key)
			}
		}
		if len(other) != len(en) {
			t.Errorf("%s has %d keys, en-US %d", locale, len(other), len(en))
		}
	}
}
