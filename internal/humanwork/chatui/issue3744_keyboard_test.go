package chatui

import (
	"os"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestIssue3744_KeyMatching(t *testing.T) {
	for _, c := range []struct {
		name                   string
		key, code              string
		alt, shift, ctrl, meta bool
		want                   issue3744Action
	}{
		{"alt up", "ArrowUp", "ArrowUp", true, false, false, false, issue3744Prev},
		{"alt down", "ArrowDown", "ArrowDown", true, false, false, false, issue3744Next},
		{"alt shift up", "ArrowUp", "ArrowUp", true, true, false, false, issue3744PrevUnread},
		{"alt shift down", "ArrowDown", "ArrowDown", true, true, false, false, issue3744NextUnread},
		{"mark read, option key types a symbol", "®", "KeyR", true, true, false, false, issue3744MarkRead},
		{"reply in thread", "T", "KeyT", true, true, false, false, issue3744Thread},
		{"question mark", "?", "Slash", false, true, false, false, issue3744Sheet},
		{"plain arrow is the list's own", "ArrowDown", "ArrowDown", false, false, false, false, ""},
		{"ctrl alt arrow is the system's", "ArrowDown", "ArrowDown", true, false, true, false, ""},
		{"meta alt arrow is the system's", "ArrowUp", "ArrowUp", true, false, false, true, ""},
		{"ctrl question mark", "?", "Slash", false, true, true, false, ""},
		{"alt question mark", "?", "Slash", true, true, false, false, ""},
		{"alt r without shift", "r", "KeyR", true, false, false, false, ""},
		{"search key is not ours", "k", "KeyK", false, false, true, false, ""},
	} {
		if got := issue3744Match(c.key, c.code, c.alt, c.shift, c.ctrl, c.meta); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIssue3744_StepBetweenConversations(t *testing.T) {
	ids := []string{"a", "b", "c", "d"}
	unread := []bool{false, true, false, true}
	for _, c := range []struct {
		name    string
		current string
		action  issue3744Action
		want    string
	}{
		{"next", "a", issue3744Next, "b"},
		{"previous", "c", issue3744Prev, "b"},
		{"stays at the end", "d", issue3744Next, "d"},
		{"stays at the start", "a", issue3744Prev, "a"},
		{"nothing selected goes to the first", "", issue3744Next, "a"},
		{"nothing selected goes up to the last", "", issue3744Prev, "d"},
		{"next unread skips read ones", "b", issue3744NextUnread, "d"},
		{"next unread wraps", "d", issue3744NextUnread, "b"},
		{"previous unread wraps", "b", issue3744PrevUnread, "d"},
		{"previous unread", "d", issue3744PrevUnread, "b"},
	} {
		if got := issue3744Step(ids, unread, c.current, c.action); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if got := issue3744Step(ids, []bool{false, false, false, false}, "a", issue3744NextUnread); got != "" {
		t.Errorf("no unread conversation should move nowhere, got %q", got)
	}
	if got := issue3744Step(nil, nil, "a", issue3744Next); got != "" {
		t.Errorf("empty list moved to %q", got)
	}
}

func TestIssue3744_ShortcutSheetCopy(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		rows := issue3744Shortcuts(locale, "Win32")
		if len(rows) < 7 {
			t.Fatalf("%s: %d shortcuts", locale, len(rows))
		}
		seen := map[string]bool{}
		for _, row := range rows {
			if strings.TrimSpace(row.Action) == "" || len(row.Keys) == 0 || strings.Contains(row.Action, "⟦") || strings.Contains(row.Action, "chat.issue3744") {
				t.Errorf("%s: unfinished row %+v", locale, row)
			}
			if seen[row.Action] {
				t.Errorf("%s: %q appears twice", locale, row.Action)
			}
			seen[row.Action] = true
		}
		title, closeLabel, status := issue3744SheetCopy(locale)
		if title == "" || closeLabel == "" || status == "" {
			t.Errorf("%s: sheet copy incomplete: %q %q %q", locale, title, closeLabel, status)
		}
	}
	en, _, _ := issue3744SheetCopy("en-US")
	de, _, _ := issue3744SheetCopy("de-DE")
	ar, _, _ := issue3744SheetCopy("ar")
	if en == de || en == ar || de == ar {
		t.Error("the three languages share one title")
	}
	if got := issue3744Shortcuts("en-US", "MacIntel")[0].Keys[0]; got != "Option" {
		t.Errorf("Apple keyboards print Option, got %q", got)
	}
	if got := issue3744Shortcuts("en-US", "Win32")[0].Keys[0]; got != "Alt" {
		t.Errorf("got %q, want Alt", got)
	}
	if got := strings.Join(issue3744Shortcuts("de-DE", "Win32")[6].Keys, "+"); got != "Strg+K" {
		t.Errorf("search row keys %q, want Strg+K", got)
	}
}

func TestIssue3744_SidebarDeclaresKeys(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "sent", false)
		markup := chatPolishMarkup(t, rail(m, handlers{}), 1440, "light")
		navs := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "nav" && chatPolishHasClass(n, "chat-sidebar") })
		if len(navs) != 1 {
			t.Fatalf("%s: %d sidebars", locale, len(navs))
		}
		got := chatPolishAttr(navs[0], "aria-keyshortcuts")
		for _, want := range []string{"Alt+ArrowUp", "Alt+ArrowDown", "Alt+Shift+ArrowUp", "Alt+Shift+ArrowDown"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: sidebar aria-keyshortcuts %q lacks %s", locale, got, want)
			}
		}
	}
}

