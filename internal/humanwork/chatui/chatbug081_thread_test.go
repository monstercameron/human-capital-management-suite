package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATBUG_081_Thread: Delete on a reply in the thread pane deletes
// it. The action used to look for the message in the timeline alone, where a
// reply is not, so the confirmed press did nothing.
func TestTodo_CHATBUG_081_Thread(t *testing.T) {
	type call struct {
		id       string
		revision uint64
	}
	var deleted []call
	parent := Message{ID: "p1", AuthorID: "walt", Author: "Walt Brennan", Body: "Parent", Revision: 4, Replies: 1}
	m := chatux022Model()
	m.Messages = []Message{{ID: "m1", AuthorID: "walt", Body: "In the timeline", Revision: 2}}
	m.ShowThread, m.ThreadParentID, m.ThreadParent = true, "p1", &parent
	m.ThreadMessages = []Message{{ID: "r1", AuthorID: "walt", Author: "Walt Brennan", Body: "A reply", Revision: 3}}
	m.Callbacks.DeleteMessage = func(id string, revision uint64) { deleted = append(deleted, call{id, revision}) }
	m.Callbacks.OpenMenu = func(string) {}

	for _, tc := range []struct {
		name, id string
		want     []call
	}{
		{"a reply of the open thread", "r1", []call{{"r1", 3}}},
		{"the thread's parent, outside the timeline's window", "p1", []call{{"p1", 4}}},
		{"a message of the timeline", "m1", []call{{"m1", 2}}},
		{"a message that is not on the page", "gone", nil},
	} {
		deleted = nil
		m.act("delete", tc.id)
		if len(deleted) != len(tc.want) || (len(deleted) == 1 && deleted[0] != tc.want[0]) {
			t.Errorf("%s: deleted %+v, want %+v", tc.name, deleted, tc.want)
		}
	}

	// The whole path in the pane: the reply's menu asks, and the question's
	// Delete message names the reply.
	m.MenuID = "thread:r1"
	local := localStore{box: &localUI{}}
	if !chatbug081Press(m, local, "delete") {
		t.Fatal("Delete message in a reply's menu did not ask first")
	}
	m.deleteAsk = chatbug081Settle(local, m.MenuID)
	pane := renderNode(t, threadPane(m, handlers{}))
	if !strings.Contains(pane, "Delete this message? This cannot be undone.") || !strings.Contains(pane, `data-action="delete" data-delete-ask="confirm" data-id="r1"`) {
		t.Fatalf("the reply's menu does not ask about the reply: %s", pane)
	}
	if !strings.Contains(pane, `data-action="menu" data-delete-ask="cancel" data-id="thread:r1"`) {
		t.Fatalf("Cancel does not close the reply's own menu: %s", pane)
	}
	deleted = nil
	if chatbug081Press(m, local, "delete") {
		t.Fatal("the press on the question's Delete message was swallowed")
	}
	m.act("delete", "r1")
	if len(deleted) != 1 || deleted[0] != (call{"r1", 3}) {
		t.Fatalf("the confirmed delete of a reply called %+v, want r1 at revision 3", deleted)
	}
}
