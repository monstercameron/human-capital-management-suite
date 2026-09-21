package journey

import (
	"strings"
	"testing"
)

// TestConfirmSurfaceIsCenteredWithoutTransform pins the live defect behind
// the edit-proposal dialog opening at the viewport centre and running off
// the screen: .jn-confirm-surface centred itself with
// transform:translate(-50%,-50%), and its jn-slidein animation ends at
// transform:none with fill-mode both, which replaced that translate. The
// panel is now a full-width bottom sheet in the narrow base case and, from
// 30.0625rem (481px), centred by inset and auto margins; nothing positional depends on
// transform, the height is bounded by the dynamic viewport where supported,
// and the action bar stays pinned inside the scrolling body.
func TestConfirmSurfaceIsCenteredWithoutTransform(t *testing.T) {
	css := Stylesheet()
	surface := cssRule(t, css, ".jn-confirm-surface")
	for _, forbidden := range []string{"transform:", "top:50%", "left:50%"} {
		if strings.Contains(surface, forbidden) {
			t.Fatalf("the confirm panel positions itself with %q, which its own animation overrides: %s", forbidden, surface)
		}
	}
	for _, want := range []string{"position:fixed", "inset:auto 0 0 0", "width:100%", "max-height:calc(100vh - 2rem)", "overflow-y:auto", "height:fit-content"} {
		if !strings.Contains(surface, want) {
			t.Fatalf("narrow confirm sheet missing %q: %s", want, surface)
		}
	}
	if !strings.Contains(css, ":root .jn-confirm-surface{max-height:calc(100dvh - 2rem);}") {
		t.Fatal("the panel height is not bounded by the dynamic viewport")
	}
	desktop := "@media (min-width:30.0625rem){:root .jn-confirm-surface{"
	at := strings.Index(css, desktop)
	if at < 0 {
		t.Fatal("no centred layout from 30.0625rem")
	}
	rule := css[at : at+strings.Index(css[at:], "}}")]
	for _, want := range []string{"inset:0", "margin:auto", "width:min(34rem,calc(100vw - 2rem))"} {
		if !strings.Contains(rule, want) {
			t.Fatalf("centred confirm panel missing %q: %s", want, rule)
		}
	}
	inner := cssRule(t, css, ":root .jn-confirm-surface .jn-confirm-actionbar:where(*)")
	for _, want := range []string{"bottom:-1rem", "margin-bottom:-1rem", "padding-bottom:1.625rem"} {
		if !strings.Contains(inner, want) {
			t.Fatalf("the pinned action bar leaves the panel padding uncovered; missing %q: %s", want, inner)
		}
	}
	actionbar := cssRule(t, css, ".jn-confirm-actionbar")
	if !strings.Contains(actionbar, "position:sticky") || !strings.Contains(actionbar, "bottom:0") {
		t.Fatalf("the action bar is no longer pinned: %s", actionbar)
	}
}
