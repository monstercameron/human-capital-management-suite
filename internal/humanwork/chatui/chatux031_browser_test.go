package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATUX_031_Browser opens Chat preferences on a page in each language
// and reads it as the review did: a settings list in sections, each a labelled
// row with one control; Reading language is one select that saves on change and
// shows a brief tick after a save, with no Change button and no second labelled
// select; the language counts are text, not a box; the translate switch is a
// labelled switch; and the panel fits a 320 px phone.
func TestTodo_CHATUX_031_Browser(t *testing.T) {
	for _, locale := range w7Locales {
		page := w7Parse(t, w7Page(t, locale, func(m *chatui.Model) {
			m.Callbacks.SavePreferences = func(chatui.Preferences) {}
			m.ChatFeatures = &chatui.ChatFeatures{Renderings: true, Translating: true}
		}))

		// One panel: a dialog holding the quiet hours and reading sections.
		panel := w7One(t, locale+" preferences panel", page, func(n *xhtml.Node) bool { return w7Attr(n, "data-chat-layer") == "chat-prefs" })
		if w7Attr(panel, "role") != "dialog" || w7Attr(panel, "aria-label") == "" {
			t.Errorf("%s: the preferences panel is not a named dialog", locale)
		}
		sections := w7All(panel, func(n *xhtml.Node) bool { return n.Data == "section" && w7Attr(n, "data-prefs-section") != "" })
		if len(sections) != 2 || w7Attr(sections[0], "data-prefs-section") != "quiet-hours" || w7Attr(sections[1], "data-prefs-section") != "reading-languages" {
			t.Fatalf("%s: the panel holds %d sections, want Quiet hours then Reading languages", locale, len(sections))
		}
		for _, section := range sections {
			if w7Attr(section, "aria-labelledby") == "" {
				t.Errorf("%s: the %s section is not named", locale, w7Attr(section, "data-prefs-section"))
			}
		}

		// Reading language is one labelled select; nothing asks to be changed first.
		reading := sections[1]
		selects := w7All(reading, func(n *xhtml.Node) bool { return n.Data == "select" })
		if len(selects) != 1 || w7Attr(selects[0], "id") != "chatrender-reading" || w7Attr(selects[0], "name") != "reading" {
			t.Fatalf("%s: the reading section holds %d selects, want the one reading select", locale, len(selects))
		}
		labels := w7All(reading, func(n *xhtml.Node) bool { return n.Data == "label" && w7Attr(n, "for") == "chatrender-reading" })
		if len(labels) != 1 || w7Text(labels[0]) == "" {
			t.Errorf("%s: the reading select has %d labels, want one with a name", locale, len(labels))
		}
		if len(w7All(panel, func(n *xhtml.Node) bool { return w7Class(n, "chat-prefs-change") })) != 0 {
			t.Errorf("%s: a Change button is still drawn", locale)
		}
		langs := 0
		for _, option := range w7All(selects[0], func(n *xhtml.Node) bool { return n.Data == "option" }) {
			if w7Text(option) == "" || strings.HasPrefix(w7Text(option), "chatrender") {
				t.Errorf("%s: a language option is nameless or prints a key", locale)
			}
			langs++
		}
		if langs < 4 {
			t.Errorf("%s: the reading select offers %d languages", locale, langs)
		}
		// The translate switch is a labelled switch of its own row.
		sw := w7One(t, locale+" translate switch", reading, func(n *xhtml.Node) bool { return n.Data == "input" && w7Attr(n, "id") == "chatrender-translate" })
		if w7Attr(sw, "role") != "switch" || w7Attr(sw, "type") != "checkbox" || sw.Parent.Data != "label" || w7Text(sw.Parent) == "" {
			t.Errorf("%s: the translate switch is not a labelled switch", locale)
		}

		// Saving shows a brief tick, and an unsaved row shows none.
		render := func(saved bool) *xhtml.Node {
			markup, err := ui.RenderToString(chatui.RenderingSettings(chatui.RenderingSettingsModel{Locale: locale, Preference: chatrender.DefaultPreference(locale), AutoSave: true, Saved: saved}))
			if err != nil {
				t.Fatal(err)
			}
			return w7Parse(t, markup)
		}
		ticks := func(root *xhtml.Node) int {
			return len(w7All(root, func(n *xhtml.Node) bool { return w7Class(n, "chatux031-tick") }))
		}
		status := func(root *xhtml.Node) *xhtml.Node {
			return w7One(t, locale+" settings status", root, func(n *xhtml.Node) bool { return w7Attr(n, "id") == "chatrender-settings-status" })
		}
		before, after := render(false), render(true)
		if ticks(before) != 0 || ticks(after) != 1 {
			t.Errorf("%s: %d ticks before a save and %d after, want none and one", locale, ticks(before), ticks(after))
		}
		if w7Attr(status(before), "data-state") != "help" || w7Attr(status(after), "data-state") != "saved" || w7Attr(status(after), "role") != "status" {
			t.Errorf("%s: the status line does not move from help to saved as a status", locale)
		}
		if w7Text(status(before)) == "" || w7Text(status(after)) == "" || w7Text(status(before)) == w7Text(status(after)) {
			t.Errorf("%s: the status line says the same thing before and after a save", locale)
		}

		// Read-only counts are text, never a box that looks like a field.
		indicator, err := ui.RenderToString(chatui.RenderingLanguageIndicator(locale, map[string]int{"de": 2, "en": 1}))
		if err != nil {
			t.Fatal(err)
		}
		root := w7Parse(t, indicator)
		counts := w7All(root, func(n *xhtml.Node) bool { return w7Class(n, "chatrender-language-count") })
		if len(counts) != 2 {
			t.Fatalf("%s: %d language counts, want 2", locale, len(counts))
		}
		for _, count := range counts {
			if n := count.Data; n != "span" && n != "p" && n != "li" {
				t.Errorf("%s: a language count is a <%s>", locale, n)
			}
			if !strings.ContainsAny(w7Text(count), s31Digits(locale, "0123456789")) {
				t.Errorf("%s: a language count %q has no number in the locale's numerals", locale, w7Text(count))
			}
		}
		if controls := w7All(root, func(n *xhtml.Node) bool {
			return n.Data == "input" || n.Data == "select" || n.Data == "textarea" || n.Data == "button"
		}); len(controls) != 0 {
			t.Errorf("%s: the language counts hold %d controls, want plain text", locale, len(controls))
		}
	}

	// The look: a settings list that fits a 320 px phone, the skin tones in one row
	// of six, a neutral off switch and an accent on switch.
	prefs := chatui.ChatUX031Styles
	for _, want := range []string{"min-inline-size:min(220px,calc(100vw - 16px))", "@container(max-width:250px)", ".chat-prefs-head{display:flex;flex-wrap:wrap", "repeat(6,minmax(0,1fr))", "min-block-size:44px;padding:0 12px", ".chatrender-language-count{margin:0;border:0"} {
		if !strings.Contains(prefs, want) {
			t.Errorf("the preferences styles lack %q", want)
		}
	}
	sheet := strings.ReplaceAll(chatui.ScopedStylesheet(), ":scope", ".chat-workspace")
	for _, want := range []string{
		`:root .chat-workspace :is(.switch,.chatmod-switch-track){border-color:var(--hcm-color-control-border);background:var(--hcm-color-control-border)}`,
		`:root .chat-workspace :is(.switch:checked,.chatmod-switch[aria-checked="true"] .chatmod-switch-track){border-color:var(--accent);background:var(--accent)}`,
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("the page's stylesheet lacks %q", want)
		}
	}
}
