package journeycss

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

// declareJourneyTracker styles the Journeys tracker's filter form
// (UXLIVE-031) and the request card's identity header (UXLIVE-032).
func declareJourneyTracker() {
	// One grid for controls and actions: search (wider), status, order and
	// grouping on the first row; from, to and the actions on the second.
	// Two columns on a tablet, one on a phone -- never a lone field wrapped
	// onto a row of its own.
	declareGlobal(`.jn-journey-filter`,
		gwccss.Raw("margin-block", ".75rem 1rem"),
	)
	// Mobile first, like every other rule in this sheet: one column on a
	// phone, two on a tablet (search across both), four on desktop.
	declareGlobal(`.jn-journey-filter-fields`,
		gwccss.Display.Grid,
		gwccss.Raw("grid-template-columns", "minmax(0,1fr)"),
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.Raw("align-items", "end"),
	)
	declareGlobal(`.jn-journey-filter-actions`,
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Items.Center,
		gwccss.Gap(gwccss.Rem(.75)),
		gwccss.Raw("min-block-size", "2.5rem"),
	)
	// On a phone the controls other than search sit behind a "Filters"
	// toggle so the requests are on the first screen; closed, the panel
	// takes no room at all.
	declareGlobal(`.jn-journey-filter-toggle`,
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.Raw("justify-self", "start"),
		gwccss.Gap(gwccss.Rem(.375)),
	)
	declareGlobal(`.jn-journey-filter-panel`,
		gwccss.Display.Grid,
		gwccss.Raw("grid-template-columns", "minmax(0,1fr)"),
		gwccss.Gap(gwccss.Rem(.75)),
	)
	declareGlobal(`.jn-journey-filter-panel[data-open="false"]`,
		gwccss.Display.None,
	)
	// From a tablet up there is room for every control: the toggle goes and
	// the panel's children join the form's own grid.
	declareGlobal(`.jn-journey-filter-toggle`,
		mediaRule(gwccss.RawMedia("(min-width:40rem)"), gwccss.Display.None),
	)
	declareGlobal(`.jn-journey-filter-panel,.jn-journey-filter-panel[data-open="false"]`,
		mediaRule(gwccss.RawMedia("(min-width:40rem)"), gwccss.Raw("display", "contents")),
	)
	declareGlobal(`.jn-journey-filter-fields`,
		mediaRule(gwccss.RawMedia("(min-width:40rem)"), gwccss.Raw("grid-template-columns", "repeat(2,minmax(0,1fr))")),
	)
	declareGlobal(`.jn-journey-filter-fields>.jn-field:first-child`,
		mediaRule(gwccss.RawMedia("(min-width:40rem)"), gwccss.Raw("grid-column", "1/-1")),
	)
	declareGlobal(`.jn-journey-filter-fields`,
		mediaRule(gwccss.RawMedia("(min-width:68.75rem)"), gwccss.Raw("grid-template-columns", "repeat(5,minmax(0,1fr))")),
	)
	declareGlobal(`.jn-journey-filter-fields>.jn-field:first-child`,
		mediaRule(gwccss.RawMedia("(min-width:68.75rem)"), gwccss.Raw("grid-column", "span 2")),
	)
	declareGlobal(`.jn-journey-filter-actions`,
		mediaRule(gwccss.RawMedia("(min-width:68.75rem)"), gwccss.Raw("grid-column", "span 3")),
	)
	declareGlobal(`.jn-journey-filter-clear`,
		gwccss.Raw("color", "var(--jn-accent)"),
		gwccss.Raw("font-size", "0.875rem"),
		gwccss.Raw("font-weight", "600"),
	)
	// The heading is the task label alone and may wrap between words; the
	// status chip keeps its own width beside it instead of being squeezed.
	declareGlobal(`.jn-journey-top h3`,
		gwccss.Raw("min-inline-size", "0"),
		gwccss.Raw("overflow-wrap", "break-word"),
	)
	declareGlobal(`.jn-journey-status`,
		gwccss.Display.InlineFlex,
		gwccss.Raw("flex", "none"),
	)
	// "Request 8CF888" is secondary metadata on its own line. The label and
	// the token may move to the next line as a whole, but the token itself
	// never breaks: it is one isolated left-to-right run with no wrap point.
	declareGlobal(`.jn-journey-refline`,
		gwccss.Display.Flex,
		gwccss.Raw("flex-wrap", "wrap"),
		gwccss.Items.Center,
		gwccss.Raw("column-gap", ".375rem"),
		gwccss.Raw("row-gap", ".25rem"),
		gwccss.Margin(gwccss.Zero),
		gwccss.Raw("font-size", "0.75rem"),
		gwccss.Raw("color", "var(--jn-ink-muted)"),
	)
	declareGlobal(`.jn-journey-refline .jn-journey-ref`,
		gwccss.Raw("margin-inline-start", "0"),
		gwccss.Raw("white-space", "nowrap"),
		gwccss.Raw("direction", "ltr"),
		gwccss.Raw("opacity", "1"),
		gwccss.Raw("color", "var(--jn-ink)"),
	)
}
