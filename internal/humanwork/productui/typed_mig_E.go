package productui

import (
	"fmt"
	"strings"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// Typed builders for the E-batch consts from styles.go (plus the
// themeCoverageStyles composition with focus.go bridge/focus parts).
// Selectors are byte-identical to the string consts; gwccss.Raw covers
// var() fallbacks, logical props, system colors, color-mix, multi-layer
// backgrounds, shadows, grid shorthands, and !important values.

func AppearanceRobustnessStylesheet() string {
	return buildTypedSheet(declareAppearanceRobustnessStyles)
}

func declareAppearanceRobustnessStyles() {
	declareGlobal(".appearance-status[data-tone=\"warning\"]",
		gwccss.TextColor(gwccss.Var("warning")),
		gwccss.Raw("font-weight", "700"),
	)
}

func AppearanceSwatchStylesheet() string {
	return buildTypedSheet(declareAppearanceSwatchStyles)
}

func declareAppearanceSwatchStyles() {
	// The catalog is the source of truth for both the editor and its CSP-pinned
	// swatches. New palettes cannot silently appear without a matching preview.
	for _, preset := range palettePresets {
		for index, color := range preset.Option.Swatches {
			declareGlobal(fmt.Sprintf(".swatch-%s-%d", preset.Option.ID, index+1),
				gwccss.Bg(gwccss.Hex(strings.TrimPrefix(color, "#"))),
			)
		}
	}
}

func CustomerIdentityStylesheet() string {
	return buildTypedSheet(declareCustomerIdentityStyles)
}

func declareCustomerIdentityStyles() {
	declareGlobal(".app-shell .wordmark",
		gwccss.Gap(gwccss.Px(10)),
		gwccss.Raw("padding-inline-start", "20px"),
	)
	declareGlobal(".app-shell .wordmark:before",
		gwccss.Display.None,
	)
	declareGlobal(".app-shell .wordmark-mark",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.Raw("flex", "none"),
		gwccss.W(gwccss.Px(34)),
		gwccss.H(gwccss.Px(34)),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Var("accent")),
		gwccss.TextColor(gwccss.Var("on-brand")),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Tracking(gwccss.Zero),
	)
	declareGlobal(".wordmark-label",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.MaxWidth(gwccss.Percent(100)),
		gwccss.Raw("overflow-wrap", "anywhere"),
		gwccss.Raw("text-overflow", "clip"),
		gwccss.Raw("white-space", "normal"),
		gwccss.Raw("line-height", "1.15"),
	)
	declareGlobal(".brand-cluster .wordmark-label",
		gwccss.Raw("display", "-webkit-box"),
		gwccss.Raw("-webkit-box-orient", "vertical"),
		gwccss.Raw("-webkit-line-clamp", "2"),
		gwccss.Raw("overflow", "hidden"),
	)
	// The persistent desktop icon rail must never lay out the fallback name
	// in its narrow brand cell. The sibling sr-only name remains accessible.
	declareGlobal(".app-shell.nav-collapsed .brand-cluster .brand-logo-slot .wordmark-label",
		mediaRule(gwccss.MinW(761), gwccss.Raw("display", "none!important")),
	)
	declareGlobal(".sidebar.collapsed .nav-empty,.sidebar.collapsed .nav-support-label",
		mediaRule(gwccss.MinW(761), gwccss.Raw("display", "none!important")),
	)
	declareGlobal(".appearance-brand-fields",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(120)), gwccss.Fr(.32))),
		gwccss.Gap(gwccss.Px(12)),
	)
	declareGlobal(".appearance-brand-fields label",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(7)),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".appearance-brand-fields input",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(9)), gwccss.PaddingX(gwccss.Px(11)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".appearance-brand-fields small",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("font-weight", "400"),
	)
	declareGlobal(".appearance-preview-mark",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.MinWidth(gwccss.Px(25)),
		gwccss.H(gwccss.Px(25)),
		gwccss.PaddingY(gwccss.Zero), gwccss.PaddingX(gwccss.Px(5)),
		// Derived from the bar's own ink rather than assuming it is white,
		// so a brand with a light masthead does not lose the outline.
		gwccss.Raw("border", "1px solid color-mix(in srgb,currentColor 45%,transparent)"),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Raw("background", "color-mix(in srgb,var(--on-brand) 16%,transparent)"),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(":root[data-hcm-navigation=\"tinted\"] .sidebar,:root[data-hcm-navigation=\"tinted\"] .appearance-preview-nav",
		gwccss.Bg(gwccss.Var("soft")),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .sidebar,:root[data-hcm-navigation=\"brand\"] .appearance-preview-nav",
		gwccss.BorderColor(gwccss.Var("accent-hover")),
		gwccss.Bg(gwccss.Var("accent")),
		gwccss.TextColor(gwccss.Var("on-brand")),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .sidebar .tenant,:root[data-hcm-navigation=\"brand\"] .sidebar .nav-section-label,:root[data-hcm-navigation=\"brand\"] .sidebar .nav-favorite,:root[data-hcm-navigation=\"brand\"] .sidebar .nav-chevron",
		gwccss.Raw("color", "color-mix(in srgb,var(--on-brand) 78%,transparent)"),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .sidebar .sidebar-toggle,:root[data-hcm-navigation=\"brand\"] .sidebar .nav-link,:root[data-hcm-navigation=\"brand\"] .sidebar .nav-group-summary",
		gwccss.Raw("border-color", "color-mix(in srgb,var(--on-brand) 34%,transparent)"),
		gwccss.TextColor(gwccss.Var("on-brand")),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .sidebar .nav-link:hover,:root[data-hcm-navigation=\"brand\"] .sidebar .nav-link[aria-current=\"page\"],:root[data-hcm-navigation=\"brand\"] .sidebar .nav-group-summary:hover,:root[data-hcm-navigation=\"brand\"] .sidebar .nav-group.current>.nav-group-summary",
		gwccss.Raw("background", "color-mix(in srgb,var(--on-brand) 16%,transparent)"),
		gwccss.TextColor(gwccss.Var("on-brand")),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .sidebar .nav-count",
		gwccss.Raw("background", "color-mix(in srgb,var(--on-brand) 18%,transparent)"),
		gwccss.TextColor(gwccss.Var("on-brand")),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .sidebar .menu-filter input",
		gwccss.Raw("border-color", "color-mix(in srgb,var(--on-brand) 42%,transparent)"),
		gwccss.Raw("background", "color-mix(in srgb,var(--on-brand) 12%,transparent)"),
		gwccss.TextColor(gwccss.Var("on-brand")),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .sidebar .menu-filter input::placeholder",
		gwccss.Raw("color", "color-mix(in srgb,var(--on-brand) 76%,transparent)"),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .sidebar .menu-filter-submit",
		gwccss.TextColor(gwccss.Var("on-brand")),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .sidebar :focus-visible",
		gwccss.Raw("outline-color", "var(--on-brand)"),
		gwccss.Shadow(gwccss.ShadowOf(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(3), gwccss.Var("accent-hover"))),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .appearance-preview-nav .nav-icon",
		gwccss.TextColor(gwccss.Var("on-brand")),
		gwccss.Raw("stroke", "#fff"),
	)
	declareGlobal(":root[data-hcm-navigation=\"brand\"] .appearance-preview-nav",
		gwccss.BorderColor(gwccss.Var("accent-hover")),
	)
	declareGlobal(":root[data-hcm-density=\"compact\"] .main",
		mediaRule(gwccss.MinW(761), gwccss.Raw("padding-block", "24px 18px")),
	)
	declareGlobal(":root[data-hcm-density=\"compact\"] .page-head",
		mediaRule(gwccss.MinW(761), gwccss.Raw("margin-bottom", "22px")),
	)
	declareGlobal(":root[data-hcm-density=\"compact\"] .nav-link,:root[data-hcm-density=\"compact\"] .nav-group-summary",
		mediaRule(gwccss.MinW(761), gwccss.MinHeight(gwccss.Px(41)), gwccss.Raw("padding-block", "7px")),
	)
	declareGlobal(":root[data-hcm-density=\"compact\"] .work-row",
		mediaRule(gwccss.MinW(761), gwccss.MinHeight(gwccss.Px(74)), gwccss.Raw("padding-block", "10px")),
	)
	declareGlobal(":root[data-hcm-density=\"compact\"] .home-grid,:root[data-hcm-density=\"compact\"] .side-stack,:root[data-hcm-density=\"compact\"] .workbench,:root[data-hcm-density=\"compact\"] .insights-grid",
		mediaRule(gwccss.MinW(761), gwccss.Gap(gwccss.Px(15))),
	)
	declareGlobal(":root[data-hcm-density=\"spacious\"] .main",
		mediaRule(gwccss.MinW(761), gwccss.Raw("padding-block", "38px 26px")),
	)
	declareGlobal(":root[data-hcm-density=\"spacious\"] .page-head",
		mediaRule(gwccss.MinW(761), gwccss.Raw("margin-bottom", "36px")),
	)
	declareGlobal(":root[data-hcm-density=\"spacious\"] .nav-link,:root[data-hcm-density=\"spacious\"] .nav-group-summary",
		mediaRule(gwccss.MinW(761), gwccss.MinHeight(gwccss.Px(51))),
	)
	declareGlobal(":root[data-hcm-density=\"spacious\"] .work-row",
		mediaRule(gwccss.MinW(761), gwccss.MinHeight(gwccss.Px(96)), gwccss.Raw("padding-block", "18px")),
	)
	declareGlobal(":root[data-hcm-density=\"spacious\"] .home-grid,:root[data-hcm-density=\"spacious\"] .side-stack,:root[data-hcm-density=\"spacious\"] .workbench,:root[data-hcm-density=\"spacious\"] .insights-grid",
		mediaRule(gwccss.MinW(761), gwccss.Gap(gwccss.Px(25))),
	)
	declareGlobal(".appearance-brand-fields",
		mediaRule(gwccss.MaxW(680), gwccss.GridCols(gwccss.Fr(1))),
	)
}

func AppearanceStylesheet() string {
	return buildTypedSheet(declareAppearanceStyles)
}

func declareAppearanceStyles() {
	declareGlobal(".appearance-page",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(18)),
	)
	declareGlobal(".main-scroll:has(.appearance-page)",
		gwccss.Raw("scroll-padding-block-end", "160px"),
		mediaRule(gwccss.MaxW(680), gwccss.Raw("scroll-padding-block-end", "220px")),
	)
	declareGlobal(".appearance-section-nav",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Px(8)),
		gwccss.Padding(gwccss.Px(12)),
	)
	declareGlobal(".appearance-section-link",
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingX(gwccss.Px(14)),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".appearance-section-link:hover,.appearance-section-link:focus-visible",
		gwccss.Bg(gwccss.Var("soft")),
	)
	declareGlobal(".appearance-group[id]",
		gwccss.Raw("scroll-margin-block-start", "18px"),
	)
	declareGlobal(".appearance-intro",
		gwccss.Display.Flex,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(24)),
		gwccss.PaddingY(gwccss.Px(22)), gwccss.PaddingX(gwccss.Px(24)),
	)
	declareGlobal(".appearance-intro-copy",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(7)),
		gwccss.MaxWidth(gwccss.Px(760)),
	)
	declareGlobal(".appearance-intro-actions",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(10)),
	)
	declareGlobal(".appearance-intro h2",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(1.25)),
	)
	declareGlobal(".appearance-badge",
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(7)),
		gwccss.Raw("white-space", "nowrap"),
		gwccss.PaddingY(gwccss.Px(7)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-status")),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(".appearance-form",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1.5)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(280)), gwccss.Fr(.7))),
		gwccss.Gap(gwccss.Px(18)),
		gwccss.Raw("align-items", "start"),
	)
	declareGlobal(".appearance-controls",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(14)),
	)
	declareGlobal(".appearance-group",
		gwccss.Margin(gwccss.Zero),
		gwccss.PaddingY(gwccss.Px(20)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.Raw("border", "0"),
	)
	// A legend renders across its fieldset's top edge, and these fieldsets
	// are cards: each group's title sat on the card's border with the
	// padding opening underneath it. Floated, the legend lays out inside the
	// padding like any other heading; the next element clears it.
	declareGlobal(".appearance-group>legend",
		gwccss.Raw("float", "inline-start"),
		gwccss.W(gwccss.Percent(100)),
		gwccss.Raw("padding", "0 0 10px"),
		gwccss.FontSize(gwccss.Rem(1)),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(".appearance-group>legend+*",
		gwccss.Raw("clear", "both"),
	)
	// A group's description is set like every card's description (14px), a
	// step under the group title rather than level with the page's body copy.
	declareGlobal(".appearance-group-help",
		gwccss.Raw("margin", "-6px 0 14px"),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("line-height", "1.5"),
	)
	declareGlobal(".appearance-choices",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(3, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
		gwccss.Gap(gwccss.Px(10)),
	)
	declareGlobal(".appearance-choices.palette-choices",
		gwccss.GridCols(gwccss.Repeat(4, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".appearance-choice",
		gwccss.Position.Relative,
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(7)),
		gwccss.MinHeight(gwccss.Px(94)),
		gwccss.Padding(gwccss.Px(13)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal(".appearance-choice:hover",
		gwccss.BorderColor(gwccss.Var("accent")),
	)
	declareGlobal(".appearance-choice:has(input:checked)",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Shadow(gwccss.ShadowOf(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(2), gwccss.Var("soft"))),
	)
	declareGlobal(".appearance-choice input",
		gwccss.Position.Absolute,
		gwccss.Top(gwccss.Px(12)),
		gwccss.Right(gwccss.Px(12)),
		gwccss.Raw("accent-color", "var(--accent)"),
	)
	declareGlobal(".appearance-choice strong",
		gwccss.Raw("padding-right", "22px"),
		gwccss.FontSize(gwccss.Rem(0.875)),
	)
	declareGlobal(".appearance-choice small",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.LineHeight(gwccss.Num(1.35)),
	)
	declareGlobal(".appearance-swatches",
		gwccss.Display.Flex,
		gwccss.Gap(gwccss.Px(4)),
	)
	declareGlobal(".appearance-swatch",
		gwccss.W(gwccss.Px(23)),
		gwccss.H(gwccss.Px(23)),
		// A swatch outlined in a fixed dark ink disappears against a dark
		// canvas, which is where a colour picker most needs its edge.
		gwccss.Raw("border", "1px solid var(--line)"),
		gwccss.Rounded(gwccss.Percent(50)),
	)
	declareGlobal(".appearance-preview",
		gwccss.Position.Sticky,
		gwccss.Top(gwccss.Px(18)),
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(14)),
		gwccss.Padding(gwccss.Px(20)),
	)
	declareGlobal(".appearance-preview-grid>.appearance-preview-dark,.appearance-preview-grid>.appearance-preview-compact",
		gwccss.Display.None,
	)
	declareGlobal(":root[data-hcm-color-mode=dark] .appearance-preview-grid>.appearance-preview-light",
		gwccss.Display.None,
	)
	declareGlobal(":root[data-hcm-color-mode=dark] .appearance-preview-grid>.appearance-preview-dark",
		gwccss.Display.Block,
	)
	declareGlobal(":root[data-hcm-color-mode=system] .appearance-preview-grid>.appearance-preview-light",
		mediaRule(gwccss.RawMedia("(prefers-color-scheme:dark)"), gwccss.Display.None),
	)
	declareGlobal(":root[data-hcm-color-mode=system] .appearance-preview-grid>.appearance-preview-dark",
		mediaRule(gwccss.RawMedia("(prefers-color-scheme:dark)"), gwccss.Display.Block),
	)
	declareGlobal(".appearance-preview-window",
		gwccss.Raw("overflow", "hidden"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")),
		gwccss.Bg(gwccss.Var("canvas")),
	)
	declareGlobal(".appearance-preview-bar",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(7)),
		gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".appearance-preview-dot",
		gwccss.W(gwccss.Px(7)),
		gwccss.H(gwccss.Px(7)),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.Bg(gwccss.Var("accent")),
	)
	declareGlobal(".appearance-preview-body",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(54)), gwccss.Fr(1)),
		gwccss.MinHeight(gwccss.Px(174)),
	)
	declareGlobal(".appearance-preview-nav",
		gwccss.Display.Grid,
		gwccss.Raw("align-content", "start"),
		gwccss.Gap(gwccss.Px(7)),
		gwccss.PaddingY(gwccss.Px(12)), gwccss.PaddingX(gwccss.Px(9)),
		gwccss.Raw("border-inline-end", "1px solid var(--line)"),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".appearance-preview-nav span",
		gwccss.H(gwccss.Px(8)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-xs")),
		gwccss.Bg(gwccss.Var("line")),
	)
	declareGlobal(".appearance-preview-nav span:first-child",
		gwccss.Bg(gwccss.Var("accent")),
	)
	declareGlobal(".appearance-preview-content",
		gwccss.Display.Grid,
		gwccss.Raw("align-content", "start"),
		gwccss.Gap(gwccss.Px(9)),
		gwccss.Padding(gwccss.Px(15)),
	)
	declareGlobal(".appearance-preview-content>span",
		gwccss.W(gwccss.Percent(55)),
		gwccss.H(gwccss.Px(10)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-xs")),
		gwccss.Bg(gwccss.Var("ink")),
	)
	declareGlobal(".appearance-preview-card",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(7)),
		gwccss.Padding(gwccss.Px(12)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Raw("box-shadow", "none"),
	)
	declareGlobal(".appearance-preview-card i",
		gwccss.W(gwccss.Percent(42)),
		gwccss.H(gwccss.Px(7)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-xs")),
		gwccss.Bg(gwccss.Var("accent")),
	)
	declareGlobal(".appearance-preview-card span",
		gwccss.H(gwccss.Px(6)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-xs")),
		gwccss.Bg(gwccss.Var("line")),
	)
	declareGlobal(".appearance-actions",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(10)),
		gwccss.Raw("padding-top", "4px"),
	)
	declareGlobal(".appearance-actions-sticky",
		gwccss.Position.Sticky,
		gwccss.Raw("bottom", "0"),
		gwccss.ZIndex(4),
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(8)),
		gwccss.Padding(gwccss.Px(12)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Raw("box-shadow", "0 -8px 28px color-mix(in srgb,var(--canvas) 75%,transparent)"),
	)
	declareGlobal(".appearance-edit-preview",
		gwccss.Position.Absolute,
		gwccss.Top(gwccss.Px(8)),
		gwccss.Raw("inset-inline-end", "8px"),
		gwccss.W(gwccss.Px(40)),
		gwccss.H(gwccss.Px(40)),
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
	)
	declareGlobal(".appearance-edit-proposed",
		gwccss.Raw("padding-inline-end", "42px"),
	)
	declareGlobal(".appearance-edit-proposed",
		mediaRule(gwccss.MaxW(680), gwccss.Raw("padding-inline-end", "0")),
	)
	declareGlobal(".appearance-edit-context",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
		gwccss.Gap(gwccss.Px(10)),
	)
	declareGlobal(".appearance-edit-context>div",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(2)),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".appearance-edit-context small",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".appearance-context-short,.appearance-label-short",
		gwccss.Display.None,
	)
	declareGlobal(".appearance-context-short,.appearance-label-short",
		mediaRule(gwccss.MaxW(680), gwccss.Display.Inline),
	)
	declareGlobal(".appearance-context-long,.appearance-label-long",
		mediaRule(gwccss.MaxW(680), gwccss.Display.None),
	)
	declareGlobal(".appearance-edit-context strong",
		gwccss.Raw("overflow-wrap", "anywhere"),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".appearance-edit-command",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".appearance-edit-command .appearance-status",
		gwccss.Raw("flex", "1 1 150px"),
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".appearance-edit-command .button",
		gwccss.MinHeight(gwccss.Px(44)),
	)
	declareGlobal(".appearance-actions-sticky[data-hcm-edit-dirty=true]",
		gwccss.BorderColor(gwccss.Var("accent")),
	)
	declareGlobal(".appearance-actions-sticky",
		mediaRule(gwccss.MaxW(680), gwccss.Padding(gwccss.Px(8)), gwccss.Gap(gwccss.Px(6))),
	)
	declareGlobal(".appearance-edit-command",
		mediaRule(gwccss.MaxW(680), gwccss.Display.Grid, gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.Px(40))), gwccss.Gap(gwccss.Px(6))),
	)
	// On a phone the three commands share one row and the status sits above
	// them only when it has something to say. With no changes it repeats what
	// the disabled Save already shows, and its own row made the sticky bar a
	// fifth of the screen; it stays in the accessibility tree as the live
	// region, visually hidden, and returns when the draft is dirty.
	declareGlobal(".appearance-edit-command .appearance-status",
		mediaRule(gwccss.MaxW(680), gwccss.Raw("grid-column", "1 / -1"), gwccss.Raw("grid-row", "1"), gwccss.MinHeight(gwccss.Zero)),
	)
	declareGlobal(".appearance-actions-sticky[data-hcm-edit-dirty=false] .appearance-edit-command .appearance-status",
		mediaRule(gwccss.MaxW(680), gwccss.Position.Absolute, gwccss.W(gwccss.Px(1)), gwccss.H(gwccss.Px(1)), gwccss.Raw("overflow", "hidden"), gwccss.Raw("clip-path", "inset(50%)"), gwccss.Raw("white-space", "nowrap")),
	)
	declareGlobal(".appearance-edit-command .appearance-edit-preview",
		mediaRule(gwccss.MaxW(680), gwccss.Position.Static, gwccss.Raw("grid-column", "3"), gwccss.Raw("grid-row", "2")),
	)
	declareGlobal(".appearance-edit-command .button.primary",
		mediaRule(gwccss.MaxW(680), gwccss.Raw("grid-column", "1"), gwccss.Raw("grid-row", "2")),
	)
	declareGlobal(".appearance-edit-command .button.secondary:not(.appearance-edit-preview)",
		mediaRule(gwccss.MaxW(680), gwccss.Raw("grid-column", "2"), gwccss.Raw("grid-row", "2")),
	)
	declareGlobal(".appearance-edit-command .button",
		mediaRule(gwccss.MaxW(680), gwccss.W(gwccss.Percent(100)), gwccss.MinHeight(gwccss.Px(40)), gwccss.PaddingX(gwccss.Px(7))),
	)
	declareGlobal(".appearance-status",
		gwccss.MinHeight(gwccss.Px(22)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".appearance-status[data-tone=\"success\"]",
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(":root[data-hcm-density=\"compact\"] .surface,:root[data-hcm-density=\"compact\"] .appearance-group",
		gwccss.Raw("padding-block", "calc(20px * var(--hcm-density))"),
	)
	declareGlobal(":root[data-hcm-density=\"spacious\"] .surface,:root[data-hcm-density=\"spacious\"] .appearance-group",
		gwccss.Raw("padding-block", "calc(20px * var(--hcm-density))"),
	)
	declareGlobal(".appearance-form",
		mediaRule(gwccss.MaxW(1040), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".appearance-preview",
		mediaRule(gwccss.MaxW(1040), gwccss.Position.Static, gwccss.GridRow(gwccss.GridLineAt(1))),
	)
	declareGlobal(".appearance-choices.palette-choices",
		mediaRule(gwccss.MaxW(1040), gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))))),
	)
	declareGlobal(".appearance-intro",
		mediaRule(gwccss.MaxW(680), gwccss.Display.Grid, gwccss.Padding(gwccss.Px(18))),
	)
	declareGlobal(".appearance-intro-actions",
		mediaRule(gwccss.MaxW(680), gwccss.Raw("justify-content", "space-between")),
	)
	declareGlobal(".appearance-choices,.appearance-choices.palette-choices",
		mediaRule(gwccss.MaxW(680), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".appearance-group,.appearance-preview",
		mediaRule(gwccss.MaxW(680), gwccss.Padding(gwccss.Px(16))),
	)
	declareGlobal(".appearance-preview-body",
		mediaRule(gwccss.MaxW(680), gwccss.MinHeight(gwccss.Px(145))),
	)
	declareGlobal(".appearance-glyph-sample",
		gwccss.Display.Flex,
		gwccss.Gap(gwccss.Px(9)),
		gwccss.Raw("align-items", "center"),
		gwccss.Raw("margin-top", "auto"),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".brand-asset-actions",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Px(8)),
		gwccss.Raw("margin-top", "12px"),
	)
	declareGlobal(".brand-asset-controls .muted",
		gwccss.Display.Block,
		gwccss.Raw("margin-top", "5px"),
	)
	declareGlobal(".appearance-glyph-sample .nav-icon",
		gwccss.W(gwccss.Px(23)), gwccss.H(gwccss.Px(23)),
	)
	declareGlobal(".appearance-preview-open",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(8)),
		gwccss.Raw("justify-content", "center"),
	)
	declareGlobal(".appearance-preview-open .nav-icon",
		gwccss.W(gwccss.Px(18)), gwccss.H(gwccss.Px(18)),
	)
	declareGlobal(".appearance-preview-overlay",
		gwccss.Position.Fixed,
		gwccss.Raw("inset", "0"),
		gwccss.Raw("z-index", "100"),
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.Padding(gwccss.Px(24)),
		gwccss.Raw("background", "rgba(8,18,28,.76)"),
	)
	declareGlobal(".appearance-preview-overlay[hidden]", gwccss.Display.None)
	declareGlobal(".appearance-preview-modal",
		gwccss.W(gwccss.Percent(100)),
		gwccss.Raw("max-width", "1180px"),
		gwccss.Raw("max-height", "min(90vh,980px)"),
		gwccss.Display.Flex,
		gwccss.Raw("flex-direction", "column"),
		gwccss.Raw("overflow", "hidden"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("box-shadow", "0 28px 80px rgba(0,0,0,.34)"),
	)
	declareGlobal(".appearance-preview-modal-header",
		gwccss.Display.Flex,
		gwccss.Raw("flex-shrink", "0"),
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(16)),
		gwccss.PaddingY(gwccss.Px(17)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".appearance-preview-modal-header h2",
		gwccss.Raw("margin", "0 0 4px"), gwccss.FontSize(gwccss.Rem(1.125)),
	)
	declareGlobal(".appearance-preview-modal-header p", gwccss.Raw("margin", "0"), gwccss.FontSize(gwccss.Rem(0.8125)))
	declareGlobal(".appearance-preview-modal-tabs",
		gwccss.Display.Flex, gwccss.Gap(gwccss.Px(6)),
		gwccss.Raw("flex-shrink", "0"),
		gwccss.Raw("overflow-x", "auto"),
		gwccss.Raw("overflow-y", "hidden"),
		gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(20)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".appearance-preview-toolbar",
		gwccss.Display.Flex, gwccss.Items.Center,
		gwccss.Raw("justify-content", "flex-end"),
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Px(12)),
		gwccss.PaddingY(gwccss.Px(9)), gwccss.PaddingX(gwccss.Px(20)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".appearance-preview-choice-group",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(9)),
	)
	declareGlobal(".appearance-preview-choice-label",
		gwccss.TextColor(gwccss.Var("muted")), gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".appearance-preview-options",
		gwccss.Display.InlineFlex,
		gwccss.Padding(gwccss.Px(3)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("canvas")),
	)
	declareGlobal(".appearance-preview-option",
		gwccss.Raw("border", "0"),
		gwccss.Raw("padding", "6px 12px"),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Raw("background", "transparent"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal(".appearance-preview-option.is-selected",
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("box-shadow", "0 1px 3px rgba(0,0,0,.14)"),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".appearance-preview-option:focus-visible",
		gwccss.Raw("outline", "2px solid var(--hcm-color-focus)"),
		gwccss.Raw("outline-offset", "2px"),
	)
	declareGlobal(".appearance-preview-tab",
		gwccss.Raw("border", "1px solid transparent"),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Raw("background", "transparent"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("padding", "8px 13px"),
		gwccss.Raw("white-space", "nowrap"),
		gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal(".appearance-preview-tab:hover,.appearance-preview-tab.is-selected",
		gwccss.Bg(gwccss.Var("soft")), gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".appearance-preview-tab:focus-visible",
		gwccss.Raw("outline", "2px solid var(--hcm-color-focus)"),
		gwccss.Raw("outline-offset", "2px"),
	)
	declareGlobal(".appearance-preview-live",
		gwccss.Raw("flex", "1 1 auto"),
		gwccss.Raw("overflow", "auto"),
		gwccss.Raw("min-height", "0"),
		gwccss.Bg(gwccss.Var("canvas")),
	)
	declareGlobal(".appearance-preview-scene",
		gwccss.Raw("min-height", "100%"),
		gwccss.Bg(gwccss.Var("canvas")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".appearance-preview-live-header",
		gwccss.Display.Flex, gwccss.Items.Center,
		gwccss.Raw("position", "sticky"), gwccss.Top(gwccss.Zero),
		gwccss.Raw("z-index", "1"),
		gwccss.Raw("min-height", "58px"),
		gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(24)),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".appearance-preview-page-name",
		gwccss.Raw("margin-inline-start", "auto"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("font-size", "0.8125rem"),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".appearance-preview-live-content",
		gwccss.Padding(gwccss.Px(24)),
		gwccss.Raw("min-height", "430px"),
	)
	// The dialog owns the vertical scrollport. A nested People table scrollbar
	// makes the wheel gesture ambiguous and traps the last rows in a short pane.
	declareGlobal(".appearance-preview-scene .people-directory .data-table-scroll",
		gwccss.Raw("max-height", "none"),
		gwccss.Raw("overflow", "visible"),
	)
	// Count chips in the shared page CSS use a fixed light fill; remap them to
	// semantic colors so switching only the preview canvas remains legible.
	declareGlobal(".appearance-preview-scene .count",
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".appearance-preview-live-content :is(a,button,input,select,summary)", gwccss.Raw("cursor", "default"))
	declareGlobal(".appearance-preview-modal-note",
		gwccss.Raw("margin", "0"),
		gwccss.Raw("max-width", "none"),
		gwccss.PaddingY(gwccss.Px(9)), gwccss.PaddingX(gwccss.Px(20)),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".appearance-preview-overlay",
		mediaRule(gwccss.MaxW(680), gwccss.Padding(gwccss.Px(8))),
	)
	declareGlobal(".appearance-preview-modal",
		mediaRule(gwccss.MaxW(680), gwccss.Raw("max-height", "96vh")),
	)
	declareGlobal(".appearance-preview-modal-header",
		mediaRule(gwccss.MaxW(680), gwccss.Padding(gwccss.Px(14)), gwccss.Raw("flex-direction", "column"), gwccss.Raw("align-items", "stretch")),
	)
	declareGlobal(".appearance-preview-modal-header .button",
		mediaRule(gwccss.MaxW(680), gwccss.Raw("align-self", "flex-end")),
	)
	declareGlobal(".appearance-preview-modal-tabs",
		mediaRule(gwccss.MaxW(680), gwccss.Raw("flex-wrap", "wrap"), gwccss.Raw("overflow", "visible"), gwccss.Padding(gwccss.Px(8))),
	)
	declareGlobal(".appearance-preview-toolbar",
		mediaRule(gwccss.MaxW(680), gwccss.Raw("justify-content", "flex-start"), gwccss.Padding(gwccss.Px(8))),
	)
	declareGlobal(".appearance-preview-live-content",
		mediaRule(gwccss.MaxW(680), gwccss.Padding(gwccss.Px(14))),
	)
}

func SemanticThemeStylesheet() string {
	return buildTypedSheet(declareSemanticThemeStyles)
}

func declareSemanticThemeStyles() {
	declareGlobal(":root",
		gwccss.Raw("font-family", "var(--hcm-font-sans)"),
		gwccss.Custom("accent", "var(--hcm-color-brand-primary)"),
		gwccss.Custom("accent-hover", "var(--hcm-color-brand-hover)"),
		gwccss.Custom("soft", "var(--hcm-color-brand-soft)"),
		gwccss.Custom("ink", "var(--hcm-color-text)"),
		gwccss.Custom("muted", "var(--hcm-color-text-muted)"),
		gwccss.Custom("canvas", "var(--hcm-color-canvas)"),
		gwccss.Custom("surface", "var(--hcm-color-surface)"),
		gwccss.Custom("line", "var(--hcm-color-border)"),
		gwccss.Custom("warning", "var(--hcm-color-warning)"),
		gwccss.Custom("warning-bg", "var(--hcm-color-warning-surface)"),
		gwccss.Custom("danger", "var(--hcm-color-danger)"),
		gwccss.Custom("radius", "var(--hcm-radius-control)"),
		// Status chips follow the customer's validated control shape without
		// exposing a second, independently drifting radius preference.
		gwccss.Custom("hcm-radius-status", "var(--hcm-radius-control)"),
	)
	declareGlobal("body",
		gwccss.FontSize(gwccss.VarLength("hcm-font-size-body")),
		gwccss.Raw("line-height", "var(--hcm-line-height)"),
	)
	declareGlobal(".page-head h1",
		gwccss.FontSize(gwccss.VarLength("hcm-font-size-heading")),
	)
	declareGlobal(".button,.nav-link,.work-row,.people-row",
		gwccss.FontSize(gwccss.VarLength("hcm-font-size-small")),
	)
}

func MotionStylesheet() string {
	return buildTypedSheet(declareMotionStyles)
}

func declareMotionStyles() {
	declareGlobal(".main>.page-head,.main>.home-grid,.main>.workbench,.main>.people-page,.main>.person-page,.main>.organization-page,.main>.insights-grid,.main>.admin-grid,.main>.studio-page,.main>.jn-embedded",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Keyframes("hcm-page-enter", gwccss.At("from", gwccss.OpacityNum(gwccss.Num(.01)), gwccss.Raw("transform", "translateY(var(--hcm-motion-distance))")), gwccss.At("to", gwccss.OpacityNum(gwccss.Num(1)), gwccss.Raw("transform", "none"))), gwccss.Animation(gwccss.RawDuration("var(--hcm-motion-slow)"), gwccss.Easing("var(--hcm-motion-easing)")), gwccss.Raw("animation-fill-mode", "both")),
	)
	declareGlobal(".work-row,.people-row,.history-row,.jn-embedded .jn-griditem",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Keyframes("hcm-item-enter", gwccss.At("from", gwccss.OpacityNum(gwccss.Num(.01)), gwccss.Raw("transform", "translateY(calc(var(--hcm-motion-distance) * .65))")), gwccss.At("to", gwccss.OpacityNum(gwccss.Num(1)), gwccss.Raw("transform", "none"))), gwccss.Animation(gwccss.RawDuration("var(--hcm-motion-slow)"), gwccss.Easing("var(--hcm-motion-easing)")), gwccss.Raw("animation-fill-mode", "both")),
	)
	declareGlobal(".work-row:nth-child(2),.people-row:nth-child(2),.history-row:nth-child(2),.jn-embedded .jn-griditem:nth-child(2)",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Raw("animation-delay", "35ms")),
	)
	declareGlobal(".work-row:nth-child(3),.people-row:nth-child(3),.history-row:nth-child(3),.jn-embedded .jn-griditem:nth-child(3)",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Raw("animation-delay", "70ms")),
	)
	declareGlobal(".work-row:nth-child(4),.people-row:nth-child(4),.history-row:nth-child(4),.jn-embedded .jn-griditem:nth-child(4)",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Raw("animation-delay", "105ms")),
	)
	declareGlobal(".work-row:nth-child(n+5),.people-row:nth-child(n+5),.history-row:nth-child(n+5),.jn-embedded .jn-griditem:nth-child(n+5)",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Raw("animation-delay", "140ms")),
	)
	declareGlobal(".status,.count",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Keyframes("hcm-status-settle", gwccss.At("0%", gwccss.Transform(gwccss.Scale(.96)), gwccss.OpacityNum(gwccss.Num(.2))), gwccss.At("100%", gwccss.Transform(gwccss.Scale(1)), gwccss.OpacityNum(gwccss.Num(1)))), gwccss.Animation(gwccss.RawDuration("var(--hcm-motion-normal)"), gwccss.Easing("var(--hcm-motion-easing)")), gwccss.Raw("animation-fill-mode", "both")),
	)
	declareGlobal(".notifications[open] .popover",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Keyframes("hcm-item-enter", gwccss.At("from", gwccss.OpacityNum(gwccss.Num(.01)), gwccss.Raw("transform", "translateY(calc(var(--hcm-motion-distance) * .65))")), gwccss.At("to", gwccss.OpacityNum(gwccss.Num(1)), gwccss.Raw("transform", "none"))), gwccss.Animation(gwccss.RawDuration("var(--hcm-motion-normal)"), gwccss.Easing("var(--hcm-motion-easing)")), gwccss.Raw("animation-fill-mode", "both")),
	)
	declareGlobal(".jn-embedded .jn-loading",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Position.Relative, gwccss.Raw("overflow", "hidden")),
	)
	declareGlobal(".jn-embedded .jn-loading:after",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Position.Absolute, gwccss.Raw("inset", "0"), gwccss.Raw("content", "\"\""), gwccss.Raw("pointer-events", "none"), gwccss.Raw("background", "linear-gradient(100deg,transparent 25%,rgba(255,255,255,.65) 48%,transparent 72%)"), gwccss.Keyframes("hcm-shimmer", gwccss.At("from", gwccss.Raw("transform", "translateX(-115%)")), gwccss.At("to", gwccss.Raw("transform", "translateX(115%)"))), gwccss.Animation(gwccss.S(1.25), gwccss.Linear), gwccss.Raw("animation-iteration-count", "infinite")),
	)
	declareGlobal(".button,.nav-link,.nav-favorite,.sidebar-toggle,.surface,.jn-embedded .jn-card",
		gwccss.Raw("transition", "transform var(--hcm-motion-fast) var(--hcm-motion-easing),box-shadow var(--hcm-motion-normal) var(--hcm-motion-easing),border-color var(--hcm-motion-fast) var(--hcm-motion-easing),background-color var(--hcm-motion-fast) var(--hcm-motion-easing),color var(--hcm-motion-fast) var(--hcm-motion-easing)"),
	)
	declareGlobal(".button:hover,.jn-embedded .jn-btn:hover",
		mediaRule(gwccss.RawMedia("(hover:hover) and (prefers-reduced-motion:no-preference)"), gwccss.Transform(gwccss.TranslateY(gwccss.Px(-1)))),
	)
	declareGlobal(".button:active,.jn-embedded .jn-btn:active",
		mediaRule(gwccss.RawMedia("(hover:hover) and (prefers-reduced-motion:no-preference)"), gwccss.Raw("transform", "translateY(0) scale(.985)")),
	)
	declareGlobal(".surface:hover",
		mediaRule(gwccss.RawMedia("(hover:hover) and (prefers-reduced-motion:no-preference)"), gwccss.BorderColor(gwccss.Var("control-border"))),
	)
	declareGlobal(".notifications[open] .popover",
		mediaRule(gwccss.RawMedia("(hover:hover) and (prefers-reduced-motion:no-preference)"), gwccss.Raw("box-shadow", "var(--hcm-shadow-raised)")),
	)
	declareGlobal(":root",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.CustomDuration("hcm-motion-fast", gwccss.RawDuration(".01ms")), gwccss.CustomDuration("hcm-motion-normal", gwccss.RawDuration(".01ms")), gwccss.CustomDuration("hcm-motion-slow", gwccss.RawDuration(".01ms")), gwccss.CustomLength("hcm-motion-distance", gwccss.Px(0))),
	)
	declareGlobal("*,*::before,*::after",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("animation", "none!important"), gwccss.TransitionDuration(gwccss.RawDuration(".01ms!important")), gwccss.Raw("transition-delay", "0ms!important"), gwccss.Raw("scroll-behavior", "auto!important")),
	)
}