// The browser binding finds its targets by these selectors. A rename in the
// markup would turn every shortcut into a silent no-op, so the markup is pinned
// here: rows by conversation id with a select button and the current mark, an
// unread badge, and a message that carries its id and a Reply button.
func TestIssue3744_BindingSelectorsMatchMarkup(t *testing.T) {
	m := chat4Fixture("en-US", "sent", false)
	m.Conversations[1].Unread = 3
	markup := chatPolishMarkup(t, rail(m, handlers{}), 1440, "light")
	rows := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
		return chatPolishHasClass(n, "chat-rail-row") && chatPolishAttr(n, "data-conversation-id") != ""
	})
	byID := map[string]*xhtml.Node{}
	for _, row := range rows {
		byID[chatPolishAttr(row, "data-conversation-id")] = row
	}
	for _, id := range []string{"general", "people"} {
		row := byID[id]
		if row == nil {
			t.Fatalf("no sidebar row for %q", id)
		}
		selects := chatPolishNodesIn(row, func(n *xhtml.Node) bool {
			return n.Data == "button" && chatPolishAttr(n, "data-action") == "select"
		})
		if len(selects) != 1 {
			t.Fatalf("%s: %d select buttons", id, len(selects))
		}
		wantCurrent := map[bool]string{true: "true", false: "false"}[id == m.SelectedID]
		if got := chatPolishAttr(selects[0], "aria-current"); got != wantCurrent {
			t.Errorf("%s: aria-current %q, want %q", id, got, wantCurrent)
		}
		badges := chatPolishNodesIn(row, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "chat-badge") })
		if (len(badges) > 0) != (id == "people") {
			t.Errorf("%s: %d unread badges", id, len(badges))
		}
	}

	msg := m.Messages[0]
	article := renderNode(t, message(m, handlers{local: localUI{focusRow: msg.ID}}, msg, false))
	articles := chatPolishNodes(t, article, func(n *xhtml.Node) bool {
		return n.Data == "article" && chatPolishHasClass(n, "message") && chatPolishAttr(n, "data-message-id") == msg.ID
	})
	if len(articles) != 1 {
		t.Fatalf("%d message articles carrying the id", len(articles))
	}
	replies := chatPolishNodesIn(articles[0], func(n *xhtml.Node) bool {
		return n.Data == "button" && chatPolishAttr(n, "data-action") == "reply"
	})
	if len(replies) != 1 {
		t.Errorf("a focused message has %d Reply buttons, want the one Alt+Shift+T presses", len(replies))
	}
	if m.Callbacks.OpenThread == nil || m.selected().Agent {
		t.Fatal("fixture must allow threads")
	}
}

// The sheet is drawn by the page's one pinned stylesheet: the server's content
// security policy refuses a style element added at run time, which would leave
// the dialog bare.
func TestIssue3744_SheetStylesAreInThePinnedStylesheet(t *testing.T) {
	for _, want := range []string{"dialog.chat-shortcuts", ".chat-shortcuts-keys", ".chat-shortcuts-close", ".chat-shortcuts-status"} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("Stylesheet lacks %s", want)
		}
	}
	source, err := os.ReadFile("issue3744_keyboard_js.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source), `"style"`) {
		t.Error("the browser binding adds a style element; the content security policy would block it")
	}
}
