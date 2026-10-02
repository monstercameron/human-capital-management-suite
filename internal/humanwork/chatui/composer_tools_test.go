package chatui

import (
	"os"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func composerToolsModel(locale string, direct bool) Model {
	m := chat4Fixture(locale, "sent", direct)
	m.ChatFeatures = &ChatFeatures{Locations: true}
	m.Callbacks.OpenChannelPoll = func(string) {}
	m.Callbacks.OpenChannelTodo = func() {}
	return m
}

func composerToolsMarkup(t *testing.T, m Model, local localUI) string {
	t.Helper()
	return renderNode(t, composer(m, handlers{local: local}))
}

func composerToolsButtonsInRow(t *testing.T, markup string) []*xhtml.Node {
	t.Helper()
	return chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
		return n.Data == "button" && chatbugHasAncestorClass(n, "composer-tools") &&
			!chatPolishAncestor(n, "composer-add-menu") && !chatPolishAncestor(n, "emoji-picker") && !chatPolishAncestor(n, "giphy-picker") &&
			!chatPolishAncestor(n, "chattone-options") && !chatPolishAncestor(n, "chatmap-sheet") && !chatPolishAncestor(n, "chatvoice-panel")
	})
}

// TestTodo_CHATUX_004 pins the composer's tool row: the Add menu with only what
// is available here, the mention button, the emoji button, the Aa toggle, no
// "Markdown", no attach control, and the hint with Send at the right.
func TestTodo_CHATUX_004(t *testing.T) {
	menuKinds := func(markup string) []string {
		var kinds []string
		for _, n := range chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
			return n.Data == "button" && chatPolishAttr(n, "data-action") == "composer-add"
		}) {
			kinds = append(kinds, chatPolishAttr(n, "data-extra"))
		}
		return kinds
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		channel := composerToolsMarkup(t, composerToolsModel(locale, false), localUI{})
		if got := strings.Join(menuKinds(channel), ","); got != "poll,todo,location" {
			t.Fatalf("%s channel: Add menu holds %q, want poll,todo,location", locale, got)
		}
		direct := composerToolsMarkup(t, composerToolsModel(locale, true), localUI{})
		if got := strings.Join(menuKinds(direct), ","); got != "location,voice" {
			t.Fatalf("%s direct: Add menu holds %q, want location,voice", locale, got)
		}
		off := composerToolsModel(locale, false)
		off.ChatFeatures = &ChatFeatures{Locations: false}
		if got := strings.Join(menuKinds(composerToolsMarkup(t, off, localUI{})), ","); got != "poll,todo" {
			t.Fatalf("%s: Add menu holds %q with locations off, want poll,todo", locale, got)
		}
		if strings.Contains(channel, `aria-label="`+off.t(KeyAttach)+`"`) {
			t.Fatalf("%s: an attach control is still in the composer", locale)
		}
		for _, gone := range []string{"Markdown", "format-tools-menu", "format-kind"} {
			if strings.Contains(channel, gone) {
				t.Fatalf("%s: the composer still carries %q", locale, gone)
			}
		}
	}

	// Left to right: Add, Mention, emoji, Aa; Send last.
	m := composerToolsModel("en-US", false)
	m.Draft = "hello"
	markup := composerToolsMarkup(t, m, localUI{})
	order := []string{"composer-add-trigger", `data-action="composer-mention"`, `data-action="emoji-toggle"`, `data-action="composer-format-toggle"`, `class="send-button"`}
	at := -1
	for _, want := range order {
		i := strings.Index(markup, want)
		if i < 0 || i <= at {
			t.Fatalf("tool row order broken at %q (index %d after %d)", want, i, at)
		}
		at = i
	}
	if !strings.Contains(markup, `title="Mention someone"`) || !strings.Contains(markup, `title="Add to your message"`) {
		t.Fatal("+ and @ have no tooltip")
	}

	// The formatting row is always in the tree, holds the six tools and carries
	// the Aa choice: automatic by default, the viewer's choice over the saved one.
	rows := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "composer-format-row") })
	if len(rows) != 1 || len(chatPolishNodesIn(rows[0], func(n *xhtml.Node) bool { return n.Data == "button" })) != len(composerFormats) {
		t.Fatalf("formatting row missing or incomplete: %d rows", len(rows))
	}
	for _, c := range []struct{ saved, local, attr, pressed string }{
		{"", "", "auto", "true"}, // viewportAtLeast is true outside the browser
		{"hidden", "", "hidden", "false"},
		{"shown", "", "shown", "true"},
		{"shown", "hidden", "hidden", "false"},
		{"hidden", "shown", "shown", "true"},
		{"bogus", "", "auto", "true"},
	} {
		model := composerToolsModel("en-US", false)
		model.ComposerFormatRow = c.saved
		out := composerToolsMarkup(t, model, localUI{formatRow: c.local})
		if !strings.Contains(out, `data-format-row="`+c.attr+`"`) || !strings.Contains(out, `aria-pressed="`+c.pressed+`"`) {
			t.Fatalf("saved=%q local=%q: want format-row %s pressed %s", c.saved, c.local, c.attr, c.pressed)
		}
	}

	// The hint teaches Enter and Shift+Enter, then goes after three sends.
	if !strings.Contains(markup, `id="composer-help"`) || !strings.Contains(markup, "Enter to send, Shift+Enter for a new line") || !strings.Contains(markup, `aria-describedby="composer-help"`) {
		t.Fatal("the Enter hint is missing")
	}
	many := m
	many.Messages = []Message{{AuthorID: "alice"}, {AuthorID: "alice"}, {AuthorID: "bob"}, {AuthorID: "alice"}}
	for name, gone := range map[string]string{
		"three sent this session": composerToolsMarkup(t, m, localUI{sentCount: 3}),
		"three own messages":      composerToolsMarkup(t, many, localUI{}),
	} {
		if strings.Contains(gone, "composer-help") {
			t.Fatalf("%s: the hint is still drawn or still described", name)
		}
	}
	if !composerHintVisible(m, localUI{sentCount: 2}) || composerHintVisible(m, localUI{sentCount: 3}) {
		t.Fatal("hint threshold is not three")
	}
}