func InteractionMotionStylesheet() string {
	return buildTypedSheet(declareInteractionMotionStyles)
}

func declareInteractionMotionStyles() {
	declareGlobal(":root",
		gwccss.Raw("interpolate-size", "allow-keywords"),
	)
	declareGlobal(":root[data-hcm-motion-preference=\"limited\"]",
		gwccss.CustomDuration("hcm-motion-fast", gwccss.Ms(70)),
		gwccss.CustomDuration("hcm-motion-normal", gwccss.Ms(100)),
		gwccss.CustomDuration("hcm-motion-slow", gwccss.Ms(130)),
		gwccss.CustomLength("hcm-motion-distance", gwccss.Px(2)),
	)
	declareGlobal(".brand-cluster",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.Items.Center,
		gwccss.H(gwccss.Px(81)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("border-inline-end", "1px solid var(--line)"),
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".brand-cluster .wordmark",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.H(gwccss.Percent(100)),
		gwccss.Raw("padding-inline", "20px 6px"),
		gwccss.Raw("border-right", "0"),
	)
	declareGlobal(".header-nav-toggle",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.Raw("flex", "none"),
		gwccss.W(gwccss.Px(44)),
		gwccss.H(gwccss.Px(44)),
		gwccss.Raw("margin-inline-end", "8px"),
		gwccss.Border(gwccss.Px(1), gwccss.Transparent),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".header-nav-toggle:hover",
		gwccss.BorderColor(gwccss.Var("line")),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	// Header icons share one size (20px): search, the launcher and the page
	// utilities beside this toggle are all 20.
	declareGlobal(".header-nav-toggle .nav-icon",
		gwccss.W(gwccss.Px(20)),
		gwccss.H(gwccss.Px(20)),
	)
	declareGlobal(".sidebar",
		gwccss.Raw("padding-top", "14px"),
	)
	declareGlobal(".tenant",
		gwccss.Raw("padding-top", "2px"),
	)
	declareGlobal(".wordmark-label,.tenant,.nav-label,.nav-count",
		gwccss.Raw("overflow", "hidden"),
		gwccss.OpacityNum(gwccss.Num(1)),
		gwccss.Transform(gwccss.TranslateX(gwccss.Zero)),
	)
	declareGlobal(".wordmark-label",
		gwccss.MaxWidth(gwccss.Px(180)),
	)
	declareGlobal(".nav-label",
		gwccss.Display.InlineBlock,
		gwccss.MaxWidth(gwccss.Px(160)),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(".nav-count",
		gwccss.MaxWidth(gwccss.Px(54)),
	)
	declareGlobal(".app-shell.nav-collapsed .brand-cluster",
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(32)), gwccss.TrackLen(gwccss.Px(32))),
		gwccss.Gap(gwccss.Px(4)),
		gwccss.Raw("padding-inline", "2px"),
	)
	declareGlobal(".app-shell.nav-collapsed .brand-cluster .wordmark",
		gwccss.Justify.Center,
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("border", "0"),
	)
	declareGlobal(".app-shell.nav-collapsed .wordmark-mark",
		gwccss.W(gwccss.Px(30)),
		gwccss.H(gwccss.Px(30)),
	)
	declareGlobal(".app-shell.nav-collapsed .header-nav-toggle",
		gwccss.W(gwccss.Px(32)),
		gwccss.H(gwccss.Px(32)),
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".app-shell.nav-collapsed .wordmark-label",
		gwccss.Display.Block,
		gwccss.MaxWidth(gwccss.Zero),
		gwccss.OpacityNum(gwccss.Num(0)),
		gwccss.Transform(gwccss.TranslateX(gwccss.Px(-4))),
	)
	declareGlobal(".sidebar.collapsed .tenant",
		gwccss.Raw("display", "block!important"),
		gwccss.MaxHeight(gwccss.Zero),
		gwccss.Raw("padding-block", "0"),
		gwccss.OpacityNum(gwccss.Num(0)),
		gwccss.Raw("overflow", "hidden"),
		gwccss.Transform(gwccss.TranslateX(gwccss.Px(-4))),
		gwccss.Raw("pointer-events", "none"),
	)
	declareGlobal(".sidebar.collapsed .nav-label",
		gwccss.Raw("display", "inline-block!important"),
		gwccss.MaxWidth(gwccss.Zero),
		gwccss.OpacityNum(gwccss.Num(0)),
		gwccss.Transform(gwccss.TranslateX(gwccss.Px(-4))),
		gwccss.Raw("pointer-events", "none"),
	)
	declareGlobal(".sidebar.collapsed .nav-count",
		gwccss.Raw("display", "inline-flex!important"),
		gwccss.MaxWidth(gwccss.Zero),
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("padding-inline", "0"),
		gwccss.OpacityNum(gwccss.Num(0)),
		gwccss.Transform(gwccss.Scale(.85)),
		gwccss.Raw("pointer-events", "none"),
	)
	declareGlobal(".sidebar.collapsed .nav-favorite,.sidebar.collapsed .nav-section-label",
		gwccss.Display.None,
	)
	declareGlobal(".nav-group",
		gwccss.Raw("overflow", "clip"),
	)
	declareGlobal(".nav-group::details-content",
		gwccss.Raw("block-size", "0"),
		gwccss.Raw("overflow", "clip"),
		gwccss.OpacityNum(gwccss.Num(0)),
		gwccss.Raw("content-visibility", "hidden"),
		gwccss.Raw("transition", "block-size var(--hcm-motion-normal) var(--hcm-motion-easing),opacity var(--hcm-motion-fast) linear,content-visibility var(--hcm-motion-normal) allow-discrete"),
	)
	declareGlobal(".nav-group[open]::details-content",
		gwccss.Raw("block-size", "auto"),
		gwccss.OpacityNum(gwccss.Num(1)),
		gwccss.Raw("content-visibility", "visible"),
	)
	declareGlobal(".nav-chevron",
		gwccss.Raw("transform-origin", "center"),
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("transform")), gwccss.VarDuration("hcm-motion-normal"), gwccss.Easing("var(--hcm-motion-easing)")),
	)
	declareGlobal(".nav-group[open]>.nav-group-summary .nav-chevron",
		gwccss.Transform(gwccss.Rotate(gwccss.Deg(90))),
	)
	declareGlobal(":where(.shell-grid,.topbar,.sidebar,.brand-cluster,.wordmark,.wordmark-label,.tenant,.nav-label,.nav-count,.header-nav-toggle,.nav-icon,.nav-favorite,.menu-filter-control,.button,.tab,.scope,.surface,.work-row,.people-row,.history-row,.activity,.metric,.org-node,.workflow-card,.choice,.accessibility-choice,.jn-embedded .jn-card,.jn-embedded .jn-btn)",
		gwccss.Raw("transition-property", "width,max-width,max-height,grid-template-columns,opacity,transform,box-shadow,border-color,background-color,color"),
		gwccss.TransitionDuration(gwccss.VarDuration("hcm-motion-normal")),
		gwccss.Raw("transition-timing-function", "var(--hcm-motion-easing)"),
	)
	declareGlobal(":where(input,select,textarea)",
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("border-color"), gwccss.Prop("box-shadow"), gwccss.Prop("background-color")), gwccss.VarDuration("hcm-motion-fast"), gwccss.Easing("var(--hcm-motion-easing)")),
	)
	declareGlobal(".header-nav-toggle:hover .nav-icon",
		mediaRule(gwccss.RawMedia("(hover:hover) and (prefers-reduced-motion:no-preference)"), gwccss.Transform(gwccss.TranslateX(gwccss.Px(-2)))),
	)
	declareGlobal(".app-shell.nav-collapsed .header-nav-toggle:hover .nav-icon",
		mediaRule(gwccss.RawMedia("(hover:hover) and (prefers-reduced-motion:no-preference)"), gwccss.Transform(gwccss.TranslateX(gwccss.Px(2)))),
	)
	declareGlobal(".nav-link:hover .nav-icon,.nav-group-summary:hover .nav-icon",
		mediaRule(gwccss.RawMedia("(hover:hover) and (prefers-reduced-motion:no-preference)"), gwccss.Transform(gwccss.Scale(1.08))),
	)
	declareGlobal(".nav-favorite:hover",
		mediaRule(gwccss.RawMedia("(hover:hover) and (prefers-reduced-motion:no-preference)"), gwccss.Transform(gwccss.Scale(1.12), gwccss.Rotate(gwccss.Deg(-8)))),
	)
	declareGlobal(":where(.work-row,.people-row,.history-row):hover",
		mediaRule(gwccss.RawMedia("(hover:hover) and (prefers-reduced-motion:no-preference)"), gwccss.Transform(gwccss.TranslateX(gwccss.Px(2)))),
	)
	declareGlobal(":where(.metric,.org-node,.workflow-card,.choice,.accessibility-choice):hover",
		mediaRule(gwccss.RawMedia("(hover:hover) and (prefers-reduced-motion:no-preference)"), gwccss.BorderColor(gwccss.Var("control-border"))),
	)
	declareGlobal(".activity:hover .check",
		mediaRule(gwccss.RawMedia("(hover:hover) and (prefers-reduced-motion:no-preference)"), gwccss.Transform(gwccss.Scale(1.06))),
	)
	declareGlobal(".avatar:hover",
		mediaRule(gwccss.RawMedia("(hover:hover) and (prefers-reduced-motion:no-preference)"), gwccss.Transform(gwccss.Scale(1.04))),
	)
	declareGlobal(".tab:hover",
		mediaRule(gwccss.RawMedia("(hover:hover) and (prefers-reduced-motion:no-preference)"), gwccss.TextColor(gwccss.Var("accent"))),
	)
	declareGlobal(":root[data-hcm-motion-preference=\"limited\"] :where(.work-row,.people-row,.history-row,.jn-embedded .jn-griditem)",
		gwccss.Raw("animation-delay", "0ms!important"),
	)
	declareGlobal(":root[data-hcm-motion-preference=\"limited\"] :where(.header-nav-toggle:hover .nav-icon,.nav-link:hover .nav-icon,.nav-group-summary:hover .nav-icon,.nav-favorite:hover,.work-row:hover,.people-row:hover,.history-row:hover,.metric:hover,.org-node:hover,.workflow-card:hover,.choice:hover,.accessibility-choice:hover,.activity:hover .check,.avatar:hover)",
		gwccss.Raw("transform", "none!important"),
	)
	declareGlobal(".brand-cluster,.app-shell.nav-collapsed .brand-cluster",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Block, gwccss.H(gwccss.Px(65)), gwccss.Padding(gwccss.Zero), gwccss.Raw("border-right", "0")),
	)
	declareGlobal(".brand-cluster .wordmark,.app-shell.nav-collapsed .brand-cluster .wordmark",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("justify-content", "flex-start"), gwccss.H(gwccss.Px(65)), gwccss.Raw("padding-left", "16px")),
	)
	declareGlobal(".app-shell.nav-collapsed .wordmark-label",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Inline, gwccss.MaxWidth(gwccss.Px(180)), gwccss.OpacityNum(gwccss.Num(1)), gwccss.Raw("transform", "none")),
	)
	declareGlobal(".app-shell.nav-collapsed .wordmark-mark",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.Px(34)), gwccss.H(gwccss.Px(34))),
	)
	declareGlobal(".header-nav-toggle",
		mediaRule(gwccss.MaxW(760), gwccss.Display.None),
	)
	declareGlobal(".sidebar",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("padding-top", "10px")),
	)
	declareGlobal(".header-nav-toggle",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("ButtonText"))),
	)
	declareGlobal(".header-nav-toggle:hover",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Bg(gwccss.Color("Highlight")), gwccss.TextColor(gwccss.Color("HighlightText"))),
	)
	declareGlobal(".nav-group::details-content",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("transition", "none")),
	)
	declareGlobal(".nav-group[open]::details-content",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("block-size", "auto")),
	)
}

