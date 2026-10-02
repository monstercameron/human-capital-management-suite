package productui

import (
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_059_Catalog: every key of Chat's core copy is answered in
// German and in Arabic by the product catalog itself, not by the English
// fallback, so a new English sentence cannot reach a German or Arabic page
// untranslated. The few words each language shares with English are listed.
func TestTodo_CHATBUG_059_Catalog(t *testing.T) {
	// Words German writes as English does (a proper noun, a loan word).
	shared := map[string]map[string]bool{
		"de-DE": {"Moderation": true, "Agent": true, "Chat": true, "Status": true, "Name": true, "Link": true, "Filter": true, "Details": true, "Thread": true, "Apps": true, "GIF": true, "Code": true, "Emoji": true, "Online": true, "Offline": true, "Person": true, "Version:": true, "Team": true, "OK": true,
			// A template that is only placeholders, a loan word or a word both languages spell alike.
			"Emoji {emoji}": true, "Workflow · {workflow}": true, "{workflow}: {name}": true, "Normal": true, "In {channel}": true, "in {channel}": true, "Thread · {name}": true},
		"ar": {"GIF": true, "{workflow}: {name}": true},
	}
	keys := make([]string, 0, len(chatui.EnglishCopy()))
	for key := range chatui.EnglishCopy() {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, locale := range []string{"de-DE", "ar"} {
		ctx := ResolveProductLocale(locale)
		for _, key := range keys {
			english := chatui.EnglishCopy()[key]
			result, err := ctx.Resolve(key)
			if err != nil {
				t.Errorf("%s: %s: %v", locale, key, err)
				continue
			}
			if result.Locale != ctx.Resolved || result.Text == english && !shared[locale][english] {
				t.Errorf("%s: %s is not translated: %q (answered by %s)", locale, key, result.Text, result.Locale)
			}
		}
	}
}
