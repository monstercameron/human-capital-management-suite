package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

type composerCommandLog struct {
	cleared, giphy, location int
	query, notice            string
}

func (l *composerCommandLog) runtime(m Model) composerCommandRuntime {
	return composerCommandRuntime{
		Model:        m,
		ClearDraft:   func() { l.cleared++ },
		Notice:       func(text string) { l.notice = text },
		OpenGiphy:    func(query string) { l.giphy++; l.query = query },
		OpenLocation: func() { l.location++ },
	}
}

func TestTodo_CHATCMD_001(t *testing.T) {
	m := composerToolsModel("en-US", false)
	m.GiphyAPIKey = "key"

	// Parsing: one slash and a valid name, then the rest as text.
	for _, c := range []struct {
		in, name, args string
		ok             bool
	}{
		{"/giphy cats", "giphy", "cats", true},
		{"  /giphy   cats and dogs ", "giphy", "cats and dogs", true},
		{"/location", "location", "", true},
		{"/GIPHY", "GIPHY", "", true},
		{"/usr/bin/go", "", "", false},
		{"//giphy", "", "", false},
		{"/", "", "", false},
		{"giphy", "", "", false},
		{"/1abc", "", "", false},
	} {
		name, args, ok := parseComposerCommand(c.in)
		if name != c.name || args != c.args || ok != c.ok {
			t.Errorf("parseComposerCommand(%q) = %q,%q,%v; want %q,%q,%v", c.in, name, args, ok, c.name, c.args, c.ok)
		}
	}

	// Dispatch through the registry, as send() does.
	var log composerCommandLog
	text, consumed := composerSendCommand(log.runtime(m), "/giphy cats")
	if !consumed || text != "" || log.giphy != 1 || log.query != "cats" || log.cleared != 1 {
		t.Fatalf("/giphy cats: consumed=%v text=%q log=%+v", consumed, text, log)
	}
	text, consumed = composerSendCommand(log.runtime(m), "/location")
	if !consumed || text != "" || log.location != 1 || log.cleared != 2 {
		t.Fatalf("/location: consumed=%v text=%q log=%+v", consumed, text, log)
	}
	// An unknown word is not posted: the composer says so and keeps the draft.
	log = composerCommandLog{}
	text, consumed = composerSendCommand(log.runtime(m), "/nonsense now")
	if !consumed || text != "" || log.cleared != 0 || !strings.Contains(log.notice, "/nonsense is not a command here") || !strings.Contains(log.notice, "//") {
		t.Fatalf("/nonsense: consumed=%v text=%q log=%+v", consumed, text, log)
	}
	// A line starting with // posts one slash and runs nothing.
	log = composerCommandLog{}
	text, consumed = composerSendCommand(log.runtime(m), "//giphy cats")
	if consumed || text != "/giphy cats" || log.giphy != 0 {
		t.Fatalf("//giphy: consumed=%v text=%q log=%+v", consumed, text, log)
	}
	// Ordinary text and path-like lines go through untouched.
	for _, body := range []string{"hello /giphy", "/usr/bin/go is here", "no slash"} {
		if text, consumed := composerSendCommand(log.runtime(m), body); consumed || text != body {
			t.Errorf("%q changed: consumed=%v text=%q", body, consumed, text)
		}
	}
	// /giphy without a configured key says why and posts nothing.
	log = composerCommandLog{}
	noKey := m
	noKey.GiphyAPIKey = ""
	text, consumed = composerSendCommand(log.runtime(noKey), "/giphy cats")
	if !consumed || log.giphy != 0 || log.cleared != 0 || log.notice != noKey.t(KeyGiphyUnavailable) {
		t.Fatalf("/giphy without a key: consumed=%v log=%+v", consumed, log)
	}
	// A command that is not available here is absent from the menu and unknown.
	off := m
	off.ChatFeatures = &ChatFeatures{Locations: false}
	log = composerCommandLog{}
	if _, consumed := composerSendCommand(log.runtime(off), "/location"); !consumed || log.location != 0 || !strings.Contains(log.notice, "/location is not a command here") {
		t.Fatalf("/location with locations off: %+v", log)
	}
	for _, c := range defaultComposerCommands().menu(off, "") {
		if c.Name == "location" {
			t.Fatal("/location is in the menu with locations off")
		}
	}

	// The menu: names that start with the query, then names that contain it.
	registry := defaultComposerCommands()
	if names := composerCommandNames(registry.menu(m, "")); names != "giphy,location" {
		t.Fatalf("menu = %s", names)
	}
	if names := composerCommandNames(registry.menu(m, "gi")); names != "giphy" {
		t.Fatalf("menu(gi) = %s", names)
	}
	if names := composerCommandNames(registry.menu(m, "cat")); names != "location" {
		t.Fatalf("menu(cat) = %s", names)
	}
	if names := composerCommandNames(registry.menu(m, "zzz")); names != "" {
		t.Fatalf("menu(zzz) = %s", names)
	}

	// The list opens for a slash at the start of the draft while its first word
	// is being typed, and for nothing else.
	for _, c := range []struct {
		value string
		caret int
		open  bool
		query string
	}{
		{"/", 1, true, ""},
		{"/gi", 3, true, "gi"},
		{"/gi", 2, false, ""},
		{"/gi cats", 3, true, "gi"},
		{"/gi cats", 6, false, ""},
		{"hi /gi", 6, false, ""},
		{"/usr/bin", 8, false, ""},
		{"/zzz", 4, false, ""},
		{"", 0, false, ""},
	} {
		next := composerCommandMenuNext(m, composerCommandMenu{}, "chat-composer", c.value, c.caret)
		if next.Open != c.open || next.Query != c.query {
			t.Errorf("menu for %q caret %d = %+v; want open=%v query=%q", c.value, c.caret, next, c.open, c.query)
		}
	}

	// Keys: arrows wrap, Escape closes and leaves the text, other keys are not the list's.
	state := composerCommandMenu{Target: "chat-composer", Open: true}
	if next, handled := composerCommandMenuMove(state, "ArrowDown", 2); !handled || next.Active != 1 {
		t.Fatalf("ArrowDown: %+v", next)
	}
	if next, handled := composerCommandMenuMove(composerCommandMenu{Open: true, Active: 1}, "ArrowDown", 2); !handled || next.Active != 0 {
		t.Fatalf("ArrowDown wraps: %+v", next)
	}
	if next, handled := composerCommandMenuMove(state, "ArrowUp", 2); !handled || next.Active != 1 {
		t.Fatalf("ArrowUp wraps: %+v", next)
	}
	if next, handled := composerCommandMenuMove(state, "Escape", 2); !handled || next.Open {
		t.Fatalf("Escape: %+v", next)
	}
	if _, handled := composerCommandMenuMove(state, "a", 2); handled {
		t.Fatal("a letter is not the list's")
	}
	if _, handled := composerCommandMenuMove(composerCommandMenu{}, "ArrowDown", 2); handled {
		t.Fatal("a closed list took a key")
	}

	// Choosing writes "/name " and leaves the caret after the space.
	for _, c := range []struct {
		value string
		caret int
		name  string
		want  string
		next  int
	}{
		{"/", 1, "giphy", "/giphy ", 7},
		{"/gi", 3, "giphy", "/giphy ", 7},
		{"/gi cats", 3, "giphy", "/giphy cats", 7},
	} {
		got, caret := composerCommandApply(c.value, c.caret, c.name)
		if got != c.want || caret != c.next {
			t.Errorf("apply(%q,%d) = %q,%d; want %q,%d", c.value, c.caret, got, caret, c.want, c.next)
		}
	}

	// The registry has the one comparison for each name: the old helpers read it.
	if q, ok := giphyCommand("/giphy cats"); !ok || q != "cats" {
		t.Fatalf("giphyCommand = %q %v", q, ok)
	}
	if !ChatmapSlashCommand("/location") || ChatmapSlashCommand("/location now") {
		t.Fatal("ChatmapSlashCommand disagrees with the registry")
	}
}

