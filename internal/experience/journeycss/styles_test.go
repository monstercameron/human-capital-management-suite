package journeycss

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/tokens"
)

// TestTextPairsMeetAA scores every foreground/background combination the
// page declares against WCAG 2.2 AA for normal text (1.4.3, 4.5:1) using
// tokens.ContrastRatio -- the same relative-luminance math the workspace
// palette is gated with, so both surfaces are held to one standard.
//
// The list is only as honest as it is complete: a renderer that paints a
// combination not in TextPairs is a gap this test cannot see, which is why
// TestStylesheetUsesOnlyPaletteHexes exists beside it.
func TestTextPairsMeetAA(t *testing.T) {
	for _, pair := range TextPairs() {
		t.Run(pair.Purpose, func(t *testing.T) {
			ratio, err := tokens.ContrastRatio(pair.Foreground.Hex, pair.Background.Hex)
			if err != nil {
				t.Fatalf("ContrastRatio(%s, %s): %v", pair.Foreground.Hex, pair.Background.Hex, err)
			}
			if ratio < tokens.MinRatioNormalText {
				t.Errorf("%s: %s (%s) on %s (%s) = %.2f:1, want at least %.1f:1",
					pair.Purpose, pair.Foreground.Name, pair.Foreground.Hex,
					pair.Background.Name, pair.Background.Hex, ratio, tokens.MinRatioNormalText)
			}
		})
	}
}

// TestUIPairsMeetNonTextAA holds the boundaries that carry meaning without
// text -- control edges, the focus ring, step markers -- to WCAG 2.2 AA
// non-text contrast (1.4.11, 3:1).
func TestUIPairsMeetNonTextAA(t *testing.T) {
	for _, pair := range UIPairs() {
		t.Run(pair.Purpose, func(t *testing.T) {
			ratio, err := tokens.ContrastRatio(pair.Foreground.Hex, pair.Background.Hex)
			if err != nil {
				t.Fatalf("ContrastRatio(%s, %s): %v", pair.Foreground.Hex, pair.Background.Hex, err)
			}
			if ratio < tokens.MinRatioLargeText {
				t.Errorf("%s: %s (%s) on %s (%s) = %.2f:1, want at least %.1f:1",
					pair.Purpose, pair.Foreground.Name, pair.Foreground.Hex,
					pair.Background.Name, pair.Background.Hex, ratio, tokens.MinRatioLargeText)
			}
		})
	}
}

func TestSwatchesAreWellFormedAndUnique(t *testing.T) {
	hexPattern := regexp.MustCompile(`^#[0-9a-f]{6}$`)
	byName := make(map[string]string)
	for _, s := range Swatches() {
		if !hexPattern.MatchString(s.Hex) {
			t.Errorf("swatch %q has hex %q, want lowercase #rrggbb", s.Name, s.Hex)
		}
		if prev, dup := byName[s.Name]; dup {
			t.Errorf("swatch name %q declared twice (%s and %s)", s.Name, prev, s.Hex)
		}
		byName[s.Name] = s.Hex
	}
	if len(byName) == 0 {
		t.Fatal("Swatches() is empty")
	}
}

// TestStylesheetDeclaresEverySwatch is the join between the two
// representations of the palette: the Go values the contrast tests score
// and the CSS custom properties the browser actually paints with. Without
// it a swatch could be corrected in Go and left stale in the CSS, and every
// contrast assertion above would still pass while the page failed.
func TestStylesheetDeclaresEverySwatch(t *testing.T) {
	css := Stylesheet()
	for _, s := range Swatches() {
		decl := fmt.Sprintf("--jn-%s:%s;", s.Name, s.Hex)
		if !strings.Contains(css, decl) {
			t.Errorf("stylesheet does not declare %q", decl)
		}
	}
}

