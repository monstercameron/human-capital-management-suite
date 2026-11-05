package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATUX_050_Browser draws the served Chat page in each language:
// the search key reads as printed on the reader's keyboard, Send sits in the
// tool row under the field, every row menu item leads with an icon, the Saved
// counts add up, the details help is one sentence, the workspace filters show
// only what differs, and a GIF is its own picture.
func TestTodo_CHATUX_050_Browser(t *testing.T) {
	chips := map[string]string{"en-US": "Ctrl+K", "de-DE": "Strg+K", "ar": "Ctrl+K"}
	for _, locale := range w7Locales {
		page := w7Parse(t, w7Page(t, locale, func(m *chatui.Model) {
			m.RailMenuID = "general"
			m.ShowDetails = true
			m.Messages = []chatui.Message{{ID: "p1", Author: "Ann Lee", Body: "party time",
				Attachments: []chatui.Attachment{{ID: "a1", Name: "party.gif", ContentType: "image/gif", Width: 320, Height: 240, Bytes: 12000}}}}
		}))

		// The search key is the one on the reader's keyboard, kept left to
		// right in its own chip.
		chip := w7One(t, locale+" search shortcut", page, func(n *xhtml.Node) bool {
			return n.Data == "kbd" && w7Class(n, "chat-search-shortcut")
		})
		if got := w7Text(chip); got != chips[locale] {
			t.Errorf("%s: the search chip reads %q, want %q", locale, got, chips[locale])
		}
		if w7Attr(chip, "dir") != "ltr" || w7Attr(chip, "aria-hidden") != "true" {
			t.Errorf("%s: the search chip lost its direction or hidden state", locale)
		}

		// Send is in the tool row, under the field, beside the hint.
		toolbar := w7One(t, locale+" composer toolbar", page, func(n *xhtml.Node) bool { return w7Class(n, "composer-toolbar") })
		sends := w7All(toolbar, func(n *xhtml.Node) bool { return w7Class(n, "send-button") })
		if len(sends) != 1 {
			t.Errorf("%s: %d Send buttons are in the tool row, want 1", locale, len(sends))
		}
		if locale == "en-US" {
			hint := w7One(t, "composer hint", page, func(n *xhtml.Node) bool { return w7Attr(n, "id") == "composer-help" })
			if got := w7Text(hint); got != "Enter to send, Shift+Enter for a new line" {
				t.Errorf("the composer hint reads %q", got)
			}
		}

		// Every row menu item leads with an icon; the notification radios
		// keep their radio roles and the choice in force its check.
		menu := w7One(t, locale+" row menu", page, func(n *xhtml.Node) bool { return w7Class(n, "rail-row-menu") })
		radios := 0
		for _, item := range w7All(menu, func(n *xhtml.Node) bool {
			return n.Data == "button" && (w7Attr(n, "role") == "menuitem" || w7Attr(n, "role") == "menuitemradio")
		}) {
			if len(w7All(item, func(n *xhtml.Node) bool { return n.Data == "svg" })) == 0 {
				t.Errorf("%s: a row menu item has no leading icon: %q", locale, w7Text(item))
			}
			if w7Attr(item, "role") == "menuitemradio" {
				radios++
				if !w7Has(item, "aria-checked") {
					t.Errorf("%s: a notification choice lost its radio state: %q", locale, w7Text(item))
				}
			}
		}
		if radios != 3 {
			t.Errorf("%s: the row menu holds %d notification radios, want 3", locale, radios)
		}

		// The details title block sits under the panel head, and the help
		// under the rows is one sentence.
		details := w7One(t, locale+" details", page, func(n *xhtml.Node) bool { return w7Class(n, "chat-details") })
		if len(w7All(details, func(n *xhtml.Node) bool { return w7Class(n, "details-summary") })) != 1 {
			t.Errorf("%s: the details title block is missing", locale)
		}
		if locale == "en-US" {
			helps := w7All(details, func(n *xhtml.Node) bool { return w7Class(n, "manage-sec-help") })
			if len(helps) == 0 {
				t.Error("no row help renders in the details panel")
			}
			for _, help := range helps {
				if text := w7Text(help); strings.Count(text, ".") > 1 {
					t.Errorf("a details row help is two sentences: %q", text)
				}
			}
		}

		// A GIF is drawn as its own picture with the badge after it.
		figs := w7All(page, func(n *xhtml.Node) bool { return n.Data == "figure" && w7Class(n, "attachment-image") })
		if len(figs) != 1 || len(w7All(figs[0], func(n *xhtml.Node) bool { return n.Data == "img" })) != 1 {
			t.Errorf("%s: the GIF is not drawn as its own picture", locale)
		}
	}

	// Saved: an All larger than To do plus Done explains itself on its tab.
	saved, err := ui.RenderToString(chatui.RenderSavedMessages(chatui.SavedMessagesView{Locale: "en-US", Tab: "todo", TodoCount: 0, DoneCount: 2, AllCount: 3}))
	if err != nil {
		t.Fatal(err)
	}
	all := w7One(t, "Saved All tab", w7Parse(t, saved), func(n *xhtml.Node) bool {
		return n.Data == "button" && w7Attr(n, "data-saved-tab") == "all"
	})
	if !strings.Contains(w7Attr(all, "title"), "whose original was deleted") {
		t.Errorf("the All tab does not explain its extra item: %q", w7Text(all))
	}

	// Workspace filters from a channel: only the differing lists, or the
	// one line that says the channel matches, in each language.
	defs := []chatfilter.Definition{
		{ID: "builtin-en-profanity", Name: "profanity", Language: "en", Kind: "words", Action: "block", Product: true},
		{ID: "builtin-en-slurs", Name: "slurs", Language: "en", Kind: "words", Action: "block", Product: true},
	}
	enabled := []chatfilter.Enablement{
		{RuleID: "builtin-en-profanity", Enabled: true},
		{RuleID: "builtin-en-slurs", Enabled: true},
		{RuleID: "builtin-en-profanity", Channel: "general", Enabled: false},
	}
	same := map[string]string{"en-US": "Same as workspace", "de-DE": "Wie im Arbeitsbereich", "ar": "مطابق لإعدادات مساحة العمل"}
	for _, locale := range w7Locales {
		markup, err := ui.RenderToString(chatui.ModAdminPanel(chatui.ModAdminProps{
			Model:     chatui.Model{Locale: locale, SelectedID: "general", IsTenantAdmin: true},
			Workspace: true, Channel: "general", Definitions: defs, Enablements: enabled[:2], CanManage: true,
		}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, same[locale]) {
			t.Errorf("%s: a channel that matches the workspace does not say %q", locale, same[locale])
		}
	}
}
