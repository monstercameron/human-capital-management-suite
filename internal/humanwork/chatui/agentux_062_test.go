package chatui

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// agentux062Layers lists, for every kind of anchored layer Chat opens, the
// class its element carries. Each of them is built by anchoredChatLayer (or, for
// the emoji picker, by the one function that also gives it its own placement),
// so it is a popover in the top layer that no other element makes room for.
var agentux062Layers = map[string]string{
	"todo": "channel-tray-card", "poll": "channel-tray-card", "more": "channel-tray-card",
	"reaction": "reaction-picker", "menu": "message-menu", "search": "chat-search-layer",
	"saved": "chatsave-panel", "voice": "chatvoice-panel", "writing-style": "chattone-options",
	"location": "chatmap-sheet",
}

// agentux062Sources are the files that build each layer; every one of them goes
// through the shared constructor rather than a hand-placed element.
var agentux062Sources = map[string]string{
	"channel-tray-card": "channel_tray.go", "message-menu": "render.go", "chat-search-layer": "agentux_chat5_layers.go",
	"chatsave-panel": "chatsave_view.go", "chatvoice-panel": "chatvoice_view.go", "chattone-options": "chattone_toolbar.go",
	"chatmap-sheet": "chatmap_location.go",
}

// TestTodo_AGENTUX_062 checks the one layer rule: every panel, picker and menu
// of Chat is an anchored layer that changes no other element's position, one
// picker opens per action, the layers close with Escape and give focus back to
// their opener, and the details in the entry (empty to-do list, labelled thread
// close, "Jump to newest" in its own gutter) are in the markup and the styles.
func TestTodo_AGENTUX_062(t *testing.T) {
	// 1. Built by the shared constructor.
	for class, file := range agentux062Sources {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		built := false
		for _, line := range strings.Split(source, "\n") {
			if regexp.MustCompile(`Class: "(?:[^"]* )?` + regexp.QuoteMeta(class) + `(?: [^"]*)?"`).MatchString(line) {
				built = true
				if !strings.Contains(line, "anchoredChatLayer(") {
					t.Errorf("%s builds .%s without anchoredChatLayer: %q", file, class, strings.TrimSpace(line))
				}
			}
		}
		if !built {
			t.Errorf("%s no longer builds .%s", file, class)
		}
	}
	if emoji, err := os.ReadFile("chatemoji_view.go"); err != nil || !strings.Contains(string(emoji), "anchoredChatLayer(props, kind, children...)") {
		t.Error("the emoji picker is not an anchored layer when it opens from a message")
	}

	// 2. No layer is in the flow of the page: position is fixed (from
	// [data-chat-layer]) and no later rule gives a layer's class another.
	if got := chatbugCascadeValue(Stylesheet, "[data-chat-layer]", "position"); got != "fixed" {
		t.Errorf("[data-chat-layer] has position %q", got)
	}
	finalPosition := map[string]string{}
	for _, rule := range agentux058Rules(Stylesheet) {
		for _, selector := range strings.Split(rule[0], ",") {
			fields := strings.Fields(strings.TrimSpace(selector))
			if len(fields) == 0 {
				continue
			}
			subject := fields[len(fields)-1]
			for _, class := range agentux062Layers {
				if subject == "."+class || strings.HasPrefix(subject, "."+class+"[") || strings.HasPrefix(subject, "."+class+":") {
					if found := regexp.MustCompile(`(?:^|;)\s*position\s*:\s*(\w+)`).FindStringSubmatch(rule[1]); found != nil {
						finalPosition[class] = found[1]
					}
				}
			}
		}
	}
	for class, position := range finalPosition {
		if position != "fixed" {
			t.Errorf(".%s ends up position:%s, in the flow of the page", class, position)
		}
	}
	if len(finalPosition) == 0 {
		t.Error("no layer class has a position rule: the test looks at the wrong selectors")
	}

	// 3. The layers render as layers, in every language.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "answered", false)
		for _, kind := range []string{"todo", "poll"} {
			chat4Require(t, renderNode(t, channelTray(m, handlers{}, kind)), `data-chat-layer="`+kind+`"`, `popover="manual"`, `role="dialog"`)
		}
		chat4Require(t, renderNode(t, chatSearchLayer(m, handlers{local: localUI{searchOpen: true}})), `data-chat-layer="search"`, `popover="manual"`)

		// One picker per action: the thread root is also a row of the list.
		m.ShowThread, m.ThreadParentID, m.PickerID = true, "question", "question"
		page := renderAgentUXChat3Node(t, Build(m), 1440)
		if n := strings.Count(page, `data-chat-layer="reaction"`); n != 1 {
			t.Errorf("%s: a reaction opened %d pickers", locale, n)
		}
		chat4Require(t, page, chat5Text(m, "chat.emoji.search"), `aria-label="`+m.t(KeyCloseThread)+`"`)
		// The picker is inside the row it reacts to, never a second list's.
		if strings.Contains(page, `class="message-list"`) && strings.Count(page, `emoji-search`) == 0 {
			t.Errorf("%s: the picker has no search", locale)
		}

		// A message's menu opens with actions, or its button is not drawn.
		own := Message{ID: "own", AuthorID: m.CurrentUser, Body: "Question"}
		other := Message{ID: "other", AuthorID: "walt", Body: "Question"}
		m.MenuID = "own"
		m.Callbacks.CopyLink, m.Callbacks.Pin, m.Callbacks.BeginEdit, m.Callbacks.DeleteMessage, m.Callbacks.MarkUnreadFrom = func(string) {}, func(string) {}, func(string) {}, func(string, uint64) {}, func(string) {}
		items := renderNode(t, message(m, handlers{}, own, false))
		chat4Require(t, items, `data-action="copy-link"`, `data-action="edit"`, `data-action="delete"`, `data-action="pin"`, `data-action="mark-unread"`)
		m.MenuID = "other"
		items = renderNode(t, message(m, handlers{}, other, false))
		chat4Require(t, items, `data-action="copy-link"`, `data-action="pin"`)
		if strings.Contains(items, `data-action="edit"`) || strings.Contains(items, `data-action="delete"`) {
			t.Errorf("%s: another person's message offers edit or delete", locale)
		}
		m.MenuID = ""
		m.Callbacks.OpenMenu = nil
		if bar := renderNode(t, message(m, handlers{local: localUI{pointerRow: "own"}}, own, false)); strings.Contains(bar, `data-action="menu"`) && !strings.Contains(bar, "hidden") {
			t.Errorf("%s: the More button is drawn with nothing to open", locale)
		}

		// An empty to-do list says so and offers the next step.
		chat4Require(t, renderNode(t, channelTodoSection(m, handlers{})), chat5Text(m, "chat.todo.empty_next"), chat5Text(m, "chat.todo.add_next"))
	}

	// 4. Every open thing closes with Escape: one step per state, topmost first.
	state := reflect.TypeOf(chatEscapeState{})
	for i := 0; i < state.NumField(); i++ {
		field := state.Field(i)
		s := reflect.New(state).Elem()
		switch field.Type.Kind() {
		case reflect.Bool:
			s.Field(i).SetBool(true)
		case reflect.String:
			s.Field(i).SetString(map[string]string{"TopLayer": "reaction", "Tray": "todo"}[field.Name])
			if field.Name == "TopLayer" {
				continue
			}
		}
		if step := chatEscapeStep(s.Interface().(chatEscapeState)); step == "" {
			t.Errorf("%s is open and Escape closes nothing", field.Name)
		}
	}
	for _, kind := range []string{"todo", "poll", chatcmd002TrayMore} {
		if got := chatEscapeStep(chatEscapeState{TopLayer: kind, Tray: kind}); got != "tray" {
			t.Errorf("Escape over the %s card closes %q", kind, got)
		}
	}
	if got := chatEscapeStep(chatEscapeState{TopLayer: "todo", Tray: "todo", Picker: true}); got != "tray" {
		t.Errorf("a reaction picker behind an open to-do list closed first: %q", got)
	}

	// 5. Focus goes back to the opener. Every opener action maps to a layer
	// kind, and each close path of the root's key handler restores it.
	for _, action := range []string{"open-todo", "tray-todo", "open-poll", "tray-poll", "tray-more", "emoji-toggle", "react-pick", "menu", "details", "stats", "agents-here", "reply", "chat-search-open"} {
		if chatLayerKind(action) == "" {
			t.Errorf("%s is not a layer opener: focus cannot return to it", action)
		}
	}
	raw, err := os.ReadFile("render.go")
	if err != nil {
		t.Fatal(err)
	}
	keyHandler := string(raw)
	keyHandler = keyHandler[strings.Index(keyHandler, "switch chatEscapeStep("):]
	keyHandler = keyHandler[:strings.Index(keyHandler, "case \"create\":")]
	for _, want := range []string{`restoreChatLayerFocus("search")`, `restoreChatLayerFocus("reaction")`, `restoreMessageMenuFocus(m.MenuID)`, `restoreChatLayerFocus(kind)`, `restoreRailMenuFocus(m.RailMenuID)`} {
		if !strings.Contains(keyHandler, want) {
			t.Errorf("the Escape handler does not %s", want)
		}
	}

	// 6. "Jump to newest" has its own gutter above the composer; it is not drawn
	// on top of the text.
	for _, want := range []string{".chat-workspace .jump-newest{position:static", ".timeline-frame{display:flex;flex-direction:column"} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("styles lack %s", want)
		}
	}
}