// TestStylesheetUsesOnlyPaletteHexes catches the other direction: a color
// hard-coded into a rule instead of taken from the palette would never be
// scored for contrast at all.
func TestStylesheetUsesOnlyPaletteHexes(t *testing.T) {
	declared := make(map[string]bool, len(Swatches()))
	for _, s := range Swatches() {
		declared[strings.ToLower(s.Hex)] = true
	}
	hexPattern := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	for _, match := range hexPattern.FindAllString(Stylesheet(), -1) {
		if !declared[strings.ToLower(match)] {
			t.Errorf("stylesheet uses hex %s, which is not a palette swatch", match)
		}
	}
}

// TestStylesheetIsSafeToInline guards the two things that would break the
// document Document writes: a closing style tag ends the block early, and a
// backtick cannot appear inside the Go raw string that holds it.
func TestStylesheetIsSafeToInline(t *testing.T) {
	css := Stylesheet()
	if css == "" {
		t.Fatal("Stylesheet() is empty")
	}
	if strings.Contains(strings.ToLower(css), "</style") {
		t.Error("stylesheet contains a closing style tag")
	}
	if strings.Contains(css, "`") {
		t.Error("stylesheet contains a backtick")
	}
	if strings.Contains(css, "\r") {
		t.Error("stylesheet contains a carriage return; the CSP hash is over the LF form")
	}
}

// TestStylesheetLoadsNothingExternal restates the CSP in a test: under
// `default-src 'none'` any url(), @import or @font-face would resolve to a
// blocked request and a silently missing asset.
func TestStylesheetLoadsNothingExternal(t *testing.T) {
	css := strings.ToLower(Stylesheet())
	for _, forbidden := range []string{"url(", "@import", "@font-face", "http://", "https://"} {
		if strings.Contains(css, forbidden) {
			t.Errorf("stylesheet contains %q; the content-security-policy blocks every external load", forbidden)
		}
	}
}

// TestStylesheetUsesTheSystemFontStacks checks that the two declared faces
// are the system stacks the brief pins, not a webfont name that would
// silently fall back.
func TestStylesheetUsesTheSystemFontStacks(t *testing.T) {
	css := Stylesheet()
	want := []string{
		`--jn-font:system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;`,
		`--jn-mono:ui-monospace,"Cascadia Mono",Consolas,monospace;`,
	}
	for _, decl := range want {
		if !strings.Contains(css, decl) {
			t.Errorf("stylesheet does not declare %q", decl)
		}
	}
}

// TestBreakpointsOnlyAddColumns keeps the narrow layout the base case. A
// max-width query would mean the wide layout is written first and undone
// below the breakpoint, which is how 320px reflow regressions get in.
func TestBreakpointsOnlyAddColumns(t *testing.T) {
	queries := regexp.MustCompile(`@media\s*\(([^)]*)\)`).FindAllStringSubmatch(Stylesheet(), -1)
	if len(queries) == 0 {
		t.Fatal("stylesheet declares no media queries; the page cannot be responsive")
	}
	for _, q := range queries {
		condition := q[1]
		switch {
		case strings.Contains(condition, "min-width"):
		case strings.Contains(condition, "prefers-reduced-motion"):
		default:
			t.Errorf("media query (%s) is neither a min-width nor a motion preference", condition)
		}
	}
}

// TestStylesheetSizesInRelativeUnits checks the type scale is zoomable: a
// font-size in px does not respond to the browser's text-size setting
// (WCAG 1.4.4).
func TestStylesheetSizesInRelativeUnits(t *testing.T) {
	pxFontSize := regexp.MustCompile(`font-size:\s*[0-9.]+px`)
	if match := pxFontSize.FindString(Stylesheet()); match != "" {
		t.Errorf("stylesheet sets a pixel font size (%s); text zoom would not reach it", match)
	}
}

