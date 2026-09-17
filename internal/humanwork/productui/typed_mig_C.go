package productui

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

func uxReviewRefinementsStylesheet() string {
	return buildTypedSheet(declareuxReviewRefinementsStyles)
}

func declareuxReviewRefinementsStyles() {
	declareGlobal(":root",
		gwccss.CustomLength("hcm-navigation-width", gwccss.Px(270)),
	)
	declareGlobal(".main",
		gwccss.MaxWidth(gwccss.Px(1440)),
	)
	declareGlobal(".page-head",
		gwccss.Raw("margin-bottom", "26px"),
	)
	declareGlobal(".notifications summary",
		gwccss.FontSize(gwccss.RawLength("inherit")),
	)
	declareGlobal(".notifications summary:before",
		gwccss.Display.None,
	)
	declareGlobal(".notifications summary>.nav-icon",
		gwccss.W(gwccss.Px(20)),
		gwccss.H(gwccss.Px(20)),
	)
	declareGlobal(".side-stack .panel",
		gwccss.Raw("margin-top", "0"),
	)
	declareGlobal(".side-stack .facts",
		gwccss.Raw("padding", "2px 22px 17px"),
	)
	declareGlobal(".side-stack .summary-scope",
		gwccss.Raw("margin", "0"),
		gwccss.Raw("padding", "0 22px"),
		gwccss.FontSize(gwccss.Rem(.8125)),
	)
	declareGlobal(".side-stack .facts>div",
		gwccss.Raw("padding-block", "10px"),
	)
	declareGlobal(".quick-actions",
		gwccss.Gap(gwccss.Px(8)),
		gwccss.Raw("padding", "0 18px 18px"),
	)
	declareGlobal(".quick-actions .button",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinHeight(gwccss.Px(42)),
	)
	// Populated Home shortcuts stay contextual. The quiet-state composition
	// promotes just its first authorized next step.
	declareGlobal(".home-quick-actions .quick-actions",
		gwccss.Raw("justify-items", "start"),
	)
	declareGlobal(".home-quick-actions .quick-actions .button",
		gwccss.W(gwccss.Auto),
		gwccss.MaxWidth(gwccss.Percent(100)),
		gwccss.MinHeight(gwccss.Px(44)),
	)
	declareGlobal(".home-grid-empty",
		gwccss.GridCols(gwccss.Fr(1)), gwccss.MaxWidth(gwccss.Px(760)),
	)
	declareGlobal(".home-grid-empty .home-primary-rail",
		gwccss.GridCols(gwccss.Fr(1)),
	)
	declareGlobal(".home-empty-primary .quick-actions .button.primary",
		gwccss.Raw("justify-self", "start"),
	)
	declareGlobal(".home-continuity-summary>p",
		gwccss.Raw("margin", "0"), gwccss.Raw("padding", "0 18px 18px"),
	)
	declareGlobal(".settings-overview-grid",
		gwccss.Raw("align-items", "start"),
	)
	declareGlobal(".settings-accessibility-layout",
		gwccss.Raw("align-items", "start"),
	)
	declareGlobal(".settings-accessibility-layout>.panel",
		gwccss.Raw("margin-top", "0"),
	)
	declareGlobal(".settings-accessibility-layout .quick-actions a",
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(14)),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".history-filter-controls",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal(".history-filter-controls>input",
		gwccss.Raw("flex", "2 1 280px"),
	)
	declareGlobal(".history-filter-controls>select",
		gwccss.Raw("flex", "1 1 150px"),
	)
	declareGlobal(".history-filter-controls>.button",
		gwccss.Raw("flex", "0 0 auto"),
	)
	declareGlobal(".nav-label",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("text-overflow", "ellipsis"),
	)
	declareGlobal(".subnav .nav-label",
		gwccss.MaxWidth(gwccss.Px(170)),
		gwccss.Raw("text-overflow", "ellipsis"),
	)
	declareGlobal(".jn-embedded .jn-journey-technical",
		gwccss.Position.Relative,
		gwccss.ZIndex(1),
		gwccss.Raw("margin-top", "2px"),
		gwccss.Raw("padding-top", "8px"),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".jn-embedded .jn-journey-technical>summary",
		gwccss.W(gwccss.RawLength("max-content")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.75)),
		gwccss.Raw("font-weight", "650"),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".jn-embedded .jn-journey-technical>summary::-webkit-details-marker",
		gwccss.Display.None,
	)
	declareGlobal(".jn-embedded .jn-journey-technical>summary:after",
		gwccss.Raw("content", "\" +\""),
	)
	declareGlobal(".jn-embedded .jn-journey-technical[open]>summary:after",
		gwccss.Raw("content", "\" −\""),
	)
	declareGlobal(".jn-embedded .jn-journey-technical .jn-meta",
		gwccss.Raw("margin-top", "8px"),
		gwccss.PaddingY(gwccss.Px(9)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".topbar",
		mediaRule(gwccss.MinW(1191), gwccss.GridCols(gwccss.TrackLen(gwccss.Px(270)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(220)), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
	declareGlobal(".shell-grid",
		mediaRule(gwccss.MinW(1191), gwccss.GridCols(gwccss.TrackLen(gwccss.Px(270)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".sidebar",
		mediaRule(gwccss.MinW(1191), gwccss.W(gwccss.Px(270))),
	)
	declareGlobal(".nav-label",
		mediaRule(gwccss.MinW(1191), gwccss.MaxWidth(gwccss.Px(198))),
	)
	declareGlobal(".loading-progress",
		mediaRule(gwccss.MinW(1191), gwccss.Left(gwccss.Px(270))),
	)
	declareGlobal(".app-shell.nav-collapsed .loading-progress",
		mediaRule(gwccss.MinW(1191), gwccss.Left(gwccss.Px(72))),
	)
	declareGlobal(".history-filter-controls",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Grid, gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".history-filter-controls>*",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal(".side-stack",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".jn-embedded .jn-journey-technical>summary",
		mediaRule(gwccss.MaxW(760), gwccss.MinHeight(gwccss.Px(44)), gwccss.Display.Flex, gwccss.Items.Center),
	)
	declareGlobal(".jn-embedded .jn-journey-technical",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("CanvasText"))),
	)
}

func loadingLayoutOffsetsStylesheet() string {
	return buildTypedSheet(declareloadingLayoutOffsetsStyles)
}

func declareloadingLayoutOffsetsStyles() {
	declareGlobal(".app-shell .loading-progress",
		gwccss.Left(gwccss.Px(232)),
	)
	declareGlobal(".app-shell.nav-collapsed .loading-progress",
		gwccss.Left(gwccss.Px(72)),
	)
	declareGlobal(".app-shell .loading-progress",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1190px)"), gwccss.Left(gwccss.Px(210))),
	)
	declareGlobal(".app-shell.nav-collapsed .loading-progress",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1190px)"), gwccss.Left(gwccss.Px(72))),
	)
	declareGlobal(".app-shell .loading-progress,.app-shell.nav-collapsed .loading-progress",
		mediaRule(gwccss.MaxW(760), gwccss.Top(gwccss.Zero), gwccss.Left(gwccss.Zero)),
	)
}

func loadingProxyStylesStylesheet() string {
	return buildTypedSheet(declareloadingProxyStylesStyles)
}

func declareloadingProxyStylesStyles() {
	declareGlobal(".loading-proxy",
		gwccss.Position.Relative,
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.VarLength("theme-section-gap")),
		gwccss.MinHeight(gwccss.RawLength("clamp(360px,58vh,760px)")),
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".loading-progress",
		gwccss.Position.Fixed,
		gwccss.ZIndex(30),
		gwccss.Top(gwccss.Px(79)),
		gwccss.Left(gwccss.RawLength("var(--hcm-navigation-width,232px)")),
		gwccss.Right(gwccss.Zero),
		gwccss.H(gwccss.Px(3)),
		gwccss.Raw("overflow", "hidden"),
		gwccss.Bg(gwccss.Var("surface-muted")),
	)
	declareGlobal(".loading-progress:after",
		gwccss.Display.Block,
		gwccss.W(gwccss.Percent(38)),
		gwccss.H(gwccss.Percent(100)),
		gwccss.Rounded(gwccss.Px(999)),
		gwccss.Bg(gwccss.Var("accent")),
		gwccss.Raw("content", "\"\""),
	)
	declareGlobal(".loading-panel",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Padding(gwccss.VarLength("theme-panel-padding")),
		gwccss.Raw("overflow", "hidden"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Raw("box-shadow", "none"),
	)
	declareGlobal(".loading-two-column",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1.6)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(270)), gwccss.Fr(.8))),
		gwccss.Gap(gwccss.VarLength("theme-section-gap")),
		gwccss.Raw("align-items", "start"),
	)
	declareGlobal(".loading-toolbar",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(18)),
		gwccss.MinHeight(gwccss.Px(46)),
	)
	declareGlobal(".loading-block",
		gwccss.Position.Relative,
		gwccss.Display.Block,
		gwccss.Raw("overflow", "hidden"),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface-muted")),
		gwccss.Raw("isolation", "isolate"),
	)
	declareGlobal(".loading-line",
		gwccss.W(gwccss.Percent(100)),
		gwccss.H(gwccss.Px(12)),
	)
	declareGlobal(".loading-line-short",
		gwccss.W(gwccss.Percent(36)),
	)
	declareGlobal(".loading-line-medium",
		gwccss.W(gwccss.Percent(68)),
	)
	declareGlobal(".loading-value",
		gwccss.W(gwccss.Percent(42)),
		gwccss.H(gwccss.Px(32)),
		gwccss.Raw("margin-top", "12px"),
	)
	declareGlobal(".loading-control",
		gwccss.W(gwccss.Px(190)),
		gwccss.H(gwccss.Px(44)),
	)
	declareGlobal(".loading-chip",
		gwccss.Raw("flex", "none"),
		gwccss.W(gwccss.Px(88)),
		gwccss.H(gwccss.Px(27)),
		gwccss.Rounded(gwccss.Px(999)),
	)
	declareGlobal(".loading-avatar",
		gwccss.Raw("flex", "none"),
		gwccss.W(gwccss.Px(64)),
		gwccss.H(gwccss.Px(64)),
		gwccss.Rounded(gwccss.Percent(50)),
	)
	declareGlobal(".loading-avatar-small",
		gwccss.W(gwccss.Px(42)),
		gwccss.H(gwccss.Px(42)),
	)
	declareGlobal(".loading-copy",
		gwccss.Display.Grid,
		gwccss.Raw("flex", "1"),
		gwccss.Gap(gwccss.Px(10)),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".loading-rows,.loading-facts,.loading-bars",
		gwccss.Display.Grid,
	)
	declareGlobal(".loading-row",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(14)),
		// UXAUDIT-012: this proxy stands in for .work-row (typed_styles.go),
		// which declares the same 86px min-height. They were 76px/86px
		// before this fix -- a 10px-per-row layout shift on every Home,
		// Work and Journeys cold load. Keep these two in lockstep; see
		// TestTodo_UXAUDIT_012's geometry assertion, which fails if they
		// drift apart again.
		gwccss.MinHeight(gwccss.Px(86)),
		gwccss.PaddingY(gwccss.Px(14)), gwccss.PaddingX(gwccss.Zero),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("divider")),
	)
	declareGlobal(".loading-row:last-child,.loading-fact:last-child",
		gwccss.Raw("border-bottom", "0"),
	)
	declareGlobal(".loading-fact",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Px(80)), gwccss.Fr(.65)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(110)), gwccss.Fr(1))),
		gwccss.Gap(gwccss.Px(22)),
		gwccss.PaddingY(gwccss.Px(15)), gwccss.PaddingX(gwccss.Zero),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("divider")),
	)
	declareGlobal(".loading-profile-layout,.loading-analysis-layout,.loading-work-layout,.loading-table-layout,.loading-settings-layout",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.VarLength("theme-section-gap")),
	)
	declareGlobal(".loading-profile-head",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(18)),
	)
	declareGlobal(".loading-metrics",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(3, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
		gwccss.Gap(gwccss.VarLength("theme-section-gap")),
	)
	declareGlobal(".loading-table-panel",
		gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(".loading-table",
		gwccss.Display.Grid,
	)
	declareGlobal(".loading-table-row",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Fr(1.4), gwccss.Fr(1), gwccss.Fr(1), gwccss.Fr(.7)),
		gwccss.Gap(gwccss.Px(20)),
		gwccss.Items.Center,
		// UXAUDIT-012: this proxy stands in for .people-row (typed_styles.go),
		// which declares the same 65px min-height. They were 67px/65px
		// before this fix -- a 2px-per-row layout shift on every People
		// cold load. Keep these two in lockstep; see TestTodo_UXAUDIT_012's
		// geometry assertion, which fails if they drift apart again.
		gwccss.MinHeight(gwccss.Px(65)),
		gwccss.PaddingY(gwccss.Px(12)), gwccss.PaddingX(gwccss.Px(20)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("divider")),
	)
	declareGlobal(".loading-table-head",
		// Matches .people-columns' 44px min-height for the same reason.
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".loading-bar-row",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(90)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.Gap(gwccss.Px(18)),
		gwccss.Items.Center,
		gwccss.PaddingY(gwccss.Px(14)), gwccss.PaddingX(gwccss.Zero),
	)
	declareGlobal(".loading-bar",
		gwccss.H(gwccss.Px(24)),
	)
	declareGlobal(".loading-bar-5",
		gwccss.W(gwccss.Percent(58)),
	)
	declareGlobal(".loading-bar-6",
		gwccss.W(gwccss.Percent(72)),
	)
	declareGlobal(".loading-bar-7",
		gwccss.W(gwccss.Percent(64)),
	)
	declareGlobal(".loading-bar-8",
		gwccss.W(gwccss.Percent(83)),
	)
	declareGlobal(".loading-bar-9",
		gwccss.W(gwccss.Percent(69)),
	)
	declareGlobal(".notification-loading",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.MinWidth(gwccss.Px(44)),
		gwccss.MinHeight(gwccss.Px(44)),
	)
	declareGlobal(".loading-notification",
		gwccss.W(gwccss.Px(22)),
		gwccss.H(gwccss.Px(22)),
		gwccss.Rounded(gwccss.Percent(50)),
	)
	declareGlobal(".app-shell.is-loading .nav-count",
		gwccss.TextColor(gwccss.Transparent),
		gwccss.MinWidth(gwccss.Px(29)),
		gwccss.MinHeight(gwccss.Px(20)),
		gwccss.Bg(gwccss.Var("surface-muted")),
	)
	declareGlobal(":root:not([data-hcm-motion-preference=\"reduce\"]):not([data-hcm-motion-preference=\"limited\"]) .loading-block:after",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Position.Absolute, gwccss.Raw("inset", "0"), gwccss.Raw("background", "linear-gradient(100deg,transparent 18%,color-mix(in srgb,var(--surface) 72%,transparent) 48%,transparent 78%)"), gwccss.Raw("content", "\"\""), gwccss.Keyframes("hcm-shimmer", gwccss.At("from", gwccss.Raw("transform", "translateX(-115%)")), gwccss.At("to", gwccss.Raw("transform", "translateX(115%)"))), gwccss.Animation(gwccss.S(1.25), gwccss.Linear), gwccss.Raw("animation-iteration-count", "infinite")),
	)
	declareGlobal(":root:not([data-hcm-motion-preference=\"reduce\"]):not([data-hcm-motion-preference=\"limited\"]) .loading-progress:after",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"),
			gwccss.Keyframes("hcm-route-progress",
				gwccss.At("0%", gwccss.Raw("transform", "translateX(-110%)")),
				gwccss.At("100%", gwccss.Raw("transform", "translateX(365%)")),
			),
			gwccss.Animation(gwccss.RawDuration("1.15s"), gwccss.Easing("var(--hcm-motion-easing)")),
			gwccss.Raw("animation-iteration-count", "infinite"),
		),
	)
	declareGlobal(".loading-two-column",
		mediaRule(gwccss.MaxW(990), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".loading-metrics",
		mediaRule(gwccss.MaxW(990), gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))))),
	)
	declareGlobal(".loading-detail-proxy",
		mediaRule(gwccss.MaxW(990), gwccss.Display.None),
	)
	declareGlobal(".loading-progress",
		mediaRule(gwccss.MaxW(760), gwccss.Top(gwccss.Zero), gwccss.Left(gwccss.Zero)),
	)
	declareGlobal(".loading-toolbar",
		mediaRule(gwccss.MaxW(760), gwccss.Items.Stretch, gwccss.FlexDir.Col),
	)
	declareGlobal(".loading-control",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal(".loading-table-row",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1.3), gwccss.Fr(1))),
	)
	declareGlobal(".loading-table-row>:nth-child(n+3)",
		mediaRule(gwccss.MaxW(760), gwccss.Display.None),
	)
	declareGlobal(".loading-metrics",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".loading-block,.loading-progress",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Border(gwccss.Px(1), gwccss.Color("CanvasText")), gwccss.Bg(gwccss.Color("Canvas"))),
	)
	declareGlobal(".loading-progress:after",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Bg(gwccss.Color("Highlight"))),
	)
}

