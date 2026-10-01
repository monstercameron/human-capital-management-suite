package productui

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

// declareMissingPunchStyles lays out the correction request and the
// supervisor review queue. It shares notices and tokens with the clock page.
func declareMissingPunchStyles() {
	declareGlobal(".punch-page", clockRaws("display", "grid", "gap", clockGapLg, "min-width", "0", "max-width", "56rem")...)
	declareGlobal(".punch-head", clockRaws("margin-block-end", "0")...)
	declareGlobal(".punch-request,.punch-reviews", clockRaws("display", "grid", "gap", clockGapLg, "min-width", "0", "padding", "calc(var(--hcm-space-4) * var(--hcm-density) * 1.25)")...)
	declareGlobal(".punch-request,.punch-reviews", mediaRule(gwccss.MaxW(600), clockRaws("padding", "calc(var(--hcm-space-3) * var(--hcm-density))")...))
	declareGlobal(".punch-request h2,.punch-reviews h2", clockRaws("margin", "0", "font-size", "1.25rem", "font-weight", "700", "letter-spacing", "-0.01em")...)

	declareGlobal(".punch-record-block", clockRaws("display", "grid", "gap", clockGapSm, "padding", clockGap, "border-radius", "var(--hcm-radius-control,8px)", "background", "var(--surface-subtle)")...)
	declareGlobal(".punch-record-title", clockRaws("margin", "0", "color", "var(--muted)", "font-size", "var(--hcm-font-size-small)", "font-weight", "600")...)
	declareGlobal(".punch-record", clockRaws("display", "grid", "gap", clockGapSm, "margin", "0")...)
	declareGlobal(".punch-record>div", clockRaws("display", "grid", "grid-template-columns", "minmax(7rem,10rem) minmax(0,1fr)", "gap", "0.25rem 1rem", "align-items", "baseline")...)
	declareGlobal(".punch-record>div", mediaRule(gwccss.MaxW(520), clockRaws("grid-template-columns", "minmax(0,1fr)", "gap", "0.1rem")...))
	declareGlobal(".punch-record dt", clockRaws("color", "var(--muted)", "font-size", "var(--hcm-font-size-small)")...)
	declareGlobal(".punch-record dd", clockRaws("margin", "0", "font-weight", "600", "overflow-wrap", "anywhere")...)

	declareGlobal(".punch-form", clockRaws("display", "grid", "gap", clockGapLg, "max-width", "40rem")...)
	declareGlobal(".punch-field", clockRaws("display", "grid", "gap", "0.35rem")...)
	declareGlobal(".punch-field label", clockRaws("font-weight", "650", "font-size", "var(--hcm-font-size-body)", "color", "var(--ink)")...)
	declareGlobal(".punch-field input,.punch-field textarea", clockRaws(
		"box-sizing", "border-box", "width", "100%", "min-height", "var(--hcm-control-height,44px)", "padding", "0.6rem 0.75rem",
		"border", "1px solid var(--control-border,var(--line))", "border-radius", "var(--hcm-radius-control,8px)",
		"background", "var(--surface)", "color", "var(--ink)", "font", "inherit",
	)...)
	declareGlobal(".punch-field textarea", clockRaws("resize", "vertical", "line-height", "1.45")...)
	declareGlobal(".punch-help", clockRaws("margin", "0", "color", "var(--muted)", "font-size", "var(--hcm-font-size-small)")...)
	declareGlobal(".punch-error", clockRaws("margin", "0", "padding", "0.6rem 0.8rem", "border-radius", "var(--hcm-radius-control,8px)", "border-inline-start", "4px solid var(--hcm-color-danger)", "background", "var(--hcm-color-danger-surface)", "color", "var(--hcm-color-danger)", "font-weight", "600")...)
	declareGlobal(".punch-error-empty", clockRaws("display", "none")...)
	declareGlobal(".punch-submit", clockRaws("justify-self", "start", "min-height", "3rem", "padding-inline", "1.5rem", "font-size", "1.05rem", "font-weight", "700")...)
	declareGlobal(".punch-submit", mediaRule(gwccss.MaxW(600), clockRaws("justify-self", "stretch")...))

	declareGlobal(".punch-sent", clockRaws("border-inline-start", "8px solid var(--hcm-color-success)")...)
	declareGlobal(".punch-reference", clockRaws("margin", "0", "font-weight", "650", "font-variant-numeric", "tabular-nums", "overflow-wrap", "anywhere")...)
	declareGlobal(".punch-trace", clockRaws("justify-self", "start", "font-weight", "600")...)

	declareGlobal(".punch-review-list", clockRaws("display", "grid", "gap", clockGapLg)...)
	declareGlobal(".punch-review", clockRaws(
		"display", "grid", "gap", clockGap, "padding", "calc(var(--hcm-space-4) * var(--hcm-density))", "min-width", "0",
		"border", "1px solid var(--line)", "border-inline-start", "6px solid var(--hcm-color-warning)", "border-radius", "var(--hcm-radius-surface,12px)", "background", "var(--surface)",
	)...)
	declareGlobal(".punch-review-decided", clockRaws("border-inline-start-color", "var(--line)")...)
	declareGlobal(".punch-review-head", clockRaws("display", "flex", "flex-wrap", "wrap", "align-items", "baseline", "gap", "0.25rem 0.75rem")...)
	declareGlobal(".punch-review-head h3", clockRaws("margin", "0", "font-size", "1.15rem", "font-weight", "700")...)
	declareGlobal(".punch-review-head p", clockRaws("margin", "0")...)
	declareGlobal(".punch-ask", clockRaws("margin", "0", "font-size", "1.1rem", "font-weight", "500", "line-height", "1.5")...)
	declareGlobal(".punch-ask strong", clockRaws("font-weight", "700")...)
	declareGlobal(".punch-decision", clockRaws("display", "flex", "flex-wrap", "wrap", "gap", clockGapSm)...)
	declareGlobal(".punch-decision .button", clockRaws("min-height", "3rem", "padding-inline", "1.25rem", "font-weight", "700")...)
	declareGlobal(".punch-decision", mediaRule(gwccss.MaxW(520), clockRaws("display", "grid")...))
	declareGlobal(".punch-empty", clockRaws("margin", "0")...)
}
