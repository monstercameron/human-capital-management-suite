package chatui

import (
	"regexp"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// chatbugCascadeValue is the last value the stylesheet gives a property to a
// rule whose selector list contains exactly the selector asked for.
func chatbugCascadeValue(css, selector, property string) string {
	value := ""
	declaration := regexp.MustCompile(`(?:^|;)\s*` + regexp.QuoteMeta(property) + `\s*:\s*([^;]+)`)
	for _, rule := range regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`).FindAllStringSubmatch(css, -1) {
		for _, candidate := range strings.Split(rule[1], ",") {
			if strings.TrimSpace(candidate) != selector {
				continue
			}
			if found := declaration.FindStringSubmatch(rule[2]); found != nil {
				value = strings.TrimSpace(found[1])
			}
		}
	}
	return value
}

func TestTodo_CHATBUG_002(t *testing.T) {
	zero := chatLayerRect{}
	pressed := chatLayerRect{1380, 521, 1420, 549}
	// RED: the geometry of a detached opener's all-zero rectangle is a page corner.
	corner := anchoredChatGeometry(zero, 360, 220, 1440, 900, true, false)
	if corner.left != 8 || corner.top != 8 {
		t.Fatalf("premise changed: a zero anchor no longer lands in the corner: %+v", corner)
	}
	if chatLayerRectUsable(zero) || !chatLayerRectUsable(pressed) {
		t.Fatal("a zero rectangle must never be usable as an anchor")
	}
	if got, ok := chatLayerAnchorRect(zero, pressed); !ok || got != pressed {
		t.Fatalf("a detached opener must fall back to the rectangle taken when it was pressed: %+v %v", got, ok)
	}
	live := chatLayerRect{1000, 300, 1040, 328}
	if got, ok := chatLayerAnchorRect(live, pressed); !ok || got != live {
		t.Fatalf("a live opener outranks the remembered rectangle: %+v %v", got, ok)
	}
	if _, ok := chatLayerAnchorRect(zero, zero); ok {
		t.Fatal("with no anchor at all the layer must not be placed in a corner")
	}
	for _, width := range []float64{1440, 800, 390, 320} {
		for _, rtl := range []bool{false, true} {
			anchor := chatLayerRect{min(pressed.left, width-48), 521, min(pressed.right, width-8), 549}
			got, _ := chatLayerAnchorRect(zero, anchor)
			g := anchoredChatGeometry(got, 360, 220, width, 900, true, rtl)
			if g.left < 8 || g.left+g.width > width-8 || g.top < 8 || g.top+g.height > 892 {
				t.Fatalf("picker leaves the viewport at %v: %+v", width, g)
			}
			if end := g.top + g.height; end > anchor.top-4 || end < anchor.top-24 {
				t.Fatalf("picker is not beside the button that opened it at %v: %+v for %+v", width, g, anchor)
			}
		}
	}
	// The hover bar and the reaction chips both carry react-pick for one message.
	if got := chatLayerPickOpener([]bool{false, true}, []bool{true, true}, true); got != 1 {
		t.Fatalf("the bar button pressed must be re-found in the bar, got %d", got)
	}
	if got := chatLayerPickOpener([]bool{false, true}, []bool{true, true}, false); got != 0 {
		t.Fatalf("the chip pressed must be re-found as the chip, got %d", got)
	}
	if got := chatLayerPickOpener([]bool{false, true}, []bool{true, false}, true); got != 0 {
		t.Fatalf("a button with no layout must lose to one that has some, got %d", got)
	}
	if got := chatLayerPickOpener([]bool{true}, []bool{false}, true); got != -1 {
		t.Fatalf("no laid-out button means no opener, got %d", got)
	}
	for kind, want := range map[string]bool{"reaction": true, "emoji": true, "menu": true, "voice": true, "writing-style": true, "search": false, "thread": false} {
		if chatLayerAbove(kind) != want {
			t.Fatalf("%s above = %v", kind, !want)
		}
	}
	// Layout: stacked rows, not the old flex row that put the search label beside
	// its box and stood each category in a column a few letters wide.
	css := Stylesheet
	for _, selector := range []string{".reaction-picker", ".emoji-picker"} {
		if got := chatbugCascadeValue(css, selector, "display"); got != "block" {
			t.Fatalf("%s lays its children out as %q, want block", selector, got)
		}
	}
	if got := chatbugCascadeValue(css, ".emoji-category h3", "white-space"); got != "nowrap" {
		t.Fatalf("category labels wrap: white-space = %q", got)
	}
	if got := chatbugCascadeValue(css, ".emoji-search", "display"); got != "block" {
		t.Fatalf("the search box shares a row: display = %q", got)
	}
	if got := chatbugCascadeValue(css, ".reaction-picker>label", "display"); got != "block" {
		t.Fatalf("the search label shares a row with its box: display = %q", got)
	}
}

func TestTodo_CHATBUG_002_Browser(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		m.PickerID = "question"
		m.Callbacks.OpenPicker = func(string) {}
		m.Callbacks.ReactWith = func(string, string) {}
		// CHATEMOJI-002 replaced the old picker: one component, a search field that
		// is the first row of the dialog, and the starter emoji under their own heading.
		picker := chatPolishMarkup(t, chatEmojiReactionLayer(m, localUI{}), width, theme)
		chat4Require(t, picker, `data-chat-layer="reaction"`, `role="dialog"`, `popover="manual"`)
		roots := chatPolishNodes(t, picker, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "emoji-pop") })
		if len(roots) != 1 {
			t.Fatalf("%d reaction pickers", len(roots))
		}
		inputs := chatPolishNodes(t, picker, func(n *xhtml.Node) bool {
			return n.Data == "input" && chatPolishAttr(n, "type") == "search" && chatPolishAttr(n, "aria-label") != ""
		})
		if len(inputs) != 1 || !chatPolishAncestor(inputs[0], "emoji-pop-search") {
			t.Fatal("the picker's search box is not one labelled field in its own row")
		}
		choices := chatPolishNodes(t, picker, func(n *xhtml.Node) bool {
			return chatPolishAttr(n, "data-action") == "react-with" && chatPolishHasClass(n, "emoji-pop-btn")
		})
		if len(choices) < 8 {
			t.Fatalf("%d emoji choices, want at least the starter row of 8", len(choices))
		}
		// The button that opened the picker stays drawn, expanded, in its row's bar
		// while the picker is open, so there is a rectangle to anchor to.
		page := chatPolishMarkup(t, Build(m), width, theme)
		bars := chatPolishNodes(t, page, func(n *xhtml.Node) bool {
			return n.Data == "button" && chatPolishAttr(n, "data-action") == "react-pick" && chatPolishAttr(n, "aria-expanded") == "true" && chatPolishAncestor(n, "message-actions")
		})
		if len(bars) != 1 {
			t.Fatalf("%d open react buttons in a message bar; the opener must stay rendered while its picker is open", len(bars))
		}
	})
}

func TestTodo_CHATBUG_010(t *testing.T) {
	// RED: .thread-more is absolutely positioned at the top-right of the nearest
	// positioned ancestor, which is now the bar itself, so it sat on Save for later.
	if got := chatbugCascadeValue(Stylesheet, ".thread-more", "position"); got != "absolute" {
		t.Fatalf("premise changed: .thread-more position = %q", got)
	}
	if got := chatbugCascadeValue(Stylesheet, ".thread-message-actions .thread-more", "position"); got != "static" {
		t.Fatalf("a more button inside a thread bar must take its own flex place, position = %q", got)
	}
}

func TestTodo_CHATBUG_010_Browser(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		m.Callbacks.OpenMenu = func(string) {}
		m.ShowThread, m.ThreadParentID = true, "question"
		m.ThreadMessages = []Message{{ID: "reply", Author: "Alex", Body: "Reply"}}
		thread := chatPolishMarkup(t, threadPane(m, handlers{}), width, theme)
		bars := chatPolishNodes(t, thread, func(n *xhtml.Node) bool {
			return n.Data == "div" && chatPolishHasClass(n, "message-actions") && chatPolishHasClass(n, "thread-message-actions") && chatPolishAncestor(n, "thread-message")
		})
		if len(bars) != 1 {
			t.Fatalf("%d reply bars", len(bars))
		}
		order := chatbugBarOrder(bars[0])
		if len(order) != 2 || order[0] != "save" || order[1] != "more" {
			t.Fatalf("thread reply bar order = %v, want [save more]", order)
		}
		// The main timeline's bar leads with Save for later and ends with More.
		own := Message{ID: "question", AuthorID: m.CurrentUser, Author: "Alice", Body: "Question"}
		m.Callbacks.OpenMenu = func(string) {}
		main := renderNode(t, message(m, handlers{local: localUI{focusRow: own.ID}}, own, false))
		mainBars := chatPolishNodes(t, main, func(n *xhtml.Node) bool {
			return n.Data == "div" && chatPolishHasClass(n, "message-actions") && !chatPolishHasClass(n, "thread-message-actions")
		})
		if len(mainBars) != 1 {
			t.Fatalf("%d timeline bars", len(mainBars))
		}
		main2 := chatbugBarOrder(mainBars[0])
		if len(main2) == 0 || main2[len(main2)-1] != "more" {
			t.Fatalf("timeline bar does not end with More: %v", main2)
		}
		saveAt, moreAt := -1, -1
		for i, name := range main2 {
			if name == "save" {
				saveAt = i
			}
			if name == "more" {
				moreAt = i
			}
		}
		if saveAt < 0 || saveAt > moreAt {
			t.Fatalf("timeline bar %v does not put Save before More", main2)
		}
	})
}

// chatbugBarOrder names the Save for later and More buttons of a bar in the
// order they are drawn.
func chatbugBarOrder(bar *xhtml.Node) []string {
	var order []string
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "button" {
			switch {
			case chatPolishHasClass(n, "chatsave-action"):
				order = append(order, "save")
			case chatPolishAttr(n, "data-action") == "menu":
				order = append(order, "more")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(bar)
	return order
}

func TestTodo_CHATBUG_017(t *testing.T) {
	// A mouse resting on a row never draws its actions at phone width; a touch
	// screen's emulated hover never does either; a tap (focus without
	// :focus-visible) does.
	for _, tc := range []struct {
		pointer string
		compact bool
		want    bool
	}{{"mouse", false, true}, {"pen", false, true}, {"mouse", true, false}, {"pen", true, false}, {"touch", false, false}, {"touch", true, false}} {
		if got := chatRowPointerActivates(tc.pointer, tc.compact); got != tc.want {
			t.Fatalf("pointer %s compact=%v activates=%v, want %v", tc.pointer, tc.compact, got, tc.want)
		}
	}
	for _, tc := range []struct{ visible, compact, want bool }{{true, false, true}, {false, false, false}, {false, true, true}, {true, true, true}} {
		if got := chatRowFocusActivates(tc.visible, tc.compact); got != tc.want {
			t.Fatalf("focus visible=%v compact=%v activates=%v, want %v", tc.visible, tc.compact, got, tc.want)
		}
	}
	// The bar sits inside its row at phone width and on touch screens, not half
	// across the boundary with the message above.
	chat4Require(t, AgentUXChat4Styles, `@media(max-width:767px),(pointer:coarse){.chat-workspace .message-list .message .message-actions{top:4px;transform:none}}`)
	if got := chatbugCascadeValue(Stylesheet, ".chat-workspace .message-list .message .message-actions", "transform"); got != "none" {
		t.Fatalf("compact bar transform = %q", got)
	}
}

func TestTodo_CHATBUG_017_Browser(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		idle := chatPolishMarkup(t, Build(m), width, theme)
		if strings.Contains(idle, `class="message-actions"`) {
			t.Fatal("a bar is drawn for a message nobody is touching or focusing")
		}
		touched := Message{ID: "question", AuthorID: "alice", Author: "Alice", Body: "Question"}
		m.Callbacks.OpenMenu = func(string) {}
		for _, local := range []localUI{{focusRow: "question"}, {pointerRow: "question"}} {
			row := chatPolishMarkup(t, message(m, handlers{local: local}, touched, false), width, theme)
			chat4Require(t, row, `class="message-actions"`)
		}
		other := chatPolishMarkup(t, message(m, handlers{local: localUI{focusRow: "other"}}, touched, false), width, theme)
		if strings.Contains(other, `class="message-actions"`) {
			t.Fatal("a bar is drawn on a row other than the active one")
		}
	})
}
