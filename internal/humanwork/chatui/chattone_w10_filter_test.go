package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// TestTodo_CHATTONE_002_FilterActionCopy: "Reword" is a choice of the filter
// editor and of the built-in lists, and a filter saved with it reads in words in
// every moderation listing, never as a raw key, in English, German and Arabic.
func TestTodo_CHATTONE_002_FilterActionCopy(t *testing.T) {
	if !modContains(ModActions, "reword") || !modContains(ModBuiltinActions, "reword") || modContains(ModBuiltinActions, "notify") {
		t.Fatalf("the choices are %v and %v", ModActions, ModBuiltinActions)
	}
	custom := chatfilter.Definition{ID: "sarcasm-ab12cd", Name: "Sarcasm", Version: "1.0.0", Kind: "words", Action: "reword", Match: []string{"great job"}}
	for _, locale := range chatmodLocales {
		m := Model{Locale: locale}
		for _, key := range []string{"act_reword", "ah_reword", "v_reword", "o_reword", "h_reword", "h_dry_reword"} {
			text := modadminText(m, key)
			if text == "" || text == key || strings.ContainsAny(text, "⟦⟧") {
				t.Errorf("%s %s: %q", locale, key, text)
			}
		}
		// The listing's one line about the filter.
		if line := modDescribe(m, custom); line == "" || strings.Contains(line, "v_reword") || strings.Contains(line, "{what}") {
			t.Errorf("%s: the listing line is %q", locale, line)
		}
		// What happened to a message it caught, and what would have.
		for _, dry := range []bool{false, true} {
			got := modHitOutcome(m, chatfilter.Hit{Action: "reword", DryRun: dry})
			if got == "" || got == modadminText(m, "h_other") || strings.Contains(got, "h_") {
				t.Errorf("%s dry=%v: the hit outcome is %q", locale, dry, got)
			}
		}
		// The sentence under "Try it".
		nodes := modOutcomeNodes(m, ModOutcome{Kind: "reword", Rule: "Sarcasm"}, false)
		if len(nodes) != 1 || !strings.Contains(renderNode(t, nodes[0]), modadminText(m, "o_reword")) {
			t.Errorf("%s: the Try it outcome for a reword filter does not say so", locale)
		}
	}
	if modadminText(Model{Locale: "en-US"}, "act_reword") != "Reword for readers" {
		t.Fatalf("the action is named %q", modadminText(Model{Locale: "en-US"}, "act_reword"))
	}
}

// TestTodo_CHATLANG_005_SearchWrittenIn: the search page offers "Written in" in
// each language, and a "lang:de" filter shows as a chip a person can remove.
func TestTodo_CHATLANG_005_SearchWrittenIn(t *testing.T) {
	words := map[string]struct{ label, any, chip string }{
		"en-US": {"Written in", "Any language", "Remove filter: Written in German"},
		"de-DE": {"Geschrieben auf", "Jede Sprache", "Filter entfernen: Geschrieben auf Deutsch"},
		"ar":    {"مكتوبة بـ", "أي لغة", "إزالة المرشح: مكتوبة بـ الألمانية"},
	}
	for locale, want := range words {
		markup := renderNode(t, RenderChatSearch(locale, ChatSearchView{Query: "budget lang:de"}))
		for _, text := range []string{want.label, want.any, `name="written"`, `value="de"`, `value="ar"`, want.chip} {
			if !strings.Contains(markup, text) {
				t.Errorf("%s: the search page misses %q", locale, text)
			}
		}
	}
}
