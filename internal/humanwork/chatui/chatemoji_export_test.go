package chatui

import "testing"

// EmojiPickerMarkupForTest renders the composer's open emoji picker (and its
// button) with the shipped emoji data loaded for the model's language, so a test
// outside this package can render it against the product's own catalog. state is
// "browse", "search" (a query typed) or "tone" (the skin-tone choices open).
func EmojiPickerMarkupForTest(t *testing.T, m Model, state string) string {
	t.Helper()
	var markup string
	withEmojiHost(t, m, func(*localUI) {
		st := emojiOpenState("chat-composer", false, false, false)
		switch state {
		case "search":
			st = emojiQuery(st, "party")
		case "tone":
			st.ToneOpen = true
		}
		markup = renderNode(t, chatEmojiComposerPicker(m, localUI{emoji: st}, "chat-composer", false)) + renderNode(t, chatEmojiComposerLayer(m, localUI{emoji: st}, "chat-composer"))
		if state == "browse" {
			m.PickerID = "post-1"
			markup += renderNode(t, chatEmojiReactionLayer(m, localUI{}))
		}
	})
	return markup
}
