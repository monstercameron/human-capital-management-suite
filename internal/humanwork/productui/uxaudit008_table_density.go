package productui

import (
	"fmt"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// peopleColumnNthChildSelector mirrors typed_mig_B.go's unscoped
// `.people-table :is(th,td):nth-child(N)` column-width selectors, scoped
// under `.people-directory` so the `!important` min-width restoration below
// only ever applies to the People directory's own table.
func peopleColumnNthChildSelector(nthChild int) string {
	return fmt.Sprintf(".people-directory .people-table :is(th,td):nth-child(%d)", nthChild)
}

// pxImportant renders an `!important` pixel length, matching the literal
// "<n>px!important" spelling used elsewhere in this package's typed CSS
// (e.g. typed_mig_B.go's `min-width:0!important`).
func pxImportant(px int) string {
	return fmt.Sprintf("%dpx!important", px)
}

// UXAUDIT-008: the People directory's table viewport.
//
// The live audit measured `thead th` computed `position` as `static` at
// 1024x768. The existing sticky rules (peopleStickyHeaderStylesStylesheet,
// dataTableStylesStylesheet) set position:sticky on the header <tr> and on
// <thead> itself, but CSS position never inherits: a plain <th> child's own
// computed position stays static regardless of what its ancestor declares.
// That is the literal fact the audit recorded, and it is why the header
// cells never established their own sticky positioning context. This file
// adds position:sticky directly to the header cells, additively -- it does
// not replace or reorder the existing rules, which stay correct and now
// redundant-but-harmless.
//
// It also gives the directory's own scroll region room to show more than
// zero rows above the fold: `.data-table-scroll`'s prior cap was a fixed
// min(70vh,760px) regardless of how much height the filter and summary
// chrome above it already consumed, which is why the audit found zero rows
// fully visible in the first screen. Scoping a taller, flexible allowance
// to `.people-directory .data-table-scroll` (higher specificity than the
// shared `.data-table-scroll` rule, so it wins without needing rule order)
// leaves every other DataTable consumer's sizing untouched.
//
// FOLLOW-UP (live re-audit, still UXAUDIT-008): the first version of this
// file scoped the taller cap unconditionally -- present at every width,
// including inside dataTableStylesStylesheet's own `@media (max-width:
// 1050px)` card-mode breakpoint, where that shared rule resets
// `.data-table-scroll` to `max-height:none;overflow:visible` on purpose (the
// table becomes a stacked card list below 1050px, and the wrapper stops
// being its own scroll container because the whole card list is meant to
// flow into the page). Because the scoped selector here
// (`.people-directory .data-table-scroll`, two classes) is more specific
// than the shared one-class selector, it beat `max-height:none` even inside
// that media query -- but it did not also restate `overflow`, so the
// wrapper ended up clamped to a bounded height while still declared
// `overflow:visible`. `section.surface.people-directory` clips at its own
// box (`overflow:clip`), so content past that clamp had nowhere to go and
// was silently dropped: 16 of 20 workers were unreachable at 1024x768, a
// regression this file introduced (nothing clamped the wrapper below
// 1050px before this file existed). A bounded `max-height` and a scrolling
// `overflow` are a matched pair; the fix is to give the pair its own
// `min-width` media context that simply does not exist below 761px, rather
// than trying to out-specify the shared reset property-by-property (see
// TestTodo_UXAUDIT_008's "scroll wrapper max-height and overflow stay a
// matched pair" subtest in uxaudit008_people_table_test.go for the
// rule-level invariant this encodes: it was mutation-verified against the
// exact broken pairing this note describes -- see that subtest's own
// comment for the observed fail/revert/pass sequence).
//
// Separately, the live re-audit found 1024x768 -- a desktop viewport by
// UXAUDIT-008's own RED ("only a few rows fit in a desktop viewport") --
// still landed inside the shared component's card breakpoint (<=1050px),
// so even with the clipping fixed, the directory showed roughly three
// 199px-tall stacked cards per screen. Two ways to close that were on the
// table: densify the card layout, or give the People directory back the
// real, dense (46px/row) table down to a narrower width than the shared
// component's default. This file takes the second path, scoped entirely
// under `.people-directory` so History and Organization (the other
// `.data-table` consumers, which never had a RED filed against their
// 1024px behavior) keep the shared component's card breakpoint verbatim.
// Every rule below re-declares, at higher selector specificity (and
// `!important` where the rule it counters used `!important`), exactly the
// desktop-mode property values dataTableStylesStylesheet already uses above
// 1050px -- it does not invent a new visual language, it just extends the
// existing desktop table down to 761px for this one consumer. Below 761px
// (phone width) the People directory still falls back to the shared
// component's stacked-card mode, which this todo's own RED never named and
// which the "scroll wrapper max-height and overflow stay a matched pair"
// subtest proves stays a safe, matched max-height/overflow pair by
// declaring nothing for the wrapper below 761px at all.
func uxaudit008TableDensityStylesheet() string {
	return buildTypedSheet(declareUxaudit008TableDensityStyles)
}

func declareUxaudit008TableDensityStyles() {
	declareGlobal(".data-table thead th",
		gwccss.Position.Sticky,
		gwccss.Top(gwccss.Zero),
	)
	// The bounded cap now lives only inside a `min-width:761px` context, so
	// it does not exist at all in the shared component's card-mode range
	// (<=1050px, further stacked to a single column at <=760px) -- it never
	// competes with that mode's own `max-height:none;overflow:visible`
	// reset. `overflow:auto` travels with the cap in the same rule so the
	// two can never be split across contexts again.
	declareGlobal(".people-directory .data-table-scroll",
		mediaRule(gwccss.MinW(761),
			gwccss.MaxHeight(gwccss.MinLen(gwccss.Vh(82), gwccss.Px(920))),
			gwccss.MinHeight(gwccss.Rem(14)),
			gwccss.Raw("overflow", "auto"),
		),
	)
	// Give the People directory back a real, dense <table> down to 761px
	// instead of the shared component's <=1050px stacked-card mode (option
	// 2 of UXAUDIT-008's outcome 2 -- "move the table/card breakpoint below
	// 1024" -- applied only to this consumer). Each rule below restates,
	// scoped under `.people-directory` at higher specificity than the
	// generic `@media (max-width:1050px)` rule it counters (matching
	// `!important` wherever that generic rule used it), the exact
	// desktop-mode value dataTableStylesStylesheet already uses above
	// 1050px. Only the display/box-model properties that actually change
	// under the card-mode reset are restated; grid/flex-only properties
	// (grid-template-columns, justify-content, gap sized for a card) become
	// inert automatically once display stops being grid/flex, so they are
	// left alone rather than duplicated.
	declareGlobal(".people-directory .data-table",
		mediaRule(gwccss.MinW(761), gwccss.Raw("display", "table")),
	)
	declareGlobal(".people-directory .data-table thead",
		mediaRule(gwccss.MinW(761),
			gwccss.Raw("display", "table-header-group"),
			gwccss.Padding(gwccss.Zero),
			gwccss.Raw("border-bottom", "0"),
			gwccss.Raw("background", "transparent"),
			gwccss.Raw("box-shadow", "none"),
			gwccss.Raw("overflow-x", "visible"),
		),
	)
	declareGlobal(".people-directory .data-table-head",
		mediaRule(gwccss.MinW(761), gwccss.Raw("display", "table-row!important")),
	)
	declareGlobal(".people-directory .data-table-head>.data-table-column",
		mediaRule(gwccss.MinW(761),
			gwccss.Raw("display", "table-cell"),
			gwccss.PaddingY(gwccss.Zero), gwccss.PaddingX(gwccss.Px(14)),
			gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
			gwccss.Bg(gwccss.Var("surface-subtle")),
			gwccss.Raw("box-shadow", "0 8px 14px color-mix(in srgb,var(--ink) 6%,transparent)"),
		),
	)
	declareGlobal(".people-directory .data-table-head>.data-table-column.align-end",
		mediaRule(gwccss.MinW(761), gwccss.Raw("display", "table-cell")),
	)
	declareGlobal(".people-directory .data-table-sort",
		mediaRule(gwccss.MinW(761),
			gwccss.MinHeight(gwccss.Px(48)),
			gwccss.Padding(gwccss.Zero),
			gwccss.Raw("border", "0"),
			gwccss.Rounded(gwccss.Zero),
			gwccss.Raw("background", "transparent"),
		),
	)
	declareGlobal(".people-directory .data-table-sort.active",
		mediaRule(gwccss.MinW(761),
			gwccss.Raw("border-color", "transparent"),
			gwccss.Raw("background", "transparent"),
		),
	)
	declareGlobal(".people-directory .data-table-sort-label",
		mediaRule(gwccss.MinW(761), gwccss.Display.None),
	)
	declareGlobal(".people-directory .data-table-body",
		mediaRule(gwccss.MinW(761), gwccss.Raw("display", "table-row-group")),
	)
	declareGlobal(".people-directory .data-table .data-table-row",
		mediaRule(gwccss.MinW(761), gwccss.Raw("display", "table-row")),
	)
	declareGlobal(".people-directory .data-table .data-table-cell",
		mediaRule(gwccss.MinW(761),
			gwccss.Raw("display", "table-cell!important"),
			gwccss.PaddingY(gwccss.Px(11)), gwccss.PaddingX(gwccss.Px(14)),
			gwccss.BorderBottom(gwccss.Px(1), gwccss.Var("line")),
			gwccss.Raw("background", "var(--surface)!important"),
			gwccss.Raw("text-align", "left"),
		),
	)
	declareGlobal(".people-directory .data-table .data-table-cell:not(.data-table-row-header):not(.people-row-actions):before",
		mediaRule(gwccss.MinW(761), gwccss.Raw("content", "none")),
	)
	declareGlobal(".people-directory .data-table .people-row-actions",
		mediaRule(gwccss.MinW(761), gwccss.Raw("display", "table-cell!important")),
	)
	// The People columns' min-width floor (name/role/team/manager/location/
	// actions) is shared, unscoped, non-`!important` CSS
	// (`.people-table :is(th,td):nth-child(N)` in typed_mig_B.go) that
	// already governs both header and body cells at every width. The
	// generic card-mode rules force it to `0!important` on both the header
	// cell (`.data-table-head>.data-table-column`) and the body cell
	// (`.data-table .data-table-cell`) so columns can collapse to full-width
	// stacked fields; restoring a real table between 761px and 1050px means
	// restoring those same six widths as `!important` here too, at higher
	// specificity, so the columns line up exactly as they do above 1050px.
	for index, minWidthPx := range [...]int{185, 180, 145, 125, 135, 110} {
		declareGlobal(peopleColumnNthChildSelector(index+1),
			mediaRule(gwccss.MinW(761), gwccss.Raw("min-width", pxImportant(minWidthPx))),
		)
	}
	// Compact availability affordance (GREEN: "compact row actions
	// communicate availability without visual noise"). A short label plus a
	// muted, bordered chip reads as a status marker rather than a paragraph;
	// the full reason stays in `title` and the visually-hidden child span,
	// never in the visible layout.
	declareGlobal(".people-availability-badge",
		gwccss.Display.InlineFlex,
		gwccss.Items.Center,
		gwccss.PaddingY(gwccss.Px(3)), gwccss.PaddingX(gwccss.Px(8)),
		gwccss.Border(gwccss.Px(1), gwccss.Var("line")),
		gwccss.Rounded(gwccss.VarLength("hcm-radius-control")),
		gwccss.FontSize(gwccss.Rem(0.75)),
		gwccss.Raw("white-space", "nowrap"),
		gwccss.Raw("cursor", "default"),
	)
}
