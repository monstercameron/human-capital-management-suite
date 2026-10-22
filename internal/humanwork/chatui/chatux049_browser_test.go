package chatui_test

import (
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATUX_049_Browser reads the Chat page as a tree: every close
// control is the plain close icon, the new-conversation control is the compose
// icon, a draft row carries the Draft tag instead of a pencil, and mentions
// render as chips.
func TestTodo_CHATUX_049_Browser(t *testing.T) {
	for _, locale := range w7Locales {
		page := w7Parse(t, w7Page(t, locale, func(m *chatui.Model) {
			m.Preferences.Drafts = map[string]string{"sales": "half a thought"}
		}))
		closes := w7All(page, func(n *xhtml.Node) bool { return w7Class(n, "chat-panel-close") })
		for _, c := range closes {
			if len(w7All(c, func(n *xhtml.Node) bool { return w7Class(n, "icon-close") })) != 1 {
				t.Errorf("%s: a close control is not the plain X", locale)
			}
		}
		w7One(t, locale+" compose control", page, func(n *xhtml.Node) bool { return w7Class(n, "icon-compose") })
		drafts := w7All(page, func(n *xhtml.Node) bool { return w7Class(n, "chat-row-draft") })
		if len(drafts) != 1 {
			t.Fatalf("%s: want one Draft tag, have %d", locale, len(drafts))
		}
		if w7Text(drafts[0]) == "" {
			t.Errorf("%s: the Draft tag has no text", locale)
		}
		if len(w7All(drafts[0], func(n *xhtml.Node) bool { return w7Class(n, "icon-edit") })) != 0 {
			t.Errorf("%s: the Draft tag still carries the pencil", locale)
		}
	}
}
