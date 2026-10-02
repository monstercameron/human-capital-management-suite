//go:build !(js && wasm)

package journeycss

import (
	"strings"
	"sync"
)

// Stylesheet returns the page's CSS as one string so the server can pin its
// sha256 in the content-security-policy's style-src. The string is built
// from typed GWC declarations (see typed_journey_a/b/c.go) and memoized, so
// every call returns byte-identical bytes cheaply; the guarantees the old
// literal const gave (exact rule order, hashed @keyframes names) are
// preserved by TestStylesheetAnimationNamesHaveKeyframes and the content
// assertions below.
//
// The browser bundle does not run this builder: it returns the same bytes
// from stylesheet_blob.go (see stylesheet_wasm.go), which
// TestStylesheetBlobMatchesBuilder keeps identical to this result.
var stylesheetOnce = sync.OnceValue(journeyTypedStylesheet)

func Stylesheet() string { return stylesheetOnce() }

// journeyTypedStylesheet assembles the whole page's CSS in original rule
// order: tokens and base rules, forms and tables, then motion and print.
func journeyTypedStylesheet() string {
	var out strings.Builder
	grow := func(declare func()) string { return buildTypedSheet(declare) }
	out.WriteString(grow(declareJourneyTokens))
	out.WriteString(grow(declareJourneyForms))
	out.WriteString(grow(declareJourneyNotes))
	out.WriteString(grow(declareJourneyMotion))
	out.WriteString(grow(declareJourneyTracker))
	out.WriteString(grow(declareJourneyReview))
	return out.String()
}
