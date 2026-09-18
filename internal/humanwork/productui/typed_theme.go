package productui

import (
	"fmt"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// Typed GWC rendering of the customer theme stylesheet (W1). Preset loop
// order and rule order match the original string emitter exactly; the GWC
// serializer emits declarations within one block sorted by property name
// (canonical form), so palette blocks list --hcm-color-brand-hover before
// --hcm-color-brand-primary and glyph rules carry a trailing semicolon.
// Proven semantically identical via /tmp/cssdiff.py (exit 0).
//
// Theme.CSS is intentionally NOT migrated: its single :root block emits
// custom properties in registry order, while the typed path sorts them, so
// no typed rendering reproduces the golden digest pinned in
// theme_test.go TestTodo_WEB_013_Golden (8ac7d3...; first byte differs at
// offset 20: registry `--hcm-color-brand-primary` vs sorted
// `--hcm-color-border`). The pin must pass unchanged.

func declareCustomerThemeStyles() {
	for _, group := range []struct {
		attribute string
		presets   []appearancePreset
	}{
		{"data-hcm-palette", palettePresets},
		{"data-hcm-shape", shapePresets},
		{"data-hcm-density", densityPresets},
		{"data-hcm-motion", motionPresets},
		{"data-hcm-typeface", typefacePresets},
	} {
		for _, preset := range group.presets {
			if len(preset.Overrides) == 0 {
				continue
			}
			theme, err := ResolveTheme(preset.Overrides)
			if err != nil {
				panic(fmt.Sprintf("productui: invalid built-in %s preset %q: %v", group.attribute, preset.Option.ID, err))
			}
			rules := make([]any, 0, len(preset.Overrides))
			for _, token := range registeredThemeTokens {
				value, changed := preset.Overrides[token.Name]
				if !changed {
					continue
				}
				resolved, _ := theme.Value(token.Name)
				if resolved != value {
					panic("productui: preset resolution drift")
				}
				rules = append(rules, gwccss.Custom(token.CSSVariable, resolved))
			}
			declareGlobal(`:root[`+group.attribute+`="`+preset.Option.ID+`"]`, rules...)
		}
	}
	declareGlobal(`:root[data-hcm-glyphs="rounded-line"] .nav-icon`,
		gwccss.Raw("stroke-width", "1.9"),
		gwccss.Raw("stroke-linecap", "round"),
		gwccss.Raw("stroke-linejoin", "round"),
	)
	declareGlobal(`:root .appearance-glyph-sample[data-hcm-glyph-sample="rounded-line"] .nav-icon`,
		gwccss.Raw("stroke-width", "1.9"), gwccss.Raw("stroke-linecap", "round"), gwccss.Raw("stroke-linejoin", "round"),
	)
	declareGlobal(`:root[data-hcm-glyphs="precision-line"] .nav-icon`,
		gwccss.Raw("stroke-width", "1.55"),
		gwccss.Raw("stroke-linecap", "square"),
		gwccss.Raw("stroke-linejoin", "miter"),
	)
	declareGlobal(`:root .appearance-glyph-sample[data-hcm-glyph-sample="precision-line"] .nav-icon`,
		gwccss.Raw("stroke-width", "1.55"), gwccss.Raw("stroke-linecap", "square"), gwccss.Raw("stroke-linejoin", "miter"),
	)
	declareGlobal(`:root[data-hcm-glyphs="bold-line"] .nav-icon`,
		gwccss.Raw("stroke-width", "2.35"),
		gwccss.Raw("stroke-linecap", "round"),
		gwccss.Raw("stroke-linejoin", "round"),
	)
	declareGlobal(`:root .appearance-glyph-sample[data-hcm-glyph-sample="bold-line"] .nav-icon`,
		gwccss.Raw("stroke-width", "2.35"), gwccss.Raw("stroke-linecap", "round"), gwccss.Raw("stroke-linejoin", "round"),
	)
	declareGlobal(`:root[data-hcm-glyphs="fine-line"] .nav-icon,:root .appearance-glyph-sample[data-hcm-glyph-sample="fine-line"] .nav-icon`,
		gwccss.Raw("stroke-width", "1.2"),
		gwccss.Raw("stroke-linecap", "round"),
		gwccss.Raw("stroke-linejoin", "round"),
	)
	declareGlobal(`:root[data-hcm-glyphs="square-bold"] .nav-icon,:root .appearance-glyph-sample[data-hcm-glyph-sample="square-bold"] .nav-icon`,
		gwccss.Raw("stroke-width", "2.5"),
		gwccss.Raw("stroke-linecap", "square"),
		gwccss.Raw("stroke-linejoin", "miter"),
	)
	declareGlobal(`:root[data-hcm-glyphs="soft-badge"] .nav-icon,:root .appearance-glyph-sample[data-hcm-glyph-sample="soft-badge"] .nav-icon`,
		gwccss.Raw("stroke-width", "2"),
		gwccss.Raw("stroke-linecap", "round"),
		gwccss.Raw("stroke-linejoin", "round"),
		gwccss.Raw("background", "var(--soft)"),
		gwccss.Raw("border-radius", "var(--hcm-radius-control)"),
		gwccss.Raw("padding", "3px"),
	)
	declareGlobal(`:root .appearance-glyph-sample:not([data-hcm-glyph-sample="soft-badge"]) .nav-icon`,
		gwccss.Raw("background", "transparent"),
		gwccss.Raw("padding", "0"),
		gwccss.Raw("border-radius", "0"),
	)
	declareGlobal(".appearance-color-grid",
		gwccss.Display.Grid,
		gwccss.Raw("grid-template-columns", "repeat(auto-fit,minmax(min(100%,190px),1fr))"),
		gwccss.Gap(gwccss.Px(10)),
	)
	declareGlobal(".appearance-color-field",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(12)),
		gwccss.MinHeight(gwccss.Px(48)),
		gwccss.PaddingY(gwccss.Px(5)),
		gwccss.PaddingX(gwccss.Px(9)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".appearance-color-field:focus-within",
		gwccss.Raw("outline", "2px solid var(--hcm-color-focus)"),
		gwccss.Raw("outline-offset", "2px"),
	)
	declareGlobal(".appearance-color-field input[type=color]",
		gwccss.W(gwccss.Px(52)),
		gwccss.H(gwccss.Px(36)),
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("border", "0"),
		gwccss.Raw("background", "transparent"),
		gwccss.Raw("cursor", "pointer"),
	)
}

// The editor's light and dark specimens resolve independently of the active
// workspace mode. A dark customer theme must not turn its "Light preview"
// dark (or vice versa), and palette changes must affect both specimens.
func declareAppearancePreviewThemeStyles() {
	for _, preset := range palettePresets {
		if preset.Option.ID == "custom" {
			continue // The admitted organization colors are compiled per request.
		}
		modes, err := ResolveThemeModes(preset.Overrides)
		if err != nil {
			panic(fmt.Sprintf("productui: invalid preview palette %q: %v", preset.Option.ID, err))
		}
		declareAppearancePreviewColors(preset.Option.ID, modes)
	}
	declareGlobal(".appearance-preview-content h4",
		gwccss.Raw("margin", "0"),
		gwccss.Raw("font-family", "var(--hcm-font-sans)"),
		gwccss.Raw("font-size", "0.875rem"),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".appearance-preview-nav .nav-icon",
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("stroke", "var(--ink)"),
	)
	declareGlobal(".appearance-preview-card strong",
		gwccss.Raw("font-size", "0.75rem"),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".appearance-preview-card small",
		gwccss.Raw("font-size", "0.75rem"),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".appearance-preview-pill",
		gwccss.Raw("font-style", "normal"),
		gwccss.Raw("font-size", "0.75rem"),
		gwccss.Raw("font-weight", "700"),
		gwccss.Raw("justify-self", "start"),
		gwccss.Raw("padding", "4px 8px"),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("accent")),
		gwccss.TextColor(gwccss.Var("on-brand")),
	)
	declareGlobal(".appearance-preview-compact .appearance-preview-content",
		gwccss.Gap(gwccss.Px(5)),
		gwccss.Padding(gwccss.Px(10)),
	)
	declareGlobal(".appearance-preview-compact .appearance-preview-card",
		gwccss.Gap(gwccss.Px(4)),
		gwccss.Padding(gwccss.Px(8)),
	)
}