// TestTodo_AGENTUX_062_Accessibility: every layer is named, a menu holds only
// menu items and separators, and every control in a layer has a name.
func TestTodo_AGENTUX_062_Accessibility(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		m := chat4Fixture(locale, "answered", false)
		m.Callbacks.CopyLink, m.Callbacks.Pin, m.Callbacks.BeginEdit, m.Callbacks.DeleteMessage = func(string) {}, func(string) {}, func(string) {}, func(string, uint64) {}
		m.MenuID = "question"
		surfaces := map[string]string{
			"menu":   renderAgentUXChat3Node(t, Build(m), width),
			"todo":   renderAgentUXChat3Node(t, channelTray(m, handlers{}, "todo"), width),
			"poll":   renderAgentUXChat3Node(t, channelTray(m, handlers{}, "poll"), width),
			"search": renderAgentUXChat3Node(t, chatSearchLayer(m, handlers{local: localUI{searchOpen: true}}), width),
		}
		picker := chat4Fixture(locale, "answered", false)
		picker.PickerID = "question"
		surfaces["reaction"] = renderAgentUXChat3Node(t, Build(picker), width)
		for name, markup := range surfaces {
			root, err := xhtml.Parse(strings.NewReader(markup))
			if err != nil {
				t.Fatal(err)
			}
			layers := 0
			walkChat5HTML(root, func(n *xhtml.Node) {
				if n.Type != xhtml.ElementNode || chat5Attr(n, "data-chat-layer") == "" {
					return
				}
				layers++
				role := chat5Attr(n, "role")
				if role == "" && chat5Attr(n, "data-emoji-picker") == "" {
					t.Errorf("%s: a layer has no role", name)
				}
				if chat5Attr(n, "aria-label") == "" && chat5Attr(n, "aria-labelledby") == "" {
					t.Errorf("%s: a %s layer has no name", name, role)
				}
				if role == "menu" {
					walkChat5HTML(n, func(item *xhtml.Node) {
						if item.Type == xhtml.ElementNode && (item.Data == "button" || item.Data == "a") && chat5Attr(item, "role") != "menuitem" {
							t.Errorf("%s: <%s class=%q> in a menu is not a menu item", name, item.Data, chat5Attr(item, "class"))
						}
					})
				}
			})
			if layers == 0 {
				t.Errorf("%s: no layer was rendered", name)
			}
			agentux063Controls(t, markup, func(n *xhtml.Node, controlName string) {
				if controlName == "" {
					t.Errorf("%s/%s/%d: <%s class=%q> has no name", name, locale, width, n.Data, chat5Attr(n, "class"))
				}
			})
		}
	})
}

