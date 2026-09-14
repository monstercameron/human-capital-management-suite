package productui

import (
	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

func historyTableStylesheet() string {
	return buildTypedSheet(declarehistoryTableStyles)
}

func declarehistoryTableStyles() {
	declareGlobal(".history-filter-controls",
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Px(210)), gwccss.Fr(1)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(140)), gwccss.Fr(.72)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(130)), gwccss.Fr(.62)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(145)), gwccss.Fr(.68)), gwccss.TrackLen(gwccss.RawLength("auto"))),
	)
	declareGlobal(".history-clear",
		gwccss.W(gwccss.RawLength("max-content")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(.8125)),
		gwccss.Raw("font-weight", "650"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".history-clear:hover",
		gwccss.Raw("text-decoration", "underline"),
	)
	declareGlobal(".history-columns,.history-row",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Px(185)), gwccss.Fr(1.05)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(170)), gwccss.Fr(1)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(165)), gwccss.Fr(.82)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(88)), gwccss.TrackLen(gwccss.RawLength("auto"))), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(108)), gwccss.TrackLen(gwccss.RawLength("auto")))),
		gwccss.Gap(gwccss.Px(16)),
		gwccss.Items.Center,
	)
	declareGlobal(".history-columns",
		gwccss.MinHeight(gwccss.Px(43)),
		gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Hex("f6f8f6")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.75)),
		gwccss.Raw("font-weight", "650"),
	)
	declareGlobal(".history-list",
		gwccss.Raw("border-top", "0"),
	)
	declareGlobal(".history-sort",
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".history-sort:hover,.history-sort.active",
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".history-sort.active",
		gwccss.Raw("font-weight", "750"),
	)
	declareGlobal(".history-record-heading",
		gwccss.Raw("text-align", "right"),
	)
	declareGlobal(".history-change",
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("overflow-wrap", "anywhere"),
		gwccss.FontSize(gwccss.Rem(.8125)),
	)
	declareGlobal(".history-mobile-label",
		gwccss.Display.None,
	)
	declareGlobal(".history-dates small",
		gwccss.Display.Block,
	)
	declareGlobal(".history-source",
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.Raw("font-weight", "650"),
	)
	declareGlobal(".history-row>.button",
		gwccss.Raw("justify-self", "end"),
	)
	declareGlobal(".history-filter-controls",
		mediaRule(gwccss.MaxW(1080), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Px(210)), gwccss.Fr(1)), gwccss.Repeat(3, gwccss.MinMax(gwccss.TrackLen(gwccss.Px(125)), gwccss.Fr(.7))))),
	)
	declareGlobal(".history-filter-controls>.button",
		mediaRule(gwccss.MaxW(1080), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1)))),
	)
	declareGlobal(".history-columns",
		mediaRule(gwccss.MaxW(1080), gwccss.Display.None),
	)
	declareGlobal(".history-row",
		mediaRule(gwccss.MaxW(1080), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
	declareGlobal(".history-change,.history-dates",
		mediaRule(gwccss.MaxW(1080), gwccss.GridColumn(gwccss.GridLineAt(1))),
	)
	declareGlobal(".history-mobile-label",
		mediaRule(gwccss.MaxW(1080), gwccss.Display.Block, gwccss.Raw("margin-bottom", "4px"), gwccss.FontSize(gwccss.Rem(.75)), gwccss.Raw("font-weight", "650")),
	)
	declareGlobal(".history-row>.status",
		mediaRule(gwccss.MaxW(1080), gwccss.GridColumn(gwccss.GridLineAt(2)), gwccss.GridRow(gwccss.GridLineAt(1))),
	)
	declareGlobal(".history-row>.button",
		mediaRule(gwccss.MaxW(1080), gwccss.GridColumn(gwccss.GridLineAt(2)), gwccss.GridRow(gwccss.GridRange(gwccss.GridLineAt(2), gwccss.GridLineAt(4)))),
	)
	declareGlobal(".history-filter-controls",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".history-filter-controls>.button",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(1))),
	)
	declareGlobal(".history-row",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1)), gwccss.Gap(gwccss.Px(10))),
	)
	declareGlobal(".history-change,.history-dates,.history-row>.status,.history-row>.button",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(1)), gwccss.GridRow(gwccss.GridAuto)),
	)
	declareGlobal(".history-row>.button",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("justify-self", "stretch"), gwccss.W(gwccss.Percent(100))),
	)
}

func profileDetailStylesheet() string {
	return buildTypedSheet(declareprofileDetailStyles)
}

