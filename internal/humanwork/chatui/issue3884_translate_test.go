package chatui

import (
	"regexp"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func issue3884Switch(t *testing.T, locale string, auto, unavailable bool) string {
	t.Helper()
	probe := func(struct{}) ui.Node {
		return RenderingSettings(RenderingSettingsModel{Locale: locale, AutoSave: auto, TranslationUnavailable: unavailable, Submit: ui.UseEvent(func() {})})
	}
	markup, err := ui.RenderToString(ui.CreateElement(probe, struct{}{}))
	if err != nil {
		t.Fatal(err)
	}
	box := regexp.MustCompile(`<input[^>]*id="chatrender-translate"[^>]*>`).FindString(markup)
	if box == "" {
		t.Fatalf("no translate switch: %s", markup)
	}
	return box
}

// TestIssue3884_TranslateTitleOnlyWhenUnavailable: an enabled switch carries no
// "not available" tooltip; a disabled one explains why, in every language.
func TestIssue3884_TranslateTitleOnlyWhenUnavailable(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, auto := range []bool{true, false} {
			enabled := issue3884Switch(t, locale, auto, false)
			if regexp.MustCompile(`title=`).MatchString(enabled) || regexp.MustCompile(`\sdisabled`).MatchString(enabled) {
				t.Errorf("%s auto=%v: an enabled switch has a title or is disabled: %s", locale, auto, enabled)
			}
			off := issue3884Switch(t, locale, auto, true)
			if !regexp.MustCompile(`title="[^"]+"`).MatchString(off) || !regexp.MustCompile(`\sdisabled`).MatchString(off) {
				t.Errorf("%s auto=%v: an unavailable switch is not disabled with its explanation: %s", locale, auto, off)
			}
		}
	}
}
