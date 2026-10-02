package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// The Writing style row is supplied by the writing style feature. While nothing
// supplies it the panel has no such row; when something does, it sits after
// Reading languages and before Emoji skin tone.
func TestTodo_CHATBUG_045_WritingStyleRowOrder(t *testing.T) {
	m := Model{Locale: "en-US", CurrentUser: "walt", CurrentTenantID: "t", EmojiPrefs: `{"v":1,"tone":0,"usage":[],"seq":0}`,
		Callbacks: Callbacks{SavePreferences: func(Preferences) {}, SaveEmojiPrefs: func(string) {}}}
	sections := func() []string {
		markup := chatPolishMarkup(t, rail(m, handlers{}), 1280, "light")
		var out []string
		for _, n := range chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-prefs-section") != "" }) {
			out = append(out, chatPolishAttr(n, "data-prefs-section"))
		}
		return out
	}
	if got := strings.Join(sections(), ","); got != "quiet-hours,reading-languages,emoji-tone" {
		t.Fatalf("without a writing style feature the rows are %s", got)
	}
	saved := chatbug045WritingStyleRow
	defer func() { chatbug045WritingStyleRow = saved }()
	chatbug045WritingStyleRow = func(Model) ui.Node { return chatux002Row("writing-style", "Writing style", "Friendly", nil) }
	if got := strings.Join(sections(), ","); got != "quiet-hours,reading-languages,writing-style,emoji-tone" {
		t.Fatalf("with a writing style row the rows are %s", got)
	}
	chatbug045WritingStyleRow = func(Model) ui.Node { return nil }
	if got := strings.Join(sections(), ","); got != "quiet-hours,reading-languages,emoji-tone" {
		t.Fatalf("a writing style feature that answers nothing leaves a row: %s", got)
	}
}

// Choosing a tone goes through the emoji feature's own function: the page's
// choice changes and the person's preferences are written to the server.
func TestTodo_CHATBUG_045_ToneIsSavedThroughTheEmojiFeature(t *testing.T) {
	var written []string
	m := Model{Locale: "en-US", CurrentUser: "walt", CurrentTenantID: "t",
		Callbacks: Callbacks{SaveEmojiPrefs: func(encoded string) { written = append(written, encoded) }}}
	withEmojiHost(t, m, func(*localUI) {
		chatbug045SetTone(2)
		if chatEmojiHost.prefs.Tone != 2 {
			t.Fatalf("the page's tone = %d, want 2", chatEmojiHost.prefs.Tone)
		}
		if len(written) != 1 || decodeEmojiPrefs(written[0]).Tone != 2 {
			t.Fatalf("the server was given %v, want one write with tone 2", written)
		}
	})
}
