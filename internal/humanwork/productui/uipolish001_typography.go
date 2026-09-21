package productui

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

// uipolish001TypographyStylesheet is the production typography contract.
// Components opt into named roles; no page is expected to choose a pixel size.
func uipolish001TypographyStylesheet() string {
	return buildTypedSheet(declareUIPolish001Typography)
}

func declareUIPolish001Typography() {
	declareGlobal(":root", gwccss.Custom("hcm-type-display", "clamp(2rem,1.5rem + 2vw,3.5rem)"), gwccss.Custom("hcm-type-page-title", "clamp(1.75rem,var(--hcm-font-size-heading,1.35rem),2.5rem)"), gwccss.Custom("hcm-type-section", "1.25rem"), gwccss.Custom("hcm-type-body", "var(--hcm-font-size-body,1rem)"), gwccss.Custom("hcm-type-label", "1rem"), gwccss.Custom("hcm-type-helper", "clamp(.875rem,var(--hcm-font-size-small,.82rem),1rem)"), gwccss.Custom("hcm-type-table", "clamp(.875rem,var(--hcm-font-size-small,.82rem),1rem)"), gwccss.Custom("hcm-type-code", "clamp(.875rem,var(--hcm-font-size-small,.82rem),1rem)"), gwccss.Custom("hcm-leading-display", "1.1"), gwccss.Custom("hcm-leading-heading", "1.25"), gwccss.Custom("hcm-leading-body", "var(--hcm-line-height,1.5)"), gwccss.Custom("hcm-leading-tight", "1.35"), gwccss.Custom("hcm-measure-readable", "70ch"), gwccss.Custom("hcm-measure-prose", "65ch"), gwccss.Custom("hcm-tracking-caps", ".05em"))
	declareGlobal(":where(.app-shell,.jn-embedded)", gwccss.Raw("font-family", "var(--hcm-font-sans)"), gwccss.Raw("font-size", "var(--hcm-type-body)"), gwccss.Raw("line-height", "var(--hcm-leading-body)"), gwccss.Raw("font-size-adjust", ".52"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(h1,h2,h3,h4,h5,h6)", gwccss.Raw("font-family", "var(--hcm-font-sans)"), gwccss.Raw("font-weight", "700"), gwccss.Raw("text-wrap", "balance"), gwccss.Raw("overflow-wrap", "anywhere"), gwccss.Raw("max-inline-size", "var(--hcm-measure-readable)"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is([data-type-role=\"display\"],.type-display)", gwccss.Raw("font-size", "var(--hcm-type-display)"), gwccss.Raw("line-height", "var(--hcm-leading-display)"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.page-head h1,[data-type-role=\"page-title\"],.type-page-title)", gwccss.Raw("font-size", "var(--hcm-type-page-title)"), gwccss.Raw("line-height", "var(--hcm-leading-display)"))
	// A bare heading element gets the scale as a zero-specificity default; the
	// explicit role attribute and class keep class weight. Joined in one :is()
	// the element took the class arm's (0,1,0), outranked every element-level
	// heading rule -- the journey renderer's 1.25rem h2 among them -- and put a
	// 26px section heading two pixels under a 28px page title.
	declareGlobal(":where(.app-shell,.jn-embedded) :where(h2)", gwccss.Raw("font-size", "var(--hcm-type-section)"), gwccss.Raw("line-height", "var(--hcm-leading-heading)"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is([data-type-role=\"section\"],.type-section)", gwccss.Raw("font-size", "var(--hcm-type-section)"), gwccss.Raw("line-height", "var(--hcm-leading-heading)"))
	// A label-level heading sits a weight below the headings above it; at the
	// shared 700 an h3 in a card was bolder than the 600 h2 it sat under.
	// The label size is one fixed step (1rem): the fluid clamp it replaces
	// landed between steps at most widths (15.48px at 1360px).
	declareGlobal(":where(.app-shell,.jn-embedded) :where(h3,h4,h5,h6)", gwccss.Raw("font-size", "var(--hcm-type-label)"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(h3,h4,h5,h6)", gwccss.Raw("font-weight", "600"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is([data-type-role=\"label\"],.type-label)", gwccss.Raw("font-size", "var(--hcm-type-label)"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"))
	// The body role is declared twice on purpose, because it is two
	// different things wearing one name.
	//
	// Asking for the body role -- .prose, .provenance, .body-copy,
	// data-type-role="body" -- is a choice a component made, and it keeps
	// the specificity that choice deserves.
	//
	// A bare p, li, dd, dt or blockquote is not a choice. It is the
	// default for text nobody said anything else about. Declared together
	// with the classes above it was not behaving like one: :where() scores
	// nothing, but :is() takes the specificity of its most specific
	// argument, and `.prose p` made the whole rule (0,1,1) -- more than the
	// (0,1,0) of a component naming its own size. Sixty-one such rules
	// across this product were losing, so a nav section label asking for
	// .67rem, an eyebrow asking for .75rem and a hero pay figure asking for
	// 1.5rem all came out at body size, and page after page rendered as one
	// flat column of 16px text.
	//
	// UXLIVE-024 met the same rule from the layout side, where the measure
	// capped structural list items, and released those items by name. That
	// patch stays and is still tested; splitting the rule is the general
	// form of the same correction.
	//
	// :where() on the element list makes the default score zero, so any
	// component class beats it and nothing needs !important to be heard.
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.prose,.prose p,.prose li,.provenance,.body-copy,[data-type-role=\"body\"],.type-body)", gwccss.Raw("font-size", "var(--hcm-type-body)"), gwccss.Raw("line-height", "var(--hcm-leading-body)"), gwccss.Raw("max-inline-size", "var(--hcm-measure-prose)"), gwccss.Raw("overflow-wrap", "anywhere"))
	declareGlobal(":where(.app-shell,.jn-embedded) :where(p,li,dd,dt,blockquote)", gwccss.Raw("font-size", "var(--hcm-type-body)"), gwccss.Raw("line-height", "var(--hcm-leading-body)"), gwccss.Raw("max-inline-size", "var(--hcm-measure-prose)"), gwccss.Raw("overflow-wrap", "anywhere"))
	// <strong> and <b> had no rule either, so they fell to the browser
	// default of `bolder` -- a step relative to whatever they sit in. Inside
	// a 600 label that resolves to 900, which is how employee names on the
	// People table came out heavier than the page title. Emphasis is a fixed
	// step on the weight scale, not a nudge away from its surroundings.
	declareGlobal(":where(.app-shell,.jn-embedded) :where(strong,b)", gwccss.Raw("font-weight", "600"))
	// <small> had no rule anywhere, so it fell to the browser default of
	// 0.8em -- relative to whatever it sits in. Nested inside text that is
	// already below body size it compounds: worker numbers on the People
	// page rendered at 10.83px. A size that shrinks again every time it is
	// nested is not a size, so it is given the helper role instead, whose
	// clamp has a floor. Zero specificity, so any component that wants its
	// own small text still gets it.
	declareGlobal(":where(.app-shell,.jn-embedded) :where(small)", gwccss.Raw("font-size", "var(--hcm-type-helper)"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"))
	// Labels, tables and code: the bare element is a zero-specificity default
	// and the role classes keep class weight. In one :is() the element took
	// the class arms' (0,1,0) and beat every component's own rule by source
	// order -- the journey form's .jn-label (0.8125rem) rendered at 16px.
	declareGlobal(":where(.app-shell,.jn-embedded) :where(label)", gwccss.Raw("font-size", "var(--hcm-type-label)"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.label,[data-type-role=\"label\"],.type-label)", gwccss.Raw("font-size", "var(--hcm-type-label)"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.helper,.field .error,[data-type-role=\"helper\"],.type-helper)", gwccss.Raw("font-size", "var(--hcm-type-helper)"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"), gwccss.Raw("overflow-wrap", "anywhere"))
	declareGlobal(":where(.app-shell,.jn-embedded) :where(table)", gwccss.Raw("font-size", "var(--hcm-type-table)"), gwccss.Raw("line-height", "var(--hcm-leading-body)"), gwccss.Raw("max-inline-size", "100%"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is([data-type-role=\"table\"],.type-table,.table-scroll,.data-table,.data-table-scroll)", gwccss.Raw("font-size", "var(--hcm-type-table)"), gwccss.Raw("line-height", "var(--hcm-leading-body)"), gwccss.Raw("max-inline-size", "100%"))
	declareGlobal(":where(.app-shell,.jn-embedded) :where(code,kbd,pre)", gwccss.Raw("font-family", "var(--hcm-font-mono)"), gwccss.Raw("font-size", "var(--hcm-type-code)"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"), gwccss.Raw("overflow-wrap", "anywhere"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is([data-type-role=\"code\"],.type-code)", gwccss.Raw("font-family", "var(--hcm-font-mono)"), gwccss.Raw("font-size", "var(--hcm-type-code)"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"), gwccss.Raw("overflow-wrap", "anywhere"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.eyebrow,.overline,.metadata,.meta)", gwccss.Raw("font-size", "var(--hcm-type-helper)"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"), gwccss.Raw("text-transform", "none"), gwccss.Raw("letter-spacing", ".02em"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.page-head,.page-title,.section-heading,.card,.surface)", gwccss.Raw("min-inline-size", "0"), gwccss.Raw("max-inline-size", "100%"))
	declareGlobal(":where(.app-shell) .sidebar .nav-group-summary>.nav-label", gwccss.Raw("flex", "1 1 auto"), gwccss.Raw("min-inline-size", "0"), gwccss.Raw("max-inline-size", "none"), gwccss.Raw("overflow", "visible"), gwccss.Raw("white-space", "normal"), gwccss.Raw("overflow-wrap", "normal"), gwccss.Raw("word-break", "normal"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"))
}
