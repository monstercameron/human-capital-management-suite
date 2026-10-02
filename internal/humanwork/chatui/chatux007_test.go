package chatui

import (
	"os"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func chatux007Model(locale string) Model {
	m := chatux002Model(locale)
	m.SelectedID = "general"
	m.SavedOpenCount = 2
	m.Conversations = []Conversation{
		{ID: "general", Name: "general", Kind: PublicChannel, Joined: true, Unread: 4},
		{ID: "read", Name: "read-room", Kind: PublicChannel, Joined: true},
		{ID: "unread", Name: "unread-room", Kind: PublicChannel, Joined: true, Unread: 3},
		{ID: "mention", Name: "mention-room", Kind: PublicChannel, Joined: true, Unread: 5, Mentions: 2},
		{ID: "muted", Name: "muted-room", Kind: PublicChannel, Joined: true, Unread: 9, Muted: true},
	}
	m.Callbacks.SelectConversation = func(string) {}
	return m
}

func chatux007Row(t *testing.T, markup, id string) *xhtml.Node {
	t.Helper()
	rows := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
		return n.Data == "button" && chatPolishAttr(n, "data-action") == "select" && chatPolishAttr(n, "data-id") == id
	})
	if len(rows) != 1 {
		t.Fatalf("%d rows for %s", len(rows), id)
	}
	return rows[0]
}

