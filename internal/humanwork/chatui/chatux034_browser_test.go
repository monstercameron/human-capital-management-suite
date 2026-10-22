package chatui_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATUX_034_Browser opens the composer's menus on a page in each
// language and reads their details: the emoji preview shows a name and never a
// :shortcode:, the "/" list is a name and a description per row with a footer in
// plain words, the add menu's rows are one line each with the attachment limits
// in the tooltip, the add and channel menus use icons that match their actions,
// a message that is not saved has a plain (unpressed) bookmark, and Browse
// channels is a live control.
func TestTodo_CHATUX_034_Browser(t *testing.T) {
	shortcode := regexp.MustCompile(`:[a-z0-9_+-]+:`)
	limits := regexp.MustCompile(`20|٢٠`)
	for _, locale := range w7Locales {
		page := w7Parse(t, w7Page(t, locale, func(m *chatui.Model) {
			m.Chatattach001 = &chatui.Chatattach001Composer{Choose: func() {}}
			m.GiphyAPIKey = "key"
			m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
			m.Messages = []chatui.Message{{ID: "m1", AuthorID: "jake", Author: "Jake Sullivan", Body: "Lunch at noon?", Revision: 1, SentAt: time.Date(2026, 10, 1, 11, 18, 0, 0, time.Local), Sequence: 1}}
			m.MenuID = "m1" // the message toolbar is drawn for the row whose menu is open
			m.Callbacks.OpenMenu = func(string) {}
			m.Callbacks.OpenBrowse = func() {}
			m.Callbacks.CreateSection = func(string) {}
		}))

		// The emoji preview is the name, and the skin tone control names itself.
		preview := w7One(t, locale+" emoji preview", page, func(n *xhtml.Node) bool {
			return w7Class(n, "emoji-pop-preview") && w7Inside(n, func(p *xhtml.Node) bool { return w7Attr(p, "data-target") == "chat-composer" })
		})
		if text := w7Text(preview); text == "" || shortcode.MatchString(text) {
			t.Errorf("%s: the emoji preview reads %q, want a name with no :shortcode:", locale, text)
		}
		if len(w7All(page, func(n *xhtml.Node) bool { return w7Class(n, "emoji-pop-code") })) != 0 {
			t.Errorf("%s: a shortcode line is drawn in the emoji picker", locale)
		}
		tone := w7All(page, func(n *xhtml.Node) bool { return n.Data == "button" && w7Class(n, "emoji-pop-tone") })
		labels := w7All(page, func(n *xhtml.Node) bool { return w7Class(n, "emoji-pop-tone-label") })
		if len(tone) == 0 || len(labels) == 0 || w7Text(labels[0]) == "" || w7Attr(tone[0], "aria-label") == "" {
			t.Errorf("%s: the skin tone control is not labelled beside its button", locale)
		}

		// The "/" list: one line a row (the command and what it does), the hint
		// kept for the selected row, a footer of plain words.
		menu := w7One(t, locale+" command list", page, func(n *xhtml.Node) bool {
			return w7Class(n, "command-menu") && w7Attr(n, "id") == "chat-composer-commands"
		})
		rows := w7All(menu, func(n *xhtml.Node) bool { return w7Class(n, "command-option") })
		if len(rows) == 0 {
			t.Fatalf("%s: the command list has no rows", locale)
		}
		for _, row := range rows {
			name := w7All(row, func(n *xhtml.Node) bool { return w7Class(n, "mention-name") })
			detail := w7All(row, func(n *xhtml.Node) bool { return w7Class(n, "mention-detail") })
			if len(name) != 1 || len(detail) != 1 || !strings.HasPrefix(w7Text(name[0]), "/") || w7Text(detail[0]) == "" {
				t.Errorf("%s: a command row is not a /name and a description", locale)
			}
		}
		footer := w7One(t, locale+" command footer", menu, func(n *xhtml.Node) bool { return w7Class(n, "kbd-hint") })
		if text := w7Text(footer); text == "" || strings.Contains(text, "Tab details") || strings.Contains(text, "cmd-keys") {
			t.Errorf("%s: the command footer reads %q, want plain words", locale, text)
		}

		// The add menu: one line of description a row, limits in the tooltip.
		items := w7All(page, func(n *xhtml.Node) bool { return n.Data == "button" && w7Class(n, "composer-add-item") })
		if len(items) < 3 {
			t.Fatalf("%s: the add menu has %d rows", locale, len(items))
		}
		for _, item := range items {
			notes := w7All(item, func(n *xhtml.Node) bool { return w7Class(n, "composer-add-note") })
			if len(notes) != 1 || len([]rune(w7Text(notes[0]))) > 50 || w7Text(notes[0]) == "" {
				t.Errorf("%s: an add menu row's description is not one short line: %v", locale, notes)
			}
			if w7Attr(item, "data-extra") == "attachment" {
				if limits.MatchString(w7Text(item)) || !limits.MatchString(w7Attr(item, "title")) {
					t.Errorf("%s: the attachment limits are not moved from the row to its tooltip (row %q, tooltip %q)", locale, w7Text(item), w7Attr(item, "title"))
				}
			}
		}

		// The channel menu: matching icons, one chevron-free New section, a live Browse.
		iconOf := func(root *xhtml.Node) string {
			for _, svg := range w7All(root, func(n *xhtml.Node) bool { return w7Class(n, "chat-icon") }) {
				for _, class := range strings.Fields(w7Attr(svg, "class")) {
					if strings.HasPrefix(class, "icon-") && class != "icon-chevron-down" {
						return class
					}
				}
			}
			return ""
		}
		create := w7One(t, locale+" Create a channel", page, func(n *xhtml.Node) bool {
			return n.Data == "button" && w7Attr(n, "data-action") == "open-create" && w7Class(n, "chatux002-item")
		})
		browse := w7One(t, locale+" Browse channels", page, func(n *xhtml.Node) bool { return n.Data == "button" && w7Attr(n, "data-action") == "open-browse" })
		section := w7One(t, locale+" New section", page, func(n *xhtml.Node) bool {
			return n.Data == "button" && w7Attr(n, "data-action") == "open-section-create"
		})
		icons := map[string]string{"create": iconOf(create), "browse": iconOf(browse), "section": iconOf(section)}
		if icons["create"] != "icon-hash" || icons["section"] != "icon-section-new" || icons["browse"] == "" || icons["create"] == icons["section"] || icons["browse"] == icons["create"] || icons["browse"] == icons["section"] {
			t.Errorf("%s: the channel menu's icons do not match their actions: %v", locale, icons)
		}
		if w7Has(browse, "disabled") {
			t.Errorf("%s: Browse channels is a dead control", locale)
		}

		// An unsaved message has a plain bookmark: pressed only when it is saved.
		message := w7One(t, locale+" message", page, func(n *xhtml.Node) bool { return n.Data == "article" && w7Class(n, "message") })
		marks := w7All(message, func(n *xhtml.Node) bool {
			return n.Data == "button" && w7Class(n, "chatsave-action") && w7Class(n, "message-action")
		})
		if len(marks) == 0 || w7Attr(marks[0], "aria-pressed") != "false" {
			t.Errorf("%s: the bookmark of an unsaved message is not an unpressed toggle", locale)
		}
	}

	// The look that goes with them is in the page's stylesheet.
	sheet := chatui.ScopedStylesheet()
	for _, want := range []string{
		`.command-option .command-args{display:none}`,
		`.command-option:is(.active,[aria-selected="true"]) .command-args{display:block}`,
		`.composer-add-item{min-block-size:56px`,
		`.section-create-trigger>.chat-icon{display:none}`,
		`.message-actions .chatsave-action[aria-pressed="true"]{color:var(--hcm-color-text)}`,
		`.emoji-pop .emoji-pop-input:is(:focus,:focus-visible){outline:0`,
	} {
		if !strings.Contains(strings.ReplaceAll(sheet, ":scope", ".chat-workspace"), strings.ReplaceAll(want, ".chat-workspace ", "")) {
			t.Errorf("the page's stylesheet lacks %q", want)
		}
	}
}
