package productui

import (
	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// This file holds typed-css builders for the component styles formerly
// authored as raw strings in styles.go. Selectors stay literal semantic
// strings (the api's Sel/Fragment model); declarations are typed where a
// constructor exists. Raw covers only what has no constructor: var()
// fallbacks, logical properties, system colors, and keyword values.

func historyNavigationStylesheet() string {
	return buildTypedSheet(declareHistoryNavigationStyles)
}

func declareHistoryNavigationStyles() {
	declareGlobal(".header-navigation-tools",
		gwccss.Display.Flex,
		gwccss.Items.Center, gwccss.Gap(gwccss.Px(9)), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".history-navigation",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(4)),
	)
	declareGlobal(".header-navigation-tools>.global-search",
		gwccss.Raw("flex", "1 1 0"), gwccss.MinWidth(gwccss.Zero),
		// On broad displays the search remains useful without swallowing the
		// entire utility bar; its suggestions keep the same bounded anchor.
		mediaRule(gwccss.MinW(1440), gwccss.MaxWidth(gwccss.Px(720))),
	)
	declareGlobal(".history-navigation-button",
		gwccss.Display.Grid, gwccss.Raw("place-items", "center"),
		gwccss.W(gwccss.RawLength("var(--hcm-control-height)")), gwccss.H(gwccss.RawLength("var(--hcm-control-height)")), gwccss.Padding(gwccss.Zero),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(1)), gwccss.LineHeight(gwccss.Num(1)),
		gwccss.Transition(
			gwccss.TransitionProps(gwccss.Prop("border-color"), gwccss.Prop("background-color"), gwccss.Prop("color"), gwccss.Prop("transform")),
			gwccss.VarDuration("hcm-motion-fast"), gwccss.Easing("var(--hcm-motion-easing)"),
		),
		notDisabledRule(
			hoverRule(
				gwccss.Raw("border-color", "var(--hcm-hover-border)"),
				gwccss.Raw("background", "var(--hcm-hover-surface)"),
				gwccss.TextColor(gwccss.Var("accent")),
				gwccss.Transform(gwccss.TranslateY(gwccss.Px(-1))),
			),
		),
		disabledRule(
			gwccss.Raw("cursor", "not-allowed"), gwccss.OpacityNum(gwccss.Num(0.38)),
			gwccss.Bg(gwccss.Var("surface-subtle")), gwccss.TextColor(gwccss.Var("muted")),
		),
	)
	declareGlobal(".history-navigation-glyph",
		gwccss.W(gwccss.Px(18)), gwccss.H(gwccss.Px(18)),
	)
	declareGlobal("[dir=rtl] .history-navigation-glyph",
		gwccss.Raw("transform", "scaleX(-1)"),
	)
	declareGlobal(`:root[data-hcm-motion-preference="limited"] .history-navigation-button`,
		gwccss.TransitionDuration(gwccss.Ms(1)),
	)
	declareGlobal(`:root[data-hcm-motion-preference="limited"] .history-navigation-button:hover`,
		gwccss.Raw("transform", "none"),
	)
	declareGlobal(".topbar>.header-navigation-tools",
		mediaRule(gwccss.MaxW(760),
			gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
			gwccss.GridRow(gwccss.GridLineAt(2)),
			gwccss.Raw("padding", "0 0 14px"),
		),
	)
	declareGlobal(".header-navigation-tools>.global-search",
		mediaRule(gwccss.MaxW(760),
			gwccss.Raw("grid-column", "auto"),
			gwccss.Raw("grid-row", "auto"),
			gwccss.Padding(gwccss.Zero),
		),
	)
	declareGlobal(".header-navigation-tools",
		mediaRule(gwccss.MaxW(430), gwccss.Gap(gwccss.Px(6))),
	)
	declareGlobal(".history-navigation",
		mediaRule(gwccss.MaxW(430), gwccss.Gap(gwccss.Px(3))),
	)
	declareGlobal(".history-navigation-button",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"),
			gwccss.Raw("transition", "none"),
			hoverRule(gwccss.Raw("transform", "none")),
		),
	)
	declareGlobal(".history-navigation-button",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"),
			gwccss.Raw("border-color", "ButtonText"),
		),
	)
	declareGlobal(".history-navigation-button",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"),
			disabledRule(
				gwccss.TextColor(gwccss.Color("GrayText")), gwccss.OpacityNum(gwccss.Num(1)),
			),
		),
	)
	declareGlobal(".history-navigation-button",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"),
			notDisabledRule(
				hoverRule(
					gwccss.Bg(gwccss.Color("Highlight")), gwccss.TextColor(gwccss.Color("HighlightText")),
				),
			),
		),
	)
}

func permissionBoundaryStylesheet() string {
	return buildTypedSheet(declarePermissionBoundaryStyles)
}

func declarePermissionBoundaryStyles() {
	declareGlobal(".app-shell .button:disabled,.app-shell .button:disabled:hover",
		gwccss.Bg(gwccss.Var("surface-subtle")), gwccss.TextColor(gwccss.Var("muted")),
		gwccss.BorderColor(gwccss.Var("line")), gwccss.Raw("cursor", "not-allowed"),
		gwccss.Raw("transform", "none"), gwccss.Raw("box-shadow", "none"),
	)
	declareGlobal(".appearance-edit-boundary,.worker-id-edit-boundary", gwccss.Raw("display", "contents"))
	declareGlobal(".appearance-edit-boundary:disabled", gwccss.OpacityNum(gwccss.Num(0.82)))
	declareGlobal(".worker-id-edit-boundary:disabled", gwccss.OpacityNum(gwccss.Num(0.82)))
	declareGlobal(".appearance-edit-boundary:disabled :is(input,select,textarea,button)", gwccss.Raw("cursor", "not-allowed"))
	declareGlobal(".worker-id-edit-boundary:disabled :is(input,select,textarea,button)", gwccss.Raw("cursor", "not-allowed"))
}

func GlobalSearchStylesheet() string {
	return buildTypedSheet(declareGlobalSearchStyles)
}

func declareGlobalSearchStyles() {
	declareGlobal(".global-search",
		gwccss.Position.Relative,
		gwccss.ZIndex(30),
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".global-search-control",
		gwccss.Position.Relative,
		gwccss.Display.Flex,
		gwccss.Items.Center,
	)
	declareGlobal(".global-search-input",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinHeight(gwccss.RawLength("var(--hcm-control-height)")),
		gwccss.Raw("block-size", "var(--hcm-control-height)"),
		gwccss.Raw("padding-block", "0"),
		gwccss.Raw("padding-inline", "42px 14px"),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Raw("background", "var(--surface-subtle,var(--canvas))"),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("box-shadow", "none"),
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("border-color"), gwccss.Prop("background")), gwccss.RawDuration("var(--hcm-motion-fast,.14s)"), gwccss.Ease),
	)
	declareGlobal(".global-search-input::placeholder",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.OpacityNum(gwccss.Num(.88)),
	)
	declareGlobal(".global-search-input:hover",
		gwccss.Raw("border-color", "color-mix(in srgb,var(--accent) 44%,var(--line))"),
	)
	declareGlobal(".global-search-input:focus",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Raw("box-shadow", "var(--hcm-focus-ring)"),
		gwccss.Raw("outline", "0"),
	)
	declareGlobal(".global-search-glyph",
		gwccss.Position.Absolute,
		gwccss.Raw("inset-inline-start", "15px"),
		gwccss.ZIndex(1),
		gwccss.W(gwccss.Px(18)), gwccss.H(gwccss.Px(18)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("pointer-events", "none"),
	)
	declareGlobal(".global-search-panel",
		gwccss.Position.Absolute,
		gwccss.Top(gwccss.RawLength("calc(100% + 9px)")),
		gwccss.Raw("inset-inline", "0"),
		gwccss.ZIndex(80),
		gwccss.MaxHeight(gwccss.MinLen(gwccss.Vh(62), gwccss.Px(530))),
		gwccss.Padding(gwccss.Px(7)),
		gwccss.Raw("overflow-y", "auto"),
		gwccss.Raw("overscroll-behavior", "contain"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Raw("box-shadow", "var(--hcm-shadow-raised)"),
		gwccss.Raw("scrollbar-width", "thin"),
		gwccss.Raw("scrollbar-color", "color-mix(in srgb,var(--muted) 55%,transparent) transparent"),
		gwccss.Raw("transform-origin", "top center"),
		gwccss.Keyframes("hcm-search-enter",
			gwccss.At("from", gwccss.OpacityNum(gwccss.Num(0)), gwccss.Transform(gwccss.TranslateY(gwccss.Px(-5)), gwccss.Scale(.992))),
			gwccss.At("to", gwccss.OpacityNum(gwccss.Num(1)), gwccss.Raw("transform", "none")),
		),
		gwccss.Animation(gwccss.RawDuration("var(--hcm-motion-fast,.14s)"), gwccss.EaseOut),
		gwccss.Raw("animation-fill-mode", "both"),
	)
	declareGlobal(".global-search-panel-head",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(14)),
		gwccss.Raw("padding", "7px 10px 9px"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".global-search-panel-head strong",
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("letter-spacing", "var(--hcm-tracking-caps)"),
		gwccss.Raw("text-transform", "uppercase"),
	)
	declareGlobal(".global-search-result",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(38)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(11)),
		gwccss.MinHeight(gwccss.Px(61)),
		gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.Border(gwccss.Px(1), gwccss.Transparent),
		gwccss.Rounded(gwccss.RawLength("calc(var(--hcm-radius-control,var(--radius)) - 1px)")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".global-search-result:hover,.global-search-result.active,.global-search-result:focus",
		gwccss.Raw("border-color", "color-mix(in srgb,var(--accent) 20%,transparent)"),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.Raw("outline", "0"),
	)
	declareGlobal(".global-search-result.active",
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("accent"))),
	)
	declareGlobal("[dir=rtl] .global-search-result.active",
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(-3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("accent"))),
	)
	declareGlobal(".global-search-result>.nav-icon",
		gwccss.Raw("justify-self", "center"),
		gwccss.W(gwccss.Px(21)),
		gwccss.H(gwccss.Px(21)),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".global-search-result>.avatar.small",
		gwccss.W(gwccss.Px(34)),
		gwccss.H(gwccss.Px(34)),
	)
	declareGlobal(".global-search-copy",
		gwccss.Display.Grid,
		gwccss.MinWidth(gwccss.Zero),
		gwccss.LineHeight(gwccss.Num(1.28)),
	)
	declareGlobal(".global-search-copy strong",
		gwccss.Raw("overflow", "hidden"),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.TextOverflowEllipsis(),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(".global-search-copy small",
		gwccss.Raw("display", "-webkit-box"),
		gwccss.Raw("margin-top", "2px"),
		gwccss.Raw("overflow", "hidden"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("-webkit-box-orient", "vertical"),
		gwccss.Raw("-webkit-line-clamp", "1"),
	)
	declareGlobal(".global-search-kind",
		gwccss.Raw("align-self", "center"),
		gwccss.PaddingY(gwccss.Px(3)), gwccss.PaddingX(gwccss.Px(7)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-status")),
		gwccss.Raw("background", "color-mix(in srgb,var(--soft) 75%,var(--surface))"),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(".global-search-empty",
		gwccss.PaddingY(gwccss.Px(24)), gwccss.PaddingX(gwccss.Px(16)),
		gwccss.Raw("text-align", "center"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.875)),
	)
	declareGlobal(".global-search",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))), gwccss.GridRow(gwccss.GridLineAt(2)), gwccss.Raw("padding-bottom", "12px")),
	)
	declareGlobal(".global-search-panel",
		mediaRule(gwccss.MaxW(760), gwccss.Position.Fixed, gwccss.Top(gwccss.Px(122)), gwccss.Right(gwccss.Px(12)), gwccss.Left(gwccss.Px(12)), gwccss.MaxHeight(gwccss.RawLength("calc(100dvh - 140px)"))),
	)
	declareGlobal(".global-search-panel-head span",
		mediaRule(gwccss.MaxW(760), gwccss.Display.None),
	)
	declareGlobal(".global-search-result",
		mediaRule(gwccss.MaxW(480), gwccss.GridCols(gwccss.TrackLen(gwccss.Px(34)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))), gwccss.Raw("padding-inline", "8px")),
	)
	declareGlobal(".global-search-kind",
		mediaRule(gwccss.MaxW(480), gwccss.GridColumn(gwccss.GridLineAt(2)), gwccss.Raw("justify-self", "start"), gwccss.Raw("margin-top", "-4px")),
	)
	declareGlobal(".global-search-copy small",
		mediaRule(gwccss.MaxW(480), gwccss.Raw("-webkit-line-clamp", "2")),
	)
	declareGlobal(".global-search-panel",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("animation", "none")),
	)
	declareGlobal(".global-search-input,.global-search-panel,.global-search-result",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Border(gwccss.Px(1), gwccss.Color("CanvasText"))),
	)
	declareGlobal(".global-search-result.active",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("outline", "2px solid Highlight"), gwccss.OutlineOffset(gwccss.Px(-2))),
	)
	declareGlobal(".global-search-kind",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Border(gwccss.Px(1), gwccss.Color("CanvasText"))),
	)
	declareGlobal(".global-search-panel",
		mediaRule(gwccss.RawMedia("(print)"), gwccss.Display.None),
	)
}

func ContextSwitcherStylesheet() string {
	return buildTypedSheet(declareContextSwitcherStyles)
}

