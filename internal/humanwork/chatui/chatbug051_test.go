package chatui

import (
	"os"
	"strings"
	"testing"
)

// TestTodo_CHATBUG_051 places every layer kind against an opener near each
// edge of the conversation area and checks the one rule: the layer touches its
// opener, opens toward the side with room, and stays inside the conversation
// area, never over the application header or the sidebar.
func TestTodo_CHATBUG_051(t *testing.T) {
	const viewportWidth, viewportHeight = 1280.0, 800.0
	// The page: application header 0-82, sidebar 72-360, conversation 360-1280.
	area := chatLayerRect{left: 360, top: 82, right: 1280, bottom: 800}
	openers := map[string]chatLayerRect{
		"chip under the header, at the left":  {left: 380, top: 142, right: 556, bottom: 170},
		"header button, at the right":         {left: 1236, top: 92, right: 1268, bottom: 124},
		"composer add button, bottom left":    {left: 388, top: 736, right: 420, bottom: 768},
		"send row, bottom right":              {left: 1170, top: 730, right: 1252, bottom: 772},
		"message bar near the top":            {left: 1040, top: 176, right: 1240, bottom: 208},
		"message bar in the middle":           {left: 1040, top: 400, right: 1240, bottom: 432},
		"reaction chip at the left":           {left: 446, top: 300, right: 494, bottom: 324},
		"a control taller than the room left": {left: 600, top: 90, right: 700, bottom: 790},
	}
	kinds := []string{"todo", "poll", "emoji", "reaction", "menu", "search", "voice", "writing-style", "location", "formatting", "details", "thread", "rail-menu", "saved", chatComposerAddKind, "gif"}
	for _, kind := range kinds {
		for name, opener := range openers {
			for _, rtl := range []bool{false, true} {
				for _, need := range []float64{120, 320, 900} {
					bounds := chatLayerBounds(opener, area, viewportWidth, viewportHeight)
					if bounds != area {
						t.Fatalf("%s: an opener in the conversation is bounded by %+v", name, bounds)
					}
					g := anchoredChatGeometryIn(opener, bounds, chatLayerWidth(kind, viewportWidth), need, chatLayerAbove(kind), rtl)
					where := kind + " from " + name
					if g.left < area.left || g.left+g.width > area.right {
						t.Errorf("%s (rtl=%v, need %.0f): x %.0f-%.0f leaves the conversation %.0f-%.0f", where, rtl, need, g.left, g.left+g.width, area.left, area.right)
					}
					if g.top < area.top || g.top+g.height > area.bottom {
						t.Errorf("%s (rtl=%v, need %.0f): y %.0f-%.0f leaves the conversation %.0f-%.0f", where, rtl, need, g.top, g.top+g.height, area.top, area.bottom)
					}
					// It touches its opener: directly under it, or directly over it.
					under, over := g.top == opener.bottom+4, g.top+g.height == opener.top-10
					squeezed := g.height == 0 || opener.bottom-opener.top > 600
					if !under && !over && !squeezed {
						t.Errorf("%s (rtl=%v, need %.0f): top %.0f height %.0f touches neither side of opener %.0f-%.0f", where, rtl, need, g.top, g.height, opener.top, opener.bottom)
					}
					// It opens toward the side with more room when its own side cannot hold it.
					roomBelow, roomAbove := area.bottom-opener.bottom-12, opener.top-area.top-18
					if need <= max(roomBelow, roomAbove) && g.height < need {
						t.Errorf("%s (rtl=%v): needs %.0f, has %.0f above and %.0f below, was given %.0f", where, rtl, need, roomAbove, roomBelow, g.height)
					}
					placed := chatLayerPlaceWithin(chatLayerPlace(g, opener, viewportHeight), bounds, viewportHeight)
					if placed.up && viewportHeight-placed.bottom-placed.maxHeight < area.top {
						t.Errorf("%s: grows upward past the conversation top (max height %.0f)", where, placed.maxHeight)
					}
					if !placed.up && placed.top+placed.maxHeight > area.bottom {
						t.Errorf("%s: grows downward past the conversation bottom", where)
					}
				}
			}
		}
	}

	// The three reported cases, with the numbers from the report.
	// The poll card opened from the composer's add button sat at x=19.
	add := openers["composer add button, bottom left"]
	if g := anchoredChatGeometryIn(add, area, chatLayerWidth("poll", viewportWidth), 240, false, false); g.left != area.left+8 || g.top+g.height != add.top-10 {
		t.Fatalf("poll card from the add button: left %.0f top %.0f height %.0f", g.left, g.top, g.height)
	}
	// Opened from the chip, its left 160px lay over the sidebar.
	chip := openers["chip under the header, at the left"]
	if g := anchoredChatGeometryIn(chip, area, chatLayerWidth("todo", viewportWidth), 300, false, false); g.left != area.left+8 || g.top != chip.bottom+4 {
		t.Fatalf("to-do card from its chip: left %.0f top %.0f", g.left, g.top)
	}
	// A message's menu near the top opened upward over the header. Inside the
	// messages the area starts under the conversation header (here at 134).
	messages := chatLayerRect{left: 360, top: 134, right: 1280, bottom: 800}
	bar := openers["message bar near the top"]
	if g := anchoredChatGeometryIn(bar, messages, chatLayerWidth("menu", viewportWidth), 280, true, false); g.top != bar.bottom+4 || g.height != 280 {
		t.Fatalf("message menu near the top: top %.0f height %.0f, want it below its bar", g.top, g.height)
	}

	// An opener outside the conversation (the sidebar, a page control) keeps the
	// whole viewport, and a page with no conversation area measured does too.
	viewport := chatLayerRect{left: 0, top: 0, right: viewportWidth, bottom: viewportHeight}
	if got := chatLayerBounds(chatLayerRect{left: 90, top: 700, right: 340, bottom: 730}, area, viewportWidth, viewportHeight); got != viewport {
		t.Fatalf("sidebar opener bounded by %+v", got)
	}
	if got := chatLayerBounds(add, chatLayerRect{}, viewportWidth, viewportHeight); got != viewport {
		t.Fatalf("no area measured: %+v", got)
	}
	// The viewport rule is the same rule: nothing that used it has moved.
	for name, opener := range openers {
		for _, above := range []bool{false, true} {
			if anchoredChatGeometry(opener, 360, 300, viewportWidth, viewportHeight, above, false) != anchoredChatGeometryIn(opener, viewport, 360, 300, above, false) {
				t.Fatalf("%s: the viewport placement changed", name)
			}
		}
	}
	// When the opener scrolls out of the area its layer is hidden, not left
	// over the header, and it shows again when the opener is back.
	if !chatLayerAnchorGone(chatLayerRect{left: 1040, top: 40, right: 1240, bottom: 72}, messages) || !chatLayerAnchorGone(chatLayerRect{left: 1040, top: 810, right: 1240, bottom: 842}, messages) || chatLayerAnchorGone(bar, messages) {
		t.Fatal("an opener that left the conversation still shows its layer, or one inside hides it")
	}
}

