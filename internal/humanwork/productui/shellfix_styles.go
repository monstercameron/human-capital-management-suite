package productui

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

// ShellFixStylesheet holds shell-wide corrective rules. They deliberately sit
// after feature sheets so every product page receives the same rail, control,
// and page-frame contract.
func ShellFixStylesheet() string {
	return buildTypedSheet(declareShellFixStyles)
}

func declareShellFixStyles() {
	// Tablets retain the desktop shell but not the expanded desktop rail. An
	// explicit nav=expanded choice adds .nav-expanded and wins this default.
	const tabletRail = ".app-shell:not(.nav-expanded)"
	declareGlobal(tabletRail+" .shell-grid",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1023px)"),
			gwccss.GridCols(gwccss.TrackLen(gwccss.Px(72)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(tabletRail+" .sidebar",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1023px)"),
			gwccss.W(gwccss.Px(72)), gwccss.Raw("padding-inline", "9px")),
	)
	declareGlobal(tabletRail+" .sidebar :is(.tenant,.nav-label,.nav-count,.subnav,.nav-favorite,.nav-section-label,.menu-filter)",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1023px)"), gwccss.Raw("display", "none!important")),
	)
	declareGlobal(tabletRail+" .sidebar :is(.nav-link,.nav-group-summary)",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1023px)"), gwccss.Justify.Center, gwccss.Raw("padding-inline", "8px")),
	)
	declareGlobal(tabletRail+" .brand-cluster",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1023px)"), gwccss.GridCols(gwccss.TrackLen(gwccss.Px(32)), gwccss.TrackLen(gwccss.Px(32))), gwccss.Gap(gwccss.Px(4)), gwccss.Raw("padding-inline", "2px")),
	)
	declareGlobal(tabletRail+" .wordmark-label",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1023px)"), gwccss.Display.None),
	)
	declareGlobal(tabletRail+" .wordmark-mark",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1023px)"), gwccss.Raw("display", "grid!important"), gwccss.Raw("place-items", "center"), gwccss.W(gwccss.Px(34)), gwccss.H(gwccss.Px(34)), gwccss.Bg(gwccss.Var("accent")), gwccss.TextColor(gwccss.Var("on-brand"))),
	)

	// The favourite control remains a dark-surface control in dark mode. Its
	// hover and focus affordances are border/outline changes, never a pale fill.
	declareGlobal(":root[data-hcm-color-mode=\"dark\"] .nav-favorite",
		gwccss.Raw("background", "var(--hcm-color-surface)"), gwccss.Raw("border-color", "var(--hcm-color-surface)"),
	)
	declareGlobal(":root[data-hcm-color-mode=\"dark\"] .nav-favorite:hover",
		gwccss.Raw("background", "var(--hcm-color-surface)"), gwccss.Raw("border-color", "var(--hcm-hover-border)"),
	)
	declareGlobal(":root[data-hcm-color-mode=\"dark\"] .nav-favorite:focus-visible",
		gwccss.Raw("background", "var(--hcm-color-surface)"), gwccss.Raw("outline-color", "var(--hcm-color-focus)"),
	)

	// Browser-native dark checkboxes otherwise paint an opaque neutral fill;
	// keeping them transparent makes an unticked control distinct from disabled.
	declareGlobal(":root[data-hcm-color-mode=\"dark\"] :where(.app-shell,.jn-embedded) input[type=checkbox]:not(:checked)",
		gwccss.Raw("appearance", "none"), gwccss.W(gwccss.RawLength("1.125rem")), gwccss.H(gwccss.RawLength("1.125rem")),
		gwccss.Raw("background", "transparent"), gwccss.Raw("border", "1.5px solid var(--hcm-color-border)"), gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
	)

	// Anchor buttons are navigation, not selected tabs: no rest fill or focus
	// tint. Focus stays visible through the platform :focus-visible ring.
	declareGlobal(":where(.app-shell) a.button.secondary", gwccss.Raw("background", "transparent"))
	declareGlobal(":where(.app-shell) a.button.secondary:is(:hover,:active)", gwccss.Raw("background", "var(--hcm-hover-surface)"))
	declareGlobal(":where(.app-shell) a.button.secondary:focus-visible", gwccss.Raw("background", "transparent"))

	declareGlobal(".product-page-frame",
		gwccss.W(gwccss.Percent(100)), gwccss.Raw("max-inline-size", "1200px"), gwccss.MarginY(gwccss.Zero), gwccss.MarginX(gwccss.RawLength("auto")), gwccss.Display.Grid, gwccss.Gap(gwccss.RawLength("var(--hcm-space-3)")), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".product-page-frame-title-row",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Justify.Between, gwccss.Gap(gwccss.RawLength("var(--hcm-space-2)")), gwccss.Raw("flex-wrap", "wrap"), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".product-page-frame-title-row>h1", gwccss.Margin(gwccss.Zero), gwccss.MinWidth(gwccss.Zero))
	declareGlobal(".product-page-frame-actions", gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.RawLength("var(--hcm-space-1)")), gwccss.Raw("margin-inline-start", "auto"), gwccss.Raw("flex-wrap", "wrap"))
	declareGlobal(".product-page-frame-breadcrumbs", gwccss.MinWidth(gwccss.Zero))
	declareGlobal(".product-page-frame",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("padding-inline", "var(--hcm-space-2)")),
	)
	declareGlobal(".footer", mediaRule(gwccss.MaxW(760), gwccss.Raw("margin-inline", "var(--hcm-space-2)")))
}
