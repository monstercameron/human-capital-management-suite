package chatui

import (
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// chatbugFuncBody returns the source of one function of a wasm-only file, so a
// rule that cannot run without a browser (nothing waits for a timer; the
// document hears Escape) can still be pinned by a test that fails when it goes.
func chatbugFuncBody(t *testing.T, file, name string) string {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func "+name+"(")
	if start < 0 {
		t.Fatalf("%s no longer defines %s", file, name)
	}
	end := strings.Index(source[start+1:], "\nfunc ")
	if end < 0 {
		return source[start:]
	}
	return source[start : start+1+end]
}

// chatbugShape writes the element structure of a node down to depth, ignoring
// text, so two renders that a reconciler would patch in place compare equal.
func chatbugShape(n *xhtml.Node, depth int) string {
	if n == nil || n.Type != xhtml.ElementNode {
		return ""
	}
	out := n.Data + "." + chatPolishAttr(n, "class")
	if chatPolishAttr(n, "data-chat-layer") != "" {
		out += "#layer"
	}
	if depth > 0 {
		var kids []string
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if shape := chatbugShape(c, depth-1); shape != "" {
				kids = append(kids, shape)
			}
		}
		out += "[" + strings.Join(kids, ",") + "]"
	}
	return out
}

func chatbugDisclosure(t *testing.T, markup, class string) *xhtml.Node {
	t.Helper()
	found := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
		return chatPolishAttr(n, "data-chat-disclosure") == "true" && chatPolishHasClass(n, class)
	})
	if len(found) != 1 {
		t.Fatalf("%d disclosures of class %q", len(found), class)
	}
	return found[0]
}