func composerCommandNames(commands []composerCommand) string {
	names := make([]string, len(commands))
	for i, c := range commands {
		names[i] = c.Name
	}
	return strings.Join(names, ",")
}

// TestTodo_CHATCMD_001_Browser renders the open list in every language and the
// field's references to it.
func TestTodo_CHATCMD_001_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := composerToolsModel(locale, false)
		local := localUI{commandMenu: composerCommandMenu{Target: "chat-composer", Query: "", Open: true, Active: 1}}
		markup := composerToolsMarkup(t, m, local)
		menu := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "command-menu") })
		if len(menu) != 1 || chatPolishAttr(menu[0], "role") != "listbox" || chatPolishAttr(menu[0], "aria-label") != composerText(m, keyComposerCommands) {
			t.Fatalf("%s: no labelled command list", locale)
		}
		options := chatPolishNodesIn(menu[0], func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "option" })
		if len(options) != 2 {
			t.Fatalf("%s: %d options, want /giphy and /location", locale, len(options))
		}
		for i, option := range options {
			wantSelected := "false"
			if i == 1 {
				wantSelected = "true"
			}
			if chatPolishAttr(option, "aria-selected") != wantSelected || chatPolishAttr(option, "data-action") != "command-pick" || chatPolishAttr(option, "tabindex") != "-1" || chatPolishAttr(option, "title") == "" {
				t.Fatalf("%s: option %d wrong: %v", locale, i, option.Attr)
			}
		}
		if !strings.Contains(markup, composerText(m, keyComposerCommandGiphy)) || !strings.Contains(markup, "/giphy") || !strings.Contains(markup, "/location") {
			t.Fatalf("%s: the list does not name the commands and what they do", locale)
		}
		field := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == "chat-composer" && n.Data == "textarea" })
		if len(field) != 1 || chatPolishAttr(field[0], "aria-controls") != "chat-composer-commands" || chatPolishAttr(field[0], "aria-activedescendant") != "chat-composer-command-2" || chatPolishAttr(field[0], "aria-autocomplete") != "list" {
			t.Fatalf("%s: the field does not point at the open list: %v", locale, field[0].Attr)
		}
		if len(chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == "chat-composer-command-2" })) != 1 {
			t.Fatalf("%s: aria-activedescendant names nothing", locale)
		}
		// Closed: no list and no references to it.
		closed := composerToolsMarkup(t, m, localUI{})
		if strings.Contains(closed, "command-menu\"") || strings.Contains(closed, "aria-activedescendant") || strings.Contains(closed, "chat-composer-commands") {
			t.Fatalf("%s: a closed list leaves traces", locale)
		}
	}
	// Locations off: /location is not offered.
	m := composerToolsModel("en-US", false)
	m.ChatFeatures = &ChatFeatures{Locations: false}
	markup := composerToolsMarkup(t, m, localUI{commandMenu: composerCommandMenu{Target: "chat-composer", Open: true}})
	if strings.Contains(markup, ">/location<") || !strings.Contains(markup, ">/giphy<") {
		t.Fatal("the list offers /location with locations off")
	}
}

// TestTodo_CHATCMD_001_Accessibility: the list's rows are labelled by their own
// text and the unknown-command line is a status.
func TestTodo_CHATCMD_001_Accessibility(t *testing.T) {
	m := composerToolsModel("en-US", false)
	markup := composerToolsMarkup(t, m, localUI{composerNotice: composerUnknownNotice(m, "nonsense")})
	if !strings.Contains(markup, `role="status"`) || !strings.Contains(markup, "/nonsense is not a command here") {
		t.Fatal("the unknown-command line is not announced")
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		text := composerUnknownNotice(composerToolsModel(locale, false), "x")
		if !strings.Contains(text, "/x") || strings.Contains(text, "{name}") || strings.Contains(text, "⟦") {
			t.Fatalf("%s: notice %q", locale, text)
		}
	}
}