func navigationPolishStylesStylesheet() string {
	return buildTypedSheet(declarenavigationPolishStylesStyles)
}

func declarenavigationPolishStylesStyles() {
	declareGlobal(".app-shell.nav-collapsed .brand-cluster",
		mediaRule(gwccss.MinW(761), gwccss.GridCols(gwccss.TrackLen(gwccss.Px(28)), gwccss.TrackLen(gwccss.Px(32))), gwccss.Gap(gwccss.Px(4)), gwccss.Raw("padding-inline", "4px")),
	)
	declareGlobal(".app-shell.nav-collapsed .brand-cluster .wordmark",
		mediaRule(gwccss.MinW(761), gwccss.Gap(gwccss.Zero)),
	)
	declareGlobal(".app-shell.nav-collapsed .wordmark-mark",
		mediaRule(gwccss.MinW(761), gwccss.W(gwccss.Px(28)), gwccss.H(gwccss.Px(28))),
	)
}

func surfaceTokenCoverageStylesheet() string {
	return buildTypedSheet(declaresurfaceTokenCoverageStyles)
}

func declaresurfaceTokenCoverageStyles() {
	declareGlobal(".surface",
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
}

func brandLogoStylesStylesheet() string {
	return buildTypedSheet(declarebrandLogoStylesStyles)
}

func declarebrandLogoStylesStyles() {
	declareGlobal(".brand-logo-slot",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.MinWidth(gwccss.Zero),
		gwccss.MaxWidth(gwccss.Percent(100)),
	)
	declareGlobal(".brand-logo-image",
		gwccss.Display.Block,
		gwccss.W(gwccss.RawLength("auto")),
		gwccss.MaxWidth(gwccss.MinLen(gwccss.Px(180), gwccss.Percent(100))),
		gwccss.H(gwccss.Px(40)),
		gwccss.Raw("object-fit", "contain"),
		gwccss.Raw("object-position", "left center"),
	)
	declareGlobal(".brand-logo-fallback",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(10)),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".brand-logo-slot[data-hcm-brand-logo-state=\"fallback\"] .brand-logo-image,.brand-logo-slot[data-hcm-brand-logo-state=\"loading\"] .brand-logo-image",
		gwccss.Display.None,
	)
	declareGlobal(".brand-logo-slot[data-hcm-brand-logo-state=\"configured\"] .brand-logo-fallback",
		gwccss.Display.None,
	)
	declareGlobal(".appearance-preview-logo",
		gwccss.MaxWidth(gwccss.Px(154)),
	)
	declareGlobal(".appearance-preview-logo .brand-logo-image",
		gwccss.H(gwccss.Px(28)),
		gwccss.MaxWidth(gwccss.Px(154)),
	)
	declareGlobal(".appearance-preview-logo .wordmark-mark",
		gwccss.W(gwccss.Px(26)),
		gwccss.H(gwccss.Px(26)),
		gwccss.FontSize(gwccss.Rem(.7)),
	)
	declareGlobal(".appearance-brand-logo-field",
		gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
	)
	declareGlobal(".app-shell.nav-collapsed .brand-logo-slot",
		mediaRule(gwccss.MinW(761), gwccss.W(gwccss.Px(28))),
	)
	declareGlobal(".app-shell.nav-collapsed .brand-logo-image",
		mediaRule(gwccss.MinW(761), gwccss.Display.None),
	)
	declareGlobal(".app-shell.nav-collapsed .brand-logo-fallback",
		mediaRule(gwccss.MinW(761), gwccss.Display.Flex),
	)
	declareGlobal(".appearance-brand-logo-field",
		mediaRule(gwccss.MaxW(680), gwccss.GridColumn(gwccss.GridAuto)),
	)
}