func declareprofileDetailStyles() {
	declareGlobal(".person-detail-stack",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(16)),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".sensitive-details",
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".sensitive-summary",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(18)),
		gwccss.MinHeight(gwccss.Px(86)),
		gwccss.PaddingY(gwccss.Px(18)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".sensitive-summary::-webkit-details-marker",
		gwccss.Display.None,
	)
	declareGlobal(".sensitive-summary-chevron",
		gwccss.Order(3),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.W(gwccss.Px(18)), gwccss.H(gwccss.Px(18)),
		gwccss.Raw("flex", "none"),
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("transform")), gwccss.S(.15), gwccss.Ease),
	)
	declareGlobal(".sensitive-details[open]>.sensitive-summary .sensitive-summary-chevron",
		gwccss.Transform(gwccss.Rotate(gwccss.Deg(90))),
	)
	declareGlobal(".sensitive-summary:hover",
		gwccss.Bg(gwccss.Var("soft")),
	)
	declareGlobal(".sensitive-heading",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(12)),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".sensitive-heading h2,.sensitive-heading p",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".sensitive-heading h2",
		gwccss.FontSize(gwccss.Rem(1.125)),
	)
	declareGlobal(".sensitive-heading p",
		gwccss.Raw("margin-top", "3px"),
		gwccss.FontSize(gwccss.Rem(.8125)),
	)
	declareGlobal(".privacy-icon",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.Raw("flex", "none"),
		gwccss.W(gwccss.Px(36)),
		gwccss.H(gwccss.Px(36)),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(.55)),
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(1), gwccss.Hex("c6ddd5"))),
	)
	declareGlobal(".privacy-icon-glyph",
		gwccss.W(gwccss.Px(18)), gwccss.H(gwccss.Px(18)),
	)
	declareGlobal(".privacy-badge",
		gwccss.Raw("margin-left", "auto"),
		gwccss.Bg(gwccss.Hex("f2f4f3")),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".privacy-notice",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.RowGap(gwccss.Px(5)), gwccss.ColumnGap(gwccss.Px(12)),
		gwccss.Raw("margin", "0 22px 18px"),
		gwccss.PaddingY(gwccss.Px(13)), gwccss.PaddingX(gwccss.Px(14)),
		gwccss.BorderLeft(gwccss.Px(3), gwccss.Hex("9d6b16")),
		gwccss.Rounded(gwccss.RawLength("0 var(--radius) var(--radius) 0")),
		gwccss.Bg(gwccss.Var("warning-bg")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.8125)),
	)
	declareGlobal(".privacy-notice strong",
		gwccss.TextColor(gwccss.Var("warning")),
	)
	declareGlobal(".sensitive-fact-grid",
		gwccss.Raw("border-bottom", "0"),
	)
	declareGlobal(".sensitive-summary",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("align-items", "flex-start"), gwccss.Padding(gwccss.Px(17))),
	)
	declareGlobal(".privacy-badge",
		mediaRule(gwccss.MaxW(760), gwccss.Display.None),
	)
	declareGlobal(".privacy-notice",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1)), gwccss.Raw("margin-inline", "17px")),
	)
	declareGlobal(".sensitive-heading",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("align-items", "flex-start")),
	)
}

func workflowHistoryStylesheet() string {
	return buildTypedSheet(declareworkflowHistoryStyles)
}

func declareworkflowHistoryStyles() {
	declareGlobal(".workflow-history",
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".history-heading",
		gwccss.Raw("align-items", "flex-start"),
	)
	declareGlobal(".history-heading h2",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".history-heading p",
		gwccss.Raw("margin", "3px 0 0"),
	)
	declareGlobal(".history-filter",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(7)),
		gwccss.Raw("padding", "0 22px 18px"),
	)
	declareGlobal(".history-filter>label",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.75)),
		gwccss.Raw("font-weight", "650"),
	)
	declareGlobal(".history-filter-controls",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Px(220)), gwccss.Fr(1)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(160)), gwccss.TrackLen(gwccss.Px(220))), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.Gap(gwccss.Px(9)),
	)
	declareGlobal(".history-filter input,.history-filter select",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(9)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Border(gwccss.Px(1), gwccss.Hex("7b8997")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Hex("fff")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".history-list",
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".history-row",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Px(220)), gwccss.Fr(1.4)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(210)), gwccss.Fr(.9)), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.Gap(gwccss.Px(18)),
		gwccss.Items.Center,
		gwccss.PaddingY(gwccss.Px(19)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Hex("e8ede9")),
	)
	declareGlobal(".history-row:last-child",
		gwccss.Raw("border-bottom", "0"),
	)
	declareGlobal(".history-row:hover",
		gwccss.Bg(gwccss.Hex("fcfcfa")),
	)
	declareGlobal(".history-record",
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".history-record h3",
		gwccss.MarginY(gwccss.Px(2)), gwccss.MarginX(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(1)),
	)
	declareGlobal(".history-record p",
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("overflow-wrap", "anywhere"),
		gwccss.FontSize(gwccss.Rem(.8125)),
	)
	declareGlobal(".history-dates",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(4)),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".history-empty",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(5)),
		gwccss.PaddingY(gwccss.Px(28)), gwccss.PaddingX(gwccss.Px(22)),
	)
	declareGlobal(".history-empty p",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".history-row",
		mediaRule(gwccss.MaxW(980), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
	declareGlobal(".history-dates",
		mediaRule(gwccss.MaxW(980), gwccss.GridColumn(gwccss.GridLineAt(1))),
	)
	declareGlobal(".history-row>.status",
		mediaRule(gwccss.MaxW(980), gwccss.GridColumn(gwccss.GridLineAt(2)), gwccss.GridRow(gwccss.GridLineAt(1))),
	)
	declareGlobal(".history-row>.button",
		mediaRule(gwccss.MaxW(980), gwccss.GridColumn(gwccss.GridLineAt(2)), gwccss.GridRow(gwccss.GridLineAt(2))),
	)
	declareGlobal(".history-filter-controls",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".history-filter-controls .button",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal(".history-row",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1)), gwccss.Gap(gwccss.Px(12))),
	)
	declareGlobal(".history-dates,.history-row>.status,.history-row>.button",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(1)), gwccss.GridRow(gwccss.GridAuto)),
	)
	declareGlobal(".history-row>.button",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.Percent(100))),
	)
}

