package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_067_ColorMode(t *testing.T) {
	organization := DefaultCustomerTheme()
	organization.ColorMode = "light"
	if got := EffectiveColorMode(organization, "").ColorMode; got != "light" {
		t.Fatalf("unset personal color mode = %q, want organization light", got)
	}
	if got := EffectiveColorMode(organization, "dark").ColorMode; got != "dark" {
		t.Fatalf("personal dark override = %q, want dark", got)
	}
	if got := EffectiveColorMode(organization, "sepia").ColorMode; got != "light" {
		t.Fatalf("invalid personal color mode = %q, want organization light", got)
	}
}

func TestTodo_UXBLIND_067_ColorMode_Browser(t *testing.T) {
	props := AccessibilityPreferencesProps{
		I18nProps:  I18nProps{Locale: ResolveProductLocale("en-US")},
		Value:      AccessibilityPreferences{ColorMode: "dark"},
		ColorModes: AccessibilityColorModeOptions(),
	}
	markup, err := ui.RenderToString(AccessibilityPreferencesPanel(props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name="color-mode"`, `aria-label="Use organization setting"`, `aria-label="Light"`, `aria-label="Dark"`, `value="dark"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("personal color mode control missing %q: %s", want, markup)
		}
	}
}

func TestTodo_UXBLIND_067_ColorMode_Accessibility(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		props := AccessibilityPreferencesProps{
			I18nProps: I18nProps{Locale: ResolveProductLocale(locale)},
			Value:     AccessibilityPreferences{}, ColorModes: AccessibilityColorModeOptions(),
		}
		markup, err := ui.RenderToString(AccessibilityPreferencesPanel(props))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(markup, `aria-label="accessibility.color_mode`) || strings.Contains(markup, `aria-label="organization"`) {
			t.Fatalf("%s personal color mode has an untranslated or raw accessible name: %s", locale, markup)
		}
	}
}
