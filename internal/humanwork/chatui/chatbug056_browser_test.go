package chatui_test

import (
	"html"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	xhtml "golang.org/x/net/html"
)

// chatbug056Options is the rows of the command list in a rendered composer,
// and the list itself.
func chatbug056Options(t *testing.T, markup string) (list *xhtml.Node, rows []*xhtml.Node) {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		for _, attr := range n.Attr {
			if attr.Key == "id" && attr.Val == "chat-composer-commands" {
				list = n
			}
			if attr.Key == "role" && attr.Val == "option" {
				rows = append(rows, n)
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if list == nil {
		t.Fatal("the composer has no command list")
	}
	return list, rows
}

func chatbug056Attr(n *xhtml.Node, key string) (string, bool) {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val, true
		}
	}
	return "", false
}

func chatbug056Text(n *xhtml.Node) string {
	if n.Type == xhtml.TextNode {
		return n.Data
	}
	var text strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		text.WriteString(chatbug056Text(child))
	}
	return text.String()
}

// TestTodo_CHATBUG_056_Browser renders the composer with the product's own
// catalog in the three languages and reads the command list as the page holds
// it: every command the conversation supports with a description, a hint that
// names only the list's keys, rows that narrow with the typed word, and the
// rules that keep every row on screen (page findings 5 and 6 of 2026-10-02:
// the list is in the page before the "/" so the keystroke can show it at once,
// and it is tall enough for all of its rows).
func TestTodo_CHATBUG_056_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			m := chatcmd002CatalogModel(locale)
			m.GiphyAPIKey = "key"
			m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}

			open := chatui.ComposerMarkupForTest(t, m, "", true)
			if text := html.UnescapeString(open); strings.ContainsAny(text, "⟦⟧") || strings.Contains(text, "cmd-poll") || strings.Contains(text, "cmd-keys") {
				t.Fatal("the composer prints a key in place of its words")
			}
			list, rows := chatbug056Options(t, open)
			if state, _ := chatbug056Attr(list, "data-open"); !strings.HasPrefix(state, "true:") {
				t.Fatalf("the open list is marked %q", state)
			}
			if role, _ := chatbug056Attr(list, "role"); role != "listbox" {
				t.Fatalf("the list's role is %q", role)
			}
			names := []string{}
			for i, row := range rows {
				name, _ := chatbug056Attr(row, "data-command")
				names = append(names, name)
				if _, hidden := chatbug056Attr(row, "hidden"); hidden {
					t.Errorf("/%s is hidden in the full list", name)
				}
				words := strings.TrimSpace(strings.TrimPrefix(chatbug056Text(row), "/"+name))
				if len([]rune(words)) < 8 {
					t.Errorf("/%s has no description: %q", name, words)
				}
				if id, _ := chatbug056Attr(row, "id"); id != "chat-composer-command-"+string(rune('1'+i)) {
					t.Errorf("/%s is row %q", name, id)
				}
			}
			if strings.Join(names, ",") != "poll,todo,giphy,location" {
				t.Fatalf("the list holds %v", names)
			}
			hint := html.UnescapeString(open)
			for _, key := range []string{"↑↓", "Enter", "Tab", "Esc"} {
				if !strings.Contains(hint, key) {
					t.Errorf("the hint does not name %s", key)
				}
			}
			if locale == "en-US" && (strings.Contains(hint, "Tab details") || !strings.Contains(hint, "Enter or Tab to choose")) {
				t.Error("the hint names a key that does nothing for a command")
			}

			// Typing narrows the list: the rows stay in the page and the ones
			// that do not match are hidden, so the keystroke can do the same.
			_, narrowed := chatbug056Options(t, chatui.ComposerMarkupForTest(t, m, "po", true))
			shown := []string{}
			for _, row := range narrowed {
				name, _ := chatbug056Attr(row, "data-command")
				if _, hidden := chatbug056Attr(row, "hidden"); !hidden {
					shown = append(shown, name)
					if selected, _ := chatbug056Attr(row, "aria-selected"); selected != "true" {
						t.Errorf("the only match, /%s, is not the highlighted row", name)
					}
				}
			}
			if len(narrowed) != 4 || strings.Join(shown, ",") != "poll" {
				t.Fatalf("after /po the list shows %v of %d rows", shown, len(narrowed))
			}

			// Closed, the list is in the page and marked closed.
			closedList, closedRows := chatbug056Options(t, chatui.ComposerMarkupForTest(t, m, "", false))
			if state, _ := chatbug056Attr(closedList, "data-open"); !strings.HasPrefix(state, "false:") || len(closedRows) != 4 {
				t.Fatalf("the closed list is marked %q with %d rows", state, len(closedRows))
			}
		})
	}
	for _, rule := range []string{
		`.chat-workspace .command-menu:not([data-open^="true"]){display:none}`,
		`.chat-workspace .command-menu>.command-option{flex:none}`,
		`.chat-workspace .command-menu>.command-option[hidden]{display:none}`,
		`max-height:min(440px,70vh);overflow-y:auto`,
		`.chat-workspace .command-menu>.mention-hint{order:999;flex:none;position:static`,
	} {
		if !strings.Contains(chatui.Stylesheet, rule) {
			t.Errorf("the stylesheet lacks %s", rule)
		}
	}
}