func peopleDirectoryStylesheet() string {
	return buildTypedSheet(declarepeopleDirectoryStyles)
}

func declarepeopleDirectoryStyles() {
	declareGlobal(".people-page",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(16)),
	)
	declareGlobal(".people-page .directory-tools",
		gwccss.Raw("margin-bottom", "0"),
	)
	declareGlobal(".people-filter",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(7)),
		gwccss.PaddingY(gwccss.Px(16)), gwccss.PaddingX(gwccss.Px(18)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("panel")),
		gwccss.Bg(gwccss.Hex("fff")),
	)
	declareGlobal(".people-filter label",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.75)),
		gwccss.Raw("font-weight", "650"),
	)
	declareGlobal(".people-filter-control",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.Gap(gwccss.Px(10)),
	)
	declareGlobal(".people-filter input",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(13)),
		gwccss.Border(gwccss.Px(1), gwccss.Hex("7b8997")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Hex("fff")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".people-filter-actions",
		gwccss.Display.Flex,
		gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".people-directory",
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".people-pager",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(16)),
		gwccss.MinHeight(gwccss.Px(68)),
		gwccss.PaddingY(gwccss.Px(11)), gwccss.PaddingX(gwccss.Px(17)),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Hex("fcfcfa")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.8125)),
	)
	declareGlobal(".pager-status",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(9)),
	)
	declareGlobal(".people-pager .button",
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(7)), gwccss.PaddingX(gwccss.Px(12)),
	)
	declareGlobal(".button.disabled",
		gwccss.BorderColor(gwccss.Var("line")),
		gwccss.Bg(gwccss.Hex("f3f5f3")),
		gwccss.TextColor(gwccss.Hex("8b9690")),
		gwccss.Raw("cursor", "not-allowed"),
		gwccss.OpacityNum(gwccss.Num(.78)),
	)
	declareGlobal(".people-filter-control",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".people-filter-actions",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Grid, gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1))),
	)
	declareGlobal(".people-filter-actions .button:only-child",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1)))),
	)
	declareGlobal(".people-pager",
		mediaRule(gwccss.MaxW(760), gwccss.Items.Stretch, gwccss.FlexDir.Col),
	)
	declareGlobal(".pager-status",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Grid, gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1))),
	)
	declareGlobal(".pager-status>span:first-child",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1)))),
	)
	declareGlobal(".people-pager .button",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.Percent(100))),
	)
}

func personProfileStylesheet() string {
	return buildTypedSheet(declarepersonProfileStyles)
}