func compactBrandStylesStylesheet() string {
	return buildTypedSheet(declarecompactBrandStylesStyles)
}

func declarecompactBrandStylesStyles() {
	declareGlobal(".brand-logo-slot[data-hcm-brand-logo-state=\"fallback\"] .wordmark-label",
		mediaRule(gwccss.MaxW(360), gwccss.Display.None),
	)
	declareGlobal(".brand-logo-slot[data-hcm-brand-logo-state=\"fallback\"] .brand-logo-fallback",
		mediaRule(gwccss.MaxW(360), gwccss.Gap(gwccss.Zero)),
	)
}

func legacySurfaceCoverageStylesheet() string {
	return buildTypedSheet(declarelegacySurfaceCoverageStyles)
}

func declarelegacySurfaceCoverageStyles() {
	declareGlobal(".people-filter",
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".people-pager,.button.disabled",
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".history-row:hover",
		gwccss.Bg(gwccss.Var("soft")),
	)
	declareGlobal(".studio-notice",
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("muted")),
	)
}

func navigationScrollbarStylesStylesheet() string {
	return buildTypedSheet(declarenavigationScrollbarStylesStyles)
}

func declarenavigationScrollbarStylesStyles() {
	declareGlobal(":root",
		gwccss.Custom("hcm-nav-scrollbar-track", "color-mix(in srgb,var(--surface) 82%,var(--soft))"),
		gwccss.Custom("hcm-nav-scrollbar-thumb", "color-mix(in srgb,var(--muted) 38%,var(--surface))"),
		gwccss.Custom("hcm-nav-scrollbar-thumb-hover", "color-mix(in srgb,var(--accent) 82%,var(--muted))"),
		gwccss.Custom("hcm-nav-scrollbar-thumb-active", "var(--accent-hover)"),
		gwccss.CustomLength("hcm-nav-scrollbar-size", gwccss.Px(10)),
		gwccss.CustomLength("hcm-nav-scrollbar-size-rail", gwccss.Px(6)),
	)
	declareGlobal(".sidebar",
		gwccss.Raw("scrollbar-width", "thin"),
		gwccss.Raw("scrollbar-color", "var(--hcm-nav-scrollbar-thumb) var(--hcm-nav-scrollbar-track)"),
		gwccss.Raw("scrollbar-gutter", "stable"),
	)
	declareGlobal(".sidebar::-webkit-scrollbar",
		gwccss.W(gwccss.VarLength("hcm-nav-scrollbar-size")),
	)
	declareGlobal(".sidebar::-webkit-scrollbar-track",
		gwccss.Bg(gwccss.Var("hcm-nav-scrollbar-track")),
	)
	declareGlobal(".sidebar::-webkit-scrollbar-thumb",
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.Border(gwccss.Px(3), gwccss.Var("hcm-nav-scrollbar-track")),
		gwccss.Rounded(gwccss.Px(999)),
		gwccss.Bg(gwccss.Var("hcm-nav-scrollbar-thumb")),
		gwccss.Raw("background-clip", "padding-box"),
	)
	declareGlobal(".sidebar::-webkit-scrollbar-thumb:hover",
		gwccss.Bg(gwccss.Var("hcm-nav-scrollbar-thumb-hover")),
		gwccss.Raw("background-clip", "padding-box"),
	)
	declareGlobal(".sidebar::-webkit-scrollbar-thumb:active",
		gwccss.Bg(gwccss.Var("hcm-nav-scrollbar-thumb-active")),
		gwccss.Raw("background-clip", "padding-box"),
	)
	declareGlobal(".sidebar",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("scrollbar-width", "auto"), gwccss.Raw("scrollbar-color", "auto"), gwccss.Raw("scrollbar-gutter", "auto")),
	)
	declareGlobal(".sidebar::-webkit-scrollbar",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.RawLength("auto"))),
	)
	declareGlobal(".sidebar",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("scrollbar-color", "ButtonText Canvas")),
	)
	declareGlobal(".sidebar::-webkit-scrollbar-track",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Bg(gwccss.Color("Canvas"))),
	)
	declareGlobal(".sidebar::-webkit-scrollbar-thumb",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("Canvas")), gwccss.Bg(gwccss.Color("ButtonText"))),
	)
}

