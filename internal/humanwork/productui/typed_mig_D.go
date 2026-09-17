package productui

import (
	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// Typed builders for the D-batch string-CSS consts from styles.go.
// Selectors stay byte-identical to the originals; declarations use typed
// constructors where exact, gwccss.Raw elsewhere.

func colorModeControlStylesStylesheet() string {
	return buildTypedSheet(declareColorModeControlStylesStyles)
}

func declareColorModeControlStylesStyles() {
	declareGlobal(".color-mode-choices",
		gwccss.GridCols(gwccss.Repeat(3, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".swatch-system-1,.swatch-light-1",
		gwccss.Bg(gwccss.Hex("fff")),
	)
	declareGlobal(".swatch-system-2,.swatch-dark-1",
		gwccss.Bg(gwccss.Hex("101820")),
	)
	declareGlobal(".swatch-light-2",
		gwccss.Bg(gwccss.Hex("eaf3ef")),
	)
	declareGlobal(".swatch-dark-2",
		gwccss.Bg(gwccss.Hex("70b7a5")),
	)
	declareGlobal(".appearance-choice-color_mode-system .appearance-swatches",
		gwccss.Position.Relative,
	)
	declareGlobal(".appearance-choice-color_mode-system .appearance-swatches:after",
		gwccss.Position.Absolute,
		gwccss.Left(gwccss.Px(19)),
		gwccss.Top(gwccss.Zero),
		gwccss.W(gwccss.Px(23)),
		gwccss.H(gwccss.Px(23)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.Raw("background", "linear-gradient(135deg,#fff 0 50%,#101820 50%)"),
		gwccss.Raw("content", "\"\""),
	)
	declareGlobal(".wordmark-mark",
		gwccss.TextColor(gwccss.Var("on-brand")),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .sidebar",
		gwccss.TextColor(gwccss.Var("on-brand")),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .sidebar :is(.tenant,.nav-section-label,.nav-favorite,.nav-chevron)",
		gwccss.Raw("color", "color-mix(in srgb,var(--on-brand) 78%,transparent)"),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .sidebar :is(.sidebar-toggle,.nav-link,.nav-group-summary,.menu-filter input,.menu-filter-submit)",
		gwccss.TextColor(gwccss.Var("on-brand")),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .sidebar .menu-filter input::placeholder",
		gwccss.Raw("color", "color-mix(in srgb,var(--on-brand) 76%,transparent)"),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .sidebar :focus-visible",
		gwccss.Raw("outline-color", "var(--on-brand)"),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .appearance-preview-nav .nav-icon",
		gwccss.TextColor(gwccss.Var("on-brand")),
		gwccss.Raw("stroke", "var(--on-brand)"),
	)
	declareGlobal(".skip-link,.menu-filter input:focus",
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".notifications summary:after",
		gwccss.BorderColor(gwccss.Var("surface")),
	)
	declareGlobal(".button",
		gwccss.BorderColor(gwccss.Var("control-border")),
	)
	declareGlobal(".appearance-swatch",
		gwccss.BorderColor(gwccss.Var("line")),
	)
	declareGlobal(".color-mode-choices",
		mediaRule(gwccss.MaxW(680), gwccss.GridCols(gwccss.Fr(1))),
	)
}

func localeStylesStylesheet() string {
	return buildTypedSheet(declareLocaleStylesStyles)
}

func declareLocaleStylesStyles() {
	declareGlobal(".locale-menu",
		gwccss.Position.Relative,
	)
	declareGlobal(".locale-menu>summary",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.MinWidth(gwccss.Px(44)),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Zero), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(.75)),
		gwccss.Raw("font-weight", "750"),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".locale-menu>summary::-webkit-details-marker",
		gwccss.Display.None,
	)
	declareGlobal(".locale-popover",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(4)),
		gwccss.W(gwccss.Px(190)),
	)
	declareGlobal(".locale-option",
		gwccss.Display.Flex,
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.Items.Center,
		gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".locale-option:hover,.locale-option[aria-current=true]",
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal("[dir=rtl] :where(input,textarea,select)",
		gwccss.Raw("text-align", "start"),
	)
	declareGlobal("[dir=rtl] .popover",
		gwccss.Right(gwccss.RawLength("auto")),
		gwccss.Left(gwccss.Zero),
	)
	declareGlobal("[dir=rtl] .nav-link[aria-current=page],[dir=rtl] .work-row.selected,[dir=rtl] .people-row.selected",
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(-3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("accent"))),
	)
	declareGlobal("[dir=rtl] .nav-chevron,[dir=rtl] .work-row-chevron,[dir=rtl] .organization-unit-glyph,[dir=rtl] .sensitive-summary-chevron",
		gwccss.Raw("transform", "scaleX(-1)"),
	)
	declareGlobal("[dir=rtl] .sensitive-details[open]>.sensitive-summary .sensitive-summary-chevron",
		gwccss.Raw("transform", "scaleX(-1) rotate(90deg)"),
	)
	declareGlobal("[dir=rtl] .organization-unit-disclosure[open]>.org-node.manager .organization-unit-glyph",
		gwccss.Raw("transform", "scaleX(-1) rotate(90deg)"),
	)
	declareGlobal("[dir=rtl] .nav-group[open]>.nav-group-summary .nav-chevron",
		gwccss.Raw("transform", "scaleX(-1) rotate(90deg)"),
	)
	declareGlobal("[dir=rtl] .appearance-choice input",
		gwccss.Right(gwccss.RawLength("auto")),
		gwccss.Left(gwccss.Px(12)),
	)
	declareGlobal("[dir=rtl] .appearance-choice strong",
		gwccss.Raw("padding-right", "0"),
		gwccss.Raw("padding-left", "22px"),
	)
	declareGlobal("[dir=rtl] .callout",
		gwccss.Raw("border-left", "0"),
		gwccss.BorderRight(gwccss.Px(3), gwccss.Var("accent")),
		gwccss.Rounded(gwccss.RawLength("var(--radius) 0 0 var(--radius)")),
	)
	declareGlobal(".topbar,.app-shell.nav-collapsed .topbar",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
	declareGlobal(".locale-popover",
		mediaRule(gwccss.MaxW(760), gwccss.Right(gwccss.Zero), gwccss.Left(gwccss.RawLength("auto"))),
	)
	declareGlobal("[dir=rtl] .locale-popover",
		mediaRule(gwccss.MaxW(760), gwccss.Right(gwccss.RawLength("auto")), gwccss.Left(gwccss.Zero)),
	)
}

func localePreferenceStylesStylesheet() string {
	return buildTypedSheet(declareLocalePreferenceStylesStyles)
}

func declareLocalePreferenceStylesStyles() {
	declareGlobal(".settings-page-stack",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.VarLength("theme-section-gap")),
	)
	declareGlobal(".settings-overview-grid",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Px(280)), gwccss.Fr(.82)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(420)), gwccss.Fr(1.18))),
		gwccss.Gap(gwccss.VarLength("theme-section-gap")),
		gwccss.Items.Stretch,
	)
	declareGlobal(".settings-overview-grid>.settings-context",
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".locale-preferences",
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".locale-preferences .section-head p",
		gwccss.Raw("margin", "4px 0 0"),
	)
	declareGlobal(".locale-choice-list",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(9)),
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("padding", "0 22px 18px"),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".locale-choice",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(42)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.Gap(gwccss.Px(12)),
		gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(72)),
		gwccss.PaddingY(gwccss.Px(11)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".locale-choice:hover",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.Transform(gwccss.TranslateY(gwccss.Px(-1))),
	)
	declareGlobal(".locale-choice.current",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(1), gwccss.Var("accent"))),
	)
	declareGlobal(".locale-choice-code",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.W(gwccss.Px(42)),
		gwccss.H(gwccss.Px(42)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(.72)),
		gwccss.Raw("font-weight", "800"),
		gwccss.Tracking(gwccss.Ems(.04)),
	)
	declareGlobal(".locale-choice-copy,.locale-choice-copy strong,.locale-choice-copy small",
		gwccss.Display.Block,
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".locale-choice-copy small",
		gwccss.Raw("margin-top", "3px"),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".locale-current",
		gwccss.PaddingY(gwccss.Px(5)), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.Rounded(gwccss.Px(999)),
		gwccss.Bg(gwccss.Var("accent")),
		gwccss.TextColor(gwccss.Var("on-brand")),
		gwccss.FontSize(gwccss.Rem(.7)),
		gwccss.Raw("font-weight", "750"),
	)
	declareGlobal(".locale-preferences-status",
		gwccss.Margin(gwccss.Zero),
		gwccss.PaddingY(gwccss.Px(13)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.78)),
	)
	declareGlobal(".settings-overview-grid",
		mediaRule(gwccss.MaxW(960), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".locale-choice-list",
		mediaRule(gwccss.MaxW(520), gwccss.Raw("padding-inline", "16px")),
	)
	declareGlobal(".locale-choice",
		mediaRule(gwccss.MaxW(520), gwccss.GridCols(gwccss.TrackLen(gwccss.Px(38)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".locale-choice-code",
		mediaRule(gwccss.MaxW(520), gwccss.W(gwccss.Px(38)), gwccss.H(gwccss.Px(38))),
	)
	declareGlobal(".locale-current",
		mediaRule(gwccss.MaxW(520), gwccss.GridColumn(gwccss.GridLineAt(2)), gwccss.W(gwccss.RawLength("max-content"))),
	)
	declareGlobal(".locale-preferences-status",
		mediaRule(gwccss.MaxW(520), gwccss.Raw("padding-inline", "16px")),
	)
	declareGlobal(".locale-choice.current,.locale-current",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Border(gwccss.Px(1), gwccss.Color("CanvasText"))),
	)
	declareGlobal(".locale-current",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Bg(gwccss.Color("Highlight")), gwccss.TextColor(gwccss.Color("HighlightText"))),
	)
}

func localePreferenceAccessibilityStylesStylesheet() string {
	return buildTypedSheet(declareLocalePreferenceAccessibilityStylesStyles)
}

func declareLocalePreferenceAccessibilityStylesStyles() {
	declareGlobal(":root[data-hcm-links=\"underlined\"] .main .settings-page-stack .locale-preferences a.locale-choice",
		gwccss.Raw("text-decoration", "none"),
	)
}

func accessibilityStylesStylesheet() string {
	return buildTypedSheet(declareAccessibilityStylesStyles)
}

func declareAccessibilityStylesStyles() {
	declareGlobal("body",
		gwccss.FontSize(gwccss.Rem(1)),
	)
	declareGlobal(":root[data-hcm-text-size=\"large\"]",
		gwccss.FontSize(gwccss.Percent(112.5)),
	)
	declareGlobal(":root[data-hcm-text-size=\"larger\"]",
		gwccss.FontSize(gwccss.Percent(125)),
	)
	declareGlobal(":root[data-hcm-contrast=\"more\"]",
		gwccss.CustomColor("ink", gwccss.Hex("000")),
		gwccss.CustomColor("muted", gwccss.Hex("292929")),
		gwccss.CustomColor("canvas", gwccss.Hex("fff")),
		gwccss.CustomColor("surface", gwccss.Hex("fff")),
		gwccss.CustomColor("soft", gwccss.Hex("e7f1ed")),
		gwccss.CustomColor("line", gwccss.Hex("555")),
		gwccss.CustomColor("control-border", gwccss.Hex("222")),
		gwccss.CustomColor("accent", gwccss.Hex("004c3f")),
		gwccss.CustomColor("accent-hover", gwccss.Hex("00382f")),
	)
	declareGlobal(":root[data-hcm-motion-preference=\"reduce\"]",
		gwccss.CustomDuration("hcm-motion-fast", gwccss.RawDuration(".01ms")),
		gwccss.CustomDuration("hcm-motion-normal", gwccss.RawDuration(".01ms")),
		gwccss.CustomDuration("hcm-motion-slow", gwccss.RawDuration(".01ms")),
		gwccss.CustomLength("hcm-motion-distance", gwccss.Px(0)),
	)
	declareGlobal(":root[data-hcm-motion-preference=\"reduce\"] *, :root[data-hcm-motion-preference=\"reduce\"] *::before,:root[data-hcm-motion-preference=\"reduce\"] *::after",
		gwccss.Raw("animation", "none!important"),
		gwccss.TransitionDuration(gwccss.RawDuration(".01ms!important")),
		gwccss.Raw("transition-delay", "0ms!important"),
		gwccss.Raw("scroll-behavior", "auto!important"),
	)
	declareGlobal(":root[data-hcm-links=\"underlined\"] .main a:not(.button):not(.work-row):not(.people-row)",
		gwccss.Raw("text-decoration", "underline"),
		gwccss.TextDecorationThickness(gwccss.Ems(.08)),
		gwccss.TextUnderlineOffset(gwccss.Ems(.18)),
	)
	declareGlobal(":where(a,button,input,select,textarea,summary)",
		gwccss.Raw("touch-action", "manipulation"),
	)
	declareGlobal(":focus-visible",
		gwccss.Raw("outline", "3px solid var(--hcm-color-focus)"),
		gwccss.OutlineOffset(gwccss.Px(3)),
		gwccss.Shadow(gwccss.ShadowOf(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(2), gwccss.Var("surface"))),
	)
	declareGlobal("#page-title:focus",
		gwccss.Raw("outline", "none"),
	)
	declareGlobal(".route-announcer",
		gwccss.Raw("pointer-events", "none"),
	)
	declareGlobal(".metrics,.recent,.org-branches,.work-rows,.people-rows",
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".facts,.person-fact-grid",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".facts dt,.profile-fact dt",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.75)),
	)
	declareGlobal(".facts dd,.profile-fact dd",
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("overflow-wrap", "anywhere"),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".activity>time",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.8)),
	)
	declareGlobal(".nav-favorite",
		gwccss.MinWidth(gwccss.Px(44)),
		gwccss.MinHeight(gwccss.Px(44)),
	)
	declareGlobal(".menu-filter-submit",
		gwccss.W(gwccss.Px(44)),
		gwccss.H(gwccss.Px(44)),
	)
	declareGlobal(".work-row[aria-current=true],.people-row[aria-current=true]",
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(4), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("accent"))),
	)
	declareGlobal(".history-action",
		gwccss.Raw("justify-self", "end"),
	)
	declareGlobal(".history-action>.button",
		gwccss.W(gwccss.RawLength("max-content")),
	)
	declareGlobal(".history-table",
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".accessibility-preferences",
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".accessibility-preferences .section-head p",
		gwccss.Raw("margin", "4px 0 0"),
	)
	declareGlobal(".accessibility-form",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Zero),
		gwccss.Raw("padding", "0 22px 22px"),
	)
	declareGlobal(".accessibility-group",
		gwccss.Margin(gwccss.Zero),
		gwccss.PaddingY(gwccss.Px(18)), gwccss.PaddingX(gwccss.Zero),
		gwccss.Raw("border", "0"),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".accessibility-group legend",
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("font-weight", "750"),
	)
	declareGlobal(".accessibility-group>p",
		gwccss.Raw("margin", "4px 0 12px"),
	)
	declareGlobal(".accessibility-options",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(3, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
		gwccss.Gap(gwccss.Px(9)),
	)
	declareGlobal(".accessibility-choice",
		gwccss.Display.Flex,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Gap(gwccss.Px(10)),
		gwccss.MinHeight(gwccss.Px(72)),
		gwccss.Padding(gwccss.Px(12)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal(".accessibility-choice:has(input:checked)",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(1), gwccss.Var("accent"))),
	)
	declareGlobal(".accessibility-choice input",
		gwccss.Raw("flex", "none"),
		gwccss.Raw("margin-top", "4px"),
		gwccss.Raw("accent-color", "var(--accent)"),
	)
	declareGlobal(".accessibility-choice span,.accessibility-choice small",
		gwccss.Display.Block,
	)
	declareGlobal(".accessibility-choice small",
		gwccss.Raw("margin-top", "3px"),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".accessibility-actions",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Px(10)),
		gwccss.Raw("padding-top", "18px"),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".accessibility-status",
		gwccss.MinHeight(gwccss.Px(24)),
		gwccss.Raw("margin", "10px 0 0"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.8)),
	)
	declareGlobal(".accessibility-status[data-tone=\"success\"]",
		gwccss.TextColor(gwccss.Var("success")),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".accessibility-status[data-tone=\"warning\"]",
		gwccss.TextColor(gwccss.Var("warning")),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(":root",
		mediaRule(gwccss.RawMedia("(prefers-contrast:more)"), gwccss.CustomColor("ink", gwccss.Hex("000")), gwccss.CustomColor("muted", gwccss.Hex("292929")), gwccss.CustomColor("canvas", gwccss.Hex("fff")), gwccss.CustomColor("surface", gwccss.Hex("fff")), gwccss.CustomColor("soft", gwccss.Hex("e7f1ed")), gwccss.CustomColor("line", gwccss.Hex("555")), gwccss.CustomColor("control-border", gwccss.Hex("222")), gwccss.CustomColor("accent", gwccss.Hex("004c3f")), gwccss.CustomColor("accent-hover", gwccss.Hex("00382f"))),
	)
	declareGlobal(".muted",
		mediaRule(gwccss.RawMedia("(prefers-contrast:more)"), gwccss.TextColor(gwccss.Var("muted"))),
	)
	declareGlobal(":where(.button,.surface,input,select,textarea,.nav-link[aria-current=page])",
		mediaRule(gwccss.RawMedia("(prefers-contrast:more)"), gwccss.Raw("border-color", "currentColor")),
	)
	declareGlobal(".status",
		mediaRule(gwccss.RawMedia("(prefers-contrast:more)"), gwccss.Raw("border", "1px solid currentColor")),
	)
	declareGlobal(":focus-visible",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("outline", "3px solid Highlight!important"), gwccss.Raw("box-shadow", "none!important")),
	)
	declareGlobal(".button,.nav-favorite,.menu-filter-submit,.locale-menu>summary",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("forced-color-adjust", "auto")),
	)
	declareGlobal(".status,.count,[aria-current=page]",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Border(gwccss.Px(1), gwccss.Color("CanvasText"))),
	)
	declareGlobal(".status.success",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.TextColor(gwccss.Color("CanvasText"))),
	)
	declareGlobal(".popover,.surface,.mini-page",
		mediaRule(gwccss.RawMedia("(prefers-reduced-transparency:reduce)"), gwccss.Raw("box-shadow", "none!important")),
	)
	declareGlobal(".people-columns",
		mediaRule(gwccss.MaxW(760), gwccss.Display.None),
	)
	declareGlobal(".people-row",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("grid-template-columns", "1fr!important"), gwccss.Gap(gwccss.Px(7))),
	)
	declareGlobal(".people-row>span:nth-child(n)",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("display", "flex!important"), gwccss.Raw("grid-column", "1!important"), gwccss.MinWidth(gwccss.Zero)),
	)
	declareGlobal(".people-row>.people-cell",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("justify-content", "space-between"), gwccss.Gap(gwccss.Px(18))),
	)
	declareGlobal(".people-row>.people-cell:before",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("content", "attr(data-label)"), gwccss.Raw("flex", "none"), gwccss.TextColor(gwccss.Var("muted")), gwccss.FontSize(gwccss.Rem(.75)), gwccss.Raw("font-weight", "650")),
	)
	declareGlobal(".history-action,.history-action>.button",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("justify-self", "stretch"), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal(".history-row>.history-action",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(1)), gwccss.GridRow(gwccss.GridAuto)),
	)
	declareGlobal(".accessibility-options",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".main",
		mediaRule(gwccss.MaxW(400), gwccss.Raw("padding-inline", "12px")),
	)
	declareGlobal(".topbar",
		mediaRule(gwccss.MaxW(400), gwccss.Raw("padding-inline", "12px"), gwccss.Gap(gwccss.Px(8))),
	)
	declareGlobal(".page-head h1",
		mediaRule(gwccss.MaxW(400), gwccss.Raw("overflow-wrap", "anywhere")),
	)
	declareGlobal(".button",
		mediaRule(gwccss.MaxW(400), gwccss.Raw("white-space", "normal"), gwccss.Raw("text-align", "center")),
	)
	declareGlobal(".accessibility-form",
		mediaRule(gwccss.MaxW(400), gwccss.Raw("padding-inline", "16px")),
	)
}