func declarepersonProfileStyles() {
	declareGlobal(".person-page",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(18)),
	)
	declareGlobal(".back-link",
		gwccss.W(gwccss.RawLength("max-content")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(.875)),
		gwccss.Raw("font-weight", "650"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".back-link:hover",
		gwccss.Raw("text-decoration", "underline"),
	)
	declareGlobal(".person-hero",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(24)),
		gwccss.PaddingY(gwccss.Px(26)), gwccss.PaddingX(gwccss.Px(28)),
		gwccss.Raw("background", "linear-gradient(120deg,var(--soft),#fff 70%)"),
	)
	declareGlobal(".person-identity",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(16)),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".avatar.profile",
		gwccss.W(gwccss.Px(64)),
		gwccss.H(gwccss.Px(64)),
		gwccss.BorderColor(gwccss.Hex("b6d7ca")),
		gwccss.FontSize(gwccss.Rem(1)),
	)
	declareGlobal(".person-identity h2",
		gwccss.MarginY(gwccss.Px(2)), gwccss.MarginX(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(1.5)),
		gwccss.Tracking(gwccss.Ems(-.025)),
	)
	declareGlobal(".person-identity p",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".eyebrow",
		gwccss.Display.Block,
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(.7)),
		gwccss.Raw("font-weight", "750"),
		gwccss.Tracking(gwccss.Ems(.08)),
		gwccss.Raw("text-transform", "uppercase"),
	)
	declareGlobal(".person-hero-meta",
		gwccss.Display.Grid,
		gwccss.Raw("justify-items", "end"),
		gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".person-layout",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1.12)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(360)), gwccss.Fr(.88))),
		gwccss.Gap(gwccss.Px(18)),
		gwccss.Raw("align-items", "start"),
	)
	declareGlobal(".person-details,.workflow-launcher",
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".person-details .section-head h2,.workflow-heading h2",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".person-details .section-head p,.workflow-heading p",
		gwccss.Raw("margin", "3px 0 0"),
	)
	declareGlobal(".person-fact-grid",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".profile-fact",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(5)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.PaddingY(gwccss.Px(17)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".profile-fact:nth-child(odd)",
		gwccss.BorderRight(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".profile-fact small",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.75)),
	)
	declareGlobal(".profile-fact strong",
		gwccss.Raw("overflow-wrap", "anywhere"),
		gwccss.FontSize(gwccss.Rem(.9)),
	)
	declareGlobal(".profile-missing-summary",
		gwccss.Display.Flex,
		gwccss.Raw("align-items", "center"),
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(12)),
		gwccss.PaddingY(gwccss.Px(15)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("cursor", "pointer"),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".profile-missing-summary::-webkit-details-marker",
		gwccss.Display.None,
	)
	declareGlobal(".profile-missing-summary:focus-visible",
		gwccss.Raw("outline", "2px solid var(--hcm-color-focus)"),
		gwccss.OutlineOffset(gwccss.Px(-3)),
	)
	declareGlobal(".profile-missing-chevron",
		gwccss.Raw("width", "16px"), gwccss.Raw("height", "16px"),
		gwccss.Raw("flex", "none"),
	)
	declareGlobal(".profile-missing-details[open] .profile-missing-chevron",
		gwccss.Raw("transform", "rotate(90deg)"),
	)
	declareGlobal("[dir=rtl] .profile-missing-summary .profile-missing-chevron",
		gwccss.Raw("transform", "scaleX(-1)"),
	)
	declareGlobal("[dir=rtl] .profile-missing-details[open] .profile-missing-chevron",
		gwccss.Raw("transform", "rotate(90deg)"),
	)
	declareGlobal(".workflow-launcher",
		gwccss.Position.Sticky,
		gwccss.Top(gwccss.Px(20)),
	)
	declareGlobal(".workflow-heading",
		gwccss.Raw("align-items", "flex-start"),
	)
	declareGlobal(".workflow-search",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(7)),
		gwccss.Raw("padding", "0 22px 18px"),
	)
	declareGlobal(".workflow-search label",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.75)),
		gwccss.Raw("font-weight", "650"),
	)
	declareGlobal(".workflow-search-control",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".workflow-search input",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Border(gwccss.Px(1), gwccss.Hex("7b8997")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Hex("fff")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".workflow-results",
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".workflow-card",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.Gap(gwccss.Px(14)),
		gwccss.Items.Center,
		gwccss.PaddingY(gwccss.Px(20)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".workflow-card:last-child",
		gwccss.Raw("border-bottom", "0"),
	)
	declareGlobal(".workflow-icon",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.W(gwccss.Px(40)),
		gwccss.H(gwccss.Px(40)),
		gwccss.Rounded(gwccss.Px(10)),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.Raw("font-weight", "750"),
	)
	declareGlobal(".workflow-icon-glyph",
		gwccss.Raw("width", "20px"), gwccss.Raw("height", "20px"),
	)
	declareGlobal(".workflow-copy",
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".workflow-copy h3",
		gwccss.MarginY(gwccss.Px(2)), gwccss.MarginX(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(1)),
	)
	declareGlobal(".workflow-copy p",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(.8125)),
	)
	declareGlobal(".workflow-empty",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(6)),
		gwccss.PaddingY(gwccss.Px(26)), gwccss.PaddingX(gwccss.Px(22)),
	)
	declareGlobal(".workflow-empty p",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".person-layout",
		mediaRule(gwccss.MaxW(1040), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".workflow-launcher",
		mediaRule(gwccss.MaxW(1040), gwccss.Position.Static),
	)
	declareGlobal(".person-hero",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("align-items", "flex-start"), gwccss.FlexDir.Col, gwccss.Padding(gwccss.Px(21))),
	)
	declareGlobal(".person-hero-meta",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("justify-items", "start")),
	)
	declareGlobal(".person-fact-grid",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".profile-fact:nth-child(odd)",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("border-right", "0")),
	)
	declareGlobal(".workflow-card",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".workflow-card>.button",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1)))),
	)
	declareGlobal(".workflow-search-control",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".workflow-search-control .button",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.Percent(100))),
	)
}

func liveDataRefinementsStylesheet() string {
	return buildTypedSheet(declareliveDataRefinementsStyles)
}

func declareliveDataRefinementsStyles() {
	declareGlobal(".notifications summary:after",
		gwccss.Display.None,
	)
	declareGlobal(".page-stack",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(18)),
	)
}

func viewportShellStylesheet() string {
	return buildTypedSheet(declareviewportShellStyles)
}

