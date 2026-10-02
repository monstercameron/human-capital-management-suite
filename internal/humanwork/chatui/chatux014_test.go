package chatui

import (
	"regexp"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func chatux014Model(kind ConversationKind, name string) Model {
	root := Message{ID: "root", Author: "Ben", AuthorID: "ben", Body: "plan"}
	return Model{State: StateReady, Locale: "en-US", CurrentUser: "walt", CurrentTenantID: "t", SelectedID: "room", ShowThread: true, ThreadParentID: "root", ThreadParent: &root,
		Conversations: []Conversation{{ID: "room", Name: name, Kind: kind, Joined: true}}, Messages: []Message{root},
		Callbacks: Callbacks{SendMessage: func(string, string) {}, ReplyInThread: func(string, string) {}}}
}

// chatux014Tools lists the controls of a composer's tool row in the order they
// are drawn, by what they do.
func chatux014Tools(t *testing.T, markup, form string) []string {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	forms := chatPolishNodesIn(root, func(n *xhtml.Node) bool {
		return n.Data == "form" && strings.Contains(chatPolishAttr(n, "class"), form)
	})
	if len(forms) != 1 {
		t.Fatalf("%d %s forms in %s", len(forms), form, markup)
	}
	var kinds []string
	for _, tools := range chatPolishNodesIn(forms[0], func(n *xhtml.Node) bool { return strings.Contains(chatPolishAttr(n, "class"), "composer-tools") }) {
		for _, control := range chatPolishNodesIn(tools, func(n *xhtml.Node) bool {
			return n.Data == "button" && strings.Contains(chatPolishAttr(n, "class"), "tool-button")
		}) {
			switch class := chatPolishAttr(control, "class"); {
			case control.Parent != nil && (strings.Contains(chatPolishAttr(control.Parent, "class"), "chatvoice-tool") || strings.Contains(chatPolishAttr(control.Parent, "class"), "chatmap-control")):
				kinds = append(kinds, "opener")
			case strings.Contains(class, "composer-add-trigger"):
				kinds = append(kinds, "add")
			case strings.Contains(class, "composer-mention-button"):
				kinds = append(kinds, "mention")
			case strings.Contains(class, "composer-format-toggle"):
				kinds = append(kinds, "format")
			case strings.Contains(chatPolishAttr(control, "data-action"), "emoji"), strings.Contains(class, "emoji"):
				kinds = append(kinds, "emoji")
			default:
				kinds = append(kinds, class)
			}
		}
	}
	return kinds
}

// TestTodo_CHATUX_014 compares the thread composer with the conversation
// composer: the same tools in the same order, the formatting buttons the
// conversation has (code and lists included), the same hint, and the one thing
// a reply adds, "Also send to #channel".
func TestTodo_CHATUX_014(t *testing.T) {
	m := chatux014Model(PublicChannel, "general")
	thread := renderNode(t, threadPane(m, handlers{}))
	main := renderNode(t, composer(m, handlers{}))

	threadTools := chatux014Tools(t, thread, "thread-composer")
	mainTools := chatux014Tools(t, main, "chat-composer")
	// The conversation's Add menu holds what belongs to a conversation, not to
	// a reply; every other tool is in the thread, in the conversation's order.
	var want []string
	for _, kind := range mainTools {
		if kind != "add" && kind != "opener" {
			want = append(want, kind)
		}
	}
	if strings.Join(threadTools, ",") != strings.Join(want, ",") || len(threadTools) < 3 || threadTools[0] != "mention" {
		t.Fatalf("the thread composer's tools %v are not the conversation's %v", threadTools, want)
	}
	for _, format := range []string{"bold", "italic", "code", "link", "bullets", "quote"} {
		if !strings.Contains(thread, `data-extra="`+format+`"`) || !strings.Contains(thread, `data-id="thread-composer"`) {
			t.Errorf("the thread composer has no %s button", format)
		}
	}
	if !strings.Contains(thread, `data-action="composer-format-toggle"`) || !strings.Contains(thread, `data-format-row="hidden"`) || !strings.Contains(thread, `id="thread-composer-format-row"`) {
		t.Error("the thread composer has no Formatting toggle and row")
	}
	if !strings.Contains(thread, `data-action="composer-mention"`) {
		t.Error("the thread composer has no mention button")
	}
	if !strings.Contains(thread, `id="thread-also-channel"`) || !strings.Contains(thread, "Also send to #general") {
		t.Errorf("a reply cannot also be sent to the channel: %s", thread)
	}
	if !strings.Contains(thread, m.t(KeyComposeHint)) || !strings.Contains(main, m.t(KeyComposeHint)) {
		t.Error("the Enter hint differs")
	}
	// A direct message names the person; a conversation that cannot send offers nothing.
	dm := chatux014Model(DirectMessage, "Loretta Haynes")
	if markup := renderNode(t, threadPane(dm, handlers{})); !strings.Contains(markup, "Also send to Loretta Haynes") {
		t.Errorf("a direct message thread reads %s", regexp.MustCompile(`thread-also[^>]*>`).FindString(markup))
	}
	m.Callbacks.SendMessage = nil
	if markup := renderNode(t, threadPane(m, handlers{})); strings.Contains(markup, "thread-also-channel") {
		t.Error("the choice is offered where the conversation cannot be written to")
	}
	for _, locale := range []string{"de-DE", "ar"} {
		if text := chatux014Text(locale, "also"); text == "" || text == chatux014Text("en-US", "also") || !strings.Contains(text, "{name}") {
			t.Errorf("%s has no copy for the choice: %q", locale, text)
		}
	}
}

// TestTodo_CHATUX_014_Browser holds the styles that lay the thread composer
// out: the formatting row follows the same switch, the buttons the old rule
// hid are drawn, and a folded composer takes the new rows away with the tools.
func TestTodo_CHATUX_014_Browser(t *testing.T) {
	for _, want := range []string{
		`.chat-workspace .thread-composer[data-format-row="shown"] .composer-format-row{display:flex}`,
		`@media(min-width:800px){.chat-workspace .thread-composer[data-format-row="auto"] .composer-format-row{display:flex}}`,
		`.chat-workspace .thread-composer .format-button[data-extra=code],.chat-workspace .thread-composer .format-button[data-extra=bullets],.chat-workspace .thread-composer .format-button[data-extra=quote]{display:inline-flex}`,
		`@container chat (max-width:1100px){.chat-workspace .thread-composer:not(:focus-within) :is(.composer-tools,.composer-help,.composer-format-row,.thread-also){display:none}}`,
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("the stylesheet lacks %q", want)
		}
	}
}
