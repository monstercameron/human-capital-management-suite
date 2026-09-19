package productui

import (
	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// FocusIndicator is the platform-owned visible-focus contract shared by every
// product page and reusable component. The dimensions are intentionally not
// customer-overridable: the platform selects a mode-aware protected focus
// color, while the indicator's visibility and geometry remain an
// accessibility boundary.
type FocusIndicator struct {
	Selector       string
	ColorVariable  string
	RingWidth      string
	SeparatorWidth string
	RingOffset     string
}

// VisibleFocusIndicator returns the immutable focus indicator contract used by
// the platform stylesheet. Returning a value (rather than exposing mutable
// package state) keeps SSR and browser publication deterministic.
func VisibleFocusIndicator() FocusIndicator {
	return FocusIndicator{
		Selector:       `:where(a[href],button,input,select,textarea,summary,[contenteditable="true"],[tabindex]:not([tabindex="-1"])):focus-visible`,
		ColorVariable:  "--hcm-color-focus",
		RingWidth:      "2px",
		SeparatorWidth: "2px",
		RingOffset:     "4px",
	}
}

// focusStyles is appended after customer and component styles. This makes
// visible focus a platform boundary: component states can style borders and
// surfaces, but cannot accidentally remove the keyboard indicator.
// journeyFocusBridgeStyles gives the embedded Journey renderer the same
// semantic focus token and ring geometry as the product shell. Standalone
// Journey documents still use their own accent fallback; embedded documents
// inherit the protected product focus color and gap token.

func declareJourneyFocusBridge() {
	declareGlobal(".jn-embedded",
		gwccss.Custom("jn-ring", "0 0 0 var(--hcm-focus-ring-gap) var(--hcm-color-focus)"),
		// --jn-focus-color is the declared bridge between the product's
		// protected focus colour and the journey surface, and WEB-019 pins it
		// as a contract. No rule reads it today -- the ring above names
		// --hcm-color-focus directly -- so it is a promise the journey sheet
		// can keep rather than one it currently uses. It stays declared
		// because the contract is the point; if it is ever retired, WEB-019
		// is the conversation to have first.
		gwccss.Custom("jn-focus-color", "var(--hcm-color-focus)"),
	)
	declareGlobal(".jn-embedded",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"),
			gwccss.Custom("jn-ring", "0 0 0 2px Highlight"),
			gwccss.Custom("jn-focus-color", "Highlight")),
	)
}

func declareFocusStyles() {
	declareGlobal(":root",
		gwccss.CustomLength("hcm-focus-ring-width", gwccss.Px(2)),
		gwccss.CustomLength("hcm-focus-ring-gap", gwccss.Px(2)),
		gwccss.CustomLength("hcm-focus-ring-offset", gwccss.Px(4)),
	)
	declareGlobal(`:where(a[href],button,input,select,textarea,summary,[contenteditable="true"],[tabindex]:not([tabindex="-1"])):focus-visible`,
		gwccss.Raw("outline", "var(--hcm-focus-ring-width) solid var(--hcm-color-focus)"),
		gwccss.OutlineOffset(gwccss.VarLength("hcm-focus-ring-offset")),
		gwccss.Shadow(gwccss.ShadowOf(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.VarLength("hcm-focus-ring-gap"), gwccss.Var("surface"))),
	)
	declareGlobal(".wordmark:focus-visible",
		gwccss.OutlineOffset(gwccss.RawLength("calc(var(--hcm-focus-ring-offset) * -1)")),
		gwccss.Raw("box-shadow", "none"),
	)
	declareGlobal(`.jn-embedded :where(a[href],button,input,select,textarea,summary,[contenteditable="true"],[tabindex]:not([tabindex="-1"])):focus-visible`,
		gwccss.Raw("outline", "var(--hcm-focus-ring-width) solid var(--hcm-color-focus)"),
		gwccss.OutlineOffset(gwccss.VarLength("hcm-focus-ring-offset")),
		gwccss.Shadow(gwccss.ShadowOf(gwccss.Zero, gwccss.Zero, gwccss.Zero, gwccss.VarLength("hcm-focus-ring-gap"), gwccss.Var("surface"))),
	)
	declareGlobal(`.jn-embedded .jn-masthead :where(a[href],button,input,select,textarea,summary,[contenteditable="true"],[tabindex]:not([tabindex="-1"])):focus-visible`,
		gwccss.Raw("outline-color", "var(--hcm-color-on-brand)"),
	)
	declareGlobal(`:where(a[href],button,input,select,textarea,summary,[contenteditable="true"],[tabindex]:not([tabindex="-1"])):focus-visible`,
		mediaRule(gwccss.RawMedia("(prefers-reduced-motion:reduce)"),
			gwccss.Raw("animation", "none!important"),
			gwccss.Raw("transition", "none!important")),
	)
	declareGlobal(`:where(a[href],button,input,select,textarea,summary,[contenteditable="true"],[tabindex]:not([tabindex="-1"])):focus-visible`,
		mediaRule(gwccss.RawMedia("(forced-colors:active)"),
			gwccss.Raw("outline", "2px solid Highlight!important"),
			gwccss.OutlineOffset(gwccss.Px(2)),
			gwccss.Raw("box-shadow", "0 0 0 2px Canvas!important"),
			gwccss.Raw("forced-color-adjust", "auto")),
	)
	declareGlobal(".wordmark:focus-visible",
		mediaRule(gwccss.RawMedia("(forced-colors:active)"),
			gwccss.OutlineOffset(gwccss.RawLength("-4px!important")),
			gwccss.Raw("box-shadow", "none!important")),
	)
}
