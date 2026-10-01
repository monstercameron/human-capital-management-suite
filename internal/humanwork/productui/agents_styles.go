package productui

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

// agentsStylesheet returns the responsive visual contract for the Agents
// workspace. Content and availability remain owned by agents_page.go; this
// sheet only gives that semantic markup a bounded, theme-aware layout.
func agentsStylesheet() string {
	return buildTypedSheet(declareAgentsStyles)
}

func declareAgentsStyles() {
	declareGlobal(".agents-page",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(20)),
		gwccss.MinWidth(gwccss.Zero), gwccss.W(gwccss.Percent(100)),
		gwccss.Raw("padding-block", "24px"), gwccss.Raw("padding-inline", "clamp(16px, 3vw, 32px)"),
	)
	declareGlobal(".agents-page > h1",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".agents-page > .agents-page-subtitle",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".agents-page-subtitle",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("max-inline-size", "70ch"),
	)
	declareGlobal(".agents-layout",
		gwccss.Display.Grid,
		gwccss.GridCols(
			gwccss.MinMax(gwccss.TrackLen(gwccss.Px(224)), gwccss.TrackLen(gwccss.Px(288))),
			gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)),
		),
		gwccss.Items.Start,
		gwccss.AlignSelf.Start,
		gwccss.Gap(gwccss.Px(16)), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".agents-main",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(16)), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".agents-sidebar,.agents-threads,.agents-tasks,.agents-composer,.agents-task-view,.agents-thread,.agents-task-group,.agents-task-detail,.agents-task-plan,.agents-task-live,.agents-plan-diff,.agents-approval-card",
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Raw("padding-block", "16px"), gwccss.Raw("padding-inline", "18px"),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".agents-sidebar,.agents-threads,.agents-tasks,.agents-composer,.agents-task-view",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(12)),
	)
	declareGlobal(".agents-sidebar h2,.agents-threads h2,.agents-tasks h2,.agents-thread h2,.agents-task-view h2,.agents-task-group h3,.agents-task-view h3,.agents-task-view h4,.agents-composer label",
		gwccss.Margin(gwccss.Zero), gwccss.FontWeight.Semibold,
	)
	declareGlobal(".agents-agent-list,.agents-post-list,.agents-task-list,.agents-skills,.agents-task-plan ol,.agents-task-detail ul,.agents-approval-card ul",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(10)),
		gwccss.Margin(gwccss.Zero), gwccss.Padding(gwccss.Zero),
		gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".agents-agent-item,.agents-post,.agents-task-row,.agents-plan-step",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(6)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".agents-agent-item,.agents-post,.agents-task-row,.agents-plan-step,.agents-approval-card",
		gwccss.Raw("border-block-start", "1px solid var(--control-border,var(--line))"),
		gwccss.Raw("padding-block", "10px"),
	)
	declareGlobal(".agents-agent-item:first-child,.agents-post:first-child,.agents-task-row:first-child,.agents-plan-step:first-child",
		gwccss.Raw("border-block-start", "0"), gwccss.Raw("padding-block-start", "0"),
	)
	declareGlobal(".agents-agent-status,.agents-post-at,.agents-step-tier",
		gwccss.TextColor(gwccss.Var("muted")), gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(".agents-skills",
		gwccss.Display.Flex, gwccss.Raw("flex-wrap", "wrap"), gwccss.Gap(gwccss.Px(6)),
	)
	declareGlobal(".agents-skill,.agents-acting-for,.agents-task-status,.agents-taint",
		gwccss.Display.InlineFlex, gwccss.Items.Center,
		gwccss.MinHeight(gwccss.Px(28)), gwccss.Raw("padding-block", "3px"), gwccss.Raw("padding-inline", "8px"),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".agents-post-body,.agents-task-goal,.agents-task-answer p,.agents-task-live p,.agents-plan-diff pre,.agents-digest",
		gwccss.Margin(gwccss.Zero), gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".agents-task-answer p",
		gwccss.Raw("white-space", "pre-line"),
	)
	declareGlobal(".agents-plan-diff pre",
		gwccss.Raw("white-space", "pre-wrap"), gwccss.Raw("overflow", "auto"),
		gwccss.Raw("background", "var(--surface-subtle,var(--canvas))"),
		gwccss.Raw("padding-block", "10px"), gwccss.Raw("padding-inline", "12px"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
	)
	declareGlobal(".agents-composer textarea",
		gwccss.Display.Block, gwccss.W(gwccss.Percent(100)), gwccss.MinWidth(gwccss.Zero),
		gwccss.MinHeight(gwccss.Px(96)), gwccss.Raw("padding-block", "10px"), gwccss.Raw("padding-inline", "12px"),
		gwccss.Raw("resize", "vertical"),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Bg(gwccss.Var("canvas")), gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".agents-composer-help,.agents-composer-status",
		gwccss.Margin(gwccss.Zero), gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".agents-composer-actions,.agents-task-actions",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(10)),
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal(".agents-page .button,.agents-task-link",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Justify.Center,
		gwccss.MinHeight(gwccss.Px(44)), gwccss.MinWidth(gwccss.Px(44)),
		gwccss.Raw("max-inline-size", "100%"), gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".agents-task-link",
		gwccss.Raw("padding-block", "8px"), gwccss.Raw("padding-inline", "12px"),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Raw("background", "var(--surface-subtle,var(--canvas))"),
		gwccss.TextColor(gwccss.Var("ink")), gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".agents-page :is(.button,.agents-task-link,.agents-composer textarea):focus-visible",
		gwccss.Raw("outline", "2px solid var(--hcm-color-focus,var(--accent))"),
		gwccss.OutlineOffset(gwccss.Px(3)),
		gwccss.Raw("box-shadow", "0 0 0 3px var(--surface)"),
	)
	declareGlobal(".agents-task-heading",
		gwccss.Display.Grid, gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.RawLength("auto"))),
		gwccss.Items.Center, gwccss.Gap(gwccss.Px(10)), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".agents-task-heading .status",
		gwccss.JustifySelf.End, gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(".agents-empty-thread,.agents-page-unavailable,.agents-page-disabled",
		gwccss.MinWidth(gwccss.Zero), gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".agents-page-unavailable,.agents-page-disabled",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(12)),
	)
	declareGlobal(".agents-page > .agents-task-view",
		gwccss.Raw("max-inline-size", "100%"),
	)
	declareGlobal(".agents-page > .agents-task-view .agents-task-detail ul",
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal("#agent-rollout-portable,#agent-rollout-panel,#agent-portable-panel,#agent-portable-draft",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(12)), gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("max-inline-size", "100%"), gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal("#agent-rollout-portable form,.agent-portable-mapping",
		gwccss.Display.Grid, gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.Gap(gwccss.Px(8)), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal("#agent-rollout-portable :is(input,select,textarea,fieldset)",
		gwccss.W(gwccss.Percent(100)), gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("min-inline-size", "0"), gwccss.Raw("max-inline-size", "100%"), gwccss.Raw("box-sizing", "border-box"),
	)
	declareGlobal("#agent-rollout-portable :is(input,select,textarea)",
		gwccss.MinHeight(gwccss.Px(44)), gwccss.Raw("padding", "8px 12px"),
		gwccss.Bg(gwccss.Var("canvas")), gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal("#agent-rollout-portable button",
		gwccss.MinHeight(gwccss.Px(44)), gwccss.Raw("max-inline-size", "100%"),
		gwccss.Raw("white-space", "normal"), gwccss.Raw("overflow-wrap", "anywhere"), gwccss.JustifySelf.Start,
	)
	declareGlobal("#agent-rollout-portable :is(input,select,textarea,button):focus-visible",
		gwccss.Raw("outline", "2px solid var(--hcm-color-focus,var(--accent))"), gwccss.OutlineOffset(gwccss.Px(3)),
	)
	declareGlobal(".agent-portable-draft-instructions",
		gwccss.MinWidth(gwccss.Zero), gwccss.Raw("white-space", "pre-wrap"), gwccss.Raw("overflow-wrap", "anywhere"),
	)

	declareGlobal(".agents-layout",
		mediaRule(gwccss.MaxW(800),
			gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		),
	)
	declareGlobal(".agents-page",
		mediaRule(gwccss.MaxW(480),
			gwccss.Gap(gwccss.Px(14)), gwccss.Raw("padding-block", "16px"), gwccss.Raw("padding-inline", "12px"),
		),
	)
	declareGlobal(".agents-sidebar,.agents-threads,.agents-tasks,.agents-composer,.agents-task-view,.agents-thread,.agents-task-group,.agents-task-detail,.agents-task-plan,.agents-task-live,.agents-plan-diff,.agents-approval-card",
		mediaRule(gwccss.MaxW(480), gwccss.Raw("padding-block", "14px"), gwccss.Raw("padding-inline", "14px")),
	)
	declareGlobal(".agents-composer-actions .button,.agents-task-actions .button",
		mediaRule(gwccss.MaxW(390), gwccss.Raw("flex", "1 1 100%"), gwccss.W(gwccss.Percent(100))),
	)
}