func TestTodo_CHATBUG_026(t *testing.T) {
	// RED: the two sidebar panels were placed by the generic anchor rule, which
	// right-aligns a 360 px layer to a row that sits in a 280 px column. The panel
	// started at the left edge of the page and covered the rail, the sidebar and
	// part of the composer.
	column := chatLayerRect{0, 0, 280, 600}
	reading := chatLayerRect{0, 560, 280, 594}
	old := anchoredChatGeometry(reading, 360, 300, 800, 600, false, false)
	if old.left+old.width <= column.right {
		t.Fatalf("premise changed: the generic rule no longer overflows the sidebar column: %+v", old)
	}
	for _, size := range [][2]float64{{1440, 900}, {800, 600}, {390, 844}, {320, 568}} {
		for _, rtl := range []bool{false, true} {
			width, height := size[0], size[1]
			col := chatLayerRect{0, 0, min(280, width), height}
			if rtl {
				col = chatLayerRect{width - min(280, width), 0, width, height}
			}
			row := chatLayerRect{col.left, height - 40, col.right, height - 6}
			p := chatSidebarLayerPlacement(row, col, width, height, rtl)
			if !p.up || p.left < col.left || p.left+p.width > col.right || p.left < 8 || p.left+p.width > width-8 {
				t.Fatalf("%vx%v rtl=%v: panel leaves the sidebar column %+v: %+v", width, height, rtl, col, p)
			}
			if edge := height - p.bottom; edge > row.top-6+0.001 || edge < row.top-6-0.001 {
				t.Fatalf("%vx%v: panel is not directly above its row: bottom edge %v, row top %v", width, height, edge, row.top)
			}
			if p.maxHeight <= 0 || p.maxHeight > row.top-8+0.001 {
				t.Fatalf("%vx%v: panel may not grow above the page: %+v", width, height, p)
			}
		}
	}

	// One at a time: opening either closes the other and nothing else.
	if chatLayerGroup("quiet-hours") != chatLayerGroup("reading-languages") || chatLayerGroup("reaction") == chatLayerGroup("menu") {
		t.Fatal("the two sidebar panels are one group and no other kinds are")
	}
	open := []string{"quiet-hours", "", "menu", "todo"}
	if got := chatLayersToClose(open, "reading-languages"); len(got) != 1 || got[0] != 0 {
		t.Fatalf("opening Reading languages closes %v, want only Quiet hours (index 0)", got)
	}
	if got := chatLayersToClose([]string{"reading-languages", ""}, "reading-languages"); len(got) != 1 || got[0] != 0 {
		t.Fatalf("reopening a panel closes %v", got)
	}
	if got := chatLayersToClose([]string{"menu", "", "todo"}, "quiet-hours"); got != nil {
		t.Fatalf("a menu and a tray are not closed by a sidebar panel: %v", got)
	}
	if !chatLayerOutsideDismisses("quiet-hours") || !chatLayerOutsideDismisses("reading-languages") || chatLayerOutsideDismisses("todo") || chatLayerOutsideDismisses("reaction") {
		t.Fatal("a click elsewhere closes the sidebar panels and only them")
	}

	// A managed layer is the one Escape closes first, whatever the document order.
	top := chatPolishTopLayer([]chatPolishLayer{{kind: "quiet-hours", order: 3, visible: true}, {kind: "menu", order: 1, visible: true}, {kind: "todo", order: 2, visible: false}})
	if top != 0 {
		t.Fatalf("topmost layer = %d, want the newest visible one (0)", top)
	}
	for _, kind := range []string{"quiet-hours", "reading-languages"} {
		if !chatPolishManagedLayer(kind) {
			t.Fatalf("%s is not closed by Escape", kind)
		}
	}

	// Focus: a restore that arrives after the person clicked the composer is dropped.
	if chatFocusRestoreAllowed(chatFocusHolder{Idle: false, Connected: true}) {
		t.Fatal("focus was taken back from a control the person had focused")
	}
	if !chatFocusRestoreAllowed(chatFocusHolder{Idle: true, Connected: false}) || !chatFocusRestoreAllowed(chatFocusHolder{Idle: false, Connected: false}) {
		t.Fatal("focus must return to the row when nothing holds it")
	}

	// The reading row: no service, no row; loading is shown inside the panel.
	props := renderingPersonalProps{Locale: "en-US", Conversation: "general"}
	for name, state := range map[string]renderingPersonalState{"unavailable": {Unavailable: true}, "checking": {Checking: true}} {
		markup := chatPolishMarkup(t, renderingPersonalView(props, state, ui.Handler{}, ui.Handler{}, nil), 800, "light")
		if strings.Contains(markup, "chat-disclosure-button") || strings.Contains(markup, RenderingText("en-US", "title")) || strings.Contains(markup, "disabled") {
			t.Fatalf("%s: a row is still drawn (greyed or not): %s", name, markup)
		}
	}
	loaded := chatPolishMarkup(t, renderingPersonalView(props, renderingPersonalState{}, ui.Handler{}, ui.Handler{}, RenderingLanguageStatusIndicator("en-US", map[string]int{"en": 2}, false, false)), 800, "light")
	loading := chatPolishMarkup(t, renderingPersonalView(props, renderingPersonalState{Loading: true, LanguagesLoading: true}, ui.Handler{}, ui.Handler{}, RenderingLanguageStatusIndicator("en-US", nil, true, false)), 800, "light")
	if !strings.Contains(loading, RenderingText("en-US", "loading")) || !strings.Contains(loading, RenderingText("en-US", "languages_loading")) {
		t.Fatal("loading is not shown")
	}
	loadedBody, loadingBody := chatbugDisclosure(t, loaded, "chatrender-personal"), chatbugDisclosure(t, loading, "chatrender-personal")
	// The panel's own elements are the same whether it is loading or loaded; a
	// reconciler patches them in place and the open panel is never replaced
	// (and so never reset to closed) by a state change.
	if a, b := chatbugShape(loadedBody, 2), chatbugShape(loadingBody, 2); a != b {
		t.Fatalf("a state change would replace the open panel:\n%s\n%s", a, b)
	}
	// CHATUX-002 changed this assertion and the one after the failed render: the
	// reading settings are a section of the one Chat preferences panel, not a
	// popover of their own, so the section is looked for instead of the layer.
	// CHATBUG-045 changed this assertion: the reading settings save every change
	// at once, so a save in flight no longer turns the controls off under the
	// reader's hand (a keyboard user would lose their place). The section stays
	// drawn, with its controls usable and the loading line showing.
	if strings.Contains(loading, `data-prefs-section="reading-languages"`) == false || strings.Contains(loading, "disabled") {
		t.Fatal("while loading, the controls stay usable inside the panel, which is not replaced")
	}
	// A failed languages count removes only the count, never the panel.
	failed := chatPolishMarkup(t, renderingPersonalView(props, renderingPersonalState{LanguagesFailed: true}, ui.Handler{}, ui.Handler{}, nil), 800, "light")
	chat4Require(t, failed, `data-prefs-section="reading-languages"`)

	// The service is asked once, when the row mounts, not when it is opened.
	component := chatbugFuncBody(t, "chatrender_personal.go", "renderingPersonalPanel")
	if !strings.Contains(component, "Checking: renderingChecksAvailability") || strings.Contains(component, "current.Loading = true\n\t\tstate.Set(current)\n\t\treturn renderingLoadSettings") {
		t.Fatal("the settings load is not the mount-time availability check")
	}
	if got := chatbugCascadeValue(Stylesheet, ".chatrender-personal-slot", "display"); got != "none" {
		t.Fatalf("the absent row leaves a box: display = %q", got)
	}
}

