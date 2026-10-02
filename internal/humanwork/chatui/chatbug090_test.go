package chatui

import (
	"os"
	"strings"
	"testing"
)

// TestTodo_CHATBUG_090: the sidebar's row menu opens and stays open. The cause
// of the dead menu was a document-wide scroll listener that closed it on the
// first scroll of any element, and the message list scrolls by itself right
// after a render; only the sidebar's own scroll (or the page's) may close it.
// The menu is also placed with a visible box beside its button for the first, a
// middle and the last row of a scrolled list, and the section menu is the same
// anchored layer.
func TestTodo_CHATBUG_090(t *testing.T) {
	t.Run("only the sidebar or the page scroll closes the menu", func(t *testing.T) {
		for _, tc := range []struct {
			name                   string
			inMenu, inRail, isPage bool
			want                   bool
		}{
			{"message list scrolls by itself", false, false, false, false},
			{"details pane", false, false, false, false},
			{"the menu's own list", true, true, false, false},
			{"the sidebar list moved the button", false, true, false, true},
			{"the page moved", false, false, true, true},
		} {
			if got := chatbug090RailMenuScrollDismisses(tc.inMenu, tc.inRail, tc.isPage); got != tc.want {
				t.Errorf("%s: dismisses = %v, want %v", tc.name, got, tc.want)
			}
		}
		raw, err := os.ReadFile("events_js.go")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "chatbug090ScrollTargetDismisses(") {
			t.Error("the row menu's dismiss listener does not use the scroll rule")
		}
	})

	t.Run("a visible box beside the button on the first, a middle and the last row", func(t *testing.T) {
		const menuHeight = 581
		width := chatLayerWidth("rail-menu", 1280)
		for _, viewport := range []float64{900, 700, 600} {
			bounds := chatLayerRect{0, 0, 1280, viewport}
			// The list is scrolled: the first row sits just under the heading, a
			// middle row mid-list and the last row right above the Archived band.
			for name, top := range map[string]float64{"first": 252, "middle": viewport / 2, "last": viewport - 66} {
				button := chatLayerRect{left: 323, top: top, right: 349, bottom: top + 28}
				g := chatLayerPlaceWithin(chatLayerPlace(anchoredChatGeometryIn(button, bounds, width, menuHeight, false, false), button, viewport), bounds, viewport)
				if g.width <= 0 || g.maxHeight < 120 {
					t.Errorf("%s row at %.0f px: no usable box: %+v", name, viewport, g)
					continue
				}
				if g.left < 0 || g.left+g.width > 1280 {
					t.Errorf("%s row at %.0f px: box leaves the page sideways: %+v", name, viewport, g)
				}
				shown := min(menuHeight, g.maxHeight)
				boxTop, boxBottom := g.top, g.top+shown
				if g.up {
					boxBottom = viewport - g.bottom
					boxTop = boxBottom - shown
				}
				if boxTop < 0 || boxBottom > viewport {
					t.Errorf("%s row at %.0f px: box %.0f..%.0f leaves the page: %+v", name, viewport, boxTop, boxBottom, g)
				}
				if boxTop < button.bottom && boxBottom > button.top {
					t.Errorf("%s row at %.0f px: box %.0f..%.0f covers its button %.0f..%.0f", name, viewport, boxTop, boxBottom, button.top, button.bottom)
				}
				if gap := min(abs(boxTop-button.bottom), abs(button.top-boxBottom)); gap > 16 {
					t.Errorf("%s row at %.0f px: box is %.0f px from its button, want it next to it", name, viewport, gap)
				}
			}
		}
	})

	t.Run("the section menu is the same anchored layer", func(t *testing.T) {
		m := chat4Fixture("en-US", "answered", false)
		m.RailMenuID = chatside001MenuPrefix + "channels"
		m.Sections = []SidebarSection{{ID: "channels", Name: "Channels"}, {ID: "direct", Name: "Direct messages"}}
		menu := renderNode(t, chatux020RailMenu(m, handlers{}))
		for _, want := range []string{`data-chat-layer="rail-menu"`, `popover="manual"`, `role="menu"`, `class="rail-row-menu"`} {
			if !strings.Contains(menu, want) {
				t.Errorf("the section menu misses %s: %s", want, menu)
			}
		}
	})

	t.Run("one scroll area between fixed bands", func(t *testing.T) {
		for _, want := range []string{
			`.rail-scroll{min-height:96px;scroll-snap-type:y proximity`,
			`.chat-rail>.chatstate-archived{flex:none`,
			`.rail-scroll>.sidebar-section>.section-controls::after`,
			`calc(100% - 12px),transparent)`,
		} {
			if !strings.Contains(Stylesheet, want) {
				t.Errorf("the stylesheet misses %q", want)
			}
		}
	})
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
