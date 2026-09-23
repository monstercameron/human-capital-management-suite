//go:build !(js && wasm)

package chatui

import (
	"strings"
	"testing"
)

func TestChatCopyContentsMenusAndDispatch(t *testing.T) {
	called := ""
	root := Message{ID: "root", Body: "first\nsecond"}
	reply := Message{ID: "reply", Body: "reply text"}
	m := Model{State: StateReady, SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "Room"}}, Messages: []Message{root}, ShowThread: true, ThreadParentID: root.ID, ThreadParent: &root, ThreadMessages: []Message{reply}, MenuID: root.ID, Callbacks: Callbacks{CopyContents: func(id string) { called = id }, OpenMenu: func(string) {}}}
	markup := render(t, m)
	if !strings.Contains(markup, `data-action="copy-contents" data-id="root"`) || !strings.Contains(markup, "Copy message contents") {
		t.Fatal("timeline menu omitted copy contents")
	}
	m.act("copy-contents", "root")
	if called != "root" {
		t.Fatalf("copy target = %q", called)
	}
	for _, id := range []string{"root", "reply"} {
		m.MenuID = "thread:" + id
		markup = render(t, m)
		if !strings.Contains(markup, `data-action="copy-contents" data-id="`+id+`"`) || !strings.Contains(markup, `data-action="menu" data-id="thread:`+id+`"`) {
			t.Fatalf("thread %s menu omitted copy action", id)
		}
	}
	m.Messages[0].Body = ""
	m.MenuID = "root"
	markup = render(t, m)
	if !strings.Contains(markup, "No message text to copy") || !strings.Contains(markup, `disabled`) {
		t.Fatal("textless message did not explain disabled copy")
	}
}
