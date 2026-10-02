package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestTodo_CHATBUG_027(t *testing.T) {
	// Pressed state follows the open state exactly: the pressed button is the
	// button of the open tray, a second press closes it, the other button replaces it.
	tray := ""
	press := func(action string) (opening bool) { tray, opening = chatTrayToggle(tray, action); return }
	if !press("open-todo") || tray != "todo" {
		t.Fatalf("To-do list did not open: %q", tray)
	}
	if !press("open-poll") || tray != "poll" {
		t.Fatalf("Channel poll did not replace the to-do list: %q", tray)
	}
	if press("open-poll") || tray != "" {
		t.Fatalf("pressing the open Channel poll did not close it: %q", tray)
	}
	if !press("tray-todo") || tray != "todo" || press("tray-todo") || tray != "" {
		t.Fatalf("the chip does not toggle the same tray: %q", tray)
	}

	// Escape closes the topmost layer only, one per press, and never skips one.
	steps := []struct {
		state chatEscapeState
		want  string
	}{
		{chatEscapeState{TopLayer: "reaction", Picker: true, Tray: "todo", Thread: true, Details: true}, "reaction"},
		{chatEscapeState{TopLayer: "todo", Tray: "todo", Thread: true, Details: true}, "tray"},
		{chatEscapeState{TopLayer: "", Thread: true, Details: true}, "thread"},
		{chatEscapeState{Details: true}, "details"},
		{chatEscapeState{}, ""},
		// A tray behind the newest layer waits its turn.
		{chatEscapeState{TopLayer: "menu", Menu: true, Tray: "poll"}, "menu"},
		{chatEscapeState{TopLayer: "poll", Menu: true, Tray: "poll"}, "tray"},
		// An open New section form closes before anything else when nothing is layered over it.
		{chatEscapeState{SectionCreate: true, Thread: true}, "section-create"},
		{chatEscapeState{TopLayer: "todo", SectionCreate: true, Tray: "todo"}, "tray"},
		// A dialog and the share dialog beneath everything else.
		{chatEscapeState{Share: true, Create: true, Thread: true}, "share"},
		{chatEscapeState{Create: true, Thread: true}, "create"},
		{chatEscapeState{TopLayer: "search", Search: true, Tray: "todo"}, "search"},
	}
	for i, step := range steps {
		if got := chatEscapeStep(step.state); got != step.want {
			t.Fatalf("step %d %+v: Escape closes %q, want %q", i, step.state, got, step.want)
		}
	}

	// The reaction picker and menu of a thread reply close with the thread.
	m := Model{ShowThread: true, ThreadParentID: "question", ThreadMessages: []Message{{ID: "reply"}}, PickerID: "reply", MenuID: "thread:reply"}
	ClearThreadOverlays(&m)
	if m.PickerID != "" || m.MenuID != "" {
		t.Fatalf("overlays survive their thread: picker %q menu %q", m.PickerID, m.MenuID)
	}
	m = Model{ShowThread: true, ThreadParentID: "question", ThreadMessages: []Message{{ID: "reply"}}, PickerID: "question", MenuID: "question"}
	ClearThreadOverlays(&m)
	if m.PickerID != "question" || m.MenuID != "question" {
		t.Fatalf("a timeline message's picker closed with the thread: picker %q menu %q", m.PickerID, m.MenuID)
	}
	ClearThreadOverlays(nil)

	// A popover sizes to its content: the bound is the room there is, never the
	// height it had when it was first measured. It was measured while its list was
	// loading (about 60 px) and then held to that, so three rows scrolled.
	anchor := chatLayerRect{600, 60, 640, 94}
	for _, measured := range []float64{60, 180, 420} {
		placement := chatLayerPlace(anchoredChatGeometry(anchor, 360, measured, 800, 600, false, false), anchor, 600)
		if placement.up || placement.top != anchor.bottom+4 || placement.maxHeight < 480 {
			t.Fatalf("measured %v: popover is bounded by what it held then, %+v", measured, placement)
		}
	}
	above := chatLayerRect{700, 500, 740, 528}
	if placement := chatLayerPlace(anchoredChatGeometry(above, 360, 120, 800, 600, true, false), above, 600); !placement.up || placement.bottom != 600-(above.top-10) || placement.maxHeight != above.top-10-8 {
		t.Fatalf("a layer above its opener is pinned by its bottom edge: %+v", placement)
	}
	if got := chatLayerContentHeight(300, 302, 300); got != 302 {
		t.Fatalf("content height leaves out the border: %v", got)
	}
	if chatLayerWidth("poll", 800) < chatLayerWidth("todo", 800) || chatLayerWidth("todo", 320) != 304 {
		t.Fatalf("widths: poll %v todo %v at 320 %v", chatLayerWidth("poll", 800), chatLayerWidth("todo", 800), chatLayerWidth("todo", 320))
	}

	// CSS: no scrollbar the content does not need, the poll's primary action inside
	// the card, and pressed told apart from hover and from "has a poll".
	css := Stylesheet
	for selector, want := range map[string]map[string]string{
		".channel-tray-card[data-chat-layer]":                                               {"overflow-x": "hidden", "overflow-y": "auto", "box-sizing": "border-box"},
		".channel-tray-card[data-chat-layer] .channel-poll-actions":                         {"position": "static", "margin": "8px 0 0"},
		".chat-workspace .channel-todo-trigger[aria-pressed=true]":                          {"border-color": "var(--accent)"},
		".chat-workspace .channel-poll-trigger[aria-pressed=true]":                          {"border-color": "var(--accent)"},
		".chat-workspace .channel-todo-trigger:hover":                                       {"border-color": "var(--line)", "color": "var(--ink)"},
		".chat-workspace .channel-poll-trigger:hover":                                       {"border-color": "var(--line)", "color": "var(--ink)"},
		".chat-workspace .channel-poll-trigger.active:not([aria-pressed=true]):not(:hover)": {"border-color": "transparent", "background": "transparent"},
	} {
		for property, value := range want {
			if got := chatbugCascadeValue(css, selector, property); got != value {
				t.Fatalf("%s %s = %q, want %q", selector, property, got, value)
			}
		}
	}
	// The pressed rule must come after the hover rule so a pressed button under the
	// pointer is still drawn pressed.
	if strings.LastIndex(css, `.chat-workspace .channel-todo-trigger[aria-pressed=true]`) < strings.LastIndex(css, `.chat-workspace .channel-todo-trigger:hover`) {
		t.Fatal("hover is drawn over pressed")
	}
}

