package productui

import (
	"fmt"
	"strings"
	"testing"
)

func TestTodo_UXSCAN_006_PreviewBrandUsesSurfaceInk(t *testing.T) {
	css := Stylesheet()
	if strings.Contains(css, ".wordmark-mark,.appearance-preview-bar") ||
		strings.Contains(css, ".button.primary,.wordmark-mark,.appearance-preview-bar") {
		t.Fatal("neutral preview header inherits the on-brand foreground")
	}
	if !strings.Contains(css, ".appearance-preview-bar{") ||
		!strings.Contains(css, "color:var(--ink)") {
		t.Fatal("preview header has no semantic surface-ink foreground")
	}
	for _, preset := range palettePresets {
		if preset.Option.ID == "custom" {
			continue
		}
		modes, err := ResolveThemeModes(preset.Overrides)
		if err != nil {
			t.Fatal(err)
		}
		assertPreviewContrast(t, preset.Option.ID, modes)
		for _, mode := range []ThemeMode{ThemeModeLight, ThemeModeDark} {
			selector := fmt.Sprintf(`:root[data-hcm-palette="%s"] .appearance-preview-window[data-hcm-preview-color-mode="%s"]`, preset.Option.ID, mode)
			if !strings.Contains(css, selector) {
				t.Errorf("%s %s preview does not resolve its own palette", preset.Option.ID, mode)
			}
		}
	}
	custom, err := ResolveThemeModes(map[string]string{
		"color.brand.primary": "#7c3aed",
		"color.brand.hover":   "#5b21b6",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertPreviewContrast(t, "custom", custom)
}

func assertPreviewContrast(t *testing.T, palette string, modes map[ThemeMode]Theme) {
	t.Helper()
	for _, mode := range []ThemeMode{ThemeModeLight, ThemeModeDark} {
		resolved := modes[mode]
		ink, _ := resolved.Value("color.text.primary")
		surface, _ := resolved.Value("color.surface")
		ratio, err := contrastRatio(ink, surface)
		if err != nil || ratio < 4.5 {
			t.Errorf("%s %s preview header contrast %.2f:1, want >= 4.5:1 (%v)", palette, mode, ratio, err)
		}
	}
}