func accessibilityLayoutStylesStylesheet() string {
	return buildTypedSheet(declareAccessibilityLayoutStylesStyles)
}

func declareAccessibilityLayoutStylesStyles() {
	declareGlobal(".topbar",
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(232)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(220)), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto"))),
	)
	declareGlobal(".app-shell.nav-collapsed .topbar",
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(72)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(220)), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto"))),
	)
	declareGlobal(".settings-accessibility-layout",
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Px(300)), gwccss.Fr(.75)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(520)), gwccss.Fr(1.25))),
	)
	declareGlobal(".topbar",
		mediaRule(gwccss.MaxW(1190), gwccss.GridCols(gwccss.TrackLen(gwccss.Px(210)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(180)), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
	declareGlobal(".settings-accessibility-layout",
		mediaRule(gwccss.MaxW(1190), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".topbar,.app-shell.nav-collapsed .topbar",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
}

func accessibilityReviewStylesStylesheet() string {
	return buildTypedSheet(declareAccessibilityReviewStylesStyles)
}

func declareAccessibilityReviewStylesStyles() {
	declareGlobal(".settings-accessibility-layout,.insights-grid:has(.accessibility-preferences)",
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
	)
	declareGlobal(".accessibility-preferences",
		gwccss.MaxWidth(gwccss.RawLength("none")),
	)
}

func declareThemeCoverageBaseStylesStyles() {
	declareGlobal(":root",
		gwccss.Custom("on-brand", "var(--hcm-color-on-brand)"),
		gwccss.Custom("control-border", "var(--hcm-color-control-border)"),
		gwccss.Custom("success", "var(--hcm-color-success)"),
		gwccss.Custom("success-bg", "var(--hcm-color-success-surface)"),
		gwccss.Custom("danger-bg", "var(--hcm-color-danger-surface)"),
		gwccss.Custom("info", "var(--hcm-color-info)"),
		gwccss.Custom("info-bg", "var(--hcm-color-info-surface)"),
		gwccss.Custom("surface-subtle", "color-mix(in srgb,var(--surface) 72%,var(--canvas))"),
		gwccss.Custom("surface-muted", "color-mix(in srgb,var(--surface) 55%,var(--soft))"),
		gwccss.Custom("divider", "color-mix(in srgb,var(--line) 72%,var(--surface))"),
		gwccss.Custom("theme-section-gap", "calc(var(--hcm-space-3) * var(--hcm-density))"),
		gwccss.Custom("theme-row-padding", "calc(var(--hcm-space-2) * var(--hcm-density))"),
		gwccss.Custom("theme-panel-padding", "calc(var(--hcm-space-3) * var(--hcm-density))"),
	)
	declareGlobal(":where(.app-shell,.jn-embedded)",
		gwccss.Raw("font-family", "var(--hcm-font-sans)"),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(":where(.app-shell,.jn-embedded) :is(button,input,select,textarea)",
		gwccss.Raw("font-family", "inherit"),
	)
	declareGlobal(":where(.app-shell) :is(.topbar,.sidebar,.popover,.scope,.button.secondary,.people-workspace,.org-node,.metric,.settings-shell,.choice,.support-request,.studio-shell,.mini-scope,.mini-card,.appearance-choice)",
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(":where(.app-shell) :is(.people-columns,.history-columns,.person-context,.settings-context,.settings-nav,.studio-nav,.studio-canvas,.definition,.privacy-badge,.count,.nav-count,.status)",
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(":where(.app-shell) :is(.global-search input,.menu-filter input,.support-form input,.support-form textarea,.support-form select,.settings-form select,.people-filter input,.workflow-search input,.history-filter input,.history-filter select,.appearance-brand-fields input)",
		gwccss.BorderColor(gwccss.Var("control-border")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(":where(.app-shell) :is(.work-row,.people-row,.history-row)",
		gwccss.BorderColor(gwccss.Var("divider")),
	)
	declareGlobal(":where(.app-shell) :is(.button.primary,.wordmark-mark)",
		gwccss.TextColor(gwccss.Var("on-brand")),
	)
	declareGlobal(".button.secondary",
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(":where(.app-shell) :is(.admin-hero,.person-hero)",
		gwccss.Raw("background", "linear-gradient(120deg,var(--soft),var(--surface) 72%)"),
	)
	declareGlobal(".mini-page",
		gwccss.Bg(gwccss.Var("canvas")),
		gwccss.Raw("box-shadow", "none"),
	)
	declareGlobal(".bar-track",
		gwccss.Bg(gwccss.Var("surface-muted")),
	)
	declareGlobal(".bar-fill",
		gwccss.Raw("background", "linear-gradient(90deg,var(--accent) 0 72%,color-mix(in srgb,var(--accent) 55%,var(--soft)) 72% 92%,var(--warning) 92%)"),
	)
	declareGlobal(".status.success",
		gwccss.Bg(gwccss.Var("success-bg")),
		gwccss.TextColor(gwccss.Var("success")),
	)
	declareGlobal(".callout.warning,.privacy-notice",
		gwccss.BorderColor(gwccss.Var("warning")),
		gwccss.Bg(gwccss.Var("warning-bg")),
		gwccss.TextColor(gwccss.Var("warning")),
	)
	declareGlobal(".avatar",
		gwccss.BorderColor(gwccss.Var("line")),
	)
	declareGlobal(".avatar[src],.avatar.profile[src]",
		gwccss.Shadow(gwccss.Shadows(gwccss.ShadowOf(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(2), gwccss.Var("surface")), gwccss.ShadowOf(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(3), gwccss.Var("line")))),
	)
	declareGlobal(":where(.home-grid,.side-stack,.workbench,.insights-grid,.admin-grid,.studio-page,.appearance-page,.person-page,.person-detail-stack,.page-stack,.studio-governance)",
		gwccss.Gap(gwccss.VarLength("theme-section-gap")),
	)
	declareGlobal(":where(.work-row,.people-row,.history-row,.activity,.workflow-card,.sensitive-summary)",
		gwccss.Raw("padding-block", "var(--theme-row-padding)"),
	)
	declareGlobal(":where(.admin-card,.metric,.work-preview,.support-request,.appearance-intro,.appearance-preview,.appearance-group,.settings-nav,.settings-form,.settings-context,.studio-nav,.studio-structure,.studio-inspector)",
		gwccss.Padding(gwccss.VarLength("theme-panel-padding")),
	)
	declareGlobal(".jn-embedded",
		gwccss.Custom("jn-canvas", "var(--canvas)"),
		gwccss.Custom("jn-surface", "var(--surface)"),
		gwccss.Custom("jn-surface-sunk", "var(--surface-subtle)"),
		gwccss.Custom("jn-surface-muted", "var(--surface-muted)"),
		gwccss.Custom("jn-hairline", "var(--line)"),
		gwccss.Custom("jn-control-border", "var(--control-border)"),
		gwccss.Custom("jn-ink", "var(--ink)"),
		gwccss.Custom("jn-ink-muted", "var(--muted)"),
		gwccss.Custom("jn-masthead", "var(--accent)"),
		gwccss.Custom("jn-masthead-ink", "var(--on-brand)"),
		gwccss.Custom("jn-masthead-muted", "color-mix(in srgb,var(--on-brand) 78%,transparent)"),
		gwccss.Custom("jn-masthead-chip", "color-mix(in srgb,var(--accent-hover) 72%,var(--accent))"),
		gwccss.Custom("jn-masthead-chip-ink", "var(--on-brand)"),
		gwccss.Custom("jn-accent", "var(--accent)"),
		gwccss.Custom("jn-accent-strong", "var(--accent-hover)"),
		gwccss.Custom("jn-accent-ink", "var(--on-brand)"),
		gwccss.Custom("jn-accent-soft", "var(--soft)"),
		gwccss.Custom("jn-info", "var(--info)"),
		gwccss.Custom("jn-info-soft", "var(--info-bg)"),
		gwccss.Custom("jn-success", "var(--success)"),
		gwccss.Custom("jn-success-soft", "var(--success-bg)"),
		gwccss.Custom("jn-warning", "var(--warning)"),
		gwccss.Custom("jn-warning-soft", "var(--warning-bg)"),
		gwccss.Custom("jn-danger", "var(--danger)"),
		gwccss.Custom("jn-danger-ink", "var(--on-brand)"),
		gwccss.Custom("jn-danger-soft", "var(--danger-bg)"),
		gwccss.Custom("jn-neutral", "var(--muted)"),
		gwccss.Custom("jn-neutral-soft", "var(--surface-muted)"),
		gwccss.Custom("jn-font", "var(--hcm-font-sans)"),
		gwccss.Custom("jn-mono", "var(--hcm-font-mono)"),
		gwccss.Custom("jn-s1", "calc(var(--hcm-space-1) * var(--hcm-density))"),
		gwccss.Custom("jn-s2", "calc(var(--hcm-space-2) * var(--hcm-density))"),
		gwccss.Custom("jn-s3", "calc(var(--hcm-space-3) * var(--hcm-density))"),
		gwccss.Custom("jn-s4", "calc(var(--hcm-space-4) * var(--hcm-density))"),
		gwccss.Custom("jn-r1", "var(--hcm-radius-control)"),
		gwccss.Custom("jn-r2", "var(--hcm-radius-control)"),
		gwccss.Custom("jn-r3", "var(--hcm-radius-surface)"),
		gwccss.Custom("jn-r4", "var(--hcm-radius-surface)"),
		gwccss.Custom("jn-shadow", "var(--hcm-shadow-resting)"),
		gwccss.Custom("jn-shadow-raised", "var(--hcm-shadow-raised)"),
		gwccss.Custom("jn-shadow-lift", "var(--hcm-shadow-raised)"),
		gwccss.Custom("jn-ring", "0 0 0 3px color-mix(in srgb,var(--accent) 22%,transparent)"),
	)
	declareGlobal(":root[data-hcm-motion] .jn-embedded *",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.TransitionDuration(gwccss.RawDuration("var(--hcm-motion-fast)!important"))),
	)
	declareGlobal(":root[data-hcm-motion] .jn-embedded [class]",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Raw("animation-duration", "var(--hcm-motion-slow)!important")),
	)
	declareGlobal(":where(.app-shell,.jn-embedded)",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.CustomColor("surface-subtle", gwccss.Color("Canvas")), gwccss.CustomColor("surface-muted", gwccss.Color("Canvas")), gwccss.CustomColor("divider", gwccss.Color("CanvasText")), gwccss.CustomColor("control-border", gwccss.Color("CanvasText"))),
	)
	declareGlobal(".bar-fill",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Bg(gwccss.Color("Highlight"))),
	)
}

// darkModeDeclarations and lightPrintDeclarations are declaration fragments
// spliced into darkModeStyles, not full rulesets. Each has a rule-slice
// helper shared by the fragment-string func and declareDarkModeStylesStyles.

func darkModeDeclarationRules() []gwccss.Rule {
	return gwccss.Rules(
		gwccss.Raw("color-scheme", "dark"),
		gwccss.Custom("accent", "color-mix(in srgb,var(--hcm-color-brand-primary) 40%,#fff)"),
		gwccss.Custom("accent-hover", "color-mix(in srgb,var(--hcm-color-brand-hover) 32%,#fff)"),
		gwccss.Custom("soft", "color-mix(in srgb,var(--hcm-color-brand-primary) 18%,#16202a)"),
		gwccss.Custom("ink", "#f3f7fb"),
		gwccss.Custom("muted", "#aebdcb"),
		gwccss.Custom("canvas", "#0b1118"),
		gwccss.Custom("surface", "#131c26"),
		gwccss.Custom("line", "#354454"),
		gwccss.Custom("hcm-color-on-brand", "#071118"),
		gwccss.Custom("hcm-color-control-border", "#718397"),
		gwccss.Custom("hcm-color-success", "#69dda2"),
		gwccss.Custom("hcm-color-success-surface", "#123424"),
		gwccss.Custom("hcm-color-warning", "#f3c56f"),
		gwccss.Custom("hcm-color-warning-surface", "#382a15"),
		gwccss.Custom("hcm-color-danger", "#ff9d95"),
		gwccss.Custom("hcm-color-danger-surface", "#3b1d20"),
		gwccss.Custom("hcm-color-info", "#8abfff"),
		gwccss.Custom("hcm-color-info-surface", "#152c48"),
		gwccss.Custom("hcm-color-focus", "#d8e9ff"),
		gwccss.Custom("warning", "var(--hcm-color-warning)"),
		gwccss.Custom("warning-bg", "var(--hcm-color-warning-surface)"),
		gwccss.Custom("danger", "var(--hcm-color-danger)"),
		gwccss.Custom("info", "var(--hcm-color-info)"),
		gwccss.Custom("hcm-shadow-resting", "0 1px 2px rgba(0,0,0,.32)"),
		gwccss.Custom("hcm-shadow-raised", "0 16px 42px rgba(0,0,0,.46)"),
	)
}

func lightPrintDeclarationRules() []gwccss.Rule {
	return gwccss.Rules(
		gwccss.Raw("color-scheme", "light"),
		gwccss.Custom("accent", "var(--hcm-color-brand-primary)"),
		gwccss.Custom("accent-hover", "var(--hcm-color-brand-hover)"),
		gwccss.Custom("soft", "var(--hcm-color-brand-soft)"),
		gwccss.Custom("ink", "var(--hcm-color-text)"),
		gwccss.Custom("muted", "var(--hcm-color-text-muted)"),
		gwccss.Custom("canvas", "var(--hcm-color-canvas)"),
		gwccss.Custom("surface", "var(--hcm-color-surface)"),
		gwccss.Custom("line", "var(--hcm-color-border)"),
		gwccss.Custom("hcm-color-on-brand", "#fff"),
		gwccss.Custom("hcm-color-control-border", "#7b8997"),
		gwccss.Custom("hcm-color-success", "#0f6136"),
		gwccss.Custom("hcm-color-success-surface", "#dff3e6"),
		gwccss.Custom("hcm-color-warning", "#925400"),
		gwccss.Custom("hcm-color-warning-surface", "#fff6df"),
		gwccss.Custom("hcm-color-danger", "#b42318"),
		gwccss.Custom("hcm-color-danger-surface", "#fdecea"),
		gwccss.Custom("hcm-color-info", "#1555a3"),
		gwccss.Custom("hcm-color-info-surface", "#eaf1fb"),
		gwccss.Custom("hcm-color-focus", "#102238"),
		gwccss.Custom("warning", "var(--hcm-color-warning)"),
		gwccss.Custom("warning-bg", "var(--hcm-color-warning-surface)"),
		gwccss.Custom("danger", "var(--hcm-color-danger)"),
		gwccss.Custom("info", "var(--hcm-color-info)"),
		gwccss.Custom("hcm-shadow-resting", "0 1px 2px rgba(16,34,56,.06)"),
		gwccss.Custom("hcm-shadow-raised", "0 10px 30px rgba(16,34,56,.12)"),
	)
}

// The base high-contrast :root rule is deliberately earlier than dark mode.
// Reassert a dark high-contrast palette after the dark declarations so an
// explicit or system-dark choice cannot silently defeat that user preference.
func darkHighContrastDeclarationRules() []gwccss.Rule {
	return gwccss.Rules(
		gwccss.Custom("ink", "#ffffff"),
		gwccss.Custom("muted", "#e3e9ef"),
		gwccss.Custom("canvas", "#000000"),
		gwccss.Custom("surface", "#000000"),
		gwccss.Custom("soft", "#102a21"),
		gwccss.Custom("line", "#b6c4d0"),
		gwccss.Custom("control-border", "#ffffff"),
		gwccss.Custom("accent", "#9af5d0"),
		gwccss.Custom("accent-hover", "#c4ffe8"),
		gwccss.Custom("hcm-color-on-brand", "#000000"),
		gwccss.Custom("hcm-color-focus", "#ffffff"),
		gwccss.Custom("hcm-color-brand-primary", "#9af5d0"),
		gwccss.Custom("hcm-color-brand-hover", "#c4ffe8"),
		gwccss.Custom("hcm-color-brand-soft", "#102a21"),
		gwccss.Custom("hcm-color-text", "#ffffff"),
		gwccss.Custom("hcm-color-text-muted", "#e3e9ef"),
		gwccss.Custom("hcm-color-canvas", "#000000"),
		gwccss.Custom("hcm-color-surface", "#000000"),
		gwccss.Custom("hcm-color-border", "#b6c4d0"),
	)
}

func darkModeForcedColorsRules() []gwccss.Rule {
	return gwccss.Rules(
		gwccss.Raw("color-scheme", "light dark"),
		gwccss.Custom("accent", "Highlight"),
		gwccss.Custom("accent-hover", "Highlight"),
		gwccss.Custom("soft", "Canvas"),
		gwccss.Custom("ink", "CanvasText"),
		gwccss.Custom("muted", "CanvasText"),
		gwccss.Custom("canvas", "Canvas"),
		gwccss.Custom("surface", "Canvas"),
		gwccss.Custom("line", "CanvasText"),
		gwccss.Custom("hcm-color-on-brand", "HighlightText"),
		gwccss.Custom("hcm-color-control-border", "CanvasText"),
		gwccss.Custom("hcm-color-success", "CanvasText"),
		gwccss.Custom("hcm-color-success-surface", "Canvas"),
		gwccss.Custom("hcm-color-warning", "CanvasText"),
		gwccss.Custom("hcm-color-warning-surface", "Canvas"),
		gwccss.Custom("hcm-color-danger", "CanvasText"),
		gwccss.Custom("hcm-color-danger-surface", "Canvas"),
		gwccss.Custom("hcm-color-info", "CanvasText"),
		gwccss.Custom("hcm-color-info-surface", "Canvas"),
		gwccss.Custom("hcm-color-focus", "Highlight"),
		gwccss.Custom("hcm-color-brand-primary", "Highlight"),
		gwccss.Custom("hcm-color-brand-hover", "Highlight"),
		gwccss.Custom("hcm-color-brand-soft", "Canvas"),
		gwccss.Custom("hcm-color-text", "CanvasText"),
		gwccss.Custom("hcm-color-text-muted", "CanvasText"),
		gwccss.Custom("hcm-color-canvas", "Canvas"),
		gwccss.Custom("hcm-color-surface", "Canvas"),
		gwccss.Custom("hcm-color-border", "CanvasText"),
	)
}

func darkModeStylesStylesheet() string {
	return buildTypedSheet(declareDarkModeStylesStyles)
}

func declareDarkModeStylesStyles() {
	declareDarkModeStylesStylesWithCustomerModes(Theme{}, Theme{})
}

func darkModeStylesStylesheetForCustomer(light, dark Theme) string {
	return buildTypedSheet(func() { declareDarkModeStylesStylesWithCustomerModes(light, dark) })
}

func customerLightPrintDeclarationRules(light Theme) []gwccss.Rule {
	rules := lightPrintDeclarationRules()
	for _, token := range registeredThemeTokens {
		if token.Kind == ThemeColor {
			value, _ := light.Value(token.Name)
			rules = append(rules, gwccss.Custom(token.CSSVariable, value))
		}
	}
	return rules
}

func customerDarkModeDeclarationRules(dark Theme) []gwccss.Rule {
	value := func(name string) string { resolved, _ := dark.Value(name); return resolved }
	rules := gwccss.Rules(
		gwccss.Custom("accent", value("color.brand.primary")),
		gwccss.Custom("accent-hover", value("color.brand.hover")),
		gwccss.Custom("soft", value("color.brand.soft")),
		gwccss.Custom("ink", value("color.text.primary")),
		gwccss.Custom("muted", value("color.text.muted")),
		gwccss.Custom("canvas", value("color.canvas")),
		gwccss.Custom("surface", value("color.surface")),
		gwccss.Custom("line", value("color.border")),
	)
	for _, token := range registeredThemeTokens {
		if token.Kind == ThemeColor {
			rules = append(rules, gwccss.Custom(token.CSSVariable, value(token.Name)))
		}
	}
	return rules
}

func declareDarkModeStylesStylesWithCustomerModes(light, dark Theme) {
	declareAppearancePreviewThemeStyles()
	declareGlobal(`:root[data-hcm-color-mode="light"]`,
		gwccss.Raw("color-scheme", "light"),
	)
	declareGlobal(`:root[data-hcm-color-mode="system"]`,
		gwccss.Raw("color-scheme", "light dark"),
	)
	declareGlobal(`:root[data-hcm-color-mode="dark"]`,
		darkModeDeclarationRules(),
	)
	declareGlobal(`:root[data-hcm-color-mode="system"]`,
		mediaRule(gwccss.RawMedia("(prefers-color-scheme:dark)"), darkModeDeclarationRules()),
	)
	if len(dark.values) != 0 {
		declareGlobal(`:root[data-hcm-color-mode="dark"]`, customerDarkModeDeclarationRules(dark))
		declareGlobal(`:root[data-hcm-color-mode="system"]`,
			mediaRule(gwccss.RawMedia("(prefers-color-scheme:dark)"), customerDarkModeDeclarationRules(dark)),
		)
	}
	// The in-app contrast choice is independent of the operating-system media
	// preference. Keep its dark palette after the ordinary dark declarations.
	declareGlobal(`:root[data-hcm-color-mode="dark"][data-hcm-contrast="more"]`,
		darkHighContrastDeclarationRules(),
	)
	declareGlobal(`:root[data-hcm-color-mode="system"][data-hcm-contrast="more"]`,
		mediaRule(gwccss.RawMedia("(prefers-color-scheme:dark)"), darkHighContrastDeclarationRules()),
	)
	declareGlobal(`:root[data-hcm-color-mode="dark"]`,
		mediaRule(gwccss.RawMedia("(prefers-contrast:more)"), darkHighContrastDeclarationRules()),
	)
	declareGlobal(`:root[data-hcm-color-mode="system"]`,
		mediaRule(gwccss.RawMedia("(prefers-color-scheme:dark) and (prefers-contrast:more)"), darkHighContrastDeclarationRules()),
	)
	printRules := lightPrintDeclarationRules()
	if len(light.values) != 0 {
		printRules = customerLightPrintDeclarationRules(light)
	}
	declareGlobal(`:root:is([data-hcm-color-mode="dark"],[data-hcm-color-mode="system"])`,
		mediaRule(gwccss.RawMedia("(print)"), printRules),
	)
	declareGlobal(`:root:is([data-hcm-color-mode="dark"],[data-hcm-color-mode="system"])`,
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), darkModeForcedColorsRules()),
	)
	// The saved More contrast selector carries an extra attribute specificity.
	// Keep the browser's print and forced-color palettes at the same specificity
	// and later in source order so those accessibility modes always win.
	declareGlobal(`:root:is([data-hcm-color-mode="dark"],[data-hcm-color-mode="system"])[data-hcm-contrast="more"]`,
		mediaRule(gwccss.RawMedia("(print)"), printRules),
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), darkModeForcedColorsRules()),
	)
	// Tail of darkModeStyles is themeCoverageStyles
	// (= themeCoverageBaseStyles + journeyFocusBridgeStyles + focusStyles).
	declareThemeCoverageBaseStylesStyles()
	declareJourneyFocusBridge()
	declareFocusStyles()
}
