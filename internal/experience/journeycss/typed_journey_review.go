package journeycss

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

// declareJourneyReview lays out REV-091-02's review cards (promotion_review.go):
// productui's PromotionReview and CompensationGuardrailCard rendered inside
// this page, which otherwise has no rules for their class names. The cards
// stack in one column and sit side by side from 48rem; every rule uses the
// journey tokens and logical properties, so the layout holds at 390px, in
// dark mode and in Arabic.
func declareJourneyReview() {
	declareGlobal(`.jn-review-cards`,
		gwccss.Display.Grid,
		gwccss.GridCols(gwccss.Fr(1)),
		gwccss.Gap(gwccss.VarLength("jn-s3")),
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.jn-review-cards`,
		mediaRule(gwccss.RawMedia("(min-width:48rem)"), gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1))),
	)
	declareGlobal(`.jn-review-card`,
		gwccss.MinWidth(gwccss.Zero),
		gwccss.Border(gwccss.Px(1), gwccss.Var("jn-hairline")),
		gwccss.Rounded(gwccss.VarLength("jn-r2")),
		gwccss.Bg(gwccss.Var("jn-surface-muted")),
		gwccss.Padding(gwccss.VarLength("jn-s3")),
		gwccss.Raw("overflow-wrap", "anywhere"),
	)
	declareGlobal(`.jn-review-card .jn-subhead`,
		gwccss.Raw("margin", "0 0 var(--jn-s2)"),
	)
	declareGlobal(`.promotion-review`,
		gwccss.Display.Grid,
		gwccss.Gap(gwccss.VarLength("jn-s1")),
	)
	declareGlobal(`.promotion-review p`,
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)),
	)
	declareGlobal(`.promotion-review-manager`,
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.promotion-review-cycle-safe`,
		gwccss.TextColor(gwccss.Var("jn-success")),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.promotion-review-cycle-unsafe`,
		gwccss.TextColor(gwccss.Var("jn-warning")),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.compensation-guardrail>h3`,
		gwccss.Raw("margin", "0 0 var(--jn-s2)"),
		gwccss.FontSize(gwccss.Rem(0.9375)),
		gwccss.Raw("font-weight", "600"),
	)
	declareGlobal(`.compensation-guardrail-facts`,
		gwccss.Display.Grid,
		gwccss.RowGap(gwccss.Rem(.75)), gwccss.ColumnGap(gwccss.VarLength("jn-s2")),
		gwccss.GridCols(gwccss.Fr(1)),
		gwccss.Margin(gwccss.Zero),
	)
	declareGlobal(`.compensation-guardrail-facts`,
		mediaRule(gwccss.RawMedia("(min-width:34rem)"), gwccss.GridCols(gwccss.Fr(1), gwccss.Fr(1))),
	)
	declareGlobal(`.compensation-guardrail-facts>div`,
		gwccss.MinWidth(gwccss.Zero),
	)
	declareGlobal(`.compensation-guardrail-facts dt`,
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("font-weight", "600"),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.compensation-guardrail-facts dd`,
		gwccss.Margin(gwccss.Zero),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.Raw("font-weight", "600"),
		gwccss.Raw("font-variant-numeric", "tabular-nums"),
	)
	declareGlobal(`.compensation-guardrail-reason`,
		gwccss.Raw("margin", "var(--jn-s2) 0 0"),
		gwccss.FontSize(gwccss.Rem(0.875)),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
	)
	declareGlobal(`.compensation-guardrail-action`,
		gwccss.Raw("max-inline-size", "100%"),
	)
	declareCompareStacked()
}

// compareTable is the width from which the four-column comparison table
// fits. Below it the fixed columns split "Management", money and dates
// mid-word and clipped the Changed chip, so the stacked layout is the base
// and the table is what this breakpoint adds (breakpoints only add; see
// TestBreakpointsOnlyAddColumns).
var compareTable = gwccss.RawMedia("(min-width:37.5rem)")

// declareCompareStacked lays out the current-versus-proposed table on a
// phone: each row is one item with the attribute as its heading, "current
// -> proposed" on one line that wraps only between the two values, and the
// change chip and delta beneath; unchanged rows are muted. The <table>
// stays in the tree with explicit roles (comparisonTableLocale) and its
// header row is visually hidden rather than removed, so assistive
// technology still reads a table with column headers. From 37.5rem every
// property is restored to the desktop table exactly as before.
func declareCompareStacked() {
	const (
		table = `.jn-compare.jn-table`
		row   = table + `>tbody>tr`
		cell  = row + `>:is(th,td)`
	)
	declareGlobal(table,
		gwccss.Display.Block, gwccss.Raw("table-layout", "auto"),
		mediaRule(compareTable, gwccss.Raw("display", "table"), gwccss.Raw("table-layout", "fixed")),
	)
	declareGlobal(table+`>tbody`,
		gwccss.Display.Block,
		mediaRule(compareTable, gwccss.Raw("display", "table-row-group")),
	)
	declareGlobal(table+`>thead`,
		gwccss.Position.Absolute, gwccss.W(gwccss.Px(1)), gwccss.H(gwccss.Px(1)),
		gwccss.Raw("overflow", "hidden"), gwccss.Raw("clip", "rect(0,0,0,0)"),
		mediaRule(compareTable, gwccss.Raw("position", "static"), gwccss.Raw("width", "auto"), gwccss.Raw("height", "auto"),
			gwccss.Raw("overflow", "visible"), gwccss.Raw("clip", "auto")),
	)
	declareGlobal(row,
		gwccss.Display.Flex, gwccss.Raw("flex-wrap", "wrap"), gwccss.Raw("align-items", "baseline"),
		gwccss.Raw("gap", ".25rem .5rem"), gwccss.Raw("padding", ".625rem .75rem"),
		gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("jn-hairline")),
		mediaRule(compareTable, gwccss.Raw("display", "table-row"), gwccss.Raw("padding", "0"), gwccss.Raw("border-bottom", "0")),
	)
	declareGlobal(row+`:last-child`,
		gwccss.Raw("border-bottom", "0"),
	)
	// A value never breaks inside a word or a number: overflow-wrap only
	// rescues a single value wider than the whole line.
	declareGlobal(cell,
		gwccss.Display.Block, gwccss.Padding(gwccss.Zero), gwccss.Raw("border", "0"),
		gwccss.Raw("overflow-wrap", "break-word"), gwccss.Raw("white-space", "normal"), gwccss.Raw("max-inline-size", "100%"),
		mediaRule(compareTable, gwccss.Raw("display", "table-cell"), gwccss.Raw("padding", ".5625rem .75rem"),
			gwccss.Raw("border-bottom", "1px solid var(--jn-hairline)"), gwccss.Raw("overflow-wrap", "anywhere"),
			gwccss.Raw("max-inline-size", "none")),
	)
	declareGlobal(row+`:last-child>:is(th,td)`,
		mediaRule(compareTable, gwccss.Raw("border-bottom", "0")),
	)
	declareGlobal(row+`>th`,
		gwccss.Raw("flex", "1 1 100%"), gwccss.FontSize(gwccss.Rem(0.75)), gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		mediaRule(compareTable, gwccss.Raw("font-size", "inherit"), gwccss.Raw("color", "inherit")),
	)
	// The two values do not shrink, so the line breaks between them rather
	// than inside either one.
	declareGlobal(row+`>td:nth-child(2),`+row+`>td:nth-child(3)`,
		gwccss.Raw("flex", "0 0 auto"),
	)
	declareGlobal(row+`>td.jn-proposed::before`,
		gwccss.Raw("content", `"\2192" / ""`), gwccss.Raw("margin-inline-end", ".5rem"),
		gwccss.TextColor(gwccss.Var("jn-ink-muted")), gwccss.Raw("font-weight", "400"),
		mediaRule(compareTable, gwccss.Raw("content", "none")),
	)
	declareGlobal(`[dir="rtl"] `+row+`>td.jn-proposed::before`,
		gwccss.Raw("content", `"\2190" / ""`),
		mediaRule(compareTable, gwccss.Raw("content", "none")),
	)
	// An unchanged row states its one value and "No change"; stacked, the
	// proposed cell only repeated the current value after an arrow.
	declareGlobal(row+`:not([data-changed="true"])>td.jn-proposed`,
		gwccss.Display.None,
		mediaRule(compareTable, gwccss.Raw("display", "table-cell")),
	)
	declareGlobal(row+`>td.jn-change`,
		gwccss.Raw("flex", "1 1 100%"),
	)
	declareGlobal(row+`:not([data-changed="true"])>td`,
		gwccss.TextColor(gwccss.Var("jn-ink-muted")),
		mediaRule(compareTable, gwccss.Raw("color", "inherit")),
	)
}