func navigationViewportStylesStylesheet() string {
	return buildTypedSheet(declarenavigationViewportStylesStyles)
}

func declarenavigationViewportStylesStyles() {
	declareGlobal(".sidebar",
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".primary-nav",
		gwccss.Raw("flex", "1"),
		gwccss.MinHeight(gwccss.Zero),
		gwccss.Raw("overflow-y", "auto"),
		gwccss.Raw("overscroll-behavior", "contain"),
		gwccss.Raw("scrollbar-width", "thin"),
		gwccss.Raw("scrollbar-color", "var(--hcm-nav-scrollbar-thumb) var(--hcm-nav-scrollbar-track)"),
		gwccss.Raw("scrollbar-gutter", "stable"),
	)
	declareGlobal(".primary-nav::-webkit-scrollbar",
		gwccss.W(gwccss.VarLength("hcm-nav-scrollbar-size")),
	)
	declareGlobal(".primary-nav::-webkit-scrollbar-track",
		gwccss.Bg(gwccss.Var("hcm-nav-scrollbar-track")),
	)
	declareGlobal(".primary-nav::-webkit-scrollbar-thumb",
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.Border(gwccss.Px(3), gwccss.Var("hcm-nav-scrollbar-track")),
		gwccss.Rounded(gwccss.Px(999)),
		gwccss.Bg(gwccss.Var("hcm-nav-scrollbar-thumb")),
		gwccss.Raw("background-clip", "padding-box"),
	)
	declareGlobal(".primary-nav::-webkit-scrollbar-thumb:hover",
		gwccss.Bg(gwccss.Var("hcm-nav-scrollbar-thumb-hover")),
		gwccss.Raw("background-clip", "padding-box"),
	)
	declareGlobal(".primary-nav::-webkit-scrollbar-thumb:active",
		gwccss.Bg(gwccss.Var("hcm-nav-scrollbar-thumb-active")),
		gwccss.Raw("background-clip", "padding-box"),
	)
	declareGlobal(".nav-bottom",
		gwccss.Raw("flex", "none"),
	)
	declareGlobal(".sidebar",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("overflow-y", "visible")),
	)
	declareGlobal(".primary-nav",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("flex", "none"), gwccss.Raw("overflow-x", "auto"), gwccss.Raw("overflow-y", "visible"), gwccss.Raw("scrollbar-width", "auto"), gwccss.Raw("scrollbar-color", "auto"), gwccss.Raw("scrollbar-gutter", "auto")),
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

func navigationViewportXStylesStylesheet() string {
	return buildTypedSheet(declarenavigationViewportXStylesStyles)
}

func declarenavigationViewportXStylesStyles() {
	declareGlobal(".primary-nav",
		gwccss.Raw("overflow-x", "hidden"),
	)
}
