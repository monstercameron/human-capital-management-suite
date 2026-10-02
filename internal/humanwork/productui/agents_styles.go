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
		gwccss.Raw("box-sizing", "border-box"),
	)
	declareGlobal(".agents-page [hidden]", gwccss.Raw("display", "none !important"))
	declareGlobal(".agents-page-header",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Justify.Between, gwccss.Gap(gwccss.Px(12)), gwccss.Raw("flex-wrap", "wrap"),
		gwccss.W(gwccss.Percent(100)),
	)
	declareGlobal(".agents-page-nav",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(2)), gwccss.Raw("flex-wrap", "nowrap"),
		gwccss.Raw("padding", "3px"), gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")), gwccss.Bg(gwccss.Var("canvas")),
	)
	declareGlobal(".agents-page-nav-item",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Justify.Center, gwccss.MinHeight(gwccss.Px(44)),
		gwccss.Raw("padding-inline", "12px"), gwccss.Raw("white-space", "nowrap"), gwccss.Raw("text-decoration", "none"),
		gwccss.TextColor(gwccss.Var("ink")), gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
	)
	declareGlobal("a.agents-page-nav-item:hover", gwccss.Bg(gwccss.Var("surface")))
	declareGlobal(".agents-page-nav-item[aria-current=page]", gwccss.Bg(gwccss.Var("surface")), gwccss.FontWeight.Semibold, gwccss.Raw("box-shadow", "var(--hcm-shadow-resting)"))
	declareGlobal(".agents-page-header h1", gwccss.Margin(gwccss.Zero))
	declareGlobal(".agents-page > h1",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".agents-page > .agents-page-subtitle",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".agents-page-subtitle",
		gwccss.TextColor(gwccss.Var("muted")),
		gwccss.W(gwccss.Percent(100)),
	)
	declareGlobal(".agents-layout",
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.Items.Start,
		gwccss.AlignSelf.Start,
		gwccss.Gap(gwccss.Px(20)), gwccss.MinWidth(gwccss.Zero), gwccss.W(gwccss.Percent(100)),
	)
	declareGlobal(".agents-main",
		gwccss.Display.Grid, gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))), gwccss.Gap(gwccss.Px(16)), gwccss.MinWidth(gwccss.Zero), gwccss.W(gwccss.Percent(100)),
	)
	declareGlobal(".agents-composer,.agents-tasks,.agents-task-view", gwccss.W(gwccss.Percent(100)), gwccss.Raw("box-sizing", "border-box"))
	declareGlobal(".agents-composer,.agents-task-view",
		gwccss.Bg(gwccss.Var("surface")),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.Raw("padding-block", "20px"), gwccss.Raw("padding-inline", "20px"),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".agents-threads,.agents-tasks,.agents-composer,.agents-task-view",
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
	// An implicit auto grid track measures task links by their content. Give the
	// list one explicit flexible track so every row shares the tabs' full width.
	declareGlobal(".agents-task-list", gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))), gwccss.W(gwccss.Percent(100)), gwccss.MinWidth(gwccss.Zero), gwccss.Raw("justify-items", "stretch"))
	declareGlobal(".agents-agent-item,.agents-post,.agents-plan-step",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(6)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".agents-agent-item,.agents-post,.agents-plan-step,.agents-approval-card",
		gwccss.Raw("border-block-start", "1px solid var(--control-border,var(--line))"),
		gwccss.Raw("padding-block", "10px"),
	)
	declareGlobal(".agents-agent-item:first-child,.agents-post:first-child,.agents-plan-step:first-child",
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
		gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".agents-composer textarea:disabled,.agents-composer textarea[readonly]", gwccss.Bg(gwccss.Var("canvas")))
	// An empty status or notice line takes no space. Form controls are excluded:
	// a textarea with nothing typed in it also matches :empty.
	declareGlobal(".agents-composer > :empty:not(textarea):not(input):not(select)", gwccss.Raw("display", "none"))
	declareGlobal(".agents-agent-choices",
		gwccss.Margin(gwccss.Zero), gwccss.Padding(gwccss.Zero), gwccss.Raw("border", "0"), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".agents-agent-choices legend",
		gwccss.Raw("padding", "0"), gwccss.Raw("margin-block-end", "8px"), gwccss.FontWeight.Semibold,
	)
	declareGlobal(".agents-agent-choices > div",
		gwccss.Display.Grid, gwccss.Raw("grid-template-columns", "repeat(auto-fit,minmax(min(100%,220px),1fr))"), gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".agents-agent-choice",
		gwccss.Display.Flex, gwccss.Items.Start, gwccss.Justify.Start, gwccss.Gap(gwccss.Px(8)), gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("padding", "10px 12px"), gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")), gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal(".agents-agent-choice input", gwccss.Raw("flex", "0 0 auto"), gwccss.Raw("margin", "2px 0 0"))
	declareGlobal(".agents-agent-choice span",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(2)), gwccss.MinWidth(gwccss.Zero), gwccss.Raw("text-align", "start"),
	)
	declareGlobal(".agents-no-specialists,.agents-composer-actions-help",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".agents-general-agent,.agents-task-agent",
		gwccss.Margin(gwccss.Zero), gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".agents-composer-help,.agents-composer-status",
		gwccss.Margin(gwccss.Zero), gwccss.TextColor(gwccss.Var("muted")),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".agents-composer-actions,.agents-task-actions",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(10)),
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal(".agents-request-documents,.agents-document-picker-panel,.agents-document-selection",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".agents-document-heading", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(3)))
	declareGlobal(".agents-document-heading p", gwccss.Margin(gwccss.Zero))
	declareGlobal(".agents-add-document", gwccss.JustifySelf.Start)
	declareGlobal(".agents-document-picker-panel",
		gwccss.Raw("padding", "12px"), gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")), gwccss.Bg(gwccss.Var("canvas")),
	)
	declareGlobal(".agents-document-picker-panel input",
		gwccss.W(gwccss.Percent(100)), gwccss.MinWidth(gwccss.Zero), gwccss.MinHeight(gwccss.Px(44)),
		gwccss.Raw("box-sizing", "border-box"), gwccss.Raw("padding", "8px 12px"),
	)
	declareGlobal(".agents-document-picker-panel input:focus-visible", gwccss.Raw("outline", "2px solid var(--hcm-color-focus,var(--accent))"), gwccss.OutlineOffset(gwccss.Px(2)), gwccss.Raw("box-shadow", "none"))
	declareGlobal(".agents-document-results,.agents-document-chips,.agents-task-documents ul,.agents-document-omissions ul",
		gwccss.Display.Flex, gwccss.Raw("flex-wrap", "wrap"), gwccss.Gap(gwccss.Px(8)), gwccss.Margin(gwccss.Zero), gwccss.Padding(gwccss.Zero), gwccss.Raw("list-style", "none"),
	)
	declareGlobal(".agents-document-results", gwccss.Display.Grid, gwccss.W(gwccss.Percent(100)))
	declareGlobal(".agents-document-results li",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(3)), gwccss.W(gwccss.Percent(100)), gwccss.Raw("box-sizing", "border-box"),
		gwccss.Raw("padding", "8px 10px"), gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")), gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal(".agents-document-result-meta,.agents-document-result-snippet", gwccss.Raw("overflow-wrap", "anywhere"))
	declareGlobal(".agents-document-result-snippet", gwccss.Raw("display", "-webkit-box"), gwccss.Raw("-webkit-box-orient", "vertical"), gwccss.Raw("-webkit-line-clamp", "2"), gwccss.Raw("overflow", "hidden"))
	declareGlobal(".agents-document-chip",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(6)), gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("padding", "4px 8px"), gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
	)
	declareGlobal(".agents-document-chip button",
		gwccss.Raw("border", "0"), gwccss.Raw("background", "transparent"), gwccss.TextColor(gwccss.Var("ink")), gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal(".agents-document-icon", gwccss.Raw("inline-size", "1rem"), gwccss.Raw("block-size", "1rem"), gwccss.Raw("flex", "0 0 auto"))
	declareGlobal(".agents-document-count,.agents-document-access,.agents-document-picker-panel p,.agents-tasks-loading,.agents-tasks-failed p,.agents-tasks-empty",
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(".agents-task-time,.agents-task-time-exact",
		gwccss.TextColor(gwccss.Var("muted")), gwccss.FontSize(gwccss.Rem(0.875)), gwccss.JustifySelf.Start,
	)
	declareGlobal(".agents-task-times", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(2)), gwccss.JustifyItems.End, gwccss.Raw("grid-column", "3"))
	declareGlobal(".agents-task-time-exact", gwccss.Raw("display", "none"))
	declareGlobal(".agents-task-timeline", gwccss.Display.Flex, gwccss.Gap(gwccss.Px(12)), gwccss.Raw("flex-wrap", "wrap"), gwccss.Margin(gwccss.Zero))
	declareGlobal(".agents-task-documents,.agents-document-omissions",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(6)), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".agents-task-documents h2,.agents-task-documents h3", gwccss.Margin(gwccss.Zero))
	declareGlobal(".agents-task-documents.is-compact", gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(6)), gwccss.Raw("flex-wrap", "wrap"))
	declareGlobal(".agents-task-documents.is-compact ul", gwccss.Display.Flex, gwccss.Gap(gwccss.Px(8)), gwccss.Raw("flex-wrap", "wrap"))
	declareGlobal(".agents-document-reference", gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(4)), gwccss.MinWidth(gwccss.Zero))
	declareGlobal(".agents-task-next-actions",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(10)),
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal(".agents-page .button,.agents-task-link,.agents-task-filter,.agents-manage-link,.agents-back-link",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Justify.Center,
		gwccss.MinHeight(gwccss.Px(44)), gwccss.MinWidth(gwccss.Px(44)),
		gwccss.Raw("max-inline-size", "100%"), gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".agents-task-link",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(6)), gwccss.W(gwccss.Percent(100)),
		gwccss.Raw("justify-self", "stretch"), gwccss.Raw("justify-content", "stretch"),
		gwccss.Raw("padding-block", "12px"), gwccss.Raw("padding-inline", "12px"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")),
		gwccss.TextColor(gwccss.Var("ink")), gwccss.Raw("text-decoration", "none"), gwccss.Raw("text-align", "start"),
	)
	declareGlobal(".agents-task-link:hover",
		gwccss.Raw("background", "var(--surface-subtle,var(--canvas))"),
	)
	declareGlobal(".agents-page :is(.button,.agents-task-link,.agents-task-filter,.agents-manage-link,.agents-back-link,.agents-agent-choice,.agents-composer textarea):focus-visible",
		gwccss.Raw("outline", "2px solid var(--hcm-color-focus,var(--accent))"),
		gwccss.OutlineOffset(gwccss.Px(3)),
		gwccss.Raw("box-shadow", "0 0 0 3px var(--surface)"),
	)
	declareGlobal(".agents-page .agents-page-nav-item:focus-visible",
		gwccss.Raw("outline", "2px solid var(--hcm-color-focus,var(--accent))"), gwccss.OutlineOffset(gwccss.Px(3)),
		gwccss.Raw("box-shadow", "0 0 0 3px var(--surface)"),
	)
	declareGlobal(".agents-page :is(.agents-request-documents input,.agents-document-chip button,.agents-task-time):focus-visible",
		gwccss.Raw("outline", "2px solid var(--hcm-color-focus,var(--accent))"), gwccss.OutlineOffset(gwccss.Px(3)),
	)
	declareGlobal(".agents-tasks-heading",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Justify.Between, gwccss.Gap(gwccss.Px(12)), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".agents-tasks-heading h2,.agents-chat-history-note", gwccss.Margin(gwccss.Zero))
	declareGlobal(".agents-tasks-heading > div", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(4)))
	declareGlobal(".agents-task-row", gwccss.Display.Grid, gwccss.W(gwccss.Percent(100)), gwccss.MinWidth(gwccss.Zero), gwccss.Raw("justify-self", "stretch"), gwccss.Raw("border-block-end", "1px solid var(--control-border,var(--line))"))
	declareGlobal(".agents-task-row-heading",
		gwccss.Display.Grid, gwccss.Raw("grid-template-columns", "auto minmax(0,1fr) auto auto"),
		gwccss.Items.Start, gwccss.Gap(gwccss.Px(12)), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".agents-task-state-icon",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Justify.Center,
		gwccss.Raw("inline-size", "1.5rem"), gwccss.Raw("block-size", "1.5rem"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")), gwccss.FontWeight.Semibold,
	)
	declareGlobal(".agents-task-state-icon[data-tone=active]", gwccss.TextColor(gwccss.Var("info")), gwccss.Raw("border", "1px solid var(--info)"))
	declareGlobal(".agents-task-state-icon[data-tone=completed]", gwccss.TextColor(gwccss.Var("success")), gwccss.Raw("border", "1px solid var(--success)"))
	declareGlobal(".agents-task-state-icon[data-tone=failed]", gwccss.TextColor(gwccss.Var("danger")), gwccss.Raw("border", "1px solid var(--danger)"))
	declareGlobal(".agents-task-chevron", gwccss.FontSize(gwccss.Rem(1.25)), gwccss.Raw("line-height", "1"), gwccss.Raw("inline-size", "24px"), gwccss.Raw("text-align", "center"), gwccss.Raw("grid-column", "4"))
	declareGlobal(".agents-task-link[aria-expanded=true] .agents-task-chevron", gwccss.Raw("transform", "rotate(90deg)"))
	declareGlobal(".agents-task-request",
		gwccss.MinWidth(gwccss.Zero), gwccss.Raw("display", "-webkit-box"), gwccss.Raw("-webkit-box-orient", "vertical"),
		gwccss.Raw("-webkit-line-clamp", "2"), gwccss.Raw("overflow", "hidden"),
		gwccss.Raw("overflow-wrap", "normal"), gwccss.Raw("word-break", "normal"),
	)
	declareGlobal(".agents-task-preview",
		gwccss.Margin(gwccss.Zero), gwccss.TextColor(gwccss.Var("muted")), gwccss.Raw("white-space", "nowrap"),
		gwccss.Raw("overflow", "hidden"), gwccss.Raw("text-overflow", "ellipsis"),
	)
	declareGlobal(".agents-task-failure-reason", gwccss.Margin(gwccss.Zero), gwccss.TextColor(gwccss.Var("muted")), gwccss.Raw("white-space", "normal"), gwccss.Raw("overflow-wrap", "anywhere"))
	declareGlobal(".agents-task-row[data-task-category=failed] .agents-task-preview", gwccss.Raw("white-space", "normal"), gwccss.Raw("overflow", "visible"), gwccss.Raw("text-overflow", "clip"))
	declareGlobal(".agents-task-filters",
		gwccss.Display.Grid, gwccss.Raw("grid-template-columns", "repeat(3,minmax(0,1fr))"), gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".agents-task-filter",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Justify.Center, gwccss.Gap(gwccss.Px(6)), gwccss.Raw("white-space", "nowrap"),
		gwccss.Raw("padding", "6px 12px"), gwccss.Raw("border", "1px solid var(--control-border,var(--line))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")), gwccss.Bg(gwccss.Var("surface")), gwccss.TextColor(gwccss.Var("ink")),
	)
	declareGlobal(".agents-task-filter[aria-selected=\"true\"]",
		gwccss.Raw("border-color", "var(--accent)"),
		gwccss.Raw("background", "var(--surface-subtle,var(--canvas))"), gwccss.Raw("color", "var(--accent)"), gwccss.Raw("box-shadow", "none"),
	)
	declareGlobal(".agents-tasks-announcement", gwccss.Margin(gwccss.Zero))
	declareGlobal(".agents-tasks-announcement:empty", gwccss.Raw("display", "none"))
	declareGlobal(".agents-task-link.is-new", gwccss.Raw("background", "var(--surface-subtle,var(--canvas))"))
	declareGlobal(".agents-task-row > .agents-task-documents,.agents-task-row-meta", gwccss.Raw("padding-inline", "12px"), gwccss.Raw("padding-block-end", "10px"))
	declareGlobal(".agents-task-row-meta", gwccss.Display.Flex, gwccss.Items.Center, gwccss.Justify.Between, gwccss.Gap(gwccss.Px(8)), gwccss.Raw("flex-wrap", "wrap"))
	declareGlobal(".agents-task-row-actions", gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(8)), gwccss.Raw("flex-wrap", "wrap"))
	declareGlobal(".agents-task-inline-detail", gwccss.W(gwccss.Percent(100)), gwccss.MinWidth(gwccss.Zero), gwccss.Raw("padding-block", "12px"))
	declareGlobal(".agents-task-inline-detail .agents-task-view", gwccss.Raw("max-inline-size", "none"))
	declareGlobal(".agents-task-status",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Raw("padding", "3px 8px"), gwccss.Raw("white-space", "nowrap"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")), gwccss.FontSize(gwccss.Rem(0.8125)), gwccss.FontWeight.Semibold,
	)
	declareGlobal(".agents-task-status[data-tone=\"active\"]", gwccss.TextColor(gwccss.Var("info")), gwccss.Raw("border", "1px solid var(--info)"))
	declareGlobal(".agents-task-status[data-tone=\"completed\"]", gwccss.TextColor(gwccss.Var("success")), gwccss.Raw("border", "1px solid var(--success)"))
	declareGlobal(".agents-task-status[data-tone=\"failed\"]", gwccss.TextColor(gwccss.Var("danger")), gwccss.Raw("border", "1px solid var(--danger)"))
	declareGlobal(".agents-manage-link,.agents-back-link",
		gwccss.JustifySelf.Start, gwccss.TextColor(gwccss.Var("accent")),
	)
	declareGlobal(".agents-task-heading",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(10)), gwccss.MinWidth(gwccss.Zero), gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal(".agents-task-heading h1",
		gwccss.Margin(gwccss.Zero), gwccss.FontSize(gwccss.Rem(1.375)), gwccss.Raw("line-height", "1.25"),
	)
	declareGlobal(".agents-task-heading .agents-task-time",
		gwccss.Raw("margin-inline-start", "auto"),
	)
	declareGlobal(".agents-task-answer",
		gwccss.Raw("padding", "var(--hcm-space-3)"), gwccss.Raw("background", "var(--surface-subtle,var(--canvas))"),
		gwccss.Raw("border", "1px solid var(--control-border,var(--line))"), gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-surface,var(--radius))")),
	)
	declareGlobal(".agents-task-answer p", gwccss.Raw("max-inline-size", "68ch"), gwccss.Raw("font-size", "1.0625rem"), gwccss.Raw("line-height", "1.65"))
	declareGlobal(".agents-task-goal",
		gwccss.Raw("display", "-webkit-box"), gwccss.Raw("-webkit-box-orient", "vertical"), gwccss.Raw("-webkit-line-clamp", "3"), gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(".agents-plan-step",
		gwccss.Raw("grid-template-columns", "auto minmax(0,1fr) auto"), gwccss.Items.Center,
	)
	declareGlobal(".agents-plan-step .muted", gwccss.Raw("grid-column", "2 / -1"), gwccss.Margin(gwccss.Zero))
	declareGlobal(".agents-plan-step .agents-step-duration", gwccss.Raw("grid-column", "2 / -1"), gwccss.FontSize(gwccss.Rem(0.8125)))
	declareGlobal(".agents-step-number",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Justify.Center, gwccss.Raw("inline-size", "1.75rem"), gwccss.Raw("block-size", "1.75rem"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control,var(--radius))")), gwccss.Raw("background", "var(--surface-subtle,var(--canvas))"),
	)
	declareGlobal(".agents-retry-unavailable", gwccss.Raw("flex-basis", "100%"), gwccss.Margin(gwccss.Zero))
	declareGlobal(".agents-task-view > :is(.agents-task-answer,.agents-task-request-detail,.agents-task-plan,.agents-task-live,.agents-task-detail)",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)),
	)
	declareGlobal(".agents-empty-thread,.agents-page-unavailable,.agents-page-disabled",
		gwccss.MinWidth(gwccss.Zero), gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(".agents-page-unavailable,.agents-page-disabled",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(12)),
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
			gwccss.Gap(gwccss.Px(14)), gwccss.Raw("padding-block", "var(--hcm-space-2)"),
		),
	)
	declareGlobal(".agents-task-request",
		mediaRule(gwccss.MinW(600), gwccss.Raw("min-inline-size", "min(240px,100%)")),
	)
	declareGlobal(".agents-task-request",
		mediaRule(gwccss.MaxW(400), gwccss.Raw("-webkit-line-clamp", "3")),
	)
	declareGlobal(".agents-task-time-exact",
		mediaRule(gwccss.MaxW(400), gwccss.Raw("display", "inline")),
	)
	declareGlobal(".agents-task-row-heading",
		mediaRule(gwccss.MaxW(400), gwccss.Raw("grid-template-columns", "auto minmax(0,1fr) auto")),
	)
	declareGlobal(".agents-task-chevron", mediaRule(gwccss.MaxW(400), gwccss.Raw("grid-column", "3")))
	declareGlobal(".agents-task-times",
		mediaRule(gwccss.MaxW(400), gwccss.Raw("grid-column", "2 / -1"), gwccss.JustifyItems.Start),
	)
	declareGlobal(".agents-page-nav",
		mediaRule(gwccss.MaxW(480), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal(".agents-page-nav-item",
		mediaRule(gwccss.MaxW(480), gwccss.Raw("flex", "1 1 0"), gwccss.Raw("padding-inline", "6px")),
	)
	declareGlobal(".agents-task-row[data-selected=true]", gwccss.Raw("background", "var(--surface-subtle,var(--canvas))"), gwccss.Raw("align-items", "start"))
	declareGlobal(".agents-task-listing", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(12)), gwccss.MinWidth(gwccss.Zero))
	declareGlobal(".agents-task-detail-pane", gwccss.MinWidth(gwccss.Zero))
	declareGlobal(".agents-task-answer-heading", gwccss.Display.Flex, gwccss.Items.Center, gwccss.Justify.Between, gwccss.Gap(gwccss.Px(12)), gwccss.Raw("flex-wrap", "wrap"))
	declareGlobal(".agents-task-answer-heading h2,.agents-task-meta", gwccss.Margin(gwccss.Zero))
	declareGlobal(".agents-task-detail-pane .agents-task-view", gwccss.Raw("max-inline-size", "none"))
	declareGlobal(".agents-tasks[data-has-task-detail=true]",
		mediaRule(gwccss.MinW(1024), gwccss.Raw("grid-template-columns", "420px minmax(0,1fr)"), gwccss.Raw("column-gap", "20px"), gwccss.Items.Start),
		mediaRule(gwccss.MaxW(1023), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)))),
	)
	declareGlobal(".agents-tasks[data-has-task-detail=true] .agents-task-listing",
		mediaRule(gwccss.MinW(1024), gwccss.Raw("grid-column", "1")),
		mediaRule(gwccss.MaxW(1023), gwccss.Raw("display", "none")),
	)
	declareGlobal(".agents-tasks[data-has-task-detail=true] .agents-task-detail-pane", mediaRule(gwccss.MinW(1024), gwccss.Raw("grid-column", "2"), gwccss.Raw("grid-row", "1")))
	declareGlobal(".agents-composer,.agents-task-view",
		mediaRule(gwccss.MaxW(480), gwccss.Raw("padding-block", "14px"), gwccss.Raw("padding-inline", "14px")),
	)
	declareGlobal(".agents-composer-actions .button,.agents-task-actions .button",
		mediaRule(gwccss.MaxW(390), gwccss.Raw("flex", "1 1 100%"), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal(".agents-task-filter",
		mediaRule(gwccss.MaxW(390), gwccss.Raw("flex-direction", "column"), gwccss.Gap(gwccss.Px(1)), gwccss.Raw("padding-inline", "4px"), gwccss.FontSize(gwccss.Rem(0.875))),
	)
	declareGlobal(".agents-task-next-actions .button",
		mediaRule(gwccss.MaxW(390), gwccss.Raw("flex", "1 1 100%"), gwccss.W(gwccss.Percent(100))),
	)
	declareGlobal(".agent-operations-region-header",
		gwccss.Display.Flex, gwccss.Items.Start, gwccss.Justify.Between, gwccss.Gap(gwccss.Px(12)), gwccss.MinWidth(gwccss.Zero), gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal(".agent-operations-page", gwccss.Raw("box-sizing", "border-box"))
	declareGlobal(".agent-operations-page [hidden]", gwccss.Raw("display", "none !important"))
	declareGlobal(".agent-operations-page > :is(.agent-operations-hero,.agent-operations-tabs,.agent-operations-region,#agent-rollout-portable,.persona-admin-unavailable)",
		gwccss.W(gwccss.Percent(100)), gwccss.Raw("max-inline-size", "100%"), gwccss.Raw("box-sizing", "border-box"),
	)
	declareGlobal(".agent-operations-page .agent-operations-hero", gwccss.Padding(gwccss.Zero))
	declareGlobal(".agent-operations-page-nav",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(4)), gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal(".agent-operations-page-nav-link",
		gwccss.Display.InlineFlex, gwccss.Items.Center, gwccss.Justify.Center, gwccss.MinHeight(gwccss.Px(40)),
		gwccss.Raw("padding-inline", "12px"), gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control)")),
		gwccss.Raw("border", "1px solid var(--hcm-color-border)"), gwccss.TextColor(gwccss.Var("ink")), gwccss.Raw("text-decoration", "none"),
	)
	declareGlobal(".agent-operations-page-nav-link.selected",
		gwccss.Raw("border-color", "var(--hcm-color-brand-primary,var(--accent))"), gwccss.Raw("background", "var(--hcm-color-canvas)"), gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(".agent-operations-tabs",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(4)), gwccss.Raw("flex-wrap", "nowrap"),
		gwccss.Raw("border-block-end", "1px solid var(--hcm-color-border)"),
		gwccss.Raw("overflow-x", "auto"), gwccss.Raw("scrollbar-width", "thin"),
	)
	declareGlobal(".agent-operations-tabs a",
		gwccss.Raw("min-block-size", "44px"), gwccss.Display.InlineFlex, gwccss.Items.Center,
		gwccss.Raw("padding-inline", "12px"), gwccss.Raw("text-decoration", "none"), gwccss.TextColor(gwccss.Var("ink")),
		gwccss.Raw("border-block-end", "2px solid transparent"), gwccss.Raw("white-space", "nowrap"), gwccss.Raw("flex", "0 0 auto"),
	)
	declareGlobal(".agent-operations-tab-label-short", gwccss.Raw("display", "none"))
	declareGlobal(".agent-operations-tabs a[aria-selected=\"true\"]",
		gwccss.Raw("border-block-end-color", "var(--hcm-color-brand-primary,var(--accent))"), gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(".agent-operations-page .persona-admin-unavailable", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)))
	declareGlobal(".agent-operations-denied-actions", gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(8)), gwccss.Raw("flex-wrap", "wrap"))
	declareGlobal(".agent-operations-denied-actions .button", gwccss.Raw("min-block-size", "44px"), gwccss.Raw("margin", "0"))
	declareGlobal(".agent-operations-region", gwccss.Raw("overflow", "hidden"))
	declareGlobal(".agent-operations-region :is(h2,h3,h4,p)", gwccss.Margin(gwccss.Zero))
	declareGlobal(".agent-operations-region-header > div", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)))
	declareGlobal(".agent-operations-region-content", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(12)))
	declareGlobal(".agent-operations-alert",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Justify.Between, gwccss.Gap(gwccss.Px(12)), gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Raw("padding", "12px"), gwccss.Raw("border", "1px solid var(--hcm-color-danger,var(--hcm-color-border))"),
		gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control)")), gwccss.Raw("background", "var(--hcm-color-surface)"),
	)
	declareGlobal(".agent-operations-alert > div", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(4)))
	declareGlobal(".agent-operations-run", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)))
	declareGlobal(".agent-operations-failure", gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)), gwccss.Raw("padding-block-start", "4px"))
	declareGlobal(".agent-operations-recent-title", gwccss.Raw("margin-block-start", "8px"))
	declareGlobal(".agent-operations-region-header > :first-child",
		gwccss.Raw("flex", "1 1 28rem"), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".agent-operations-region-header .button",
		gwccss.W(gwccss.RawLength("auto")), gwccss.Raw("inline-size", "fit-content"), gwccss.Raw("max-inline-size", "100%"), gwccss.JustifySelf.Start,
	)
	declareGlobal(".agent-portable-step",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(10)), gwccss.MinWidth(gwccss.Zero), gwccss.Raw("max-inline-size", "100%"),
	)
	declareGlobal(".agent-portable-step .button,.agent-portable-open-form .button",
		gwccss.W(gwccss.RawLength("auto")), gwccss.Raw("inline-size", "fit-content"), gwccss.Raw("max-inline-size", "100%"), gwccss.JustifySelf.Start,
	)
	declareGlobal(".agent-portable-export-actions", gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(8)), gwccss.Raw("flex-wrap", "wrap"), gwccss.MinWidth(gwccss.Zero))
	declareGlobal(".agent-portable-file-control", gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(8)), gwccss.Raw("flex-wrap", "wrap"), gwccss.MinWidth(gwccss.Zero))
	declareGlobal(".agent-portable-file-input", gwccss.Raw("position", "absolute"), gwccss.Raw("inline-size", "1px"), gwccss.Raw("block-size", "1px"), gwccss.Raw("overflow", "hidden"), gwccss.Raw("clip-path", "inset(50%)"))
	declareGlobal(".agent-portable-draft-list",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(8)), gwccss.Margin(gwccss.Zero), gwccss.Padding(gwccss.Zero), gwccss.Raw("list-style", "none"), gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(".agent-portable-draft-row",
		gwccss.Display.Grid, gwccss.Raw("grid-template-columns", "minmax(0,1fr) auto"), gwccss.Items.Center, gwccss.Gap(gwccss.Px(12)), gwccss.MinWidth(gwccss.Zero), gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal("#agent-rollout-portable :is(input,select,textarea)",
		gwccss.Raw("background", "var(--hcm-color-surface,var(--surface))"),
	)
	declareGlobal("#agent-rollout-portable .agent-rollout-form", gwccss.Raw("max-inline-size", "720px"))
	declareGlobal("#agent-rollout-portable .agent-rollout-form > .persona-admin-editor-field:not(.agent-rollout-number)", gwccss.Raw("max-inline-size", "480px"))
	declareGlobal("#agent-rollout-portable .agent-rollout-number", gwccss.Raw("max-inline-size", "320px"))
	declareGlobal("#agent-rollout-portable .agent-rollout-number input", gwccss.Raw("inline-size", "96px"))
	declareGlobal("#agent-rollout-portable #agent-rollout-version", gwccss.Raw("inline-size", "min(100%, 30rem)"))
	declareGlobal("#agent-rollout-portable .agent-rollout-group-title", gwccss.Raw("margin-block", "4px 0"))
	declareGlobal("#agent-rollout-portable .agent-rollout-choice-group",
		gwccss.Display.Grid, gwccss.Gap(gwccss.Px(2)), gwccss.Raw("padding", "4px 8px"),
		gwccss.Raw("border", "1px solid var(--hcm-color-border)"), gwccss.Rounded(gwccss.RawLength("var(--hcm-radius-control)")),
	)
	declareGlobal("#agent-rollout-portable .agent-rollout-choice",
		gwccss.Display.Flex, gwccss.Items.Center, gwccss.Gap(gwccss.Px(10)), gwccss.Raw("min-block-size", "44px"), gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal("#agent-rollout-portable .agent-rollout-choice input",
		gwccss.Raw("appearance", "none"), gwccss.Raw("inline-size", "18px"), gwccss.Raw("block-size", "18px"), gwccss.MinHeight(gwccss.Zero),
		gwccss.Raw("padding", "0"), gwccss.Raw("flex", "0 0 18px"), gwccss.Raw("background", "transparent"),
		gwccss.Raw("border", "1.5px solid var(--hcm-color-border)"), gwccss.Rounded(gwccss.Px(4)),
	)
	declareGlobal("#agent-rollout-portable .agent-rollout-choice input:checked",
		gwccss.Raw("background", "var(--hcm-color-brand-primary,var(--accent))"), gwccss.Raw("box-shadow", "inset 0 0 0 3px var(--hcm-color-surface,var(--surface))"),
	)
	declareGlobal("#agent-rollout-portable .agent-rollout-choice span", gwccss.MinWidth(gwccss.Zero), gwccss.Raw("overflow-wrap", "anywhere"))
	declareGlobal(".agent-operations-region-header,.agent-portable-step,.agent-portable-draft-row",
		mediaRule(gwccss.MaxW(800), gwccss.Raw("grid-template-columns", "minmax(0,1fr)"), gwccss.Raw("align-items", "start")),
	)
	declareGlobal(".agent-operations-region-header",
		mediaRule(gwccss.MaxW(800), gwccss.Display.Grid),
	)
	declareGlobal(".agent-portable-draft-row .agent-portable-open-form",
		mediaRule(gwccss.MaxW(800), gwccss.JustifySelf.Start),
	)
	declareGlobal(".agent-operations-tabs",
		mediaRule(gwccss.MaxW(390), gwccss.Raw("overflow-x", "visible")),
	)
	declareGlobal(".agent-operations-tabs a",
		mediaRule(gwccss.MaxW(390), gwccss.Raw("padding-inline", "10px")),
	)
	declareGlobal(".agent-operations-tabs a[data-agent-operations-tab=\"move\"] .agent-operations-tab-label-long",
		mediaRule(gwccss.MaxW(390), gwccss.Raw("display", "none")),
	)
	declareGlobal(".agent-operations-tabs a[data-agent-operations-tab=\"move\"] .agent-operations-tab-label-short",
		mediaRule(gwccss.MaxW(390), gwccss.Raw("display", "inline")),
	)
}
