package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_CHATUX_018 reads the two halves of the entry: a blocked draft names
// its word, in the warning colour, with the word underlined in the text shown
// under the line; and choosing to hide a word shows what readers will see.
func TestTodo_CHATUX_018(t *testing.T) {
	m := Model{Locale: "en-US", AuthorBlocked: map[string]AuthorBlocked{"composer:room": {Surface: ModAuthorSurfaceMessage, Words: []string{"damn"}, Text: "Well damn, that was close", Stamp: 1}}}
	markup := renderNode(t, modAuthorLine(m, localUI{}, "composer:room", "blocked"))
	for _, want := range []string{`role="alert"`, `<mark class="chatmod002-word">damn</mark>`, `class="chatmod002-draft"`, `aria-hidden="true"`, "Well ", " that was close"} {
		if !strings.Contains(markup, want) {
			t.Errorf("the blocked line lacks %q: %s", want, markup)
		}
	}
	// A word the filter did not name, or no word, marks nothing; the one line stays.
	m.AuthorBlocked["composer:room"] = AuthorBlocked{Surface: ModAuthorSurfaceMessage, Text: "no offsets", Stamp: 2}
	if plain := renderNode(t, modAuthorLine(m, localUI{}, "composer:room", "blocked")); strings.Contains(plain, "chatmod002-draft") || !strings.Contains(plain, `role="alert"`) {
		t.Errorf("a refusal without words: %s", plain)
	}
	// A long draft is cut around the word.
	long := strings.Repeat("filler ", 60) + "damn " + strings.Repeat("more ", 60)
	cut := renderNode(t, chatux018Draft(AuthorBlocked{Words: []string{"damn"}, Text: long}))
	if !strings.Contains(cut, ">damn</mark>") || len(cut) > 800 {
		t.Errorf("a long draft is not cut around the word: %d bytes", len(cut))
	}
	if got := renderNode(t, ui.Fragment(chatux018Draft(AuthorBlocked{Text: "text"}))); strings.Contains(got, "chatmod002-draft") {
		t.Error("a draft with no named word is drawn twice")
	}
}

// TestTodo_CHATUX_018_Browser holds the panel's type scale and the example line
// that "Hide the word from readers" shows, in the three languages.
func TestTodo_CHATUX_018_Browser(t *testing.T) {
	for _, want := range []string{
		`.chat-workspace .chat-details .chatmod{font-size:.75rem;gap:12px}`,
		`.chat-workspace .chat-details .chatmod h4{font-size:.8125rem}`,
		`.chat-workspace .chatmod002-blocked{padding-inline-start:8px;border-inline-start:3px solid var(--hcm-color-warning);color:var(--hcm-color-warning)`,
		`text-decoration:underline wavy var(--hcm-color-warning)`,
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("the stylesheet lacks %q", want)
		}
	}
	for locale, want := range map[string]string{"en-US": "For example:", "de-DE": "Zum Beispiel:", "ar": "على سبيل المثال:"} {
		m := Model{Locale: locale}
		if got := renderNode(t, RenderFilterMaskedText(m, modadminText(m, "ex_mask"))); !strings.Contains(got, want) || !strings.Contains(got, `chatfilter-removed`) {
			t.Errorf("%s: the example reads %s", locale, got)
		}
	}
}
