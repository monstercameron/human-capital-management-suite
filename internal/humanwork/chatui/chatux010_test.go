package chatui

import (
	"os"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func chatux010Rail(t *testing.T, m Model, width int, theme string) *xhtml.Node {
	t.Helper()
	markup := chatPolishMarkup(t, rail(m, handlers{}), width, theme)
	found := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "rail-search") })
	if len(found) != 1 {
		t.Fatalf("%d search areas in the rail", len(found))
	}
	return found[0]
}

func chatux010Mixed() chatsearch.Response {
	return chatsearch.Response{Groups: []chatsearch.Group{
		{Kind: chatsearch.Conversation, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.Conversation, ID: "c1", Text: "holiday-planning"}}},
		{Kind: chatsearch.Person, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.Person, ID: "p1", Text: "Holiday Ho"}}},
		{Kind: chatsearch.Message, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.Message, ID: "m1", Text: "The holiday schedule"}}},
	}}
}

func TestTodo_CHATUX_010(t *testing.T) {
	// The key is written once, from the platform string.
	for platform, want := range map[string]string{"Win32": "Ctrl+K", "Linux x86_64": "Ctrl+K", "Windows": "Ctrl+K", "": "Ctrl+K", "MacIntel": "Cmd+K", "macOS": "Cmd+K", "iPhone": "Cmd+K", "iPad": "Cmd+K"} {
		if got := chatux010ShortcutLabel(platform); got != want {
			t.Errorf("%q: label %q, want %q", platform, got, want)
		}
	}
	if chatux010KeyShortcuts("MacIntel") != "Meta+K" || chatux010KeyShortcuts("Win32") != "Control+K" {
		t.Error("aria-keyshortcuts does not follow the platform")
	}

	// Which key presses take the shortcut.
	for _, c := range []struct {
		name                   string
		apple                  bool
		key                    string
		ctrl, meta, alt, shift bool
		want                   bool
	}{
		{"ctrl+k elsewhere", false, "k", true, false, false, false, true},
		{"ctrl+K capital", false, "K", true, false, false, false, true},
		{"cmd+k on apple", true, "k", false, true, false, false, true},
		{"cmd+k elsewhere is not it", false, "k", false, true, false, false, false},
		{"ctrl+k on apple stays the text field's", true, "k", true, false, false, false, false},
		{"plain k", false, "k", false, false, false, false, false},
		{"ctrl+shift+k", false, "k", true, false, false, true, false},
		{"ctrl+alt+k", false, "k", true, false, true, false, false},
		{"ctrl+j", false, "j", true, false, false, false, false},
	} {
		if got := chatux010ShortcutMatches(c.apple, c.key, c.ctrl, c.meta, c.alt, c.shift); got != c.want {
			t.Errorf("%s: matches=%v, want %v", c.name, got, c.want)
		}
	}

	// The sidebar box: one name, the key inside it.
	for locale, name := range map[string]string{"en-US": "Search Chat", "de-DE": "Chat durchsuchen", "ar": "البحث في الدردشة"} {
		m := chat4Fixture(locale, "sent", false)
		area := chatux010Rail(t, m, 1440, "light")
		inputs := chatPolishNodesIn(area, func(n *xhtml.Node) bool { return n.Data == "input" && chatPolishAttr(n, "id") == "chat-search" })
		if len(inputs) != 1 {
			t.Fatalf("%s: %d search boxes", locale, len(inputs))
		}
		if got := chatPolishAttr(inputs[0], "placeholder"); got != name {
			t.Fatalf("%s: the box reads %q, want %q", locale, got, name)
		}
		labels := chatPolishNodesIn(area, func(n *xhtml.Node) bool { return n.Data == "label" && chatPolishAttr(n, "for") == "chat-search" })
		if len(labels) != 1 || chatbug030Text(labels[0]) != name {
			t.Fatalf("%s: the box is labelled %v", locale, labels)
		}
		hints := chatPolishNodesIn(area, func(n *xhtml.Node) bool { return n.Data == "kbd" })
		if len(hints) != 1 || chatbug030Text(hints[0]) != "Ctrl+K" {
			t.Fatalf("%s: the shortcut inside the box is %v", locale, hints)
		}
		// Typing words replaces the hint with the browser's clear control.
		m.Search = "holiday"
		if hints = chatPolishNodesIn(chatux010Rail(t, m, 1440, "light"), func(n *xhtml.Node) bool { return n.Data == "kbd" }); len(hints) != 0 {
			t.Fatalf("%s: the shortcut is drawn over a typed query", locale)
		}
	}

	// No other name for the same search is left on the page.
	page := chatPolishMarkup(t, Build(chat4Fixture("en-US", "sent", false)), 1440, "light")
	for _, old := range []string{"Search messages", "Search channels, people, and messages"} {
		if strings.Contains(page, old) {
			t.Fatalf("the page still calls the search %q", old)
		}
	}
	if !strings.Contains(page, `title="Search Chat (Ctrl+K)"`) {
		t.Fatal("the header's search button does not name Search Chat with its key")
	}

	// Result groups are named in plain words.
	for locale, want := range map[string][]string{
		"en-US": {"Conversations", "People", "Messages"},
		"de-DE": {"Unterhaltungen", "Personen", "Nachrichten"},
		"ar":    {"المحادثات", "الأشخاص", "الرسائل"},
	} {
		view := ChatSearchView{Query: "holiday", Response: chatux010Mixed()}
		var heads []string
		root, _ := xhtml.Parse(strings.NewReader(renderNode(t, RenderChatSearch(locale, view))))
		for _, h := range chatPolishNodesIn(root, func(n *xhtml.Node) bool { return n.Data == "h3" }) {
			heads = append(heads, chatbug030Text(h))
		}
		if len(heads) != 3 {
			t.Fatalf("%s: %d group headings: %v", locale, len(heads), heads)
		}
		for i, w := range want {
			if !strings.HasPrefix(heads[i], w) {
				t.Fatalf("%s: group %d is %q, want %q", locale, i, heads[i], w)
			}
		}
		if locale == "en-US" {
			for _, internal := range []string{"Channel", "Person ·", "Message ·"} {
				for _, h := range heads {
					if strings.HasPrefix(h, internal) {
						t.Fatalf("a group is still named %q", h)
					}
				}
			}
		}
	}
	m := chat4Fixture("en-US", "sent", false)
	for key, want := range map[string]string{KeySearchChannels: "Conversations", KeySearchPeople: "People", KeySearchMessages: "Messages"} {
		if got := chatbug030Text(parseOne(t, renderNode(t, searchGroupHeading(m, key, 2)), "h3")); !strings.HasPrefix(got, want) {
			t.Fatalf("legacy group %s reads %q", key, got)
		}
	}
	if got := chatsearchKind("en-US", chatsearch.Pin); got != "Pinned messages" {
		t.Fatalf("the per-row kind label changed: %q", got)
	}
}