func declareviewportShellStyles() {
	declareGlobal("html,body,#app",
		gwccss.W(gwccss.Percent(100)),
		gwccss.H(gwccss.Percent(100)),
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal("body",
		gwccss.Raw("overscroll-behavior", "none"),
	)
	declareGlobal(".app-shell",
		gwccss.Display.Grid,
		gwccss.GridRows(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.W(gwccss.Percent(100)),
		gwccss.H(gwccss.Vh(100)),
		gwccss.Raw("overflow", "hidden"),
	)
	// Split fallback: one Global call keeps last-wins per property, so the
	// 100dvh progressive enhancement ships in its own block; cascade order
	// preserves the original fallback sequence (100vh, then 100dvh).
	declareGlobal(".app-shell",
		gwccss.H(gwccss.RawLength("100dvh")),
	)
	declareGlobal(".topbar",
		gwccss.Position.Relative,
		gwccss.Top(gwccss.RawLength("auto")),
	)
	declareGlobal(".shell-grid",
		gwccss.MinHeight(gwccss.Zero),
		gwccss.H(gwccss.Percent(100)),
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".sidebar",
		gwccss.MinHeight(gwccss.Zero),
		gwccss.H(gwccss.Percent(100)),
		gwccss.Raw("overflow-y", "auto"),
		gwccss.Raw("overscroll-behavior", "contain"),
	)
	declareGlobal(".main-scroll",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.MinHeight(gwccss.Zero),
		gwccss.H(gwccss.Percent(100)),
		gwccss.Raw("overflow-x", "hidden"),
		gwccss.Raw("overflow-y", "auto"),
		gwccss.Raw("overscroll-behavior-y", "contain"),
		gwccss.Raw("scrollbar-gutter", "stable"),
		gwccss.Bg(gwccss.Var("canvas")),
	)
	declareGlobal(".work-preview",
		gwccss.Top(gwccss.Px(20)),
	)
	declareGlobal(".shell-grid",
		mediaRule(gwccss.MaxW(760), gwccss.GridRows(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".sidebar,.sidebar.collapsed",
		mediaRule(gwccss.MaxW(760), gwccss.H(gwccss.RawLength("auto")), gwccss.Raw("overflow-y", "visible")),
	)
	declareGlobal(".main-scroll",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("scrollbar-gutter", "auto")),
	)
	declareGlobal("html,body,#app,.app-shell,.shell-grid",
		mediaRule(gwccss.RawMedia("print"), gwccss.H(gwccss.RawLength("auto!important")), gwccss.Raw("overflow", "visible!important")),
	)
	declareGlobal(".main-scroll,.sidebar",
		mediaRule(gwccss.RawMedia("print"), gwccss.H(gwccss.RawLength("auto!important")), gwccss.Raw("overflow", "visible!important")),
	)
}

func responsiveSafetyStylesheet() string {
	return buildTypedSheet(declareresponsiveSafetyStyles)
}

func declareresponsiveSafetyStyles() {
	declareGlobal("html,body,.app-shell,.shell-grid,.sidebar,.main",
		mediaRule(gwccss.MaxW(760), gwccss.MinWidth(gwccss.Zero), gwccss.MaxWidth(gwccss.Percent(100))),
	)
	declareGlobal(".topbar,.global-search",
		mediaRule(gwccss.MaxW(760), gwccss.MinWidth(gwccss.Zero), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal(".sidebar nav:first-of-type",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.Percent(100)), gwccss.MinWidth(gwccss.Zero), gwccss.MaxWidth(gwccss.Percent(100)), gwccss.Raw("overflow-x", "auto"), gwccss.Raw("overscroll-behavior-inline", "contain")),
	)
	declareGlobal(".sidebar nav:first-of-type>ul",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.RawLength("max-content")), gwccss.MaxWidth(gwccss.RawLength("none"))),
	)
	declareGlobal(".work-row,.row-main,.people-row>span,.facts strong,.studio-canvas,.mini-page",
		mediaRule(gwccss.MaxW(760), gwccss.MinWidth(gwccss.Zero)),
	)
	declareGlobal(".row-main,.people-row>span,.facts strong",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("overflow-wrap", "anywhere")),
	)
}

func componentRefinementsStylesheet() string {
	return buildTypedSheet(declarecomponentRefinementsStyles)
}

