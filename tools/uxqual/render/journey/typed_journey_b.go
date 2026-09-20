package journey

import (
	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// declareJourneyForms emits the people-table tail, empty states, callouts,
// forms, buttons, hero, stepper, columns, facts, and tables rules, in
// original order.
func declareJourneyForms() {
	declareGlobal(`.jn-people-journeys`,
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-people-action`,
		gwccss.Raw("text-align", "end"),
	)
	declareGlobal(`.jn-source`,
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-people tbody tr[data-tone="info"] .jn-people-idcell`,
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("jn-info"))),
	)
	declareGlobal(`.jn-people tbody tr[data-tone="success"] .jn-people-idcell`,
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("jn-success"))),
	)
	declareGlobal(`.jn-people tbody tr[data-tone="warning"] .jn-people-idcell`,
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("jn-warning"))),
	)
	declareGlobal(`.jn-people tbody tr[data-tone="danger"] .jn-people-idcell`,
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("jn-danger"))),
	)
	declareGlobal(`.jn-people tbody tr[data-selected="true"]`,
		gwccss.Bg(gwccss.Var("jn-accent-soft")),
	)
	declareGlobal(`.jn-people tbody tr[data-selected="true"] .jn-people-idcell`,
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("jn-accent"))),
	)
	// The id cell's tone bar marks the row's leading edge. In a right-to-left
	// table that edge is the cell's right side; the bar was drawn on its left,
	// between the id and the next column.
	declareGlobal(`.jn-people tbody tr[data-tone="info"] .jn-people-idcell:dir(rtl)`,
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(-3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("jn-info"))),
	)
	declareGlobal(`.jn-people tbody tr[data-tone="success"] .jn-people-idcell:dir(rtl)`,
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(-3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("jn-success"))),
	)
	declareGlobal(`.jn-people tbody tr[data-tone="warning"] .jn-people-idcell:dir(rtl)`,
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(-3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("jn-warning"))),
	)
	declareGlobal(`.jn-people tbody tr[data-tone="danger"] .jn-people-idcell:dir(rtl)`,
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(-3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("jn-danger"))),
	)
	declareGlobal(`.jn-people tbody tr[data-selected="true"] .jn-people-idcell:dir(rtl)`,
		gwccss.Shadow(gwccss.ShadowInset(gwccss.Px(-3), gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.Var("jn-accent"))),
	)
	declareGlobal(`.jn-people tbody tr[data-selected="true"] .jn-people-name`,
		gwccss.TextColor(gwccss.Var("jn-accent-strong")),
	)
	declareGlobal(`.jn-empty`,
		gwccss.Raw("text-align", "center"),
		gwccss.PaddingY(gwccss.VarLength("jn-s4")), gwccss.PaddingX(gwccss.VarLength("jn-s2")),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-empty.jn-empty-inset`,
		gwccss.PaddingY(gwccss.VarLength("jn-s3")), gwccss.PaddingX(gwccss.VarLength("jn-s2")),
		gwccss.Raw("border", "1px dashed var(--jn-control-border)"),
		gwccss.Rounded(gwccss.VarLength("jn-r2")),
		gwccss.Bg(gwccss.Var("jn-surface-sunk")),
	)
	declareGlobal(`.jn-empty-mark`,
		gwccss.TextColor(gwccss.Var("jn-control-border")),
		gwccss.Raw("margin", "0 auto var(--jn-s1)"),
	)
	declareGlobal(`.jn-empty-title`,
		gwccss.TextColor(gwccss.Var("jn-ink")),
		gwccss.Raw("font-weight", "600"),
		gwccss.FontSize(gwccss.Rem(1)),
	)
	// The explanation sits a step below its title, in a centred column narrow
	// enough to read as one thought, and wraps without stranding a last word
	// ("...Choose an employee to start / one.").
	declareGlobal(`.jn-empty-title+p`,
		gwccss.Raw("margin", ".375rem auto 0"),
		gwccss.MaxWidth(gwccss.RawLength("48ch")),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("line-height", "1.5"),
		gwccss.Raw("text-wrap", "pretty"),
	)
	declareGlobal(`.jn-callout`,
		gwccss.Display.Flex,
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-info")),
		gwccss.Bg(gwccss.Var("jn-info-soft")),
		gwccss.Rounded(gwccss.VarLength("jn-r2")),
		gwccss.PaddingY(gwccss.Rem(.875)), gwccss.PaddingX(gwccss.Rem(1)),
	)
	declareGlobal(`.jn-callout-icon`,
		gwccss.Raw("flex", "none"),
		gwccss.TextColor(gwccss.Var("jn-info")),
		gwccss.Raw("margin-top", ".125rem"),
	)
	declareGlobal(`.jn-callout-title`,
		gwccss.Raw("font-weight", "600"),
		gwccss.TextColor(gwccss.Var("jn-info")),
	)
	declareGlobal(`.jn-callout-detail`,
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("margin-top", ".125rem"),
	)
	declareGlobal(`.jn-fieldgrid`,
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.VarLength("jn-s2")),
		gwccss.GridCols(gwccss.Fr(1)),
	)
	declareGlobal(`.jn-fieldgrid`,
		mediaRule(gwccss.RawMedia("(min-width:44rem)"), gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1))),
	)
	declareGlobal(`.jn-field[data-span="full"]`,
		mediaRule(gwccss.RawMedia("(min-width:44rem)"), gwccss.GridColumn(gwccss.GridRange(gwccss.GridLineAt(1), gwccss.GridLineAt(-1)))),
	)
	declareGlobal(`.jn-field`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.3125)),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.jn-label`,
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Tracking(gwccss.Ems(-.005)),
	)
	declareGlobal(`.jn-req`,
		gwccss.TextColor(gwccss.Var("jn-danger")),
		gwccss.Raw("margin-inline-start", ".125rem"),
	)
	declareGlobal(`.jn-inputwrap`,
		gwccss.Display.Flex,
		gwccss.Items.Stretch,
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-control-border")),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.Bg(gwccss.Var("jn-surface")),
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("box-shadow"), gwccss.Prop("border-color")), gwccss.S(.15), gwccss.Easing("var(--jn-ease)")),
	)
	declareGlobal(`.jn-inputwrap:hover`,
		gwccss.BorderColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-inputwrap:focus-within`,
		gwccss.BorderColor(gwccss.Var("jn-accent")),
		gwccss.Raw("box-shadow", "var(--jn-ring)"),
	)
	declareGlobal(`.jn-field[data-invalid="true"] .jn-inputwrap`,
		gwccss.BorderColor(gwccss.Var("jn-danger")),
		gwccss.BorderWidth(gwccss.Px(2)),
	)
	declareGlobal(`.jn-adorn`,
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.PaddingY(gwccss.Zero), gwccss.PaddingX(gwccss.Rem(.625)),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Raw("font-weight", "600"),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.Bg(gwccss.Var("jn-surface-sunk")),
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(`.jn-adorn[data-side="prefix"]`,
		gwccss.Raw("border-inline-end", "1px solid var(--jn-hairline)"),
		gwccss.Rounded(gwccss.RawLength("var(--jn-r1) 0 0 var(--jn-r1)")),
	)
	declareGlobal(`.jn-adorn[data-side="suffix"]`,
		gwccss.Raw("border-inline-start", "1px solid var(--jn-hairline)"),
		gwccss.Rounded(gwccss.RawLength("0 var(--jn-r1) var(--jn-r1) 0")),
	)
	declareGlobal(`.jn-input`,
		gwccss.Raw("font", "inherit"),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.TextColor(gwccss.Var("jn-ink")),
		gwccss.W(gwccss.Percent(100)),
		gwccss.MinWidth(gwccss.Zero),
		gwccss.PaddingY(gwccss.Rem(.5)), gwccss.PaddingX(gwccss.Rem(.625)),
		gwccss.Raw("border", "0"),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.Bg(gwccss.Transparent),
	)
	declareGlobal(`.jn-input:focus`,
		gwccss.Raw("outline", "none"),
	)
	declareGlobal(`textarea.jn-input`,
		gwccss.Raw("resize", "vertical"),
		gwccss.MinHeight(gwccss.Rem(5)),
		gwccss.LineHeight(gwccss.Num(1.5)),
	)
	declareGlobal(`select.jn-input`,
		gwccss.Raw("cursor", "pointer"),
	)
	declareGlobal(`.jn-help`,
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-error`,
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.TextColor(gwccss.Var("jn-danger")),
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.3125)),
	)
	// On a phone the form's action spans the width, where a thumb reaches
	// it, rather than sitting as a small button at the left edge.
	declareGlobal(`.jn-formfoot>.jn-btn,.jn-formfoot>.jn-confirm,.jn-formfoot>.jn-confirm>summary.jn-btn`,
		gwccss.W(gwccss.Percent(100)),
		mediaRule(gwccss.RawMedia("(min-width:30rem)"), gwccss.W(gwccss.RawLength("auto"))),
	)
	declareGlobal(`.jn-formfoot`,
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.VarLength("jn-s2")),
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Raw("margin-top", "var(--jn-s3)"),
		gwccss.Raw("padding-top", "var(--jn-s2)"),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("jn-hairline")),
	)
	declareGlobal(`.jn-btn`,
		gwccss.Raw("font", "inherit"),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Tracking(gwccss.Ems(-.005)),
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.Justify.Center,
		gwccss.Gap(gwccss.Rem(.375)),
		gwccss.PaddingY(gwccss.Rem(.5625)), gwccss.PaddingX(gwccss.Rem(1.125)),
		gwccss.Rounded(gwccss.VarLength("jn-r1")),
		gwccss.Border(gwccss.Px(1), gwccss.Transparent),
		gwccss.Raw("cursor", "pointer"),
		gwccss.TextColor(gwccss.Var("jn-accent-ink")),
		gwccss.Raw("text-decoration", "none"),
		gwccss.Raw("white-space", "nowrap"),
		gwccss.Raw("background", "linear-gradient(180deg,var(--jn-accent),var(--jn-accent-strong))"),
		gwccss.Raw("box-shadow", "none"),
		gwccss.Raw("transition", "transform .12s var(--jn-ease),box-shadow .15s var(--jn-ease),filter .15s var(--jn-ease)"),
	)
	declareGlobal(`.jn-btn:hover`,
		gwccss.Raw("filter", "brightness(1.08)"),
	)
	declareGlobal(`.jn-btn:active`,
		gwccss.Transform(gwccss.TranslateY(gwccss.Px(1))),
	)
	declareGlobal(`.jn-btn[data-variant="secondary"]`,
		gwccss.Raw("background", "var(--jn-surface)"),
		gwccss.TextColor(gwccss.Var("jn-accent")),
		gwccss.BorderColor(gwccss.Var("jn-control-border")),
		gwccss.Raw("box-shadow", "none"),
	)
	declareGlobal(`.jn-btn[data-variant="secondary"]:hover`,
		gwccss.Raw("background", "var(--jn-surface-sunk)"),
		gwccss.Raw("filter", "none"),
	)
	declareGlobal(`.jn-btn[data-variant="danger"]`,
		gwccss.TextColor(gwccss.Var("jn-danger-ink")),
		gwccss.Raw("background", "linear-gradient(180deg,var(--jn-danger),var(--jn-danger))"),
	)
	declareGlobal(`a.jn-btn:hover`,
		gwccss.TextColor(gwccss.Var("jn-accent-ink")),
	)
	declareGlobal(`.jn-btn[data-size="sm"]`,
		gwccss.PaddingY(gwccss.Rem(.3125)), gwccss.PaddingX(gwccss.Rem(.6875)),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Tracking(gwccss.Zero),
	)
	declareGlobal(`.jn-btn[disabled]`,
		gwccss.Raw("cursor", "not-allowed"),
		gwccss.Bg(gwccss.Var("jn-surface-muted")),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.BorderColor(gwccss.Var("jn-hairline")),
		gwccss.Raw("box-shadow", "none"),
		gwccss.Raw("filter", "none"),
		gwccss.Raw("transform", "none"),
	)
	declareGlobal(`.jn-hero`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.Position.Relative,
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(`.jn-hero-top`,
		gwccss.Display.Flex,
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.VarLength("jn-s2")),
		gwccss.Raw("flex-wrap", "wrap"),
	)
	declareGlobal(`.jn-hero-pay`,
		gwccss.FontSize(gwccss.Rem(1.5)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Tracking(gwccss.Ems(-.024)),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(`.jn-hero-ids`,
		gwccss.Raw("padding-top", ".75rem"),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-stepper`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Zero),
	)
	declareGlobal(`.jn-step`,
		gwccss.Position.Relative,
		gwccss.Display.Flex,
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.Raw("padding-bottom", "var(--jn-s2)"),
	)
	declareGlobal(`.jn-step:last-child`,
		gwccss.Raw("padding-bottom", "0"),
	)
	declareGlobal(`.jn-step::before`,
		gwccss.Raw("content", "\"\""),
		gwccss.Position.Absolute,
		gwccss.Raw("inset-inline-start", ".8125rem"),
		gwccss.Top(gwccss.Rem(1.875)),
		gwccss.Bottom(gwccss.Rem(.25)),
		gwccss.W(gwccss.Px(2)),
		gwccss.Bg(gwccss.Var("jn-hairline")),
	)
	declareGlobal(`.jn-step::after`,
		gwccss.Raw("content", "\"\""),
		gwccss.Position.Absolute,
		gwccss.Raw("inset-inline-start", ".8125rem"),
		gwccss.Top(gwccss.Rem(1.875)),
		gwccss.W(gwccss.Px(2)),
		gwccss.H(gwccss.Zero),
		gwccss.Bg(gwccss.Var("jn-accent")),
		gwccss.Raw("transform-origin", "top"),
	)
	declareGlobal(`.jn-step:last-child::before,.jn-step:last-child::after`,
		gwccss.Display.None,
	)
	declareGlobal(`.jn-step[data-state="done"]::after`,
		gwccss.H(gwccss.RawLength("calc(100% - 2rem)")),
		gwccss.Keyframes("jn-grow-y", jnGrowYFrames...),
		gwccss.Animation(gwccss.RawDuration(".5s"), gwccss.Easing("var(--jn-ease)")),
		gwccss.Raw("animation-fill-mode", "both"),
	)
	declareGlobal(`.jn-step[data-state="done"]:nth-child(2)::after`,
		gwccss.Raw("animation-delay", ".12s"),
	)
	declareGlobal(`.jn-step[data-state="done"]:nth-child(3)::after`,
		gwccss.Raw("animation-delay", ".24s"),
	)
	declareGlobal(`.jn-step[data-state="done"]:nth-child(4)::after`,
		gwccss.Raw("animation-delay", ".36s"),
	)
	declareGlobal(`.jn-step[data-state="done"]:nth-child(5)::after`,
		gwccss.Raw("animation-delay", ".48s"),
	)
	declareGlobal(`.jn-stepmark`,
		gwccss.Raw("flex", "none"),
		gwccss.W(gwccss.Rem(1.75)),
		gwccss.H(gwccss.Rem(1.75)),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Justify.Center,
		gwccss.Border(gwccss.Px(2), gwccss.Var("jn-control-border")),
		gwccss.Bg(gwccss.Var("jn-surface")),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "700"),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
		gwccss.Position.Relative,
		gwccss.ZIndex(1),
	)
	declareGlobal(`.jn-step[data-state="done"] .jn-stepmark`,
		gwccss.Bg(gwccss.Var("jn-accent")),
		gwccss.BorderColor(gwccss.Var("jn-accent")),
		gwccss.TextColor(gwccss.Var("jn-accent-ink")),
	)
	declareGlobal(`.jn-step[data-state="done"] .jn-stepcheck`,
		gwccss.Keyframes("jn-pop", jnPopFrames...),
		gwccss.Animation(gwccss.RawDuration(".35s"), gwccss.Easing("var(--jn-ease)")),
		gwccss.Raw("animation-fill-mode", "both"),
	)
	declareGlobal(`.jn-step[data-state="active"] .jn-stepmark`,
		gwccss.BorderColor(gwccss.Var("jn-accent")),
		gwccss.BorderWidth(gwccss.Px(3)),
		gwccss.TextColor(gwccss.Var("jn-accent")),
		gwccss.Bg(gwccss.Var("jn-accent-soft")),
		gwccss.Keyframes("jn-halo", jnHaloFrames...),
		gwccss.Animation(gwccss.RawDuration("2.4s"), gwccss.Easing("var(--jn-ease)")),
		gwccss.Raw("animation-iteration-count", "infinite"),
	)
	declareGlobal(`.jn-step[data-state="failed"] .jn-stepmark`,
		gwccss.Bg(gwccss.Var("jn-danger")),
		gwccss.BorderColor(gwccss.Var("jn-danger")),
		gwccss.TextColor(gwccss.Var("jn-danger-ink")),
	)
	declareGlobal(`.jn-stepbody`,
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("padding-top", ".125rem"),
	)
	declareGlobal(`.jn-steplabel`,
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-step[data-state="active"] .jn-steplabel`,
		gwccss.TextColor(gwccss.Var("jn-accent")),
	)
	declareGlobal(`.jn-step[data-state="upcoming"] .jn-steplabel`,
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-step[data-state="failed"] .jn-steplabel`,
		gwccss.TextColor(gwccss.Var("jn-danger")),
	)
	declareGlobal(`.jn-stepdetail`,
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-stepat`,
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(`.jn-stepper`,
		mediaRule(gwccss.RawMedia("(min-width:60rem)"), gwccss.FlexDir.Row),
	)
	declareGlobal(`.jn-step`,
		mediaRule(gwccss.RawMedia("(min-width:60rem)"), gwccss.Raw("flex", "1 1 0"), gwccss.FlexDir.Col, gwccss.Gap(gwccss.Rem(.5)), gwccss.Raw("padding-bottom", "0"), gwccss.Raw("padding-inline-end", "var(--jn-s2)")),
	)
	declareGlobal(`.jn-step:last-child`,
		mediaRule(gwccss.RawMedia("(min-width:60rem)"), gwccss.Raw("padding-inline-end", "0")),
	)
	declareGlobal(`.jn-step::before`,
		mediaRule(gwccss.RawMedia("(min-width:60rem)"), gwccss.Raw("inset-inline-start", "2.25rem"), gwccss.Raw("inset-inline-end", ".5rem"), gwccss.Top(gwccss.Rem(.8125)), gwccss.Bottom(gwccss.RawLength("auto")), gwccss.W(gwccss.RawLength("auto")), gwccss.H(gwccss.Px(2))),
	)
	declareGlobal(`.jn-step::after`,
		mediaRule(gwccss.RawMedia("(min-width:60rem)"), gwccss.Raw("inset-inline-start", "2.25rem"), gwccss.Top(gwccss.Rem(.8125)), gwccss.H(gwccss.Px(2)), gwccss.W(gwccss.Zero)),
	)
	declareGlobal(`.jn-step[data-state="done"]::after`,
		mediaRule(gwccss.RawMedia("(min-width:60rem)"), gwccss.W(gwccss.RawLength("calc(100% - 2.75rem)")), gwccss.H(gwccss.Px(2)), gwccss.Keyframes("jn-grow-x", jnGrowXFrames...), gwccss.Animation(gwccss.RawDuration(".5s"), gwccss.Easing("var(--jn-ease)")), gwccss.Raw("animation-fill-mode", "both")),
	)
	declareGlobal(`.jn-stepbody`,
		mediaRule(gwccss.RawMedia("(min-width:60rem)"), gwccss.Raw("padding-top", "0")),
	)
	declareGlobal(`.jn-columns`,
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.VarLength("jn-s3")),
		gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1))),
		gwccss.Raw("align-items", "start"),
	)
	declareGlobal(`.jn-columns`,
		mediaRule(gwccss.RawMedia("(min-width:64rem)"), gwccss.GridCols(gwccss.MinMax(gwccss.TrackLen(gwccss.Zero), gwccss.Fr(1)), gwccss.TrackLen(gwccss.Rem(23)))),
	)
	declareGlobal(`.jn-rail`,
		mediaRule(gwccss.RawMedia("(min-width:64rem)"), gwccss.Position.Static),
	)
	declareGlobal(`.jn-col`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.VarLength("jn-s3")),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.jn-context-nav`,
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.RowGap(gwccss.Rem(.5)), gwccss.ColumnGap(gwccss.Rem(1.25)),
		gwccss.Items.Center,
	)
	declareGlobal(`.jn-context-nav a`,
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
		gwccss.TextColor(gwccss.Var("jn-accent")),
	)
	declareGlobal(`.jn-context-nav a:first-child::before`,
		gwccss.Raw("content", "\"\\2190\\00a0\""),
	)
	declareGlobal(`.jn-subsection`,
		gwccss.Raw("margin-top", "var(--jn-s3)"),
	)
	declareGlobal(`.jn-subsection:first-of-type`,
		gwccss.Raw("margin-top", "var(--jn-s2)"),
	)
	declareGlobal(`.jn-subhead`,
		gwccss.Raw("margin-bottom", ".75rem"),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("letter-spacing", "var(--hcm-tracking-caps,.05em)"),
		gwccss.Raw("text-transform", "uppercase"),
	)
	declareGlobal(`.jn-facts`,
		gwccss.Display.Grid,
		gwccss.RowGap(gwccss.Rem(.75)), gwccss.ColumnGap(gwccss.VarLength("jn-s2")),
		gwccss.GridCols(gwccss.Fr(1)),
	)
	declareGlobal(`.jn-facts`,
		mediaRule(gwccss.RawMedia("(min-width:34rem)"), gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1))),
	)
	declareGlobal(`.jn-fact`,
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.jn-fact dt`,
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Tracking(gwccss.Ems(.02)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	// The value is the fact; its label is the caption. At regular weight the
	// value read as a second line of the same caption.
	declareGlobal(`.jn-fact dd`,
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-fact[data-tone="success"] dd`,
		gwccss.TextColor(gwccss.Var("jn-success")),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-fact[data-tone="warning"] dd`,
		gwccss.TextColor(gwccss.Var("jn-warning")),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-fact[data-tone="danger"] dd`,
		gwccss.TextColor(gwccss.Var("jn-danger")),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-fact[data-tone="info"] dd`,
		gwccss.TextColor(gwccss.Var("jn-info")),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-tablewrap`,
		gwccss.Raw("overflow-x", "auto"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.Rounded(gwccss.VarLength("jn-r2")),
	)
	declareGlobal(`.jn-people-preview-foot`,
		gwccss.Position.Sticky,
		gwccss.Raw("inset-inline-start", "0"),
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "space-between"),
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.Padding(gwccss.Rem(.75)),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.Bg(gwccss.Var("jn-surface-muted")),
	)
	declareGlobal(`.jn-people-preview-foot p`,
		gwccss.Margin(gwccss.Zero),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(`table.jn-table`,
		gwccss.W(gwccss.Percent(100)),
		gwccss.Raw("border-collapse", "collapse"),
		gwccss.FontSize(gwccss.Rem(0.875)),
	)
	declareGlobal(`.jn-table th,.jn-table td`,
		gwccss.PaddingY(gwccss.Rem(.5625)), gwccss.PaddingX(gwccss.Rem(.75)),
		gwccss.Raw("text-align", "start"),
		gwccss.Raw("vertical-align", "top"),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("jn-hairline")),
	)
	declareGlobal(`.jn-table thead th`,
		gwccss.Bg(gwccss.Var("jn-surface-muted")),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("letter-spacing", "var(--hcm-tracking-caps,.05em)"),
		gwccss.Raw("text-transform", "uppercase"),
		gwccss.Raw("white-space", "nowrap"),
		gwccss.Position.Sticky,
		gwccss.Top(gwccss.Zero),
	)
	declareGlobal(`.jn-table tbody tr:last-child th,.jn-table tbody tr:last-child td`,
		gwccss.Raw("border-bottom", "0"),
	)
	declareGlobal(`.jn-table td.jn-num,.jn-table th.jn-num`,
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(`.jn-table tbody th`,
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-table th.jn-mono`,
		gwccss.Raw("white-space", "nowrap"),
		gwccss.Raw("overflow-wrap", "normal"),
	)
	declareGlobal(`.jn-zebra tbody tr:nth-child(even)`,
		gwccss.Bg(gwccss.Var("jn-surface-sunk")),
	)
	declareGlobal(`.jn-table tbody tr`,
		gwccss.Transition(gwccss.TransitionProps(gwccss.Prop("background-color")), gwccss.S(.15), gwccss.Easing("var(--jn-ease)")),
	)
	declareGlobal(`.jn-table tbody tr:hover`,
		gwccss.Bg(gwccss.Var("jn-accent-soft")),
	)
	declareGlobal(`.jn-table tr[data-changed="true"]`,
		gwccss.Bg(gwccss.Var("jn-accent-soft")),
	)
	declareGlobal(`.jn-table tr[data-changed="true"] td.jn-proposed`,
		gwccss.Raw("font-weight", "600"),
		gwccss.TextColor(gwccss.Var("jn-accent")),
	)
	// The amount sits on its own line under the Changed chip and never
	// splits: "+USD" stranded beside the chip read as a separate value.
	declareGlobal(`.jn-delta`,
		gwccss.Display.Block,
		gwccss.Raw("margin-top", ".25rem"),
		gwccss.Raw("white-space", "nowrap"),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(`.jn-table td.jn-change`,
		gwccss.Raw("white-space", "nowrap"),
	)
	declareGlobal(`.jn-board`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.5)),
	)
	declareGlobal(`.jn-check`,
		gwccss.Display.Flex,
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.Raw("align-items", "flex-start"),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.Rounded(gwccss.VarLength("jn-r2")),
		gwccss.PaddingY(gwccss.Rem(.625)), gwccss.PaddingX(gwccss.Rem(.75)),
		gwccss.Bg(gwccss.Var("jn-surface")),
		gwccss.Keyframes("jn-slidein", jnSlideinFrames...),
		gwccss.Animation(gwccss.RawDuration(".4s"), gwccss.Easing("var(--jn-ease)")),
		gwccss.Raw("animation-fill-mode", "both"),
	)
	declareGlobal(`.jn-check[data-row="1"]`,
		gwccss.Raw("animation-delay", ".06s"),
	)
	declareGlobal(`.jn-check[data-row="2"]`,
		gwccss.Raw("animation-delay", ".12s"),
	)
	declareGlobal(`.jn-check[data-row="3"]`,
		gwccss.Raw("animation-delay", ".18s"),
	)
	declareGlobal(`.jn-check[data-row="4"]`,
		gwccss.Raw("animation-delay", ".24s"),
	)
	declareGlobal(`.jn-check[data-row="5"]`,
		gwccss.Raw("animation-delay", ".30s"),
	)
	declareGlobal(`.jn-check[data-row="6"]`,
		gwccss.Raw("animation-delay", ".36s"),
	)
	declareGlobal(`.jn-check[data-row="7"]`,
		gwccss.Raw("animation-delay", ".42s"),
	)
	declareGlobal(`.jn-check[data-severity="blocking"]`,
		gwccss.Raw("border-inline-start", "3px solid var(--jn-danger)"),
	)
	declareGlobal(`.jn-check[data-severity="warning"]`,
		gwccss.Raw("border-inline-start", "3px solid var(--jn-warning)"),
	)
	declareGlobal(`.jn-check[data-severity="success"]`,
		gwccss.Raw("border-inline-start", "3px solid var(--jn-success)"),
	)
	declareGlobal(`.jn-check[data-severity="info"]`,
		gwccss.Raw("border-inline-start", "3px solid var(--jn-info)"),
	)
	declareGlobal(`.jn-checkpill`,
		gwccss.Raw("flex", "none"),
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.3125)),
		gwccss.MinWidth(gwccss.Rem(6.5)),
		gwccss.Rounded(gwccss.VarLength("jn-rpill")),
		gwccss.PaddingY(gwccss.Rem(.25)), gwccss.PaddingX(gwccss.Rem(.625)),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("letter-spacing", "var(--hcm-tracking-caps,.05em)"),
		gwccss.Raw("text-transform", "uppercase"),
		gwccss.Bg(gwccss.Var("jn-neutral-soft")),
		gwccss.TextColor(gwccss.Var("jn-neutral")),
		gwccss.Raw("box-shadow", "inset 0 0 0 1px rgba(22,25,42,.05)"),
		gwccss.Position.Relative,
		gwccss.Raw("overflow", "hidden"),
	)
	declareGlobal(`.jn-checkpill[data-tone="info"]`,
		gwccss.Bg(gwccss.Var("jn-info-soft")),
		gwccss.TextColor(gwccss.Var("jn-info")),
	)
	declareGlobal(`.jn-checkpill[data-tone="success"]`,
		gwccss.Bg(gwccss.Var("jn-success-soft")),
		gwccss.TextColor(gwccss.Var("jn-success")),
	)
	declareGlobal(`.jn-checkpill[data-tone="warning"]`,
		gwccss.Bg(gwccss.Var("jn-warning-soft")),
		gwccss.TextColor(gwccss.Var("jn-warning")),
	)
	declareGlobal(`.jn-checkpill[data-tone="danger"]`,
		gwccss.Bg(gwccss.Var("jn-danger-soft")),
		gwccss.TextColor(gwccss.Var("jn-danger")),
	)
	declareGlobal(`.jn-checkpill::after`,
		gwccss.Raw("content", "\"\""),
		gwccss.Position.Absolute,
		gwccss.Raw("inset", "0"),
		gwccss.Rounded(gwccss.RawLength("inherit")),
		gwccss.Raw("background", "linear-gradient(100deg,transparent 20%,rgba(255,255,255,.55) 50%,transparent 80%)"),
		gwccss.Transform(gwccss.TranslateX(gwccss.Percent(-100))),
	)
	declareGlobal(`.jn-checkpill[data-tone="success"]::after`,
		gwccss.Keyframes("jn-sweep", jnSweepFrames...),
		gwccss.Animation(gwccss.RawDuration(".9s"), gwccss.Easing("var(--jn-ease)")),
		gwccss.Raw("animation-delay", ".35s"),
		gwccss.Raw("animation-fill-mode", "both"),
	)
}