// TestTodo_AGENTUX_062_OutsidePress: a press outside every layer closes the
// channel card and the search layer unless they hold something typed, and the
// formatting tools like a sidebar panel; a poll form keeps its fields.
func TestTodo_AGENTUX_062_OutsidePress(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		local                localUI
		m                    Model
		wantTray, wantSearch bool
	}{
		{"to-do list, nothing typed", localUI{tray: "todo"}, Model{}, true, false},
		{"to-do list, a task typed", localUI{tray: "todo"}, Model{ChannelTodoDraft: "Book room"}, false, false},
		{"more card", localUI{tray: chatcmd002TrayMore}, Model{}, true, false},
		{"poll form", localUI{tray: "poll"}, Model{}, false, false},
		{"empty search layer", localUI{searchOpen: true}, Model{}, false, true},
		{"search layer with a query", localUI{searchOpen: true}, Model{Search: "carry"}, false, false},
		{"nothing open", localUI{}, Model{}, false, false},
	} {
		tray, search := chatOutsidePress(tc.local, tc.m)
		if tray != tc.wantTray || search != tc.wantSearch {
			t.Errorf("%s: closes tray=%v search=%v, want %v and %v", tc.name, tray, search, tc.wantTray, tc.wantSearch)
		}
	}
	if !chatLayerOutsideDismisses("formatting") || chatLayerOutsideDismisses("todo") || chatLayerOutsideDismisses("reaction") {
		t.Error("the outside-press set is not the sidebar panels and the formatting tools")
	}
	raw, err := os.ReadFile("render.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "chatOutsidePress(local.get(), m)") {
		t.Error("the workspace's click handler does not ask what an outside press closes")
	}
}

