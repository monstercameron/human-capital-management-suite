package productui

import (
	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

func visualQARefinementsStylesheet() string {
	return buildTypedSheet(declarevisualQARefinementsStyles)
}

func declarevisualQARefinementsStyles() {
	declareGlobal(".brand-logo-fallback",
		gwccss.Raw("direction", "ltr"),
	)
	declareGlobal(".studio-back-link",
		gwccss.Display.InlineFlex,
		gwccss.W(gwccss.RawLength("max-content")),
		gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(5)), gwccss.PaddingX(gwccss.Px(2)),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "700"),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".studio-back-link:hover",
		gwccss.Raw("text-decoration", "underline"),
	)
	declareGlobal(".home-grid-without-work",
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
	)
	declareGlobal(".home-grid-without-work .side-stack",
		gwccss.Raw("grid-template-columns", "repeat(auto-fit,minmax(260px,1fr))"),
	)
	declareGlobal(".home-card-empty",
		gwccss.Raw("border-bottom", "0"),
		gwccss.Raw("padding-block", "18px"),
	)
	declareGlobal(".recent-people-intro",
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("padding-block", "0 calc(var(--hcm-space-2) * var(--hcm-density))"),
		gwccss.Raw("padding-inline", "calc(var(--hcm-space-3) * var(--hcm-density))"),
	)
	declareGlobal(".work-list .section-head p",
		gwccss.Raw("margin", "6px 0 0"),
	)
	declareGlobal(".work-row",
		mediaRule(gwccss.MaxW(420), gwccss.Raw("grid-template-columns", "minmax(0,1fr) auto")),
	)
	declareGlobal(".work-row>.row-main",
		mediaRule(gwccss.MaxW(420), gwccss.Raw("grid-column", "1 / -1")),
	)
	declareGlobal(".work-row>.row-end",
		mediaRule(gwccss.MaxW(420), gwccss.Raw("grid-column", "1"), gwccss.Raw("max-width", "none")),
	)
	declareGlobal(".work-row>.work-row-chevron",
		mediaRule(gwccss.MaxW(420), gwccss.Raw("grid-column", "2"), gwccss.Raw("grid-row", "2"), gwccss.Raw("align-self", "center")),
	)
	declareGlobal(".subnav .nav-copy>.nav-label",
		gwccss.Raw("display", "-webkit-box"),
		gwccss.MaxWidth(gwccss.RawLength("none")),
		gwccss.Raw("overflow", "hidden"),
		gwccss.Raw("white-space", "normal"),
		gwccss.Raw("text-overflow", "clip"),
		gwccss.Raw("-webkit-box-orient", "vertical"),
		gwccss.Raw("-webkit-line-clamp", "2"),
	)
	declareGlobal(".subnav .nav-link",
		gwccss.MinHeight(gwccss.Px(46)),
		gwccss.H(gwccss.RawLength("auto")),
	)
	declareGlobal(".nav-link.has-search-detail .nav-copy>.nav-label",
		gwccss.Raw("white-space", "normal"),
		gwccss.Raw("text-overflow", "clip"),
	)
	declareGlobal(".sidebar .nav-copy>.nav-label",
		mediaRule(gwccss.MinW(761),
			gwccss.Raw("white-space", "normal"),
			gwccss.Raw("overflow-wrap", "normal"),
			gwccss.Raw("word-break", "normal"),
			gwccss.Raw("hyphens", "none"),
		),
	)
	declareGlobal(".global-search .global-search-input",
		gwccss.Raw("padding-block", "10px"),
		gwccss.Raw("padding-inline", "48px 14px"),
	)
	declareGlobal(".global-search-glyph",
		gwccss.W(gwccss.Px(20)),
		gwccss.Raw("text-align", "center"),
	)
	declareGlobal(".people-page",
		gwccss.Gap(gwccss.RawLength("calc(var(--hcm-space-3) * var(--hcm-density))")),
	)
	declareGlobal(".people-filter",
		gwccss.PaddingY(gwccss.RawLength("calc(var(--hcm-space-2) * var(--hcm-density))")),
		gwccss.PaddingX(gwccss.RawLength("calc(var(--hcm-space-2) * var(--hcm-density))")),
	)
	declareGlobal(".people-table .data-table-row",
		mediaRule(gwccss.MaxW(1050),
			gwccss.Gap(gwccss.RawLength("calc(var(--hcm-space-1) * var(--hcm-density))")),
			gwccss.Padding(gwccss.RawLength("calc(var(--hcm-space-2) * var(--hcm-density))")),
		),
	)
	declareGlobal(".people-table .data-table-cell",
		mediaRule(gwccss.MaxW(1050),
			gwccss.Gap(gwccss.RawLength("calc(var(--hcm-space-2) * var(--hcm-density))")),
		),
	)
	declareGlobal(".brand-logo-slot[data-hcm-brand-logo-state=\"fallback\"] .wordmark-label",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1190px)"), gwccss.Display.None),
	)
	declareGlobal(".brand-logo-slot[data-hcm-brand-logo-state=\"fallback\"] .wordmark-mark",
		mediaRule(gwccss.RawMedia("(min-width:761px) and (max-width:1190px)"), gwccss.Display.Grid, gwccss.Raw("place-items", "center"), gwccss.W(gwccss.Px(34)), gwccss.H(gwccss.Px(34)), gwccss.Rounded(gwccss.VarLength("hcm-radius-surface")), gwccss.Bg(gwccss.Var("accent")), gwccss.TextColor(gwccss.Var("on-brand")), gwccss.FontSize(gwccss.Rem(1)), gwccss.Tracking(gwccss.Zero)),
	)
	declareGlobal(".studio-back-link",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.TextColor(gwccss.Color("LinkText"))),
	)
}

func validationStylesStylesheet() string {
	return buildTypedSheet(declarevalidationStylesStyles)
}

func declarevalidationStylesStyles() {
	declareGlobal(".validation-summary",
		gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
		gwccss.Margin(gwccss.Zero),
		gwccss.PaddingY(gwccss.Px(14)), gwccss.PaddingX(gwccss.Px(16)),
		gwccss.Border(gwccss.Px(2), gwccss.Var("hcm-color-danger")),
		gwccss.Raw("border-inline-start-width", "5px"),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("hcm-color-danger-surface")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".validation-summary:focus",
		gwccss.Raw("outline", "var(--hcm-focus-ring-width,2px) solid var(--hcm-color-focus)"),
		gwccss.OutlineOffset(gwccss.RawLength("var(--hcm-focus-ring-offset,4px)")),
	)
	declareGlobal(".validation-summary h2",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(1)),
	)
	declareGlobal(".validation-summary-intro",
		gwccss.Raw("margin", "4px 0 8px"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.875)),
	)
	declareGlobal(".validation-summary ul",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(4)),
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("padding-inline-start", "1.25rem"),
	)
	declareGlobal(".validation-summary a",
		gwccss.TextColor(gwccss.Var("hcm-color-danger")),
		gwccss.Raw("font-weight", "700"),
		gwccss.Raw("text-decoration", "underline"),
		gwccss.TextUnderlineOffset(gwccss.Px(2)),
	)
	declareGlobal(".validation-field",
		gwccss.Display.Grid,
		gwccss.Raw("align-content", "start"),
		gwccss.Gap(gwccss.Px(6)),
	)
	declareGlobal(".validation-field-label",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".validation-field-help",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("font-weight", "400"),
		gwccss.LineHeight(gwccss.Num(1.35)),
	)
	declareGlobal(".validation-field input[aria-invalid=true],.validation-field select[aria-invalid=true],.validation-field textarea[aria-invalid=true]",
		gwccss.BorderColor(gwccss.Var("hcm-color-danger")),
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(1), gwccss.Var("hcm-color-danger"))),
	)
	declareGlobal(".validation-field-error",
		gwccss.Display.Block,
		gwccss.TextColor(gwccss.Var("hcm-color-danger")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(".validation-summary-empty[hidden]",
		gwccss.Display.None,
	)
	declareGlobal(".validation-summary",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"),
			gwccss.Keyframes("hcm-status-settle",
				gwccss.At("0%", gwccss.Transform(gwccss.Scale(.96)), gwccss.OpacityNum(gwccss.Num(0.2))),
				gwccss.At("100%", gwccss.Transform(gwccss.Scale(1)), gwccss.OpacityNum(gwccss.Num(1))),
			),
			gwccss.Animation(gwccss.VarDuration("hcm-motion-fast"), gwccss.Easing("var(--hcm-motion-easing)")),
			gwccss.Raw("animation-fill-mode", "both"),
		),
	)
	declareGlobal(".validation-summary",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("animation", "none")),
	)
	declareGlobal(".validation-summary",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("CanvasText")), gwccss.Bg(gwccss.Color("Canvas")), gwccss.TextColor(gwccss.Color("CanvasText"))),
	)
	declareGlobal(".validation-summary a",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.TextColor(gwccss.Color("LinkText"))),
	)
	declareGlobal(".validation-field input[aria-invalid=true],.validation-field select[aria-invalid=true],.validation-field textarea[aria-invalid=true]",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("border-color", "Mark!important"), gwccss.Raw("outline", "1px solid Mark"), gwccss.Raw("box-shadow", "none")),
	)
	declareGlobal(".validation-field-error",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.TextColor(gwccss.Color("CanvasText")), gwccss.Raw("font-weight", "700")),
	)
	declareGlobal(".validation-field-error::before",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("content", "\"! \""), gwccss.Raw("font-weight", "800")),
	)
}

