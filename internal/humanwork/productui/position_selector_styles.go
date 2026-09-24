package productui

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

func positionSelectorStylesheet() string {
	return buildTypedSheet(func() {
		declareGlobal(".position-selector",
			gwccss.Display.Grid,
			gwccss.Gap(gwccss.RawLength("calc(var(--hcm-space-2) * var(--hcm-density))")),
			gwccss.MaxWidth(gwccss.Px(640)),
			gwccss.Raw("min-width", "0"),
		)
		declareGlobal(".position-selector label",
			gwccss.TextColor(gwccss.Var("ink")),
			gwccss.FontSize(gwccss.VarLength("hcm-font-size-small")),
			gwccss.Raw("font-weight", "600"),
		)
		declareGlobal(".position-selector select",
			gwccss.W(gwccss.Percent(100)),
			gwccss.MinHeight(gwccss.VarLength("hcm-control-height")),
			gwccss.PaddingY(gwccss.RawLength("calc(var(--hcm-space-1) * var(--hcm-density))")),
			gwccss.PaddingX(gwccss.RawLength("calc(var(--hcm-space-2) * var(--hcm-density))")),
			gwccss.TextColor(gwccss.Var("ink")),
			gwccss.Bg(gwccss.Var("surface")),
			gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
			gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		)
		declareGlobal(".position-selector-help,.position-selector-empty",
			gwccss.Margin(gwccss.Zero),
			gwccss.TextColor(gwccss.Var("muted")),
			gwccss.FontSize(gwccss.VarLength("hcm-font-size-small")),
		)
		declareGlobal(".position-selector-submit", gwccss.Raw("justify-self", "start"))
	})
}
