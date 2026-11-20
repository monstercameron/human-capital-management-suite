package chatui

import (
	"strings"
	"testing"
)

// TestIssue82_PrefsPopoverStaysInViewport: the Chat preferences panel is placed
// inside the viewport at every supported width and direction, and the panel's CSS
// never asks for more than the placement gives it (no 340 px minimum, no RTL
// offset that pushes it past the edge).
func TestIssue82_PrefsPopoverStaysInViewport(t *testing.T) {
	const viewportHeight = 900.0
	for _, width := range []float64{1440, 800, 390, 320} {
		for _, rtl := range []bool{false, true} {
			column := chatLayerRect{left: 0, top: 0, right: min(width, 300), bottom: viewportHeight}
			opener := chatLayerRect{left: 40, top: 8, right: 72, bottom: 44}
			if width == 800 {
				column = chatLayerRect{left: 496, top: 0, right: 728, bottom: viewportHeight}
				opener = chatLayerRect{left: 688, top: 8, right: 720, bottom: 44}
			}
			if rtl && width != 800 {
				column = chatLayerRect{left: max(0, width-min(width, 300)), top: 0, right: width, bottom: viewportHeight}
				opener = chatLayerRect{left: column.right - 72, top: 8, right: column.right - 40, bottom: 44}
			}
			p := chatSidebarLayerPlacement(opener, column, width, viewportHeight, rtl)
			// The CSS minimum is min(220px, 100vw-16px); the layer is never narrower.
			rendered := max(p.width, min(220, width-16))
			if p.left < 0 || p.left+rendered > width {
				t.Errorf("width %.0f rtl=%v: panel spans [%.0f,%.0f] in a %.0f px viewport", width, rtl, p.left, p.left+rendered, width)
			}
		}
	}
	for _, bad := range []string{"min-inline-size:340px", "min-width:340px", "translateX(-8px)", "translateX(8px)"} {
		if strings.Contains(ChatUX031Styles, bad) {
			t.Errorf("the preferences styles still carry %q", bad)
		}
	}
	if !strings.Contains(ChatUX031Styles, `[data-chat-layer="chat-prefs"]{padding:0;min-inline-size:min(220px,calc(100vw - 16px))`) {
		t.Error("the preferences minimum width no longer follows the viewport")
	}
}
