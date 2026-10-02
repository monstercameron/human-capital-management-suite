package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_068_Browser: a conversation that stops being readable says
// so where it is, in every language, with its name still in the header and the
// rail still holding the person's other conversations; and the notice about a
// draft saved on another device reads in plain words.
func TestTodo_CHATBUG_068_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		unreadable := chatui.ConversationUnreadableText(locale)
		draft := chatui.DraftUpdatedElsewhereText(locale)
		if unreadable == "" || draft == "" || strings.Contains(unreadable, "chat.bug068") || strings.Contains(draft, "chat.bug068") {
			t.Fatalf("%s: the copy is missing or prints its key: %q, %q", locale, unreadable, draft)
		}
		page := lane3Page(t, locale, func(m *chatui.Model) {
			m.State, m.Error = chatui.StateError, unreadable
			m.Notice = draft
		})
		lane3NoLeaks(t, locale+" unreadable conversation", page)
		if !strings.Contains(page, unreadable) {
			t.Errorf("%s: the page does not say the conversation is no longer available: %s", locale, page)
		}
		if !strings.Contains(page, draft) {
			t.Errorf("%s: the page does not carry the draft notice", locale)
		}
		if !strings.Contains(page, "General Chat!") || !strings.Contains(page, "sales") {
			t.Errorf("%s: the conversation or the rail went away with the notice", locale)
		}
	}
	if chatui.ConversationUnreadableText("en-US") != "This conversation is no longer available to you." {
		t.Errorf("the English line changed: %q", chatui.ConversationUnreadableText("en-US"))
	}
	if chatui.DraftUpdatedElsewhereText("en-US") != "Draft updated on another device. What you are typing here is kept." {
		t.Errorf("the English draft line changed: %q", chatui.DraftUpdatedElsewhereText("en-US"))
	}
}
