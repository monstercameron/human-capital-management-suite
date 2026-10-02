package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATSAVE-002. The two glyphs the shared icon set does not hold, drawn in its
// hand (24 units, 1.8 stroke, round caps). The rest of the panel uses icon().
var chatsave002Paths = map[string]string{
	"bookmark": "M6 3h12v18l-6-4-6 4V3z",
	"bell":     "M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9M13.7 21a2 2 0 0 1-3.4 0",
}

// chatsave002Icon is a glyph of the panel; a name outside this file's two falls
// to the shared set. A filled bookmark is the saved state.
func chatsave002Icon(name string, filled bool) ui.Node {
	d, ok := chatsave002Paths[name]
	if !ok {
		return icon(name)
	}
	class := "chat-icon icon-" + name
	if filled {
		class += " chatsave-filled"
	}
	return html.Tag("svg", html.Props{Class: class, Raw: iconSVGAttrs}, html.Tag("path", html.Props{Raw: map[string]any{"d": d}}))
}
