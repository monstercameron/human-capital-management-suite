package productui

import (
	"strings"
	"testing"
)

// TestTodo_UIPOLISH_005_CustomerThemeAdmission proves the browser stylesheet
// uses the same admission boundary as the resolved light and dark themes.
// The assertions intentionally exercise the returned values and error text;
// a call-only test could pass while bypassing qualification entirely.
func TestTodo_UIPOLISH_005_CustomerThemeAdmission(t *testing.T) {
	t.Run("unsafe brand is refused", func(t *testing.T) {
		_, err := StylesheetForTheme(map[string]string{
			"color.brand.primary": "#ffffff",
			"color.brand.hover":   "#ffffff",
		})
		if err == nil {
			t.Fatal("unsafe white brand was admitted")
		}
		if !strings.Contains(err.Error(), "customer theme admission rejected") ||
			!strings.Contains(err.Error(), "contrast") {
			t.Fatalf("unsafe brand error is not actionable: %v", err)
		}
	})

	t.Run("valid light admission reaches production CSS", func(t *testing.T) {
		css, err := StylesheetForTheme(map[string]string{
			"color.brand.primary": "#7c3aed",
			"color.brand.hover":   "#5b21b6",
			"color.brand.soft":    "#f3e8ff",
		})
		if err != nil {
			t.Fatalf("valid light customer theme rejected: %v", err)
		}
		if !strings.Contains(css, "--hcm-color-brand-primary:#7c3aed") {
			t.Fatalf("admitted light theme was not compiled into production CSS: %s", css)
		}
	})

	t.Run("dark admission qualifies effective rendered values", func(t *testing.T) {
		modes, err := ResolveThemeModes(map[string]string{
			"color.brand.primary": "#7c3aed",
			"color.brand.hover":   "#5b21b6",
		})
		if err != nil {
			t.Fatalf("customer theme rejected while qualifying modes: %v", err)
		}
		light := modes[ThemeModeLight]
		dark := modes[ThemeModeDark]
		lightPrimary, lightOK := light.Value("color.brand.primary")
		darkPrimary, darkOK := dark.Value("color.brand.primary")
		if !lightOK || !darkOK || lightPrimary == darkPrimary {
			t.Fatalf("dark mode did not expose an effective qualified brand: light=%q dark=%q", lightPrimary, darkPrimary)
		}
		if failures := validateThemeContrast(dark.values); len(failures) != 0 {
			t.Fatalf("effective dark theme contains unsafe semantic pairs: %v", failures)
		}
	})
}
