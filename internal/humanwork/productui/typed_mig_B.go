package productui

import (
	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// Typed builders for migrated string-CSS consts (owner batch B).
// NOTE: @starting-style and @supports have no typed constructor in the GWC
// css API, so those two blocks wrap typed declarations via the atRule helper
// in typed_sheet.go until upstream support lands.
func networkTransitionStylesStylesheet() string {
	return buildTypedSheet(declarenetworkTransitionStylesPre) +
		startingStyleNetworkReady() +
		buildTypedSheet(declarenetworkTransitionStylesPost)
}

// startingStyleNetworkReady restores the @starting-style entry state for the
// network stage. The inner declarations are typed; only the header keyword is
// literal until the GWC css API gains an at-rule constructor.
func startingStyleNetworkReady() string {
	return atRule("@starting-style", buildTypedSheet(func() {
		declareGlobal(".network-stage-ready",
			gwccss.OpacityNum(gwccss.Num(0.94)),
			gwccss.Transform(gwccss.TranslateY(gwccss.Px(2))),
		)
	}))
}

func declarenetworkTransitionStylesPre() {
	declareGlobal(".network-stage",
		gwccss.OpacityNum(gwccss.Num(1)),
		gwccss.Raw("transform", "none"),
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("opacity"), gwccss.Prop("transform")), gwccss.VarDuration("hcm-motion-normal"), gwccss.Easing("var(--hcm-motion-easing)")),
	)
	declareGlobal(".network-stage-refreshing",
		gwccss.OpacityNum(gwccss.Num(.985)),
		gwccss.Transform(gwccss.TranslateY(gwccss.Px(1))),
	)
	declareGlobal(".network-stage>.page-head,.network-stage>.home-grid,.network-stage>.workbench,.network-stage>.people-page,.network-stage>.person-page,.network-stage>.organization-page,.network-stage>.insights-grid,.network-stage>.admin-grid,.network-stage>.studio-page,.network-stage>.jn-embedded,.network-stage :where(.work-row,.people-row,.history-row,.status,.count),.network-stage .jn-embedded .jn-griditem",
		gwccss.Raw("animation", "none"),
	)
	declareGlobal(".network-slot",
		gwccss.OpacityNum(gwccss.Num(1)),
		gwccss.Raw("transform", "none"),
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("border-color"), gwccss.Prop("background-color")), gwccss.VarDuration("hcm-motion-fast"), gwccss.Easing("var(--hcm-motion-easing)")),
	)
	declareGlobal(".network-slot-pending",
		gwccss.OpacityNum(gwccss.Num(.72)),
	)
	declareGlobal(".loading-viewer-profile",
		gwccss.W(gwccss.Px(40)),
		gwccss.H(gwccss.Px(40)),
		gwccss.Rounded(gwccss.Percent(50)),
	)
	declareGlobal(".network-progress",
		gwccss.Raw("pointer-events", "none"),
	)
	declareGlobal(".app-shell.is-refreshing .network-progress:after",
		gwccss.Keyframes("hcm-route-progress",
			gwccss.At("0%", gwccss.Transform(gwccss.TranslateX(gwccss.Percent(-110)))),
			gwccss.At("100%", gwccss.Transform(gwccss.TranslateX(gwccss.Percent(365)))),
		),
		gwccss.Animation(gwccss.S(1.15), gwccss.Easing("var(--hcm-motion-easing)")),
		gwccss.Raw("animation-iteration-count", "infinite"),
	)
	declareGlobal(".people-directory.is-refreshing",
		gwccss.Position.Relative,
	)
	declareGlobal(".people-directory-progress",
		gwccss.Position.Absolute,
		gwccss.ZIndex(8),
		gwccss.Raw("inset", "0 0 auto"),
		gwccss.H(gwccss.Px(3)),
		gwccss.Raw("pointer-events", "none"),
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".people-directory.is-refreshing .people-directory-progress:after",
		gwccss.Keyframes("hcm-route-progress",
			gwccss.At("0%", gwccss.Transform(gwccss.TranslateX(gwccss.Percent(-110)))),
			gwccss.At("100%", gwccss.Transform(gwccss.TranslateX(gwccss.Percent(365)))),
		),
		gwccss.Animation(gwccss.S(1.15), gwccss.Easing("var(--hcm-motion-easing)")),
		gwccss.Raw("animation-iteration-count", "infinite"),
	)
}

func declarenetworkTransitionStylesPost() {
	declareGlobal(":root[data-hcm-motion-preference=\"limited\"] .network-stage-refreshing",
		gwccss.OpacityNum(gwccss.Num(.995)),
		gwccss.Raw("transform", "none"),
	)
	declareGlobal(":root[data-hcm-motion-preference=\"limited\"] .network-slot-pending",
		gwccss.OpacityNum(gwccss.Num(.86)),
	)
	declareGlobal(".network-stage,.network-slot",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.OpacityNum(gwccss.Num(1)), gwccss.Raw("transform", "none"), gwccss.Raw("transition", "none")),
	)
	declareGlobal(".app-shell.is-refreshing .network-progress:after,.people-directory.is-refreshing .people-directory-progress:after",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("animation", "none")),
	)
	declareGlobal(".network-stage-refreshing,.network-slot-pending",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.OpacityNum(gwccss.Num(1))),
	)
	declareGlobal(".loading-viewer-profile",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Border(gwccss.Px(1), gwccss.Color("CanvasText"))),
	)
	declareGlobal(".people-directory-progress",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderTop(gwccss.Px(2), gwccss.Color("Highlight"))),
	)
}
func navigationInteractionRefinementsStylesheet() string {
	return buildTypedSheet(declarenavigationInteractionRefinementsStyles)
}

func declarenavigationInteractionRefinementsStyles() {
	declareGlobal(".sidebar",
		mediaRule(gwccss.MinW(761), gwccss.CustomLength("hcm-nav-content-inset", gwccss.Px(14)), gwccss.CustomLength("hcm-nav-rail-shift", gwccss.Px(10)), gwccss.Raw("overflow", "hidden"), gwccss.Raw("padding-inline-end", "0"), gwccss.Raw("padding-right", "0")),
	)
	declareGlobal(".sidebar.collapsed",
		mediaRule(gwccss.MinW(761), gwccss.CustomLength("hcm-nav-content-inset", gwccss.Px(9)), gwccss.CustomLength("hcm-nav-rail-shift", gwccss.Px(7)), gwccss.Raw("padding-inline-end", "0"), gwccss.Raw("padding-right", "0")),
	)
	declareGlobal(".menu-filter",
		mediaRule(gwccss.MinW(761), gwccss.Raw("margin-inline-end", "calc(5px + var(--hcm-nav-content-inset))")),
	)
	declareGlobal(".primary-nav",
		mediaRule(gwccss.MinW(761), gwccss.Raw("flex", "1 1 auto"), gwccss.Raw("align-self", "stretch"), gwccss.W(gwccss.RawLength("calc(100% + var(--hcm-nav-rail-shift))")), gwccss.MinWidth(gwccss.RawLength("calc(100% + var(--hcm-nav-rail-shift))")), gwccss.MaxWidth(gwccss.RawLength("none")), gwccss.Raw("margin-inline-end", "calc(-1 * var(--hcm-nav-rail-shift))"), gwccss.Raw("padding-inline-end", "0"), gwccss.Raw("scrollbar-gutter", "auto"), gwccss.Raw("scrollbar-color", "var(--hcm-nav-scrollbar-thumb) transparent"), gwccss.Raw("scroll-padding-block", "12px 24px")),
	)
	declareGlobal(".primary-nav>ul",
		gwccss.Raw("padding-block-end", "20px"),
	)
	declareGlobal(".primary-nav>ul,.nav-bottom",
		mediaRule(gwccss.MinW(761), gwccss.Raw("padding-inline-end", "var(--hcm-nav-content-inset)")),
	)
	declareGlobal(".primary-nav::-webkit-scrollbar",
		mediaRule(gwccss.MinW(761), gwccss.W(gwccss.VarLength("hcm-nav-scrollbar-size-rail"))),
	)
	declareGlobal(".primary-nav::-webkit-scrollbar-track",
		mediaRule(gwccss.MinW(761), gwccss.Bg(gwccss.Transparent)),
	)
	declareGlobal(".primary-nav::-webkit-scrollbar-thumb",
		mediaRule(gwccss.MinW(761), gwccss.MinHeight(gwccss.Px(44)), gwccss.Border(gwccss.Px(1), gwccss.Transparent), gwccss.Rounded(gwccss.Px(999)), gwccss.Bg(gwccss.Var("hcm-nav-scrollbar-thumb")), gwccss.Raw("background-clip", "padding-box")),
	)
	declareGlobal(".primary-nav .nav-entry",
		mediaRule(gwccss.MinW(761), gwccss.Display.Block),
	)
	declareGlobal(".primary-nav .nav-entry>.nav-link",
		mediaRule(gwccss.MinW(761), gwccss.Raw("padding-inline-end", "36px")),
	)
	declareGlobal(".primary-nav .nav-favorite",
		mediaRule(gwccss.MinW(761), gwccss.Position.Absolute, gwccss.Top(gwccss.Percent(50)), gwccss.Raw("inset-inline-end", "2px"), gwccss.Raw("translate", "0 -50%")),
	)
	declareGlobal(".primary-nav .nav-favorite",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (hover:hover)"), gwccss.OpacityNum(gwccss.Num(0)), gwccss.Raw("pointer-events", "none")),
	)
	declareGlobal(".primary-nav .nav-entry:hover>.nav-favorite,.primary-nav .nav-entry:focus-within>.nav-favorite,.primary-nav .nav-favorite:focus-visible",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (hover:hover)"), gwccss.OpacityNum(gwccss.Num(1)), gwccss.Raw("pointer-events", "auto")),
	)
	declareGlobal(".primary-nav",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("scrollbar-color", "ButtonText Canvas")),
	)
	declareGlobal(".primary-nav::-webkit-scrollbar-track",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Bg(gwccss.Color("Canvas"))),
	)
	declareGlobal(".primary-nav::-webkit-scrollbar-thumb",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("Canvas")), gwccss.Bg(gwccss.Color("ButtonText"))),
	)
}
func interactionThemeStylesStylesheet() string {
	return buildTypedSheet(declareinteractionThemeStylesStyles)
}

