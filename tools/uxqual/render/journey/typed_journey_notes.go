package journey

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

// declareJourneyNotes styles the notes panel (notes.go). It uses only the
// journey tokens, logical properties (so the avatar and the own-note accent
// sit on the reading-start side in Arabic) and the shared control styles for
// the textarea and button.
func declareJourneyNotes() {
	declareGlobal(`.jn-notes-count`,
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.Raw("margin-inline-start", ".5rem"),
		gwccss.Raw("padding", "0 .5rem"),
		gwccss.Raw("min-width", "1.5rem"),
		gwccss.Raw("justify-content", "center"),
		gwccss.Rounded(gwccss.VarLength("jn-rpill")),
		gwccss.Bg(gwccss.Var("jn-neutral-soft")),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
		gwccss.Raw("vertical-align", "middle"),
	)
	declareGlobal(`.jn-notes-empty`,
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.Raw("margin-bottom", "var(--jn-s2)"),
	)
	declareGlobal(`.jn-notes`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.VarLength("jn-s1")),
		gwccss.Raw("margin-bottom", "var(--jn-s2)"),
		gwccss.Raw("max-height", "28rem"),
		gwccss.Raw("overflow-y", "auto"),
		gwccss.Raw("overscroll-behavior", "contain"),
		gwccss.Raw("scrollbar-width", "thin"),
	)
	declareGlobal(`.jn-note-card`,
		gwccss.Display.Flex,
		gwccss.Gap(gwccss.Rem(.625)),
		gwccss.Raw("padding", ".625rem .75rem"),
		gwccss.Rounded(gwccss.VarLength("jn-r2")),
		gwccss.Bg(gwccss.Var("jn-surface-muted")),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-hairline")),
	)
	declareGlobal(`.jn-note[data-own="true"] .jn-note-card`,
		gwccss.Bg(gwccss.Var("jn-accent-soft")),
		gwccss.Raw("border-inline-start", "3px solid var(--jn-accent)"),
	)
	declareGlobal(`.jn-note-avatar`,
		gwccss.Raw("flex", "none"),
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.Raw("justify-content", "center"),
		gwccss.W(gwccss.Rem(1.75)),
		gwccss.H(gwccss.Rem(1.75)),
		gwccss.Rounded(gwccss.Percent(50)),
		gwccss.Bg(gwccss.Var("jn-neutral-soft")),
		gwccss.TextColor(gwccss.Var("jn-ink")),
		gwccss.FontSize(gwccss.Rem(0.6875)),
		gwccss.Raw("font-weight", "700"),
		gwccss.Raw("letter-spacing", ".02em"),
	)
	declareGlobal(`.jn-note[data-own="true"] .jn-note-avatar`,
		gwccss.Bg(gwccss.Var("jn-accent")),
		gwccss.TextColor(gwccss.Var("jn-accent-ink")),
	)
	declareGlobal(`.jn-note-main`,
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Raw("flex", "1 1 auto"),
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.125)),
	)
	declareGlobal(`.jn-note-meta`,
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Items.Baseline,
		gwccss.Raw("column-gap", ".5rem"),
		gwccss.FontSize(gwccss.Rem(0.8125)),
	)
	declareGlobal(`.jn-note-author`,
		gwccss.Raw("font-weight", "600"),
		gwccss.TextColor(gwccss.Var("jn-ink")),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(`.jn-note-own`,
		gwccss.FontSize(gwccss.Rem(0.6875)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("padding", "0 .375rem"),
		gwccss.Rounded(gwccss.VarLength("jn-rpill")),
		gwccss.Bg(gwccss.Var("jn-accent")),
		gwccss.TextColor(gwccss.Var("jn-accent-ink")),
	)
	declareGlobal(`.jn-note-at`,
		gwccss.Raw("margin-inline-start", "auto"),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(`.jn-note-stage`,
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.jn-note-body`,
		gwccss.Raw("margin-top", ".25rem"),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("line-height", "1.5"),
		gwccss.TextColor(gwccss.Var("jn-ink")),
		gwccss.Raw("white-space", "pre-wrap"),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(`.jn-note-compose`,
		gwccss.Display.Flex,
		gwccss.FlexDir.Col,
		gwccss.Gap(gwccss.Rem(.5)),
		gwccss.Raw("padding-top", "var(--jn-s2)"),
		gwccss.BorderTop(gwccss.Px(1), gwccss.Var("jn-hairline")),
	)
	declareGlobal(`.jn-note-compose textarea`,
		gwccss.Raw("min-height", "5.5rem"),
		gwccss.Raw("resize", "vertical"),
	)
	declareGlobal(`.jn-note-compose-foot`,
		gwccss.Display.Flex,
		gwccss.Items.Center,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Gap(gwccss.Rem(.75)),
	)
	declareGlobal(`.jn-note-compose-foot .jn-btn`,
		gwccss.Raw("margin-inline-start", "auto"),
	)
	declareGlobal(`.jn-note-count`,
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(`.jn-note-count[data-state="near"]`,
		gwccss.TextColor(gwccss.Var("jn-warning")),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-note-count[data-state="over"]`,
		gwccss.TextColor(gwccss.Var("jn-danger-ink")),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.jn-note-status`,
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.TextColor(gwccss.Var("jn-success")),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("min-height", "1em"),
	)
	declareGlobal(`.jn-note-status:empty`,
		gwccss.Display.None,
	)
}
