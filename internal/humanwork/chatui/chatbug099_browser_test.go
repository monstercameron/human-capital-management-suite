package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_099_Browser opens the group dialog on the served page in
// each language: the name is optional with its own hint, two or more people
// are asked for, and the channel rule never appears. The selection announcer
// rides every page.
func TestTodo_CHATBUG_099_Browser(t *testing.T) {
	placeholders := map[string]string{"en-US": "Optional", "de-DE": "Optional", "ar": "اختياري"}
	hints := map[string]string{
		"en-US": "Leave blank to name the group after its people.",
		"de-DE": "Leer lassen, um die Gruppe nach ihren Mitgliedern zu benennen.",
		"ar":    "اتركه فارغًا لتسمية المجموعة بأسماء أعضائها.",
	}
	people := map[string]string{
		"en-US": "Add two or more people",
		"de-DE": "Mindestens zwei Personen hinzufügen",
		"ar":    "أضف شخصين أو أكثر",
	}
	for _, locale := range w7Locales {
		page := w7Page(t, locale, func(m *chatui.Model) {
			m.ShowCreate, m.NewKind = true, chatui.GroupChat
		})
		for _, want := range []string{placeholders[locale], hints[locale], people[locale]} {
			if !strings.Contains(page, want) {
				t.Errorf("%s: the group dialog misses %q", locale, want)
			}
		}
		for _, channelOnly := range []string{"Lowercase, no spaces", "design-reviews"} {
			if locale == "en-US" && strings.Contains(page, channelOnly) {
				t.Errorf("%s: the group dialog shows the channel rule %q", locale, channelOnly)
			}
		}
		if !strings.Contains(page, `id="new-chat-name"`) {
			t.Errorf("%s: the group dialog lost its name field", locale)
		}
		nameAt := strings.Index(page, `id="new-chat-name"`)
		field := page[strings.LastIndex(page[:nameAt], "<input"):]
		if strings.Contains(field[:strings.Index(field, ">")+1], "required") {
			t.Errorf("%s: the group name is required", locale)
		}
		if !strings.Contains(page, `id="chat-selection-announcer"`) {
			t.Errorf("%s: the selection announcer is missing", locale)
		}
	}
}