// TestTodo_CHATUX_004_Accessibility checks names, roles and references.
func TestTodo_CHATUX_004_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, direct := range []bool{false, true} {
			m := composerToolsModel(locale, direct)
			markup := composerToolsMarkup(t, m, localUI{})
			ids := map[string]int{}
			for _, n := range chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") != "" }) {
				ids[chatPolishAttr(n, "id")]++
			}
			for id, count := range ids {
				if count > 1 {
					t.Fatalf("%s: id %q appears %d times", locale, id, count)
				}
			}
			trigger := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "composer-add-trigger") })
			if len(trigger) != 1 || chatPolishAttr(trigger[0], "aria-haspopup") != "menu" || chatPolishAttr(trigger[0], "aria-expanded") != "false" || ids[chatPolishAttr(trigger[0], "aria-controls")] != 1 || chatPolishAttr(trigger[0], "aria-label") == "" {
				t.Fatalf("%s: the + button is not a labelled menu button", locale)
			}
			menu := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "composer-add-menu") })
			if len(menu) != 1 || chatPolishAttr(menu[0], "role") != "menu" || !hasChatPolishAttribute(menu[0], "hidden") || chatPolishAttr(menu[0], "aria-label") == "" {
				t.Fatalf("%s: the Add menu is not a closed, labelled menu", locale)
			}
			items := chatPolishNodesIn(menu[0], func(n *xhtml.Node) bool { return n.Data == "button" })
			if len(items) == 0 {
				t.Fatalf("%s: empty Add menu", locale)
			}
			for _, item := range items {
				if chatPolishAttr(item, "role") != "menuitem" || chatPolishAttr(item, "aria-label") == "" || ids[chatPolishAttr(item, "aria-describedby")] != 1 || chatPolishAttr(item, "type") != "button" {
					t.Fatalf("%s: a menu item lacks role, name or description: %v", locale, item.Attr)
				}
			}
			toggle := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "composer-format-toggle") })
			if len(toggle) != 1 || chatPolishAttr(toggle[0], "aria-pressed") == "" || ids[chatPolishAttr(toggle[0], "aria-controls")] != 1 || chatPolishAttr(toggle[0], "aria-label") == "" {
				t.Fatalf("%s: the Aa toggle is not a labelled toggle button", locale)
			}
			// Every button in the row has a name; only the location and voice openers,
			// which the Add menu presses, sit outside the tab order.
			for _, b := range composerToolsButtonsInRow(t, markup) {
				if chatPolishAttr(b, "aria-label") == "" && b.FirstChild == nil {
					t.Fatalf("%s: a tool button has no name: %v", locale, b.Attr)
				}
				opener := chatPolishAttr(b, "data-chatmap-action") == "toggle" || chatPolishAttr(b, "data-chatvoice-action") == "toggle"
				if opener != (chatPolishAttr(b, "tabindex") == "-1") {
					t.Fatalf("%s: tab order wrong for %v", locale, b.Attr)
				}
			}
		}
	}
}

func TestComposerMentionInsert(t *testing.T) {
	for _, c := range []struct {
		value      string
		start, end int
		want       string
		caret      int
	}{
		{"", 0, 0, "@", 1},
		{"hi ", 3, 3, "hi @", 4},
		{"hi", 2, 2, "hi @", 4},
		{"mail", 4, 4, "mail @", 6},
		{"a b", 2, 3, "a @", 3},
		{"x", 99, 99, "x @", 3},
	} {
		got, caret := composerMentionInsert(c.value, c.start, c.end)
		if got != c.want || caret != c.caret {
			t.Errorf("composerMentionInsert(%q,%d,%d) = %q,%d; want %q,%d", c.value, c.start, c.end, got, caret, c.want, c.caret)
		}
	}
}