func JourneyIntegrationStylesheet() string {
	return buildTypedSheet(declareJourneyIntegrationStyles)
}

func declareJourneyIntegrationStyles() {
	// The journey surface's token bridge is declared once, in
	// declareMigrationDStyles. This block used to declare its own copy of
	// it -- 28 aliases, several disagreeing with that one about what they
	// meant: --jn-info as the brand accent rather than the info colour,
	// --jn-control-border as the muted text colour rather than a control
	// border. Source order settled it in favour of the other block, so
	// none of this ever took effect; it shipped in every response and
	// waited for somebody to edit the copy that does nothing.
	declareGlobal(".jn-embedded",
		gwccss.Display.Block,
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".jn-embedded>.jn-shell",
		gwccss.MaxWidth(gwccss.RawLength("none")),
		gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(".jn-embedded .jn-pagehead",
		gwccss.MaxWidth(gwccss.Rem(58)),
	)
	declareGlobal(".jn-embedded .jn-display",
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".jn-embedded .jn-btn[data-variant=\"primary\"]",
		gwccss.Raw("background", "linear-gradient(180deg,var(--accent),var(--accent-hover))"),
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Raw("box-shadow", "none"),
	)
	// Only a card that opens something takes the hover accent. An action
	// card is a static form; highlighting it under a resting pointer made the
	// first card on a journey look selected.
	declareGlobal(".jn-embedded .jn-card:not(.jn-action):hover",
		gwccss.BorderColor(gwccss.Var("accent")),
	)
	declareGlobal(".jn-embedded .jn-shell",
		mediaRule(gwccss.MaxW(760), gwccss.Padding(gwccss.Zero)),
	)
	declareGlobal(".jn-embedded .jn-pagehead",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("padding-inline", "2px")),
	)
}

