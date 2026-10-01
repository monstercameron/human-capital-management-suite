package productui

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

// clockRaws turns property/value pairs into typed raw declarations. The clock
// surfaces use logical properties and var() fallbacks that have no typed
// constructor, so they share this one adapter instead of repeating gwccss.Raw.
func clockRaws(pairs ...string) []any {
	if len(pairs)%2 != 0 {
		// A dropped value would shift every later declaration; fail loudly in
		// the first test that builds the stylesheet instead of styling wrongly.
		panic("productui: clockRaws needs property/value pairs")
	}
	rules := make([]any, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		rules = append(rules, gwccss.Raw(pairs[i], pairs[i+1]))
	}
	return rules
}

const (
	clockGap   = "calc(var(--hcm-space-3) * var(--hcm-density))"
	clockGapLg = "calc(var(--hcm-space-4) * var(--hcm-density))"
	clockGapSm = "calc(var(--hcm-space-2) * var(--hcm-density))"
)

// clockRawStyles holds the busy spinner keyframes the typed builder has no
// constructor for, and stops the spin for people who asked for less motion.
const clockRawStyles = "@keyframes clock-spin{to{transform:rotate(360deg)}}@media (prefers-reduced-motion:reduce){.clock-actions .clock-primary:disabled::before{animation:none}}"

// clockPageStylesheet styles the worker clock, the missing-punch flow and the
// shared time-surface notices. Everything resolves through the product tokens
// so light, dark, high-contrast and customer palettes apply unchanged, and
// spacing uses logical properties so Arabic mirrors without a second sheet.
func clockPageStylesheet() string {
	return buildTypedSheet(declareClockPageStyles) + kioskRawStyles + clockRawStyles
}