func declareinteractionThemeStylesStyles() {
	declareGlobal(":root",
		gwccss.Custom("hcm-hover-surface", "color-mix(in srgb,var(--accent) 10%,var(--surface))"),
		gwccss.Custom("hcm-hover-border", "color-mix(in srgb,var(--accent) 38%,var(--line))"),
	)
	declareGlobal(".nav-favorite",
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(":where(.app-shell) :is(.nav-link,.nav-group-summary,.nav-favorite,.menu-filter-submit,.button.secondary,.quick-actions a,.settings-nav a,.locale-option,.locale-choice,.studio-nav a,.studio-region,.people-workflow-option):hover",
		gwccss.BorderColor(gwccss.Var("hcm-hover-border")),
		gwccss.Bg(gwccss.Var("hcm-hover-surface")),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(":where(.app-shell) :is(.work-row,.people-row,.history-row,.sensitive-summary,.global-search-result):hover",
		gwccss.Bg(gwccss.Var("hcm-hover-surface")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(":where(.app-shell) :is(.appearance-choice,.choice,.accessibility-choice,.workflow-card,.metric,.org-node):hover",
		gwccss.BorderColor(gwccss.Var("hcm-hover-border")),
	)
	declareGlobal(".button.primary:hover",
		gwccss.Bg(gwccss.Var("accent-hover")),
		gwccss.TextColor(gwccss.Var("on-brand")),
	)
	declareGlobal(":where(.app-shell) :is(.nav-link,.nav-group-summary,.nav-favorite,.menu-filter-submit,.button,.quick-actions a,.settings-nav a,.locale-option,.locale-choice,.studio-nav a,.studio-region,.people-workflow-option,.work-row,.people-row,.history-row,.sensitive-summary,.global-search-result,.appearance-choice,.choice,.accessibility-choice,.workflow-card,.metric,.org-node):hover",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("Highlight")), gwccss.Bg(gwccss.Color("Highlight")), gwccss.TextColor(gwccss.Color("HighlightText"))),
	)
}
func collectionControlStylesStylesheet() string {
	return buildTypedSheet(declarecollectionControlStylesStyles)
}

func declarecollectionControlStylesStyles() {
	declareGlobal(".people-workflow-menu",
		gwccss.Position.Relative,
	)
	declareGlobal(".people-workflow-menu>summary",
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".people-workflow-menu>summary::-webkit-details-marker",
		gwccss.Display.None,
	)
	declareGlobal(".people-workflow-chevron",
		gwccss.Raw("margin-inline-start", "7px"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.W(gwccss.Px(16)), gwccss.H(gwccss.Px(16)),
		gwccss.Raw("flex", "none"),
		gwccss.Transform(gwccss.Rotate(gwccss.Deg(90))),
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("transform")), gwccss.RawDuration("var(--hcm-motion-fast,.14s)"), gwccss.Ease),
	)
	declareGlobal(".people-workflow-menu[open]>summary .people-workflow-chevron",
		gwccss.Transform(gwccss.Rotate(gwccss.Deg(-90))),
	)
	declareGlobal(".people-unavailable-menu>summary",
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".people-workflow-unavailable-reason",
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Px(8)),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.LineHeight(gwccss.Num(1.45)),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".people-workflow-option",
		gwccss.Display.Block,
		gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(11)),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".people-workflow-option:hover,.people-workflow-option:focus",
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.Raw("outline", "0"),
	)
	declareGlobal(".page-size-control",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(8)),
		gwccss.Raw("margin-inline", "auto"),
	)
	declareGlobal(".page-size-control label",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(8)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(".page-size-control select",
		gwccss.MinWidth(gwccss.Px(72)),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.Raw("padding", "6px 28px 6px 9px"),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".page-size-apply",
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.Raw("padding-block", "5px"),
	)
	declareGlobal(".people-pager",
		gwccss.Gap(gwccss.Px(14)),
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal(".workflow-history>.people-pager",
		gwccss.PaddingY(gwccss.Px(13)), gwccss.PaddingX(gwccss.Px(20)),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".page-size-control.enhanced .page-size-apply",
		mediaRule(gwccss.MinW(761), gwccss.Display.None),
	)
	declareGlobal(".people-pager",
		mediaRule(gwccss.MaxW(760), gwccss.Items.Stretch),
	)
	declareGlobal(".page-size-control",
		mediaRule(gwccss.MaxW(760), gwccss.Order(3), gwccss.W(gwccss.Percent(100)), gwccss.Margin(gwccss.Zero)),
	)
	declareGlobal(".page-size-control label",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("flex", "1")),
	)
	declareGlobal(".people-workflow-option,.page-size-control select",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Border(gwccss.Px(1), gwccss.Color("CanvasText"))),
	)
}
func navigationSearchStylesStylesheet() string {
	return buildTypedSheet(declarenavigationSearchStylesStyles)
}