func NavigationEnhancementsStylesheet() string {
	return buildTypedSheet(declareNavigationEnhancementsStyles)
}

func declareNavigationEnhancementsStyles() {
	declareGlobal(".sr-only",
		gwccss.Raw("position", "absolute!important"),
		gwccss.W(gwccss.RawLength("1px!important")),
		gwccss.H(gwccss.RawLength("1px!important")),
		gwccss.Padding(gwccss.RawLength("0!important")),
		gwccss.Margin(gwccss.RawLength("-1px!important")),
		gwccss.Raw("overflow", "hidden!important"),
		gwccss.Raw("clip", "rect(0,0,0,0)!important"),
		gwccss.Raw("white-space", "nowrap!important"),
		gwccss.Raw("border", "0!important"),
	)
	declareGlobal(".menu-filter",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(7)),
		gwccss.Raw("margin", "0 5px 14px"),
	)
	declareGlobal(".menu-filter-control",
		gwccss.Position.Relative,
	)
	declareGlobal(".menu-filter input",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.H(gwccss.Px(44)),
		gwccss.Raw("padding", "7px 47px 7px 11px"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Var("canvas")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".menu-filter input:focus",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".menu-filter-submit",
		gwccss.Position.Absolute,
		gwccss.Right(gwccss.Zero),
		gwccss.Top(gwccss.Zero),
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.W(gwccss.Px(44)),
		gwccss.H(gwccss.Px(44)),
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("border", "0"),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Transparent),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(1)),
	)
	declareGlobal(".menu-filter-submit:hover",
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".menu-filter-glyph",
		gwccss.W(gwccss.Px(18)),
		gwccss.H(gwccss.Px(18)),
	)
	declareGlobal(".menu-filter-clear",
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.W(gwccss.RawLength("max-content")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".menu-filter-clear:hover",
		gwccss.Raw("text-decoration", "underline"),
	)
	declareGlobal(".primary-nav>ul",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(1)),
	)
	declareGlobal(".nav-entry",
		gwccss.Position.Relative,
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.Px(28))),
		gwccss.Items.Center,
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".nav-entry>.nav-link",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("margin-right", "0"),
	)
	declareGlobal(".nav-favorite",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.W(gwccss.Px(27)),
		gwccss.H(gwccss.Px(30)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(1)),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".nav-favorite:hover",
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".nav-favorite-glyph",
		gwccss.W(gwccss.Px(18)),
		gwccss.H(gwccss.Px(18)),
	)
	declareGlobal(".nav-favorite.is-favorite .nav-favorite-glyph",
		gwccss.Raw("fill", "currentColor"),
	)
	declareGlobal(".nav-section-label",
		gwccss.Raw("padding", "13px 13px 5px"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("letter-spacing", "var(--hcm-tracking-caps)"),
		gwccss.Raw("list-style", "none"),
		gwccss.Raw("text-transform", "uppercase"),
	)
	declareGlobal(".nav-section-label:first-child",
		gwccss.Raw("padding-top", "4px"),
	)
	declareGlobal(".nav-section-all",
		gwccss.Raw("padding-top", "6px"),
	)
	declareGlobal(".nav-empty small",
		gwccss.Display.Block,
		gwccss.Raw("margin-top", "5px"),
		gwccss.LineHeight(gwccss.Num(1.35)),
	)
	declareGlobal(".nav-support-label",
		gwccss.Raw("margin", "0 12px 4px"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".nav-group",
		gwccss.MarginY(gwccss.Px(2)), gwccss.MarginX(gwccss.Zero),
	)
	declareGlobal(".nav-group-summary",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(10)),
		gwccss.MinHeight(gwccss.Px(46)),
		gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(13)),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".nav-group-summary::-webkit-details-marker",
		gwccss.Display.None,
	)
	declareGlobal(".nav-group-summary:hover,.nav-group.current>.nav-group-summary",
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".nav-group.current>.nav-group-summary",
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".nav-group-summary .nav-count",
		gwccss.Raw("margin-left", "auto"),
	)
	declareGlobal(".nav-chevron",
		gwccss.Raw("margin-left", "auto"),
		gwccss.W(gwccss.Px(16)),
		gwccss.H(gwccss.Px(16)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(1.125)),
		gwccss.LineHeight(gwccss.Num(1)),
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("transform")), gwccss.S(.15), gwccss.Ease),
	)
	declareGlobal(".nav-group-summary .nav-count+.nav-chevron",
		gwccss.Raw("margin-left", "0"),
	)
	declareGlobal(".nav-group[open]>.nav-group-summary .nav-chevron",
		gwccss.Transform(gwccss.Rotate(gwccss.Deg(90))),
	)
	declareGlobal(".subnav",
		gwccss.Raw("display", "grid!important"),
		gwccss.Gap(gwccss.Px(1)),
		gwccss.Raw("margin-inline-start", "12px!important"),
		gwccss.Raw("padding", "2px 0 5px 10px!important"),
	)
	declareGlobal(".subnav .nav-link",
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.MarginY(gwccss.Px(1)), gwccss.MarginX(gwccss.Zero),
		gwccss.PaddingY(gwccss.Px(7)), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.Gap(gwccss.Px(8)),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".subnav .nav-label",
		gwccss.Raw("white-space", "nowrap"),
	)
	// A nested item's icon a step under the top level's 20px, on the 4px
	// grid the icon set is drawn to (17px blurred the 1.8px strokes).
	declareGlobal(".subnav .nav-icon",
		gwccss.W(gwccss.Px(16)),
		gwccss.H(gwccss.Px(16)),
	)
	declareGlobal(".nav-empty",
		gwccss.MarginY(gwccss.Px(8)), gwccss.MarginX(gwccss.Px(5)),
		gwccss.Padding(gwccss.Px(12)),
		gwccss.Raw("border", "1px dashed var(--line)"),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("list-style", "none"),
		gwccss.Raw("text-align", "center"),
	)
	declareGlobal(".nav-bottom .nav-entry",
		gwccss.Display.Block,
	)
	declareGlobal(".nav-bottom .nav-favorite",
		gwccss.Display.None,
	)
	declareGlobal(".sidebar.collapsed .menu-filter,.sidebar.collapsed .nav-section-label,.sidebar.collapsed .nav-favorite",
		gwccss.Raw("display", "none!important"),
	)
	declareGlobal(".sidebar.collapsed .nav-entry",
		gwccss.Display.Block,
	)
	declareGlobal(".sidebar.collapsed .primary-nav>ul",
		gwccss.Gap(gwccss.Px(2)),
	)
	declareGlobal(".menu-filter",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("margin", "5px 0 10px")),
	)
	declareGlobal(".primary-nav",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("overflow", "visible!important")),
	)
	declareGlobal(".primary-nav>ul",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("display", "grid!important"), gwccss.W(gwccss.RawLength("100%!important")), gwccss.MaxWidth(gwccss.RawLength("100%!important"))),
	)
	declareGlobal(".nav-section-label",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("padding-left", "8px")),
	)
	declareGlobal(".subnav",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("padding-inline-start", "10px!important")),
	)
	declareGlobal(".nav-bottom",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("display", "none!important")),
	)
}

func PhotoStylesheet() string {
	return buildTypedSheet(declarePhotoStyles)
}

func declarePhotoStyles() {
	declareGlobal(".avatar[src]",
		gwccss.Display.Block,
		gwccss.Raw("object-fit", "cover"),
		gwccss.Raw("object-position", "center 32%"),
		gwccss.TextColor(gwccss.Transparent),
		gwccss.Shadow(gwccss.Shadows(gwccss.ShadowOf(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(2), gwccss.Var("surface")), gwccss.ShadowOf(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(3), gwccss.Var("line")))),
	)
	declareGlobal(".avatar.profile[src]",
		gwccss.Shadow(gwccss.Shadows(gwccss.ShadowOf(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(3), gwccss.Var("surface")), gwccss.ShadowOf(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(4), gwccss.Var("accent-hover")))),
	)
	declareGlobal(".history-identity",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(12)),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".history-person-link",
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".history-person-link:hover",
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.Raw("text-decoration", "underline"),
	)
}