func TestTodo_CHATBUG_026_Browser(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		m.Callbacks.SavePreferences = func(Preferences) {}
		page := chatPolishMarkup(t, rail(m, handlers{}), width, theme)
		// CHATUX-002 changed this block: Quiet hours and Reading languages are
		// sections of the one Chat preferences panel in the Conversations heading,
		// not two panels over footer rows.
		layers := chatPolishNodes(t, page, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-chat-layer") == chatux002PrefsKind })
		if len(layers) != 1 {
			t.Fatalf("%d sidebar panels, want the one Chat preferences panel", len(layers))
		}
		layer := layers[0]
		if !chatPolishAncestor(layer, "chat-rail") {
			t.Fatal("the panel is not inside the sidebar column")
		}
		disclosure := layer.Parent
		if chatPolishAttr(disclosure, "data-chat-disclosure") != "true" {
			t.Fatal("the panel is not the body of its own control")
		}
		button := disclosure.FirstChild
		if button == nil || button.Data != "button" || chatPolishAttr(button, "type") != "button" || chatPolishAttr(button, "data-chat-disclosure-toggle") != "true" || chatPolishAttr(button, "aria-expanded") != "false" || hasChatPolishAttribute(button, "disabled") {
			t.Fatal("its control is not an enabled, closed button")
		}
		if !hasChatPolishAttribute(layer, "hidden") || chatPolishAttr(layer, "popover") != "manual" || chatPolishAttr(layer, "role") != "dialog" {
			t.Fatal("the panel is not a closed popover dialog")
		}
		sections := chatPolishNodes(t, page, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-prefs-section") != "" })
		if len(sections) != 2 || chatPolishAttr(sections[0], "data-prefs-section") != "quiet-hours" || chatPolishAttr(sections[1], "data-prefs-section") != "reading-languages" {
			t.Fatalf("the panel holds %d sections, want Quiet hours and Reading languages", len(sections))
		}
		chat4Require(t, page, `id="quiet-hours"`, `id="chatrender-reading"`)

		// The language service is not available: the section is absent, not greyed.
		m.ChatFeatures = &ChatFeatures{Renderings: false}
		without := chatPolishMarkup(t, rail(m, handlers{}), width, theme)
		if strings.Contains(without, `data-prefs-section="reading-languages"`) || strings.Contains(without, RenderingText(m.Locale, "title")) {
			t.Fatal("a section for an unavailable language service is drawn")
		}
		switches := chatPolishNodes(t, without, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == "quiet-hours" })
		if len(switches) != 1 || hasChatPolishAttribute(switches[0], "disabled") {
			t.Fatalf("%d quiet hours switches left, want one enabled", len(switches))
		}
	})
}

// chatbugOpenLayer is the list of open layers after a layer of kind opens.
func chatbugOpenLayer(open []string, kind string) []string {
	drop := map[int]bool{}
	for _, i := range chatLayersToClose(open, kind) {
		drop[i] = true
	}
	var next []string
	for i, existing := range open {
		if !drop[i] {
			next = append(next, existing)
		}
	}
	return append(next, kind)
}

