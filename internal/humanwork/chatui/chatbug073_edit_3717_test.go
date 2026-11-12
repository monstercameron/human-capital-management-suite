package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHAT_3717 holds an edit saved with its text cleared to an inline
// message: the key hint under the edit box carries the "cannot be empty" line
// as well as the normal hint, and the page script swaps them while the box is
// blank. Whitespace alone counts as blank, as it does when the edit is saved.
func TestTodo_CHAT_3717(t *testing.T) {
	t.Run("blank test follows the save rule", func(t *testing.T) {
		for _, tc := range []struct {
			value string
			want  bool
		}{{"", true}, {"  \n\t", true}, {"x", false}, {" x ", false}} {
			if got := chatbug073EditBlank(tc.value); got != tc.want {
				t.Errorf("chatbug073EditBlank(%q) = %v, want %v", tc.value, got, tc.want)
			}
		}
	})

	t.Run("hint carries both lines in en-US, de-DE and ar", func(t *testing.T) {
		for _, tc := range []struct {
			locale, empty string
		}{
			{"en-US", "A message cannot be empty. Write something, or cancel the edit."},
			{"de-DE", "Eine Nachricht darf nicht leer sein."},
			{"ar", "لا يمكن أن تكون الرسالة فارغة."},
		} {
			m := chatbug073Model()
			m.Locale = tc.locale
			m.EditingID = "m2"
			m.Callbacks = Callbacks{EditMessage: func(string, string, uint64) {}, CancelEdit: func() {}}
			page := chatbug073Page(t, m)
			if !strings.Contains(page, `id="edit-hint-m2"`) {
				t.Fatalf("%s: edit hint missing", tc.locale)
			}
			if !strings.Contains(page, `data-edit-empty="`) || !strings.Contains(page, tc.empty) {
				t.Errorf("%s: blank-edit line %q not on the hint", tc.locale, tc.empty)
			}
			if !strings.Contains(page, `data-edit-hint="`+chatbug073Text(m, keyChatbug073Hint)+`"`) {
				t.Errorf("%s: normal key hint not kept on the hint", tc.locale)
			}
		}
	})

	t.Run("blank state is styled as an error", func(t *testing.T) {
		if !strings.Contains(ChatBug073Styles, ".edit-hint.is-empty") {
			t.Fatal("no style for the blank-edit hint")
		}
	})
}
