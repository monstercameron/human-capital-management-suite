package chatui

import (
	"strings"
	"testing"
)

// TestIssue3721_UnreadBadgeExplainsHowItCounts pins the Expected "or the UI
// explains how unread is counted": the sidebar badge says that only other
// people's messages count, so a conversation whose later messages are all the
// person's own does not look miscounted.
func TestIssue3721_UnreadBadgeExplainsHowItCounts(t *testing.T) {
	want := map[string]string{
		"en-US": "Messages from other people you have not read. Your own messages are not counted.",
		"de-DE": "Ungelesene Nachrichten von anderen Personen. Ihre eigenen Nachrichten werden nicht mitgezählt.",
		"ar":    "رسائل غير مقروءة من أشخاص آخرين. رسائلك لا تُحتسب.",
	}
	for locale, explanation := range want {
		model := Model{
			State: StateReady, Locale: locale, SelectedID: "engineering",
			Conversations: []Conversation{
				{ID: "engineering", Name: "Engineering", Kind: PublicChannel, MemberCount: 3, Unread: 1},
				{ID: "other", Name: "Other", Kind: PublicChannel, MemberCount: 3},
			},
			Messages:  []Message{{ID: "m1", Author: "Ari Chen", Body: "Hello"}},
			Callbacks: Callbacks{SelectConversation: func(string) {}},
		}
		markup := render(t, model)
		start := strings.Index(markup, `class="chat-badge"`)
		if start < 0 {
			t.Fatalf("%s: no unread badge in %s", locale, markup)
		}
		start = strings.LastIndex(markup[:start], "<span")
		end := strings.Index(markup[start:], "</span>")
		badge := markup[start : start+end]
		if !strings.Contains(badge, `title="`+explanation+`"`) {
			t.Errorf("%s: unread badge does not explain how it counts: %s", locale, badge)
		}
		if !strings.Contains(badge, `aria-label="`+model.tf(KeyUnreadCount, map[string]string{"n": model.n(1)})+`"`) {
			t.Errorf("%s: the badge lost its accessible count: %s", locale, badge)
		}
	}
}