func parseOne(t *testing.T, markup, tag string) *xhtml.Node {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	found := chatPolishNodesIn(root, func(n *xhtml.Node) bool { return n.Data == tag })
	if len(found) == 0 {
		t.Fatalf("no <%s> in %s", tag, markup)
	}
	return found[0]
}

func TestTodo_CHATUX_010_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		area := chatux010Rail(t, chat4Fixture(locale, "sent", false), 390, "light")
		input := chatPolishNodesIn(area, func(n *xhtml.Node) bool { return n.Data == "input" })[0]
		if chatPolishAttr(input, "type") != "search" || chatPolishAttr(input, "aria-keyshortcuts") != "Control+K" {
			t.Fatalf("%s: the box does not announce its key", locale)
		}
		// The visible key is decoration: a reader already hears it from the box.
		kbd := chatPolishNodesIn(area, func(n *xhtml.Node) bool { return n.Data == "kbd" })[0]
		if chatPolishAttr(kbd, "aria-hidden") != "true" {
			t.Fatalf("%s: the key label is read as well as announced", locale)
		}
		// A key combination is Latin text in a right-to-left page too.
		if chatPolishAttr(kbd, "dir") != "ltr" {
			t.Fatalf("%s: the key label has no direction", locale)
		}
		// The visible name is part of the accessible name (label in name).
		labels := chatPolishNodesIn(area, func(n *xhtml.Node) bool { return n.Data == "label" })
		if len(labels) != 1 || chatbug030Text(labels[0]) != chatPolishAttr(input, "placeholder") {
			t.Fatalf("%s: label %v does not match the placeholder %q", locale, labels, chatPolishAttr(input, "placeholder"))
		}
	}
	// Nothing about the shortcut waits for a focus event or a timer.
	source, err := os.ReadFile("chatux010_js.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"addEventListener", "focus`, "setTimeout", "hasFocus", "time.Sleep"} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("the shortcut depends on %s", forbidden)
		}
	}
	for _, want := range []string{`"keydown"`, `dialog,[role=dialog],[aria-modal=true]`, `input,textarea,select,[contenteditable='true']`, "chatux010IsApple(chatPlatform())"} {
		if !strings.Contains(string(source), want) {
			t.Fatalf("the shortcut listener lost %q", want)
		}
	}
}

func TestTodo_CHATUX_010_Browser(t *testing.T) {
	chatPolishMatrix(t, func(t *testing.T, m Model, width int, theme string) {
		count := func(markup string, match func(*xhtml.Node) bool) int { return len(chatPolishNodes(t, markup, match)) }
		page := chatPolishMarkup(t, Build(m), width, theme)
		if n := count(page, func(n *xhtml.Node) bool { return n.Data == "input" && chatPolishAttr(n, "id") == "chat-search" }); n != 1 {
			t.Fatalf("%d search boxes", n)
		}
		if n := count(page, func(n *xhtml.Node) bool { return n.Data == "kbd" && chatPolishHasClass(n, "chat-search-shortcut") }); n != 1 {
			t.Fatalf("%d shortcut labels", n)
		}
		// The header button reaches the same box rather than opening another.
		if n := count(page, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-action") == "chat-search-open" }); n != 1 {
			t.Fatalf("%d header search buttons", n)
		}
		if strings.Contains(page, "chat-search-layer") {
			t.Fatal("a second search box is drawn")
		}
		// A search in progress: still one box, the key gone, results grouped in plain words.
		m.Search = "holiday"
		view := ChatSearchView{Query: "holiday", Response: chatux010Mixed()}
		m.ChatSearch = &view
		page = chatPolishMarkup(t, Build(m), width, theme)
		if n := count(page, func(n *xhtml.Node) bool { return n.Data == "kbd" && chatPolishHasClass(n, "chat-search-shortcut") }); n != 0 {
			t.Fatalf("%d shortcut labels over a typed query", n)
		}
		groups := chatPolishNodes(t, page, func(n *xhtml.Node) bool { return n.Data == "h3" && chatPolishAncestor(n, "chatsearch-view") })
		if len(groups) != 3 || !strings.HasPrefix(chatbug030Text(groups[0]), chatux010GroupName(m.Locale, chatsearch.Conversation)) {
			t.Fatalf("result groups: %d", len(groups))
		}
	})
	// The label sits inside the box at the end of the line, and the box leaves it room.
	for _, want := range []string{".rail-search .chat-search-shortcut{position:absolute;inset-inline-end:", ".rail-search:has(.chat-search-shortcut) .chat-search{padding-inline-end:", "pointer-events:none"} {
		if !strings.Contains(ChatUX010Styles, want) {
			t.Fatalf("shortcut rules lost %q", want)
		}
	}
	if !strings.Contains(Stylesheet, ChatUX010Styles) {
		t.Fatal("the shortcut rules are not served")
	}
}