func declarenavigationSearchStylesStyles() {
	declareGlobal(".nav-copy",
		gwccss.Display.Grid,
		gwccss.MinWidth(gwccss.Zero),
		gwccss.LineHeight(gwccss.Num(1.25)),
	)
	declareGlobal(".nav-copy>.nav-label",
		gwccss.Raw("overflow", "hidden"),
		gwccss.Raw("text-overflow", "ellipsis"),
	)
	declareGlobal(".nav-search-detail",
		gwccss.Raw("display", "-webkit-box"),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("margin-top", "3px"),
		gwccss.Raw("overflow", "hidden"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "400"),
		gwccss.LineHeight(gwccss.Num(1.28)),
		gwccss.Raw("-webkit-box-orient", "vertical"),
		gwccss.Raw("-webkit-line-clamp", "2"),
	)
	declareGlobal(".nav-link.has-search-detail",
		gwccss.Raw("align-items", "flex-start"),
		gwccss.MinHeight(gwccss.Px(58)),
		gwccss.Raw("padding-block", "8px"),
	)
	declareGlobal(".nav-link.has-search-detail>.nav-icon",
		gwccss.Raw("margin-top", "2px"),
	)
	declareGlobal(".nav-link.has-search-detail:hover .nav-search-detail,.nav-link.has-search-detail[aria-current=page] .nav-search-detail",
		gwccss.Raw("color", "inherit"),
	)
	declareGlobal(".subnav .nav-link.has-search-detail",
		gwccss.MinHeight(gwccss.Px(56)),
	)
	declareGlobal(".sidebar.collapsed .nav-copy",
		gwccss.Display.None,
	)
	declareGlobal(".nav-search-detail",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.TextColor(gwccss.Color("CanvasText"))),
	)
}
func peopleSortFilterStylesStylesheet() string {
	return buildTypedSheet(declarepeopleSortFilterStylesStyles)
}

func declarepeopleSortFilterStylesStyles() {
	declareGlobal(".people-filter-control",
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Px(175)), gwccss.Fr(1.25)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(132)), gwccss.Fr(.85)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(170)), gwccss.Fr(1.1)), gwccss.TrackLen(gwccss.RawLength("max-content")), gwccss.TrackLen(gwccss.RawLength("max-content"))),
		gwccss.Items.Center,
	)
	declareGlobal(".people-eligible-filter-label",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(8)),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(".people-filter input[type=checkbox]",
		gwccss.W(gwccss.Px(18)), gwccss.H(gwccss.Px(18)),
		gwccss.MinHeight(gwccss.Zero), gwccss.Padding(gwccss.Zero),
		gwccss.Raw("flex", "none"),
	)
	declareGlobal(".people-filter select",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.Raw("padding", "9px 34px 9px 11px"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("control-border")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".people-filter-actions",
		gwccss.Raw("flex-wrap", "nowrap"),
	)
	declareGlobal(".people-sort-label",
		gwccss.Display.None,
	)
	declareGlobal(".people-sort",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("font", "inherit"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".people-sort:hover,.people-sort.active",
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".people-sort.active",
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".people-filter-control",
		mediaRule(gwccss.MaxW(1120), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Px(176)), gwccss.Fr(1)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(154)), gwccss.Fr(.9)), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
	declareGlobal(".people-filter-control>input:first-child",
		mediaRule(gwccss.MaxW(1120), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1)))),
	)
	declareGlobal(".people-filter-actions",
		mediaRule(gwccss.MaxW(1120), gwccss.Raw("justify-content", "flex-start")),
	)
	declareGlobal(".people-filter-control",
		mediaRule(gwccss.MaxW(900), gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1))),
	)
	declareGlobal(".people-filter-actions",
		mediaRule(gwccss.MaxW(900), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1)))),
	)
	declareGlobal(".people-filter-control",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".people-filter-control>input:first-child,.people-filter-actions",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(1))),
	)
	declareGlobal(".people-filter-actions",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Grid, gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1))),
	)
	declareGlobal(".people-directory .people-columns",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(8)), gwccss.Raw("overflow-x", "auto"), gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(12))),
	)
	declareGlobal(".people-sort-label",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Block, gwccss.Raw("flex", "none"), gwccss.TextColor(gwccss.Var("muted")), gwccss.FontSize(gwccss.Rem(0.75)), gwccss.Raw("font-weight", "700")),
	)
	declareGlobal(".people-columns>span",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("flex", "none")),
	)
	declareGlobal(".people-sort",
		mediaRule(gwccss.MaxW(760), gwccss.MinHeight(gwccss.Px(44)), gwccss.PaddingY(gwccss.Zero), gwccss.PaddingX(gwccss.Px(10)), gwccss.Border(gwccss.Px(1), gwccss.Var("line")), gwccss.Rounded(gwccss.VarLength("hcm-radius-control")), gwccss.Bg(gwccss.Var("surface"))),
	)
	declareGlobal(".people-sort.active",
		mediaRule(gwccss.MaxW(760), gwccss.BorderColor(gwccss.Var("accent")), gwccss.Bg(gwccss.Var("soft"))),
	)
	declareGlobal(".people-sort.active",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("outline", "1px solid Highlight"), gwccss.OutlineOffset(gwccss.Px(2))),
	)
}
func peopleQuickActionStylesStylesheet() string {
	return buildTypedSheet(declarepeopleQuickActionStylesStyles)
}

func declarepeopleQuickActionStylesStyles() {
	declareGlobal(".people-columns,.people-row",
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Px(145)), gwccss.Fr(1.3)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(150)), gwccss.Fr(1.15)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(120)), gwccss.Fr(1)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(110)), gwccss.Fr(.9)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(120)), gwccss.Fr(.85)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(92)), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
	declareGlobal(".people-person-link",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("color", "inherit"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".people-person-link:hover strong",
		gwccss.Raw("text-decoration", "underline"),
		gwccss.TextUnderlineOffset(gwccss.Ems(.16)),
	)
	declareGlobal(".people-action-heading",
		gwccss.Raw("text-align", "right"),
	)
	declareGlobal(".people-row-actions",
		gwccss.Display.Flex,
		gwccss.Raw("justify-content", "flex-end"),
		gwccss.Gap(gwccss.Px(6)),
	)
	declareGlobal(".people-row-action",
		gwccss.MinHeight(gwccss.Px(40)),
		gwccss.PaddingY(gwccss.Px(7)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(".people-directory .people-columns",
		mediaRule(gwccss.MaxW(1050), gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(8)), gwccss.Raw("overflow-x", "auto"), gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(12))),
	)
	declareGlobal(".people-sort-label",
		mediaRule(gwccss.MaxW(1050), gwccss.Display.Block, gwccss.Raw("flex", "none"), gwccss.TextColor(gwccss.Var("muted")), gwccss.FontSize(gwccss.Rem(0.75)), gwccss.Raw("font-weight", "700")),
	)
	declareGlobal(".people-columns>span",
		mediaRule(gwccss.MaxW(1050), gwccss.Raw("flex", "none")),
	)
	declareGlobal(".people-action-heading",
		mediaRule(gwccss.MaxW(1050), gwccss.Display.None),
	)
	declareGlobal(".people-sort",
		mediaRule(gwccss.MaxW(1050), gwccss.MinHeight(gwccss.Px(44)), gwccss.PaddingY(gwccss.Zero), gwccss.PaddingX(gwccss.Px(10)), gwccss.Border(gwccss.Px(1), gwccss.Var("line")), gwccss.Rounded(gwccss.VarLength("hcm-radius-control")), gwccss.Bg(gwccss.Var("surface"))),
	)
	declareGlobal(".people-sort.active",
		mediaRule(gwccss.MaxW(1050), gwccss.BorderColor(gwccss.Var("accent")), gwccss.Bg(gwccss.Var("soft"))),
	)
	declareGlobal(".people-row",
		mediaRule(gwccss.MaxW(1050), gwccss.Raw("grid-template-columns", "1fr 1fr!important"), gwccss.Gap(gwccss.Px(8)), gwccss.Raw("padding-block", "14px")),
	)
	declareGlobal(".people-row>.person-cell",
		mediaRule(gwccss.MaxW(1050), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1)))),
	)
	declareGlobal(".people-row>.people-cell",
		mediaRule(gwccss.MaxW(1050), gwccss.Raw("display", "flex!important"), gwccss.Raw("grid-column", "auto!important"), gwccss.Raw("justify-content", "space-between"), gwccss.Gap(gwccss.Px(18)), gwccss.MinWidth(gwccss.Zero)),
	)
	declareGlobal(".people-row>.people-cell:before",
		mediaRule(gwccss.MaxW(1050), gwccss.Raw("content", "attr(data-label)"), gwccss.Raw("flex", "none"), gwccss.TextColor(gwccss.Var("muted")), gwccss.FontSize(gwccss.Rem(0.75)), gwccss.Raw("font-weight", "600")),
	)
	declareGlobal(".people-row-actions",
		mediaRule(gwccss.MaxW(1050), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1)))),
	)
	declareGlobal(".people-row",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("grid-template-columns", "1fr!important")),
	)
	declareGlobal(".people-row>.people-cell",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("grid-column", "1!important")),
	)
	declareGlobal(".people-row-actions",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("justify-content", "stretch")),
	)
	declareGlobal(".people-row-action",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("flex", "1")),
	)
}
func responsiveComponentStylesStylesheet() string {
	return buildTypedSheet(declareresponsiveComponentStylesStyles)
}

