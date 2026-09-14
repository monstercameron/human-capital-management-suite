package productui

import "testing"

func TestTodo_UIPOLISH_007_WorkSelectionRecoveryAcrossLocales(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		ctx := ResolveProductLocale(locale)
		for _, key := range []string{"work.nothing_selected", "work.nothing_detail", "work.show_all"} {
			value := ctx.Text(key)
			if value == "" || value == key || (locale != "en-US" && value == ResolveProductLocale("en-US").Text(key)) {
				t.Errorf("%s %s fell back to an absent or English-only recovery message: %q", locale, key, value)
			}
		}
	}
}