func statusPresentationStylesStylesheet() string {
	return buildTypedSheet(declarestatusPresentationStylesStyles)
}

func declarestatusPresentationStylesStyles() {
	declareGlobal(".status-dimensions",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.W(gwccss.Percent(100)),
	)
	declareGlobal(".status-dimension-list",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(5, gwccss.MinMax(gwccss.TrackLen(gwccss.Rem(5.3)), gwccss.Fr(1)))),
		gwccss.Gap(gwccss.Px(5)),
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".status-dimension",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.Raw("align-content", "center"),
		gwccss.Items.Center,
		gwccss.RowGap(gwccss.Px(2)), gwccss.ColumnGap(gwccss.Px(5)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.MinHeight(gwccss.Px(43)),
		gwccss.PaddingY(gwccss.Px(5)), gwccss.PaddingX(gwccss.Px(6)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Raw("background", "var(--surface-subtle,var(--surface))"),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.LineHeight(gwccss.Num(1.15)),
	)
	declareGlobal(".status-dimension-glyph",
		gwccss.GridRow(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridSpan(2))),
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.MinWidth(gwccss.Ems(1.15)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("font-weight", "800"),
	)
	declareGlobal(".status-dimension-name",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
		gwccss.Tracking(gwccss.Ems(.02)),
		gwccss.Raw("text-transform", "uppercase"),
	)
	declareGlobal(".status-dimension-value",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".status-tone-active .status-dimension-glyph",
		gwccss.Raw("color", "var(--hcm-color-info,var(--accent))"),
	)
	declareGlobal(".status-tone-positive .status-dimension-glyph",
		gwccss.Raw("color", "var(--hcm-color-positive,var(--accent))"),
	)
	declareGlobal(".status-tone-caution .status-dimension-glyph",
		gwccss.Raw("color", "var(--hcm-color-warning,var(--warning))"),
	)
	declareGlobal(".status-tone-danger .status-dimension-glyph,.status-tone-invalid .status-dimension-glyph",
		gwccss.Raw("color", "var(--hcm-color-danger,var(--danger))"),
	)
	declareGlobal(".status-tone-invalid",
		gwccss.Raw("border-style", "double"),
	)
	declareGlobal(".status-projection-unavailable",
		gwccss.Display.Block,
		gwccss.PaddingY(gwccss.Px(6)), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.Raw("border", "1px dashed var(--line)"),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(".work-row:has(.status-dimensions)",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
	)
	declareGlobal(".work-row:has(.status-dimensions)>.avatar",
		gwccss.GridColumn(gwccss.GridLineAt(1)),
		gwccss.GridRow(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(3))),
	)
	declareGlobal(".work-row:has(.status-dimensions)>.row-main,.work-row:has(.status-dimensions)>.row-end",
		gwccss.GridColumn(gwccss.GridLineAt(2)),
	)
	declareGlobal(".work-row:has(.status-dimensions)>.row-end",
		gwccss.W(gwccss.Percent(100)),
		gwccss.Raw("justify-items", "stretch"),
	)
	declareGlobal(".work-row:has(.status-dimensions)",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
	declareGlobal(".work-row:has(.status-dimensions)>.row-main",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(2)), gwccss.GridRow(gwccss.GridLineAt(1))),
	)
	declareGlobal(".work-row:has(.status-dimensions)>.row-end",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(2)), gwccss.GridRow(gwccss.GridLineAt(2))),
	)
	declareGlobal(".work-row:has(.status-dimensions)>.work-row-chevron",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridLineAt(3)), gwccss.GridRow(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(3))), gwccss.Raw("align-self", "center")),
	)
	declareGlobal(".preview-head>.status-dimensions",
		gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
	)
	declareGlobal(".preview-head .status-dimension-list",
		gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".status-dimension-list",
		mediaRule(gwccss.MaxW(1190), gwccss.GridCols(gwccss.Repeat(3, gwccss.MinMax(gwccss.TrackLen(gwccss.Rem(5.3)), gwccss.Fr(1))))),
	)
	declareGlobal(".preview-head .status-dimension-list",
		mediaRule(gwccss.MaxW(1190), gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))))),
	)
	declareGlobal(".status-dimension-list",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))))),
	)
	declareGlobal(".status-dimension-list,.preview-head .status-dimension-list",
		mediaRule(gwccss.MaxW(420), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".status-dimension",
		mediaRule(gwccss.MaxW(420), gwccss.MinHeight(gwccss.Px(38))),
	)
	declareGlobal(".work-row:has(.status-dimensions)>.row-main,.work-row:has(.status-dimensions)>.row-end",
		mediaRule(gwccss.MaxW(420), gwccss.GridColumn(gwccss.GridLineAt(1))),
	)
	declareGlobal(".work-row:has(.status-dimensions)",
		mediaRule(gwccss.MaxW(420), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto")))),
	)
	declareGlobal(".work-row:has(.status-dimensions)>.row-main",
		mediaRule(gwccss.MaxW(420), gwccss.Raw("grid-column", "1 / -1")),
	)
	declareGlobal(".work-row:has(.status-dimensions)>.work-row-chevron",
		mediaRule(gwccss.MaxW(420), gwccss.GridColumn(gwccss.GridLineAt(2)), gwccss.GridRow(gwccss.GridLineAt(2))),
	)
	declareGlobal(".status-dimension",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("transition", "none!important"), gwccss.Raw("animation", "none!important")),
	)
	declareGlobal(".status-dimension,.status-projection-unavailable",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("CanvasText")), gwccss.Bg(gwccss.Color("Canvas")), gwccss.TextColor(gwccss.Color("CanvasText"))),
	)
	declareGlobal(".status-dimension-glyph",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("color", "CanvasText!important")),
	)
	declareGlobal(".status-dimension-value",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.TextColor(gwccss.Color("CanvasText"))),
	)
	declareGlobal(".status-dimension-name",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.TextColor(gwccss.Color("GrayText"))),
	)
	declareGlobal(".status-tone-invalid",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("outline", "2px solid Mark")),
	)
}

func provenancePresentationStylesStylesheet() string {
	return buildTypedSheet(declareprovenancePresentationStylesStyles)
}

