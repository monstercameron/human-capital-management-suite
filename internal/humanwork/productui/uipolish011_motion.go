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