func TestTodo_CHATBUG_026_State(t *testing.T) {
	// Open Quiet hours, open Reading languages, then a menu, then Quiet hours
	// again: only one sidebar panel is ever open and a menu is left alone.
	open := chatbugOpenLayer(nil, "quiet-hours")
	open = chatbugOpenLayer(open, "reading-languages")
	if len(open) != 1 || open[0] != "reading-languages" {
		t.Fatalf("both sidebar panels open: %v", open)
	}
	open = chatbugOpenLayer(open, "menu")
	open = chatbugOpenLayer(open, "quiet-hours")
	if len(open) != 2 || open[0] != "menu" || open[1] != "quiet-hours" {
		t.Fatalf("Quiet hours must replace Reading languages and leave the menu alone: %v", open)
	}
	// Escape reaches the newest visible layer first, then the one beneath it.
	layers := []chatPolishLayer{{kind: "menu", order: 1, visible: true}, {kind: "quiet-hours", order: 2, visible: true}}
	if top := chatPolishTopLayer(layers); layers[top].kind != "quiet-hours" {
		t.Fatalf("Escape reaches %s first", layers[top].kind)
	}
	layers[1].visible = false
	if top := chatPolishTopLayer(layers); layers[top].kind != "menu" {
		t.Fatalf("with the panel closed Escape reaches %s", layers[top].kind)
	}
	layers[0].visible = false
	if top := chatPolishTopLayer(layers); top != -1 {
		t.Fatalf("nothing is open but Escape found layer %d", top)
	}
}

func TestTodo_CHATBUG_026_WiredToTheBrowser(t *testing.T) {
	const layers = "agentux_chat5_layers_js.go"
	// Opening and closing never waits for a frame or a timer: a window that
	// throttles them (the review pane does) would show a panel that has not opened.
	for _, name := range []string{"toggleChatDisclosure", "closeChatDisclosureLayer", "closeTopChatLayerOnEscape", "dismissChatLayersOnOutsideClick"} {
		body := chatbugFuncBody(t, layers, name)
		for _, forbidden := range []string{"requestAnimationFrame", "setTimeout", "FocusEvent", "\"focusout\"", "\"blur\""} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s depends on %s", name, forbidden)
			}
		}
	}
	if body := chatbugFuncBody(t, layers, "closeChatLayersOfKind"); !strings.Contains(body, "chatLayersToClose(") {
		t.Fatal("opening a panel does not close its group")
	}
	if body := chatbugFuncBody(t, layers, "positionChatLayer"); !strings.Contains(body, "chatSidebarLayerPlacement(") || !strings.Contains(body, "chatLayerPlace(") {
		t.Fatal("panels are not placed by the tested placement rules")
	}
	if body := chatbugFuncBody(t, layers, "syncChatAnchoredLayers"); !strings.Contains(body, "chatDialogSelector") || !strings.Contains(body, "chatLayerOutsideDismisses(") {
		t.Fatal("a sidebar panel survives a dialog opening over it")
	}
	bind := chatbugFuncBody(t, layers, "bindChatActiveRows")
	for _, want := range []string{`doc.Call("addEventListener", "keydown", documentKey, true)`, `doc.Call("addEventListener", "click", documentClick, true)`, `MutationObserver`, "toggleChatDisclosure(root, button)"} {
		if !strings.Contains(bind, want) {
			t.Fatalf("bindChatActiveRows lost %q", want)
		}
	}
	if body := chatbugFuncBody(t, layers, "escapeChatLayersFromPage"); !strings.Contains(body, `"KeyboardEvent"`) || !strings.Contains(body, "dispatchEvent") {
		t.Fatal("Escape pressed with focus on the page is not handed to the workspace")
	}
	if body := chatbugFuncBody(t, "dialog_focus_js.go", "restoreChatDialogFocus"); !strings.Contains(body, "chatFocusRestoreAllowed(chatFocusHeldNow())") {
		t.Fatal("closing a dialog can still take focus back from the composer")
	}
	if body := chatbugFuncBody(t, layers, "restoreChatLayerFocus"); !strings.Contains(body, "chatFocusRestoreAllowed(chatFocusHeldNow())") {
		t.Fatal("closing a layer can still take focus back from where the person clicked")
	}
}
