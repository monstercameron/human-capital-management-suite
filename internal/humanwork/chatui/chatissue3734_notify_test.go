package chatui

import (
	"strings"
	"testing"
)

// TestTodo_ISSUE_3734 pins that a muted conversation does not draw unread
// emphasis (bold row or unread count) while a mention still does, matching
// the unread dot and jump-to-unread rules that already skip muted rows.
func TestTodo_ISSUE_3734(t *testing.T) {
	noop := Callbacks{SelectConversation: func(string) {}}
	mutedUnread := chatPolishMarkup(t, railRow(Model{SelectedID: "x", Callbacks: noop}, Conversation{ID: "mu", Name: "mu", Kind: PublicChannel, Unread: 3, Muted: true}), 1440, "light")
	if strings.Contains(mutedUnread, "unread") || strings.Contains(mutedUnread, `class="chat-badge"`) {
		t.Fatalf("a muted conversation with unread posts is drawn as unread: %s", mutedUnread)
	}
	if !strings.Contains(mutedUnread, "muted") {
		t.Fatalf("a muted conversation is not marked muted: %s", mutedUnread)
	}
	mutedMention := chatPolishMarkup(t, railRow(Model{SelectedID: "x", Callbacks: noop}, Conversation{ID: "mm", Name: "mm", Kind: PublicChannel, Unread: 3, Mentions: 1, Muted: true}), 1440, "light")
	if !strings.Contains(mutedMention, `class="chat-row muted unread"`) && !strings.Contains(mutedMention, `class="chat-row unread muted"`) {
		t.Fatalf("a muted conversation with a mention lost its mention emphasis: %s", mutedMention)
	}
	if !strings.Contains(mutedMention, "chat-badge mention") {
		t.Fatalf("a muted mention has no mention count: %s", mutedMention)
	}
}
