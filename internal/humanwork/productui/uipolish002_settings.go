package productui

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

// uipolish002SettingsStylesheet keeps the single account group from occupying
// only the first track of the legacy two-column overview grid.
func uipolish002SettingsStylesheet() string {
	return buildTypedSheet(func() {
		declareGlobal(".settings-group-stack",
			gwccss.Display.Grid,
			gwccss.Raw("gap", "calc(var(--hcm-space-4) * var(--hcm-density))"),
		)
		declareGlobal(".settings-page-stack .settings-overview-grid:has(>.settings-account-group:only-child)",
			gwccss.Raw("grid-template-columns", "minmax(0,1fr)"),
		)
		declareGlobal(".settings-account-group .settings-group-description",
			gwccss.Raw("max-inline-size", "65ch"),
		)
		declareGlobal(".settings-account-group .settings-group-content",
			gwccss.Display.Grid,
			gwccss.Raw("grid-template-columns", "repeat(auto-fit,minmax(min(100%,22rem),1fr))"),
			gwccss.Raw("align-items", "start"),
			gwccss.Raw("gap", "calc(var(--hcm-space-3) * var(--hcm-density))"),
			gwccss.Raw("margin-block-start", "calc(var(--hcm-space-2) * var(--hcm-density))"),
		)
		declareGlobal(".settings-account-group .settings-group-content>:is(.settings-context,.settings-task-card,.settings-signout)",
			gwccss.Raw("min-inline-size", "0"),
			gwccss.Raw("margin", "0"),
			gwccss.Raw("padding", "calc(var(--hcm-space-3) * var(--hcm-density))"),
			gwccss.Raw("border", "1px solid var(--line)"),
			gwccss.Raw("border-radius", "var(--hcm-radius-surface)"),
			gwccss.Raw("background", "var(--surface)"),
		)
		declareGlobal(".settings-account-group .settings-group-content>:is(.settings-context,.settings-task-card,.settings-signout) :is(h2,h3)",
			gwccss.Raw("margin-block-start", "0"),
		)
		declareGlobal(".settings-account-group .settings-group-content>.settings-signout",
			gwccss.Raw("grid-column", "1 / -1"),
		)
		declareGlobal(".settings-account-group .settings-group-content>.settings-task-card",
			gwccss.Display.Grid,
			gwccss.Raw("align-content", "start"),
			gwccss.Raw("gap", "calc(var(--hcm-space-2) * var(--hcm-density))"),
		)
		declareGlobal(".settings-account-group .settings-group-content>.settings-task-card p",
			gwccss.Margin(gwccss.Zero),
		)
		declareGlobal(".settings-account-group .settings-group-content>.settings-task-card .button",
			gwccss.Raw("justify-self", "start"),
			gwccss.MinHeight(gwccss.Px(44)),
		)
		declareGlobal(".settings-organization-group .settings-group-content",
			gwccss.Display.Grid,
			gwccss.Raw("margin-block-start", "calc(var(--hcm-space-2) * var(--hcm-density))"),
		)
		declareGlobal(".settings-organization-group .settings-task-card",
			gwccss.Display.Grid,
			gwccss.Raw("align-content", "start"),
			gwccss.Raw("justify-items", "start"),
			gwccss.Raw("gap", "calc(var(--hcm-space-2) * var(--hcm-density))"),
			gwccss.Raw("margin", "0"),
			gwccss.Raw("padding", "calc(var(--hcm-space-3) * var(--hcm-density))"),
			gwccss.Raw("border", "1px solid var(--line)"),
			gwccss.Raw("border-radius", "var(--hcm-radius-surface)"),
			gwccss.Raw("background", "var(--surface)"),
		)
		declareGlobal(".settings-organization-group .settings-task-card :is(h3,p)",
			gwccss.Margin(gwccss.Zero),
		)
		declareGlobal(".settings-organization-group .settings-task-card .button",
			gwccss.MinHeight(gwccss.Px(44)),
		)
		declareGlobal(".settings-preferences-group>.settings-group-content",
			gwccss.Display.Grid,
			gwccss.Raw("grid-template-columns", "minmax(0,1fr)"),
			gwccss.Raw("gap", "calc(var(--hcm-space-3) * var(--hcm-density))"),
		)
		declareGlobal(".settings-preferences-group>.settings-group-content>.settings-task-card",
			gwccss.Display.Grid,
			gwccss.Raw("align-content", "start"),
			gwccss.Raw("gap", "calc(var(--hcm-space-2) * var(--hcm-density))"),
			gwccss.Raw("padding", "calc(var(--hcm-space-3) * var(--hcm-density))"),
		)
		declareGlobal(".settings-preferences-group>.settings-group-content>.settings-task-card :is(h3,p)",
			gwccss.Margin(gwccss.Zero),
		)
		declareGlobal(".settings-preferences-group>.settings-group-content>.settings-task-card .button",
			gwccss.MinHeight(gwccss.Px(44)),
		)
		declareGlobal(".settings-navigation-choices",
			gwccss.Display.Flex,
			gwccss.Raw("flex-wrap", "wrap"),
			gwccss.Raw("gap", "calc(var(--hcm-space-2) * var(--hcm-density))"),
		)
		declareGlobal(".settings-navigation-choices .button",
			gwccss.Display.InlineFlex,
			gwccss.Raw("align-items", "center"),
			gwccss.Raw("gap", "calc(var(--hcm-space-2) * var(--hcm-density))"),
			gwccss.MinHeight(gwccss.Px(44)),
		)
		declareGlobal(".settings-navigation-choices .button[aria-current=true]",
			gwccss.BorderColor(gwccss.Var("accent")),
			gwccss.Bg(gwccss.Var("soft")),
			gwccss.Raw("box-shadow", "inset 0 0 0 1px var(--accent)"),
		)
		declareGlobal(".settings-navigation-current",
			gwccss.FontSize(gwccss.Rem(0.75)),
			gwccss.Raw("font-weight", "700"),
			gwccss.TextColor(gwccss.Var("accent")),
		)
		declareGlobal(".settings-preferences-group .locale-choice-list",
			gwccss.Raw("grid-template-columns", "repeat(3,minmax(0,1fr))"),
			mediaRule(gwccss.MaxW(900), gwccss.Raw("grid-template-columns", "repeat(2,minmax(0,1fr))")),
			mediaRule(gwccss.MaxW(600), gwccss.Raw("grid-template-columns", "minmax(0,1fr)")),
		)
		declareGlobal(".settings-preferences-group :is(.accessibility-group-contrast,.accessibility-group-links) .accessibility-options",
			gwccss.Raw("grid-template-columns", "repeat(2,minmax(0,1fr))"),
			mediaRule(gwccss.MaxW(760), gwccss.Raw("grid-template-columns", "minmax(0,1fr)")),
		)
		declareGlobal(".settings-task-card.viewer-profile-card",
			gwccss.Display.Grid,
			gwccss.Raw("grid-template-columns", "minmax(0,1fr) auto"),
			gwccss.Raw("gap", "calc(var(--hcm-space-3) * var(--hcm-density))"),
			gwccss.Raw("align-items", "center"),
		)
		declareGlobal(".viewer-profile-card .settings-profile-content",
			gwccss.MinWidth(gwccss.Zero),
		)
		declareGlobal(".viewer-profile-card .settings-profile-identity",
			gwccss.Display.Flex,
			gwccss.Raw("align-items", "center"),
			gwccss.Raw("gap", "calc(var(--hcm-space-3) * var(--hcm-density))"),
			gwccss.Raw("margin-block-start", "calc(var(--hcm-space-2) * var(--hcm-density))"),
		)
		declareGlobal(".viewer-profile-card .settings-profile-action",
			gwccss.Raw("justify-self", "end"),
			gwccss.Raw("white-space", "nowrap"),
		)
		declareGlobal(".settings-task-card.viewer-profile-card",
			mediaRule(gwccss.MaxW(600), gwccss.Raw("grid-template-columns", "minmax(0,1fr)")),
		)
		declareGlobal(".viewer-profile-card .settings-profile-action",
			mediaRule(gwccss.MaxW(600), gwccss.Raw("justify-self", "start")),
		)
	})
}
