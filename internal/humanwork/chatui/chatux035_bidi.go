package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-035. In a right-to-left page a message block starts at the right
// whatever language its text is in, and the text is isolated so its own
// direction orders its own words. The block (the body, its lists) takes the
// page's direction from the stylesheet; each paragraph or heading carries
// dir="auto" and each list item's inline text sits in a <bdi>, so an English
// sentence inside an Arabic page is laid out left-to-right (its "?" stays at its
// end) and still begins at the reading edge. Left-to-right pages are untouched.

// markdownBlockProps is the props of a paragraph or heading.
func markdownBlockProps(m Model) html.Props {
	if m.Direction == "rtl" {
		return html.Props{Dir: "auto"}
	}
	return html.Props{}
}

// markdownIsolate wraps the inline text of a tight list item.
func markdownIsolate(m Model, inline []ui.Node) []ui.Node {
	if m.Direction != "rtl" || len(inline) == 0 {
		return inline
	}
	return []ui.Node{html.Tag("bdi", html.Props{Dir: "auto"}, inline...)}
}
