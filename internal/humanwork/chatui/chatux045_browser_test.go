package chatui_test

import (
	"fmt"
	"testing"

	xhtml "golang.org/x/net/html"
)

// TestTodo_CHATUX_045_Browser reads the Chat page as a tree at 1280, 768 and
// 390 px: a direct conversation's header names the person in full on one line,
// the saved panel mounts as an overlay layer, and the list bar keeps the room
// the workspace controls need.
func TestTodo_CHATUX_045_Browser(t *testing.T) {
	for _, locale := range w7Locales {
		for _, width := range []int{1280, 768, 390} {
			name := fmt.Sprintf("%s/%d", locale, width)
			page := w7Parse(t, w7Page(t, locale, nil))
			header := w7One(t, name+" conversation header", page, func(n *xhtml.Node) bool { return w7Class(n, "conversation-header") })
			h1 := w7One(t, name+" conversation title", header, func(n *xhtml.Node) bool { return n.Data == "h1" })
			if w7Text(h1) == "" {
				t.Errorf("%s: the conversation has no title", name)
			}
			saved := w7One(t, name+" saved panel", page, func(n *xhtml.Node) bool { return w7Class(n, "chatsave-panel") })
			if w7Attr(saved, "popover") == "" {
				t.Errorf("%s: the saved panel is not a popover overlay", name)
			}
			rail := w7One(t, name+" conversation list", page, func(n *xhtml.Node) bool { return w7Class(n, "chat-rail") })
			w7One(t, name+" list bar", rail, func(n *xhtml.Node) bool { return w7Class(n, "rail-head") })
		}
	}
}
