package chatui_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// selectedReading returns the value of the option marked selected in the
// reading-language select of the rendered markup, or "" when none is marked.
func selectedReading(t *testing.T, markup string) string {
	t.Helper()
	for _, opt := range regexp.MustCompile(`<option[^>]*>`).FindAllString(markup, -1) {
		if strings.Contains(opt, "selected") {
			if m := regexp.MustCompile(`value="([^"]*)"`).FindStringSubmatch(opt); m != nil {
				return m[1]
			}
		}
	}
	return ""
}

// A person who never chose a reading language has an empty stored value; the
// select must show their interface language, not whatever option the browser
// falls back to (the last one, Hindi).
func TestIssue3682_EmptyReadingLanguageShowsInterfaceLanguage(t *testing.T) {
	for _, c := range []struct{ locale, pref, want string }{
		{"en-US", "", "en"}, {"en-US", "und", "en"}, {"de-DE", "", "de"}, {"ar", "", "ar"},
		{"en-US", "de", "de"}, {"de-DE", "en", "en"}, {"xx-XX", "", "en"},
	} {
		for _, auto := range []bool{false, true} {
			markup, err := ui.RenderToString(chatui.RenderingSettings(chatui.RenderingSettingsModel{Locale: c.locale, Preference: chatrender.Preference{ReadingLanguage: c.pref, Translate: true}, AutoSave: auto}))
			if err != nil {
				t.Fatal(err)
			}
			if got := selectedReading(t, markup); got != c.want {
				t.Errorf("locale %q stored %q autosave=%v: selected %q, want %q", c.locale, c.pref, auto, got, c.want)
			}
		}
	}
}