func declareContextSwitcherStyles() {
	declareGlobal(".context-switcher",
		gwccss.Position.Relative,
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".header-navigation-tools:has(>.context-switcher)",
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.TrackLen(gwccss.RawLength("auto"))), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Rem(10)), gwccss.Fr(1))),
	)
	declareGlobal(".context-switcher-summary",
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".context-switcher-summary::-webkit-details-marker",
		gwccss.Display.None,
	)
	declareGlobal(".context-switcher-trigger",
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(7)),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.MaxWidth(gwccss.Px(230)),
		gwccss.PaddingY(gwccss.Px(7)), gwccss.PaddingX(gwccss.Px(11)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
		gwccss.Raw("text-align", "start"),
	)
	declareGlobal(".context-switcher-trigger:hover",
		gwccss.Raw("border-color", "var(--hcm-hover-border,var(--accent))"),
		gwccss.Raw("background", "var(--hcm-hover-surface)"),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".context-switcher-current",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("overflow", "hidden"),
		gwccss.TextOverflowEllipsis(),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(".context-switcher-chevron",
		gwccss.Raw("flex", "none"),
		gwccss.W(gwccss.Px(16)), gwccss.H(gwccss.Px(16)),
		gwccss.Transform(gwccss.Rotate(gwccss.Deg(90))),
	)
	declareGlobal(".context-switcher[open] .context-switcher-chevron",
		gwccss.Transform(gwccss.Rotate(gwccss.Deg(-90))),
	)
	declareGlobal(".context-switcher-panel",
		gwccss.Position.Absolute,
		gwccss.ZIndex(30),
		gwccss.Raw("inset-block-start", "48px"),
		gwccss.Raw("inset-inline-end", "0"),
		gwccss.W(gwccss.RawLength("min(360px,calc(100vw - 28px))")),
		gwccss.MaxHeight(gwccss.MinLen(gwccss.Vh(70), gwccss.Px(560))),
		gwccss.Raw("overflow", "auto"),
		gwccss.Padding(gwccss.Px(16)),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.BorderColor(gwccss.Var("line")),
		gwccss.Raw("box-shadow", "var(--hcm-shadow-raised)"),
	)
	declareGlobal(".context-switcher-title",
		gwccss.Display.Block,
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(1)),
	)
	declareGlobal(".context-switcher-section-title",
		gwccss.Raw("margin", "13px 0 6px"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("letter-spacing", "var(--hcm-tracking-caps)"),
		gwccss.Raw("text-transform", "uppercase"),
	)
	declareGlobal(".context-switcher-current-detail",
		gwccss.Raw("margin", "4px 0 12px"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".context-switcher-options",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(6)),
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".context-switcher-option",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(2)),
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinHeight(gwccss.Px(48)),
		gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("text-align", "start"),
	)
	declareGlobal(".context-switcher-option:hover:not(:disabled)",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Raw("background", "var(--hcm-hover-surface)"),
	)
	declareGlobal(".context-switcher-option.current",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("accent"))),
		gwccss.Raw("background", "var(--surface-subtle,var(--soft))"),
	)
	declareGlobal(".context-switcher-option small",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".context-switcher-option:disabled",
		gwccss.Raw("cursor", "default"),
		gwccss.OpacityNum(gwccss.Num(.78)),
	)
	declareGlobal(".context-switcher-current-mark",
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".context-switcher-empty,.context-switcher-status",
		gwccss.Raw("margin", "10px 0 0"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".context-switcher-status:empty",
		gwccss.Display.None,
	)
	declareGlobal(".context-switcher",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridAuto)),
	)
	declareGlobal(".context-switcher-panel",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("inset-inline-start", "0"), gwccss.Raw("inset-inline-end", "auto")),
	)
	declareGlobal(".context-switcher-trigger",
		mediaRule(gwccss.MaxW(760), gwccss.MaxWidth(gwccss.Px(190))),
	)
	declareGlobal(".header-navigation-tools:has(>.context-switcher)",
		mediaRule(gwccss.MaxW(430), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
	declareGlobal(".header-navigation-tools:has(>.context-switcher)>.global-search",
		mediaRule(gwccss.MaxW(430), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1)))),
	)
	declareGlobal(".context-switcher-panel",
		mediaRule(gwccss.MaxW(430), gwccss.Raw("inset-block-start", "102px")),
	)
	declareGlobal(".context-switcher-trigger",
		mediaRule(gwccss.MaxW(430), gwccss.MaxWidth(gwccss.Percent(100))),
	)
	declareGlobal(".context-switcher-panel,.context-switcher-option",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.MarkImportant(gwccss.Raw("transition", "none"))),
	)
	declareGlobal(".context-switcher-trigger,.context-switcher-panel,.context-switcher-option",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("CanvasText")), gwccss.Bg(gwccss.Color("Canvas")), gwccss.TextColor(gwccss.Color("CanvasText"))),
	)
	declareGlobal(".context-switcher-option.current",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("outline", "2px solid Highlight")),
	)
	declareGlobal(".context-switcher-option small,.context-switcher-empty,.context-switcher-status",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.TextColor(gwccss.Color("GrayText"))),
	)
	declareGlobal(".context-switcher",
		mediaRule(gwccss.RawMedia("(print)"), gwccss.MarkImportant(gwccss.Display.None)),
	)
}

func PopoverStylesheet() string {
	return buildTypedSheet(declarePopoverStyles)
}

func declarePopoverStyles() {
	declareGlobal(".popover-root",
		gwccss.Position.Relative,
	)
	declareGlobal(".popover-root>summary",
		gwccss.Raw("cursor", "pointer"),
		gwccss.Raw("list-style", "none"),
		gwccss.MinWidth(gwccss.RawLength("var(--hcm-control-height)")),
		gwccss.MinHeight(gwccss.RawLength("var(--hcm-control-height)")),
	)
	declareGlobal(".popover-root>summary::-webkit-details-marker",
		gwccss.Display.None,
	)
	// The anonymous details-content box has zero height when its only child
	// floats. Chromium can paint that panel while omitting its text from the
	// accessibility tree. Flatten only the OPEN box; closed disclosures retain
	// their native hiding and keyboard behavior.
	declareGlobal(".popover-root[open]::details-content",
		gwccss.Raw("display", "contents"),
		gwccss.Raw("content-visibility", "visible"),
	)
	declareGlobal(".popover-surface",
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("box-shadow", "var(--hcm-shadow-raised,0 18px 48px color-mix(in srgb,var(--ink) 18%,transparent))"),
		gwccss.Raw("transform-origin", "top right"),
	)
	declareGlobal(".popover-root>.popover-surface",
		gwccss.Position.Absolute,
		gwccss.ZIndex(90),
		gwccss.Top(gwccss.RawLength("calc(100% + 7px)")),
		gwccss.Raw("inset-inline-end", "0"),
	)
	declareGlobal(".popover-root[open]::after",
		gwccss.Raw("content", "\"\""),
		gwccss.Position.Absolute,
		gwccss.ZIndex(89),
		gwccss.Top(gwccss.Percent(100)),
		gwccss.Raw("inset-inline-end", "0"),
		gwccss.W(gwccss.MaxLen(gwccss.Percent(100), gwccss.Px(48))),
		gwccss.H(gwccss.Px(9)),
	)
	declareGlobal(".popover-root[open]>.popover-surface",
		gwccss.Keyframes("hcm-popover-enter",
			gwccss.At("from", gwccss.OpacityNum(gwccss.Num(0)), gwccss.Transform(gwccss.TranslateY(gwccss.Px(-4)), gwccss.Scale(.992))),
			gwccss.At("to", gwccss.OpacityNum(gwccss.Num(1)), gwccss.Raw("transform", "none")),
		),
		gwccss.Animation(gwccss.RawDuration("var(--hcm-motion-fast,.14s)"), gwccss.Easing("var(--hcm-motion-easing,ease-out)")),
		gwccss.Raw("animation-fill-mode", "both"),
	)
	declareGlobal(".notifications>.popover-surface",
		gwccss.W(gwccss.Px(290)),
		gwccss.Padding(gwccss.Px(18)),
	)
	declareGlobal(".locale-menu>.popover-surface",
		gwccss.W(gwccss.RawLength("max-content")),
		gwccss.MinWidth(gwccss.Px(190)),
		gwccss.Padding(gwccss.Px(8)),
	)
	declareGlobal(".people-workflow-options",
		gwccss.W(gwccss.RawLength("min(270px,calc(100vw - 32px))")),
		gwccss.Padding(gwccss.RawLength("6px!important")),
	)
	declareGlobal(".people-workflow-options-list",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(2)),
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".global-search-panel.popover-surface",
		gwccss.Raw("transform-origin", "top center"),
	)
	declareGlobal(":root[data-hcm-motion-preference=\"limited\"] .popover-root[open]>.popover-surface",
		gwccss.Keyframes("hcm-popover-fade",
			gwccss.At("from", gwccss.OpacityNum(gwccss.Num(.94))),
			gwccss.At("to", gwccss.OpacityNum(gwccss.Num(1))),
		),
		gwccss.Raw("animation-duration", "1ms"),
	)
	declareGlobal(".notifications>.popover-surface,.locale-menu>.popover-surface",
		mediaRule(gwccss.MaxW(760), gwccss.Position.Fixed, gwccss.Top(gwccss.Px(68)), gwccss.Right(gwccss.Px(12)), gwccss.Left(gwccss.RawLength("auto")), gwccss.MaxWidth(gwccss.RawLength("calc(100vw - 24px)"))),
	)
	declareGlobal(".people-workflow-menu>.people-workflow-options",
		// Keep desktop row actions in table flow: an absolute panel is clipped
		// by the matrix scroll region when only one filtered row remains.
		mediaRule(gwccss.MinW(761), gwccss.Position.Static, gwccss.W(gwccss.Percent(100)), gwccss.MinWidth(gwccss.Px(160)), gwccss.Raw("margin-block-start", "8px")),
		mediaRule(gwccss.MaxW(760), gwccss.Position.Fixed, gwccss.Top(gwccss.RawLength("auto")), gwccss.Right(gwccss.Px(12)), gwccss.Bottom(gwccss.Px(12)), gwccss.Left(gwccss.Px(12)), gwccss.W(gwccss.RawLength("auto")), gwccss.MaxHeight(gwccss.MinLen(gwccss.RawLength("70dvh"), gwccss.Px(520))), gwccss.Raw("overflow", "auto"), gwccss.Raw("transform-origin", "bottom center")),
	)
	declareGlobal(".popover-root[open]>.popover-surface",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("animation", "none")),
	)
	declareGlobal(".popover-surface",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Border(gwccss.Px(1), gwccss.Color("CanvasText")), gwccss.Raw("box-shadow", "none")),
	)
	declareGlobal(".popover-root[open]::after",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("forced-color-adjust", "none")),
	)
}

func ViewerProfileStylesheet() string {
	return buildTypedSheet(declareViewerProfileStyles)
}

func declareViewerProfileStyles() {
	declareGlobal(".viewer-profile-link",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.W(gwccss.RawLength("var(--hcm-control-height)")),
		gwccss.H(gwccss.RawLength("var(--hcm-control-height)")),
		gwccss.Border(gwccss.Px(1), gwccss.Transparent),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".viewer-profile-link>.avatar",
		gwccss.W(gwccss.Px(40)),
		gwccss.H(gwccss.Px(40)),
		gwccss.Raw("object-fit", "cover"),
	)
	declareGlobal(".viewer-profile-link:hover",
		gwccss.BorderColor(gwccss.Var("hcm-hover-border")),
		gwccss.Bg(gwccss.Var("hcm-hover-surface")),
	)
	declareGlobal(".viewer-profile-link:hover>.avatar",
		gwccss.Transform(gwccss.Scale(1.03)),
	)
	declareGlobal(".viewer-profile-card",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.Gap(gwccss.Px(18)),
		gwccss.Items.Center,
		gwccss.PaddingY(gwccss.Px(22)), gwccss.PaddingX(gwccss.Px(24)),
	)
	declareGlobal(".viewer-profile-photo",
		gwccss.W(gwccss.Px(72)),
		gwccss.H(gwccss.Px(72)),
		gwccss.Raw("object-fit", "cover"),
	)
	declareGlobal(".viewer-profile-copy",
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".viewer-profile-copy h2,.viewer-profile-copy p",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".viewer-profile-copy h2",
		gwccss.Raw("margin-top", "2px"),
		gwccss.FontSize(gwccss.Rem(1.25)),
	)
	declareGlobal(".viewer-profile-copy p",
		gwccss.Raw("margin-top", "3px"),
	)
	declareGlobal(".viewer-profile-copy .muted",
		gwccss.Raw("margin-top", "7px"),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".viewer-profile-link",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.Px(44)), gwccss.H(gwccss.Px(44)), gwccss.MinHeight(gwccss.Px(44))),
	)
	declareGlobal(".viewer-profile-link>.avatar",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.Px(36)), gwccss.H(gwccss.Px(36))),
	)
	declareGlobal(".viewer-profile-card",
		mediaRule(gwccss.MaxW(760), gwccss.Padding(gwccss.Px(18))),
	)
	declareGlobal(".viewer-profile-photo",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.Px(60)), gwccss.H(gwccss.Px(60))),
	)
	declareGlobal(".viewer-profile-card",
		mediaRule(gwccss.MaxW(430), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".viewer-profile-photo",
		mediaRule(gwccss.MaxW(430), gwccss.W(gwccss.Px(56)), gwccss.H(gwccss.Px(56))),
	)
	declareGlobal(".viewer-profile-link",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("ButtonText"))),
	)
	declareGlobal(".viewer-profile-card",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("CanvasText"))),
	)
}

func baseStylesheet() string {
	return buildTypedSheet(declareBaseStyles)
}

