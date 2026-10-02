package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatux034ToneControl is the emoji picker's skin-tone button with its name
// beside it. A bare hand glyph does not say what it changes.
func chatux034ToneControl(m Model, button ui.Node) ui.Node {
	return html.Span(html.Props{Class: "emoji-pop-tone-wrap"},
		html.Span(html.Props{Class: "emoji-pop-tone-label", Text: chatEmojiText(m, emojiKeyTone)}), button)
}
