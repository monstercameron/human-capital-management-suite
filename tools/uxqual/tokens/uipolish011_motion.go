package tokens

import (
	"sync"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// MotionCSS is the renderer-neutral UIPOLISH-011 motion contract. It is
// intentionally separate from WorkspaceCSS until production composition wires
// this sheet into both the SSR and WASM style roots.
var motionCSSOnce = sync.OnceValue(func() string {
	return buildTypedSheet(declareMotionContract) + atRule("@media (prefers-reduced-motion:reduce)", buildTypedSheet(declareMotionReduceMedia))
})

func MotionCSS() string { return motionCSSOnce() }

func declareMotionContract() {
	declareGlobal(`:root`,
		gwccss.Custom("motion-duration-instant", `0ms`),
		gwccss.Custom("motion-duration-fast", `120ms`),
		gwccss.Custom("motion-duration-normal", `180ms`),
		gwccss.Custom("motion-duration-slow", `280ms`),
		gwccss.Custom("motion-easing-standard", `cubic-bezier(.2,.8,.2,1)`),
		gwccss.Custom("motion-easing-emphasized", `cubic-bezier(.2,0,0,1)`),
		gwccss.Custom("motion-distance-navigation", `8px`),
		gwccss.Custom("motion-distance-drawer", `1rem`),
		gwccss.Custom("motion-distance-popover", `4px`),
		gwccss.Custom("motion-distance-list", `8px`),
	)
	declareGlobal(`:where([data-motion="navigation"],[data-motion="drawer"],[data-motion="popover"],[data-motion="list"],[data-motion="async"])`,
		gwccss.Raw("transition", "transform var(--motion-duration-normal) var(--motion-easing-standard),opacity var(--motion-duration-fast) var(--motion-easing-standard)"),
	)
	// Navigation and drawers use opacity-only entry/exit. A physical X
	// direction would make the shared contract wrong for RTL shells.
	declareGlobal(`[data-motion="navigation"][data-motion-state="closed"],[data-motion="drawer"][data-motion-state="closed"]`, gwccss.Raw("opacity", "0"))
	declareGlobal(`[data-motion="navigation"][data-motion-state="open"],[data-motion="drawer"][data-motion-state="open"]`, gwccss.Raw("opacity", "1"))
	declareGlobal(`[data-motion="popover"][data-motion-state="closed"]`, gwccss.Raw("transform", "translateY(calc(-1 * var(--motion-distance-popover)))"), gwccss.Raw("opacity", "0"))
	declareGlobal(`[data-motion="popover"][data-motion-state="open"]`, gwccss.Raw("transform", "translateY(0)"), gwccss.Raw("opacity", "1"))
	declareGlobal(`[data-motion="list"]`, gwccss.Raw("transition-duration", "var(--motion-duration-fast)"))
	declareGlobal(`[data-motion="async"]`, gwccss.Raw("transition-duration", "var(--motion-duration-normal)"))
	declareGlobal(`:root[data-hcm-motion-preference="limited"] :where([data-motion])`,
		gwccss.Raw("transition", "opacity var(--motion-duration-fast) var(--motion-easing-standard)"), gwccss.Raw("transform", "none!important"),
	)
	declareGlobal(`:root[data-hcm-motion-preference="reduce"] :where([data-motion])`,
		gwccss.Raw("transition", "none"), gwccss.Raw("transform", "none!important"),
	)
}

func declareMotionReduceMedia() {
	declareGlobal(`:root`,
		gwccss.Custom("motion-duration-fast", `0ms`),
		gwccss.Custom("motion-duration-normal", `0ms`),
		gwccss.Custom("motion-duration-slow", `0ms`),
	)
	// A zero-duration transition still applies a translated endpoint. System
	// reduced-motion must remove that spatial movement, not merely make it
	// instantaneous.
	declareGlobal(`:where([data-motion])`,
		gwccss.Raw("transition", "none"), gwccss.Raw("transform", "none!important"),
	)
}