func declareBaseStyles() {
	declareGlobal(":root",
		gwccss.Raw("color-scheme", "light"),
		gwccss.Raw("font-family", "\"Segoe UI Variable\",\"Segoe UI\",system-ui,sans-serif"),
		gwccss.CustomColor("accent", gwccss.Hex("006b57")),
		gwccss.CustomColor("accent-hover", gwccss.Hex("005344")),
		gwccss.CustomColor("soft", gwccss.Hex("eaf3ef")),
		gwccss.CustomColor("ink", gwccss.Hex("102238")),
		gwccss.CustomColor("muted", gwccss.Hex("526171")),
		gwccss.CustomColor("canvas", gwccss.Hex("fafaf7")),
		gwccss.CustomColor("surface", gwccss.Hex("fff")),
		gwccss.CustomColor("line", gwccss.Hex("d5ddd8")),
		gwccss.CustomColor("warning", gwccss.Hex("925400")),
		gwccss.CustomColor("warning-bg", gwccss.Hex("fff6df")),
		gwccss.CustomColor("danger", gwccss.Hex("b42318")),
		gwccss.CustomLength("radius", gwccss.Px(8)),
	)
	declareGlobal("*",
		gwccss.Raw("box-sizing", "border-box"),
	)
	declareGlobal("body",
		gwccss.Margin(gwccss.Zero),
		gwccss.Bg(gwccss.Var("canvas")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Px(16)),
		gwccss.LineHeight(gwccss.Num(1.5)),
	)
	declareGlobal("a",
		gwccss.Raw("color", "inherit"),
	)
	declareGlobal("button,input,select",
		gwccss.Raw("font", "inherit"),
	)
	declareGlobal("button,a,summary",
		gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal(":focus-visible",
		gwccss.Raw("outline", "2px solid var(--ink)"),
		gwccss.OutlineOffset(gwccss.Px(3)),
		gwccss.Shadow(gwccss.ShadowOf(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(3), gwccss.Var("surface"))),
	)
	declareGlobal(".skip-link",
		gwccss.Position.Fixed,
		gwccss.Raw("inset-inline-start", "16px"),
		gwccss.Top(gwccss.Px(-80)),
		gwccss.ZIndex(100),
		gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(14)),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Border(gwccss.Px(2), gwccss.Var("ink")),
		gwccss.Rounded(gwccss.VarLength("radius")),
	)
	declareGlobal(".skip-link:focus",
		gwccss.Top(gwccss.Px(14)),
	)
	declareGlobal(".topbar",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(232)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(220)), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.Gap(gwccss.Px(14)),
		gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(81)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Raw("padding-inline-end", "24px"),
		gwccss.Position.Sticky,
		gwccss.Top(gwccss.Zero),
		gwccss.ZIndex(20),
	)
	declareGlobal(".wordmark",
		gwccss.H(gwccss.Px(81)),
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("padding", "0 24px 0 32px"),
		gwccss.Raw("border-inline-end", "1px solid var(--line)"),
		gwccss.FontSize(gwccss.Rem(1.25)),
		gwccss.Raw("font-weight", "700"),
		gwccss.Tracking(gwccss.Ems(-.045)),
		gwccss.Raw("text-decoration", "none"),
		gwccss.Position.Relative,
	)
	declareGlobal(".wordmark:before",
		gwccss.Raw("content", "\"\""),
		gwccss.Position.Absolute,
		gwccss.Left(gwccss.Px(18)),
		gwccss.Top(gwccss.Px(28)),
		gwccss.Bottom(gwccss.Px(28)),
		gwccss.W(gwccss.Px(4)),
		gwccss.Bg(gwccss.Var("accent")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-xs")),
	)
	declareGlobal(".global-search input",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(14)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Var("canvas")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".button",
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.Justify.Center,
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(9)), gwccss.PaddingX(gwccss.Px(16)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("control-border")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".button.primary",
		gwccss.Bg(gwccss.Var("accent")),
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.TextColor(gwccss.Var("on-brand")),
	)
	declareGlobal(".button.primary:hover",
		gwccss.Bg(gwccss.Var("accent-hover")),
	)
	declareGlobal(".button.secondary",
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".button.secondary:hover",
		gwccss.Bg(gwccss.Var("soft")),
	)
	declareGlobal(".button.full",
		gwccss.W(gwccss.Percent(100)),
		gwccss.Raw("margin-top", "10px"),
	)
	declareGlobal(".notifications",
		gwccss.Position.Relative,
	)
	declareGlobal(".notifications summary",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.MinWidth(gwccss.Px(44)),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.FontSize(gwccss.Zero),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".notifications summary:before",
		gwccss.Raw("content", "\"●\""),
		gwccss.FontSize(gwccss.Px(20)),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".notifications summary:after",
		gwccss.Raw("content", "\"\""),
		gwccss.Position.Absolute,
		gwccss.Raw("inset-inline-end", "8px"),
		gwccss.Top(gwccss.Px(8)),
		gwccss.W(gwccss.Px(7)),
		gwccss.H(gwccss.Px(7)),
		gwccss.Border(gwccss.Px(2), gwccss.Var("surface")),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.Bg(gwccss.Var("danger")),
	)
	declareGlobal(".popover",
		gwccss.Position.Absolute,
		gwccss.Right(gwccss.Zero),
		gwccss.Top(gwccss.Px(50)),
		gwccss.W(gwccss.Px(290)),
		gwccss.Padding(gwccss.Px(18)),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")),
		gwccss.Raw("box-shadow", "var(--hcm-shadow-raised)"),
	)
	declareGlobal(".popover h2",
		gwccss.FontSize(gwccss.Rem(1)),
	)
	declareGlobal(".popover p",
		gwccss.TextColor(gwccss.Var("muted")),
	)
	// The notifications panel speaks the header popovers' language (Start an
	// action, Page utilities): the same panel width and padding, its title as
	// the small uppercase label, and its one destination as an accent action
	// rather than an underlined ink link.
	declareGlobal(".notification-popover",
		gwccss.Padding(gwccss.Px(14)),
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(6)),
	)
	// Scoped under .notifications to outrank the older 290px rule there.
	declareGlobal(".notifications>.notification-popover",
		gwccss.W(gwccss.MinLen(gwccss.Px(320), gwccss.RawLength("calc(100vw - 28px)"))),
	)
	declareGlobal(".notification-popover>h2",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("letter-spacing", "var(--hcm-tracking-caps)"),
		gwccss.Raw("text-transform", "uppercase"),
	)
	declareGlobal(".notification-popover>p",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".notification-popover>a",
		gwccss.Raw("justify-self", "start"),
		gwccss.Display.InlineFlex, gwccss.Items.Center,
		gwccss.MinHeight(gwccss.VarLength("hcm-control-height")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".notification-popover>a:hover",
		gwccss.Raw("text-decoration", "underline"),
	)
	declareGlobal(".avatar",
		gwccss.Raw("display", "inline-grid"),
		gwccss.Raw("place-items", "center"),
		gwccss.Raw("flex", "none"),
		gwccss.W(gwccss.Px(38)),
		gwccss.H(gwccss.Px(38)),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".avatar.large",
		gwccss.W(gwccss.Px(46)),
		gwccss.H(gwccss.Px(46)),
	)
	declareGlobal(".shell-grid",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(232)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.MinHeight(gwccss.RawLength("calc(100vh - 81px)")),
	)
	declareGlobal(".sidebar",
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Raw("padding", "26px 14px 18px"),
		gwccss.Raw("border-inline-end", "1px solid var(--line)"),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".tenant",
		gwccss.Raw("padding", "0 12px 16px"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".sidebar ul",
		gwccss.Raw("list-style", "none"),
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(".nav-link",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(10)),
		gwccss.MinHeight(gwccss.Px(46)),
		gwccss.MarginY(gwccss.Px(3)), gwccss.MarginX(gwccss.Zero),
		gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(13)),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "500"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".nav-link:hover,.nav-link[aria-current=page]",
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".nav-link[aria-current=page]",
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("accent"))),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".nav-count",
		gwccss.Raw("margin-left", "auto"),
		gwccss.PaddingY(gwccss.Px(2)), gwccss.PaddingX(gwccss.Px(7)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-status")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".nav-bottom",
		gwccss.Display.Grid,
		gwccss.Raw("margin-top", "auto"),
	)
	declareGlobal(".main",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MaxWidth(gwccss.Px(1500)),
		gwccss.MarginY(gwccss.Zero), gwccss.MarginX(gwccss.RawLength("auto")),
		gwccss.Raw("padding", "32px 34px 22px"),
	)
	declareGlobal(".page-head",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(20)),
		gwccss.Raw("margin-bottom", "30px"),
	)
	declareGlobal(".page-head>.breadcrumbs",
		gwccss.Raw("flex-basis", "100%"),
		gwccss.Raw("min-width", "0"),
	)
	declareGlobal(".appearance-scope-guidance",
		gwccss.Raw("padding", "20px 24px"),
	)
	// The logo picker is a group inside a group. It had no styles at all,
	// so it drew the browser's grooved fieldset border with its legend cut
	// into the line; it is now an inset panel titled like a form label.
	declareGlobal(".brand-asset-picker",
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Px(14)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(10)),
	)
	// The panel's explanation and status read as helper text beside its
	// fields' 14px help, not as 16px body copy above them.
	declareGlobal(".brand-asset-picker p",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("line-height", "1.45"),
	)
	declareGlobal(".brand-asset-picker>legend",
		gwccss.Raw("float", "inline-start"),
		gwccss.W(gwccss.Percent(100)),
		gwccss.Padding(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
	)
	// Card headings, at the card-heading size; as bare h2s they took the
	// section scale and outranked the page's own intro heading.
	declareGlobal(".appearance-scope-guidance>h2,.appearance-preview>div>h2",
		gwccss.FontSize(gwccss.Rem(1.125)),
		gwccss.Raw("line-height", "1.3"),
	)
	declareGlobal(".appearance-scope-guidance>h2+p,.appearance-preview>div>h2+p",
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("line-height", "1.5"),
	)
	declareGlobal(".page-head h1",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.RawLength("clamp(1.75rem,2.5vw,2.15rem)")),
		gwccss.LineHeight(gwccss.Num(1.15)),
		gwccss.Tracking(gwccss.Ems(-.04)),
	)
	declareGlobal(".subtitle,.muted",
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".subtitle",
		gwccss.Raw("margin", "8px 0 0"),
	)
	declareGlobal(".scope-wrap",
		gwccss.Display.Grid,
		gwccss.Raw("justify-items", "end"),
		gwccss.Gap(gwccss.Px(4)),
	)
	declareGlobal(".scope",
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.MinHeight(gwccss.VarLength("hcm-control-height")),
		gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".scope-wrap>span",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".surface",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".panel",
		gwccss.Raw("margin-top", "20px"),
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".section-head",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(12)),
		gwccss.Raw("padding", "20px 22px 16px"),
	)
	declareGlobal(".section-head h2",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(1.125)),
	)
	// A card's description sits a step under its 18px title and under the
	// page's 16px subtitle. At 16px it was the page subtitle's size, two
	// pixels from the title it explains, and every card head read as two
	// lines of near-equal weight.
	declareGlobal(".section-head p",
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("line-height", "1.5"),
	)
	declareGlobal(".home-grid",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1.75)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(255)), gwccss.Fr(1))),
		gwccss.Gap(gwccss.Px(20)),
		gwccss.Raw("align-items", "start"),
	)
	declareGlobal(".side-stack",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(20)),
	)
	declareGlobal(".work-list",
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".tabs",
		gwccss.Display.Flex,
		gwccss.Gap(gwccss.Px(22)),
		gwccss.PaddingY(gwccss.Zero), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".work-list .tabs",
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Raw("row-gap", "0"),
	)
	// One weight for every tab: the active tab is marked by its colour and
	// underline. It used to turn bold as well, which widened it and nudged
	// every tab after it sideways each time the selection moved.
	declareGlobal(".tab",
		gwccss.Display.InlineFlex, gwccss.Items.Center,
		gwccss.PaddingY(gwccss.Px(11)), gwccss.PaddingX(gwccss.Zero),
		gwccss.BorderBottom(gwccss.Px(2), gwccss.Transparent),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "500"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".tab.active",
		gwccss.Raw("border-bottom-color", "var(--accent)"),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	// Each filter carries its own count inside its link, so the number is
	// announced with the filter it belongs to (UXLIVE-018). A small pill, so
	// it reads as a count beside the label rather than a digit run into it
	// ("Needs your action0"). It takes the tab's colour, so the active
	// filter's count follows the active tab.
	declareGlobal(".work-tab-count",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Raw("justify-content", "center"),
		gwccss.Raw("margin-inline-start", ".375rem"),
		gwccss.Raw("min-inline-size", "1.25rem"),
		gwccss.PaddingY(gwccss.Px(1)), gwccss.PaddingX(gwccss.Px(6)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-status")),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("line-height", "1.4"),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(".count",
		gwccss.PaddingY(gwccss.Px(3)), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-status")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".work-row",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(14)),
		gwccss.MinHeight(gwccss.Px(86)),
		gwccss.PaddingY(gwccss.Px(14)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".work-row:hover,.work-row.selected",
		gwccss.Bg(gwccss.Var("soft")),
	)
	declareGlobal(".work-row.selected",
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("accent"))),
	)
	declareGlobal(".row-main",
		gwccss.Raw("flex", "1"),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".row-main strong,.row-main small,.person-line strong,.person-line small",
		gwccss.Display.Block,
	)
	declareGlobal(".row-main small,.person-line small",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".row-end",
		gwccss.Display.Grid,
		gwccss.Raw("justify-items", "end"),
		gwccss.Gap(gwccss.Px(5)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".status",
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.W(gwccss.RawLength("max-content")),
		gwccss.PaddingY(gwccss.Px(4)), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-status")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".status.warning",
		gwccss.Bg(gwccss.Var("warning-bg")),
		gwccss.TextColor(gwccss.Var("warning")),
	)
	declareGlobal(".status.success,.positive",
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".status.success",
		gwccss.Bg(gwccss.Var("soft")),
	)
	declareGlobal(".panel-foot",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.PaddingY(gwccss.Px(13)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".panel-foot a",
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".coverage",
		gwccss.Raw("padding", "0 20px 18px"),
	)
	declareGlobal(".person-line",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(12)),
		gwccss.PaddingY(gwccss.Px(14)), gwccss.PaddingX(gwccss.Zero),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".callout",
		gwccss.PaddingY(gwccss.Px(12)), gwccss.PaddingX(gwccss.Px(14)),
		gwccss.BorderLeft(gwccss.Px(3), gwccss.Var("accent")),
		gwccss.Rounded(gwccss.RawLength("0 var(--radius) var(--radius) 0")),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".callout.warning",
		gwccss.BorderColor(gwccss.Var("warning")),
		gwccss.Bg(gwccss.Var("warning-bg")),
		gwccss.TextColor(gwccss.Var("warning")),
	)
	declareGlobal(".quick-actions",
		gwccss.Display.Grid,
		gwccss.Raw("padding", "0 14px 14px"),
	)
	// Plain links only. A quick action that is a .button (Home's "Choose an
	// employee to promote") took this row padding and 47px height too and
	// stood 2px taller than every other control in the shell.
	declareGlobal(".quick-actions a:not(.button)",
		gwccss.MinHeight(gwccss.Px(47)),
		gwccss.PaddingY(gwccss.Px(12)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("text-decoration", "none"),
	)
	// Each quick action is a row that goes somewhere, and ends in the shell's
	// chevron to say so; as bare accent text in a divided list the rows read
	// as a paragraph of links. The chevron points toward the reading end.
	declareGlobal(".quick-actions a:not(.button)",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(12)),
		gwccss.Raw("font-weight", "500"),
	)
	declareGlobal(".quick-actions a:not(.button)::after",
		gwccss.Raw("content", "\"\""),
		gwccss.Raw("flex", "none"),
		gwccss.Raw("inline-size", "0.375rem"),
		gwccss.Raw("block-size", "0.375rem"),
		gwccss.Raw("border-top", "1.5px solid currentColor"),
		gwccss.Raw("border-right", "1.5px solid currentColor"),
		gwccss.Raw("transform", "rotate(45deg)"),
		gwccss.Raw("opacity", ".7"),
	)
	declareGlobal("[dir=rtl] .quick-actions a:not(.button)::after",
		gwccss.Raw("transform", "rotate(-135deg)"),
	)
	declareGlobal(".quick-actions a:not(.button):hover",
		gwccss.Bg(gwccss.Var("soft")),
	)
	declareGlobal(".recent .activity,.report-list .activity",
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".activity",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(12)),
		gwccss.MinHeight(gwccss.Px(62)),
		gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(22)),
	)
	declareGlobal(".activity>.check",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.W(gwccss.Px(32)),
		gwccss.H(gwccss.Px(32)),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".activity-check-glyph",
		gwccss.W(gwccss.Px(16)),
		gwccss.H(gwccss.Px(16)),
	)
	declareGlobal(".activity>small",
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".workbench",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1.55)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(300)), gwccss.Fr(1))),
		gwccss.Gap(gwccss.Px(20)),
		gwccss.Raw("align-items", "start"),
	)
	declareGlobal(".work-preview",
		gwccss.Position.Sticky,
		gwccss.Top(gwccss.Px(101)),
		gwccss.Padding(gwccss.Px(22)),
	)
	declareGlobal(".preview-head",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.Gap(gwccss.Px(12)),
		gwccss.Raw("padding-bottom", "18px"),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".preview-head>.status",
		gwccss.GridColumn(gwccss.GridLineAt(2)),
	)
	declareGlobal(".preview-head h2",
		gwccss.MarginY(gwccss.Px(2)), gwccss.MarginX(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(1.125)),
	)
	declareGlobal(".preview-head p",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".facts",
		gwccss.PaddingY(gwccss.Px(18)), gwccss.PaddingX(gwccss.Zero),
	)
	declareGlobal(".facts h3",
		gwccss.Raw("margin", "0 0 8px"),
	)
	declareGlobal(".facts>div",
		gwccss.Display.Flex,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(14)),
		gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".facts>div>span",
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".facts>div>strong",
		gwccss.Raw("text-align", "end"),
	)
	declareGlobal(".steps",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(3, gwccss.Fr(1))),
		gwccss.PaddingY(gwccss.Px(18)), gwccss.PaddingX(gwccss.Zero),
		gwccss.Margin(gwccss.Zero),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".steps li",
		gwccss.Raw("text-align", "center"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".steps .done,.steps .current",
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".directory-tools,.toolbar",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(14)),
		gwccss.Raw("margin-bottom", "16px"),
	)
	declareGlobal(".directory-tools p",
		gwccss.MarginY(gwccss.Px(2)), gwccss.MarginX(gwccss.Zero),
	)
	declareGlobal(".people-workspace",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.Px(325))),
		gwccss.Raw("overflow", "hidden"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".people-columns,.people-row",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Fr(1.35), gwccss.Fr(1.2), gwccss.Fr(1), gwccss.Fr(1.1), gwccss.Fr(.8)),
		gwccss.Gap(gwccss.Px(10)),
		gwccss.Items.Center,
	)
	declareGlobal(".people-columns",
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(9)), gwccss.PaddingX(gwccss.Px(17)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(".people-row",
		gwccss.MinHeight(gwccss.Px(65)),
		gwccss.PaddingY(gwccss.Px(9)), gwccss.PaddingX(gwccss.Px(17)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".people-row:hover,.people-row.selected",
		gwccss.Bg(gwccss.Var("soft")),
	)
	declareGlobal(".people-row.selected",
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("accent"))),
	)
	declareGlobal(".people-identity",
		gwccss.Display.Grid,
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Gap(gwccss.Px(2)),
	)
	// The name leads and the worker ID follows it. Neither line had a size
	// of its own, so the ID -- a <small> in the row's <th> -- inherited the
	// header cell's bold at 14px and outranked a 13px name.
	declareGlobal(".people-identity>strong",
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("line-height", "1.3"),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".people-identity>small",
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "500"),
		gwccss.Raw("line-height", "1.3"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(".person-cell,.person-head",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(10)),
	)
	declareGlobal(".person-context",
		gwccss.Padding(gwccss.Px(22)),
		gwccss.Raw("border-inline-start", "1px solid var(--line)"),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".person-head",
		gwccss.Raw("padding-bottom", "16px"),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".person-head h2,.person-head p",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".upcoming",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(4)),
		gwccss.MarginY(gwccss.Px(12)), gwccss.MarginX(gwccss.Zero),
		gwccss.Padding(gwccss.Px(13)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
	)
	declareGlobal(".org",
		gwccss.Raw("padding", "0 22px 24px"),
	)
	declareGlobal(".org-node",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(10)),
		gwccss.W(gwccss.MinLen(gwccss.Px(285), gwccss.Percent(100))),
		gwccss.MinHeight(gwccss.Px(67)),
		gwccss.Padding(gwccss.Px(12)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".org-node.leader",
		gwccss.Raw("margin", "10px auto 28px"),
		gwccss.Bg(gwccss.Var("soft")),
	)
	declareGlobal(".org-branches",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(3, gwccss.Fr(1))),
		gwccss.Gap(gwccss.Px(16)),
	)
	declareGlobal(".org-node.manager",
		gwccss.W(gwccss.Percent(100)),
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Zero, gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Var("accent"))),
	)
	declareGlobal(".coverage-strip",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(22)),
		gwccss.Raw("padding", "0 22px 20px"),
	)
	declareGlobal(".coverage-strip>p",
		gwccss.Raw("flex", "1"),
	)
	declareGlobal(".coverage-strip>.button",
		gwccss.Raw("flex", "none"),
	)
	declareGlobal(".metrics",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(3, gwccss.Fr(1))),
		gwccss.Gap(gwccss.Px(14)),
		gwccss.Raw("margin-bottom", "18px"),
	)
	declareGlobal(".metric",
		gwccss.Padding(gwccss.Px(18)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".metric strong,.metric small",
		gwccss.Display.Block,
	)
	declareGlobal(".metric strong",
		gwccss.Raw("margin-top", "7px"),
		gwccss.FontSize(gwccss.Rem(1.75)),
	)
	declareGlobal(".metric small",
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".insights-grid",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.Gap(gwccss.Px(18)),
	)
	declareGlobal(".insights-grid>.insights-evidence",
		gwccss.Raw("margin-top", "0"),
	)
	declareGlobal(".insights-evidence-body",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1.15)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(240)), gwccss.Fr(.85))),
		gwccss.Gap(gwccss.Px(28)),
		gwccss.Raw("align-items", "start"),
		gwccss.Raw("padding", "0 22px 22px"),
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))), gwccss.Gap(gwccss.Px(14)), gwccss.Raw("padding", "0 18px 18px")),
	)
	declareGlobal(".insights-evidence .facts",
		gwccss.Margin(gwccss.Zero), gwccss.Padding(gwccss.Zero), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".insights-evidence .facts>div",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Px(110)), gwccss.Fr(.45)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.Gap(gwccss.Px(16)),
		gwccss.Raw("align-items", "baseline"),
		gwccss.Raw("border-bottom", "1px solid var(--line)"),
		gwccss.PaddingY(gwccss.Px(11)), gwccss.PaddingX(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)),
		mediaRule(gwccss.MaxW(430), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))), gwccss.Gap(gwccss.Px(4))),
	)
	declareGlobal(".insights-evidence .facts>div:last-child",
		gwccss.Raw("border-bottom", "0"),
	)
	declareGlobal(".insights-evidence .facts dt",
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".insights-evidence .facts dd",
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".insights-evidence-note",
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("padding", "16px 18px"),
		gwccss.Raw("line-height", "1.5"),
		gwccss.Raw("background", "var(--surface-subtle,var(--canvas))"),
		gwccss.Raw("border", "1px solid var(--line)"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		// Supporting text beside the facts, at the description size.
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("line-height", "1.5"),
	)
	declareGlobal(".bars",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(15)),
		gwccss.Raw("padding", "10px 22px 22px"),
	)
	declareGlobal(".bars>div",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(60)), gwccss.Fr(1), gwccss.TrackLen(gwccss.Px(28))),
		gwccss.Gap(gwccss.Px(10)),
		gwccss.Items.Center,
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".bar-track",
		gwccss.H(gwccss.Px(19)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-xs")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".bar-fill",
		gwccss.Display.Block,
		gwccss.H(gwccss.Percent(100)),
		gwccss.Raw("background", "linear-gradient(90deg,var(--accent) 0 72%,color-mix(in srgb,var(--accent) 55%,var(--soft)) 72% 92%,var(--warning) 92%)"),
	)
	declareGlobal(".width-58",
		gwccss.W(gwccss.Percent(58)),
	)
	declareGlobal(".width-61",
		gwccss.W(gwccss.Percent(61)),
	)
	declareGlobal(".width-67",
		gwccss.W(gwccss.Percent(67)),
	)
	declareGlobal(".width-72",
		gwccss.W(gwccss.Percent(72)),
	)
	declareGlobal(".definition",
		gwccss.Padding(gwccss.Px(12)),
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".admin-grid",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1)),
		gwccss.Gap(gwccss.Px(18)),
	)
	declareGlobal(".admin-hero",
		gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(20)),
		gwccss.PaddingY(gwccss.Px(26)), gwccss.PaddingX(gwccss.Px(28)),
		gwccss.Raw("background", "linear-gradient(120deg,var(--soft),var(--surface) 70%)"),
	)
	declareGlobal(".admin-hero h2,.admin-hero p",
		gwccss.MarginY(gwccss.Px(4)), gwccss.MarginX(gwccss.Zero),
	)
	// Description, then the card's action beneath it, at every width. Beside
	// the text, a long action ("Open promotion workflows") took half the card
	// and folded its description into four short lines; and at body size the
	// link outranked the card's own title. The action row sits at the foot, so
	// actions line up across cards of different lengths.
	declareGlobal(".admin-card",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.Raw("grid-template-rows", "1fr auto"),
		gwccss.Gap(gwccss.Px(12)),
		gwccss.Padding(gwccss.Px(22)),
	)
	declareGlobal(".admin-card h3,.admin-card p",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".admin-card h3",
		gwccss.FontSize(gwccss.Rem(1)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("line-height", "1.35"),
	)
	declareGlobal(".admin-card h3+p",
		gwccss.Raw("margin-top", "4px"),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("line-height", "1.5"),
	)
	declareGlobal(".admin-card a",
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".admin-card>a,.admin-card>.button",
		gwccss.Raw("justify-self", "start"),
	)
	// A capability that is not offered yet ends in its status and reason, not
	// an action. The status is a chip, as statuses are elsewhere; bare, it was
	// a 16px bold word that read as a second heading, and its reason
	// outweighed the card's own description.
	declareGlobal(".admin-card>strong.warning",
		gwccss.Raw("justify-self", "start"),
		gwccss.Display.InlineFlex, gwccss.Items.Center,
		gwccss.PaddingY(gwccss.Px(2)), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-status")),
		gwccss.Bg(gwccss.Var("warning-bg")),
		gwccss.TextColor(gwccss.Var("warning")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(".admin-card>strong+p",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Raw("line-height", "1.45"),
	)
	declareGlobal(".settings-shell",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(210)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.Px(285))),
		gwccss.Raw("overflow", "hidden"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".settings-nav,.settings-form,.settings-context",
		gwccss.Padding(gwccss.Px(22)),
	)
	declareGlobal(".settings-nav",
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Px(3)),
		gwccss.Raw("border-inline-end", "1px solid var(--line)"),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".settings-nav strong",
		gwccss.Raw("margin", "11px 9px 4px"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Tracking(gwccss.Ems(.07)),
	)
	declareGlobal(".settings-nav a",
		gwccss.Padding(gwccss.Px(9)),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".settings-nav a:hover,.settings-nav a.active",
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".settings-form",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(17)),
	)
	declareGlobal(".settings-form h2,.settings-form h3,.settings-form p",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".choice-grid",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1)),
		gwccss.Gap(gwccss.Px(9)),
	)
	declareGlobal(".choice",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(3)),
		gwccss.MinHeight(gwccss.Px(76)),
		gwccss.Padding(gwccss.Px(12)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("text-align", "start"),
	)
	declareGlobal(".choice.active",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(1), gwccss.Var("accent"))),
	)
	declareGlobal(".settings-form form",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1)),
		gwccss.Gap(gwccss.Px(12)),
	)
	declareGlobal(".settings-form label",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(6)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.875)),
	)
	declareGlobal(".settings-form select",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.Padding(gwccss.Px(8)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("control-border")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".settings-form form .button",
		gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
	)
	declareGlobal(".settings-context",
		gwccss.Raw("border-inline-start", "1px solid var(--line)"),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".settings-context h2,.settings-context p",
		gwccss.Raw("margin-top", "0"),
	)
	declareGlobal(".footer",
		gwccss.Display.Flex,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(10)),
		gwccss.Raw("margin-top", "24px"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".topbar",
		mediaRule(gwccss.MaxW(1190), gwccss.GridCols(gwccss.TrackLen(gwccss.Px(210)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(180)), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
	declareGlobal(".shell-grid",
		mediaRule(gwccss.MaxW(1190), gwccss.GridCols(gwccss.TrackLen(gwccss.Px(210)))),
	)
	declareGlobal(".main",
		mediaRule(gwccss.MaxW(1190), gwccss.PaddingY(gwccss.Px(28)), gwccss.PaddingX(gwccss.Px(22))),
	)
	declareGlobal(".settings-form form",
		mediaRule(gwccss.MaxW(1190), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".settings-form form .button",
		mediaRule(gwccss.MaxW(1190), gwccss.GridColumn(gwccss.GridLineAt(1))),
	)
	// Narrow rows drop only the grade-change summary. The action facts that
	// follow it (next action, next step, disposition, assignment, due) are
	// the point of the queue and must survive every width (UXAUDIT-017).
	declareGlobal(".work-row .row-main small.row-summary",
		mediaRule(gwccss.MaxW(1190), gwccss.Display.None),
	)
	// PROMOUX-012: a person's active workflows. Each item wraps its link
	// below its facts on narrow widths rather than hiding any line.
	declareGlobal(".person-active-list",
		gwccss.Raw("list-style", "none"),
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".person-active-item",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(12)),
		gwccss.PaddingY(gwccss.Px(14)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".person-active-main",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(3)),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".person-active-main small",
		gwccss.TextColor(gwccss.Var("muted")),
	)
	// The row's five lines shipped at one weight and one size: the journey's
	// name, its status, the role change, the next step and who is being
	// waited on all read as the same fact. A person scanning their own
	// profile for what is happening to them had to read every line to find
	// the one that says what happens next.
	//
	// The status is the only one of the five that is a state rather than a
	// sentence, so it is set as a chip like every other status in the
	// product rather than as another line of text.
	// The work row is the product's densest component and appears on both
	// Home and My Work. Its five lines -- the journey, the person, the role
	// change, the next step and who is being waited on -- all shipped at one
	// size and one weight, so the row read as a paragraph and the only line
	// that tells you to do something was indistinguishable from the four
	// that do not.
	//
	// The order of importance is: what this is, who it is about, what it
	// changes, and what happens next. The last of those is set in the page's
	// own ink; everything below the title stays muted.
	declareGlobal(".row-summary",
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".row-next-step",
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("font-weight", "600"),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".row-waiting-on",
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	// The stage is a state, so it is a chip like every other state in the
	// product rather than a sixth line of prose.
	declareGlobal(".workflow-stage",
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.Raw("inline-size", "fit-content"),
		gwccss.PaddingY(gwccss.Px(2)), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-status")),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(".row-effective-date",
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("white-space", "nowrap"),
	)
	// The note under a work list explains whose rows these are. It sat at
	// body size directly against the last row, reading as another row.
	declareGlobal(".work-list-note",
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("border-block-start", "1px solid var(--line)"),
		gwccss.PaddingY(gwccss.Px(12)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.Raw("margin", "0"),
	)
	// A settings group is a heading, a sentence about it, and the cards it
	// governs. With no rule the three were unrelated boxes: the heading sat
	// at section scale competing with the page title above it, and nothing
	// tied it to the cards underneath.
	declareGlobal(".settings-group",
		gwccss.Display.Grid,
		gwccss.Raw("gap", "var(--hcm-space-1)"),
		gwccss.Raw("align-content", "start"),
		gwccss.MinWidth(gwccss.Zero),
	)
	// A group label, not a second page title -- but a step above the 1rem
	// card titles it governs, or the group and its cards read as peers.
	declareGlobal(".settings-group>h2",
		gwccss.FontSize(gwccss.Rem(1.125)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("margin", "0"),
	)
	declareGlobal(".settings-group-description",
		gwccss.Raw("margin", "0"),
		gwccss.Raw("margin-block-end", "var(--hcm-space-1)"),
	)
	// Every description on the settings page -- a group's and a card's -- is
	// set at the shell's description size (14px), as .section-head p is. Two
	// cards already were and the rest were at 16px, so the page mixed both.
	// Sign out is laid out like the settings cards beside it -- title,
	// sentence, action on a 16px rhythm. As a plain block its button sat
	// directly on the sentence above it with no space at all.
	declareGlobal(".settings-signout",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(16)), gwccss.Raw("justify-items", "start"),
	)
	declareGlobal(".settings-signout>*",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".settings-group-description,.settings-task-card-head>h2+p,:is(.settings-context,.settings-signout,.settings-task-card)>:is(h2,h3)+p",
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("line-height", "1.5"),
	)
	declareGlobal(".settings-task-groups",
		gwccss.Display.Grid,
		gwccss.Raw("gap", "var(--hcm-space-3)"),
	)
	// The card's own head had no rhythm of its own, so its title and its
	// sentence sat as far apart as two unrelated blocks.
	declareGlobal(".settings-task-card-head",
		gwccss.Display.Grid,
		gwccss.Raw("gap", "2px"),
		gwccss.Raw("margin-block-end", "var(--hcm-space-1)"),
	)
	declareGlobal(".settings-task-card-head>h2",
		gwccss.FontSize(gwccss.Rem(1)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("margin", "0"),
	)
	// The Help page's two panels -- what you can do, and who to ask when you
	// cannot -- were wrapped in a container called a grid that had no grid
	// rule, so they stacked down the left of a 1300px page with the right
	// half empty. They are peers and belong beside each other.
	declareGlobal(".help-guidance-grid",
		gwccss.Display.Grid,
		gwccss.Raw("grid-template-columns", "repeat(auto-fit,minmax(min(22rem,100%),1fr))"),
		gwccss.Raw("gap", "var(--hcm-space-3)"),
		gwccss.Raw("align-items", "start"),
	)
	// The escalation note is an aside about what to do when the product
	// cannot help, carrying role="note". It read as another body paragraph.
	declareGlobal(".help-escalation",
		gwccss.Raw("border-inline-start", "3px solid var(--accent)"),
		gwccss.Raw("padding-inline-start", "12px"),
		gwccss.Raw("margin-block", "4px"),
	)
	declareGlobal(".person-active-status",
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.Raw("inline-size", "fit-content"),
		gwccss.Raw("margin-block", "2px"),
		gwccss.PaddingY(gwccss.Px(2)), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-status")),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
	)
	// The role change is context; the next step is the thing to act on.
	declareGlobal(".person-active-summary",
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".person-active-next-step",
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("font-weight", "600"),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".person-active-waiting-on",
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	// The section had no rule at all, so its heading sat flush against the
	// list under it while every other section on the page kept its padding.
	declareGlobal(".person-active-workflows>.section-head",
		gwccss.PaddingY(gwccss.Px(18)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.Raw("padding-block-end", "12px"),
	)
	declareGlobal(".person-active-empty",
		gwccss.PaddingY(gwccss.Px(14)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)),
	)
	declareGlobal(".home-grid,.workbench,.people-workspace,.settings-shell",
		mediaRule(gwccss.MaxW(990), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".side-stack",
		mediaRule(gwccss.MaxW(990), gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1))),
	)
	declareGlobal(".work-preview",
		mediaRule(gwccss.MaxW(990), gwccss.Position.Static),
	)
	declareGlobal(".person-context,.settings-context",
		mediaRule(gwccss.MaxW(990), gwccss.Raw("border-inline-start", "0"), gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line"))),
	)
	declareGlobal(".settings-nav",
		mediaRule(gwccss.MaxW(990), gwccss.FlexDir.Row, gwccss.Raw("flex-wrap", "wrap"), gwccss.Raw("border-inline-end", "0"), gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line"))),
	)
	declareGlobal(".settings-nav strong",
		mediaRule(gwccss.MaxW(990), gwccss.Display.None),
	)
	declareGlobal(".topbar",
		mediaRule(gwccss.MaxW(760), gwccss.Position.Static, gwccss.GridCols(gwccss.Fr(1), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto"))), gwccss.PaddingY(gwccss.Zero), gwccss.PaddingX(gwccss.Px(16))),
	)
	declareGlobal(".wordmark",
		mediaRule(gwccss.MaxW(760), gwccss.H(gwccss.Px(65)), gwccss.Raw("border", "0"), gwccss.Raw("padding-inline-start", "16px")),
	)
	declareGlobal(".wordmark:before",
		mediaRule(gwccss.MaxW(760), gwccss.Left(gwccss.Px(2)), gwccss.Top(gwccss.Px(20)), gwccss.Bottom(gwccss.Px(20))),
	)
	declareGlobal(".global-search",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))), gwccss.GridRow(gwccss.GridLineAt(2)), gwccss.Raw("padding-bottom", "14px")),
	)
	declareGlobal(".topbar>.button",
		mediaRule(gwccss.MaxW(760), gwccss.Display.None),
	)
	declareGlobal(".shell-grid",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".sidebar",
		mediaRule(gwccss.MaxW(760), gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(14)), gwccss.Raw("border-inline-end", "0"), gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line"))),
	)
	declareGlobal(".tenant",
		mediaRule(gwccss.MaxW(760), gwccss.Display.None),
	)
	declareGlobal(".sidebar nav:first-of-type",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("overflow", "auto")),
	)
	declareGlobal(".sidebar ul",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Flex, gwccss.Gap(gwccss.Px(4))),
	)
	declareGlobal(".nav-link",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("white-space", "nowrap")),
	)
	declareGlobal(".nav-bottom",
		mediaRule(gwccss.MaxW(760), gwccss.Display.None),
	)
	declareGlobal(".main",
		mediaRule(gwccss.MaxW(760), gwccss.PaddingY(gwccss.Px(23)), gwccss.PaddingX(gwccss.Px(17))),
	)
	declareGlobal(".page-head",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Grid, gwccss.Raw("margin-bottom", "22px")),
	)
	declareGlobal(".scope-wrap",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("justify-items", "start")),
	)
	declareGlobal(".home-grid,.side-stack,.insights-grid",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".people-columns",
		mediaRule(gwccss.MaxW(760), gwccss.Display.None),
	)
	declareGlobal(".people-row",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1.2), gwccss.Fr(1))),
	)
	declareGlobal(".people-row>span:nth-child(n+4)",
		mediaRule(gwccss.MaxW(760), gwccss.Display.None),
	)
	declareGlobal(".people-row>span:nth-child(3)",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(2))),
	)
	declareGlobal(".org-branches,.metrics,.admin-grid",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".admin-hero",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(1)), gwccss.Raw("align-items", "flex-start"), gwccss.FlexDir.Col),
	)
	declareGlobal(".coverage-strip,.toolbar,.directory-tools",
		mediaRule(gwccss.MaxW(760), gwccss.Items.Stretch, gwccss.FlexDir.Col),
	)
	declareGlobal(".footer",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Grid),
	)
	declareGlobal(".work-row",
		mediaRule(gwccss.MaxW(420), gwccss.Raw("padding-inline", "14px")),
	)
	declareGlobal(".work-row>.avatar",
		mediaRule(gwccss.MaxW(420), gwccss.Display.None),
	)
	declareGlobal(".row-end",
		mediaRule(gwccss.MaxW(420), gwccss.MaxWidth(gwccss.Px(96))),
	)
	declareGlobal(".tabs",
		mediaRule(gwccss.MaxW(420), gwccss.Gap(gwccss.Px(16)), gwccss.Raw("padding-inline", "15px"), gwccss.Raw("overflow", "auto")),
	)
	declareGlobal(".activity",
		mediaRule(gwccss.MaxW(420), gwccss.Raw("padding-inline", "15px")),
	)
	declareGlobal(".settings-form,.settings-context",
		mediaRule(gwccss.MaxW(420), gwccss.Padding(gwccss.Px(18))),
	)
	declareGlobal(".choice-grid",
		mediaRule(gwccss.MaxW(420), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal("*",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("scroll-behavior", "auto!important"), gwccss.TransitionDuration(gwccss.RawDuration(".01ms!important")), gwccss.Raw("animation-duration", ".01ms!important")),
	)
	declareGlobal(":root",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.CustomColor("line", gwccss.Color("CanvasText"))),
	)
	declareGlobal(".nav-link[aria-current=page],.status,.surface,.people-workspace",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Border(gwccss.Px(1), gwccss.Color("CanvasText"))),
	)
	declareGlobal(".topbar,.sidebar,.scope-wrap,.button,.notifications",
		mediaRule(gwccss.RawMedia("print"), gwccss.Raw("display", "none!important")),
	)
	declareGlobal(".shell-grid",
		mediaRule(gwccss.RawMedia("print"), gwccss.Display.Block),
	)
	declareGlobal(".main",
		mediaRule(gwccss.RawMedia("print"), gwccss.MaxWidth(gwccss.RawLength("none")), gwccss.Padding(gwccss.Zero)),
	)
	declareGlobal(".surface,.people-workspace,.settings-shell",
		mediaRule(gwccss.RawMedia("print"), gwccss.Raw("break-inside", "avoid"), gwccss.BorderColor(gwccss.Hex("000"))),
	)
	declareGlobal(".footer",
		mediaRule(gwccss.RawMedia("print"), gwccss.BorderTop(gwccss.Px(1), gwccss.Hex("000")), gwccss.Raw("padding-top", "12px")),
	)
	declareVisualFoundationStyles()
}

