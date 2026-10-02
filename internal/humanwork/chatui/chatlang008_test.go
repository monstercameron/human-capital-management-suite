package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

// TestTodo_CHATLANG_008_Browser: the conversation banner is one line in every
// language with one "Show originals" switch, is not drawn again once it has been
// dismissed, a translation on its way keeps the original with a small
// "Translating", and "Not specified" is never put in front of a person.
func TestTodo_CHATLANG_008_Browser(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "Show originals", "de-DE": "Originale anzeigen", "ar": "عرض النصوص الأصلية"} {
		m, _, _, _ := chatlangModel(locale)
		bar := renderNode(t, chatlangBar(m, handlers{}))
		if strings.Count(bar, want) != 1 {
			t.Errorf("%s: the banner has %d %q switches, want one: %s", locale, strings.Count(bar, want), want, bar)
		}
		// Controls named for assistive technology, the sentence is not clipped away from them.
		for _, need := range []string{`data-action="chatlang-originals"`, `data-action="chatlang-settings"`, `data-action="chatlang-bar-dismiss"`, `aria-pressed="false"`} {
			if !strings.Contains(bar, need) {
				t.Errorf("%s: the banner lacks %s: %s", locale, need, bar)
			}
		}
		if strings.Contains(bar, "⟦") {
			t.Errorf("%s: the banner prints a copy key: %s", locale, bar)
		}
		if chatlangBar(m, handlers{local: localUI{chatlang: chatlangLocal{barDismissed: true}}}) != nil {
			t.Errorf("%s: a dismissed banner is drawn again", locale)
		}
	}
	// One line: the row does not wrap and the sentence gives way with an ellipsis,
	// whatever the language.
	for _, rule := range []string{".chatlang-bar{display:flex;flex-wrap:nowrap", ".chatlang-bar-text{flex:1 1 auto;min-inline-size:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}"} {
		if !strings.Contains(ChatlangStyles, rule) {
			t.Errorf("the banner style lacks %q", rule)
		}
	}

	// A translation on its way: the original, with "Translating…".
	m, _, _, arabic := chatlangModel("en-US")
	waiting := chatlangMessageMarkup(t, m, arabic, localUI{})
	if !strings.Contains(waiting, chatlangArabic) || !strings.Contains(waiting, "Translating…") {
		t.Fatalf("a pending translation: %s", waiting)
	}

	// "Not specified" is never shown: the languages of a conversation list only
	// the languages people read.
	indicator := renderNode(t, RenderingLanguageStatusIndicator("en-US", map[string]int{"en": 1, "und": 17}, false, false))
	if strings.Contains(indicator, "Not specified") || !strings.Contains(indicator, "English: 1") {
		t.Fatalf("the language list: %s", indicator)
	}
	for _, locale := range []string{"de-DE", "ar"} {
		if got := renderNode(t, RenderingLanguageStatusIndicator(locale, map[string]int{"und": 3}, false, false)); strings.Contains(got, RenderingText(locale, "und")) {
			t.Fatalf("%s: an unspecified language is shown: %s", locale, got)
		}
	}
	// A message whose selection says its own author wrote it is not marked as translated.
	own := ReaderSelection{Revision: 1,
		Rendering: chatrender.Rendering{Message: "en", Revision: 1, Tone: chatrender.AsWritten, Language: "en", SourceLanguage: "en", Text: chatlangEnglish},
		Mark:      chatrender.Mark{State: "original", SourceLanguage: "en", CanShowOriginal: true}}
	m.ReaderSelections["en"] = own
	if got := chatlangMessageMarkup(t, m, m.Messages[1], localUI{}); strings.Contains(got, "Translated from") || strings.Contains(got, "Show original") {
		t.Fatalf("a message left as written carries a translation mark: %s", got)
	}
}
