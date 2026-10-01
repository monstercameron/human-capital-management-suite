package productui

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

// kioskRawStyles holds the one construct the typed builder has no constructor
// for: the return-to-start countdown keyframes and their reduced-motion
// override.
const kioskRawStyles = "@keyframes kiosk-return{from{transform:scaleX(1)}to{transform:scaleX(0)}}@media (prefers-reduced-motion:reduce){.kiosk-return-bar::after{animation:none}}"

// declareKioskStyles sizes the shared tablet for a hand that may be gloved and
// a screen that may be in sun: every control is at least 4.5rem tall, the
// state is a word plus a colour, and the layout is one centred column that
// works in landscape and portrait.
func declareKioskStyles() {
	declareGlobal(".timeclock-kiosk", clockRaws(
		"box-sizing", "border-box", "height", "100vh", "overflow-y", "auto", "display", "grid", "grid-template-rows", "auto 1fr", "gap", "clamp(1rem,3vw,2rem)",
		"padding", "clamp(1rem,3vw,2.5rem)", "background", "var(--canvas)", "color", "var(--ink)", "font-size", "1.125rem",
	)...)
	declareGlobal(".timeclock-kiosk", mediaRule(gwccss.RawMedia("(min-height:1px)"), clockRaws("height", "100dvh")...))
	declareGlobal(".timeclock-kiosk>*", clockRaws("min-width", "0")...)

	declareGlobal(".kiosk-header", clockRaws("display", "flex", "flex-wrap", "wrap", "align-items", "center", "justify-content", "space-between", "gap", clockGapSm, "width", "100%", "max-width", "56rem", "margin-inline", "auto")...)
	declareGlobal(".kiosk-identity", clockRaws("display", "grid", "gap", "0.1rem")...)
	declareGlobal(".kiosk-title", clockRaws("margin", "0", "font-size", "clamp(1.5rem,3.5vw,2rem)", "font-weight", "750", "letter-spacing", "-0.01em")...)
	declareGlobal(".kiosk-site", clockRaws("margin", "0", "color", "var(--muted)", "font-size", "1.125rem", "font-weight", "550")...)
	declareGlobal(".kiosk-connection", clockRaws(
		"display", "inline-flex", "align-items", "center", "gap", "0.6rem", "padding", "0.5rem 1rem", "border", "1px solid var(--line)",
		"border-radius", "999px", "background", "var(--surface)", "font-weight", "600", "--kiosk-tone", "var(--hcm-color-success)",
	)...)
	declareGlobal(`.kiosk-connection[data-state="offline"]`, clockRaws("--kiosk-tone", "var(--hcm-color-warning)")...)
	declareGlobal(".kiosk-dot", clockRaws("flex", "none", "width", "0.8rem", "height", "0.8rem", "border-radius", "50%", "background", "var(--kiosk-tone,currentColor)")...)
	declareGlobal(".kiosk-connection-label", clockRaws("color", "var(--kiosk-tone)")...)
	declareGlobal(".kiosk-queue", clockRaws("color", "var(--muted)", "font-weight", "500", "font-size", "1rem", "padding-inline-start", "0.6rem", "border-inline-start", "1px solid var(--line)")...)

	declareGlobal(".kiosk-stage", clockRaws("display", "grid", "gap", clockGapSm, "align-content", "center", "justify-items", "center", "min-width", "0")...)
	declareGlobal(".kiosk-panel,.kiosk-confirm", clockRaws("width", "min(100%,36rem)")...)
	declareGlobal(".kiosk-panel", clockRaws(
		"flex", "none", "box-sizing", "border-box", "display", "grid", "gap", "calc(var(--hcm-space-3) * var(--hcm-density))", "padding", "clamp(1.25rem,4vw,2.25rem)",
		"border", "1px solid var(--line)", "border-radius", "1.25rem", "background", "var(--surface)", "box-shadow", "var(--hcm-shadow-raised,0 8px 24px rgba(0,0,0,.08))",
	)...)
	declareGlobal(".kiosk-panel h2", clockRaws("margin", "0", "text-align", "center", "font-size", "clamp(1.6rem,4.5vw,2.25rem)", "line-height", "1.15", "font-weight", "750", "letter-spacing", "-0.01em")...)
	declareGlobal(".kiosk-help", clockRaws("margin", "0", "text-align", "center", "color", "var(--muted)", "font-size", "1.25rem")...)
	declareGlobal(".kiosk-label", clockRaws("font-weight", "650")...)

	declareGlobal(".kiosk-panel .button,.kiosk-submit", clockRaws(
		"box-sizing", "border-box", "display", "inline-flex", "align-items", "center", "justify-content", "center", "width", "100%",
		"min-height", "4.5rem", "font-size", "1.4rem", "font-weight", "700", "border-radius", "1rem", "text-align", "center",
	)...)
	declareGlobal(".kiosk-panel input:not([type=checkbox])", clockRaws(
		"box-sizing", "border-box", "width", "100%", "min-height", "3.5rem", "padding", "0.6rem 0.9rem", "font", "inherit", "font-size", "1.2rem",
		"border", "2px solid var(--control-border,var(--line))", "border-radius", "0.9rem", "background", "var(--surface)", "color", "var(--ink)",
	)...)
	declareGlobal(".kiosk-panel input.kiosk-credential", clockRaws(
		"min-height", "4.5rem", "font-size", "2.25rem", "font-weight", "700", "text-align", "center", "letter-spacing", "0.35em",
		"font-variant-numeric", "tabular-nums", "-webkit-text-security", "disc", "text-security", "disc",
	)...)
	declareGlobal(".kiosk-keypad", clockRaws("display", "grid", "grid-template-columns", "repeat(3,minmax(0,1fr))", "gap", "0.75rem")...)
	declareGlobal(".kiosk-key", clockRaws(
		"box-sizing", "border-box", "min-height", "4.75rem", "padding", "0", "border", "1px solid var(--control-border,var(--line))", "border-radius", "1rem",
		"background", "var(--surface-subtle)", "color", "var(--ink)", "font", "inherit", "font-size", "1.9rem", "font-weight", "650", "cursor", "pointer",
		"font-variant-numeric", "tabular-nums", "touch-action", "manipulation", "-webkit-tap-highlight-color", "transparent",
	)...)
	declareGlobal(".kiosk-key-quiet", clockRaws("font-size", "1.3rem", "font-weight", "650", "color", "var(--ink)")...)
	declareGlobal(".kiosk-key:hover", clockRaws("background", "var(--hcm-hover-surface,var(--soft))", "border-color", "var(--hcm-hover-border,var(--accent))")...)
	declareGlobal(".kiosk-key:active", clockRaws("background", "var(--soft)", "transform", "scale(0.97)")...)
	declareGlobal(".kiosk-keypad", mediaRule(gwccss.RawMedia("(max-height:640px)"), clockRaws("gap", "0.5rem")...))
	declareGlobal(".kiosk-key", mediaRule(gwccss.RawMedia("(max-height:640px)"), clockRaws("min-height", "3.75rem")...))

	// Landscape tablets split the identify panel so the whole flow, keypad and
	// continue button included, fits one screen without scrolling.
	wide := gwccss.RawMedia("(min-width:800px) and (orientation:landscape)")
	declareGlobal(".kiosk-identify", mediaRule(wide, clockRaws("width", "min(100%,54rem)", "grid-template-columns", "minmax(0,1fr) minmax(0,1.1fr)", "column-gap", "2.25rem", "align-content", "center", "align-items", "center")...))
	declareGlobal(".kiosk-identify>h2,.kiosk-identify>.kiosk-help,.kiosk-identify>.kiosk-credential,.kiosk-identify>.kiosk-submit", mediaRule(wide, clockRaws("grid-column", "1")...))
	declareGlobal(".kiosk-identify>h2,.kiosk-identify>.kiosk-help", mediaRule(wide, clockRaws("text-align", "start")...))
	declareGlobal(".kiosk-identify>.kiosk-keypad", mediaRule(wide, clockRaws("grid-column", "2", "grid-row", "1 / span 4")...))
	declareGlobal(".kiosk-identify>.kiosk-submit", mediaRule(wide, clockRaws("grid-row", "4")...))

	declareGlobal(".kiosk-hello", clockRaws("--kiosk-phase", "var(--muted)", "--kiosk-phase-surface", "var(--surface-subtle)", "justify-items", "stretch")...)
	declareGlobal(`.kiosk-hello[data-phase="in"]`, clockRaws("--kiosk-phase", "var(--hcm-color-success)", "--kiosk-phase-surface", "var(--hcm-color-success-surface)", "border-top", "8px solid var(--hcm-color-success)")...)
	declareGlobal(`.kiosk-hello[data-phase="break"]`, clockRaws("--kiosk-phase", "var(--hcm-color-warning)", "--kiosk-phase-surface", "var(--hcm-color-warning-surface)", "border-top", "8px solid var(--hcm-color-warning)")...)
	declareGlobal(`.kiosk-hello[data-phase="out"]`, clockRaws("border-top", "8px solid var(--muted)")...)
	declareGlobal(".kiosk-state", clockRaws(
		"margin", "0", "justify-self", "center", "display", "inline-flex", "align-items", "center", "gap", "0.6rem", "padding", "0.5rem 1.1rem",
		"border-radius", "999px", "border", "1px solid var(--line)", "background", "var(--kiosk-phase-surface,var(--surface-subtle))", "color", "var(--kiosk-phase,var(--ink))", "font-size", "1.35rem", "font-weight", "700",
	)...)
	declareGlobal(".kiosk-state .kiosk-dot", clockRaws("background", "currentColor")...)
	declareGlobal(".kiosk-shift", clockRaws("margin", "0", "text-align", "center", "color", "var(--muted)", "font-size", "1.25rem")...)
	declareGlobal(".kiosk-key:disabled", clockRaws("opacity", "0.45", "cursor", "not-allowed")...)
	declareGlobal(".kiosk-stage:has(.kiosk-identify) .kiosk-offline,.kiosk-stage:has(.kiosk-identify) .kiosk-error", mediaRule(gwccss.RawMedia("(min-width:800px) and (orientation:landscape)"), clockRaws("width", "min(100%,54rem)")...))
	declareGlobal(".kiosk-actions", clockRaws("display", "grid", "gap", "0.75rem")...)
	declareGlobal(".kiosk-actions-pair", clockRaws("grid-template-columns", "repeat(2,minmax(0,1fr))")...)
	declareGlobal(".kiosk-actions-question", clockRaws("grid-column", "1 / -1")...)
	declareGlobal(".kiosk-actions-pair", mediaRule(gwccss.MaxW(520), clockRaws("grid-template-columns", "minmax(0,1fr)")...))

	declareGlobal(".kiosk-details", clockRaws("border", "1px solid var(--line)", "border-radius", "1rem", "background", "var(--surface-subtle)")...)
	declareGlobal(".kiosk-details>summary", clockRaws("display", "flex", "align-items", "center", "min-height", "3.5rem", "padding", "0.5rem 1rem", "font-weight", "650", "cursor", "pointer", "color", "var(--ink)")...)
	declareGlobal(".kiosk-details-body", clockRaws("display", "grid", "gap", "1.1rem", "padding", "0.25rem 1rem 1rem")...)
	declareGlobal(".kiosk-detail-group", clockRaws("display", "grid", "gap", "0.6rem", "padding-block-end", "1rem", "border-bottom", "1px solid var(--line)")...)
	declareGlobal(".kiosk-detail-group:last-child", clockRaws("border-bottom", "0", "padding-block-end", "0")...)
	declareGlobal(".kiosk-panel input.kiosk-credential::placeholder", clockRaws("letter-spacing", "0", "font-size", "1.35rem", "font-weight", "500", "color", "var(--muted)")...)
	declareGlobal(".kiosk-panel .kiosk-action-out", clockRaws("background", "var(--surface)", "color", "var(--ink)", "border", "3px solid var(--ink)")...)
	declareGlobal(".kiosk-receipt-detail", clockRaws("margin", "0", "text-align", "center", "font-size", "1.35rem", "font-weight", "550", "line-height", "1.4")...)
	declareGlobal(".kiosk-field", clockRaws("display", "grid", "gap", "0.3rem")...)
	declareGlobal(".kiosk-detail-actions", clockRaws("display", "grid", "gap", "0.6rem")...)
	declareGlobal(".kiosk-detail-actions .button", clockRaws("min-height", "3.5rem", "font-size", "1.1rem")...)
	declareGlobal(".kiosk-panel .kiosk-cancel", clockRaws("min-height", "3.75rem", "font-size", "1.2rem", "font-weight", "650")...)

	declareGlobal(".kiosk-receipt", clockRaws("justify-items", "center", "text-align", "center", "border-top", "8px solid var(--hcm-color-success)")...)
	declareGlobal(".kiosk-receipt-mark", clockRaws("display", "grid", "place-items", "center", "width", "6rem", "height", "6rem", "border-radius", "50%", "background", "var(--hcm-color-success-surface)", "color", "var(--hcm-color-success)")...)
	declareGlobal(".kiosk-receipt-icon", clockRaws("width", "3.5rem", "height", "3.5rem")...)
	declareGlobal(".kiosk-return", clockRaws("margin", "0", "color", "var(--muted)", "font-size", "1rem")...)
	declareGlobal(".kiosk-return-bar", clockRaws("--kiosk-return", "10s", "position", "relative", "width", "100%", "height", "0.4rem", "border-radius", "999px", "background", "var(--line)", "overflow", "hidden")...)
	declareGlobal(".kiosk-return-bar::after", clockRaws(
		"content", `""`, "position", "absolute", "inset", "0", "background", "var(--hcm-color-success)", "transform-origin", "left center",
		"animation", "kiosk-return var(--kiosk-return,10s) linear forwards",
	)...)
	declareGlobal("[dir=rtl] .kiosk-return-bar::after", clockRaws("transform-origin", "right center")...)

	declareGlobal(".kiosk-alert", clockRaws("border-top", "8px solid var(--hcm-color-danger)")...)
	declareGlobal(".kiosk-offline", clockRaws(
		"width", "min(100%,36rem)", "box-sizing", "border-box", "margin", "0", "padding", "0.9rem 1.1rem", "border-radius", "1rem",
		"border-inline-start", "6px solid var(--hcm-color-warning)", "background", "var(--hcm-color-warning-surface)", "color", "var(--ink)", "font-size", "1.1rem", "font-weight", "550",
	)...)
	declareGlobal(".kiosk-error", clockRaws(
		"width", "min(100%,36rem)", "box-sizing", "border-box", "margin", "0", "padding", "0.9rem 1.1rem", "border-radius", "1rem",
		"border-inline-start", "6px solid var(--hcm-color-danger)", "background", "var(--hcm-color-danger-surface)", "color", "var(--hcm-color-danger)", "font-size", "1.2rem", "font-weight", "650",
	)...)
}
