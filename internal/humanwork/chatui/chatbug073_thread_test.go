package chatui

import (
	stdhtml "html"
	"strings"
	"testing"
)

// TestTodo_CHATBUG_073_Thread: an edited message says so in the thread pane as
// it does in the timeline, after its text, for the thread's parent and for a
// reply, in the reader's language.
func TestTodo_CHATBUG_073_Thread(t *testing.T) {
	for locale, mark := range map[string]string{"en-US": "(edited)", "de-DE": "(bearbeitet)", "ar": "(معدّلة)"} {
		parent := Message{ID: "p1", AuthorID: "walt", Author: "Walt Brennan", Body: "Parent text", Revision: 2, Edited: true, Replies: 2}
		m := chatux022Model()
		m.Locale = locale
		m.Messages = []Message{parent}
		m.ShowThread, m.ThreadParentID = true, "p1"
		m.ThreadMessages = []Message{
			{ID: "r1", AuthorID: "loretta", Author: "Loretta Haynes", Body: "Reply as sent", Revision: 1},
			{ID: "r2", AuthorID: "walt", Author: "Walt Brennan", Body: "Reply corrected", Revision: 2, Edited: true},
		}
		pane := stdhtml.UnescapeString(renderNode(t, threadPane(m, handlers{})))
		edited := `<span class="message-edited">` + mark + `</span>`
		if got := strings.Count(pane, edited); got != 2 {
			t.Fatalf("%s: the thread pane marks %d messages edited, want the parent and one reply: %s", locale, got, pane)
		}
		root := pane[strings.Index(pane, `class="thread-root"`):strings.Index(pane, `data-message-id="r1"`)]
		if text, at := strings.Index(root, "Parent text"), strings.Index(root, edited); text < 0 || at < text {
			t.Errorf("%s: %q does not follow the parent's text", locale, mark)
		}
		first := pane[strings.Index(pane, `data-message-id="r1"`):strings.Index(pane, `data-message-id="r2"`)]
		if strings.Contains(first, "message-edited") {
			t.Errorf("%s: a reply that was never edited is marked edited", locale)
		}
		second := pane[strings.Index(pane, `data-message-id="r2"`):]
		if text, at := strings.Index(second, "Reply corrected"), strings.Index(second, edited); text < 0 || at < text {
			t.Errorf("%s: %q does not follow the edited reply's text", locale, mark)
		}
	}
}
