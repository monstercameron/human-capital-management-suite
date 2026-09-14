package productui

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

// uipolish001TypographyStylesheet is the production typography contract.
// Components opt into named roles; no page is expected to choose a pixel size.
func uipolish001TypographyStylesheet() string {
	return buildTypedSheet(declareUIPolish001Typography)
}

func declareUIPolish001Typography() {
	declareGlobal(":root", gwccss.Custom("hcm-type-display", "clamp(2rem,1.5rem + 2vw,3.5rem)"), gwccss.Custom("hcm-type-page-title", "clamp(1.75rem,var(--hcm-font-size-heading,1.35rem),2.5rem)"), gwccss.Custom("hcm-type-section", "clamp(1.25rem,1.1rem + .6vw,1.75rem)"), gwccss.Custom("hcm-type-body", "var(--hcm-font-size-body,1rem)"), gwccss.Custom("hcm-type-label", "clamp(.875rem,.84rem + .15vw,1rem)"), gwccss.Custom("hcm-type-helper", "clamp(.875rem,var(--hcm-font-size-small,.82rem),1rem)"), gwccss.Custom("hcm-type-table", "clamp(.875rem,var(--hcm-font-size-small,.82rem),1rem)"), gwccss.Custom("hcm-type-code", "clamp(.875rem,var(--hcm-font-size-small,.82rem),1rem)"), gwccss.Custom("hcm-leading-display", "1.1"), gwccss.Custom("hcm-leading-heading", "1.25"), gwccss.Custom("hcm-leading-body", "var(--hcm-line-height,1.5)"), gwccss.Custom("hcm-leading-tight", "1.35"), gwccss.Custom("hcm-measure-readable", "70ch"), gwccss.Custom("hcm-measure-prose", "65ch"))
	declareGlobal(":where(.app-shell,.jn-embedded)", gwccss.Raw("font-family", "var(--hcm-font-sans)"), gwccss.Raw("font-size", "var(--hcm-type-body)"), gwccss.Raw("line-height", "var(--hcm-leading-body)"), gwccss.Raw("font-size-adjust", ".52"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(h1,h2,h3,h4,h5,h6)", gwccss.Raw("font-family", "var(--hcm-font-sans)"), gwccss.Raw("font-weight", "700"), gwccss.Raw("text-wrap", "balance"), gwccss.Raw("overflow-wrap", "anywhere"), gwccss.Raw("max-inline-size", "var(--hcm-measure-readable)"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is([data-type-role=\"display\"],.type-display)", gwccss.Raw("font-size", "var(--hcm-type-display)"), gwccss.Raw("line-height", "var(--hcm-leading-display)"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.page-head h1,[data-type-role=\"page-title\"],.type-page-title)", gwccss.Raw("font-size", "var(--hcm-type-page-title)"), gwccss.Raw("line-height", "var(--hcm-leading-display)"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(h2,[data-type-role=\"section\"],.type-section)", gwccss.Raw("font-size", "var(--hcm-type-section)"), gwccss.Raw("line-height", "var(--hcm-leading-heading)"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(h3,h4,h5,h6,[data-type-role=\"label\"],.type-label)", gwccss.Raw("font-size", "var(--hcm-type-label)"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.prose,.prose p,.prose li,.provenance,.body-copy,p,li,dd,dt,blockquote,[data-type-role=\"body\"],.type-body)", gwccss.Raw("font-size", "var(--hcm-type-body)"), gwccss.Raw("line-height", "var(--hcm-leading-body)"), gwccss.Raw("max-inline-size", "var(--hcm-measure-prose)"), gwccss.Raw("overflow-wrap", "anywhere"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(label,.label,[data-type-role=\"label\"],.type-label)", gwccss.Raw("font-size", "var(--hcm-type-label)"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.helper,.field .error,[data-type-role=\"helper\"],.type-helper)", gwccss.Raw("font-size", "var(--hcm-type-helper)"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"), gwccss.Raw("overflow-wrap", "anywhere"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(table,[data-type-role=\"table\"],.type-table,.table-scroll,.data-table,.data-table-scroll)", gwccss.Raw("font-size", "var(--hcm-type-table)"), gwccss.Raw("line-height", "var(--hcm-leading-body)"), gwccss.Raw("max-inline-size", "100%"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(code,kbd,pre,[data-type-role=\"code\"],.type-code)", gwccss.Raw("font-family", "var(--hcm-font-mono)"), gwccss.Raw("font-size", "var(--hcm-type-code)"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"), gwccss.Raw("overflow-wrap", "anywhere"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.eyebrow,.overline,.metadata,.meta)", gwccss.Raw("font-size", "var(--hcm-type-helper)"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"), gwccss.Raw("text-transform", "none"), gwccss.Raw("letter-spacing", ".02em"))
	declareGlobal(":where(.app-shell,.jn-embedded) :is(.page-head,.page-title,.section-heading,.card,.surface)", gwccss.Raw("min-inline-size", "0"), gwccss.Raw("max-inline-size", "100%"))
	declareGlobal(":where(.app-shell) .sidebar .nav-group-summary>.nav-label", gwccss.Raw("flex", "1 1 auto"), gwccss.Raw("min-inline-size", "0"), gwccss.Raw("max-inline-size", "none"), gwccss.Raw("overflow", "visible"), gwccss.Raw("white-space", "normal"), gwccss.Raw("overflow-wrap", "normal"), gwccss.Raw("word-break", "normal"), gwccss.Raw("line-height", "var(--hcm-leading-tight)"))
}
