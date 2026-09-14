package productui

import (
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_005(t *testing.T) {
	for _, overrides := range []map[string]string{nil, {
		"color.brand.primary": "#7c3aed",
		"color.brand.hover":   "#5b21b6",
	}} {
		modes, err := ResolveThemeModes(overrides)
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []ThemeMode{ThemeModeLight, ThemeModeDark} {
			if _, ok := modes[mode].Value("color.focus"); !ok {
				t.Fatalf("%s theme omitted focus semantic", mode)
			}
			if len(modes[mode].CSS()) == 0 {
				t.Fatalf("%s theme emitted empty CSS", mode)
			}
		}
	}
}

func TestTodo_UIPOLISH_005_Property(t *testing.T) {
	accepted := 0
	for _, brand := range []string{"#000000", "#ffffff", "#abcdef", "#ff0000", "#00ff00", "#0000ff"} {
		modes, err := ResolveThemeModes(map[string]string{"color.brand.primary": brand, "color.brand.hover": brand})
		if err != nil {
			// Unsafe light-mode combinations are deliberately refused at the
			// admission boundary; they must never produce a partial theme.
			continue
		}
		accepted++
		for mode, theme := range modes {
			if failures := validateThemeContrast(theme.values); len(failures) != 0 {
				t.Fatalf("brand %s mode %s failed: %v", brand, mode, failures)
			}
		}
	}
	if accepted == 0 {
		t.Fatal("property vectors did not produce an admitted theme")
	}
}

func TestTodo_UIPOLISH_005_Regression(t *testing.T) {
	for _, brand := range []string{"purple", "#123"} {
		if _, err := ResolveThemeModes(map[string]string{"color.brand.primary": brand}); err == nil {
			t.Fatalf("malformed brand %q was admitted", brand)
		}
	}
}

func TestTodo_UIPOLISH_005_DarkCSSMatchesQualification(t *testing.T) {
	for _, brand := range []string{"#000000", "#075a3b", "#7c3aed"} {
		light, err := ResolveTheme(map[string]string{"color.brand.primary": brand})
		if err != nil {
			t.Fatalf("brand %s: %v", brand, err)
		}
		dark := darkThemeValues(light.values)
		for _, item := range []struct {
			token       string
			source      string
			whiteAmount float64
		}{
			{"color.brand.primary", "color.brand.primary", .60},
			{"color.brand.hover", "color.brand.hover", .68},
		} {
			want := blendThemeColor(light.values[item.source], "#ffffff", item.whiteAmount)
			if dark[item.token] != want {
				t.Errorf("brand %s %s: got %s, want rendered %s", brand, item.token, dark[item.token], want)
			}
		}
	}
}

func TestTodo_UIPOLISH_005_DarkHighContrastWinsCascade(t *testing.T) {
	css := darkModeStylesStylesheet()
	for _, want := range []string{
		`:root[data-hcm-color-mode="dark"][data-hcm-contrast="more"]{`,
		`@media (prefers-color-scheme:dark){:root[data-hcm-color-mode="system"][data-hcm-contrast="more"]{`,
		`@media (prefers-contrast:more){:root[data-hcm-color-mode="dark"]{`,
		`@media (prefers-color-scheme:dark) and (prefers-contrast:more){:root[data-hcm-color-mode="system"]{`,
		`--canvas:#000000`, `--ink:#ffffff`, `--control-border:#ffffff`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("dark high-contrast stylesheet missing %q", want)
		}
	}
	if strings.LastIndex(css, `--canvas:#000000`) < strings.LastIndex(css, `--canvas:#0b1118`) {
		t.Error("ordinary dark canvas follows the high-contrast override")
	}
	for _, pair := range [][2]string{{"#ffffff", "#000000"}, {"#e3e9ef", "#000000"}, {"#9af5d0", "#000000"}, {"#000000", "#9af5d0"}} {
		ratio, err := contrastRatio(pair[0], pair[1])
		if err != nil || ratio < 7 {
			t.Errorf("high-contrast pair %s/%s = %.2f:1, want >= 7:1 (%v)", pair[0], pair[1], ratio, err)
		}
	}
}

func TestTodo_UIPOLISH_005_DarkHighContrastYieldsToPrintAndForcedColors(t *testing.T) {
	css := darkModeStylesStylesheet()
	selector := `:root:is([data-hcm-color-mode="dark"],[data-hcm-color-mode="system"])[data-hcm-contrast="more"]`
	for _, want := range []string{
		`@media (print){` + selector + `{--accent:var(--hcm-color-brand-primary);`,
		`@media (forced-colors:active){` + selector + `{--accent:Highlight;--accent-hover:Highlight;`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("saved high contrast does not yield to media palette: %q", want)
		}
	}
	if strings.LastIndex(css, selector) < strings.LastIndex(css, `:root[data-hcm-color-mode="dark"][data-hcm-contrast="more"]`) {
		t.Fatal("print/forced-color override precedes saved high-contrast selector")
	}
}
