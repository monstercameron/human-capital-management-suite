package chatui

import (
	stdhtml "html"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// TestTodo_CHATBUG_076 holds the hover bar above the message's top edge and off
// the author line, cuts it down in a narrow column, and tints a grouped message
// that shows it.
func TestTodo_CHATBUG_076(t *testing.T) {
	const bar = ".chat-workspace .message-list .message .message-actions"
	if !strings.Contains(ChatBug076Styles, "@media(min-width:768px) and (pointer:fine){\n"+bar+"{top:4px;transform:translateY(-100%)}") {
		t.Fatalf("the bar is not placed with its bottom edge 4px inside the message's top padding, from 768px up: %s", ChatBug076Styles)
	}
	// Nothing joined later may move it back: the last rule for the selector, in
	// the whole stylesheet, is this one.
	if top, transform := chatbugCascadeValue(Stylesheet, bar, "top"), chatbugCascadeValue(Stylesheet, bar, "transform"); top != "4px" || transform != "translateY(-100%)" {
		t.Fatalf("a later sheet moves the bar: top=%q transform=%q", top, transform)
	}
	// The message's top padding is at least the 4px the bar reaches into it, so
	// the bar ends above the author line.
	if padding := chatbugCascadeValue(Stylesheet, ".message", "padding-block-start"); padding != "8px" {
		t.Fatalf("premise changed: a message's top padding is %q, the bar is placed for 8px", padding)
	}
	// The author line no longer keeps room for a bar that is not over it.
	if got := chatbugCascadeValue(Stylesheet, ".chat-workspace .message-list .message .message-meta", "padding-inline-end"); got != "0" {
		t.Fatalf("the author line still keeps %q free for the bar", got)
	}
	if got := chatbugCascadeValue(Stylesheet, ".chat-workspace .message-list .message.continued:has(>.message-actions)", "background"); !strings.Contains(got, "color-mix") {
		t.Fatalf("a grouped message is not tinted while its bar is shown: %q", got)
	}
	if !strings.Contains(ChatBug076Styles, `@container chatmain (max-width:560px){
.chat-workspace .message-list .message-actions :is(.quick-react,.chatsave-action,[data-action="pin"],[data-action="unpin"]){display:none}`) {
		t.Fatalf("under 560px of column width the bar is not cut down: %s", ChatBug076Styles)
	}
	if !strings.Contains(Stylesheet, ".chat-main{container:chatmain/inline-size}") {
		t.Fatal("premise changed: the conversation column is no longer the chatmain container the narrow rule measures")
	}
}

// TestTodo_CHATBUG_076_Browser renders a row with its bar and checks that the
// tree is the one the rules select: the bar is the message's own child, and in
// a narrow column exactly the reaction picker, Reply and More are left.
func TestTodo_CHATBUG_076_Browser(t *testing.T) {
	at := time.Date(2026, 10, 2, 12, 55, 0, 0, time.UTC)
	m := Model{
		State: StateReady, Locale: "en-US", SelectedID: "room", CurrentUser: "walt", CurrentTenantID: "tenant",
		Conversations: []Conversation{{ID: "room", Name: "general", Kind: PublicChannel, Joined: true}},
		Messages: []Message{
			{ID: "m1", AuthorID: "walt", Author: "Walt Brennan", Body: "First", SentAt: at, Pinned: true},
			{ID: "m2", AuthorID: "walt", Author: "Walt Brennan", Body: "Second", SentAt: at.Add(time.Minute)},
		},
	}
	m.Callbacks = Callbacks{OpenThread: func(string) {}, ReactWith: func(string, string) {}, OpenPicker: func(string) {}, Pin: func(string) {}, OpenMenu: func(string) {}}
	page, err := ui.RenderToString(message(m, handlers{local: localUI{pointerRow: "m2"}}, m.Messages[1], true))
	if err != nil {
		t.Fatal(err)
	}
	rows := chatPolishNodes(t, stdhtml.UnescapeString(page), func(n *xhtml.Node) bool { return n.Data == "article" })
	if len(rows) != 1 || !chatPolishHasClass(rows[0], "message") || !chatPolishHasClass(rows[0], "continued") {
		t.Fatalf("the row is not a grouped message: %s", page)
	}
	var toolbar *xhtml.Node
	for c := rows[0].FirstChild; c != nil; c = c.NextSibling {
		if chatPolishHasClass(c, "message-actions") {
			toolbar = c
		}
	}
	if toolbar == nil {
		t.Fatalf("the bar is not the message's own child, so \":has(>.message-actions)\" and the placement rule miss it: %s", page)
	}
	var kept, dropped []string
	for c := toolbar.FirstChild; c != nil; c = c.NextSibling {
		if c.Data != "button" {
			continue
		}
		action := chatPolishAttr(c, "data-action")
		if chatPolishHasClass(c, "quick-react") || chatPolishHasClass(c, "chatsave-action") || action == "pin" || action == "unpin" {
			dropped = append(dropped, action)
		} else {
			kept = append(kept, action)
		}
	}
	if got := strings.Join(kept, ","); got != "reply,react-pick,menu" {
		t.Fatalf("in a narrow column the bar keeps %q, want reply, the reaction picker and More", got)
	}
	if len(dropped) != 5 {
		t.Fatalf("the narrow rule hides %d buttons (%v), want the three quick reactions, Save and Pin", len(dropped), dropped)
	}
}
