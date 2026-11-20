package chatui

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// #900005 part 1: one result-order control, and a summary line that does not
// read as a second heading.
func TestIssue900005_SearchOffersOneResultOrderControl(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup := renderNode(t, RenderChatSearch(locale, ChatSearchView{Query: "payroll"}))
		if strings.Contains(markup, `name="order"`) || strings.Contains(markup, `id="chatsearch-order"`) {
			t.Errorf("%s: Refine search still carries a second result-order select: %s", locale, markup)
		}
		if n := strings.Count(markup, `data-chatsearch-action="order"`); n != 2 {
			t.Errorf("%s: want the one switch with its two choices, got %d choice buttons", locale, n)
		}
		label := chatsearchText(locale, "order")
		if n := strings.Count(markup, `aria-label="`+label+`"`); n != 1 {
			t.Errorf("%s: want one control named %q, got %d", locale, label, n)
		}
	}
}

func TestIssue900005_ResultsSummaryIsNotASecondHeading(t *testing.T) {
	markup := renderNode(t, RenderChatSearch("en-US", ChatSearchView{Query: "payroll"}))
	at := strings.Index(markup, `id="`+Issue3857SummaryID+`"`)
	if at < 0 {
		t.Fatalf("the focusable summary is gone: %s", markup)
	}
	open := strings.LastIndex(markup[:at], "<")
	tag := markup[open : at+strings.Index(markup[at:], ">")]
	if !strings.HasPrefix(tag, "<p ") || !strings.Contains(tag, `tabIndex="-1"`) || strings.Contains(tag, "<h") {
		t.Errorf("the summary must stay a focusable paragraph, not a heading: %s", tag)
	}
	css := regexp.MustCompile(`\.chatsearch-scope:not\(:focus\)\{[^}]*\}`).FindString(ChatSearchStyles)
	for _, want := range []string{"clip-path:inset(50%)", "position:absolute", "overflow:hidden"} {
		if !strings.Contains(css, want) {
			t.Errorf("the summary is not visually hidden until focused (missing %s in %q)", want, css)
		}
	}
}

// #900005 part 2: in a right-to-left page a message body whose language is not
// known takes the page's direction. dir=auto would resolve to the text's own
// direction in the browser, ignoring the stylesheet, and start a left-to-right
// message at the far left edge, away from its author. Its paragraphs keep
// dir=auto, so each script is still ordered by its own direction.
func TestIssue900005_UnknownLanguageBodyStartsNextToItsAuthorInRTL(t *testing.T) {
	m, _, _, _ := chatlangModel("ar")
	m.Locale, m.Direction = "ar", "rtl"
	msg := Message{ID: "plain", Revision: 1, AuthorID: "eve", Author: "Eve", Body: "If you are travelling to the Portland site"}
	got := chatlangMessageMarkup(t, m, msg, localUI{})
	body := regexp.MustCompile(`<div[^>]*message-body[^>]*>`).FindString(got)
	if !strings.Contains(body, `dir="rtl"`) || strings.Contains(body, `dir="auto"`) {
		t.Errorf("the body must follow the page direction: %s", body)
	}
	if !strings.Contains(got, `<p dir="auto">`) {
		t.Errorf("the paragraph must keep its own direction: %s", got)
	}
	m.Locale, m.Direction = "en-US", "ltr"
	body = regexp.MustCompile(`<div[^>]*message-body[^>]*>`).FindString(chatlangMessageMarkup(t, m, msg, localUI{}))
	if !strings.Contains(body, `dir="auto"`) {
		t.Errorf("a left-to-right page keeps automatic direction: %s", body)
	}
}

func TestIssue900005_RTLBodyDirHelper(t *testing.T) {
	for _, c := range []struct{ dir, page, want string }{
		{"auto", "rtl", "rtl"}, {"auto", "ltr", "auto"}, {"auto", "", "auto"}, {"ltr", "rtl", "ltr"}, {"rtl", "rtl", "rtl"},
	} {
		if got := issue900005BodyDir(Model{Direction: c.page}, c.dir); got != c.want {
			t.Errorf("dir %q in a %q page: got %q want %q", c.dir, c.page, got, c.want)
		}
	}
}

// #900005 part 3: the shortcuts sheet is reachable from visible UI as well as
// from the question mark key.
func TestIssue900005_ShortcutsOpenFromChatPreferences(t *testing.T) {
	want := map[string][]string{
		"en-US": {"Keyboard shortcuts", "Show keyboard shortcuts"},
		"de-DE": {"Tastenkürzel", "Tastenkürzel anzeigen"},
		"ar":    {"اختصارات لوحة المفاتيح", "عرض اختصارات لوحة المفاتيح"},
	}
	for locale, words := range want {
		m := chat4Fixture(locale, "sent", false)
		markup := renderNode(t, chatux002PrefsControl(m, handlers{}))
		button := regexp.MustCompile(`<button[^>]*data-action="shortcuts-open"[^>]*>`).FindString(markup)
		if button == "" || !strings.Contains(button, `type="button"`) {
			t.Fatalf("%s: no Show keyboard shortcuts button in the preferences panel: %s", locale, markup)
		}
		for _, w := range words {
			if !strings.Contains(markup, w) {
				t.Errorf("%s: preferences panel lacks %q", locale, w)
			}
		}
		if !strings.Contains(markup, "aria-haspopup=\"dialog\"") || !strings.Contains(markup, "?") {
			t.Errorf("%s: the button must say it opens a dialog and the row must name the key", locale)
		}
	}
}

func TestIssue900005_QuestionMarkOpensSheetOnAnyLayout(t *testing.T) {
	// Shift+/ on a US keyboard, Shift+ß on a German one and Arabic's Shift+Slash
	// all type "?"; the code of the physical key does not matter.
	for _, code := range []string{"Slash", "Minus", "IntlRo", ""} {
		if got := issue3744Match("?", code, false, true, false, false); got != issue3744Sheet {
			t.Errorf("? on %q: %q", code, got)
		}
	}
	if got := issue3744Match("?", "Slash", false, true, true, false); got != "" {
		t.Errorf("Ctrl+? belongs to the browser: %q", got)
	}
}

// The binding decides "a dialog or menu is open" from what is visible. A closed
// popover or drawer that carries role=dialog without the hidden attribute must
// not silence the key.
func TestIssue900005_KeyboardGuardCountsOnlyVisibleLayers(t *testing.T) {
	source, err := os.ReadFile("issue3744_keyboard_js.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "chatKeyboardLayerVisible") {
		t.Error("chatKeyboardBlocked must filter open layers through a visibility check")
	}
}