func declareprovenancePresentationStylesStyles() {
	declareGlobal(".provenance-grammar",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(10)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("margin-top", "18px"),
		gwccss.PaddingY(gwccss.Px(15)), gwccss.PaddingX(gwccss.Px(16)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Raw("background", "var(--surface-subtle,var(--surface))"),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".provenance-heading",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)),
	)
	declareGlobal(".provenance-item-list,.provenance-meta",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(6)),
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(".provenance-item-list",
		gwccss.GridCols(gwccss.Repeat(4, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".provenance-meta",
		gwccss.GridCols(gwccss.Repeat(3, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
		gwccss.Raw("padding-top", "8px"),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".provenance-item",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.Raw("align-content", "center"),
		gwccss.Items.Center,
		gwccss.RowGap(gwccss.Px(2)), gwccss.ColumnGap(gwccss.Px(6)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.MinHeight(gwccss.Px(48)),
		gwccss.PaddingY(gwccss.Px(6)), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.LineHeight(gwccss.Num(1.2)),
	)
	declareGlobal(".provenance-item-glyph",
		gwccss.GridRow(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridSpan(2))),
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.MinWidth(gwccss.Ems(1.15)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("font-weight", "800"),
	)
	declareGlobal(".provenance-item-name",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
		gwccss.Tracking(gwccss.Ems(.02)),
		gwccss.Raw("text-transform", "uppercase"),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".provenance-item-value",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("overflow-wrap", "anywhere"),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".provenance-tone-info .provenance-item-glyph",
		gwccss.Raw("color", "var(--hcm-color-info,var(--accent))"),
	)
	declareGlobal(".provenance-tone-caution .provenance-item-glyph",
		gwccss.Raw("color", "var(--hcm-color-warning,var(--warning))"),
	)
	declareGlobal(".provenance-tone-invalid",
		gwccss.Raw("border-style", "double"),
	)
	declareGlobal(".provenance-tone-invalid .provenance-item-glyph",
		gwccss.Raw("color", "var(--hcm-color-danger,var(--danger))"),
	)
	declareGlobal(".provenance-boundary,.provenance-opaque-boundary",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.LineHeight(gwccss.Num(1.35)),
	)
	declareGlobal(".provenance-boundary",
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".provenance-opaque-boundary",
		gwccss.Display.Flex,
		gwccss.Gap(gwccss.Px(7)),
		gwccss.Raw("align-items", "flex-start"),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(".provenance-boundary-glyph",
		gwccss.Raw("flex", "none"),
		gwccss.Raw("color", "var(--hcm-color-warning,var(--warning))"),
	)
	declareGlobal(".work-preview>.provenance-grammar",
		gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
	)
	declareGlobal(".work-preview .provenance-item-list",
		gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".work-preview .provenance-meta",
		gwccss.GridCols(gwccss.Fr(1)),
	)
	declareGlobal(".provenance-item-list",
		mediaRule(gwccss.MaxW(900), gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))))),
	)
	declareGlobal(".provenance-item-list,.provenance-meta,.work-preview .provenance-item-list",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".provenance-item",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("transition", "none!important"), gwccss.Raw("animation", "none!important")),
	)
	declareGlobal(".provenance-grammar,.provenance-item",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("CanvasText")), gwccss.Bg(gwccss.Color("Canvas")), gwccss.TextColor(gwccss.Color("CanvasText"))),
	)
	declareGlobal(".provenance-item-glyph,.provenance-boundary-glyph",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("color", "CanvasText!important")),
	)
	declareGlobal(".provenance-item-name,.provenance-boundary",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.TextColor(gwccss.Color("GrayText"))),
	)
	declareGlobal(".provenance-item-value,.provenance-opaque-boundary",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.TextColor(gwccss.Color("CanvasText"))),
	)
	declareGlobal(".provenance-grammar",
		mediaRule(gwccss.RawMedia("(print)"), gwccss.Raw("break-inside", "avoid"), gwccss.Bg(gwccss.Transparent), gwccss.BorderColor(gwccss.Hex("000"))),
	)
	declareGlobal(".provenance-item",
		mediaRule(gwccss.RawMedia("(print)"), gwccss.Bg(gwccss.Transparent), gwccss.BorderColor(gwccss.Hex("000"))),
	)
	declareGlobal(".provenance-boundary,.provenance-item-name",
		mediaRule(gwccss.RawMedia("(print)"), gwccss.TextColor(gwccss.Hex("000"))),
	)
}

func roleAccessStylesStylesheet() string {
	return buildTypedSheet(declareroleAccessStylesStyles)
}