func declarecomponentRefinementsStyles() {
	declareGlobal(".topbar",
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(232)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(220)), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto"))),
	)
	declareGlobal(".subnav",
		gwccss.Raw("margin", "0 0 6px 16px!important"),
		gwccss.Raw("padding-left", "10px!important"),
		gwccss.BorderLeft(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".subnav-link",
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("padding-block", "7px"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.78)),
	)
	declareGlobal(".collection-empty",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(4)),
		gwccss.PaddingY(gwccss.Px(28)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".collection-empty small",
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".support-summary",
		gwccss.Raw("padding", "0 22px 22px"),
	)
	declareGlobal(".support-request",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(16)),
		gwccss.MaxWidth(gwccss.Px(760)),
		gwccss.Padding(gwccss.Px(26)),
	)
	declareGlobal(".support-request h2,.support-request p",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".support-form",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(14)),
	)
	declareGlobal(".support-form label",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(6)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.875)),
	)
	declareGlobal(".support-form input,.support-form textarea,.support-form select",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(9)), gwccss.PaddingX(gwccss.Px(11)),
		gwccss.Border(gwccss.Px(1), gwccss.Hex("7b8997")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Hex("fff")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".support-form textarea",
		gwccss.Raw("resize", "vertical"),
	)
	declareGlobal(".support-form .button",
		gwccss.Raw("justify-self", "start"),
	)
	declareGlobal(".settings-anchor",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(3)),
		gwccss.Padding(gwccss.Px(12)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Raw("scroll-margin-top", "100px"),
	)
	declareGlobal(".settings-anchor span",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.8125)),
	)
	declareGlobal(".topbar",
		mediaRule(gwccss.MaxW(1190), gwccss.GridCols(gwccss.TrackLen(gwccss.Px(210)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(180)), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
	declareGlobal(".topbar",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
}

func refinementsStylesheet() string {
	return buildTypedSheet(declarerefinementsStyles)
}

func declarerefinementsStyles() {
	declareGlobal(".empty-state",
		gwccss.Display.Grid,
		gwccss.Raw("justify-items", "start"),
		gwccss.Gap(gwccss.Px(8)),
		gwccss.Padding(gwccss.Px(34)),
	)
	declareGlobal(".empty-state h2,.empty-state p",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".studio-page",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(18)),
	)
	declareGlobal(".studio-toolbar,.studio-actions",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(10)),
	)
	declareGlobal(".studio-actions",
		gwccss.Raw("justify-content", "flex-end"),
	)
	declareGlobal(".studio-notice",
		gwccss.Margin(gwccss.Zero),
		gwccss.PaddingY(gwccss.Px(12)), gwccss.PaddingX(gwccss.Px(16)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.BorderLeft(gwccss.Px(4), gwccss.Var("accent")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Hex("fff")),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".studio-notice.success",
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".studio-notice.warning",
		gwccss.Raw("border-left-color", "#bc7a16"),
		gwccss.Bg(gwccss.Var("warning-bg")),
		gwccss.TextColor(gwccss.Var("warning")),
	)
	declareGlobal(".studio-shell",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(170)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(225)), gwccss.Fr(.72)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(320)), gwccss.Fr(1.45)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(220)), gwccss.Fr(.78))),
		gwccss.MinHeight(gwccss.Px(590)),
		gwccss.Raw("overflow", "hidden"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("panel")),
		gwccss.Bg(gwccss.Hex("fff")),
	)
	declareGlobal(".studio-nav,.studio-structure,.studio-inspector",
		gwccss.Padding(gwccss.Px(20)),
	)
	declareGlobal(".studio-nav",
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Px(4)),
		gwccss.BorderRight(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Hex("f7f9f7")),
	)
	declareGlobal(".studio-nav small",
		gwccss.Raw("margin", "14px 9px 5px"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("font-weight", "700"),
		gwccss.Tracking(gwccss.Ems(.08)),
	)
	declareGlobal(".studio-nav a",
		gwccss.Padding(gwccss.Px(9)),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.FontSize(gwccss.Rem(.8125)),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".studio-nav a:hover,.studio-nav a.active",
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.Raw("font-weight", "650"),
	)
	declareGlobal(".studio-structure",
		gwccss.BorderRight(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".studio-section-head",
		gwccss.Display.Flex,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(10)),
		gwccss.Raw("margin-bottom", "18px"),
	)
	declareGlobal(".studio-section-head h2",
		gwccss.MarginY(gwccss.Px(3)), gwccss.MarginX(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(1.05)),
	)
	declareGlobal(".studio-region",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(9)),
		gwccss.MinHeight(gwccss.Px(64)),
		gwccss.Raw("margin-bottom", "9px"),
		gwccss.Padding(gwccss.Px(10)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".studio-region:hover",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Bg(gwccss.Var("soft")),
	)
	declareGlobal(".studio-region>.drag-handle",
		gwccss.TextColor(gwccss.Hex("93a09a")),
	)
	declareGlobal(".studio-region>small",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.7)),
	)
	declareGlobal(".studio-canvas",
		gwccss.Padding(gwccss.Px(24)),
		gwccss.Bg(gwccss.Hex("f0f3f0")),
	)
	declareGlobal(".canvas-bar",
		gwccss.Display.Flex,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Raw("margin-bottom", "12px"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.75)),
	)
	declareGlobal(".mini-page",
		gwccss.MinHeight(gwccss.Px(435)),
		gwccss.Padding(gwccss.Px(24)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Var("canvas")),
		gwccss.Shadow(gwccss.ShadowOf(gwccss.Zero, gwccss.Px(12), gwccss.Px(30), gwccss.Zero, gwccss.Hex("10223812"))),
	)
	declareGlobal(".mini-head",
		gwccss.Display.Flex,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(15)),
		gwccss.Raw("margin-bottom", "22px"),
	)
	declareGlobal(".mini-head h2",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(1.35)),
	)
	declareGlobal(".mini-head p",
		gwccss.MarginY(gwccss.Px(3)), gwccss.MarginX(gwccss.Zero),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.75)),
	)
	declareGlobal(".mini-scope",
		gwccss.H(gwccss.RawLength("max-content")),
		gwccss.PaddingY(gwccss.Px(6)), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.Px(5)),
		gwccss.Bg(gwccss.Hex("fff")),
		gwccss.FontSize(gwccss.Rem(.7)),
	)
	declareGlobal(".mini-grid",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Fr(1.5), gwccss.Fr(1)),
		gwccss.Gap(gwccss.Px(12)),
	)
	declareGlobal(".mini-card",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(7)),
		gwccss.MinHeight(gwccss.Px(125)),
		gwccss.Padding(gwccss.Px(15)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.Bg(gwccss.Hex("fff")),
	)
	declareGlobal(".mini-card.selected",
		gwccss.Raw("outline", "2px solid var(--accent)"),
		gwccss.OutlineOffset(gwccss.Px(2)),
	)
	declareGlobal(".mini-card.wide",
		gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
		gwccss.MinHeight(gwccss.Px(100)),
	)
	declareGlobal(".mini-card small,.mini-card span",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.7)),
	)
	declareGlobal(".studio-inspector",
		gwccss.BorderLeft(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".studio-inspector h2",
		gwccss.MarginY(gwccss.Px(3)), gwccss.MarginX(gwccss.Zero),
	)
	declareGlobal(".studio-inspector h3",
		gwccss.Raw("margin-top", "22px"),
	)
	declareGlobal(".swatches",
		gwccss.Display.Flex,
		gwccss.Gap(gwccss.Px(9)),
	)
	declareGlobal(".swatch",
		gwccss.W(gwccss.Px(34)),
		gwccss.H(gwccss.Px(34)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.Percent(50)),
	)
	declareGlobal(".swatch.accent",
		gwccss.Bg(gwccss.Var("accent")),
	)
	declareGlobal(".swatch.ink",
		gwccss.Bg(gwccss.Var("ink")),
	)
	declareGlobal(".swatch.canvas",
		gwccss.Bg(gwccss.Var("canvas")),
	)
	declareGlobal(".studio-governance",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(3, gwccss.Fr(1))),
		gwccss.Gap(gwccss.Px(14)),
	)
	declareGlobal(".governance-card",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Fr(1), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.Gap(gwccss.Px(10)),
		gwccss.Padding(gwccss.Px(18)),
	)
	declareGlobal(".governance-card h3,.governance-card p",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".governance-card a",
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(.8125)),
		gwccss.Raw("font-weight", "650"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".studio-shell",
		mediaRule(gwccss.MaxW(1320), gwccss.GridCols(gwccss.TrackLen(gwccss.Px(150)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(210)), gwccss.Fr(.72)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(330)), gwccss.Fr(1.35)))),
	)
	declareGlobal(".studio-inspector",
		mediaRule(gwccss.MaxW(1320), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(2), gwccss.GridLineAt(-1))), gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")), gwccss.Raw("border-left", "0")),
	)
	declareGlobal(".studio-inspector .facts",
		mediaRule(gwccss.MaxW(1320), gwccss.Display.Grid, gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1)), gwccss.RowGap(gwccss.Zero), gwccss.ColumnGap(gwccss.Px(20))),
	)
	declareGlobal(".studio-shell",
		mediaRule(gwccss.MaxW(980), gwccss.GridCols(gwccss.TrackLen(gwccss.Px(150)), gwccss.Fr(1))),
	)
	declareGlobal(".studio-canvas",
		mediaRule(gwccss.MaxW(980), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1)))),
	)
	declareGlobal(".studio-inspector",
		mediaRule(gwccss.MaxW(980), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1)))),
	)
	declareGlobal(".studio-governance",
		mediaRule(gwccss.MaxW(980), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".studio-toolbar",
		mediaRule(gwccss.MaxW(980), gwccss.Items.Stretch, gwccss.FlexDir.Col),
	)
	declareGlobal(".studio-actions",
		mediaRule(gwccss.MaxW(980), gwccss.Raw("flex-wrap", "wrap"), gwccss.Raw("justify-content", "flex-start")),
	)
	declareGlobal(".studio-shell",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".studio-nav",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("border-right", "0"), gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line"))),
	)
	declareGlobal(".studio-structure",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("border-right", "0")),
	)
	declareGlobal(".studio-actions",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Grid, gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1))),
	)
	declareGlobal(".studio-actions .primary",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1)))),
	)
	declareGlobal(".mini-grid",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".mini-card.wide",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(1))),
	)
	declareGlobal(".studio-inspector .facts",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Block),
	)
}

