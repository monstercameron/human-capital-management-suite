package chatui_test

import (
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATUX_033_Browser renders the conversation page a phone loads, in
// each language, and reads what the phone draws: one conversation bar holding
// the four things the GREEN names (back to the list, the conversation name,
// search, details), a composer with the add menu, mention and emoji (all named
// in the reader's language), and messages whose avatar is the small one. The
// sizes the phone cascade gives them are the page's own phone stylesheet.
func TestTodo_CHATUX_033_Browser(t *testing.T) {
	for _, locale := range w7Locales {
		page := w7Parse(t, w7Page(t, locale, func(m *chatui.Model) {
			m.Chatattach001 = &chatui.Chatattach001Composer{Choose: func() {}}
			m.Messages = []chatui.Message{{ID: "m1", AuthorID: "jake", Author: "Jake Sullivan", Body: "Lunch at noon?", Revision: 1, SentAt: time.Date(2026, 10, 1, 11, 18, 0, 0, time.Local), Sequence: 1}}
			m.Callbacks.ToggleDetails = func(bool) {}
		}))

		// One bar: the conversation header holds back, the name, search and details.
		bars := w7All(page, func(n *xhtml.Node) bool { return n.Data == "header" && w7Class(n, "conversation-header") })
		if len(bars) != 1 {
			t.Fatalf("%s: %d conversation bars, want one", locale, len(bars))
		}
		bar := bars[0]
		for what, pred := range map[string]func(*xhtml.Node) bool{
			"the back button to the list": func(n *xhtml.Node) bool {
				return n.Data == "button" && w7Class(n, "mobile-chat-toggle") && w7Attr(n, "data-action") == "open-rail"
			},
			"the conversation name": func(n *xhtml.Node) bool {
				return n.Data == "button" && w7Class(n, "conversation-name-button") && w7Text(n) == "general"
			},
			"search": func(n *xhtml.Node) bool { return n.Data == "button" && w7Attr(n, "data-action") == "chat-search-open" },
			"details": func(n *xhtml.Node) bool {
				return n.Data == "button" && w7Class(n, "icon-button") && w7Attr(n, "data-action") == "details"
			},
		} {
			found := w7All(bar, pred)
			if len(found) != 1 {
				t.Errorf("%s: the conversation bar has %d of %s, want one", locale, len(found), what)
				continue
			}
			if w7Attr(found[0], "aria-label") == "" && w7Text(found[0]) == "" {
				t.Errorf("%s: %s has no name in the reader's language", locale, what)
			}
		}
		// The page has no second bar of conversation chrome above the header.
		if others := w7All(page, func(n *xhtml.Node) bool { return n.Data == "header" && w7Class(n, "chat-topbar") }); len(others) != 0 {
			t.Errorf("%s: the page draws a second bar above the conversation header", locale)
		}

		// The composer holds the add menu, mention and emoji, each named.
		form := w7One(t, locale+" composer", page, func(n *xhtml.Node) bool { return n.Data == "form" && w7Class(n, "chat-composer") })
		for what, pred := range map[string]func(*xhtml.Node) bool{
			"the add menu": func(n *xhtml.Node) bool {
				return n.Data == "button" && w7Has(n, "aria-haspopup") && w7Inside(n, func(p *xhtml.Node) bool { return w7Class(p, "composer-add") })
			},
			"mention": func(n *xhtml.Node) bool { return n.Data == "button" && w7Class(n, "composer-mention-button") },
			"emoji":   func(n *xhtml.Node) bool { return n.Data == "button" && w7Class(n, "emoji-trigger") },
			"send":    func(n *xhtml.Node) bool { return n.Data == "button" && w7Class(n, "send-button") },
		} {
			found := w7All(form, pred)
			if len(found) == 0 {
				t.Errorf("%s: the composer has no %s", locale, what)
				continue
			}
			if w7Attr(found[0], "aria-label") == "" && w7Text(found[0]) == "" && w7Attr(found[0], "title") == "" {
				t.Errorf("%s: the composer's %s has no name in the reader's language", locale, what)
			}
		}
		// The field is one field that grows with its text.
		if fields := w7All(form, func(n *xhtml.Node) bool { return n.Data == "textarea" && w7Class(n, "composer-input") }); len(fields) != 1 {
			t.Errorf("%s: the composer has %d fields, want one", locale, len(fields))
		}

		// A message is an avatar and a text column.
		message := w7One(t, locale+" message", page, func(n *xhtml.Node) bool { return n.Data == "article" && w7Class(n, "message") })
		if len(w7All(message, func(n *xhtml.Node) bool { return w7Class(n, "avatar") })) != 1 {
			t.Errorf("%s: a message does not hold one avatar", locale)
		}
	}

	// What the phone does with them is the phone stylesheet the page ships.
	phone := chatui.ChatUX033Styles
	for _, want := range []string{"@media(max-width:760px){", "28px minmax(0,1fr)", ".message .avatar{width:28px;height:28px", ".conversation-header .icon-button{width:44px;height:44px}", "field-sizing:content", ".tool-button", "min-width:44px;min-height:44px", ".composer-add{order:-1}", `)::after{content:"";position:absolute;inset:-10px -4px`} {
		if !strings.Contains(phone, want) {
			t.Errorf("the phone styles lack %q", want)
		}
	}
	if sheet := chatui.ScopedStylesheet(); !strings.Contains(sheet, "28px minmax(0,1fr)") {
		t.Error("the page's stylesheet does not carry the phone styles")
	}
}
