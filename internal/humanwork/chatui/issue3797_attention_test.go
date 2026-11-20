package chatui

import "testing"

func TestIssue3797_ReadsOnlyWhileVisibleAndFocused(t *testing.T) {
	for _, tc := range []struct {
		visible, focused, want bool
	}{{true, true, true}, {true, false, false}, {false, true, false}, {false, false, false}} {
		if got := ChatReadsAllowed(tc.visible, tc.focused); got != tc.want {
			t.Errorf("ChatReadsAllowed(%v,%v) = %v, want %v", tc.visible, tc.focused, got, tc.want)
		}
	}
}

func TestIssue3797_CountsHonourNotificationPreference(t *testing.T) {
	m := Model{
		Conversations: []Conversation{
			{ID: "a", Unread: 3, Mentions: 1},
			{ID: "muted", Unread: 9, Mentions: 4},
			{ID: "mentions", Unread: 5, Mentions: 2},
			{ID: "quiet", Unread: 4},
		},
		Preferences: Preferences{Notifications: map[string]NotificationMode{
			"muted": NotifyMute, "mentions": NotifyMention, "quiet": NotifyMention,
		}},
	}
	unread, mentions := ChatAttentionCounts(m)
	if unread != 3+2 || mentions != 1+2 {
		t.Fatalf("counts = %d unread, %d mentions; want 5 and 3", unread, mentions)
	}
}

func TestIssue3797_TabTitlePrefixIsLocalized(t *testing.T) {
	cases := []struct {
		locale          string
		unread, mention int
		want            string
	}{
		{"en-US", 0, 0, ""},
		{"en-US", 3, 0, "(3) "},
		{"de-DE", 3, 2, "(3 · @2) "},
		{"ar", 12, 0, "(١٢) "},
		{"ar", 12, 1, "(١٢ · @١) "},
		{"en-US", 0, 2, "(2 · @2) "},
	}
	for _, tc := range cases {
		if got := ChatAttentionTitlePrefix(tc.locale, tc.unread, tc.mention); got != tc.want {
			t.Errorf("prefix(%s,%d,%d) = %q, want %q", tc.locale, tc.unread, tc.mention, got, tc.want)
		}
	}
}

func TestIssue3797_TabTitleCapsHugeCounts(t *testing.T) {
	if got := ChatAttentionTitlePrefix("en-US", 1500, 0); got != "(99+) " {
		t.Fatalf("got %q", got)
	}
}