func declareVisualFoundationStyles() {
	declareGlobal(":root",
		gwccss.Custom("hcm-control-height", "44px"),
		gwccss.Custom("hcm-control-height-compact", "44px"),
		gwccss.Custom("hcm-focus-ring", "0 0 0 3px color-mix(in srgb,var(--hcm-color-focus) 24%,transparent)"),
		gwccss.Custom("hcm-scrollbar-thumb", "color-mix(in srgb,var(--muted) 42%,transparent)"),
		gwccss.Custom("hcm-scrollbar-track", "color-mix(in srgb,var(--surface) 72%,var(--canvas))"),
		gwccss.CustomLength("hcm-scrollbar-size", gwccss.Px(10)),
	)
	declareGlobal(":where(.app-shell,.jn-embedded)",
		gwccss.Raw("font-family", "var(--hcm-font-sans)"),
		gwccss.Raw("text-rendering", "optimizeLegibility"),
		gwccss.Raw("-webkit-font-smoothing", "antialiased"),
		gwccss.Raw("scrollbar-color", "var(--hcm-scrollbar-thumb) var(--hcm-scrollbar-track)"),
		gwccss.Raw("scrollbar-width", "thin"),
	)
	declareGlobal(":where(.app-shell,.jn-embedded) h1",
		gwccss.LineHeight(gwccss.Num(1.12)), gwccss.Tracking(gwccss.Ems(-.035)),
	)
	declareGlobal(":where(.app-shell,.jn-embedded) :is(h2,h3,h4,h5,h6)",
		gwccss.LineHeight(gwccss.Num(1.25)),
	)
	declareGlobal(":where(.app-shell,.jn-embedded) :is(p,ul,ol,dl,blockquote,pre)",
		gwccss.Raw("margin-block", "0"),
	)
	// Twenty-two rules across this product set their own padding on an input
	// or a select. Any one of them can push that control past the shared
	// minimum, and then a filter row reads as a stack of nearly-aligned
	// boxes: on People the text field came out 45px at y246 beside a 44px
	// select at y247.
	//
	// A minimum cannot prevent that, because padding adds to it. A fixed
	// block-size can: box-sizing is border-box here, so the padding those
	// rules choose changes where the text sits inside the control and never
	// how tall the control is.
	//
	// Single-line controls only. A textarea is sized by its rows, and
	// checkboxes, radios, file, range and colour inputs have their own
	// geometry -- forcing any of them to a text field's height would be a
	// different bug, not a fix.
	declareGlobal(":where(.app-shell,.jn-embedded) :is(input:not([type=\"checkbox\"]):not([type=\"radio\"]):not([type=\"file\"]):not([type=\"range\"]):not([type=\"color\"]),select:not([multiple]))",
		gwccss.Raw("block-size", "var(--hcm-control-height)"),
	)
	// A field's own type: the app's family at body size and regular weight.
	// With font:inherit a field took whatever its parent set, and most fields
	// here sit inside their <label>, so an entered value came out in the
	// label's 12px bold caption (Worker IDs, Brand & appearance, ...).
	// Components that want a smaller field still set it themselves.
	declareGlobal(":where(.app-shell,.jn-embedded) :is(input,select,textarea)",
		gwccss.MinHeight(gwccss.VarLength("hcm-control-height")),
		gwccss.Raw("font-family", "inherit"),
		gwccss.FontSize(gwccss.Rem(1)),
		gwccss.Raw("font-weight", "400"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("control-border")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
	)
	// font-family only. The shorthand also reset size and weight, and at the
	// same specificity as .button and .jn-btn, and later, it won: every button
	// took its surroundings' text size, so one action ("Choose an employee to
	// promote") was 14px/600 on Home and 16px/400 on Journeys.
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.button,.jn-btn)",
		gwccss.Raw("font-family", "inherit"),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
	)
	// A full-size journey button is the same control as .button and takes
	// the shell's control height; it was 43px beside 44px controls.
	declareGlobal(":where(.app-shell,.jn-embedded) .jn-btn:not([data-size=\"sm\"])",
		gwccss.MinHeight(gwccss.VarLength("hcm-control-height")),
	)
	// Keep summary-like controls on the same minimum geometry as native
	// controls, even when a component stylesheet supplies a narrower default.
	declareGlobal(".context-switcher-trigger,.popover-root>summary",
		gwccss.MinHeight(gwccss.RawLength("var(--hcm-control-height)")),
	)
	declareGlobal(".popover-root>summary:focus-visible",
		gwccss.Raw("outline", "2px solid var(--hcm-color-focus)"),
		gwccss.OutlineOffset(gwccss.Px(2)),
		gwccss.Raw("box-shadow", "var(--hcm-focus-ring)"),
	)
	// Keep the existing authorized drawer trigger discoverable while the
	// off-canvas navigation covers the header. The trigger's accessible name
	// already changes to "Close navigation menu"; this only restores a visible
	// close affordance and keeps it above the drawer surface.
	declareGlobal(":where(.app-shell):has(.sidebar.nav-drawer-open) .nav-drawer-trigger",
		gwccss.Position.Fixed,
		gwccss.Raw("inset-block-start", "12px"),
		gwccss.Raw("inset-inline-start", "calc(min(86vw,320px) - 52px)"),
		gwccss.ZIndex(60),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("box-shadow", "var(--hcm-shadow-raised)"),
	)
	declareGlobal(":where(.app-shell):has(.sidebar.nav-drawer-open) .nav-drawer-trigger .nav-icon",
		gwccss.Display.None,
	)
	declareGlobal(":where(.app-shell):has(.sidebar.nav-drawer-open) .nav-drawer-trigger::after",
		gwccss.Raw("content", "\"×\""),
		gwccss.FontSize(gwccss.Px(26)),
		gwccss.LineHeight(gwccss.Num(1)),
	)
	declareGlobal(":where(.app-shell,.jn-embedded) :is(button,input,select,textarea):focus-visible",
		gwccss.Raw("outline", "2px solid var(--hcm-color-focus)"),
		gwccss.OutlineOffset(gwccss.Px(2)),
		gwccss.Raw("box-shadow", "var(--hcm-focus-ring)"),
	)
	// Text-entry fields show focus the way the header search field always
	// has: the border turns accent and the focus halo sits against it. The
	// ring above is right for buttons and links, which have no border of
	// their own to light up, but around a field it drew a second, dark
	// outline a few pixels off the border, and forms disagreed with each
	// other (the Worker ID and admin forms had their own faint outline).
	// The transparent outline is invisible normally and is what forced-
	// colours mode paints once it drops the halo.
	// Checkboxes and radios are not fields: the rule above gave them the 44px
	// control height too, so a 13px radio stood 44px tall and baseline
	// alignment dropped its label to the line below it (the position picker).
	// Their target is the label they sit in, which carries the 44px.
	declareGlobal(":where(.app-shell,.jn-embedded) :is(input[type=checkbox],input[type=radio])",
		gwccss.Raw("min-height", "auto"),
		gwccss.Raw("block-size", "auto"),
		// Checked in the brand color everywhere, not only in the four
		// components that set it; elsewhere they checked in browser blue.
		gwccss.Raw("accent-color", "var(--accent)"),
	)
	declareGlobal(textFieldFocusSelector,
		gwccss.Raw("border-color", "var(--accent)"),
		gwccss.Raw("outline", "2px solid transparent"),
		gwccss.OutlineOffset(gwccss.Px(0)),
		gwccss.Raw("box-shadow", "var(--hcm-focus-ring)"),
	)
	declareGlobal(":where(.app-shell,.jn-embedded) :is(button,input,select,textarea):disabled",
		gwccss.OpacityNum(gwccss.Num(.58)),
		gwccss.Raw("cursor", "not-allowed"),
	)
	declareGlobal(":where(.app-shell,.jn-embedded) :is(button,a):not(:disabled):active",
		gwccss.Raw("transform", "translateY(1px)"),
	)
	declareGlobal(":where(.app-shell) :is(.surface,.panel,.people-workspace,.settings-shell,.metric,.choice,.org-node)",
		gwccss.BorderColor(gwccss.Var("line")),
		gwccss.Raw("border-radius", "var(--hcm-radius-surface)"),
		gwccss.Raw("box-shadow", "none"),
	)
	declareGlobal(":where(.app-shell) :is(.surface,.panel,.people-workspace,.settings-shell,.metric,.choice,.org-node):focus-within",
		gwccss.Raw("box-shadow", "var(--hcm-focus-ring)"),
	)
	declareGlobal(":where(.app-shell) :is(.status.success,.positive)",
		gwccss.TextColor(gwccss.Var("success")),
	)
	declareGlobal(":where(.app-shell) .status.success",
		gwccss.Bg(gwccss.Var("success-bg")),
	)
	declareGlobal(":where(.app-shell) .status.warning",
		gwccss.Bg(gwccss.Var("warning-bg")), gwccss.TextColor(gwccss.Var("warning")),
	)
	declareGlobal(":where(.app-shell) .status.danger",
		gwccss.Bg(gwccss.Var("danger-bg")), gwccss.TextColor(gwccss.Var("danger")),
	)
	declareGlobal(":where(.app-shell) .status.info",
		gwccss.Bg(gwccss.Var("info-bg")), gwccss.TextColor(gwccss.Var("info")),
	)
	declareGlobal(":where(.app-shell) :is(.button.primary,.button.secondary):hover",
		gwccss.BorderColor(gwccss.Var("accent-hover")),
	)
	declareGlobal(":where(.app-shell) .button.primary",
		gwccss.TextColor(gwccss.Var("on-brand")),
	)
	declareGlobal(":where(.app-shell) .button.secondary",
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(":where(.app-shell) .button:disabled",
		gwccss.Bg(gwccss.Var("surface-subtle")), gwccss.TextColor(gwccss.Var("muted")),
		gwccss.BorderColor(gwccss.Var("line")), gwccss.Raw("box-shadow", "none"),
	)
	declareGlobal(":where(.app-shell) ::selection",
		gwccss.Bg(gwccss.Var("accent")), gwccss.TextColor(gwccss.Var("on-brand")),
	)
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.main-scroll,.global-search-panel,.data-table-scroll,.action-launcher-dialog,.utility-drawer-dialog,.people-workflow-options)",
		gwccss.Raw("scrollbar-color", "var(--hcm-scrollbar-thumb) var(--hcm-scrollbar-track)"),
		gwccss.Raw("scrollbar-width", "thin"),
	)
	// Every other scroll area -- the position picker, role tables, a long
	// select list -- takes the same thin themed bar. scrollbar-width does not
	// inherit, so each unnamed container drew the browser's wide default with
	// arrow buttons inside an otherwise themed page.
	declareGlobal(":where(.app-shell,.jn-embedded) :where(*)",
		gwccss.Raw("scrollbar-width", "thin"),
	)
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.main-scroll,.global-search-panel,.data-table-scroll,.action-launcher-dialog,.utility-drawer-dialog,.people-workflow-options)::-webkit-scrollbar",
		gwccss.W(gwccss.VarLength("hcm-scrollbar-size")), gwccss.H(gwccss.VarLength("hcm-scrollbar-size")),
	)
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.main-scroll,.global-search-panel,.data-table-scroll,.action-launcher-dialog,.utility-drawer-dialog,.people-workflow-options)::-webkit-scrollbar-track",
		gwccss.Bg(gwccss.Var("hcm-scrollbar-track")),
	)
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.main-scroll,.global-search-panel,.data-table-scroll,.action-launcher-dialog,.utility-drawer-dialog,.people-workflow-options)::-webkit-scrollbar-thumb",
		gwccss.Bg(gwccss.Var("hcm-scrollbar-thumb")),
		gwccss.Rounded(gwccss.Px(999)),
		gwccss.Raw("border", "3px solid transparent"), gwccss.Raw("background-clip", "padding-box"),
	)
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.main-scroll,.global-search-panel,.data-table-scroll,.action-launcher-dialog,.utility-drawer-dialog,.people-workflow-options)",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("scrollbar-color", "ButtonText Canvas")),
	)
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.main-scroll,.global-search-panel,.data-table-scroll,.action-launcher-dialog,.utility-drawer-dialog,.people-workflow-options)::-webkit-scrollbar-track",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Bg(gwccss.Color("Canvas"))),
	)
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.main-scroll,.global-search-panel,.data-table-scroll,.action-launcher-dialog,.utility-drawer-dialog,.people-workflow-options)::-webkit-scrollbar-thumb",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("Canvas")), gwccss.Bg(gwccss.Color("ButtonText"))),
	)
	declareGlobal(":root[data-hcm-density=\"compact\"] :where(.app-shell,.jn-embedded) :is(button,input,select,textarea)",
		gwccss.MinHeight(gwccss.VarLength("hcm-control-height-compact")),
	)
	declareGlobal(":root[data-hcm-density=\"compact\"] :where(.app-shell) :is(.section-head,.settings-nav,.settings-form,.settings-context)",
		gwccss.Raw("padding-block", "calc(var(--hcm-space-2) * var(--hcm-density))"),
	)
	declareGlobal(":root[data-hcm-density=\"spacious\"] :where(.app-shell) :is(.section-head,.settings-nav,.settings-form,.settings-context)",
		gwccss.Raw("padding-block", "calc(var(--hcm-space-3) * var(--hcm-density))"),
	)
	declareGlobal(":where(.app-shell,.jn-embedded)",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"),
			gwccss.Raw("scroll-behavior", "auto!important"),
			gwccss.Raw("transition-duration", ".01ms!important"),
			gwccss.Raw("animation-duration", ".01ms!important"),
			gwccss.Raw("animation-iteration-count", "1!important"),
		),
	)
	declareGlobal(":root[data-hcm-motion-preference=\"limited\"] :where(.app-shell,.jn-embedded) *",
		gwccss.TransitionDuration(gwccss.RawDuration("1ms!important")),
		gwccss.Raw("animation-duration", "1ms!important"),
	)
	declareGlobal(":where(.app-shell,.jn-embedded)",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"),
			gwccss.Raw("scrollbar-color", "ButtonText Canvas"),
		),
	)
}