// TestTodo_AGENTUX_062_EmojiBelowHeader: the emoji picker is cut to start under
// the conversation header, and opens on the roomier side below its opener when
// that leaves it too short.
func TestTodo_AGENTUX_062_EmojiBelowHeader(t *testing.T) {
	area := chatLayerRect{left: 360, top: 134, right: 1280, bottom: 800}
	opener := chatLayerRect{left: 400, top: 300, right: 430, bottom: 330}
	fits := emojiPlacement{Left: 400, Top: 100, Width: 340, Height: 300}
	if got := chatEmojiBelowHeader(fits, opener, area, 800); got.Top < area.top+emojiPickerEdge || got.Top+got.Height != fits.Top+fits.Height {
		t.Errorf("a picker over the header is not cut at its top: %+v", got)
	}
	if got := chatEmojiBelowHeader(emojiPlacement{Left: 400, Top: 200, Width: 340, Height: 100}, opener, area, 800); got.Top != 200 || got.Height != 100 {
		t.Errorf("a picker already inside the area moved: %+v", got)
	}
	short := emojiPlacement{Left: 400, Top: 20, Width: 340, Height: 280}
	if got := chatEmojiBelowHeader(short, opener, area, 800); got.Top != opener.bottom+emojiPickerGap {
		t.Errorf("a picker left with %d px under the header did not open below its opener: %+v", int(280-(area.top+emojiPickerEdge-20)), got)
	}
	if got := chatEmojiBelowHeader(short, opener, chatLayerRect{}, 800); got != short {
		t.Errorf("an unmeasured area moved the picker: %+v", got)
	}
	raw, err := os.ReadFile("chatemoji_js.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "chatEmojiBelowHeader(placement, anchor, area, vh)") {
		t.Error("the emoji picker's placement does not keep the picker under the header")
	}
}