// TestStylesheetAnimationNamesHaveKeyframes proves every animation the
// stylesheet references resolves to an @keyframes block it also emits.
// GWC hashes @keyframes names by content, so a frame edit renames the
// block; this test fails loudly instead of shipping a dangling
// animation-name. It also re-pins the hashed names, so an upstream hash
// change requires an explicit re-pin, not a silent unmatch.
func TestStylesheetAnimationNamesHaveKeyframes(t *testing.T) {
	css := Stylesheet()
	pinned := map[string]string{
		"jn-slidein":    jnSlideinHash,
		"jn-pop":        jnPopHash,
		"jn-grow-y":     jnGrowYHash,
		"jn-grow-x":     jnGrowXHash,
		"jn-grow-x-svg": jnGrowXSVGHash,
		"jn-drop":       jnDropHash,
		"jn-sweep":      jnSweepHash,
		"jn-halo":       jnHaloHash,
		"jn-amber":      jnAmberHash,
		"jn-alert":      jnAlertHash,
		"jn-breathe":    jnBreatheHash,
	}
	for base, hash := range pinned {
		if hash == "" {
			t.Errorf("pinned hash for %s is empty", base)
			continue
		}
		block := "@keyframes " + base + "-" + hash + "{"
		if !strings.Contains(css, block) {
			t.Errorf("stylesheet has no live block %q (frames or hash changed; re-pin jnKeyframesHash)", block)
		}
	}
	namePattern := regexp.MustCompile(`(?:animation-name|animation):([^;{}]+)`)
	nameShape := regexp.MustCompile(`^jn-[a-z-]+-[0-9a-z]+$`)
	seen := map[string]bool{}
	for _, match := range namePattern.FindAllStringSubmatch(css, -1) {
		// Each comma-separated animation takes its name from its first
		// token (the dual-animation meter rule names two).
		for _, segment := range strings.Split(match[1], ",") {
			fields := strings.Fields(segment)
			if len(fields) == 0 {
				continue
			}
			name := fields[0]
			if !nameShape.MatchString(name) || seen[name] {
				continue
			}
			seen[name] = true
			if !strings.Contains(css, "@keyframes "+name+"{") {
				t.Errorf("animation-name %q has no matching @keyframes block", name)
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("found no animation-name references; the stylesheet lost its motion")
	}
}

func TestResponsiveCompositionProtectsEveryJourneySurface(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`:where(.jn-page,.jn-embedded) :is(img,svg,video,canvas){max-width:100%;}`,
		`:where(.jn-shell,.jn-page,.jn-pagehead,.jn-card,.jn-cardhead,.jn-grid,.jn-griditem,`,
		`:where(.jn-page,.jn-embedded) :is(input,select,textarea,button){max-width:100%;}`,
		`:where(.jn-pagehead,.jn-cardhead,.jn-toolbar,.jn-actions,.jn-provenance){flex-wrap:wrap;}`,
		`.jn-tablewrap{max-width:100%;overscroll-behavior-inline:contain;scrollbar-width:thin;}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("journey responsive composition missing %q", want)
		}
	}
}

// TestJourneyKeyframesResolve guards the runtime contract behind GWC's
// content-hashed animation names: every animation-name the sheet assigns and
// every shorthand name reference must have a matching @keyframes block.
// The danger-tone meter keeps one dual-animation Raw shorthand (two Keyframes
// rules would emit competing animation-name declarations), so this test is
// what fails loudly if a frames edit ever desyncs a pinned hash.
func TestJourneyKeyframesResolve(t *testing.T) {
	css := Stylesheet()
	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`@keyframes\s+([A-Za-z0-9_-]+)`).FindAllStringSubmatch(css, -1) {
		defined[m[1]] = true
	}
	if len(defined) == 0 {
		t.Fatal("journey stylesheet defines no keyframes")
	}
	for _, m := range regexp.MustCompile(`animation-name:([A-Za-z0-9_-]+)`).FindAllStringSubmatch(css, -1) {
		if !defined[m[1]] {
			t.Errorf("animation-name %q has no @keyframes block", m[1])
		}
	}
	for _, m := range regexp.MustCompile(`animation:(jn-[A-Za-z0-9_-]+)`).FindAllStringSubmatch(css, -1) {
		if !defined[m[1]] {
			t.Errorf("plain animation reference %q has no @keyframes block", m[1])
		}
	}
	for _, m := range regexp.MustCompile(`both,(jn-[A-Za-z0-9_-]+)`).FindAllStringSubmatch(css, -1) {
		if !defined[m[1]] {
			t.Errorf("second animation reference %q has no @keyframes block", m[1])
		}
	}
}
