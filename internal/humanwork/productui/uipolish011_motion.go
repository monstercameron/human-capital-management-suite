package productui

import (
	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// UIPolish011MotionStylesheet is the final, additive motion layer for the
// real shell surfaces. Stylesheet composition should append it after
// InteractionMotionStylesheet so these bounded preference rules win without
// changing the existing component contracts.
func UIPolish011MotionStylesheet() string {
	return buildTypedSheet(declareUIPolish011Motion)
}

func declareUIPolish011Motion() {
	// Cold page entry should acknowledge the new context once, then settle.
	// The older layer used the slow token for both the page and every row and
	// added up to 140ms of stagger, which made dense pages continue moving after
	// they were already usable. Keep the page cue brief and make repeated rows
	// immediate, uniform children of that cue.
	declareGlobal(".main>.page-head,.main>.home-grid,.main>.workbench,.main>.people-page,.main>.person-page,.main>.organization-page,.main>.insights-grid,.main>.admin-grid,.main>.studio-page,.main>.jn-embedded",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Raw("animation-duration", "var(--hcm-motion-normal)")),
	)
	declareGlobal(".work-row,.people-row,.history-row,.jn-embedded .jn-griditem",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Raw("animation-duration", "var(--hcm-motion-fast)"), gwccss.Raw("animation-delay", "0ms!important")),
	)
	declareGlobal(".status,.count,.validation-summary,.organization-unit-disclosure[open] .organization-unit-members",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:no-preference)"), gwccss.Raw("animation-duration", "var(--hcm-motion-fast)"), gwccss.Raw("animation-delay", "0ms!important")),
	)

	// Authoritative refreshes retain their old content. A one-pixel translation
	// of the entire page made that stable region appear to jump, especially when
	// a table spinner was also moving. Opacity alone supplies the state cue.
	declareGlobal(".network-stage",
		gwccss.Raw("transition", "opacity var(--hcm-motion-fast) var(--hcm-motion-easing)"),
	)
	declareGlobal(".network-stage-refreshing",
		gwccss.Raw("opacity", ".99"),
		gwccss.Raw("transform", "none"),
	)

	// Transition only properties each component actually changes. The legacy
	// catch-all mixed width, max-size, grid layout, paint and transforms on every
	// surface; that caused unnecessary layout work and let unrelated transitions
	// compete. Navigation geometry remains explicit below, while ordinary
	// controls and surfaces stay on fast paint/composite properties.
	declareGlobal(":where(.button,.nav-link,.nav-favorite,.sidebar-toggle,.header-nav-toggle,.nav-icon,.avatar,.activity .check,.jn-embedded .jn-btn)",
		gwccss.Raw("transition-property", "transform,box-shadow,border-color,background-color,color,opacity"),
		gwccss.Raw("transition-duration", "var(--hcm-motion-fast)"),
		gwccss.Raw("transition-timing-function", "var(--hcm-motion-easing)"),
	)
	declareGlobal(":where(.surface,.work-row,.people-row,.history-row,.activity,.metric,.org-node,.workflow-card,.choice,.accessibility-choice,.jn-embedded .jn-card)",
		gwccss.Raw("transition-property", "box-shadow,border-color,background-color,color,opacity"),
		gwccss.Raw("transition-duration", "var(--hcm-motion-fast)"),
		gwccss.Raw("transition-timing-function", "var(--hcm-motion-easing)"),
	)
	declareGlobal(":where(.work-row,.people-row,.history-row):hover",
		gwccss.Raw("transform", "none"),
	)

	// Only the small shell elements that actually collapse are allowed to
	// animate layout. Their label fade finishes before the column does, which
	// keeps the control responsive without clipping text halfway through.
	declareGlobal(".shell-grid",
		gwccss.Raw("transition-property", "grid-template-columns"),
		gwccss.Raw("transition-duration", "var(--hcm-motion-normal)"),
		gwccss.Raw("transition-timing-function", "var(--hcm-motion-easing)"),
	)
	declareGlobal(".brand-cluster",
		gwccss.Raw("transition-property", "grid-template-columns,border-color,background-color"),
		gwccss.Raw("transition-duration", "var(--hcm-motion-normal),var(--hcm-motion-fast),var(--hcm-motion-fast)"),
		gwccss.Raw("transition-timing-function", "var(--hcm-motion-easing)"),
	)
	declareGlobal(".wordmark-label,.tenant,.nav-label,.nav-count",
		gwccss.Raw("transition-property", "max-width,max-height,opacity,transform"),
		gwccss.Raw("transition-duration", "var(--hcm-motion-normal),var(--hcm-motion-normal),var(--hcm-motion-fast),var(--hcm-motion-fast)"),
		gwccss.Raw("transition-timing-function", "var(--hcm-motion-easing)"),
	)

	// The drawer already uses logical inset-inline-start for its off-canvas
	// state; only its transition is normalized here, keeping RTL direction safe.
	declareGlobal(".app-shell :is(.sidebar,.sidebar.collapsed)",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("transition", "inset-inline-start var(--hcm-motion-normal) var(--hcm-motion-easing)")),
	)
	// Limited motion keeps the logical open/closed state but removes spatial
	// drawer travel. The important rule also wins over the native WASM drawer
	// controller's ordinary inline transform transition.
	declareGlobal(":root[data-hcm-motion-preference=\"limited\"] .sidebar,:root[data-hcm-motion-preference=\"limited\"] .sidebar.collapsed",
		mediaRule(gwccss.MaxW(760), gwccss.Raw("transition", "none!important")),
	)
	// This layer is intentionally appended after the legacy motion sheets, so
	// restate both reduced-preference paths to make the cascade fail-safe.
	declareGlobal(":root[data-hcm-motion-preference=\"reduce\"] .sidebar,:root[data-hcm-motion-preference=\"reduce\"] .sidebar.collapsed",
		gwccss.Raw("transition", "none!important"),
	)
	declareGlobal(".sidebar,.sidebar.collapsed",
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"), gwccss.Raw("transition", "none!important")),
	)
}