func BreadcrumbStylesheet() string {
	return buildTypedSheet(declareBreadcrumbStyles)
}

func declareBreadcrumbStyles() {
	declareGlobal(".breadcrumbs",
		gwccss.Raw("margin-block", "0 10px"),
	)
	declareGlobal(".breadcrumbs ol",
		gwccss.Display.Flex,
		gwccss.FlexWrap.Wrap,
		gwccss.Items.Center,
		gwccss.RowGap(gwccss.Px(4)),
		gwccss.ColumnGap(gwccss.Px(8)),
		gwccss.Raw("list-style", "none"),
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(".breadcrumbs li",
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.ColumnGap(gwccss.Px(8)),
		gwccss.MinWidth(gwccss.Zero),
	)
	// Underlined on hover only, as the rule always meant: without resetting
	// the browser's link underline it was underlined at rest as well.
	declareGlobal(".breadcrumbs a",
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.Raw("text-decoration", "none"),
		gwccss.Raw("font-weight", "500"),
		gwccss.TextUnderlineOffset(gwccss.Px(2)),
		hoverRule(gwccss.Raw("text-decoration", "underline")),
	)
	declareGlobal(".breadcrumbs [aria-current=page]",
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontWeight.Semibold,
	)
	declareGlobal(".breadcrumb-separator",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.UserSelect.None,
	)
}

func UtilityDrawerStylesheet() string {
	return buildTypedSheet(declareUtilityDrawerStyles)
}

func declareUtilityDrawerStyles() {
	// Page utilities is the same kind of control as Start an action and sits
	// beside it in the same bar, so every value below is the launcher's: the
	// same trigger, the same anchored popover, the same head, the same rows.
	// The two used to disagree on nearly all of them -- a different radius
	// nesting, a text "Close utilities" button above the content, bordered
	// section blocks, body-size row titles -- and a reader moving between them
	// saw two design systems in one toolbar.
	//
	// The root is what anchors the popover. The trigger and dialog used to be
	// a bare fragment, so the dialog's position:absolute resolved against
	// whatever positioned ancestor happened to be nearest.
	declareGlobal(".utility-drawer", gwccss.Raw("box-sizing", "border-box"))
	declareGlobal(".utility-drawer-root", gwccss.Position.Relative, gwccss.MinWidth(gwccss.Px(0)))
	declareGlobal(".utility-drawer-trigger",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(7)),
		gwccss.MinHeight(gwccss.Px(44)), gwccss.MaxWidth(gwccss.Px(230)),
		gwccss.PaddingY(gwccss.Zero), gwccss.PaddingX(gwccss.Px(11)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(0.75)), gwccss.Raw("font-weight", "700"), gwccss.Raw("text-align", "start"),
		hoverRule(
			gwccss.Raw("border-color", "var(--hcm-hover-border,var(--accent))"),
			gwccss.Raw("background", "var(--hcm-hover-surface)"),
			gwccss.TextColor(gwccss.Var("accent")),
		),
	)
	declareGlobal(".utility-drawer-trigger .nav-icon", gwccss.Raw("flex", "none"))
	// Open is shown on the trigger as well as by the popover, so a reader who
	// looks back at the bar can tell which control the open panel belongs to.
	declareGlobal(".utility-drawer-open>.utility-drawer-trigger",
		gwccss.Raw("border-color", "var(--accent)"),
		gwccss.Raw("background", "var(--hcm-hover-surface)"),
	)
	declareGlobal(".utility-drawer-dialog",
		gwccss.Position.Absolute, gwccss.ZIndex(30),
		gwccss.Raw("inset-block-start", "48px"), gwccss.Raw("inset-inline-end", "0"),
		gwccss.W(gwccss.MinLen(gwccss.Px(400), gwccss.RawLength("calc(100vw - 28px)"))),
		gwccss.MaxHeight(gwccss.MinLen(gwccss.Vh(70), gwccss.Px(560))),
		gwccss.Raw("overflow", "auto"),
		gwccss.Padding(gwccss.Px(12)),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")),
		gwccss.Raw("box-shadow", "var(--hcm-shadow-raised)"),
	)
	declareGlobal(".utility-drawer-dialog-hidden", gwccss.Display.None)
	declareGlobal(".utility-drawer-head",
		gwccss.Display.Flex, gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"), gwccss.Gap(gwccss.Px(8)),
		gwccss.Raw("margin-bottom", "10px"), gwccss.Raw("padding-bottom", "6px"),
		gwccss.Raw("border-block-end", "1px solid var(--line)"),
	)
	declareGlobal(".utility-drawer-head>strong",
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("letter-spacing", "var(--hcm-tracking-caps)"),
		gwccss.Raw("text-transform", "uppercase"),
	)
	// The close control keeps its 44px target but no longer looks like a
	// second form button: it is the head's quiet dismiss, as Escape and a
	// click outside are.
	declareGlobal(".utility-drawer-close",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Raw("justify-content", "center"),
		gwccss.MinHeight(gwccss.Px(44)), gwccss.MinWidth(gwccss.Px(44)),
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("border", "1px solid transparent"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Raw("background", "transparent"), gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("cursor", "pointer"),
		hoverRule(
			gwccss.TextColor(gwccss.Var("ink")),
			gwccss.Raw("background", "var(--hcm-hover-surface)"),
		),
	)
	declareGlobal(".utility-drawer-section",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(2)),
		gwccss.Raw("margin-block", "0 10px"),
	)
	declareGlobal(".utility-drawer-section:last-child", gwccss.Raw("margin-block-end", "0"))
	// Scoped to the dialog because the type scale's
	// ":where(.app-shell) :is(h2,[data-type-role=section],.type-section)"
	// scores (0,1,0) from its attribute and class arguments, ties a single
	// class, and comes later: the bare class rule lost and this 0.75rem group
	// label rendered at section-heading size.
	declareGlobal(".utility-drawer-dialog .utility-drawer-section-title",
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("padding-inline", "10px"), gwccss.Raw("padding-block", "4px 2px"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)), gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(".utility-drawer-list",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(2)),
		gwccss.Raw("list-style", "none"),
		gwccss.Margin(gwccss.Zero), gwccss.Padding(gwccss.Zero),
	)
	// The launcher's row: borderless at rest; on hover the soft fill and
	// faint accent edge that global search and the launcher use. The
	// transparent border is reserved so nothing moves when it is highlighted.
	declareGlobal(".utility-drawer-item a",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.ColumnGap(gwccss.Px(10)),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.Raw("border", "1px solid transparent"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.TextColor(gwccss.Var("ink")), gwccss.Raw("text-decoration", "none"),
		hoverRule(
			gwccss.Raw("border-color", "color-mix(in srgb,var(--accent) 20%,transparent)"),
			gwccss.Bg(gwccss.Var("hcm-hover-surface")),
		),
	)
	declareGlobal(".utility-drawer-item a>svg", gwccss.Raw("flex", "none"))
	declareGlobal(".utility-drawer-item-text",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(2)), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".utility-drawer-item-label",
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("line-height", "1.3"),
	)
	declareGlobal(".utility-drawer-item-description",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("line-height", "1.35"),
	)
	// On a phone the launcher's popover pins to the viewport rather than to
	// its trigger, which may be a 44px icon at the edge of the bar. The drawer
	// is the same popover and takes the same rule.
	declareGlobal(".utility-drawer-dialog",
		mediaRule(gwccss.MaxW(760),
			gwccss.Position.Fixed,
			gwccss.Raw("inset-block-start", "68px"),
			gwccss.Raw("inset-inline", "12px"),
			gwccss.W(gwccss.RawLength("auto")),
			gwccss.MaxHeight(gwccss.RawLength("calc(100dvh - 80px)")),
		),
	)
	// Icon-only between phone and desktop, like the launcher beside it.
	declareGlobal(".utility-drawer-trigger .utility-drawer-label",
		mediaRule(gwccss.RawMedia("(min-width:431px) and (max-width:1050px)"), gwccss.Display.None),
	)
	declareGlobal(".utility-drawer-trigger",
		mediaRule(gwccss.RawMedia("(min-width:431px) and (max-width:1050px)"), gwccss.W(gwccss.Px(44)), gwccss.Padding(gwccss.Zero), gwccss.Raw("justify-content", "center")),
	)
	declareGlobal(".utility-drawer-trigger,.utility-drawer-dialog,.utility-drawer-close",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"),
			gwccss.Raw("border-color", "CanvasText"), gwccss.Raw("background", "Canvas"), gwccss.Raw("color", "CanvasText"),
		),
	)
}