func declareroleAccessStylesStyles() {
	declareGlobal(".roles-access-page",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.VarLength("theme-section-gap")),
	)
	declareGlobal(".roles-access-intro",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(12)),
		gwccss.Raw("padding", "0 2px"),
	)
	declareGlobal(".roles-page-jumps",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Px(16)),
	)
	declareGlobal(".roles-page-jumps a",
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.Raw("text-decoration", "underline"),
	)
	declareGlobal(".roles-access-layout",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.Gap(gwccss.Px(18)),
		gwccss.Raw("align-items", "start"),
	)
	declareGlobal(".role-create-disclosure>summary",
		gwccss.Raw("cursor", "pointer"),
		gwccss.Raw("padding", "14px 22px"),
		gwccss.Raw("font-weight", "700"),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".role-create-disclosure>summary:focus-visible",
		gwccss.Raw("outline", "3px solid var(--hcm-color-focus)"),
		gwccss.Raw("outline-offset", "-3px"),
	)
	declareGlobal(".role-catalog,.employee-role-directory",
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".access-role-grid",
		gwccss.Display.Grid,
		gwccss.Raw("grid-template-columns", "repeat(auto-fit,minmax(min(100%,380px),1fr))"),
		gwccss.Gap(gwccss.Px(9)),
		gwccss.Raw("padding", "0 22px 22px"),
	)
	declareGlobal(".access-role-card",
		gwccss.Raw("overflow", "hidden"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".access-role-card[open]",
		gwccss.Raw("grid-column", "1 / -1"),
	)
	declareGlobal(".access-role-card>summary",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.RowGap(gwccss.Px(5)), gwccss.ColumnGap(gwccss.Px(12)),
		gwccss.PaddingY(gwccss.Px(13)), gwccss.PaddingX(gwccss.Px(14)),
		gwccss.Raw("cursor", "pointer"),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".access-role-card>summary::-webkit-details-marker",
		gwccss.Display.None,
	)
	declareGlobal(".access-role-card>summary:hover",
		gwccss.Raw("background-color", "var(--hcm-hover-surface,var(--soft))"),
	)
	declareGlobal(".access-role-card[open]>summary",
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".access-role-identity",
		gwccss.Display.Flex,
		gwccss.Items.Baseline,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Px(8)),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".access-role-card code",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".access-role-card summary p",
		gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".access-role-expand",
		gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
	)
	declareGlobal(".role-definition",
		gwccss.Raw("padding", "16px 18px"),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".role-definition h3,.role-definition p",
		gwccss.Raw("margin", "0 0 10px"),
	)
	declareGlobal(".role-definition dl",
		gwccss.Display.Grid,
		gwccss.Raw("grid-template-columns", "repeat(auto-fit,minmax(min(100%,220px),1fr))"),
		gwccss.Gap(gwccss.Px(12)),
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".role-definition dt",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".role-definition dd",
		gwccss.Raw("margin", "3px 0 0"),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".role-page-access",
		gwccss.Display.Block,
		gwccss.Padding(gwccss.Px(15)),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".role-page-access>summary",
		gwccss.Raw("cursor", "pointer"),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".role-page-access-help",
		gwccss.Raw("margin", "10px 0"),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".role-feature-page-groups",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".role-feature-page",
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".role-feature-page>summary",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(12)),
		gwccss.Raw("padding", "11px 13px"),
		gwccss.Raw("cursor", "pointer"),
		gwccss.Raw("font-weight", "700"),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".role-feature-page>summary:hover",
		gwccss.Raw("background-color", "var(--hcm-hover-surface,var(--soft))"),
	)
	declareGlobal(".role-feature-page[open]>summary",
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
	)
	declareGlobal(".role-feature-page .role-page-table-wrap",
		gwccss.Raw("border", "0"),
		gwccss.Raw("border-radius", "0"),
	)
	declareGlobal(".role-feature-table",
		gwccss.MinWidth(gwccss.Px(590)),
	)
	declareGlobal(".role-feature-table tbody th",
		gwccss.MinWidth(gwccss.Px(260)),
		gwccss.Raw("text-align", "left"),
	)
	declareGlobal(".role-feature-description",
		gwccss.Display.Block,
		gwccss.Raw("margin-top", "4px"),
		gwccss.Raw("font-weight", "400"),
		gwccss.Raw("line-height", "1.4"),
	)
	declareGlobal(".role-page-scroll-hint",
		gwccss.Display.None,
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".role-unpublished-pages",
		gwccss.Raw("margin-top", "14px"),
		gwccss.Raw("border-top", "1px solid var(--line)"),
		gwccss.Raw("padding-top", "12px"),
	)
	declareGlobal(".role-unpublished-pages>summary",
		gwccss.Raw("cursor", "pointer"),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".role-unpublished-pages>p",
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".role-page-table-wrap",
		gwccss.MaxWidth(gwccss.Percent(100)),
		gwccss.Raw("overflow-x", "auto"),
		gwccss.Raw("overscroll-behavior-inline", "contain"),
		gwccss.Raw("margin", "0"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
	)
	declareGlobal(".role-directory-guidance",
		gwccss.Raw("padding", "0 22px 12px"),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".role-effective-boundary",
		gwccss.Raw("margin", "0 22px 14px"),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".role-directory-pagination",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(12)),
		gwccss.Raw("padding", "14px 22px 20px"),
	)
	declareGlobal(".role-page-table",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinWidth(gwccss.Px(670)),
		gwccss.Raw("border-collapse", "separate"),
		gwccss.BorderSpacing(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".role-page-table th,.role-page-table td",
		gwccss.PaddingY(gwccss.Px(9)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Raw("text-align", "center"),
		gwccss.Raw("vertical-align", "middle"),
	)
	declareGlobal(".role-page-table thead th",
		gwccss.Position.Sticky,
		gwccss.Top(gwccss.Zero),
		gwccss.ZIndex(1),
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("text-transform", "uppercase"),
		gwccss.Tracking(gwccss.Ems(.05)),
	)
	declareGlobal(".role-page-table th:first-child",
		gwccss.Raw("text-align", "left"),
	)
	declareGlobal(".role-page-table tbody tr:last-child>*",
		gwccss.Raw("border-bottom", "0"),
	)
	declareGlobal(".role-page-table tbody tr:hover",
		gwccss.Raw("background-color", "var(--hcm-hover-surface,var(--soft))"),
	)
	declareGlobal(".role-page-table tbody th",
		gwccss.MinWidth(gwccss.Px(220)),
	)
	declareGlobal(".role-page-table tbody th small",
		gwccss.Display.Block,
		gwccss.Raw("margin-top", "2px"),
		gwccss.Raw("font-weight", "400"),
	)
	declareGlobal(".permission-check",
		gwccss.Raw("display", "inline-grid"),
		gwccss.Raw("place-items", "center"),
		gwccss.MinWidth(gwccss.Px(44)),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal(".permission-check:hover",
		gwccss.Bg(gwccss.Var("soft")),
	)
	declareGlobal(".permission-check input",
		gwccss.W(gwccss.Px(18)),
		gwccss.H(gwccss.Px(18)),
		gwccss.Raw("accent-color", "var(--accent)"),
	)
	declareGlobal(".permission-check:has(input:disabled)",
		gwccss.Raw("cursor", "not-allowed"),
		gwccss.OpacityNum(gwccss.Num(.65)),
	)
	declareGlobal(".permission-read-only",
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(".button.compact",
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(6)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".create-role-form",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(14)),
		gwccss.PaddingY(gwccss.Px(20)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".create-role-form h3",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".create-role-fields",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(12)),
	)
	declareGlobal(".create-role-fields label",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(6)),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".create-role-fields input,.create-role-fields textarea,.role-directory-filter input",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinHeight(gwccss.Px(43)),
		gwccss.PaddingY(gwccss.Px(9)), gwccss.PaddingX(gwccss.Px(11)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("control-border")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".create-role-fields textarea",
		gwccss.MinHeight(gwccss.Px(78)),
		gwccss.Raw("resize", "vertical"),
	)
	declareGlobal(".create-role-fields small",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("font-weight", "400"),
	)
	declareGlobal(".role-form-actions",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(12)),
	)
	declareGlobal(".role-form-actions p",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".role-directory-filter",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(7)),
		gwccss.Raw("padding", "0 22px 18px"),
	)
	declareGlobal(".role-directory-filter>label",
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".role-directory-filter>div",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".employee-role-list",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(8)),
		gwccss.Raw("padding", "0 22px 22px"),
	)
	declareGlobal(".employee-role-editor",
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".employee-role-editor>summary",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(11)),
		gwccss.PaddingY(gwccss.Px(11)), gwccss.PaddingX(gwccss.Px(13)),
		gwccss.Raw("cursor", "pointer"),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".employee-role-editor>summary::-webkit-details-marker,.role-visibility-editor>summary::-webkit-details-marker",
		gwccss.Display.None,
	)
	declareGlobal(".employee-role-editor[open]>summary",
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".employee-role-identity",
		gwccss.Display.Grid,
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".employee-role-identity small",
		gwccss.Raw("overflow", "hidden"),
		gwccss.TextOverflowEllipsis(),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(".employee-role-badges",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Raw("justify-content", "flex-end"),
		gwccss.Gap(gwccss.Px(5)),
	)
	declareGlobal(".employee-role-form",
		gwccss.Padding(gwccss.Px(15)),
	)
	declareGlobal(".employee-role-form fieldset",
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("border", "0"),
	)
	declareGlobal(".employee-role-form legend",
		gwccss.Raw("padding", "0 0 10px"),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".employee-role-choices",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
		gwccss.Gap(gwccss.Px(7)),
		gwccss.Raw("margin-bottom", "14px"),
	)
	declareGlobal(".employee-role-choice",
		gwccss.Display.Flex,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Gap(gwccss.Px(8)),
		gwccss.Padding(gwccss.Px(9)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal(".employee-role-choice:has(input:checked)",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Bg(gwccss.Var("soft")),
	)
	declareGlobal(".organization-visibility-nav",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".role-visibility-list",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".role-visibility-editor",
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".role-visibility-editor>summary",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(18)),
		gwccss.PaddingY(gwccss.Px(17)), gwccss.PaddingX(gwccss.Px(20)),
		gwccss.Raw("cursor", "pointer"),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".role-visibility-editor>summary>div",
		gwccss.Display.Flex,
		gwccss.Items.Baseline,
		gwccss.Gap(gwccss.Px(9)),
	)
	declareGlobal(".role-visibility-editor>summary code",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".role-visibility-editor[open]>summary",
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".roles-access-layout",
		mediaRule(gwccss.MaxW(1050), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".roles-access-intro",
		mediaRule(gwccss.MaxW(680), gwccss.Raw("align-items", "flex-start"), gwccss.FlexDir.Col),
	)
	declareGlobal(".employee-role-choices",
		mediaRule(gwccss.MaxW(680), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".employee-role-badges",
		mediaRule(gwccss.MaxW(680), gwccss.GridColumn(gwccss.GridLineAt(2)), gwccss.Raw("justify-content", "flex-start")),
	)
	declareGlobal(".employee-role-editor>summary",
		mediaRule(gwccss.MaxW(680), gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".role-form-actions",
		mediaRule(gwccss.MaxW(680), gwccss.Items.Stretch, gwccss.FlexDir.Col),
	)
	declareGlobal(".role-form-actions .button",
		mediaRule(gwccss.MaxW(680), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal(".role-visibility-editor>summary",
		mediaRule(gwccss.MaxW(680), gwccss.Raw("align-items", "flex-start"), gwccss.FlexDir.Col),
	)
	declareGlobal(".role-page-access",
		mediaRule(gwccss.MaxW(680), gwccss.Padding(gwccss.Px(10))),
	)
	declareGlobal(".role-page-scroll-hint",
		mediaRule(gwccss.MaxW(680), gwccss.Display.Block),
	)
	declareGlobal(".access-role-card,.employee-role-editor,.employee-role-choice,.role-visibility-editor,.role-page-table-wrap",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("CanvasText"))),
	)
}

func organizationDisclosureStylesStylesheet() string {
	return buildTypedSheet(declareorganizationDisclosureStylesStyles)
}

func declareorganizationDisclosureStylesStyles() {
	declareGlobal(".organization-page .org-branches",
		gwccss.Raw("grid-template-columns", "minmax(0,1fr)"),
		gwccss.Raw("align-items", "start"),
	)
	declareGlobal(".organization-unit",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".organization-unit-disclosure",
		gwccss.Raw("overflow", "hidden"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".organization-unit-disclosure>.org-node.manager",
		gwccss.W(gwccss.Percent(100)),
		gwccss.Raw("border", "0"),
		gwccss.Rounded(gwccss.Zero),
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Zero, gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Var("accent"))),
		gwccss.Raw("cursor", "pointer"),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".organization-unit-disclosure>.org-node.manager::-webkit-details-marker",
		gwccss.Display.None,
	)
	declareGlobal(".organization-unit-glyph",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.W(gwccss.Px(26)),
		gwccss.H(gwccss.Px(26)),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(1.25)),
		gwccss.LineHeight(gwccss.Num(1)),
		gwccss.Raw("transform", "rotate(0)"),
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("transform")), gwccss.VarDuration("hcm-motion-normal"), gwccss.Easing("var(--hcm-motion-easing)")),
	)
	declareGlobal(".organization-unit-chevron",
		gwccss.W(gwccss.Px(16)), gwccss.H(gwccss.Px(16)),
	)
	declareGlobal(".work-row-chevron",
		gwccss.W(gwccss.Px(16)), gwccss.H(gwccss.Px(16)),
		gwccss.Raw("flex", "none"),
	)
	declareGlobal(".organization-unit-disclosure[open]>.org-node.manager .organization-unit-glyph",
		gwccss.Transform(gwccss.Rotate(gwccss.Deg(90))),
	)
	declareGlobal(".organization-unit-members",
		gwccss.Display.Grid,
		gwccss.Raw("grid-template-columns", "repeat(auto-fit,minmax(min(100%,280px),1fr))"),
		gwccss.Gap(gwccss.Px(8)),
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Px(10)),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".organization-unit-members .ownership-card",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinHeight(gwccss.Px(54)),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".ownership-person",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(5)),
		gwccss.W(gwccss.MinLen(gwccss.Px(440), gwccss.Percent(100))),
	)
	declareGlobal(".ownership-person-disclosure>summary",
		gwccss.Raw("width", "fit-content"),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(5)), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
		gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal(".ownership-person-disclosure>summary:hover",
		gwccss.Bg(gwccss.Var("hcm-hover-surface")),
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".ownership-person-disclosure>summary:focus-visible",
		gwccss.Raw("outline", "2px solid var(--accent)"),
		gwccss.Raw("outline-offset", "2px"),
	)
	declareGlobal(".ownership-person-details",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(6)),
		gwccss.Padding(gwccss.Px(10)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".ownership-person-detail",
		gwccss.Display.Flex,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(12)),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".ownership-person-detail small",
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".relationship-explanation",
		gwccss.Raw("margin", "0"),
		gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.Raw("border-inline-start", "3px solid var(--accent)"),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.LineHeight(gwccss.Num(1.4)),
	)
	declareGlobal(".ownership-card.current-person",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("accent"))),
	)
	declareGlobal(".ownership-card.selected-person",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("accent"))),
	)
	declareGlobal(".myself-organization",
		gwccss.Raw("padding", "0 22px 22px"),
	)
	declareGlobal(".myself-organization>p",
		gwccss.MaxWidth(gwccss.Ch(72)),
		gwccss.Raw("margin", "0 0 18px"),
	)
	declareGlobal(".organization-unit-disclosure[open] .organization-unit-members",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"),
			gwccss.Keyframes("hcm-item-enter",
				gwccss.At("from", gwccss.OpacityNum(gwccss.Num(0.01)), gwccss.Transform(gwccss.TranslateY(gwccss.RawLength("calc(var(--hcm-motion-distance) * .65)")))),
				gwccss.At("to", gwccss.OpacityNum(gwccss.Num(1)), gwccss.Raw("transform", "none")),
			),
			gwccss.Animation(gwccss.VarDuration("hcm-motion-normal"), gwccss.Easing("var(--hcm-motion-easing)")),
			gwccss.Raw("animation-fill-mode", "both"),
		),
	)
	declareGlobal(".organization-unit-members",
		mediaRule(gwccss.MaxW(760), gwccss.Padding(gwccss.Px(8))),
	)
	declareGlobal(".myself-organization",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("padding", "0 17px 17px")),
	)
	declareGlobal(".organization-unit-disclosure,.organization-unit-members,.ownership-card.current-person,.ownership-card.selected-person,.ownership-person-details,.relationship-explanation",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("CanvasText"))),
	)
	declareGlobal(".ownership-card.current-person",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("outline", "2px solid Highlight")),
	)
}

