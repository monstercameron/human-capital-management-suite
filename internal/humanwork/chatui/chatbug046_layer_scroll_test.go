package chatui

import "testing"

// TestTodo_CHATBUG_046 pins the rule that keeps a sidebar panel scrollable: once
// it has an anchor it is never given the general width first, because the
// sidebar placement is narrower and the wider intermediate layout shortened the
// panel and reset its scroll position on every scroll event.
func TestTodo_CHATBUG_046(t *testing.T) {
	for _, kind := range []string{"quiet-hours", "reading-languages", chatux002PrefsKind, chatux002ChannelsKind} {
		if chatLayerWritesGeneralWidth(kind, true) {
			t.Errorf("anchored sidebar panel %q is given the general width before its placement", kind)
		}
		if !chatLayerWritesGeneralWidth(kind, false) {
			t.Errorf("sidebar panel %q without an anchor has no width at all", kind)
		}
	}
	for _, kind := range []string{"menu", "emoji", "reaction", "search", "poll"} {
		if !chatLayerWritesGeneralWidth(kind, true) || !chatLayerWritesGeneralWidth(kind, false) {
			t.Errorf("layer %q is measured at the general width and must be given it first", kind)
		}
	}
	// The two widths really differ in a sidebar of ordinary width, which is what
	// made the intermediate layout visible.
	column := chatLayerRect{left: 60, top: 60, right: 272, bottom: 900}
	placed := chatSidebarLayerPlacement(chatLayerRect{left: 70, top: 80, right: 260, bottom: 104}, column, 1280, 900, false)
	if general := chatLayerWidth("reading-languages", 1280); placed.width >= general {
		t.Fatalf("sidebar placement width %.0f is not narrower than the general width %.0f", placed.width, general)
	}
}