// TestTodo_AGENTUX_062_RailMenu: the conversation's menu in the sidebar is an
// anchored layer like the rest, placed against its row's button by the shared
// rule; no code positions it by hand any more.
func TestTodo_AGENTUX_062_RailMenu(t *testing.T) {
	if chatLayerKind("rail-menu") != "rail-menu" || chatLayerWidth("rail-menu", 1440) != 200 || chatLayerWidth("rail-menu", 120) != 104 {
		t.Error("the rail menu has no layer kind or no width of its own")
	}
	m := chat4Fixture("en-US", "answered", false)
	m.RailMenuID = "general"
	menu := renderNode(t, chatux020RailMenu(m, handlers{}))
	chat4Require(t, menu, `data-chat-layer="rail-menu"`, `popover="manual"`, `role="menu"`, `class="rail-row-menu"`)
	// The menu opens where its button is: the opener's rectangle is the anchor,
	// end-aligned and below, flipping above when the room below is short.
	opener := chatLayerRect{left: 200, top: 700, right: 232, bottom: 728}
	g := anchoredChatGeometryIn(opener, chatLayerRect{0, 0, 1440, 800}, chatLayerWidth("rail-menu", 1440), 420, false, false)
	if g.left+g.width != opener.right || g.top+g.height != opener.top-10 {
		t.Errorf("a tall menu from a row near the bottom is not above its button: %+v", g)
	}
	for _, name := range []string{"events_js.go", "events_other.go", "render.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "positionRailMenu") || strings.Contains(string(raw), `setProperty", "--chat-rail-menu`) {
			t.Errorf("%s still places the conversation menu by hand", name)
		}
	}
}

// TestTodo_AGENTUX_062_ComposerLayers: a layer opened from the composer sits
// wholly above the composer, 8 px clear of its top edge, inside the conversation
// column, and is cut to the room between the conversation header and the
// composer (it scrolls inside). Checked with the composer at the bottom of a
// 900 px and of a 600 px window, in both directions.
func TestTodo_AGENTUX_062_ComposerLayers(t *testing.T) {
	for _, vh := range []float64{900, 600} {
		area := chatLayerRect{left: 360, top: 134, right: 1280, bottom: vh}
		composer := chatLayerRect{left: 380, top: vh - 96, right: 1260, bottom: vh - 16}
		for _, rtl := range []bool{false, true} {
			opener := chatLayerRect{left: 392, top: vh - 70, right: 424, bottom: vh - 38}
			if rtl {
				opener = chatLayerRect{left: 1216, top: vh - 70, right: 1248, bottom: vh - 38}
			}
			for _, size := range [][2]float64{{340, 392}, {360, 240}, {400, 900}} {
				g := chatComposerLayerPlace(opener, composer, area, size[0], size[1], rtl)
				where := fmt.Sprintf("vh=%.0f rtl=%v size=%v: %+v", vh, rtl, size, g)
				if g.top+g.height != composer.top-chatComposerGap {
					t.Errorf("%s: bottom edge is not %d px above the composer", where, chatComposerGap)
				}
				if g.top < area.top+8 || g.left < area.left+8 || g.left+g.width > area.right-8 {
					t.Errorf("%s: leaves the conversation column", where)
				}
				if want := min(size[1], composer.top-chatComposerGap-(area.top+8)); g.height != want {
					t.Errorf("%s: height %.0f, want %.0f (capped to the room under the header)", where, g.height, want)
				}
			}
		}
	}
	for _, kind := range []string{"emoji", "menu", "voice", "writing-style", "location"} {
		if !chatLayerAbove(kind) {
			t.Errorf("%s opens below its opener inside the composer", kind)
		}
	}
	raw, err := os.ReadFile("chatemoji_js.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "chatComposerLayerPlace(anchor, composer,") {
		t.Error("the emoji picker does not use the composer placement rule")
	}
}
