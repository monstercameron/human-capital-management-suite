package productui

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

func ChatRetentionStylesheet() string {
	return buildTypedSheet(func() {
		declareGlobal(".chat-settings-page",
			gwccss.Display.Grid,
			gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
			gwccss.Gap(gwccss.RawLength("calc(var(--hcm-space-3) * var(--hcm-density))")),
			gwccss.Raw("min-width", "0"),
		)
		declareGlobal(".chat-settings-page>p", gwccss.Margin(gwccss.Zero))
		declareGlobal(".chat-settings-page>.button",
			gwccss.Raw("justify-self", "start"),
		)
		declareGlobal(".chat-retention-settings",
			gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1))),
			gwccss.Display.Grid,
			gwccss.Gap(gwccss.RawLength("calc(var(--hcm-space-3) * var(--hcm-density))")),
			gwccss.Padding(gwccss.RawLength("calc(var(--hcm-space-4) * var(--hcm-density))")),
			mediaRule(gwccss.MaxW(680), gwccss.Padding(gwccss.RawLength("calc(var(--hcm-space-3) * var(--hcm-density))"))),
		)
		declareGlobal(".chat-retention-settings h2,.chat-retention-settings p",
			gwccss.Margin(gwccss.Zero),
		)
		declareGlobal(".chat-retention-settings h2",
			gwccss.FontSize(gwccss.VarLength("hcm-font-size-body")), gwccss.Raw("font-weight", "650"), gwccss.Raw("letter-spacing", "-0.01em"),
		)
		declareGlobal(".chat-retention-settings header",
			gwccss.Display.Grid, gwccss.Gap(gwccss.RawLength("calc(var(--hcm-space-1) * var(--hcm-density))")),
		)
		declareGlobal(".chat-retention-current",
			gwccss.PaddingY(gwccss.RawLength("calc(var(--hcm-space-2) * var(--hcm-density))")),
			gwccss.PaddingX(gwccss.RawLength("calc(var(--hcm-space-2) * var(--hcm-density))")),
			gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
			gwccss.Bg(gwccss.Var("surface-subtle")), gwccss.TextColor(gwccss.Var("ink")),
			gwccss.FontSize(gwccss.VarLength("hcm-font-size-small")), gwccss.Raw("font-weight", "600"),
		)
		declareGlobal(".chat-retention-form",
			gwccss.Display.Grid, gwccss.Gap(gwccss.RawLength("calc(var(--hcm-space-3) * var(--hcm-density))")), gwccss.MaxWidth(gwccss.Px(720)),
		)
		declareGlobal(".chat-retention-field,.chat-retention-policy-field",
			gwccss.Display.Grid, gwccss.Gap(gwccss.RawLength("calc(var(--hcm-space-1) * var(--hcm-density))")),
		)
		declareGlobal(".chat-retention-field label,.chat-retention-policy-field label",
			gwccss.TextColor(gwccss.Var("ink")), gwccss.FontSize(gwccss.VarLength("hcm-font-size-small")), gwccss.Raw("font-weight", "600"),
		)
		declareGlobal(".chat-retention-field select,.chat-retention-policy-field input",
			gwccss.W(gwccss.Percent(100)), gwccss.MinHeight(gwccss.VarLength("hcm-control-height")),
			gwccss.PaddingY(gwccss.RawLength("calc(var(--hcm-space-1) * var(--hcm-density))")),
			gwccss.PaddingX(gwccss.RawLength("calc(var(--hcm-space-2) * var(--hcm-density))")),
			gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
			gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
			gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		)
		declareGlobal(".chat-retention-budget-input",
			gwccss.Display.Grid, gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("max-content"))),
			gwccss.Items.Center, gwccss.Gap(gwccss.RawLength("calc(var(--hcm-space-2) * var(--hcm-density))")),
		)
		declareGlobal(".chat-retention-budget-input span",
			gwccss.TextColor(gwccss.Var("muted")), gwccss.FontSize(gwccss.VarLength("hcm-font-size-small")),
		)
		declareGlobal(".chat-retention-policy-field>.muted,.chat-retention-settings>.muted",
			gwccss.FontSize(gwccss.VarLength("hcm-font-size-small")), gwccss.Raw("line-height", "var(--hcm-line-height)"),
		)
		declareGlobal(".chat-retention-form>.button",
			gwccss.Raw("justify-self", "start"),
		)
		declareGlobal(".chat-retention-confirm-backdrop",
			gwccss.Position.Fixed, gwccss.Raw("inset", "0"), gwccss.ZIndex(100),
			gwccss.Display.Grid, gwccss.Raw("place-items", "center"),
			gwccss.Padding(gwccss.RawLength("calc(var(--hcm-space-4) * var(--hcm-density))")),
			gwccss.Raw("background", "color-mix(in srgb,var(--ink) 42%,transparent)"),
		)
		declareGlobal(".chat-retention-confirm",
			gwccss.Display.Grid, gwccss.Gap(gwccss.RawLength("calc(var(--hcm-space-2) * var(--hcm-density))")), gwccss.W(gwccss.Percent(100)), gwccss.MaxWidth(gwccss.Px(480)),
			gwccss.Padding(gwccss.RawLength("calc(var(--hcm-space-4) * var(--hcm-density))")),
			gwccss.Raw("box-shadow", "var(--hcm-shadow-raised)"),
		)
		declareGlobal(".chat-retention-confirm h3,.chat-retention-confirm p",
			gwccss.Margin(gwccss.Zero),
		)
		declareGlobal(".chat-retention-confirm h3",
			gwccss.FontSize(gwccss.VarLength("hcm-font-size-body")), gwccss.TextColor(gwccss.Var("ink")),
		)
		declareGlobal(".chat-retention-confirm>.button",
			gwccss.Raw("justify-self", "start"),
		)
		declareGlobal(".chat-retention-form", mediaRule(gwccss.MaxW(680), gwccss.Raw("max-width", "none")))
	})
}
