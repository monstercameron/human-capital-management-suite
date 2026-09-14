package productui

import (
	"strings"
	"testing"
)

// finalThemeCoverageLayer renders the terminal cascade boundary exactly as
// the production sheet assembles it: theme coverage base, then the Journey
// focus bridge, then the platform focus boundary (the dark-mode tail).
func finalThemeCoverageLayer() string {
	return buildTypedSheet(func() {
		declareThemeCoverageBaseStylesStyles()
		declareJourneyFocusBridge()
		declareFocusStyles()
	})
}

func TestEveryRegisteredPageReceivesTheCompleteThemeContract(t *testing.T) {
	appearance := CustomerTheme{
		BrandName: "Northstar People", BrandMark: "NP", ColorMode: "dark", Palette: "plum", Shape: "rounded",
		Density: "compact", Glyphs: "bold-line", Typeface: "classic", Navigation: "brand", Motion: "brisk",
	}
	wants := []string{
		`data-hcm-color-mode="dark"`,
		`data-hcm-palette="plum"`, `data-hcm-shape="rounded"`, `data-hcm-density="compact"`,
		`data-hcm-glyphs="bold-line"`, `data-hcm-typeface="classic"`, `data-hcm-navigation="brand"`,
		`data-hcm-motion="brisk"`, `data-hcm-brand-name`, `>Northstar People</span>`,
	}
	for _, definition := range PageDefinitions() {
		t.Run(string(definition.ID), func(t *testing.T) {
			view := testView(definition.ID)
			view.Appearance = appearance
			document, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range wants {
				if !strings.Contains(document, want) {
					t.Errorf("page %q dropped theme contract %q", definition.ID, want)
				}
			}
			if !strings.Contains(document, `<title>`+escapeTitle(ResolveDocumentPageTitle(view))+` · Northstar People</title>`) {
				t.Errorf("page %q did not brand its document title", definition.ID)
			}
			if !strings.Contains(document, `data-hcm-brand-name="">Northstar People</span>`) {
				t.Errorf("page %q did not brand its footer", definition.ID)
			}
		})
	}
}

func TestEveryProductionPageFamilyConsumesSemanticThemeTokens(t *testing.T) {
	css := Stylesheet()
	if !strings.HasSuffix(css, finalThemeCoverageLayer()) {
		t.Fatal("theme coverage layer is not the final cascade boundary")
	}
	for _, selector := range []string{
		".home-grid", ".workbench", ".history-row", ".people-workspace", ".person-page", ".org-node",
		".insights-grid", ".admin-grid", ".appearance-page", ".studio-shell", ".support-request", ".settings-shell", ".jn-embedded",
	} {
		if !strings.Contains(finalThemeCoverageLayer(), selector) {
			t.Errorf("production page family %q is absent from the final theme coverage layer", selector)
		}
	}
	for _, token := range []string{
		"--surface-subtle:", "--control-border:", "--theme-section-gap:", "var(--hcm-font-sans)",
		"var(--hcm-radius-control)", "var(--hcm-radius-surface)", "var(--hcm-motion-fast)",
	} {
		if !strings.Contains(finalThemeCoverageLayer(), token) {
			t.Errorf("cross-component theme semantic %q is missing", token)
		}
	}
}

func TestEmbeddedJourneysUseTheCompleteProductThemeVocabulary(t *testing.T) {
	for _, token := range []string{
		"--jn-canvas:var(--canvas)", "--jn-surface:var(--surface)", "--jn-control-border:var(--control-border)",
		"--jn-masthead:var(--accent)", "--jn-accent-ink:var(--on-brand)", "--jn-info:var(--info)",
		"--jn-success:var(--success)", "--jn-warning:var(--warning)", "--jn-danger:var(--danger)",
		"--jn-font:var(--hcm-font-sans)", "--jn-mono:var(--hcm-font-mono)",
		"--jn-s2:calc(var(--hcm-space-2) * var(--hcm-density))", "--jn-r3:var(--hcm-radius-surface)",
	} {
		if !strings.Contains(finalThemeCoverageLayer(), token) {
			t.Errorf("embedded journey theme bridge missing %q", token)
		}
	}
}

func TestCustomerThemeCanRestyleEverySharedSurfaceWithoutRawCSS(t *testing.T) {
	css, err := StylesheetForTheme(map[string]string{
		"color.brand.primary": "#4d1f78", "color.brand.hover": "#371455", "color.brand.soft": "#f3eafb",
		"color.text.primary": "#20142b", "color.text.muted": "#5e5167", "color.canvas": "#fbf9fd",
		"color.surface": "#ffffff", "color.border": "#d9cfdf", "radius.control": "6px", "radius.surface": "14px",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--hcm-color-brand-primary:#4d1f78", "--hcm-color-canvas:#fbf9fd", "--hcm-color-surface:#ffffff",
		"--hcm-color-border:#d9cfdf", "--hcm-radius-control:6px", "--hcm-radius-surface:14px", finalThemeCoverageLayer(),
	} {
		if !strings.Contains(css, want) {
			t.Errorf("complete themed stylesheet missing %q", want)
		}
	}
	if _, err := StylesheetForTheme(map[string]string{"color.status.success": "#ff00ff"}); err == nil {
		t.Fatal("customer theme changed a protected semantic status color")
	}
}