func TestComposerAddItemsAreGatedByWhatIsAvailable(t *testing.T) {
	m := composerToolsModel("en-US", false)
	m.Callbacks.OpenChannelPoll = nil
	if kinds := composerAddItems(m); len(kinds) != 2 || kinds[0].kind != "todo" || kinds[1].kind != "location" {
		t.Fatalf("items = %+v", kinds)
	}
	m.SelectedID = ""
	if len(composerAddItems(m)) != 1 {
		t.Fatal("no conversation, but widgets offered")
	}
	group := composerToolsModel("en-US", true)
	group.Conversations[0].Kind = GroupChat
	if kinds := composerAddItems(group); len(kinds) != 2 || kinds[1].kind != "voice" {
		t.Fatalf("group conversation items = %+v", kinds)
	}
}

func TestComposerFormatChooseKeepsTheChoiceAndTellsTheClient(t *testing.T) {
	var told []bool
	m := composerToolsModel("en-US", false)
	m.Callbacks.SetComposerFormatRow = func(shown bool) { told = append(told, shown) }
	local := localStore{box: &localUI{}}
	composerFormatChoose(m, local) // automatic is shown here, so the first press hides
	if local.get().formatRow != composerFormatHidden || len(told) != 1 || told[0] {
		t.Fatalf("first press: %q %v", local.get().formatRow, told)
	}
	composerFormatChoose(m, local)
	if local.get().formatRow != composerFormatShown || len(told) != 2 || !told[1] {
		t.Fatalf("second press: %q %v", local.get().formatRow, told)
	}
}

// TestTodo_CHATUX_011_Browser pins phone width: the composer is one line until
// it has focus or text, then grows to six lines; the tool row appears with
// focus. All of it is CSS state, so nothing waits for a focus event or a timer.
func TestTodo_CHATUX_011_Browser(t *testing.T) {
	m := composerToolsModel("en-US", false)
	markup := composerToolsMarkup(t, m, localUI{})
	// :placeholder-shown needs a placeholder, and :empty needs the hint slot to
	// hold nothing while there is no hint.
	if !strings.Contains(markup, `placeholder="`) || strings.Contains(markup, `placeholder=""`) {
		t.Fatal("the field has no placeholder, so an empty composer cannot be told from a typed one")
	}
	if !strings.Contains(markup, `<div class="composer-hint-slot"></div>`) {
		t.Fatal("the hint slot is not empty without a hint")
	}
	for _, class := range []string{"composer-tools", "composer-format-row", "composer-toolbar", "composer-draft"} {
		if !strings.Contains(markup, `class="`+class+`"`) {
			t.Fatalf("the phone rules need .%s in the tree", class)
		}
	}
	phone := ChatComposerToolsStyles[strings.Index(ChatComposerToolsStyles, "@media(max-width:599px){"):]
	phone = phone[:strings.Index(phone, "}}")+2]
	for _, want := range []string{
		".composer-hint-slot:empty{display:none}",
		"min-block-size:40px",
		"max-block-size:calc(6*1.45*.9375rem + 18px)",
		":not(:focus-within):has(.composer-input:placeholder-shown){flex-direction:row",
		":is(.composer-tools,.composer-help,.composer-format-row,.composer-embeds){display:none}",
	} {
		if !strings.Contains(phone, want) {
			t.Errorf("phone rules miss %q", want)
		}
	}
	if !strings.Contains(Stylesheet, ChatComposerToolsStyles) {
		t.Error("ChatComposerToolsStyles is not part of the stylesheet")
	}
	// Nothing in the composer's own code waits for a focus or blur event or a timer.
	for _, file := range []string{"composer_tools.go", "composer_tools_js.go", "composer_commands.go", "composer_tools_styles.go"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"OnFocus", "OnBlur", `"focusin"`, `"focusout"`, `"blur"`, "setTimeout", "AfterFunc", "requestAnimationFrame", "hasFocus"} {
			if strings.Contains(string(source), banned) {
				t.Errorf("%s depends on %s", file, banned)
			}
		}
	}
}

// TestTodo_CHATUX_004_Styles pins the breakpoints: formatting row from 800 px,
// the hint from 600 px, and the viewer's choice over both.
func TestTodo_CHATUX_004_Styles(t *testing.T) {
	for _, want := range []string{
		`.chat-composer[data-format-row="shown"] .composer-format-row{display:flex}`,
		`@media(min-width:800px){.chat-workspace .chat-composer[data-format-row="auto"] .composer-format-row{display:flex}}`,
		`.composer-format-row{display:none`,
		`@media(min-width:600px){.chat-workspace .chat-composer .composer-toolbar .composer-help{display:block`,
		`.composer-toolbar .composer-help{display:none}`,
		`.composer-add-menu{position:absolute`,
		`pointer-events:none`,
	} {
		if !strings.Contains(ChatComposerToolsStyles, want) {
			t.Errorf("tool row styles miss %q", want)
		}
	}
	if composerFormatDefaultWidth != 800 || composerHintMinWidth != 600 {
		t.Fatal("breakpoints moved")
	}
}
