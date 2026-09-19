package productui

import (
	"strings"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ScrollRegionSelectors lists every selector ScrollRegion's shared stylesheet
// (declareScrollRegionStyles) governs together: shell.go's ".main-scroll",
// navigation_components.go's ".primary-nav" and ".sidebar" (the narrow-
// viewport drawer), data_table.go's ".data-table-scroll" and
// action_launcher.go's ".action-launcher-dialog". It is exported as data --
// not a hardcoded literal repeated in every test that needs the adopted set
// -- specifically so the accessibility and regression tests derive their
// expectations from the same list ScrollRegion's CSS is built from, rather
// than a second, driftable copy of the same five names.
//
// Deliberately not a shared HTML class: every one of these selectors is
// already asserted, elsewhere in this package's test suite, as the complete,
// exact value of its element's class attribute (e.g. `class="sidebar"`,
// `class="data-table-scroll"`). Adding a marker class to the DOM would
// silently widen every one of those strings and break assertions that have
// nothing to do with scrolling. The shared CSS below reaches all five
// through one comma-separated selector list instead, so "one place declares
// this" holds in the Go source without touching the rendered markup's class
// values at all.
var ScrollRegionSelectors = []string{".main-scroll", ".primary-nav", ".sidebar", ".data-table-scroll", ".action-launcher-dialog"}

// ScrollRegionProps configures the one component every scroll-owning surface
// in the shell renders through (UIPOLISH-004 REFACTOR: "one scroll-region
// component owns overflow, shadows, restoration, reduced motion and
// accessible naming"). Before this file existed, four different call sites
// each declared their own overflow CSS and their own (or no)
// role/tabIndex/aria-label wiring by hand, with no shared answer between
// them. ScrollRegion does not change what each region visually looks like or
// how it is sized (every adopter keeps its own selector -- see Class -- and
// every selector-scoped CSS rule, including UXAUDIT-008's ".people-directory
// .data-table-scroll" max-height/overflow matched-pair invariant, is
// untouched); it consolidates the parts that are supposed to be identical in
// kind across every scroll owner: the shared focus-visible and reduced-
// motion contract declared once in declareScrollRegionStyles, whether the
// region is keyboard-focusable, its accessible name, and whether it restores
// its scroll position across a re-render.
type ScrollRegionProps struct {
	// Tag selects the element ScrollRegion renders ("main", "nav", "aside",
	// "div", ...). Empty defaults to "div". A semantic landmark tag keeps
	// its own implicit ARIA role: ScrollRegion never assigns Role for you,
	// so a caller rendering a <main> or <nav> simply leaves Role empty
	// instead of ScrollRegion silently overwriting the landmark.
	Tag string
	ID  string
	// Class is the region's own, complete class list (".main-scroll",
	// ".data-table-scroll", ...), rendered verbatim -- see
	// ScrollRegionSelectors for why ScrollRegion never appends a marker
	// class of its own.
	Class string
	// Role sets an explicit ARIA role (e.g. "region" for a plain wrapper
	// div, "dialog" for an overlay). Leave empty to keep the tag's own
	// implicit landmark role.
	Role string
	// Focusable marks a region GREEN requires to be keyboard-scrollable on
	// its own: it renders tabIndex="0", the only way to make a
	// non-interactive scrolling container keyboard-reachable at all.
	Focusable bool
	// RestoreScroll opts this instance into scroll-position restoration
	// across re-renders (real on a js/wasm client; a no-op during SSR and
	// native tests -- see scroll_restoration_native.go and
	// scroll_restoration_wasm.go). ID is the restoration key, so it must be
	// set whenever this is true.
	RestoreScroll bool
	Aria          map[string]string
	Raw           map[string]any
	Data          map[string]string
	OnKeyDown     ui.Handler
	Children      []ui.Node
}

// scrollRestorationEnabled is the pure predicate useScrollRestoration (native
// no-op / wasm real, scroll_restoration_native.go / scroll_restoration_wasm.go)
// guards on before touching the DOM. It is kept here, build-tag-free, so it
// can be exercised directly by a native `go test` run rather than only by a
// wasm-only test file this lane cannot execute.
func scrollRestorationEnabled(id string, enabled bool) bool {
	return enabled && strings.TrimSpace(id) != ""
}

// ScrollRegion renders props.Tag (default "div") with the shared scroll
// contract applied. See ScrollRegionProps for what each field controls.
func ScrollRegion(props ScrollRegionProps) ui.Node {
	useScrollRestoration(props.ID, props.RestoreScroll)
	tag := strings.TrimSpace(props.Tag)
	if tag == "" {
		tag = "div"
	}
	htmlProps := html.Props{
		ID: props.ID, Class: props.Class, Role: props.Role,
		Aria: props.Aria, Raw: props.Raw, Data: props.Data, OnKeyDown: props.OnKeyDown,
	}
	if props.Focusable {
		htmlProps.TabIndex = html.TabIndexZero
	}
	return html.Tag(tag, htmlProps, props.Children...)
}

// scrollRegionStylesheet builds the shared contract every ScrollRegion
// instance renders under, plus the handful of per-selector fixes RED named
// concretely (".main-scroll" and ".action-launcher-dialog" both reached the
// browser with an unstyled, untokenized scrollbar; ".action-launcher-dialog"
// also offered a horizontal scrollbar its wrapping text never needs). These
// per-selector rules are appended, not merged into an existing declareGlobal
// call for the same selector elsewhere, so they can never disturb a byte-
// pinned regression assertion on that other rule's own text -- they only add
// properties those rules did not already declare.
func scrollRegionStylesheet() string {
	return buildTypedSheet(declareScrollRegionStyles)
}

func declareScrollRegionStyles() {
	shared := strings.Join(ScrollRegionSelectors, ",")
	focusVisible := make([]string, len(ScrollRegionSelectors))
	for index, selector := range ScrollRegionSelectors {
		focusVisible[index] = selector + ":focus-visible"
	}
	// Shared contract: every scroll region contains its own gesture
	// (redundant-but-harmless where a selector already declares this, new
	// for one that did not) and gets the same focus-visible treatment
	// whether or not it previously had a keyboard focus outline at all. One
	// comma-separated declaration per concern, built from
	// ScrollRegionSelectors, is "one place" in the Go source without adding
	// a marker class to the rendered DOM (see ScrollRegionSelectors' doc for
	// why: several of these selectors are pinned elsewhere as the complete,
	// exact class attribute value of their element).
	declareGlobal(shared,
		gwccss.Raw("overscroll-behavior", "contain"),
	)
	declareGlobal(strings.Join(focusVisible, ","),
		gwccss.Raw("outline", "var(--hcm-focus-ring-width) solid var(--hcm-color-focus)"),
		gwccss.OutlineOffset(gwccss.Px(-2)),
	)
	declareGlobal(shared,
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("scroll-behavior", "auto")),
	)

	// Scroll-shadow affordance: two zero-height, sticky, pointer-events-none
	// pseudo-elements pinned to the region's own top and bottom edges. Using
	// ::before/::after (rather than a box-shadow or background property on
	// the region itself) means this can never clobber a region's own
	// background or box-shadow -- generated content paints as its own,
	// independent layer. It is a static affordance (always visible, not
	// scroll-position-aware): a scroll-position-reactive version needs a
	// live-browser pass to tune the show/hide threshold, which is exactly
	// the "what needs a live browser" carve-out for this todo.
	//
	// Scoped to ".data-table-scroll" only, not every ScrollRegionSelectors
	// entry: it is the one selector already guaranteed to be a safe
	// positioning context (Position.Relative, declared unconditionally in
	// dataTableStylesStylesheet) for a sticky pseudo-element to anchor
	// inside. Adding position:relative to ".main-scroll" or ".primary-nav"
	// blind, only to support a decorative shadow, risks changing the
	// containing block for some existing absolutely-positioned descendant
	// this lane has not audited live -- exactly the kind of change deferred
	// to the live-browser pass.
	declareGlobal(".data-table-scroll::before,.data-table-scroll::after",
		gwccss.Position.Sticky,
		gwccss.Display.Block,
		gwccss.H(gwccss.Zero),
		gwccss.Raw("content", "\"\""),
		gwccss.Raw("pointer-events", "none"),
		gwccss.ZIndex(6),
	)
	declareGlobal(".data-table-scroll::before",
		gwccss.Top(gwccss.Zero),
		gwccss.Raw("box-shadow", "inset 0 8px 10px -8px color-mix(in srgb,var(--ink) 22%,transparent)"),
	)
	declareGlobal(".data-table-scroll::after",
		gwccss.Raw("bottom", "0"),
		gwccss.Raw("box-shadow", "inset 0 -8px 10px -8px color-mix(in srgb,var(--ink) 22%,transparent)"),
	)
	declareGlobal(".data-table-scroll::before,.data-table-scroll::after",
		mediaRule(gwccss.RawMedia("print"), gwccss.Display.None),
	)

	// UIPOLISH-004 RED, item 1 & 2: ".main-scroll" was the one measured
	// scroll region with no scrollbar tokens at all (scrollbar-width:auto,
	// scrollbar-color:auto -- a 15px native bar) while ".data-table-scroll"
	// and ".primary-nav" both already used
	// var(--hcm-nav-scrollbar-thumb)/var(--hcm-nav-scrollbar-track). This
	// gives it the same tokens; shell.go's pageFrame supplies the
	// role/tabIndex/aria-label half of the fix by rendering it through
	// ScrollRegion.
	declareGlobal(".main-scroll",
		gwccss.Raw("scrollbar-width", "thin"),
		gwccss.Raw("scrollbar-color", "var(--hcm-nav-scrollbar-thumb) var(--hcm-nav-scrollbar-track)"),
	)

	// UIPOLISH-004 RED, "a styled scrollbar intrudes into content": the
	// fourth ad hoc scroll region this todo's own text names --
	// ".action-launcher-dialog" -- declared "overflow:auto" on both axes
	// with no scrollbar tokens (an unstyled native bar) and no reason to
	// offer horizontal scrolling at all: its content is a heading, a search
	// input and a column of result rows, none of which need to scroll
	// sideways. overflow-x/overflow-y here are longhands of the shorthand
	// "overflow:auto" action_launcher.go's own declareActionLauncherStyles
	// still declares; longhands from a later-registered rule win per
	// property against an earlier shorthand at equal specificity, so this
	// narrows the dialog to vertical-only without editing that rule.
	declareGlobal(".action-launcher-dialog",
		gwccss.Raw("overflow-x", "hidden"),
		gwccss.Raw("overflow-y", "auto"),
		gwccss.Raw("scrollbar-width", "thin"),
		gwccss.Raw("scrollbar-color", "var(--hcm-nav-scrollbar-thumb) var(--hcm-nav-scrollbar-track)"),
	)

	// UXSCAN-005: history is a page-owned scroll surface. Keep all desktop
	// filter controls in explicit, usable tracks so native select values and
	// the search prompt are not squeezed into an implicit overflowing column.
	// Narrow viewports retain the one-column contract from the history styles.
	declareGlobal(".workflow-history",
		gwccss.Raw("overflow", "visible"),
	)
	declareGlobal(".history-columns",
		gwccss.Position.Sticky,
		gwccss.Top(gwccss.Zero),
		gwccss.ZIndex(5),
	)
	declareGlobal(".history-filter-controls",
		mediaRule(gwccss.MinW(1200), gwccss.Display.Grid, gwccss.GridCols(
			gwccss.MinMax(gwccss.TrackLen(gwccss.Px(300)), gwccss.Fr(1.3)),
			gwccss.MinMax(gwccss.TrackLen(gwccss.Px(150)), gwccss.Fr(.7)),
			gwccss.MinMax(gwccss.TrackLen(gwccss.Px(145)), gwccss.Fr(.7)),
			gwccss.MinMax(gwccss.TrackLen(gwccss.Px(180)), gwccss.Fr(.8)),
		)),
		mediaRule(gwccss.MinW(1400), gwccss.GridCols(
			gwccss.MinMax(gwccss.TrackLen(gwccss.Px(300)), gwccss.Fr(1.3)),
			gwccss.MinMax(gwccss.TrackLen(gwccss.Px(150)), gwccss.Fr(.7)),
			gwccss.MinMax(gwccss.TrackLen(gwccss.Px(145)), gwccss.Fr(.7)),
			gwccss.MinMax(gwccss.TrackLen(gwccss.Px(180)), gwccss.Fr(.8)),
			gwccss.TrackLen(gwccss.RawLength("max-content")),
		)),
	)
	// The button takes the next free cell rather than a fixed column 4. The
	// person and year selects render only when they can filter something, so
	// on a profile the row is search, outcome, button -- and a button pinned
	// to column 4 and pushed to its end sat far from the filters it applies.
	declareGlobal(".history-filter-controls>.button",
		mediaRule(gwccss.MinW(1200), gwccss.Raw("grid-column", "auto"), gwccss.Raw("justify-self", "start")),
	)
	// At desktop widths the main page is the sole vertical scroll owner.
	// This also lets table headers stick to the page scroll rather than an
	// inner viewport that has no meaningful height of its own.
	declareGlobal(".people-directory .data-table-scroll",
		mediaRule(gwccss.MinW(1081),
			gwccss.MaxHeight(gwccss.RawLength("none")),
			gwccss.MinHeight(gwccss.Zero),
			gwccss.Raw("overflow", "visible"),
		),
	)
	declareGlobal(".history-filter-controls input,.history-filter-controls select",
		gwccss.Raw("text-overflow", "clip"),
		gwccss.Raw("white-space", "normal"),
	)
}
