package productui

import "testing"

// TestTodo_UIPOLISH_005_ProductionStates checks the effective resolved values
// (including dark-mode brand blends), rather than a disconnected reference
// palette. Disabled and visited have no independent color tokens in the
// production contract: disabled contrast is intentionally not scored under
// WCAG 1.4.3, and visited inherits the link semantic color.
func TestTodo_UIPOLISH_005_ProductionStates(t *testing.T) {
	for _, overrides := range []map[string]string{nil, {"color.brand.primary": "#7c3aed", "color.brand.hover": "#5b21b6"}} {
		modes, err := ResolveThemeModes(overrides)
		if err != nil {
			t.Fatalf("ResolveThemeModes(%v): %v", overrides, err)
		}
		for _, mode := range []ThemeMode{ThemeModeLight, ThemeModeDark} {
			theme := modes[mode]
			pairs := []struct {
				name, foreground, background string
				minimum                      float64
			}{
				{"focus ring vs canvas", "color.focus", "color.canvas", 3},
				{"brand text vs primary", "color.on.brand", "color.brand.primary", 4.5},
				{"brand text vs hover", "color.on.brand", "color.brand.hover", 4.5},
				{"primary action vs surface", "color.brand.primary", "color.surface", 4.5},
				{"hover action vs surface", "color.brand.hover", "color.surface", 4.5},
				{"selected surface text", "color.text.primary", "color.brand.soft", 4.5},
				{"success status", "color.status.success", "color.status.success.surface", 4.5},
				{"warning status", "color.status.warning", "color.status.warning.surface", 4.5},
				{"danger status", "color.status.danger", "color.status.danger.surface", 4.5},
				{"info status", "color.status.info", "color.status.info.surface", 4.5},
			}
			for _, pair := range pairs {
				foreground, fok := theme.Value(pair.foreground)
				background, bok := theme.Value(pair.background)
				if !fok || !bok {
					t.Fatalf("%s %s omitted semantic state values", mode, pair.name)
				}
				ratio, err := contrastRatio(foreground, background)
				if err != nil || ratio < pair.minimum {
					t.Errorf("%s %s contrast %.2f:1, want >= %.1f:1 (%v)", mode, pair.name, ratio, pair.minimum, err)
				}
			}
		}
	}
}