func chatux007Badges(row *xhtml.Node) []*xhtml.Node {
	return chatux002Descend(row, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chat-badge") })
}

func TestTodo_CHATUX_007(t *testing.T) {
	m := chatux007Model("en-US")
	markup := chatPolishMarkup(t, rail(m, handlers{}), 1440, "light")

	// Saved: its open count is plain muted text, not a badge.
	counts := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-saved-count") == "true" })
	if len(counts) != 1 || chatux002NodeText(counts[0]) != "2" || chatPolishHasClass(counts[0], "chat-badge") || !chatPolishHasClass(counts[0], "chatsave-count") || hasChatPolishAttribute(counts[0], "hidden") {
		t.Fatalf("the Saved count is not plain text: %d nodes", len(counts))
	}
	if got := chatbugCascadeValue(Stylesheet, ".chatsave-count", "color"); got != "var(--muted)" {
		t.Fatalf("Saved count colour = %q, want muted text", got)
	}
	if got := chatbugCascadeValue(Stylesheet, ".chatsave-count", "background"); got != "" {
		t.Fatalf("the Saved count has a fill: %q", got)
	}
	m.SavedOpenCount = 0
	if empty := chatPolishMarkup(t, chatsaveSidebar(m), 1440, "light"); !strings.Contains(empty, `data-saved-count="true"`) || !strings.Contains(empty, "hidden") {
		t.Fatal("with nothing saved the count is not hidden")
	}

	// A read conversation: plain, no count. Unread: bold marker and a neutral
	// count. Mentioned: the count is the mention count, drawn as a mention.
	read, unread, mention, muted, selected := chatux007Row(t, markup, "read"), chatux007Row(t, markup, "unread"), chatux007Row(t, markup, "mention"), chatux007Row(t, markup, "muted"), chatux007Row(t, markup, "general")
	if chatPolishHasClass(read, "unread") || len(chatux007Badges(read)) != 0 {
		t.Fatal("a read conversation is marked or counted")
	}
	if !chatPolishHasClass(unread, "unread") || chatPolishHasClass(unread, "selected") {
		t.Fatalf("an unread conversation is not marked unread: %s", chatPolishAttr(unread, "class"))
	}
	if badges := chatux007Badges(unread); len(badges) != 1 || chatPolishHasClass(badges[0], "mention") || chatux002NodeText(badges[0]) != "3" || !strings.Contains(chatPolishAttr(badges[0], "aria-label"), "3") {
		t.Fatalf("an unread conversation does not carry its unread count: %d badges", len(badges))
	}
	if !chatPolishHasClass(mention, "unread") {
		t.Fatal("a mentioned conversation is not marked unread")
	}
	if badges := chatux007Badges(mention); len(badges) != 1 || !chatPolishHasClass(badges[0], "mention") || chatux002NodeText(badges[0]) != "2" || !strings.Contains(chatPolishAttr(badges[0], "aria-label"), "2") {
		t.Fatalf("a mentioned conversation does not carry its mention count: %d badges", len(badges))
	}
	if !chatPolishHasClass(muted, "muted") {
		t.Fatal("a muted conversation is not marked muted")
	}
	if !chatPolishHasClass(selected, "selected") || chatPolishAttr(selected, "class") == chatPolishAttr(unread, "class") {
		t.Fatal("the open conversation is not told apart")
	}
	// A mention is unread by definition, even if the count of unread posts has not arrived.
	only := chatPolishMarkup(t, railRow(Model{SelectedID: "x", Callbacks: Callbacks{SelectConversation: func(string) {}}}, Conversation{ID: "m", Name: "m", Kind: PublicChannel, Mentions: 1}), 1440, "light")
	if !strings.Contains(only, `class="chat-row unread"`) {
		t.Fatalf("a conversation with only a mention is not unread: %s", only)
	}

	// The look. No count in the list is a filled pill; an unread row is bold; a
	// mention count is the accent colour; the selected row is the only filled row.
	for selector, want := range map[string]map[string]string{
		".chat-rail-row .chat-badge":         {"background": "none", "color": "var(--ink)"},
		".chat-rail-row .chat-badge.mention": {"background": "none", "color": "var(--accent)"},
		".chat-rail-row .chat-row.unread":    {"font-weight": "700", "color": "var(--ink)"},
	} {
		for property, value := range want {
			if got := chatbugCascadeValue(Stylesheet, selector, property); got != value {
				t.Fatalf("%s %s = %q, want %q", selector, property, got, value)
			}
		}
	}
	if got := chatbugCascadeValue(Stylesheet, ".chat-row.selected", "background"); !strings.Contains(got, "var(--accent)") {
		t.Fatalf("the selected row is not filled: %q", got)
	}
	if strings.Contains(ChatUX007Styles, "danger") {
		t.Fatal("the list's counts use the alert colour")
	}
	if strings.LastIndex(Stylesheet, ".chat-rail-row .chat-badge.mention{") < strings.LastIndex(Stylesheet, ".chat-badge.mention{background:var(--hcm-color-danger)") {
		t.Fatal("the red mention badge is drawn over the accent count")
	}
}

func TestTodo_CHATUX_007_JumpToUnread(t *testing.T) {
	// The unread conversations can be out of view: the list scrolls. The control
	// appears on the side where one is, and takes the reader to the nearest.
	view := [2]float64{100, 400}
	row := func(top float64) chatux007Span { return chatux007Span{top, top + 32} }
	cases := []struct {
		name         string
		rows         []chatux007Span
		above, below bool
		up, down     int
	}{
		{"none", nil, false, false, -1, -1},
		{"all in view", []chatux007Span{row(110), row(300)}, false, false, -1, -1},
		{"clipped by a few pixels still counts as in view", []chatux007Span{row(86), row(380)}, false, false, -1, -1},
		{"mostly clipped counts as out of view", []chatux007Span{row(60), row(395)}, true, true, 0, 1},
		{"one above", []chatux007Span{row(20), row(200)}, true, false, 0, -1},
		{"two below", []chatux007Span{row(200), row(500), row(900)}, false, true, -1, 1},
		{"both sides", []chatux007Span{row(-300), row(-100), row(200), row(450), row(800)}, true, true, 1, 3},
	}
	for _, c := range cases {
		above, below := chatux007OutOfView(view[0], view[1], c.rows)
		if above != c.above || below != c.below {
			t.Errorf("%s: out of view above=%v below=%v, want %v %v", c.name, above, below, c.above, c.below)
		}
		if got := chatux007Target(chatux007Up, view[0], view[1], c.rows); got != c.up {
			t.Errorf("%s: jump up goes to %d, want %d", c.name, got, c.up)
		}
		if got := chatux007Target(chatux007Down, view[0], view[1], c.rows); got != c.down {
			t.Errorf("%s: jump down goes to %d, want %d", c.name, got, c.down)
		}
	}
	if above, below := chatux007Hidden(0, 100, chatux007Span{50, 50}); above || below {
		t.Fatal("a row with no height is out of view")
	}

	// Both controls are in the tree, closed, ready for the client to open: a click
	// is the plain "jump-unread" action with its direction.
	m := chatux007Model("en-US")
	markup := chatPolishMarkup(t, rail(m, handlers{}), 390, "dark")
	for _, direction := range []string{chatux007Up, chatux007Down} {
		buttons := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "button" && chatPolishAttr(n, "data-jump") == direction })
		if len(buttons) != 1 {
			t.Fatalf("%d jump %s controls", len(buttons), direction)
		}
		b := buttons[0]
		if !hasChatPolishAttribute(b, "hidden") || chatPolishAttr(b, "data-action") != "jump-unread" || chatPolishAttr(b, "data-id") != direction || chatPolishAttr(b, "type") != "button" || chatPolishAttr(b, "aria-label") != chatux002Text(m, keyChatux007Jump) || !chatPolishAncestor(b, "rail-scroll") {
			t.Fatalf("jump %s is not a closed, named button in the list: %v", direction, b.Attr)
		}
	}
	// The client keeps them in step with the list on every scroll, resize and
	// change, and writes only what changed.
	if body := chatbugFuncBody(t, "agentux_chat5_layers_js.go", "syncChatAnchoredLayers"); !strings.Contains(body, "chatux007SyncJump()") {
		t.Fatal("the list's scroll position does not reach the jump controls")
	}
	if body := chatbugFuncBody(t, "chatux007_jump_js.go", "chatux007SyncJump"); !strings.Contains(body, `Get("hidden").Bool() ==`) {
		t.Fatal("the jump controls are written on every sync, which would wake the observer that runs it")
	}
	if source, err := os.ReadFile("render.go"); err != nil || !strings.Contains(strings.ReplaceAll(string(source), "\r\n", "\n"), "case \"jump-unread\":\n\t\tchatux007JumpTo(id)") {
		t.Fatal("the jump action is not handled")
	}
}
