package productui

import (
	"fmt"
	"strings"
	"testing"
)

func TestCustomerPaletteCatalogQualifiesBothModesAndEverySwatch(t *testing.T) {
	css := Stylesheet()
	for _, preset := range palettePresets {
		t.Run(preset.Option.ID, func(t *testing.T) {
			if preset.Option.ID == "custom" {
				if len(preset.Option.Swatches) != 0 {
					t.Fatal("authored colors must not display a misleading fixed swatch")
				}
				return
			}
			if len(preset.Option.Swatches) != 3 {
				t.Fatalf("palette has %d swatches, want brand, supporting surface and ink", len(preset.Option.Swatches))
			}
			modes, err := ResolveThemeModes(preset.Overrides)
			if err != nil {
				t.Fatalf("palette failed light/dark admission: %v", err)
			}
			for _, mode := range []ThemeMode{ThemeModeLight, ThemeModeDark} {
				if modes[mode].values["color.brand.primary"] == "" {
					t.Errorf("%s mode has no brand color", mode)
				}
			}
			for index, swatch := range preset.Option.Swatches {
				selector := fmt.Sprintf(".swatch-%s-%d{background-color:%s;}", preset.Option.ID, index+1, swatch)
				if !strings.Contains(css, selector) {
					t.Errorf("preview is missing %q", selector)
				}
			}
			if !strings.Contains(css, `:root[data-hcm-palette="`+preset.Option.ID+`"]`) && preset.Option.ID != DefaultCustomerTheme().Palette {
				t.Error("palette does not reach the product theme engine")
			}
		})
	}
}

func TestEveryCustomerShapeIsAdmittedAndRendered(t *testing.T) {
	css := Stylesheet()
	for _, preset := range shapePresets {
		if _, err := ResolveTheme(preset.Overrides); err != nil {
			t.Errorf("shape %q failed admission: %v", preset.Option.ID, err)
		}
		if len(preset.Overrides) > 0 && !strings.Contains(css, `:root[data-hcm-shape="`+preset.Option.ID+`"]`) {
			t.Errorf("shape %q has no stylesheet rule", preset.Option.ID)
		}
	}
}

func TestAppearanceSpecimensKeepLightAndDarkIndependentOfWorkspaceMode(t *testing.T) {
	css := Stylesheet()
	for _, preset := range palettePresets {
		if preset.Option.ID == "custom" {
			continue
		}
		modes, err := ResolveThemeModes(preset.Overrides)
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []ThemeMode{ThemeModeLight, ThemeModeDark} {
			selector := fmt.Sprintf(`:root[data-hcm-palette="%s"] .appearance-preview-window[data-hcm-preview-color-mode="%s"]{`, preset.Option.ID, mode)
			start := strings.Index(css, selector)
			if start < 0 {
				t.Errorf("%s %s specimen has no scoped theme", preset.Option.ID, mode)
				continue
			}
			end := strings.IndexByte(css[start:], '}')
			if end < 0 {
				t.Fatalf("%s %s specimen rule is incomplete", preset.Option.ID, mode)
			}
			rule := css[start : start+end]
			for name, token := range map[string]string{"--accent:": "color.brand.primary", "--canvas:": "color.canvas", "--ink:": "color.text.primary", "--on-brand:": "color.on.brand"} {
				value, _ := modes[mode].Value(token)
				if !strings.Contains(rule, name+value) {
					t.Errorf("%s %s specimen missing %s%s", preset.Option.ID, mode, name, value)
				}
			}
		}
	}
}

func TestAuthoredColorsAreAdmittedAndChangeEverySharedSurface(t *testing.T) {
	custom := CustomerTheme{Palette: "custom", TokenOverrides: map[string]string{
		"color.brand.primary": "#4d1f78", "color.brand.hover": "#371455", "color.brand.soft": "#f3eafb",
		"color.text.primary": "#20142b", "color.text.muted": "#5e5167", "color.canvas": "#fbf9fd",
		"color.surface": "#ffffff", "color.border": "#d9cfdf",
	}}
	if err := ValidateCustomerTheme(custom); err != nil {
		t.Fatal(err)
	}
	css, err := StylesheetForTheme(custom.TokenOverrides)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--hcm-color-brand-primary:#4d1f78", "--hcm-color-canvas:#fbf9fd", "--hcm-color-border:#d9cfdf",
		`:root[data-hcm-palette="custom"] .appearance-preview-window[data-hcm-preview-color-mode="light"]`,
		`:root[data-hcm-palette="custom"] .appearance-preview-window[data-hcm-preview-color-mode="dark"]`,
		finalThemeCoverageLayer(),
	} {
		if !strings.Contains(css, want) {
			t.Errorf("authored theme missing %q", want)
		}
	}
	if css == Stylesheet() {
		t.Fatal("authored color sheet did not differ from the default")
	}
	for name, overrides := range map[string]map[string]string{
		"raw css":  {"color.brand.primary": "red;body{display:none}"},
		"status":   {"color.status.success": "#ff00ff"},
		"shape":    {"radius.surface": "24px"},
		"contrast": {"color.brand.primary": "#ffffff"},
		"unknown":  {"color.object": "#000000"},
	} {
		if err := ValidateCustomerTheme(CustomerTheme{Palette: "custom", TokenOverrides: overrides}); err == nil {
			t.Errorf("%s override was admitted", name)
		}
	}
	if err := ValidateCustomerTheme(CustomerTheme{Palette: "ocean", TokenOverrides: custom.TokenOverrides}); err == nil {
		t.Fatal("authored colors were admitted under a non-custom palette")
	}
}