func TestTodo_CHATBUG_027_Browser(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		m.Callbacks.OpenChannelTodo = func() {}
		m.Callbacks.OpenChannelPoll = func(string) {}
		m.Callbacks.SavePreferences = func(Preferences) {}
		m.ChannelTodo = ChannelTodoList{Items: []ChannelTodoItem{{ID: "a", Text: "One"}, {ID: "b", Text: "Two"}, {ID: "c", Text: "Three"}}}

		// The header buttons are pressed exactly while their popover is on the page.
		for _, tray := range []string{"", "todo", "poll"} {
			page := chatPolishMarkup(t, timeline(m, handlers{local: localUI{tray: tray}}), width, theme)
			for _, which := range []string{"todo", "poll"} {
				buttons := chatPolishNodes(t, page, func(n *xhtml.Node) bool {
					return n.Data == "button" && chatPolishHasClass(n, "channel-"+which+"-trigger")
				})
				layers := chatPolishNodes(t, page, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-chat-layer") == which })
				if which == "poll" {
					// CHATUX-001: the poll button left the header; its popover is
					// still drawn exactly while the poll tray is the open one.
					if len(buttons) != 0 || (len(layers) == 1) != (tray == "poll") {
						t.Fatalf("tray %q: %d poll buttons in the header, %d poll popovers", tray, len(buttons), len(layers))
					}
					continue
				}
				if len(buttons) != 1 {
					t.Fatalf("tray %q: %d %s buttons", tray, len(buttons), which)
				}
				pressed := chatPolishAttr(buttons[0], "aria-pressed")
				if pressed != map[bool]string{true: "true", false: "false"}[tray == which] || (pressed == "true") != (len(layers) == 1) {
					t.Fatalf("tray %q: %s button pressed=%q with %d popovers on the page", tray, which, pressed, len(layers))
				}
			}
		}

		// Every disclosure is a real button: Enter and Space press it, and click opens
		// a body that is there, closed, ready to be shown.
		sections := rail(m, handlers{})
		todo := channelTodoSection(m, handlers{})
		for name, node := range map[string]struct {
			markup string
			class  string
			field  string
		}{
			"New section":  {chatPolishMarkup(t, sections, width, theme), "section-create", `id="chat-new-section"`},
			"More options": {chatPolishMarkup(t, todo, width, theme), "channel-todo-options", `id="chat-todo-pin"`},
		} {
			disclosure := chatbugDisclosure(t, node.markup, node.class)
			button := disclosure.FirstChild
			if button == nil || button.Data != "button" || chatPolishAttr(button, "type") != "button" || chatPolishAttr(button, "aria-expanded") != "false" || hasChatPolishAttribute(button, "disabled") || chatPolishAttr(button, "tabindex") == "-1" {
				t.Fatalf("%s is not an enabled button that starts closed", name)
			}
			body := button.NextSibling
			if body == nil || chatPolishAttr(body, "data-chat-disclosure-body") != "true" || !hasChatPolishAttribute(body, "hidden") {
				t.Fatalf("%s has no closed body after its button", name)
			}
			if !strings.Contains(node.markup, node.field) {
				t.Fatalf("%s opens onto nothing: %s missing", name, node.field)
			}
		}

		// The popovers hold their content: the to-do rows and the poll's primary action
		// are inside the card that scrolls, which is a popover dialog of its own kind.
		todoCard := chatPolishMarkup(t, channelTray(m, handlers{}, "todo"), width, theme)
		cards := chatPolishNodes(t, todoCard, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-chat-layer") == "todo" })
		rows := chatPolishNodes(t, todoCard, func(n *xhtml.Node) bool {
			return chatPolishHasClass(n, "channel-todo-row") && chatPolishAncestor(n, "channel-tray-card")
		})
		if len(cards) != 1 || chatPolishAttr(cards[0], "popover") != "manual" || chatPolishAttr(cards[0], "role") != "dialog" || len(rows) != 3 {
			t.Fatalf("to-do popover: %d cards, %d rows", len(cards), len(rows))
		}
		pollCard := chatPolishMarkup(t, channelTray(m, handlers{local: localUI{pollReady: true}}, "poll"), width, theme)
		create := chatPolishNodes(t, pollCard, func(n *xhtml.Node) bool {
			return n.Data == "button" && chatPolishAttr(n, "type") == "submit" && chatPolishAncestor(n, "channel-poll-actions") && chatPolishAncestor(n, "channel-tray-card")
		})
		if len(create) != 1 || !strings.Contains(pollCard, `data-chat-layer="poll"`) {
			t.Fatalf("the poll popover does not hold its Create poll button: %d", len(create))
		}

		// A reaction picker opened on a reply is gone once its thread is.
		m.ShowThread, m.ThreadParentID, m.ThreadMessages, m.PickerID = true, "question", []Message{{ID: "reply", Author: "Alex", Body: "Reply"}}, "reply"
		if chatEmojiReactionLayer(m, localUI{}) == nil {
			t.Fatal("the picker is not drawn while its thread is open")
		}
		ClearThreadOverlays(&m)
		m.ShowThread, m.ThreadParentID, m.ThreadMessages = false, "", nil
		if chatEmojiReactionLayer(m, localUI{}) != nil || strings.Contains(chatPolishMarkup(t, Build(m), width, theme), `data-chat-layer="reaction"`) {
			t.Fatal("the reaction picker is still drawn after its thread closed")
		}

		// Nothing is left over to swallow a click once a dialog has closed.
		m.ShowCreate = true
		m.Callbacks.CloseCreate = func() {}
		open := chatPolishMarkup(t, Build(m), width, theme)
		chat4Require(t, open, "chat-dialog-backdrop")
		m.ShowCreate = false
		closed := chatPolishMarkup(t, Build(m), width, theme)
		for _, leftover := range []string{"chat-dialog-backdrop", "chat-scrim", `<dialog`} {
			if strings.Contains(closed, leftover) {
				t.Fatalf("%s remains after the dialog closed", leftover)
			}
		}
	})
}