// The browser half uses the tested rule and measures the conversation area.
func TestTodo_CHATBUG_051_Browser(t *testing.T) {
	source, err := os.ReadFile("agentux_chat5_layers_js.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	position := text[strings.Index(text, "func positionChatLayer("):strings.Index(text, "func chatLayerArea(")]
	for _, want := range []string{"chatLayerBounds(anchorRect, chatLayerArea(root, opener)", "anchoredChatGeometryIn(anchorRect, bounds", "chatLayerPlaceWithin(", "chatLayerAnchorGone(anchorRect, bounds)"} {
		if !strings.Contains(position, want) {
			t.Errorf("positionChatLayer does not use %s", want)
		}
	}
	if strings.Contains(position, "anchoredChatGeometry(anchorRect") {
		t.Error("a layer is still placed against the whole viewport")
	}
	areaBody := text[strings.Index(text, "func chatLayerArea("):strings.Index(text, "func syncChatAnchoredLayers(")]
	for _, want := range []string{`".chat-main"`, `".chat-side"`, `".conversation-header"`, `".timeline-frame,.chat-composer"`} {
		if !strings.Contains(areaBody, want) {
			t.Errorf("the conversation area is measured without %s", want)
		}
	}
	// The channel cards find their chip when nothing else is remembered.
	if sync := text[strings.Index(text, "func syncChatAnchoredLayers("):]; !strings.Contains(sync, `"[data-action=tray-" + kind + "]"`) {
		t.Error("the channel card has no opener to fall back to")
	}
}