func customAppearancePreviewStylesheet(theme Theme) string {
	return customAppearancePreviewStylesheetForModes(map[ThemeMode]Theme{
		ThemeModeLight: theme,
		ThemeModeDark:  {values: darkThemeValues(theme.values)},
	})
}

func customAppearancePreviewStylesheetForModes(modes map[ThemeMode]Theme) string {
	return buildTypedSheet(func() {
		declareAppearancePreviewColors("custom", modes)
	})
}

func declareAppearancePreviewColors(palette string, modes map[ThemeMode]Theme) {
	for _, mode := range []ThemeMode{ThemeModeLight, ThemeModeDark} {
		resolved := modes[mode]
		value := func(name string) string {
			color, _ := resolved.Value(name)
			return color
		}
		colorRules := []any{
			gwccss.Raw("color-scheme", string(mode)),
			gwccss.TextColor(gwccss.Var("ink")),
			gwccss.Custom("accent", value("color.brand.primary")),
			gwccss.Custom("accent-hover", value("color.brand.hover")),
			gwccss.Custom("soft", value("color.brand.soft")),
			gwccss.Custom("ink", value("color.text.primary")),
			gwccss.Custom("muted", value("color.text.muted")),
			gwccss.Custom("canvas", value("color.canvas")),
			gwccss.Custom("surface", value("color.surface")),
			gwccss.Custom("line", value("color.border")),
			gwccss.Custom("on-brand", value("color.on.brand")),
			gwccss.Custom("control-border", value("color.control.border")),
			gwccss.Custom("success", value("color.status.success")),
			gwccss.Custom("success-bg", value("color.status.success.surface")),
			gwccss.Custom("warning", value("color.status.warning")),
			gwccss.Custom("warning-bg", value("color.status.warning.surface")),
			gwccss.Custom("danger", value("color.status.danger")),
			gwccss.Custom("danger-bg", value("color.status.danger.surface")),
			gwccss.Custom("info", value("color.status.info")),
			gwccss.Custom("info-bg", value("color.status.info.surface")),
			gwccss.Custom("surface-subtle", "color-mix(in srgb,var(--surface) 72%,var(--canvas))"),
			gwccss.Custom("surface-muted", "color-mix(in srgb,var(--surface) 55%,var(--soft))"),
			gwccss.Custom("divider", "color-mix(in srgb,var(--line) 72%,var(--surface))"),
		}
		windowSelector := fmt.Sprintf(`:root[data-hcm-palette="%s"] .appearance-preview-window[data-hcm-preview-color-mode="%s"]`, palette, mode)
		declareGlobal(windowSelector, mediaRule(gwccss.RawMedia("(forced-colors:none)"), colorRules...))
		// The large modal embeds real page components. Rebind their semantic
		// color tokens locally so mode comparison never changes the saved or
		// surrounding workspace appearance.
		sceneRules := append([]any(nil), colorRules...)
		for _, token := range registeredThemeTokens {
			if token.Kind != ThemeColor {
				continue
			}
			sceneRules = append(sceneRules, gwccss.Custom(token.CSSVariable, value(token.Name)))
		}
		sceneSelector := fmt.Sprintf(`:root[data-hcm-palette="%s"] .appearance-preview-scene[data-hcm-preview-color-mode="%s"]`, palette, mode)
		declareGlobal(sceneSelector, mediaRule(gwccss.RawMedia("(forced-colors:none)"), sceneRules...))
	}
}