func TestAuthoredDarkPaletteIsIndependentAndPreservesSafetyLayers(t *testing.T) {
	custom := DefaultCustomerTheme()
	custom.Palette = "custom"
	custom.TokenOverrides = map[string]string{
		"color.brand.primary": "#4d1f78", "color.brand.hover": "#371455", "color.brand.soft": "#f3eafb",
		"color.text.primary": "#20142b", "color.text.muted": "#5e5167", "color.canvas": "#fbf9fd",
		"color.surface": "#ffffff", "color.border": "#d9cfdf",
	}
	custom.DarkTokenOverrides = map[string]string{
		"color.brand.primary": "#ba9ce7", "color.brand.hover": "#c8b1ea", "color.brand.soft": "#2b223a",
		"color.text.primary": "#f5f1fb", "color.text.muted": "#bdb4cb", "color.canvas": "#101019",
		"color.surface": "#1d1b28", "color.border": "#514a65",
	}
	modes, err := ResolveCustomerThemeModes(custom)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := modes[ThemeModeLight].Value("color.brand.primary"); got != "#4d1f78" {
		t.Fatalf("light brand = %s", got)
	}
	if got, _ := modes[ThemeModeDark].Value("color.brand.primary"); got != "#ba9ce7" {
		t.Fatalf("dark brand = %s", got)
	}
	css, err := StylesheetForCustomerTheme(custom)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"--accent:#ba9ce7", "--canvas:#101019", "--hcm-color-brand-primary:#ba9ce7", "--hcm-color-surface:#1d1b28", `data-hcm-preview-color-mode="dark"`, finalThemeCoverageLayer()} {
		if !strings.Contains(css, fragment) {
			t.Errorf("dark stylesheet missing %q", fragment)
		}
	}
	if strings.Index(css, "--hcm-color-brand-primary:#4d1f78") > strings.LastIndex(css, "--hcm-color-brand-primary:#ba9ce7") {
		t.Fatal("dark color appeared before the light base")
	}
	if strings.LastIndex(css, "--hcm-color-canvas:#fbf9fd") < strings.LastIndex(css, "--hcm-color-canvas:#101019") {
		t.Fatal("print did not restore the authored light canvas after dark mode")
	}
	for _, safe := range []string{"--hcm-color-canvas:#000000", "--hcm-color-canvas:Canvas", "--hcm-color-focus:Highlight"} {
		if !strings.Contains(css, safe) {
			t.Errorf("customer dark colors erased safety rule %q", safe)
		}
	}
	for name, overrides := range map[string]map[string]string{
		"script":       {"color.canvas": "#fff;body{display:none}"},
		"protected":    {"color.focus": "#ffffff"},
		"low contrast": {"color.text.primary": "#101019"},
	} {
		invalid := custom
		invalid.DarkTokenOverrides = overrides
		if err := ValidateCustomerTheme(invalid); err == nil {
			t.Errorf("%s dark override was admitted", name)
		}
	}
}

func TestEveryRegisteredPageRendersTheAuthoredThemeSheet(t *testing.T) {
	custom := DefaultCustomerTheme()
	custom.Palette = "custom"
	custom.TokenOverrides = map[string]string{
		"color.brand.primary": "#4d1f78", "color.brand.hover": "#371455", "color.brand.soft": "#f3eafb",
		"color.text.primary": "#20142b", "color.text.muted": "#5e5167", "color.canvas": "#fbf9fd",
		"color.surface": "#ffffff", "color.border": "#d9cfdf",
	}
	for _, page := range PageDefinitions() {
		t.Run(string(page.ID), func(t *testing.T) {
			view := testView(page.ID)
			view.Appearance = custom
			doc, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{`data-hcm-palette="custom"`, "--hcm-color-brand-primary:#4d1f78", "--hcm-color-canvas:#fbf9fd"} {
				if !strings.Contains(doc, want) {
					t.Errorf("%s dropped %q", page.ID, want)
				}
			}
		})
	}
}