// MobileShellStylesheet owns the narrow-viewport collapse of the topbar
// tool triggers. Labels hide visually at phone widths while the triggers
// keep their accessible names and touch targets; logical properties keep
// the collapse RTL-safe.
func FederationEntryStylesheet() string {
	return buildTypedSheet(declareFederationEntryStyles)
}

func declareFederationEntryStyles() {
	declareGlobal(".federation-entry",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(12)),
		gwccss.Raw("padding-block", "24px"),
		gwccss.MaxWidth(gwccss.Px(640)),
	)
	declareGlobal(".federation-entry-description",
		gwccss.Margin(gwccss.Zero),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".federation-entry-group",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".federation-entry-tenant",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)), gwccss.FontWeight.Semibold,
	)
	declareGlobal(".federation-entry-list",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)),
		gwccss.Raw("list-style", "none"),
		gwccss.Margin(gwccss.Zero), gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(".federation-entry-item a",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(2)),
		gwccss.PaddingY(gwccss.Px(12)), gwccss.PaddingX(gwccss.Px(14)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")), gwccss.Raw("text-decoration", "none"),
		hoverRule(
			gwccss.Raw("border-color", "var(--hcm-hover-border,var(--accent))"),
			gwccss.TextColor(gwccss.Var("accent")),
		),
	)
	declareGlobal(".federation-entry-issuer", gwccss.FontWeight.Semibold, gwccss.Raw("overflow-wrap", "anywhere"))
	declareGlobal(".federation-entry-meta",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".federation-entry-empty",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".federation-entry-recovery",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)),
		gwccss.Raw("margin-block-start", "12px"),
	)
	declareGlobal(".federation-entry-recovery-list",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)),
		gwccss.Raw("list-style", "none"),
		gwccss.Margin(gwccss.Zero), gwccss.Padding(gwccss.Zero),
	)
}

