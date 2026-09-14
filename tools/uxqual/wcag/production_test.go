package wcag

import (
	"strings"
	"testing"
)

const productionFixture = `<!doctype html><html lang="en-US" dir="ltr"><head><style>
:focus-visible{outline:2px solid #000;box-shadow:0 0 0 2px #fff}
@media (forced-colors:active){:focus-visible{outline:2px solid Highlight}}
@media (prefers-reduced-motion:reduce){*{animation:none;transition:none;scroll-behavior:auto}}
.layout{max-width:100%;display:flex;flex-wrap:wrap}
</style></head><body><header><a href="/">Home</a></header><main id="main"><h1>Home</h1><label for="query">Query</label><input id="query" type="search" inputmode="search" aria-describedby="hint"><p id="hint">Search records.</p><button type="button">Submit</button><div role="status" aria-live="polite">Ready</div></main></body></html>`

func TestProductionSurfaceScorePassesCompleteContract(t *testing.T) {
	for _, result := range SurfaceScore(productionFixture, "en-US", "ltr") {
		if !result.Pass {
			t.Errorf("%s: %s", result.Name, result.Detail)
		}
	}
}

func TestProductionAccessibilityChecksRejectMutations(t *testing.T) {
	tests := []struct {
		name  string
		doc   string
		check func(string) bool
	}{
		{"dangling aria reference", strings.Replace(productionFixture, `aria-describedby="hint"`, `aria-describedby="missing"`, 1), func(doc string) bool { return !CheckScreenReaderContract(doc).Pass }},
		{"duplicate id", strings.Replace(productionFixture, `id="hint"`, `id="query"`, 1), func(doc string) bool { return !CheckFocusContract(doc).Pass }},
		{"positive tabindex", strings.Replace(productionFixture, `<button type="button">`, `<button type="button" tabindex="2">`, 1), func(doc string) bool { return !CheckFocusContract(doc).Pass }},
		{"hidden icon-only control", strings.Replace(productionFixture, `<button type="button">Submit</button>`, `<button type="button"><span aria-hidden="true">●</span></button>`, 1), func(doc string) bool { return !CheckFocusContract(doc).Pass }},
		{"invalid inputmode", strings.Replace(productionFixture, `inputmode="search"`, `inputmode="laser"`, 1), func(doc string) bool { return !CheckInputModes(doc).Pass }},
		{"click-only pseudo-control", strings.Replace(productionFixture, `<button type="button">Submit</button>`, `<div onclick="submit()">Submit</div>`, 1), func(doc string) bool { return !CheckInputModes(doc).Pass }},
		{"missing focus paint", strings.Replace(productionFixture, `:focus-visible{outline:2px solid #000;box-shadow:0 0 0 2px #fff}`, `.button{outline:0}`, 1), func(doc string) bool { return !CheckFocusIndicator(doc).Pass }},
		{"missing reduced-motion override", strings.Replace(productionFixture, `@media (prefers-reduced-motion:reduce){*{animation:none;transition:none;scroll-behavior:auto}}`, `.layout{transition:transform .3s}`, 1), func(doc string) bool { return !CheckMotionContract(doc).Pass }},
		{"wrong locale", productionFixture, func(doc string) bool { return !CheckLocale(doc, "de-DE", "ltr").Pass }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.check(tc.doc) {
				t.Fatalf("mutation was accepted: %#v", SurfaceScore(tc.doc, "en-US", "ltr"))
			}
		})
	}
}

func TestInputModeMatrixRejectsUnknownDriver(t *testing.T) {
	for _, mode := range SupportedInputModes() {
		if !CheckInputModeCompatibility(productionFixture, mode).Pass {
			t.Errorf("supported mode %q failed", mode)
		}
	}
	if CheckInputModeCompatibility(productionFixture, InputMode("eye-tracking")).Pass {
		t.Fatal("unknown input mode accepted")
	}
}
