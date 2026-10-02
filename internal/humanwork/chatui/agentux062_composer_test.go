package chatui

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestTodo_AGENTUX_062_ComposerPlacement checks the one rule for layers opened
// from the composer at 900 px and 600 px window heights, at phone width and in
// both directions: the layer is pinned 8 px above the composer's top edge,
// stays inside the conversation column, and its room is the space between the
// header and the composer (it scrolls inside).
func TestTodo_AGENTUX_062_ComposerPlacement(t *testing.T) {
	type window struct {
		name         string
		width, high  float64
		column       chatLayerRect // conversation column under its header
		composerLeft float64
	}
	cases := []window{
		{"900 high", 1440, 900, chatLayerRect{360, 134, 1440, 900}, 380},
		{"600 high", 1280, 600, chatLayerRect{360, 134, 1280, 600}, 380},
		{"phone", 375, 667, chatLayerRect{0, 120, 375, 667}, 12},
	}
	for _, c := range cases {
		composer := chatLayerRect{c.composerLeft, c.high - 96, c.column.right - 12, c.high - 12}
		for _, rtl := range []bool{false, true} {
			opener := chatLayerRect{c.composerLeft + 10, c.high - 70, c.composerLeft + 42, c.high - 38}
			if rtl {
				opener = chatLayerRect{composer.right - 42, c.high - 70, composer.right - 10, c.high - 38}
			}
			for _, kind := range []string{chatComposerAddKind, "gif", "voice", "writing-style", "location", "formatting"} {
				if !chatComposerLayerKind(kind) || !chatLayerAbove(kind) && kind != "formatting" {
					t.Errorf("%s is not a composer layer that opens above", kind)
				}
				width := chatLayerWidth(kind, c.width)
				for _, need := range []float64{120, 400, 1200} {
					p := chatComposerLayerPlacement(opener, composer, c.column, width, need, c.high, rtl)
					where := fmt.Sprintf("%s rtl=%v %s need=%.0f: %+v", c.name, rtl, kind, need, p)
					if !p.up || c.high-p.bottom != composer.top-chatComposerGap {
						t.Errorf("%s: bottom edge is not %d px above the composer", where, chatComposerGap)
					}
					room := composer.top - chatComposerGap - (c.column.top + 8)
					if p.maxHeight != room {
						t.Errorf("%s: room %.0f, want %.0f", where, p.maxHeight, room)
					}
					shown := min(need, p.maxHeight)
					if c.high-p.bottom-shown < c.column.top+8 {
						t.Errorf("%s: reaches over the conversation header", where)
					}
					if p.left < c.column.left+8 || p.left+p.width > c.column.right-8 {
						t.Errorf("%s: leaves the conversation column", where)
					}
					wantLeft := opener.left
					if rtl {
						wantLeft = opener.right - p.width
					}
					wantLeft = max(c.column.left+8, min(wantLeft, c.column.right-p.width-8))
					if p.left != wantLeft {
						t.Errorf("%s: starts at %.0f, want the opener's inline start %.0f", where, p.left, wantLeft)
					}
				}
			}
		}
	}
	// The completion lists are cut to the same room.
	if got := chatComposerRoom(chatLayerRect{380, 504, 1268, 588}, chatLayerRect{360, 134, 1280, 600}); got != 504-8-142 {
		t.Errorf("room for the completion lists = %.0f", got)
	}
}

// TestTodo_AGENTUX_062_ComposerLayerWiring reads the places that must agree: the
// kinds are real layers, each closes by Escape (or has its own Escape), and the
// shared placement uses the composer rule.
func TestTodo_AGENTUX_062_ComposerLayerWiring(t *testing.T) {
	if !chatPolishManagedLayer(chatComposerAddKind) || !chatLayerOutsideDismisses(chatComposerAddKind) {
		t.Error("the Add menu closes neither with Escape nor with an outside press")
	}
	if chatLayerKind("giphy-toggle") != "gif" {
		t.Error("the GIF button is not remembered as the GIF picker's opener")
	}
	for name, wants := range map[string][]string{
		"composer_tools.go":          {"chatComposerAddKind, rows..."},
		"giphy_picker_view.go":       {`"gif",`},
		"agentux_chat5_layers_js.go": {"chatComposerPlacementFor(opener, kind, bounds"},
		"agentux062_composer_js.go":  {"--chat-composer-room", "closeStaleChatPopovers"},
		"agentux062_composer.go":     {"max-block-size:min(372px,50vh,var(--chat-composer-room"},
		"render.go":                  {"giphy.closeOnOutsidePress(e)"},
		"giphy_picker_view_js.go":    {"handleKey", `key != "Escape"`},
		"agentux_chat5_layers.go":    {"chatComposerLayerPlace("},
	} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(string(raw), want) {
				t.Errorf("%s lacks %s", name, want)
			}
		}
	}
	add := renderNode(t, composerAddControl(composerToolsModel("en-US", false), "chat-composer", false))
	chat4Require(t, add, `data-chat-layer="add-menu"`, `popover="manual"`, `data-chat-disclosure-body`, `role="menu"`)
}