func SessionWarningStylesheet() string {
	return buildTypedSheet(declareSessionWarningStyles)
}

func declareSessionWarningStyles() {
	declareGlobal(".session-warning",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(6)),
		gwccss.PaddingY(gwccss.Px(12)), gwccss.PaddingX(gwccss.Px(16)),
		gwccss.Raw("border-block-end", "1px solid var(--control-border,var(--line))"),
		gwccss.Raw("background", "var(--surface-subtle,var(--canvas))"),
	)
	declareGlobal(".session-warning-title",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)), gwccss.FontWeight.Semibold,
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".session-warning-detail",
		gwccss.Margin(gwccss.Zero),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".session-warning-actions",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.ColumnGap(gwccss.Px(12)),
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal(".session-warning-reauth",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.MinHeight(gwccss.Px(44)),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.TextUnderlineOffset(gwccss.Px(2)),
		hoverRule(gwccss.Raw("text-decoration", "underline")),
	)
	declareGlobal(".session-warning-dismiss",
		gwccss.Display.InlineFlex, gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(44)), gwccss.PaddingY(gwccss.Px(6)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
	)
}

func AuthorityBannerStylesheet() string {
	return buildTypedSheet(declareAuthorityBannerStyles)
}

func declareAuthorityBannerStyles() {
	declareGlobal(".acting-authority-banner",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(2)),
		gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(16)),
		gwccss.Raw("border-block-end", "1px solid var(--control-border,var(--line))"),
		gwccss.Raw("background", "var(--surface-subtle,var(--canvas))"),
	)
	declareGlobal(".acting-authority-title",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.75)), gwccss.FontWeight.Semibold,
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".acting-authority-detail",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.TextColor(gwccss.Var("ink")),
	)
}

func DelegationSelectorStylesheet() string {
	return buildTypedSheet(declareDelegationSelectorStyles)
}