func declareresponsiveComponentStylesStyles() {
	declareGlobal("img,svg,video,canvas",
		gwccss.MaxWidth(gwccss.Percent(100)),
	)
	declareGlobal(":where(.main,.page-stack,.surface,.panel,.section-head,.home-grid,.side-stack,.workbench,.work-list,.work-preview,.row-main,.people-page,.people-directory,.people-row,.people-cell,.person-page,.person-layout,.person-details,.workflow-launcher,.workflow-card,.workflow-copy,.history-row,.history-record,.settings-page-stack,.settings-overview-grid,.settings-shell,.settings-form,.settings-context,.admin-grid,.admin-card,.insights-grid,.org,.org-branches,.appearance-shell,.appearance-controls,.appearance-preview,.studio-shell,.studio-canvas,.jn-embedded,.jn-embedded .jn-shell,.jn-embedded .jn-page,.jn-embedded .jn-card,.jn-embedded .jn-griditem)",
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(":where(input,select,textarea,button)",
		gwccss.MaxWidth(gwccss.Percent(100)),
	)
	declareGlobal(":where(.row-main,.workflow-copy,.history-record,.profile-fact,.admin-card,.settings-context,.jn-embedded .jn-card)",
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".section-head,.panel-foot,.directory-tools,.toolbar",
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal(".tabs",
		gwccss.MaxWidth(gwccss.Percent(100)),
		gwccss.Raw("overflow-x", "auto"),
		gwccss.Raw("overscroll-behavior-inline", "contain"),
		gwccss.Raw("scrollbar-width", "thin"),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(".tabs>.tab",
		gwccss.Raw("flex", "none"),
	)
	declareGlobal(".notifications .popover,.locale-menu .popover",
		gwccss.MaxWidth(gwccss.RawLength("calc(100vw - 24px)")),
	)
	declareGlobal(".page-head",
		mediaRule(gwccss.MaxW(1050), gwccss.Raw("flex-wrap", "wrap")),
	)
	declareGlobal(".section-head",
		mediaRule(gwccss.MaxW(1050), gwccss.Raw("align-items", "flex-start")),
	)
	declareGlobal(".admin-card",
		mediaRule(gwccss.MaxW(1050), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".admin-card>a,.admin-card>.button",
		mediaRule(gwccss.MaxW(1050), gwccss.Raw("justify-self", "start")),
	)
	declareGlobal(".coverage-strip",
		mediaRule(gwccss.MaxW(1050), gwccss.Raw("flex-wrap", "wrap")),
	)
	declareGlobal(".jn-embedded :where(.jn-pagehead,.jn-cardhead,.jn-toolbar,.jn-actions)",
		mediaRule(gwccss.MaxW(1050), gwccss.Raw("flex-wrap", "wrap")),
	)
	// UXAUDIT-001: the topbar keeps every one of its five children (brand,
	// tools, locale, notifications, profile) in one grid row at every
	// narrow width — nothing here spans a second row — so the header never
	// wraps. Brand shrinks to a compact identity slot and the tools cluster
	// is the one flexible track, scrolling its own contents horizontally
	// (declared alongside .header-navigation-tools below) instead of
	// wrapping vertically when it does not fit.
	declareGlobal(".topbar,.app-shell.nav-collapsed .topbar",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.TrackLen(gwccss.Px(120))), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
	declareGlobal(".brand-cluster,.app-shell.nav-collapsed .brand-cluster",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("display", "grid!important"), gwccss.Raw("grid-template-columns", "minmax(0,1fr) 44px!important"), gwccss.Items.Center, gwccss.H(gwccss.Px(65)), gwccss.Padding(gwccss.Zero), gwccss.Raw("border-right", "0")),
	)
	declareGlobal(".brand-cluster .wordmark,.app-shell.nav-collapsed .brand-cluster .wordmark",
		mediaRule(gwccss.MaxW(760), gwccss.MinWidth(gwccss.Zero), gwccss.H(gwccss.Px(65)), gwccss.Raw("padding", "0 4px 0 12px")),
	)
	declareGlobal(".brand-logo-slot",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("overflow", "hidden")),
	)
	declareGlobal(".brand-logo-image",
		mediaRule(gwccss.MaxW(760), gwccss.MaxWidth(gwccss.Percent(100)), gwccss.H(gwccss.Px(34))),
	)
	declareGlobal(".header-nav-toggle,.app-shell.nav-collapsed .header-nav-toggle",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Grid, gwccss.W(gwccss.Px(44)), gwccss.H(gwccss.Px(44)), gwccss.Raw("margin", "0 4px 0 0")),
	)
	// The persistent desktop icon-rail toggle and the narrow-viewport
	// overlay drawer trigger are two affordances for the one navigation
	// model, never both reachable at once: exactly one is display:none at
	// any given width, so brand-cluster's two-column grid (wordmark, one
	// 44px control) never has to size a third visible item.
	declareGlobal(".header-nav-toggle,.app-shell.nav-collapsed .header-nav-toggle",
		mediaRule(gwccss.MaxW(760), gwccss.Display.None),
	)
	declareGlobal(".nav-drawer-trigger",
		gwccss.Display.None,
	)
	declareGlobal(".nav-drawer-trigger",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Grid, gwccss.Raw("place-items", "center"), gwccss.W(gwccss.Px(44)), gwccss.H(gwccss.Px(44)), gwccss.Padding(gwccss.Zero), gwccss.Raw("margin", "0 4px 0 0"), gwccss.Raw("border", "0"), gwccss.Raw("background", "transparent"), gwccss.TextColor(gwccss.Var("ink")), gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))"))),
	)
	declareGlobal(".nav-drawer-trigger .nav-icon",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.Px(20)), gwccss.H(gwccss.Px(20))),
	)
	// The drawer owns a visible dismissal affordance on narrow viewports.
	// Keep it out of the desktop control vocabulary while preserving a 44px
	// target and logical-end placement for RTL layouts.
	declareGlobal(".nav-drawer-close",
		gwccss.Display.None,
	)
	declareGlobal(".nav-drawer-close",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Grid, gwccss.Raw("place-items", "center"), gwccss.Raw("align-self", "flex-end"), gwccss.W(gwccss.Px(44)), gwccss.H(gwccss.Px(44)), gwccss.MinHeight(gwccss.Px(44)), gwccss.Padding(gwccss.Zero), gwccss.Raw("margin-block", "0 8px"), gwccss.Raw("border", "1px solid var(--line)"), gwccss.Raw("background", "var(--surface)"), gwccss.TextColor(gwccss.Var("ink")), gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))"))),
	)
	declareGlobal(".nav-drawer-close-glyph",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.Px(20)), gwccss.H(gwccss.Px(20))),
	)
	declareGlobal(".shell-grid,.app-shell.nav-collapsed .shell-grid",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))), gwccss.GridRows(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	// UXAUDIT-001: at narrow viewports the sidebar is an off-canvas overlay
	// drawer, not an in-flow block. Collapsed no longer has a distinct
	// narrow-width meaning (a persistent icon rail makes no sense as a
	// transient overlay), so both class states share one closed shape here;
	// NavigationSidebar's own Open prop (independent of Collapsed) is the
	// only thing that ever adds nav-drawer-open. Off-canvas is the default
	// and safe: no reachable interaction on a wider viewport can set Open,
	// since the trigger that does is itself display:none outside this
	// breakpoint (see the nav-drawer-trigger and header-nav-toggle rules
	// below), so a wide-viewport render is never openable regardless of
	// Collapsed's value.
	declareGlobal(".sidebar,.sidebar.collapsed",
		mediaRule(gwccss.MaxW(760),
			gwccss.Position.Fixed,
			gwccss.Raw("inset-block", "0"),
			gwccss.Raw("inset-inline-start", "-336px"),
			gwccss.W(gwccss.MinLen(gwccss.Vw(86), gwccss.Px(320))),
			gwccss.MaxWidth(gwccss.Percent(100)),
			gwccss.H(gwccss.RawLength("100dvh")),
			gwccss.ZIndex(55),
			gwccss.Display.Flex,
			gwccss.FlexDir.Col,
			gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(14)),
			gwccss.Raw("border-right", "0"),
			gwccss.Raw("border-inline-end", "1px solid var(--line)"),
			gwccss.Raw("box-shadow", "0 18px 48px color-mix(in srgb,var(--ink) 22%,transparent)"),
			gwccss.Raw("overflow", "hidden"),
			gwccss.Raw("visibility", "hidden"),
			gwccss.Raw("transition", "inset-inline-start .22s ease"),
		),
	)
	// !important on both properties: this is the one thing that must always
	// win the moment nav-drawer-open is present, with no dependency on
	// selector-specificity bookkeeping staying correct forever. Nothing else
	// in the stylesheet is allowed to set these two properties on .sidebar
	// with !important, so there is no fight to lose.
	declareGlobal(".sidebar.nav-drawer-open,.sidebar.collapsed.nav-drawer-open",
		mediaRule(gwccss.MaxW(760),
			gwccss.Raw("inset-inline-start", "0!important"),
			gwccss.Raw("visibility", "visible!important"),
		),
	)
	declareGlobal(".sidebar,.sidebar.collapsed",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("transition", "none")),
	)
	// The backdrop dims and click-dismisses the open drawer. It is never
	// shown outside the narrow breakpoint, and never shown closed: the
	// unqualified rule is the fail-closed default, the media-scoped one
	// widens it only under both conditions the open drawer actually needs —
	// and must restate display itself, since display:none does not
	// participate in the box properties a plain override could leave alone.
	declareGlobal(".nav-drawer-backdrop",
		gwccss.Display.None,
	)
	declareGlobal(".nav-drawer-backdrop.nav-drawer-open",
		mediaRule(gwccss.MaxW(760),
			gwccss.Raw("display", "block!important"),
			gwccss.Position.Fixed,
			gwccss.Raw("inset", "0"),
			gwccss.ZIndex(54),
			gwccss.Raw("background", "color-mix(in srgb,var(--ink) 42%,transparent)"),
		),
	)
	// A closed drawer contributes no second page-level scroller: overflow
	// stays hidden until nav-drawer-open says the overlay is actually the
	// thing on screen, at which point it may scroll internally on its own.
	declareGlobal(".primary-nav,.sidebar nav:first-of-type",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("flex", "1"), gwccss.W(gwccss.Percent(100)), gwccss.MinWidth(gwccss.Zero), gwccss.MaxWidth(gwccss.Percent(100)), gwccss.Raw("overflow", "hidden")),
	)
	declareGlobal(".sidebar.nav-drawer-open .primary-nav,.sidebar.nav-drawer-open nav:first-of-type",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("overflow-x", "hidden"), gwccss.Raw("overflow-y", "auto"), gwccss.Raw("overscroll-behavior", "contain"), gwccss.Raw("scroll-padding-block", "16px"), gwccss.Raw("scrollbar-width", "thin"), gwccss.Raw("scrollbar-color", "var(--hcm-nav-scrollbar-thumb) var(--hcm-nav-scrollbar-track)"), gwccss.Raw("scrollbar-gutter", "stable")),
	)
	declareGlobal(".primary-nav>ul,.sidebar nav:first-of-type>ul",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("display", "grid!important"), gwccss.W(gwccss.RawLength("100%!important")), gwccss.MaxWidth(gwccss.RawLength("100%!important"))),
	)
	declareGlobal(".nav-link",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("white-space", "normal")),
	)
	declareGlobal(".nav-copy,.nav-label",
		mediaRule(gwccss.MaxW(760), gwccss.MaxWidth(gwccss.RawLength("none"))),
	)
	declareGlobal(".menu-filter",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("flex", "none")),
	)
	declareGlobal(".main",
		mediaRule(gwccss.MaxW(760), gwccss.PaddingY(gwccss.Px(20)), gwccss.PaddingX(gwccss.Px(16))),
	)
	declareGlobal(".page-head",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Grid, gwccss.Gap(gwccss.Px(12)), gwccss.Raw("margin-bottom", "20px")),
	)
	declareGlobal(".page-head>*",
		mediaRule(gwccss.MaxW(760), gwccss.MinWidth(gwccss.Zero)),
	)
	declareGlobal(".scope-wrap",
		mediaRule(gwccss.MaxW(760), gwccss.MaxWidth(gwccss.Percent(100)), gwccss.Raw("justify-items", "start")),
	)
	declareGlobal(".scope",
		mediaRule(gwccss.MaxW(760), gwccss.MaxWidth(gwccss.Percent(100)), gwccss.Raw("white-space", "normal")),
	)
	declareGlobal(".section-head",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("padding", "17px 17px 14px")),
	)
	declareGlobal(".section-head>*",
		mediaRule(gwccss.MaxW(760), gwccss.MinWidth(gwccss.Zero)),
	)
	declareGlobal(".panel-foot",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("align-items", "flex-start"), gwccss.Gap(gwccss.Px(10)), gwccss.Raw("padding-inline", "17px")),
	)
	declareGlobal(".panel-foot>*",
		mediaRule(gwccss.MaxW(760), gwccss.MinWidth(gwccss.Zero)),
	)
	declareGlobal(".tabs",
		mediaRule(gwccss.MaxW(760), gwccss.Gap(gwccss.Px(18)), gwccss.Raw("padding-inline", "17px")),
	)
	declareGlobal(".work-row",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Grid, gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto"))), gwccss.RowGap(gwccss.Px(6)), gwccss.ColumnGap(gwccss.Px(12)), gwccss.PaddingY(gwccss.Px(14)), gwccss.PaddingX(gwccss.Px(17))),
	)
	declareGlobal(".work-row>.avatar",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(1)), gwccss.GridRow(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(3)))),
	)
	declareGlobal(".work-row>.row-main",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(2)), gwccss.GridRow(gwccss.GridLineAt(1))),
	)
	declareGlobal(".work-row>.row-end",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(2)), gwccss.GridRow(gwccss.GridLineAt(2)), gwccss.Raw("justify-items", "start")),
	)
	declareGlobal(".work-row>.work-row-chevron",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(3)), gwccss.GridRow(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(3))), gwccss.Raw("align-self", "center")),
	)
	declareGlobal(".facts>div",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Grid, gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(.8)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1.2)))),
	)
	declareGlobal(".facts>div>strong",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("text-align", "start")),
	)
	declareGlobal(".activity",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("align-items", "flex-start"), gwccss.Raw("flex-wrap", "wrap"), gwccss.Raw("padding-inline", "17px")),
	)
	declareGlobal(".activity>.check",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("flex", "none")),
	)
	declareGlobal(".activity>small,.activity>time",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("margin-left", "44px")),
	)
	declareGlobal(".admin-card",
		mediaRule(gwccss.MaxW(760), gwccss.Padding(gwccss.Px(18))),
	)
	declareGlobal(".admin-hero",
		mediaRule(gwccss.MaxW(760), gwccss.Padding(gwccss.Px(21))),
	)
	declareGlobal(".org",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("padding-inline", "17px")),
	)
	declareGlobal(".org-node",
		mediaRule(gwccss.MaxW(760), gwccss.MinWidth(gwccss.Zero)),
	)
	declareGlobal(".coverage-strip",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("padding-inline", "17px")),
	)
	declareGlobal(".popover",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.RawLength("min(290px,calc(100vw - 24px))"))),
	)
	declareGlobal(".notifications .popover,.locale-menu .popover",
		mediaRule(gwccss.MaxW(760), gwccss.Position.Fixed, gwccss.Top(gwccss.Px(68)), gwccss.Right(gwccss.Px(12)), gwccss.Left(gwccss.RawLength("auto"))),
	)
	declareGlobal(".settings-nav,.settings-form,.settings-context",
		mediaRule(gwccss.MaxW(760), gwccss.Padding(gwccss.Px(18))),
	)
	declareGlobal(".settings-nav",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("overflow-x", "auto"), gwccss.Raw("flex-wrap", "nowrap"), gwccss.Raw("overscroll-behavior-inline", "contain")),
	)
	declareGlobal(".settings-nav>a",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("flex", "none")),
	)
	declareGlobal(".choice,.accessibility-choice",
		mediaRule(gwccss.MaxW(760), gwccss.MinWidth(gwccss.Zero)),
	)
	declareGlobal(".jn-embedded :where(.jn-shell,.jn-page,.jn-card,.jn-grid,.jn-griditem,.jn-tablewrap)",
		mediaRule(gwccss.MaxW(760), gwccss.MaxWidth(gwccss.Percent(100)), gwccss.MinWidth(gwccss.Zero)),
	)
	declareGlobal(".jn-embedded .jn-tablewrap",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("overflow-x", "auto"), gwccss.Raw("overscroll-behavior-inline", "contain")),
	)
	declareGlobal(".main",
		mediaRule(gwccss.MaxW(520), gwccss.Raw("padding-inline", "12px")),
	)
	declareGlobal(".topbar,.app-shell.nav-collapsed .topbar",
		mediaRule(gwccss.MaxW(520), gwccss.Gap(gwccss.Px(8)), gwccss.Raw("padding-inline", "10px")),
	)
	declareGlobal(".topbar>.avatar",
		mediaRule(gwccss.MaxW(520), gwccss.Display.None),
	)
	declareGlobal(".brand-cluster,.app-shell.nav-collapsed .brand-cluster",
		mediaRule(gwccss.MaxW(520), gwccss.Raw("grid-template-columns", "minmax(0,1fr) 40px!important")),
	)
	declareGlobal(".brand-cluster .wordmark,.app-shell.nav-collapsed .brand-cluster .wordmark",
		mediaRule(gwccss.MaxW(520), gwccss.Raw("padding-left", "6px")),
	)
	declareGlobal(".header-nav-toggle,.app-shell.nav-collapsed .header-nav-toggle",
		mediaRule(gwccss.MaxW(520), gwccss.W(gwccss.Px(44)), gwccss.H(gwccss.Px(44)), gwccss.Raw("margin-right", "2px")),
	)
	declareGlobal(".person-identity",
		mediaRule(gwccss.MaxW(520), gwccss.Raw("align-items", "flex-start")),
	)
	declareGlobal(".person-identity .avatar",
		mediaRule(gwccss.MaxW(520), gwccss.W(gwccss.Px(52)), gwccss.H(gwccss.Px(52))),
	)
	declareGlobal(".workflow-card",
		mediaRule(gwccss.MaxW(520), gwccss.Raw("padding-inline", "17px")),
	)
	declareGlobal(".facts>div",
		mediaRule(gwccss.MaxW(520), gwccss.GridCols(gwccss.Fr(1)), gwccss.Gap(gwccss.Px(2))),
	)
	declareGlobal(".facts>div>strong",
		mediaRule(gwccss.MaxW(520), gwccss.Raw("text-align", "start")),
	)
	declareGlobal(".activity>small,.activity>time",
		mediaRule(gwccss.MaxW(520), gwccss.W(gwccss.Percent(100)), gwccss.Raw("margin-left", "44px")),
	)
	declareGlobal(".admin-card>a,.admin-card>.button,.coverage-strip>.button,.directory-tools>.button,.toolbar>.button",
		mediaRule(gwccss.MaxW(520), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal(".work-row",
		mediaRule(gwccss.MaxW(420), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".work-row>.avatar",
		mediaRule(gwccss.MaxW(420), gwccss.Display.None),
	)
	declareGlobal(".work-row>.row-main,.work-row>.row-end",
		mediaRule(gwccss.MaxW(420), gwccss.GridColumn(gwccss.GridLineAt(1))),
	)
	declareGlobal(".activity>small,.activity>time",
		mediaRule(gwccss.MaxW(420), gwccss.Raw("margin-left", "0")),
	)
	declareGlobal(".people-row-actions",
		mediaRule(gwccss.MaxW(420), gwccss.FlexDir.Col),
	)
	declareGlobal(".people-row-action",
		mediaRule(gwccss.MaxW(420), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal("img",
		mediaRule(gwccss.RawMedia("(print)"), gwccss.Raw("break-inside", "avoid"), gwccss.MaxWidth(gwccss.Percent(100))),
	)
	declareGlobal(".tabs",
		mediaRule(gwccss.RawMedia("(print)"), gwccss.Raw("overflow", "visible"), gwccss.Raw("white-space", "normal")),
	)
}
func peopleStickyHeaderStylesStylesheet() string {
	return buildTypedSheet(declarepeopleStickyHeaderStylesPre) +
		supportsOverflowClip() +
		buildTypedSheet(declarepeopleStickyHeaderStylesPost)
}

// supportsOverflowClip restores the @supports(overflow:clip) upgrade that must
// stay after the base .people-directory rule for the cascade to apply.
func supportsOverflowClip() string {
	return atRule("@supports(overflow:clip)", buildTypedSheet(func() {
		declareGlobal(".people-directory", gwccss.Overflow.Clip)
	}))
}

func declarepeopleStickyHeaderStylesPre() {
	declareGlobal(".people-directory",
		gwccss.Position.Relative,
		gwccss.Raw("overflow", "visible"),
		gwccss.Raw("isolation", "isolate"),
	)
}

func declarepeopleStickyHeaderStylesPost() {
	declareGlobal(".people-directory .people-columns",
		gwccss.Position.Sticky,
		gwccss.ZIndex(4),
		gwccss.Top(gwccss.Zero),
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.Raw("box-shadow", "0 1px 0 var(--line),0 10px 18px color-mix(in srgb,var(--ink) 8%,transparent)"),
	)
	declareGlobal(".people-directory .people-columns",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderBottom(gwccss.Px(2), gwccss.Color("CanvasText")), gwccss.Raw("box-shadow", "none")),
	)
	declareGlobal(".people-directory .people-columns",
		mediaRule(gwccss.RawMedia("(print)"), gwccss.Position.Static, gwccss.Raw("box-shadow", "none")),
	)
}
func dataTableStylesStylesheet() string {
	return buildTypedSheet(declaredataTableStylesStyles)
}

func declaredataTableStylesStyles() {
	declareGlobal(".data-table-scroll",
		gwccss.Position.Relative,
		gwccss.MaxWidth(gwccss.Percent(100)),
		gwccss.MaxHeight(gwccss.MinLen(gwccss.Vh(70), gwccss.Px(760))),
		gwccss.Raw("overflow", "auto"),
		gwccss.Raw("overscroll-behavior", "contain"),
		gwccss.Raw("scrollbar-width", "thin"),
		gwccss.Raw("scrollbar-color", "var(--hcm-nav-scrollbar-thumb) var(--hcm-nav-scrollbar-track)"),
	)
	declareGlobal(".data-table-loader-anchor",
		gwccss.Position.Sticky,
		gwccss.ZIndex(7),
		gwccss.Top(gwccss.Px(56)),
		gwccss.H(gwccss.Zero),
		gwccss.Display.Flex,
		gwccss.Raw("justify-content", "center"),
		gwccss.Raw("pointer-events", "none"),
	)
	declareGlobal(".data-table-loader",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(8)),
		gwccss.MinHeight(gwccss.Px(34)),
		gwccss.PaddingY(gwccss.Px(7)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
		gwccss.Raw("box-shadow", "0 10px 26px color-mix(in srgb,var(--ink) 16%,transparent)"),
	)
	declareGlobal(".data-table-spinner",
		gwccss.W(gwccss.Px(16)),
		gwccss.H(gwccss.Px(16)),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.Border(gwccss.Px(2), gwccss.Var("line")),
		gwccss.Raw("border-top-color", "var(--accent)"),
		gwccss.Raw("flex", "0 0 auto"),
	)
	declareGlobal(":root:not([data-hcm-motion-preference=\"reduce\"]):not([data-hcm-motion-preference=\"limited\"]) .data-table-spinner",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"),
			gwccss.Keyframes("hcm-table-spinner",
				gwccss.At("to", gwccss.Transform(gwccss.Rotate(gwccss.Deg(360)))),
			),
			gwccss.Animation(gwccss.Ms(720), gwccss.Linear),
			gwccss.Raw("animation-iteration-count", "infinite"),
		),
	)
	declareGlobal(".data-table-scroll.is-busy .data-table-body",
		gwccss.OpacityNum(gwccss.Num(.64)),
		gwccss.Raw("transition", "opacity var(--hcm-motion-fast) var(--hcm-motion-easing)"),
	)
	declareGlobal(":root[data-hcm-motion-preference=\"reduce\"] .data-table-scroll.is-busy .data-table-body,:root[data-hcm-motion-preference=\"limited\"] .data-table-scroll.is-busy .data-table-body",
		gwccss.Raw("transition", "none"),
	)
	declareGlobal(".data-table-loader",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Bg(gwccss.Color("Canvas")), gwccss.TextColor(gwccss.Color("CanvasText")), gwccss.BorderColor(gwccss.Color("CanvasText"))),
	)
	declareGlobal(".data-table-spinner",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("CanvasText")), gwccss.Raw("border-top-color", "Highlight")),
	)
	declareGlobal(".data-table-loader-anchor",
		mediaRule(gwccss.RawMedia("(print)"), gwccss.Display.None),
	)
	declareGlobal(".data-table",
		gwccss.W(gwccss.Percent(100)),
		gwccss.BorderSpacing(gwccss.Zero),
		gwccss.Raw("border-collapse", "separate"),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".data-table-head,.data-table .data-table-row",
		gwccss.Raw("display", "table-row"),
	)
	declareGlobal(".data-table thead",
		gwccss.Position.Sticky,
		gwccss.ZIndex(5),
		gwccss.Top(gwccss.Zero),
	)
	declareGlobal(".data-table thead th",
		gwccss.PaddingY(gwccss.Zero), gwccss.PaddingX(gwccss.Px(14)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("text-align", "left"),
		gwccss.Raw("white-space", "nowrap"),
		gwccss.Raw("box-shadow", "0 8px 14px color-mix(in srgb,var(--ink) 6%,transparent)"),
	)
	declareGlobal(".data-table-column.data-table-width-8",
		gwccss.MinWidth(gwccss.Rem(8)),
	)
	declareGlobal(".data-table-column.data-table-width-10",
		gwccss.MinWidth(gwccss.Rem(10)),
	)
	declareGlobal(".data-table-column.data-table-width-12",
		gwccss.MinWidth(gwccss.Rem(12)),
	)
	declareGlobal(".data-table-column.data-table-width-14",
		gwccss.MinWidth(gwccss.Rem(14)),
	)
	declareGlobal(".data-table-column.data-table-width-16",
		gwccss.MinWidth(gwccss.Rem(16)),
	)
	declareGlobal(".data-table-column.data-table-width-18",
		gwccss.MinWidth(gwccss.Rem(18)),
	)
	declareGlobal(".data-table-column.data-table-width-20",
		gwccss.MinWidth(gwccss.Rem(20)),
	)
	declareGlobal(".data-table th.align-end,.data-table td.align-end",
		gwccss.Raw("text-align", "right"),
	)
	declareGlobal(".data-table-sort",
		gwccss.Display.Flex,
		gwccss.MinHeight(gwccss.Px(48)),
		gwccss.Items.Center,
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("font", "inherit"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".data-table-sort:hover,.data-table-sort.active",
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".data-table-sort.active",
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".data-table-cell",
		gwccss.MinWidth(gwccss.Px(110)),
		gwccss.PaddingY(gwccss.Px(11)), gwccss.PaddingX(gwccss.Px(14)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Raw("text-align", "left"),
		gwccss.Raw("vertical-align", "middle"),
	)
	declareGlobal(".data-table-row-header",
		gwccss.MinWidth(gwccss.Px(180)),
		gwccss.Raw("font-weight", "inherit"),
	)
	declareGlobal(".data-table-row:hover",
		gwccss.Raw("transform", "none!important"),
	)
	declareGlobal(".data-table-row:hover>.data-table-cell",
		gwccss.Bg(gwccss.Var("hcm-hover-surface")),
	)
	declareGlobal(".data-table-body>.data-table-row:last-child>.data-table-cell",
		gwccss.Raw("border-bottom", "0"),
	)
	declareGlobal(".people-table :is(th,td):nth-child(1)",
		gwccss.MinWidth(gwccss.Px(170)),
	)
	declareGlobal(".people-table :is(th,td):nth-child(2)",
		gwccss.MinWidth(gwccss.Px(160)),
	)
	declareGlobal(".people-table :is(th,td):nth-child(3)",
		gwccss.MinWidth(gwccss.Px(125)),
	)
	declareGlobal(".people-table :is(th,td):nth-child(4)",
		gwccss.MinWidth(gwccss.Px(105)),
	)
	declareGlobal(".people-table :is(th,td):nth-child(5)",
		gwccss.MinWidth(gwccss.Px(125)),
	)
	declareGlobal(".people-table :is(th,td):nth-child(6)",
		gwccss.MinWidth(gwccss.Px(108)),
	)
	declareGlobal(".people-table .people-row-actions",
		gwccss.Raw("display", "table-cell"),
	)
	declareGlobal(".people-table .person-cell",
		gwccss.Display.Flex,
	)
	declareGlobal(".people-table .people-action-heading",
		gwccss.Raw("text-align", "right"),
	)
	declareGlobal(".data-table-scroll:focus-visible",
		gwccss.Raw("outline", "2px solid var(--accent)"),
		gwccss.OutlineOffset(gwccss.Px(-2)),
	)
	declareGlobal(".data-table-scroll",
		mediaRule(gwccss.MaxW(1050), gwccss.MaxHeight(gwccss.RawLength("none")), gwccss.Raw("overflow", "visible")),
	)
	declareGlobal(".data-table",
		mediaRule(gwccss.MaxW(1050), gwccss.Display.Block),
	)
	declareGlobal(".data-table thead",
		mediaRule(gwccss.MaxW(1050), gwccss.Display.Block, gwccss.Position.Sticky, gwccss.Top(gwccss.Zero), gwccss.ZIndex(5), gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(12)), gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")), gwccss.Bg(gwccss.Var("surface-subtle")), gwccss.Raw("box-shadow", "0 8px 14px color-mix(in srgb,var(--ink) 6%,transparent)"), gwccss.Raw("overflow-x", "auto"), gwccss.Raw("overscroll-behavior-inline", "contain")),
	)
	declareGlobal(".data-table-head",
		mediaRule(gwccss.MaxW(1050), gwccss.Raw("display", "flex!important"), gwccss.Items.Center, gwccss.Gap(gwccss.Px(8))),
	)
	declareGlobal(".data-table-head>.data-table-column",
		mediaRule(gwccss.MaxW(1050), gwccss.Display.Block, gwccss.MinWidth(gwccss.RawLength("0!important")), gwccss.Padding(gwccss.Zero), gwccss.Raw("border", "0"), gwccss.Bg(gwccss.Transparent), gwccss.Raw("box-shadow", "none")),
	)
	declareGlobal(".data-table-head>.data-table-column.align-end",
		mediaRule(gwccss.MaxW(1050), gwccss.Display.None),
	)
	declareGlobal(".data-table-sort",
		mediaRule(gwccss.MaxW(1050), gwccss.MinHeight(gwccss.Px(36)), gwccss.PaddingY(gwccss.Zero), gwccss.PaddingX(gwccss.Px(10)), gwccss.Border(gwccss.Px(1), gwccss.Var("line")), gwccss.Rounded(gwccss.VarLength("hcm-radius-control")), gwccss.Bg(gwccss.Var("surface"))),
	)
	declareGlobal(".data-table-sort.active",
		mediaRule(gwccss.MaxW(1050), gwccss.BorderColor(gwccss.Var("accent")), gwccss.Bg(gwccss.Var("soft"))),
	)
	declareGlobal(".data-table-body",
		mediaRule(gwccss.MaxW(1050), gwccss.Display.Grid),
	)
	declareGlobal(".data-table .data-table-row",
		mediaRule(gwccss.MaxW(1050), gwccss.Display.Grid, gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))), gwccss.Gap(gwccss.Px(8)), gwccss.Padding(gwccss.Px(14)), gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")), gwccss.Bg(gwccss.Var("surface"))),
	)
	declareGlobal(".data-table .data-table-cell",
		mediaRule(gwccss.MaxW(1050), gwccss.Raw("display", "flex!important"), gwccss.MinWidth(gwccss.RawLength("0!important")), gwccss.Padding(gwccss.Px(3)), gwccss.Raw("border", "0"), gwccss.Raw("background", "transparent!important"), gwccss.Raw("justify-content", "space-between"), gwccss.Gap(gwccss.Px(18)), gwccss.Raw("text-align", "start")),
	)
	declareGlobal(".data-table .data-table-cell:not(.data-table-row-header):not(.people-row-actions):before",
		mediaRule(gwccss.MaxW(1050), gwccss.Raw("content", "attr(data-label)"), gwccss.Raw("flex", "none"), gwccss.TextColor(gwccss.Var("muted")), gwccss.FontSize(gwccss.Rem(0.75)), gwccss.Raw("font-weight", "600")),
	)
	declareGlobal(".data-table .data-table-row-header",
		mediaRule(gwccss.MaxW(1050), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))), gwccss.Raw("justify-content", "flex-start")),
	)
	declareGlobal(".data-table .people-row-actions",
		mediaRule(gwccss.MaxW(1050), gwccss.Raw("display", "flex!important"), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))), gwccss.Raw("justify-content", "flex-end")),
	)
	declareGlobal(".data-table-sort-label",
		mediaRule(gwccss.MaxW(1050), gwccss.Display.Block, gwccss.Raw("padding", "8px 12px 0"), gwccss.TextColor(gwccss.Var("muted")), gwccss.FontSize(gwccss.Rem(0.75)), gwccss.Raw("font-weight", "700")),
	)
	declareGlobal(".data-table .data-table-row",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".data-table .data-table-cell,.data-table .data-table-row-header,.data-table .people-row-actions",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(1))),
	)
	declareGlobal(".data-table .people-row-actions",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("justify-content", "stretch")),
	)
	declareGlobal(".data-table .people-row-actions>*",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal(".data-table thead,.data-table thead th",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderBottom(gwccss.Px(2), gwccss.Color("CanvasText")), gwccss.Raw("box-shadow", "none")),
	)
	declareGlobal(".data-table-sort.active",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("outline", "1px solid Highlight"), gwccss.OutlineOffset(gwccss.Px(2))),
	)
	declareGlobal(".data-table-scroll",
		mediaRule(gwccss.RawMedia("(print)"), gwccss.MaxHeight(gwccss.RawLength("none")), gwccss.Raw("overflow", "visible")),
	)
	declareGlobal(".data-table thead",
		mediaRule(gwccss.RawMedia("(print)"), gwccss.Position.Static),
	)
	declareGlobal(".data-table thead th",
		mediaRule(gwccss.RawMedia("(print)"), gwccss.Raw("box-shadow", "none")),
	)
}
func peopleActionColumnStylesStylesheet() string {
	return buildTypedSheet(declarepeopleActionColumnStylesStyles)
}