func organizationVisibilityStylesStylesheet() string {
	return buildTypedSheet(declareorganizationVisibilityStylesStyles)
}

func declareorganizationVisibilityStylesStyles() {
	declareGlobal(".organization-visibility-page",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.VarLength("theme-section-gap")),
	)
	declareGlobal(".organization-visibility-intro",
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Raw("align-items", "stretch"),
		gwccss.Gap(gwccss.Px(10)),
		gwccss.PaddingY(gwccss.Px(14)), gwccss.PaddingX(gwccss.Px(18)),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".organization-visibility-role-selector",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(9)),
		gwccss.Padding(gwccss.Px(14)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
	)
	declareGlobal(".organization-visibility-role-selector>div",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".organization-visibility-role-link",
		gwccss.Raw("display", "inline-flex"),
		gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(36)),
		gwccss.Raw("padding", "5px 10px"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".organization-visibility-role-link:hover,.organization-visibility-role-link:focus-visible",
		gwccss.Raw("background-color", "var(--hcm-hover-surface,var(--soft))"),
		gwccss.Raw("text-decoration", "underline"),
	)
	declareGlobal(".organization-visibility-intro h2,.organization-visibility-intro p",
		gwccss.MarginY(gwccss.Px(3)), gwccss.MarginX(gwccss.Zero),
	)
	declareGlobal(".organization-visibility-form",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Px(280)), gwccss.Fr(.8)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(420)), gwccss.Fr(1.2))),
		gwccss.Gap(gwccss.Zero),
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".organization-visibility-form.mode-only",
		gwccss.GridCols(gwccss.Fr(1)),
	)
	declareGlobal(".organization-visibility-form.mode-only .organization-visibility-modes",
		gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
		gwccss.Raw("border-right", "0!important"),
		gwccss.Raw("border-bottom", "1px solid var(--line)!important"),
	)
	declareGlobal(".organization-visibility-form fieldset",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Px(22)),
		gwccss.Raw("border", "0"),
	)
	declareGlobal(".organization-visibility-form legend",
		gwccss.Raw("padding", "0 0 14px"),
		gwccss.FontSize(gwccss.Rem(1)),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".organization-visibility-modes",
		gwccss.Display.Grid,
		gwccss.Raw("align-content", "start"),
		gwccss.Gap(gwccss.Px(9)),
		gwccss.Raw("border-right", "1px solid var(--line)!important"),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".organization-visibility-mode",
		gwccss.Position.Relative,
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.Gap(gwccss.Px(10)),
		gwccss.MinHeight(gwccss.Px(76)),
		gwccss.Padding(gwccss.Px(12)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal(".organization-visibility-mode:has(input:checked)",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Px(1), gwccss.Var("accent"))),
	)
	declareGlobal(".organization-visibility-mode input,.organization-visibility-unit input",
		gwccss.Raw("margin-top", "3px"),
		gwccss.Raw("accent-color", "var(--accent)"),
	)
	declareGlobal(".organization-visibility-mode span,.organization-visibility-mode strong,.organization-visibility-mode small",
		gwccss.Display.Block,
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".organization-visibility-mode small",
		gwccss.Raw("margin-top", "4px"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.LineHeight(gwccss.Num(1.35)),
	)
	declareGlobal(".organization-visibility-units>p",
		gwccss.Raw("margin", "-8px 0 15px"),
	)
	declareGlobal(".organization-visibility-unit-grid",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
		gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".organization-visibility-unit",
		gwccss.Display.Flex,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Gap(gwccss.Px(9)),
		gwccss.MinHeight(gwccss.Px(43)),
		gwccss.PaddingY(gwccss.Px(10)), gwccss.PaddingX(gwccss.Px(11)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal(".organization-visibility-unit:has(input:checked)",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Bg(gwccss.Var("soft")),
	)
	declareGlobal(".organization-visibility-boundary",
		gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
		gwccss.Raw("margin", "0 22px 18px"),
		gwccss.PaddingY(gwccss.Px(13)), gwccss.PaddingX(gwccss.Px(15)),
		gwccss.Raw("border-inline-start", "3px solid var(--accent)"),
		gwccss.Bg(gwccss.Var("soft")),
	)
	declareGlobal(".organization-visibility-boundary p",
		gwccss.Raw("margin", "4px 0 0"),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".organization-visibility-actions",
		gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(14)),
		gwccss.PaddingY(gwccss.Px(16)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".organization-visibility-actions p",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".organization-visibility-scope",
		gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
		gwccss.Gap(gwccss.Px(12)),
		gwccss.Raw("margin", "0 22px 18px"),
		gwccss.PaddingY(gwccss.Px(14)), gwccss.PaddingX(gwccss.Px(15)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".organization-scope",
		gwccss.Display.Grid,
		gwccss.Raw("align-content", "start"),
		gwccss.Gap(gwccss.Px(6)),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".organization-scope-title",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)),
	)
	declareGlobal(".organization-scope-mode",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".organization-scope-units",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Px(6)),
		gwccss.Raw("list-style", "none"),
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(".organization-scope-unit",
		gwccss.PaddingY(gwccss.Px(3)), gwccss.PaddingX(gwccss.Px(9)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".organization-scope-empty",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".data-domain-scope",
		gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
		gwccss.Display.Grid,
		gwccss.Raw("align-content", "start"),
		gwccss.Gap(gwccss.Px(6)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("margin", "0 22px 18px"),
	)
	declareGlobal(".data-domain-scope-title",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)),
	)
	declareGlobal(".data-domain-scope-domains",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Px(6)),
		gwccss.Raw("list-style", "none"),
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(".data-domain-scope-domain",
		gwccss.PaddingY(gwccss.Px(3)), gwccss.PaddingX(gwccss.Px(9)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".data-domain-scope-empty",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".data-domain-scope-domains",
		mediaRule(gwccss.MaxW(620), gwccss.Raw("flex-direction", "column"), gwccss.Raw("align-items", "stretch")),
	)
	declareGlobal(".organization-visibility-form",
		mediaRule(gwccss.MaxW(900), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".organization-visibility-modes",
		mediaRule(gwccss.MaxW(900), gwccss.Raw("border-right", "0!important"), gwccss.Raw("border-bottom", "1px solid var(--line)!important")),
	)
	declareGlobal(".organization-visibility-intro",
		mediaRule(gwccss.MaxW(620), gwccss.Raw("align-items", "flex-start"), gwccss.FlexDir.Col, gwccss.Padding(gwccss.Px(14))),
	)
	declareGlobal(".organization-visibility-unit-grid",
		mediaRule(gwccss.MaxW(620), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".organization-visibility-form.mode-only .organization-visibility-modes",
		mediaRule(gwccss.MaxW(620), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".organization-visibility-scope",
		mediaRule(gwccss.MaxW(620), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".organization-visibility-actions",
		mediaRule(gwccss.MaxW(620), gwccss.Items.Stretch, gwccss.FlexDir.Col),
	)
	declareGlobal(".organization-visibility-actions .button",
		mediaRule(gwccss.MaxW(620), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal(".organization-visibility-mode,.organization-visibility-unit,.organization-visibility-boundary",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("CanvasText"))),
	)
}

func workerIDStylesStylesheet() string {
	return buildTypedSheet(declareworkerIDStylesStyles)
}

func declareworkerIDStylesStyles() {
	declareGlobal(".worker-id-page",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.VarLength("theme-section-gap")),
	)
	declareGlobal(".worker-id-intro",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(24)),
		gwccss.PaddingY(gwccss.Px(25)), gwccss.PaddingX(gwccss.Px(27)),
		gwccss.Raw("background", "linear-gradient(125deg,var(--soft),var(--surface) 70%)"),
	)
	declareGlobal(".worker-id-intro h2,.worker-id-intro p",
		gwccss.MarginY(gwccss.Px(3)), gwccss.MarginX(gwccss.Zero),
	)
	declareGlobal(".worker-id-layout",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1.7)), gwccss.MinMax(gwccss.TrackLen(gwccss.Px(290)), gwccss.Fr(.8))),
		gwccss.Gap(gwccss.Px(18)),
		gwccss.Raw("align-items", "start"),
	)
	declareGlobal(".worker-id-rules",
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".worker-id-rules .section-head p",
		gwccss.Raw("margin", "4px 0 0"),
	)
	declareGlobal(".admin-form-section",
		gwccss.Raw("border", "0"),
		gwccss.Raw("margin", "0"),
		gwccss.Raw("padding", "0"),
	)
	declareGlobal(".admin-form-section legend",
		gwccss.Raw("font-weight", "700"),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".admin-form-section-fields",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
		gwccss.Gap(gwccss.Px(15)),
		gwccss.Raw("padding", "14px 22px 22px"),
	)
	declareGlobal(".worker-id-fields",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(15)),
	)
	declareGlobal(".worker-id-fields label",
		gwccss.Display.Grid,
		gwccss.Raw("align-content", "start"),
		gwccss.Gap(gwccss.Px(6)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".admin-form-section-fields label",
		gwccss.Display.Grid,
		gwccss.Raw("align-content", "start"),
		gwccss.Gap(gwccss.Px(6)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".worker-id-fields input,.worker-id-fields select",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".admin-form-section-fields input,.admin-form-section-fields select",
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(8)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".worker-id-fields input:focus,.worker-id-fields select:focus,.admin-form-section-fields input:focus,.admin-form-section-fields select:focus",
		gwccss.BorderColor(gwccss.Var("accent")),
		gwccss.Raw("outline", "2px solid color-mix(in srgb,var(--accent) 20%,transparent)"),
		gwccss.OutlineOffset(gwccss.Px(1)),
	)
	declareGlobal(".worker-id-fields small,.admin-form-section-fields small",
		gwccss.Raw("font-weight", "400"),
		gwccss.LineHeight(gwccss.Num(1.35)),
	)
	declareGlobal(".worker-id-actions",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(14)),
		gwccss.PaddingY(gwccss.Px(16)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".worker-id-actions p",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".worker-id-preview",
		gwccss.Position.Sticky,
		gwccss.Top(gwccss.Px(101)),
		gwccss.Padding(gwccss.Px(22)),
	)
	declareGlobal(".worker-id-preview h2",
		gwccss.Raw("margin", "14px 0 3px"),
	)
	declareGlobal(".worker-id-preview>p",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".worker-id-examples",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(8)),
		gwccss.MarginY(gwccss.Px(18)), gwccss.MarginX(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".worker-id-examples code",
		gwccss.Display.Block,
		gwccss.PaddingY(gwccss.Px(12)), gwccss.PaddingX(gwccss.Px(14)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(1)),
		gwccss.Raw("font-weight", "700"),
		gwccss.Tracking(gwccss.Ems(.035)),
	)
	declareGlobal(".worker-id-state",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1)),
		gwccss.Gap(gwccss.Px(9)),
		gwccss.MarginY(gwccss.Px(18)), gwccss.MarginX(gwccss.Zero),
	)
	declareGlobal(".worker-id-state>div",
		gwccss.Padding(gwccss.Px(12)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
	)
	declareGlobal(".worker-id-state dt",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
		gwccss.Raw("text-transform", "uppercase"),
	)
	declareGlobal(".worker-id-state dd",
		gwccss.Raw("margin", "4px 0 0"),
		gwccss.FontSize(gwccss.Rem(1.25)),
		gwccss.Raw("font-weight", "800"),
	)
	declareGlobal(".worker-id-layout",
		mediaRule(gwccss.MaxW(980), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".worker-id-preview",
		mediaRule(gwccss.MaxW(980), gwccss.Position.Static),
	)
	declareGlobal(".worker-id-fields",
		mediaRule(gwccss.MaxW(980), gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))))),
	)
	declareGlobal(".worker-id-intro",
		mediaRule(gwccss.MaxW(620), gwccss.Raw("align-items", "flex-start"), gwccss.FlexDir.Col),
	)
	declareGlobal(".worker-id-fields",
		mediaRule(gwccss.MaxW(620), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".admin-form-section-fields",
		mediaRule(gwccss.MaxW(620), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".worker-id-actions",
		mediaRule(gwccss.MaxW(620), gwccss.Items.Stretch, gwccss.FlexDir.Col),
	)
	declareGlobal(".worker-id-actions .button",
		mediaRule(gwccss.MaxW(620), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal(".worker-id-fields input,.worker-id-fields select,.worker-id-examples code,.worker-id-state>div",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("CanvasText"))),
	)
}

func organizationHierarchyStylesStylesheet() string {
	return buildTypedSheet(declareorganizationHierarchyStylesStyles)
}

func declareorganizationHierarchyStylesStyles() {
	declareGlobal(".organization-browse-summary",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(12)),
		gwccss.PaddingY(gwccss.Px(12)), gwccss.PaddingX(gwccss.Px(14)),
		gwccss.Margin(gwccss.RawLength("0 0 14px")),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".organization-summary-facts",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Px(18)),
	)
	declareGlobal(".organization-summary-fact,.organization-summary-scope",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(2)),
		gwccss.Raw("align-content", "start"),
	)
	declareGlobal(".organization-summary-fact strong",
		gwccss.FontSize(gwccss.Rem(1)),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(".organization-summary-fact small,.organization-summary-scope small",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".organization-browse-controls",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(6)),
	)
	declareGlobal(".organization-browse-controls .button",
		gwccss.MinHeight(gwccss.Px(40)),
		gwccss.PaddingY(gwccss.Px(6)), gwccss.PaddingX(gwccss.Px(10)),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(".organization-density-label",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("font-weight", "700"),
		gwccss.Raw("margin-inline-end", "4px"),
	)
	declareGlobal(".organization-search",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(8)),
		gwccss.W(gwccss.MinLen(gwccss.Px(620), gwccss.Percent(100))),
		gwccss.Raw("margin", "0 0 18px"),
	)
	declareGlobal(".organization-search>label",
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Raw("font-weight", "700"),
	)
	declareGlobal(".organization-search-control",
		gwccss.Display.Flex,
		gwccss.Items.Stretch,
		gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".organization-search-control>input",
		gwccss.Raw("flex", "1 1 24rem"),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".organization-search-status",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(12)),
		gwccss.MinHeight(gwccss.Px(34)),
	)
	declareGlobal(".organization-search-summary",
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(".organization-view-head",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Px(16)),
		gwccss.Raw("margin", "0 0 18px"),
	)
	declareGlobal(".organization-page .org-branches",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(8)),
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
	)
	declareGlobal(".organization-page .organization-unit-disclosure",
		gwccss.Raw("border-color", "color-mix(in srgb,var(--line) 72%,var(--surface-subtle))"),
		gwccss.Raw("box-shadow", "none"),
	)
	declareGlobal(".organization-view-head .definition",
		gwccss.Raw("flex", "1"),
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".organization-view-toggle",
		gwccss.Display.InlineFlex,
		gwccss.Raw("flex", "none"),
		gwccss.Padding(gwccss.Px(3)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.Px(999)),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".organization-view-option",
		gwccss.MinHeight(gwccss.Px(44)),
		gwccss.PaddingY(gwccss.Px(7)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Rounded(gwccss.Px(999)),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
		gwccss.Raw("text-decoration", "none"),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(".organization-view-option:hover",
		gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".organization-view-option.active",
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.BorderColor(gwccss.Var("control-border")),
	)
	declareGlobal(".ownership-tree,.ownership-tree ul",
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".ownership-tree",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(14)),
	)
	declareGlobal(".ownership-virtual-tree",
		gwccss.Display.Block,
		gwccss.H(gwccss.Px(560)),
		gwccss.Raw("overflow-y", "auto"),
		gwccss.Raw("overscroll-behavior", "contain"),
		gwccss.Raw("scroll-padding-block", "144px"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
	)
	declareGlobal(".ownership-virtual-tree:focus-visible",
		gwccss.Raw("outline", "2px solid var(--accent)"),
		gwccss.Raw("outline-offset", "2px"),
	)
	declareGlobal(".ownership-virtual-row",
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(8)),
		gwccss.H(gwccss.Px(144)),
		gwccss.Padding(gwccss.Px(8)),
		gwccss.Raw("box-sizing", "border-box"),
	)
	declareGlobal(".ownership-virtual-row .ownership-card",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.MaxWidth(gwccss.Px(440)),
		gwccss.Raw("flex", "1 1 auto"),
	)
	declareGlobal(".ownership-virtual-tree .ownership-virtual-row .row-main strong,.ownership-virtual-tree .ownership-virtual-row .row-main small",
		gwccss.Display.Block,
		gwccss.Raw("white-space", "nowrap"),
		gwccss.Raw("overflow", "hidden"),
		gwccss.Raw("text-overflow", "ellipsis"),
	)
	declareGlobal(".ownership-tree ul",
		gwccss.Position.Relative,
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.Px(10)),
		gwccss.Raw("margin", "10px 0 0 25px"),
		gwccss.Raw("padding-inline-start", "28px"),
		gwccss.Raw("border-inline-start", "1px solid color-mix(in srgb,var(--accent) 32%,var(--line))"),
	)
	declareGlobal(".ownership-tree ul>li",
		gwccss.Position.Relative,
	)
	declareGlobal(".ownership-tree ul>li:before",
		gwccss.Position.Absolute,
		gwccss.Top(gwccss.Px(27)),
		gwccss.Raw("inset-inline-start", "-28px"),
		gwccss.W(gwccss.Px(28)),
		gwccss.Raw("border-top", "1px solid color-mix(in srgb,var(--accent) 32%,var(--line))"),
		gwccss.Raw("content", "\"\""),
	)
	declareGlobal(".ownership-card",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(11)),
		gwccss.W(gwccss.MinLen(gwccss.Px(440), gwccss.Percent(100))),
		gwccss.MinHeight(gwccss.Px(58)),
		gwccss.PaddingY(gwccss.Px(9)), gwccss.PaddingX(gwccss.Px(12)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".ownership-card:hover",
		gwccss.BorderColor(gwccss.Var("hcm-hover-border")),
		gwccss.Bg(gwccss.Var("hcm-hover-surface")),
	)
	declareGlobal(".ownership-card .row-main small",
		gwccss.Raw("white-space", "normal"),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".ownership-card .row-main strong", gwccss.Raw("overflow-wrap", "anywhere"))
	declareGlobal(".ownership-card.selected",
		gwccss.Raw("outline", "2px solid var(--accent)"), gwccss.Raw("outline-offset", "1px"),
	)
	declareGlobal(".ownership-manager-summary,.ownership-explanation",
		gwccss.Display.Block,
		gwccss.TextColor(gwccss.Var("muted")),
	)
	declareGlobal(".ownership-toggle",
		gwccss.Display.InlineFlex, gwccss.Items.Center,
		gwccss.Raw("justify-content", "center"),
		gwccss.Raw("cursor", "pointer"), gwccss.MinHeight(gwccss.Px(44)), gwccss.MinWidth(gwccss.Px(44)),
		gwccss.Padding(gwccss.Px(10)), gwccss.TextColor(gwccss.Var("accent")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Border(gwccss.Px(0), gwccss.Var("line")),
		gwccss.Bg(gwccss.Transparent),
	)
	declareGlobal(".ownership-toggle:hover,.organization-unit-disclosure>summary:hover",
		gwccss.Bg(gwccss.Var("hcm-hover-surface")),
	)
	declareGlobal(".ownership-toggle:focus-visible,.organization-unit-disclosure>summary:focus-visible",
		gwccss.Raw("outline", "2px solid var(--accent)"), gwccss.Raw("outline-offset", "-3px"),
	)
	declareGlobal(".ownership-count",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.MinWidth(gwccss.Px(25)),
		gwccss.H(gwccss.Px(25)),
		gwccss.Rounded(gwccss.Px(999)),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "800"),
	)
	declareGlobal(".organization-view-head",
		mediaRule(gwccss.MaxW(760), gwccss.Items.Stretch, gwccss.FlexDir.Col),
	)
	declareGlobal(".organization-view-toggle",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("align-self", "flex-start")),
	)
	declareGlobal(".ownership-tree ul",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("margin-inline-start", "16px"), gwccss.Raw("padding-inline-start", "18px")),
	)
	declareGlobal(".ownership-tree ul>li:before",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("inset-inline-start", "-18px"), gwccss.W(gwccss.Px(18))),
	)
	declareGlobal(".organization-view-toggle",
		mediaRule(gwccss.MaxW(430), gwccss.Raw("align-self", "stretch")),
	)
	declareGlobal(".organization-view-option",
		mediaRule(gwccss.MaxW(430), gwccss.Raw("flex", "1"), gwccss.Raw("text-align", "center")),
	)
	declareGlobal(".organization-search-control",
		mediaRule(gwccss.MaxW(430), gwccss.FlexDir.Col),
	)
	declareGlobal(".organization-search-control>.button",
		mediaRule(gwccss.MaxW(430), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal(".organization-search-control>input",
		mediaRule(gwccss.MaxW(430), gwccss.Raw("flex", "0 0 auto"), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal(".organization-browse-controls",
		mediaRule(gwccss.MaxW(430), gwccss.Items.Stretch, gwccss.Raw("align-items", "stretch")),
	)
	declareGlobal(".organization-browse-controls .button",
		mediaRule(gwccss.MaxW(430), gwccss.Raw("flex", "1 1 auto")),
	)
	declareGlobal(".ownership-tree ul",
		mediaRule(gwccss.MaxW(430), gwccss.Raw("margin-inline-start", "8px"), gwccss.Raw("padding-inline-start", "12px")),
	)
	declareGlobal(".ownership-tree ul>li:before",
		mediaRule(gwccss.MaxW(430), gwccss.Raw("inset-inline-start", "-12px"), gwccss.W(gwccss.Px(12))),
	)
	declareGlobal(".organization-view-toggle,.organization-view-option.active,.ownership-card,.ownership-tree ul,.ownership-tree ul>li:before",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.BorderColor(gwccss.Color("CanvasText"))),
	)
}

func myselfStylesStylesheet() string {
	return buildTypedSheet(declaremyselfStylesStyles)
}

func declaremyselfStylesStyles() {
	declareGlobal(".myself-page,.person-profile-composition",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.RawLength("var(--theme-section-gap,18px)")),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".self-service-boundary",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Px(14)),
		gwccss.PaddingY(gwccss.Px(18)), gwccss.PaddingX(gwccss.Px(20)),
		gwccss.Raw("border-color", "color-mix(in srgb,var(--accent) 42%,var(--line))"),
		gwccss.Raw("background", "color-mix(in srgb,var(--soft) 68%,var(--surface))"),
	)
	declareGlobal(".self-service-boundary-icon",
		gwccss.Display.Grid,
		gwccss.Raw("place-items", "center"),
		gwccss.W(gwccss.Px(38)),
		gwccss.H(gwccss.Px(38)),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("accent")),
		gwccss.Raw("color", "var(--on-brand,#fff)"),
		gwccss.Raw("font-weight", "800"),
	)
	declareGlobal(".self-service-boundary-glyph",
		gwccss.W(gwccss.Px(19)),
		gwccss.H(gwccss.Px(19)),
	)
	declareGlobal(".self-service-boundary-copy h2",
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(1)),
	)
	declareGlobal(".self-service-boundary-copy p",
		gwccss.Raw("margin", "3px 0 0"),
	)
	declareGlobal(".self-service-badge",
		gwccss.Raw("white-space", "nowrap"),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("accent")),
		gwccss.Raw("border", "1px solid color-mix(in srgb,var(--accent) 35%,var(--line))"),
	)
	declareGlobal(".profile-data-boundary",
		gwccss.Margin(gwccss.Zero),
		gwccss.PaddingY(gwccss.Px(14)), gwccss.PaddingX(gwccss.Px(22)),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".myself-page .compensation-details",
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("accent"))),
	)
	declareGlobal(".self-service-boundary",
		mediaRule(gwccss.MaxW(680), gwccss.GridCols(gwccss.TrackLen(gwccss.RawLength("auto")), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".self-service-badge",
		mediaRule(gwccss.MaxW(680), gwccss.GridColumn(gwccss.GridLineAt(2)), gwccss.Raw("justify-self", "start")),
	)
	declareGlobal(".self-service-boundary",
		mediaRule(gwccss.MaxW(420), gwccss.GridCols(gwccss.Fr(1)), gwccss.Gap(gwccss.Px(9)), gwccss.PaddingY(gwccss.Px(16)), gwccss.PaddingX(gwccss.Px(16))),
	)
	declareGlobal(".self-service-boundary-icon",
		mediaRule(gwccss.MaxW(420), gwccss.Display.None),
	)
	declareGlobal(".self-service-badge",
		mediaRule(gwccss.MaxW(420), gwccss.GridColumn(gwccss.GridLineAt(1))),
	)
	declareGlobal(".self-service-boundary,.self-service-badge,.self-service-boundary-icon",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Border(gwccss.Px(1), gwccss.Color("CanvasText"))),
	)
	declareGlobal(".self-service-boundary-icon",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Bg(gwccss.Color("Highlight")), gwccss.TextColor(gwccss.Color("HighlightText"))),
	)
}