func declareClockPageStyles() {
	declareGlobal(".clock-page", clockRaws("display", "grid", "gap", clockGapLg, "min-width", "0", "max-width", "72rem")...)
	declareGlobal(".clock-page-head", clockRaws("margin-block-end", "0")...)
	declareGlobal(".clock-layout", clockRaws("display", "grid", "gap", clockGapLg, "align-items", "start", "grid-template-columns", "minmax(0,1.75fr) minmax(15rem,1fr)")...)
	declareGlobal(".clock-layout", mediaRule(gwccss.MaxW(900), clockRaws("grid-template-columns", "minmax(0,1fr)")...))

	// The "now" card: a status band on the inline-start edge carries the phase
	// colour; the words carry the meaning, the colour only reinforces it.
	declareGlobal(".clock-now", clockRaws(
		"--clock-phase", "var(--muted)", "--clock-phase-surface", "var(--surface-subtle)",
		"display", "grid", "gap", clockGap, "min-width", "0",
		"padding", "calc(var(--hcm-space-4) * var(--hcm-density) * 1.25)",
		"border-inline-start", "8px solid var(--clock-phase)",
	)...)
	declareGlobal(`.clock-now[data-phase="in"]`, clockRaws("--clock-phase", "var(--hcm-color-success)", "--clock-phase-surface", "var(--hcm-color-success-surface)")...)
	declareGlobal(`.clock-now[data-phase="break"]`, clockRaws("--clock-phase", "var(--hcm-color-warning)", "--clock-phase-surface", "var(--hcm-color-warning-surface)")...)
	declareGlobal(".clock-now", mediaRule(gwccss.MaxW(600), clockRaws("padding", "calc(var(--hcm-space-3) * var(--hcm-density))", "border-inline-start-width", "6px")...))

	declareGlobal(".clock-state", clockRaws(
		"display", "inline-flex", "align-items", "center", "gap", clockGapSm, "justify-self", "start",
		"padding", "calc(var(--hcm-space-2) * var(--hcm-density)) calc(var(--hcm-space-3) * var(--hcm-density))",
		"border-radius", "999px", "background", "var(--clock-phase-surface)", "color", "var(--clock-phase)", "border", "1px solid var(--line)",
		"max-width", "100%",
	)...)
	declareGlobal(".clock-state-dot", clockRaws("flex", "none", "width", "0.85rem", "height", "0.85rem", "border-radius", "50%", "background", "currentColor")...)
	declareGlobal(`.clock-now[data-phase="in"] .clock-state-dot`, clockRaws("box-shadow", "0 0 0 4px var(--clock-phase-surface),0 0 0 6px var(--clock-phase)")...)
	declareGlobal(`.clock-now[data-phase="break"] .clock-state-dot`, clockRaws("border-radius", "2px")...)
	declareGlobal(".clock-state-label", clockRaws(
		"margin", "0", "font-size", "clamp(1.4rem,3.2vw,2rem)", "line-height", "1.15", "font-weight", "750", "letter-spacing", "-0.01em",
		"overflow-wrap", "anywhere",
	)...)
	declareGlobal(".clock-state-since", clockRaws("margin", "0", "color", "var(--ink)", "font-size", "var(--hcm-font-size-body)", "font-weight", "550")...)
	declareGlobal(".clock-worker", clockRaws("margin", "0", "color", "var(--muted)", "font-size", "var(--hcm-font-size-body)", "font-weight", "550")...)

	declareGlobal(".clock-notice", clockRaws(
		"margin", "0", "display", "flex", "flex-wrap", "wrap", "align-items", "baseline", "gap", "0.25rem 0.75rem",
		"padding", "calc(var(--hcm-space-2) * var(--hcm-density)) calc(var(--hcm-space-3) * var(--hcm-density))",
		"border-radius", "var(--hcm-radius-control,8px)", "border-inline-start", "4px solid currentColor",
		"font-size", "var(--hcm-font-size-body)", "font-weight", "550",
	)...)
	declareGlobal(".clock-notice-error", clockRaws("background", "var(--hcm-color-danger-surface)", "color", "var(--hcm-color-danger)")...)
	declareGlobal(".clock-notice-success", clockRaws("background", "var(--hcm-color-success-surface)", "color", "var(--hcm-color-success)")...)
	declareGlobal(".clock-notice-busy", clockRaws("background", "var(--hcm-color-info-surface)", "color", "var(--hcm-color-info)")...)
	declareGlobal(".clock-receipt", clockRaws("font-size", "var(--hcm-font-size-small)", "font-weight", "500", "font-variant-numeric", "tabular-nums", "overflow-wrap", "anywhere")...)

	// Actions: one primary, sized for a gloved thumb, then the quieter ones.
	declareGlobal(".clock-actions", clockRaws("display", "grid", "gap", clockGapSm)...)
	declareGlobal(".clock-actions>.button,.clock-actions>.clock-primary", clockRaws(
		"display", "inline-flex", "align-items", "center", "justify-content", "center", "text-align", "center", "text-decoration", "none",
		"box-sizing", "border-box", "width", "100%",
	)...)
	declareGlobal(".clock-actions .clock-primary", clockRaws("min-height", "4.5rem", "font-size", "1.35rem", "font-weight", "700", "border-radius", "var(--hcm-radius-surface,12px)")...)
	declareGlobal(".clock-actions .clock-secondary", clockRaws("min-height", "3.25rem", "font-size", "1.05rem")...)
	// Stopping the clock is the opposite of starting it, so it must not look the
	// same: clock in keeps the brand action colour, clock out is slate.
	declareGlobal(`.clock-now[data-phase="in"] .clock-actions>.clock-primary`, clockRaws("background", "var(--surface)", "border", "3px solid var(--ink)", "color", "var(--ink)")...)
	declareGlobal(`.clock-now[data-phase="in"] .clock-actions>.clock-primary:hover`, clockRaws("background", "var(--surface-subtle)")...)
	declareGlobal(".clock-actions .clock-primary:disabled::before", clockRaws("content", `""`, "display", "inline-block", "width", "1.1rem", "height", "1.1rem", "margin-inline-end", "0.75rem", "border", "3px solid currentColor", "border-inline-end-color", "transparent", "border-radius", "50%", "animation", "clock-spin 0.9s linear infinite")...)
	declareGlobal(".clock-actions .clock-primary:disabled", clockRaws("opacity", "0.7", "cursor", "progress")...)
	declareGlobal(".clock-hint", clockRaws("margin", "0", "color", "var(--muted)", "font-size", "var(--hcm-font-size-small)")...)

	declareGlobal(".clock-confirm", clockRaws(
		"display", "grid", "gap", clockGapSm, "padding", clockGap, "border", "2px solid var(--hcm-color-danger)",
		"border-radius", "var(--hcm-radius-surface,12px)", "background", "var(--hcm-color-danger-surface)",
	)...)
	declareGlobal(".clock-confirm-title", clockRaws("margin", "0", "font-size", "1.25rem", "font-weight", "700", "color", "var(--ink)")...)
	declareGlobal(".clock-confirm-body", clockRaws("margin", "0", "color", "var(--ink)", "font-size", "var(--hcm-font-size-body)")...)
	declareGlobal(".clock-confirm-actions", clockRaws("display", "grid", "gap", clockGapSm, "grid-template-columns", "repeat(2,minmax(0,1fr))")...)
	declareGlobal(".clock-confirm-actions", mediaRule(gwccss.MaxW(480), clockRaws("grid-template-columns", "minmax(0,1fr)")...))
	declareGlobal(".clock-confirm-actions .button", clockRaws(
		"display", "inline-flex", "align-items", "center", "justify-content", "center", "text-decoration", "none",
		"box-sizing", "border-box", "min-height", "3.5rem", "font-size", "1.05rem", "font-weight", "700",
	)...)

	// Support column.
	declareGlobal(".clock-side", clockRaws("display", "grid", "gap", clockGapLg, "min-width", "0", "padding", "calc(var(--hcm-space-4) * var(--hcm-density))")...)
	declareGlobal(".clock-facts", clockRaws("display", "grid", "gap", clockGap, "margin", "0")...)
	declareGlobal(".clock-page-fact", clockRaws("display", "grid", "gap", "0.15rem")...)
	declareGlobal(".clock-page-fact-label", clockRaws("color", "var(--muted)", "font-size", "var(--hcm-font-size-small)", "font-weight", "500")...)
	declareGlobal(".clock-page-fact-value", clockRaws("margin", "0", "font-size", "1.1rem", "font-weight", "650", "overflow-wrap", "anywhere")...)
	declareGlobal(".clock-side-title", clockRaws("margin", "0 0 0.5rem", "font-size", "var(--hcm-font-size-body)", "font-weight", "650")...)
	declareGlobal(".clock-link-list", clockRaws("display", "grid", "gap", clockGapSm)...)
	declareGlobal(".clock-link", clockRaws(
		"display", "flex", "align-items", "center", "justify-content", "space-between", "gap", clockGapSm, "box-sizing", "border-box",
		"min-height", "var(--hcm-control-height,40px)", "padding", "0.5rem 0.9rem", "border", "1px solid var(--control-border,var(--line))",
		"border-radius", "var(--hcm-radius-control,8px)", "background", "var(--surface)", "color", "var(--ink)", "font-weight", "600", "text-decoration", "none",
	)...)
	declareGlobal(".clock-link::after", clockRaws("content", `"›"`, "color", "var(--muted)", "font-size", "1.2rem", "line-height", "1")...)
	declareGlobal("[dir=rtl] .clock-link::after", clockRaws("content", `"‹"`)...)
	declareGlobal(".clock-link:hover", clockRaws("border-color", "var(--hcm-hover-border,var(--accent))", "background", "var(--hcm-hover-surface,var(--surface-subtle))")...)

	declareGlobal(".clock-kiosk", clockRaws(
		"display", "flex", "flex-wrap", "wrap", "align-items", "center", "justify-content", "space-between", "gap", clockGap,
		"padding", "calc(var(--hcm-space-3) * var(--hcm-density)) calc(var(--hcm-space-4) * var(--hcm-density))",
	)...)
	declareGlobal(".clock-kiosk-copy", clockRaws("flex", "1 1 20rem", "min-width", "0")...)
	declareGlobal(".clock-kiosk-copy h2", clockRaws("margin", "0 0 0.25rem", "font-size", "var(--hcm-font-size-body)", "font-weight", "650")...)
	declareGlobal(".clock-kiosk-copy p", clockRaws("margin", "0")...)
	declareGlobal(".clock-kiosk-actions", clockRaws("display", "flex", "flex-wrap", "wrap", "gap", clockGapSm)...)
	declareGlobal(".clock-kiosk-actions .button", clockRaws("display", "inline-flex", "align-items", "center", "text-decoration", "none")...)
	declareGlobal(".clock-kiosk", mediaRule(gwccss.MaxW(600), clockRaws("align-items", "stretch")...))
	declareGlobal(".clock-kiosk-actions", mediaRule(gwccss.MaxW(600), clockRaws("display", "grid")...))

	declareGlobal(".clock-unavailable", clockRaws(
		"display", "flex", "gap", clockGapLg, "align-items", "flex-start", "max-width", "44rem",
		"padding", "calc(var(--hcm-space-4) * var(--hcm-density))",
	)...)
	declareGlobal(".clock-unavailable h2", clockRaws("margin", "0 0 0.35rem", "font-size", "1.15rem", "font-weight", "650")...)
	declareGlobal(".clock-unavailable p", clockRaws("margin", "0")...)
	declareGlobal(".clock-unavailable-mark", clockRaws(
		"flex", "none", "display", "grid", "place-items", "center", "width", "3rem", "height", "3rem", "border-radius", "50%",
		"background", "var(--surface-subtle)", "color", "var(--muted)",
	)...)
	declareGlobal(".clock-unavailable-icon", clockRaws("width", "1.5rem", "height", "1.5rem")...)

	declareMissingPunchStyles()
	declareKioskStyles()
	declareTimeSurfaceStyles()
}