func declarepeopleActionColumnStylesStyles() {
	declareGlobal(".page-head[data-hcm-page=\"people\"]",
		gwccss.Raw("margin-bottom", "16px"),
	)
	declareGlobal(".people-page",
		gwccss.Gap(gwccss.Px(10)),
	)
	declareGlobal(".people-page .people-filter",
		gwccss.PaddingY(gwccss.Px(12)),
	)
	declareGlobal(".people-page .people-table .data-table-cell",
		gwccss.PaddingY(gwccss.Px(8)),
	)
	declareGlobal(".people-page .directory-tools>div",
		mediaRule(gwccss.MinW(1200), gwccss.Display.Flex, gwccss.Items.Baseline, gwccss.Gap(gwccss.Px(10))),
	)
	declareGlobal(".people-page .directory-tools p",
		mediaRule(gwccss.MinW(1200), gwccss.Margin(gwccss.Zero)),
	)
	declareGlobal(".people-page .people-filter",
		mediaRule(gwccss.MinW(1200), gwccss.GridCols(gwccss.Fr(1)), gwccss.Items.Center),
	)
	declareGlobal(".people-page .people-filter>label",
		// The search input has its own accessible name. On wide tables the
		// redundant visible label costs the width needed by the Clear action.
		mediaRule(gwccss.MinW(1200), gwccss.Display.None),
	)
	// The People directory uses the page's scrollport for both its rows and
	// sticky header. An inner vertical scroll clipped row menus and made the
	// page and table compete for the same wheel gesture.
	declareGlobal(".people-directory,.people-directory .data-table-scroll",
		gwccss.MaxHeight(gwccss.RawLength("none")),
		gwccss.Raw("overflow", "visible"),
	)
	declareGlobal(".people-workflow-menu>.people-workflow-options",
		mediaRule(gwccss.MinW(1051), gwccss.Position.Absolute, gwccss.W(gwccss.RawLength("min(270px,calc(100vw - 32px))")), gwccss.MinWidth(gwccss.Zero), gwccss.Raw("margin-block-start", "0")),
	)
	declareGlobal(".people-table td:last-child:has(.popover-root[open])",
		gwccss.ZIndex(8),
	)
	declareGlobal(".people-table :is(th,td):last-child",
		mediaRule(gwccss.MinW(1051), gwccss.Position.Sticky, gwccss.Raw("inset-inline-end", "0"), gwccss.Bg(gwccss.Var("surface")), gwccss.Raw("box-shadow", "-10px 0 16px color-mix(in srgb,var(--ink) 5%,transparent)")),
	)
	declareGlobal(".people-table thead th:last-child",
		mediaRule(gwccss.MinW(1051), gwccss.ZIndex(7), gwccss.Bg(gwccss.Var("surface-subtle"))),
	)
	declareGlobal(".people-table .data-table-row:hover>td:last-child",
		mediaRule(gwccss.MinW(1051), gwccss.Bg(gwccss.Var("hcm-hover-surface"))),
	)
	declareGlobal(".people-table :is(th,td):last-child",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("border-inline-start", "1px solid CanvasText"), gwccss.Raw("box-shadow", "none")),
	)
	declareNavigationDrawerShellStyles()
}

// declareNavigationDrawerShellStyles finishes the UXAUDIT-001 narrow-shell
// contract. It is called from here (rather than its own registered
// stylesheet function) purely for cascade order: historyNavigationStylesheet
// moves .topbar>.header-navigation-tools onto a second grid row at narrow
// widths, and that declaration must lose so the header stays one row. CSS
// gives equal-specificity, later-declared rules the win; this package's
// stylesheets concatenate in a fixed order (styles.go), and this call site
// is one of the last to run, after every rule it needs to outrank.
func declareNavigationDrawerShellStyles() {
	declareGlobal(".topbar>.header-navigation-tools",
		mediaRule(gwccss.MaxW(760),
			gwccss.Raw("grid-column", "auto"),
			gwccss.Raw("grid-row", "auto"),
			gwccss.Padding(gwccss.Zero),
			gwccss.MinWidth(gwccss.Zero),
			gwccss.Raw("flex", "1 1 auto"),
			gwccss.Raw("flex-wrap", "nowrap"),
			gwccss.Raw("overflow-x", "auto"),
			gwccss.Raw("overscroll-behavior-inline", "contain"),
		),
	)
}
