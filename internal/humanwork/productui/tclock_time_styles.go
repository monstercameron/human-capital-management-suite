package productui

import gwccss "github.com/monstercameron/GoWebComponents/v5/css"

// declareTimeSurfaceStyles styles the timecard, supervisor review, exceptions
// queue, device fleet and crew schedule. All of them share one vocabulary: a
// status pill (word plus colour), a confirm panel, rows that turn into
// stacked cards on narrow screens, and product tokens throughout.
func declareTimeSurfaceStyles() {
	// The product caps p, li, dd and dt at a reading measure; list rows and
	// notices on these surfaces are layout, so they fill their container.
	declareGlobal(".clock-notice,.time-fact,.timecard-day,.review-row,.exception-row,.punch-record>div,.timecard-punch", clockRaws("max-inline-size", "none")...)
	declareGlobal(".time-page", clockRaws("display", "grid", "gap", clockGapLg, "min-width", "0", "max-width", "72rem")...)
	declareGlobal(".time-head", clockRaws("margin-block-end", "0")...)
	declareGlobal(".time-head", clockRaws("display", "flex", "flex-wrap", "wrap", "align-items", "flex-end", "justify-content", "space-between", "gap", clockGap)...)
	declareGlobal(".fleet-head-action .button", clockRaws("display", "inline-flex", "align-items", "center", "text-decoration", "none", "min-height", "3rem", "padding-inline", "1.25rem", "font-weight", "700")...)
	declareGlobal(".time-empty", clockRaws("padding", "calc(var(--hcm-space-4) * var(--hcm-density) * 1.25)", "max-width", "44rem")...)
	declareGlobal(".time-empty h2", clockRaws("margin", "0 0 0.35rem", "font-size", "1.25rem", "font-weight", "700")...)
	declareGlobal(".time-empty p", clockRaws("margin", "0")...)

	// Status pill.
	declareGlobal(".time-pill", clockRaws(
		"--pill-fg", "var(--muted)", "--pill-bg", "var(--surface-subtle)",
		"display", "inline-flex", "align-items", "center", "gap", "0.45rem", "padding", "0.25rem 0.8rem", "border-radius", "999px",
		"border", "1px solid var(--line)", "background", "var(--pill-bg)", "color", "var(--pill-fg)", "font-size", "var(--hcm-font-size-small)", "font-weight", "650", "max-width", "100%",
	)...)
	declareGlobal(`.time-pill[data-tone="ok"]`, clockRaws("--pill-fg", "var(--hcm-color-success)", "--pill-bg", "var(--hcm-color-success-surface)")...)
	declareGlobal(`.time-pill[data-tone="warn"]`, clockRaws("--pill-fg", "var(--hcm-color-warning)", "--pill-bg", "var(--hcm-color-warning-surface)")...)
	declareGlobal(`.time-pill[data-tone="bad"]`, clockRaws("--pill-fg", "var(--hcm-color-danger)", "--pill-bg", "var(--hcm-color-danger-surface)")...)
	declareGlobal(`.time-pill[data-tone="info"]`, clockRaws("--pill-fg", "var(--hcm-color-info)", "--pill-bg", "var(--hcm-color-info-surface)")...)
	declareGlobal(".time-pill-dot", clockRaws("flex", "none", "width", "0.6rem", "height", "0.6rem", "border-radius", "50%", "background", "currentColor")...)

	// Shared facts list and confirm panel.
	declareGlobal(".time-facts", clockRaws("display", "grid", "gap", clockGap, "margin", "0", "grid-template-columns", "repeat(auto-fit,minmax(9rem,1fr))")...)
	declareGlobal(".time-fact", clockRaws("display", "grid", "gap", "0.15rem")...)
	declareGlobal(".time-fact dt", clockRaws("color", "var(--muted)", "font-size", "var(--hcm-font-size-small)")...)
	declareGlobal(".time-fact dd", clockRaws("margin", "0", "font-size", "1.2rem", "font-weight", "700", "font-variant-numeric", "tabular-nums", "overflow-wrap", "anywhere")...)
	declareGlobal(".time-confirm", clockRaws(
		"display", "grid", "gap", clockGapSm, "padding", clockGap, "border", "2px solid var(--accent)", "border-radius", "var(--hcm-radius-surface,12px)", "background", "var(--soft)",
	)...)
	declareGlobal(".time-confirm-danger", clockRaws("border-color", "var(--hcm-color-danger)", "background", "var(--hcm-color-danger-surface)")...)
	declareGlobal(".time-confirm-title", clockRaws("margin", "0", "font-size", "1.2rem", "font-weight", "700", "color", "var(--ink)")...)
	declareGlobal(".time-confirm-body", clockRaws("margin", "0", "color", "var(--ink)")...)
	declareGlobal(".time-confirm-actions", clockRaws("display", "flex", "flex-wrap", "wrap", "gap", clockGapSm)...)
	declareGlobal(".time-confirm-actions .button", clockRaws("min-height", "3rem", "padding-inline", "1.25rem", "font-weight", "700")...)
	declareGlobal(".time-confirm-actions", mediaRule(gwccss.MaxW(520), clockRaws("display", "grid")...))

	// Period bar (timecard and schedule).
	declareGlobal(".timecard-bar", clockRaws("display", "flex", "flex-wrap", "wrap", "align-items", "center", "gap", clockGapSm)...)
	declareGlobal(".timecard-period", clockRaws("display", "flex", "flex-wrap", "wrap", "align-items", "center", "gap", clockGapSm, "margin-inline-end", "auto")...)
	declareGlobal(".timecard-period-label", clockRaws("margin", "0", "font-size", "clamp(1.25rem,2.6vw,1.6rem)", "font-weight", "750", "letter-spacing", "-0.01em")...)
	declareGlobal(".timecard-step", clockRaws("display", "inline-flex", "align-items", "center", "text-decoration", "none", "min-height", "2.75rem", "font-weight", "600")...)

	// Timecard.
	declareGlobal(".timecard-summary", clockRaws("padding", "calc(var(--hcm-space-4) * var(--hcm-density))")...)
	declareGlobal(".timecard-days", clockRaws("display", "grid", "gap", clockGapSm, "margin", "0", "padding", "0", "list-style", "none")...)
	declareGlobal(".timecard-day", clockRaws(
		"display", "grid", "gap", "0.6rem", "padding", "calc(var(--hcm-space-3) * var(--hcm-density)) calc(var(--hcm-space-4) * var(--hcm-density))",
		"border", "1px solid var(--line)", "border-inline-start", "6px solid var(--line)", "border-radius", "var(--hcm-radius-surface,12px)", "background", "var(--surface)",
	)...)
	declareGlobal(`.timecard-day[data-state="problem"]`, clockRaws("border-inline-start-color", "var(--hcm-color-danger)")...)
	declareGlobal(`.timecard-day[data-state="ok"]`, clockRaws("border-inline-start-color", "var(--hcm-color-success)")...)
	declareGlobal(".timecard-day-today", clockRaws("box-shadow", "0 0 0 2px var(--accent)")...)
	declareGlobal(".timecard-day-head", clockRaws("display", "flex", "align-items", "baseline", "justify-content", "space-between", "gap", clockGapSm)...)
	declareGlobal(".timecard-day-title", clockRaws("display", "flex", "flex-wrap", "wrap", "align-items", "center", "gap", "0.6rem")...)
	declareGlobal(".timecard-goto", clockRaws("margin-inline-start", "0.5rem", "font-weight", "700", "color", "inherit", "text-decoration", "underline", "text-underline-offset", "0.2em")...)
	declareGlobal(".timecard-day", clockRaws("scroll-margin-block-start", "1rem")...)
	declareGlobal(".timecard-submit-blocked", clockRaws("display", "flex", "flex-wrap", "wrap", "align-items", "center", "gap", "0.5rem 1rem")...)
	declareGlobal(".timecard-submit-blocked .timecard-submit", clockRaws("width", "auto", "min-width", "16rem", "opacity", "0.55", "cursor", "not-allowed")...)
	declareGlobal(".timecard-date", clockRaws("margin", "0", "font-size", "1.1rem", "font-weight", "700")...)
	declareGlobal(".timecard-total", clockRaws("margin", "0", "font-size", "1.1rem", "font-weight", "700", "font-variant-numeric", "tabular-nums")...)
	declareGlobal(".timecard-punches", clockRaws("display", "grid", "gap", "0.25rem", "margin", "0", "padding", "0", "list-style", "none")...)
	declareGlobal(".timecard-punch", clockRaws("display", "flex", "flex-wrap", "wrap", "gap", "0.25rem 1rem", "align-items", "baseline")...)
	declareGlobal(".timecard-punch-time", clockRaws("font-weight", "600", "font-variant-numeric", "tabular-nums")...)
	declareGlobal(".timecard-punch-detail", clockRaws("color", "var(--muted)", "font-size", "var(--hcm-font-size-small)")...)
	declareGlobal(".timecard-punch-break .timecard-punch-time", clockRaws("color", "var(--hcm-color-warning)")...)
	declareGlobal(".timecard-none", clockRaws("margin", "0")...)
	declareGlobal(".timecard-problem", clockRaws("display", "flex", "flex-wrap", "wrap", "align-items", "center", "gap", clockGapSm)...)
	declareGlobal(".timecard-fix", clockRaws("display", "inline-flex", "align-items", "center", "text-decoration", "none", "min-height", "2.5rem", "font-weight", "600")...)
	declareGlobal(".timecard-empty", clockRaws("padding", clockGapLg, "margin", "0")...)
	declareGlobal(".timecard-submit-bar", clockRaws("position", "sticky", "inset-block-end", "0", "padding", "0.75rem 0", "background", "var(--canvas)", "z-index", "2")...)
	declareGlobal(".timecard-submit", clockRaws("display", "inline-flex", "align-items", "center", "justify-content", "center", "text-decoration", "none", "box-sizing", "border-box", "width", "100%", "max-width", "28rem", "min-height", "3.75rem", "font-size", "1.2rem", "font-weight", "700", "border-radius", "var(--hcm-radius-surface,12px)")...)
	declareGlobal(".timecard-submit-note", clockRaws("margin", "0")...)

	// Supervisor review.
	declareGlobal(".review-group", clockRaws("display", "grid", "gap", clockGap, "min-width", "0", "padding", "calc(var(--hcm-space-4) * var(--hcm-density))")...)
	declareGlobal(".review-group-needs", clockRaws("border-inline-start", "6px solid var(--hcm-color-danger)")...)
	declareGlobal(".review-group-ready", clockRaws("border-inline-start", "6px solid var(--hcm-color-success)")...)
	declareGlobal(".review-group-head h2", clockRaws("margin", "0 0 0.25rem", "font-size", "1.25rem", "font-weight", "700", "letter-spacing", "-0.01em")...)
	declareGlobal(".review-group-head p", clockRaws("margin", "0")...)
	declareGlobal(".review-rows", clockRaws("display", "grid", "margin", "0", "padding", "0", "list-style", "none")...)
	declareGlobal(".review-row", clockRaws(
		"display", "grid", "grid-template-columns", "minmax(0,2fr) minmax(0,2fr) minmax(0,1fr) auto", "gap", clockGapSm, "align-items", "center",
		"padding", "0.75rem 0", "border-top", "1px solid var(--line)",
	)...)
	declareGlobal(".review-row-ready", clockRaws("grid-template-columns", "minmax(0,3fr) minmax(0,2fr) auto")...)
	declareGlobal(".review-who", clockRaws("display", "grid", "gap", "0.1rem", "min-width", "0")...)
	declareGlobal(".review-pick", clockRaws("display", "flex", "align-items", "center", "gap", "0.9rem", "min-height", "3rem", "cursor", "pointer")...)
	declareGlobal(".review-pick input,.review-all input", clockRaws("width", "1.75rem", "height", "1.75rem", "flex", "none", "accent-color", "var(--accent)")...)
	declareGlobal(".review-all", clockRaws("display", "flex", "align-items", "center", "gap", "0.9rem", "min-height", "3rem", "font-weight", "650", "cursor", "pointer")...)
	declareGlobal(".review-problems", clockRaws("display", "flex", "flex-wrap", "wrap", "gap", "0.4rem")...)
	declareGlobal(".review-hours", clockRaws("font-weight", "650", "font-variant-numeric", "tabular-nums")...)
	declareGlobal(".review-action .button", clockRaws("display", "inline-flex", "align-items", "center", "text-decoration", "none", "min-height", "2.75rem", "font-weight", "600")...)
	declareGlobal(".review-row,.review-row-ready", mediaRule(gwccss.MaxW(720), clockRaws("grid-template-columns", "minmax(0,1fr)", "gap", "0.5rem")...))
	declareGlobal(".review-bar", clockRaws("position", "sticky", "inset-block-end", "0", "display", "flex", "flex-wrap", "wrap", "align-items", "center", "justify-content", "space-between", "gap", clockGapSm, "padding", "0.75rem 0", "background", "var(--surface)", "border-top", "1px solid var(--line)", "z-index", "2")...)
	declareGlobal(".review-bar-confirm", clockRaws("display", "block")...)
	declareGlobal(".review-bar-count,.review-bar-hint", clockRaws("margin", "0", "font-weight", "650")...)
	declareGlobal(".review-approve", clockRaws("min-height", "3.5rem", "padding-inline", "1.5rem", "font-size", "1.1rem", "font-weight", "700")...)
	declareGlobal(".review-approve", mediaRule(gwccss.MaxW(520), clockRaws("width", "100%")...))

	// Exceptions queue.
	declareGlobal(".exception-filters ul", clockRaws("display", "flex", "flex-wrap", "wrap", "gap", "0.5rem", "margin", "0", "padding", "0", "list-style", "none")...)
	declareGlobal(".exception-filter", clockRaws(
		"display", "inline-flex", "align-items", "center", "gap", "0.5rem", "min-height", "2.75rem", "padding", "0.25rem 1rem", "border", "1px solid var(--control-border,var(--line))",
		"border-radius", "999px", "background", "var(--surface)", "color", "var(--ink)", "font-weight", "600", "text-decoration", "none",
	)...)
	declareGlobal(".exception-filter-current", clockRaws("background", "var(--accent)", "color", "var(--on-brand,#fff)", "border-color", "var(--accent)")...)
	declareGlobal(".exception-filter-count", clockRaws("font-variant-numeric", "tabular-nums", "opacity", "0.85")...)
	declareGlobal(".exception-rows", clockRaws("display", "grid", "margin", "0", "padding", "calc(var(--hcm-space-2) * var(--hcm-density)) calc(var(--hcm-space-4) * var(--hcm-density))", "list-style", "none")...)
	declareGlobal(".exception-row", clockRaws(
		"display", "grid", "grid-template-columns", "minmax(0,3fr) minmax(0,1.2fr) auto", "gap", clockGap, "align-items", "center", "padding", "1rem 0", "border-top", "1px solid var(--line)",
	)...)
	declareGlobal(".exception-row:first-child", clockRaws("border-top", "0")...)
	declareGlobal(".exception-main", clockRaws("display", "grid", "gap", "0.35rem", "justify-items", "start", "min-width", "0")...)
	declareGlobal(".exception-worker", clockRaws("font-size", "1.1rem")...)
	declareGlobal(".exception-detail", clockRaws("margin", "0", "color", "var(--muted)")...)
	declareGlobal(".exception-when", clockRaws("display", "grid", "gap", "0.15rem", "font-weight", "600")...)
	declareGlobal(".exception-actions", clockRaws("display", "flex", "flex-wrap", "wrap", "gap", "0.75rem")...)
	declareGlobal(".exception-actions .button", clockRaws("display", "inline-flex", "align-items", "center", "text-decoration", "none", "min-height", "2.75rem", "font-weight", "650")...)
	declareGlobal(".exception-row", mediaRule(gwccss.MaxW(720), clockRaws("grid-template-columns", "minmax(0,1fr)", "gap", "0.6rem")...))

	// Device fleet.
	declareGlobal(".fleet-summary", clockRaws("margin", "0")...)
	declareGlobal(".fleet-summary .time-pill", clockRaws("font-size", "1rem", "padding", "0.4rem 1rem")...)
	declareGlobal(".fleet-cards", clockRaws("display", "grid", "gap", clockGapLg, "grid-template-columns", "repeat(auto-fill,minmax(min(100%,22rem),1fr))")...)
	declareGlobal(".fleet-card", clockRaws("display", "grid", "gap", clockGap, "align-content", "start", "min-width", "0", "padding", "calc(var(--hcm-space-4) * var(--hcm-density))", "border-inline-start", "6px solid var(--line)")...)
	declareGlobal(`.fleet-card[data-tone="ok"]`, clockRaws("border-inline-start-color", "var(--hcm-color-success)")...)
	declareGlobal(`.fleet-card[data-tone="warn"]`, clockRaws("border-inline-start-color", "var(--hcm-color-warning)")...)
	declareGlobal(`.fleet-card[data-tone="bad"]`, clockRaws("border-inline-start-color", "var(--hcm-color-danger)")...)
	declareGlobal(".fleet-card-revoked", clockRaws("opacity", "0.7")...)
	declareGlobal(".fleet-card-head", clockRaws("display", "flex", "flex-wrap", "wrap", "align-items", "flex-start", "justify-content", "space-between", "gap", clockGapSm)...)
	declareGlobal(".fleet-name", clockRaws("margin", "0", "font-size", "1.2rem", "font-weight", "700")...)
	declareGlobal(".fleet-site", clockRaws("margin", "0.1rem 0 0")...)
	declareGlobal(".fleet-facts", clockRaws("grid-template-columns", "repeat(auto-fit,minmax(7.5rem,1fr))")...)
	declareGlobal(".fleet-facts .time-fact dd", clockRaws("font-size", "1rem")...)
	declareGlobal(".fleet-issues", clockRaws("margin", "0", "padding-inline-start", "1.25rem", "color", "var(--hcm-color-danger)", "font-weight", "600")...)
	declareGlobal(".fleet-actions", clockRaws("display", "flex", "flex-wrap", "wrap", "gap", "0.75rem")...)
	declareGlobal(".fleet-revoke", clockRaws("color", "var(--hcm-color-danger)", "border-color", "transparent", "background", "transparent", "text-decoration", "underline", "text-underline-offset", "0.2em")...)
	declareGlobal(`.fleet-card[data-tone="warn"] .fleet-issues`, clockRaws("color", "var(--hcm-color-warning)")...)
	declareGlobal(".fleet-actions .button", clockRaws("display", "inline-flex", "align-items", "center", "text-decoration", "none", "min-height", "3rem", "font-weight", "650")...)
	declareGlobal(".time-retry", clockRaws("margin-block-start", "0.9rem", "min-height", "3rem", "padding-inline", "1.25rem", "font-weight", "650")...)

	// Crew schedule.
	declareGlobal(".schedule-scroll", clockRaws("overflow-x", "auto", "padding", "0")...)
	declareGlobal(".schedule-grid", clockRaws("width", "100%", "border-collapse", "collapse", "min-width", "52rem")...)
	declareGlobal(".schedule-grid th,.schedule-grid td", clockRaws("padding", "0.6rem", "border-bottom", "1px solid var(--line)", "text-align", "start", "vertical-align", "top")...)
	declareGlobal(".schedule-grid thead th", clockRaws("position", "sticky", "inset-block-start", "0", "background", "var(--surface-subtle)", "font-size", "var(--hcm-font-size-small)", "font-weight", "700", "white-space", "nowrap")...)
	declareGlobal(".schedule-worker", clockRaws("display", "grid", "gap", "0.1rem", "min-width", "8rem", "font-weight", "400")...)
	declareGlobal(".schedule-grid th,.schedule-grid td", clockRaws("padding", "0.5rem 0.4rem")...)
	declareGlobal(".schedule-cell", clockRaws("min-width", "7.5rem")...)
	declareGlobal(".schedule-shift-time", clockRaws("text-wrap", "balance")...)
	declareGlobal(".schedule-shift .time-pill", clockRaws("font-size", "0.75rem", "padding", "0.15rem 0.5rem")...)
	declareGlobal(".schedule-off", clockRaws("color", "var(--muted)", "font-size", "var(--hcm-font-size-small)")...)
	declareGlobal(".schedule-shift", clockRaws(
		"display", "grid", "gap", "0.15rem", "padding", "0.45rem 0.6rem", "border-radius", "var(--hcm-radius-control,8px)", "border", "1px solid var(--accent)", "background", "var(--soft)", "font-size", "var(--hcm-font-size-small)",
	)...)
	declareGlobal(".schedule-shift+.schedule-shift", clockRaws("margin-block-start", "0.35rem")...)
	declareGlobal(".schedule-shift-draft", clockRaws("border-style", "dashed", "background", "transparent")...)
	declareGlobal(".schedule-shift-conflict", clockRaws("border-color", "var(--hcm-color-danger)", "background", "var(--hcm-color-danger-surface)")...)
	declareGlobal(".schedule-shift-time", clockRaws("font-weight", "700", "font-variant-numeric", "tabular-nums")...)
	declareGlobal(".schedule-shift-site", clockRaws("color", "var(--muted)")...)
	declareGlobal(".schedule-shift-tag", clockRaws("justify-self", "start", "font-weight", "700", "color", "var(--accent)")...)
	declareGlobal(".schedule-shift-edit", clockRaws("display", "inline-flex", "align-items", "center", "min-height", "2.75rem", "font-weight", "650", "font-size", "var(--hcm-font-size-small)")...)
	declareGlobal(".schedule-legend", clockRaws("display", "flex", "flex-wrap", "wrap", "align-items", "center", "gap", "0.5rem 1rem", "margin", "0")...)
	declareGlobal(".schedule-legend-swatch", clockRaws("display", "inline-block", "width", "1.25rem", "height", "0.85rem", "border-radius", "4px", "border", "1px solid var(--accent)", "background", "var(--soft)")...)
	declareGlobal(".schedule-legend-draft", clockRaws("border-style", "dashed", "background", "transparent")...)
	declareGlobal(".schedule-publish", clockRaws("display", "flex", "flex-wrap", "wrap", "align-items", "center", "justify-content", "space-between", "gap", clockGap, "padding", "calc(var(--hcm-space-3) * var(--hcm-density)) calc(var(--hcm-space-4) * var(--hcm-density))")...)
	declareGlobal(".schedule-publish-note", clockRaws("margin", "0", "font-weight", "600")...)
	declareGlobal(".schedule-publish-button", clockRaws("min-height", "3.5rem", "padding-inline", "1.5rem", "font-size", "1.1rem", "font-weight", "700")...)
	declareGlobal(".schedule-publish .time-confirm", clockRaws("flex", "1 1 100%")...)

	// On a phone the grid gives way to one card per worker: each day is a
	// labelled line, so nothing scrolls sideways and nothing loses its label.
	stack := gwccss.MaxW(720)
	declareGlobal(".schedule-scroll", mediaRule(stack, clockRaws("overflow", "visible")...))
	declareGlobal(".schedule-grid", mediaRule(stack, clockRaws("min-width", "0", "display", "block")...))
	declareGlobal(".schedule-grid thead", mediaRule(stack, clockRaws("position", "absolute", "width", "1px", "height", "1px", "overflow", "hidden", "clip", "rect(0 0 0 0)")...))
	declareGlobal(".schedule-grid tbody,.schedule-grid tr", mediaRule(stack, clockRaws("display", "block")...))
	declareGlobal(".schedule-grid tr", mediaRule(stack, clockRaws("padding", "0.75rem", "border-bottom", "1px solid var(--line)")...))
	declareGlobal(".schedule-grid th.schedule-worker", mediaRule(stack, clockRaws("display", "grid", "border", "0", "padding", "0 0 0.5rem", "min-width", "0")...))
	declareGlobal(".schedule-grid td", mediaRule(stack, clockRaws("display", "grid", "grid-template-columns", "5.5rem minmax(0,1fr)", "gap", "0.5rem", "border", "0", "padding", "0.35rem 0", "min-width", "0")...))
	declareGlobal(".schedule-grid td::before", mediaRule(stack, clockRaws("content", "attr(data-day)", "font-weight", "700", "font-size", "var(--hcm-font-size-small)", "color", "var(--muted)")...))
}