func declareDelegationSelectorStyles() {
	declareGlobal(".delegation-selector",
		gwccss.Display.InlineFlex, gwccss.Items.Center,
	)
	declareGlobal(".delegation-selector-summary",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(7)),
		gwccss.MinHeight(gwccss.Px(44)), gwccss.PaddingY(gwccss.Px(7)), gwccss.PaddingX(gwccss.Px(11)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(0.75)), gwccss.FontWeight.Bold,
	)
	declareGlobal(".delegation-selector-chevron",
		gwccss.W(gwccss.Px(16)), gwccss.H(gwccss.Px(16)),
		gwccss.Raw("flex", "none"),
		gwccss.Transform(gwccss.Rotate(gwccss.Deg(90))),
	)
	declareGlobal(".delegation-selector[open] .delegation-selector-chevron",
		gwccss.Transform(gwccss.Rotate(gwccss.Deg(-90))),
	)
	declareGlobal(".delegation-selector-options",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(4)),
		gwccss.Raw("list-style", "none"),
		gwccss.Margin(gwccss.Zero), gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(".delegation-selector-status",
		gwccss.Margin(gwccss.Zero),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
}

func StepUpStylesheet() string {
	return buildTypedSheet(declareStepUpStyles)
}

func declareStepUpStyles() {
	declareGlobal(".step-up-challenge",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(6)),
		gwccss.PaddingY(gwccss.Px(12)), gwccss.PaddingX(gwccss.Px(16)),
		gwccss.Raw("border-block-end", "1px solid var(--control-border,var(--line))"),
		gwccss.Raw("background", "var(--surface-subtle,var(--canvas))"),
	)
	declareGlobal(".step-up-title",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)), gwccss.FontWeight.Semibold,
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".step-up-action",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontWeight.Semibold,
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".step-up-reason",
		gwccss.Margin(gwccss.Zero),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".step-up-actions",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.ColumnGap(gwccss.Px(12)),
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal("a.step-up-challenge",
		gwccss.Display.InlineFlex, gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(44)), gwccss.PaddingY(gwccss.Px(6)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("accent")), gwccss.TextColor(gwccss.Var("on-brand")),
		gwccss.Raw("text-decoration", "none"), gwccss.FontWeight.Semibold,
	)
	declareGlobal(".step-up-dismiss",
		gwccss.Display.InlineFlex, gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(44)), gwccss.PaddingY(gwccss.Px(6)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
	)
}

func BreakGlassStylesheet() string {
	return buildTypedSheet(declareBreakGlassStyles)
}

func declareBreakGlassStyles() {
	declareGlobal(".break-glass-activation",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(6)),
		gwccss.PaddingY(gwccss.Px(12)), gwccss.PaddingX(gwccss.Px(16)),
		gwccss.Raw("border-block-end", "1px solid var(--control-border,var(--line))"),
		gwccss.Raw("background", "var(--surface-subtle,var(--canvas))"),
	)
	declareGlobal(".break-glass-title",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)), gwccss.FontWeight.Semibold,
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".break-glass-row",
		gwccss.Display.Flex, gwccss.Items.Baseline, gwccss.ColumnGap(gwccss.Px(8)),
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal(".break-glass-label",
		gwccss.FontWeight.Semibold,
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".break-glass-value",
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".break-glass-detail",
		gwccss.Margin(gwccss.Zero),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".break-glass-capabilities",
		gwccss.Display.Flex, gwccss.ColumnGap(gwccss.Px(8)),
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("list-style", "none"),
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(".break-glass-capability",
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.PaddingY(gwccss.Px(2)), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".break-glass-actions",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.ColumnGap(gwccss.Px(12)),
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal("a.break-glass-activate",
		gwccss.Display.InlineFlex, gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(44)), gwccss.PaddingY(gwccss.Px(6)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("accent")), gwccss.TextColor(gwccss.Var("on-brand")),
		gwccss.Raw("text-decoration", "none"), gwccss.FontWeight.Semibold,
	)
	declareGlobal(".break-glass-dismiss",
		gwccss.Display.InlineFlex, gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(44)), gwccss.PaddingY(gwccss.Px(6)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
	)
}

func PolicySimulationStylesheet() string {
	return buildTypedSheet(declarePolicySimulationStyles)
}

func declarePolicySimulationStyles() {
	declareGlobal(".policy-simulation",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(6)),
		gwccss.PaddingY(gwccss.Px(12)), gwccss.PaddingX(gwccss.Px(16)),
		gwccss.Raw("border-block-end", "1px solid var(--control-border,var(--line))"),
		gwccss.Raw("background", "var(--surface-subtle,var(--canvas))"),
	)
	declareGlobal(".policy-simulation-title",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)), gwccss.FontWeight.Semibold,
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".policy-simulation-notice",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontWeight.Semibold,
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".policy-simulation-row",
		gwccss.Display.Flex, gwccss.Items.Baseline, gwccss.ColumnGap(gwccss.Px(8)),
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal(".policy-simulation-label",
		gwccss.FontWeight.Semibold,
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".policy-simulation-value",
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".policy-simulation-detail",
		gwccss.Margin(gwccss.Zero),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".policy-simulation-rules",
		gwccss.Display.Flex, gwccss.ColumnGap(gwccss.Px(8)),
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("list-style", "none"),
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(".policy-simulation-rule",
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.PaddingY(gwccss.Px(2)), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".policy-simulation-actions",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.ColumnGap(gwccss.Px(12)),
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal("a.policy-simulation-exit",
		gwccss.Display.InlineFlex, gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(44)), gwccss.PaddingY(gwccss.Px(6)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("text-decoration", "none"), gwccss.FontWeight.Semibold,
	)
}

func SignedOutStylesheet() string {
	return buildTypedSheet(declareSignedOutStyles)
}

func declareSignedOutStyles() {
	declareGlobal(".signed-out",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)),
		gwccss.PaddingY(gwccss.Px(16)), gwccss.PaddingX(gwccss.Px(16)),
	)
	declareGlobal(".signed-out-title",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(1.125)), gwccss.FontWeight.Semibold,
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".signed-out-detail",
		gwccss.Margin(gwccss.Zero),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".signed-out-revoked",
		gwccss.Display.Flex, gwccss.ColumnGap(gwccss.Px(8)),
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("list-style", "none"),
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(".signed-out-grant",
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.PaddingY(gwccss.Px(2)), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".signed-out-actions",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.ColumnGap(gwccss.Px(12)),
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal("a.signed-out-signin",
		gwccss.Display.InlineFlex, gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(44)), gwccss.PaddingY(gwccss.Px(6)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("accent")), gwccss.TextColor(gwccss.Var("on-brand")),
		gwccss.Raw("text-decoration", "none"), gwccss.FontWeight.Semibold,
	)
}

func MobileShellStylesheet() string {
	return buildTypedSheet(declareMobileShellStyles)
}

func declareMobileShellStyles() {
	// A phone header cannot preserve the desktop history pair and two full
	// text inputs without clipping the higher-value controls. History remains
	// available through the browser gesture/menu, while search becomes a
	// compact glyph that expands into a full-width field on focus. Keeping the
	// tools overflow visible also lets fixed popovers escape the header row.
	declareGlobal(".history-navigation",
		mediaRule(gwccss.MaxW(430), gwccss.Display.None),
	)
	declareGlobal(".topbar>.header-navigation-tools",
		mediaRule(gwccss.MaxW(430), gwccss.Raw("overflow", "visible")),
	)
	declareGlobal(".topbar,.app-shell.nav-collapsed .topbar",
		mediaRule(gwccss.MaxW(430), gwccss.Raw("grid-template-columns", "82px minmax(0,1fr) auto auto auto"), gwccss.Gap(gwccss.Px(6)), gwccss.Raw("padding-inline", "8px")),
	)
	declareGlobal(".topbar>.locale-menu",
		mediaRule(gwccss.MaxW(430), gwccss.Display.None),
	)
	// A 120px tablet identity slot cannot fit both the mark and a long
	// customer name. Show the mark instead of a two-letter clipped fragment;
	// BrandLogo retains its screen-reader name and the link retains its title.
	declareGlobal(".brand-cluster .brand-logo-slot[data-hcm-brand-logo-state=\"fallback\"] .wordmark-label",
		mediaRule(gwccss.MaxW(760), gwccss.Display.None),
	)
	declareGlobal(".header-navigation-tools>.global-search",
		mediaRule(gwccss.MaxW(430), gwccss.Raw("flex", "0 0 44px"), gwccss.W(gwccss.Px(44)), gwccss.Padding(gwccss.Zero)),
	)
	declareGlobal(".global-search .global-search-input",
		mediaRule(gwccss.MaxW(430), gwccss.W(gwccss.Px(44)), gwccss.MinHeight(gwccss.Px(44)), gwccss.Padding(gwccss.Zero), gwccss.Raw("color", "transparent"), gwccss.Raw("cursor", "pointer")),
	)
	declareGlobal(".global-search .global-search-input::placeholder",
		mediaRule(gwccss.MaxW(430), gwccss.TextColor(gwccss.Color("transparent"))),
	)
	declareGlobal(".global-search-glyph",
		mediaRule(gwccss.MaxW(430), gwccss.Raw("inset-inline-start", "14px")),
	)
	declareGlobal(".header-navigation-tools>.global-search:focus-within",
		mediaRule(gwccss.MaxW(430), gwccss.Position.Fixed, gwccss.Raw("inset-block-start", "68px"), gwccss.Raw("inset-inline", "12px"), gwccss.W(gwccss.RawLength("auto")), gwccss.ZIndex(90)),
	)
	declareGlobal(".global-search:focus-within .global-search-input",
		mediaRule(gwccss.MaxW(430), gwccss.W(gwccss.Percent(100)), gwccss.MinHeight(gwccss.Px(46)), gwccss.Raw("padding-inline", "42px 14px"), gwccss.TextColor(gwccss.Var("ink")), gwccss.Raw("cursor", "text")),
	)
	declareGlobal(".global-search:focus-within .global-search-input::placeholder",
		mediaRule(gwccss.MaxW(430), gwccss.TextColor(gwccss.Var("muted"))),
	)
	declareGlobal(".global-search:focus-within .global-search-glyph",
		mediaRule(gwccss.MaxW(430), gwccss.Raw("inset-inline-start", "15px")),
	)
	declareGlobal(".action-launcher-trigger,.utility-drawer-trigger",
		mediaRule(gwccss.MaxW(430), gwccss.W(gwccss.Px(44)), gwccss.H(gwccss.Px(44)), gwccss.MinHeight(gwccss.Px(44)), gwccss.Padding(gwccss.Zero), gwccss.Raw("justify-content", "center")),
	)
	// Collapsed to glyphs, search and the header actions are icon buttons,
	// drawn like the menu, notification and profile icons beside them: no
	// field border at rest, a tint on hover. Expanded, search is a field
	// again and takes its border back (the :focus-within rules above).
	declareGlobal(".global-search:not(:focus-within) .global-search-input,.action-launcher-trigger,.utility-drawer-trigger",
		mediaRule(gwccss.MaxW(430), gwccss.Raw("border-color", "transparent"), gwccss.Raw("background", "transparent")),
	)
	declareGlobal(".global-search:not(:focus-within) .global-search-input:hover,.action-launcher-trigger:hover,.utility-drawer-trigger:hover",
		mediaRule(gwccss.MaxW(430), gwccss.Raw("background", "var(--hcm-hover-surface)")),
	)
	declareGlobal(".action-launcher-trigger .action-launcher-label",
		mediaRule(gwccss.MaxW(430), gwccss.Display.None),
	)
	declareGlobal(".utility-drawer-trigger .utility-drawer-label",
		mediaRule(gwccss.MaxW(430), gwccss.Display.None),
	)
	// Below 360px the search and action glyphs cannot share a row with the
	// brand, notification and viewer controls without one covering another.
	declareGlobal(".topbar,.app-shell.nav-collapsed .topbar",
		mediaRule(gwccss.MaxW(350),
			gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto"))),
			gwccss.GridRows(gwccss.TrackLen(gwccss.Px(44)), gwccss.TrackLen(gwccss.Px(44))),
			gwccss.Gap(gwccss.Px(6)), gwccss.PaddingY(gwccss.Px(4)), gwccss.PaddingX(gwccss.Px(8)),
		),
	)
	declareGlobal(".topbar>.brand-cluster",
		mediaRule(gwccss.MaxW(350), gwccss.GridColumn(gwccss.GridLineAt(1)), gwccss.GridRow(gwccss.GridLineAt(1))),
	)
	declareGlobal(".topbar>.header-navigation-tools",
		mediaRule(gwccss.MaxW(350),
			gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
			gwccss.GridRow(gwccss.GridLineAt(2)), gwccss.W(gwccss.Percent(100)), gwccss.Padding(gwccss.Zero),
		),
	)
	declareGlobal(".topbar>.notifications",
		mediaRule(gwccss.MaxW(350), gwccss.GridColumn(gwccss.GridLineAt(2)), gwccss.GridRow(gwccss.GridLineAt(1))),
	)
	declareGlobal(".topbar>.viewer-profile-link",
		mediaRule(gwccss.MaxW(350), gwccss.GridColumn(gwccss.GridLineAt(3)), gwccss.GridRow(gwccss.GridLineAt(1))),
	)
}

// textFieldFocusSelector matches every text-entry control in the product
// shell -- not checkboxes, radios, ranges or buttons, which keep the ring.
// The :not() list lifts it to (0,2,1), above the generic control ring.
const textFieldFocusSelector = ":where(.app-shell,.jn-embedded) :is(input:not([type=checkbox],[type=radio],[type=range],[type=color],[type=file],[type=button],[type=submit],[type=reset]),select,textarea):focus-visible"