func responsiveGridFixStylesheet() string {
	return buildTypedSheet(declareresponsiveGridFixStyles)
}

func declareresponsiveGridFixStyles() {
	declareGlobal(".shell-grid",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1190px)"), gwccss.GridCols(gwccss.TrackLen(gwccss.Px(210)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".people-workspace",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1190px)"), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".person-context",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1190px)"), gwccss.Raw("border-left", "0"), gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line"))),
	)
}

func collapsibleNavigationStylesheet() string {
	return buildTypedSheet(declarecollapsibleNavigationStyles)
}

func declarecollapsibleNavigationStyles() {
	declareGlobal(".shell-grid",
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(232)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("grid-template-columns")), gwccss.S(.18), gwccss.Ease),
	)
	declareGlobal(".sidebar",
		gwccss.W(gwccss.Px(232)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("width")), gwccss.S(.18), gwccss.Ease),
	)
	declareGlobal(".nav-icon",
		gwccss.Display.Block,
		gwccss.W(gwccss.Px(20)),
		gwccss.H(gwccss.Px(20)),
		gwccss.Raw("flex", "none"),
	)
	declareGlobal(".sidebar-toggle",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(10)),
		gwccss.MinHeight(gwccss.Px(42)),
		gwccss.Raw("margin", "0 5px 14px"),
		gwccss.PaddingY(gwccss.Px(9)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("radius")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(.75)),
		gwccss.Raw("font-weight", "650"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".sidebar-toggle:hover",
		gwccss.BorderColor(gwccss.Hex("9eb5ac")),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".wordmark-mark",
		gwccss.Display.None,
	)
	declareGlobal(".app-shell.nav-collapsed .topbar",
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(72)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(220)), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto"))),
	)
	declareGlobal(".app-shell.nav-collapsed .wordmark",
		gwccss.Justify.Center,
		gwccss.Padding(gwccss.Zero),
		gwccss.BorderRight(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".app-shell.nav-collapsed .wordmark:before",
		gwccss.Display.None,
	)
	declareGlobal(".app-shell.nav-collapsed .wordmark-label",
		gwccss.Display.None,
	)
	declareGlobal(".app-shell.nav-collapsed .wordmark-mark",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.W(gwccss.Px(34)),
		gwccss.H(gwccss.Px(34)),
		gwccss.Rounded(gwccss.Px(9)),
		gwccss.Bg(gwccss.Var("accent")),
		gwccss.TextColor(gwccss.Hex("fff")),
		gwccss.FontSize(gwccss.Rem(1)),
		gwccss.Tracking(gwccss.Zero),
	)
	declareGlobal(".app-shell.nav-collapsed .shell-grid",
		gwccss.GridCols(gwccss.TrackLen(gwccss.Px(72)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
	)
	declareGlobal(".sidebar.collapsed",
		gwccss.W(gwccss.Px(72)),
		gwccss.Raw("padding-inline", "9px"),
	)
	declareGlobal(".sidebar.collapsed .tenant,.sidebar.collapsed .nav-label,.sidebar.collapsed .nav-count,.sidebar.collapsed .subnav",
		gwccss.Raw("display", "none!important"),
	)
	declareGlobal(".sidebar.collapsed .sidebar-toggle,.sidebar.collapsed .nav-link",
		gwccss.Justify.Center,
		gwccss.Raw("padding-inline", "8px"),
	)
	declareGlobal(".sidebar.collapsed .nav-link[aria-current=page]",
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("accent"))),
	)
	declareGlobal(".sidebar.collapsed .nav-bottom",
		gwccss.Display.Grid,
	)
	declareGlobal(".studio-section-head",
		gwccss.Raw("align-items", "flex-start"),
	)
	declareGlobal(".studio-section-head>.status",
		gwccss.Raw("flex", "none"),
	)
	declareGlobal(".shell-grid",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1190px)"), gwccss.GridCols(gwccss.TrackLen(gwccss.Px(210)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".sidebar",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1190px)"), gwccss.W(gwccss.Px(210))),
	)
	declareGlobal(".app-shell.nav-collapsed .shell-grid",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1190px)"), gwccss.GridCols(gwccss.TrackLen(gwccss.Px(72)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".app-shell.nav-collapsed .sidebar",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1190px)"), gwccss.W(gwccss.Px(72))),
	)
	declareGlobal(".app-shell.nav-collapsed .topbar",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1), gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
	declareGlobal(".app-shell.nav-collapsed .wordmark",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("justify-content", "flex-start"), gwccss.Raw("padding-left", "16px"), gwccss.Raw("border", "0")),
	)
	declareGlobal(".app-shell.nav-collapsed .wordmark-mark",
		mediaRule(gwccss.MaxW(760), gwccss.Display.None),
	)
	declareGlobal(".app-shell.nav-collapsed .wordmark-label",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Inline),
	)
	declareGlobal(".app-shell.nav-collapsed .shell-grid",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".sidebar,.sidebar.collapsed",
		mediaRule(gwccss.MaxW(760), gwccss.W(gwccss.Percent(100)), gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(14))),
	)
	declareGlobal(".sidebar-toggle",
		mediaRule(gwccss.MaxW(760), gwccss.Display.None),
	)
	declareGlobal(".sidebar.collapsed .nav-label,.sidebar.collapsed .nav-count",
		mediaRule(gwccss.MaxW(760), gwccss.Display.Inline),
	)
	declareGlobal(".sidebar.collapsed .nav-link",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("justify-content", "flex-start"), gwccss.Raw("padding-inline", "13px")),
	)
	declareGlobal(".sidebar.collapsed .nav-bottom",
		mediaRule(gwccss.MaxW(760), gwccss.Display.None),
	)
}