func organizationMetadataStylesStylesheet() string {
	return buildTypedSheet(declareorganizationMetadataStylesStyles)
}

func declareorganizationMetadataStylesStyles() {
	declareGlobal(".organization-page",
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.VarLength("theme-section-gap")),
	)
	declareGlobal(".organization-page>.panel",
		gwccss.Raw("margin-top", "0"),
	)
	declareGlobal(".organization-metadata",
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".organization-metadata .section-head",
		gwccss.Raw("align-items", "flex-start"),
	)
	declareGlobal(".organization-metadata .section-head p",
		gwccss.MaxWidth(gwccss.Ch(66)),
		gwccss.Raw("margin", "4px 0 0"),
	)
	declareGlobal(".business-metadata-body",
		gwccss.Raw("padding", "0 var(--theme-panel-padding) var(--theme-panel-padding)"),
	)
	declareGlobal(".business-metadata-grid",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Repeat(4, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
		gwccss.Gap(gwccss.Px(10)),
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".business-metadata-item",
		gwccss.MinWidth(gwccss.Zero),
		gwccss.PaddingY(gwccss.Px(15)), gwccss.PaddingX(gwccss.Px(16)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("divider")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.Bg(gwccss.Var("surface-subtle")),
	)
	declareGlobal(".business-metadata-item:first-child",
		gwccss.GridColumn(gwccss.GridSpan(2)),
		gwccss.Raw("background", "color-mix(in srgb,var(--soft) 72%,var(--surface))"),
	)
	declareGlobal(".business-metadata-item dt",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
		gwccss.Tracking(gwccss.Ems(.035)),
		gwccss.Raw("text-transform", "uppercase"),
	)
	declareGlobal(".business-metadata-item dd",
		gwccss.Raw("margin", "5px 0 0"),
		gwccss.Raw("overflow-wrap", "anywhere"),
		gwccss.TextColor(gwccss.Var("ink")),
		gwccss.FontSize(gwccss.Rem(1.125)),
		gwccss.Raw("font-weight", "700"),
		gwccss.LineHeight(gwccss.Num(1.28)),
	)
	declareGlobal(".business-footprint",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Px(150)), gwccss.Fr(.45)), gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1.55))),
		gwccss.Gap(gwccss.Px(18)),
		gwccss.Raw("align-items", "start"),
		gwccss.Raw("margin-top", "18px"),
		gwccss.Raw("padding-top", "18px"),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("divider")),
	)
	declareGlobal(".business-footprint h3",
		gwccss.MarginY(gwccss.Px(4)), gwccss.MarginX(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)),
	)
	declareGlobal(".business-footprint ul",
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Px(7)),
		gwccss.Margin(gwccss.Zero),
		gwccss.Padding(gwccss.Zero),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".business-footprint li",
		gwccss.PaddingY(gwccss.Px(5)), gwccss.PaddingX(gwccss.Px(9)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.Px(999)),
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(".business-metadata-boundary",
		gwccss.Raw("margin", "16px 0 0"),
		gwccss.PaddingY(gwccss.Px(11)), gwccss.PaddingX(gwccss.Px(13)),
		gwccss.Raw("border-inline-start", "3px solid var(--accent)"),
		gwccss.Bg(gwccss.Var("soft")),
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
	)
	declareGlobal(".business-metadata-grid",
		mediaRule(gwccss.MaxW(1050), gwccss.GridCols(gwccss.Repeat(3, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))))),
	)
	declareGlobal(".business-metadata-grid",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Repeat(2, gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))))),
	)
	declareGlobal(".business-metadata-item:first-child",
		mediaRule(gwccss.MaxW(760), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1)))),
	)
	declareGlobal(".business-footprint",
		mediaRule(gwccss.MaxW(760), gwccss.GridCols(gwccss.Fr(1)), gwccss.Gap(gwccss.Px(8))),
	)
	declareGlobal(".business-metadata-grid",
		mediaRule(gwccss.MaxW(430), gwccss.GridCols(gwccss.Fr(1))),
	)
	declareGlobal(".business-metadata-item:first-child",
		mediaRule(gwccss.MaxW(430), gwccss.GridColumn(gwccss.GridLineAt(1))),
	)
	declareGlobal(".business-metadata-item,.business-footprint li,.business-metadata-boundary",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Border(gwccss.Px(1), gwccss.Color("CanvasText"))),
	)
	declareGlobal(".business-metadata-boundary",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"), gwccss.Raw("border-inline-start-width", "3px")),
	)
}
