package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestIssue3797_AwayStateFollowsVisibilityAndFocus(t *testing.T) {
	defer setChatAttention(false, false)
	setChatAttention(false, false)
	if chatPageAway() {
		t.Fatal("visible and focused page reported away")
	}
	was, now := setChatAttention(true, false)
	if !was || now || !chatPageAway() {
		t.Fatalf("hidden: was=%v now=%v away=%v", was, now, chatPageAway())
	}
	if _, now = setChatAttention(false, true); now || !chatPageAway() {
		t.Fatal("visible but unfocused page must stay away")
	}
	if was, now = setChatAttention(false, false); was || !now {
		t.Fatalf("regained: was=%v now=%v", was, now)
	}
}

func TestIssue3797_DividerStaysAtFirstUnseenPost(t *testing.T) {
	posts := []chatUnseenPost{{"mine", "me"}, {"first", "ann"}, {"second", "bob"}}
	if got := chatFirstUnseen("", "me", posts); got != "first" {
		t.Fatalf("got %q, want first other-author post", got)
	}
	if got := chatFirstUnseen("earlier", "me", posts); got != "earlier" {
		t.Fatalf("divider moved to %q", got)
	}
	if got := chatFirstUnseen("", "me", posts[:1]); got != "" {
		t.Fatalf("own post started the divider: %q", got)
	}
}

func TestIssue3797_TabBadgeUsesSidebarCounts(t *testing.T) {
	model := chatui.Model{Locale: "de-DE", Conversations: []chatui.Conversation{{ID: "a", Unread: 2, Mentions: 1}, {ID: "b", Unread: 1}}}
	badge := chatTabBadge(model)
	if badge != "(3 · @1) " {
		t.Fatalf("badge = %q", badge)
	}
	if got := chatBadgedTabTitle(badge, "#general · Chat"); got != "(3 · @1) #general · Chat" {
		t.Fatalf("title = %q", got)
	}
	if chatTabBadge(chatui.Model{}) != "" {
		t.Fatal("no unread must give no badge")
	}
}
