package productui

import (
	"strings"
	"testing"
)

func TestNavigationScrollbarUsesSemanticTokensAndNativeFallbacks(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`--hcm-nav-scrollbar-track:color-mix(in srgb,var(--surface)`,
		`.sidebar{scrollbar-color:var(--hcm-nav-scrollbar-thumb) var(--hcm-nav-scrollbar-track);`,
		`--hcm-nav-scrollbar-size:10px;`,
		`--hcm-nav-scrollbar-size-rail:7px;`,
		`.sidebar::-webkit-scrollbar{width:var(--hcm-nav-scrollbar-size);}`,
		`.sidebar::-webkit-scrollbar-thumb{background-clip:padding-box;`,
		`.sidebar::-webkit-scrollbar-thumb:hover{background-clip:padding-box;background-color:var(--hcm-nav-scrollbar-thumb-hover);}`,
		`.sidebar::-webkit-scrollbar-thumb:active{background-clip:padding-box;background-color:var(--hcm-nav-scrollbar-thumb-active);}`,
		`@media (forced-colors:active){.sidebar{scrollbar-color:ButtonText Canvas;}`,
		`.sidebar{overflow:hidden;}.primary-nav{flex:1;min-height:0;overflow-y:auto;`,
		`.primary-nav::-webkit-scrollbar-thumb{background-clip:padding-box;`,
		`.primary-nav{overflow-x:hidden;}`,
		`@media (forced-colors:active){.primary-nav{scrollbar-color:ButtonText Canvas;}`,
		`@media (min-width:761px){.sidebar{--hcm-nav-content-inset:14px;--hcm-nav-rail-shift:10px;overflow:hidden`,
		`.sidebar.collapsed{--hcm-nav-content-inset:9px;--hcm-nav-rail-shift:7px;padding-inline-end:0`,
		`.primary-nav>ul,.nav-bottom{padding-inline-end:var(--hcm-nav-content-inset)`,
		`align-self:stretch;flex:1 1 auto;margin-inline-end:calc(-1 * var(--hcm-nav-rail-shift));`,
		`min-width:calc(100% + var(--hcm-nav-rail-shift))`,
		`max-width:none`,
		`margin-inline-end:calc(-1 * var(--hcm-nav-rail-shift));max-width:none;min-width:calc(100% + var(--hcm-nav-rail-shift));padding-inline-end:0;`,
		`.primary-nav::-webkit-scrollbar{width:var(--hcm-nav-scrollbar-size-rail);}`,
		`@media (min-width:761px){.primary-nav::-webkit-scrollbar-track{background-color:transparent;}}`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("navigation scrollbar is missing %q", want)
		}
	}
	if strings.Contains(css, `.main-scroll::-webkit-scrollbar`) {
		t.Fatal("navigation scrollbar styling leaked into the page scroll region")
	}
}

func TestNavigationFavoritesRevealOnIntentWithoutLeavingKeyboardOrTouchGaps(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`@media (min-width:761px) and (hover:hover){.primary-nav .nav-favorite{opacity:0;pointer-events:none;}`,
		`.primary-nav .nav-entry:hover>.nav-favorite`,
		`.primary-nav .nav-entry:focus-within>.nav-favorite`,
		`.primary-nav .nav-favorite:focus-visible{opacity:1;pointer-events:auto;}`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("favorite interaction styling is missing %q", want)
		}
	}
	if strings.Contains(css, `@media (hover:none){.primary-nav .nav-favorite{opacity:0`) {
		t.Fatal("favorite controls were hidden from touch-only users")
	}
}
