package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	xhtml "golang.org/x/net/html"
)

// TestTodo_CHATCMD_001_Thread: the reply box of a thread reads its line
// through the command registry, as the conversation's composer does. /giphy
// was a second, hand-written comparison there, a word that is no command was
// posted as a reply, and typing "/" listed nothing.
func TestTodo_CHATCMD_001_Thread(t *testing.T) {
	m := chatux022Model()
	m.GiphyAPIKey = "key"
	m.ChatFeatures = &ChatFeatures{Locations: true}
	m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
	parent := Message{ID: "p1", AuthorID: "walt", Author: "Walt Brennan", Body: "Parent", Revision: 4, Replies: 1}
	m.ShowThread, m.ThreadParentID, m.ThreadParent = true, "p1", &parent
	m.Callbacks.ReplyInThread = func(string, string) {}

	// The reply box has no location control, so /location is the
	// conversation's composer's alone; every other command is in both.
	if names := composerCommandNames(composerCommandsFor("thread-composer").menu(m, "")); names != "poll,todo,giphy" {
		t.Fatalf("the reply box offers %s", names)
	}
	if names := composerCommandNames(composerCommandsFor("chat-composer").menu(m, "")); names != "poll,todo,giphy,location" {
		t.Fatalf("the composer offers %s", names)
	}

	// Dispatch as threadSend does.
	var log composerCommandLog
	rt := log.runtime(m)
	rt.Target = "thread-composer"
	var unknown, suggestion string
	rt.Unknown = func(name, suggest string) { unknown, suggestion = name, suggest }
	if text, consumed := composerSendCommand(rt, "/giphy cats"); !consumed || text != "" || log.giphy != 1 || log.query != "cats" || log.cleared != 1 {
		t.Fatalf("/giphy cats in a thread: consumed=%v text=%q log=%+v", consumed, text, log)
	}
	if _, consumed := composerSendCommand(rt, "/location"); !consumed || log.location != 0 || unknown != "location" {
		t.Fatalf("/location in a thread opened the conversation's location sheet: %+v, unknown %q", log, unknown)
	}
	if _, consumed := composerSendCommand(rt, "/gihpy cats"); !consumed || unknown != "gihpy" || suggestion != "giphy" {
		t.Fatalf("a mistyped command in a thread: consumed=%v unknown=%q suggestion=%q", consumed, unknown, suggestion)
	}
	if text, consumed := composerSendCommand(rt, "//giphy cats"); consumed || text != "/giphy cats" {
		t.Fatalf("a line with two slashes: consumed=%v text=%q", consumed, text)
	}
	if text, consumed := composerSendCommand(rt, "an ordinary reply"); consumed || text != "an ordinary reply" {
		t.Fatalf("an ordinary reply: consumed=%v text=%q", consumed, text)
	}

	// The pane draws the list for its own box, and the field points at it.
	open := renderNode(t, threadPane(m, handlers{local: localUI{commandMenu: composerCommandMenu{Target: "thread-composer", Open: true}}}))
	lists := chatPolishNodes(t, open, func(n *xhtml.Node) bool { return chatPolishAttr(n, "id") == "thread-composer-commands" })
	if len(lists) != 1 || !strings.HasPrefix(chatPolishAttr(lists[0], "data-open"), "true:") {
		t.Fatalf("the thread pane has no open command list")
	}
	rows := chatPolishNodesIn(lists[0], func(n *xhtml.Node) bool { return chatPolishAttr(n, "role") == "option" })
	if len(rows) != 3 || chatPolishAttr(rows[0], "id") != "thread-composer-command-1" || chatPolishAttr(rows[0], "data-action") != "command-pick" {
		t.Fatalf("the thread's list has %d rows", len(rows))
	}
	field := chatPolishNodes(t, open, func(n *xhtml.Node) bool { return n.Data == "textarea" && chatPolishAttr(n, "id") == "thread-composer" })
	if len(field) != 1 || chatPolishAttr(field[0], "aria-controls") != "thread-composer-commands" || chatPolishAttr(field[0], "aria-activedescendant") != "thread-composer-command-1" {
		t.Fatalf("the reply box does not point at its list: %v", field[0].Attr)
	}
	// The list of one box is not the other's.
	other := renderNode(t, threadPane(m, handlers{local: localUI{commandMenu: composerCommandMenu{Target: "chat-composer", Open: true}}}))
	if strings.Contains(other, `data-open="true:`) {
		t.Fatal("the conversation's open list opened the thread's")
	}
	// An unknown word is answered under the reply box, with the nearest command.
	line := renderNode(t, threadPane(m, handlers{local: localUI{commandMenu: composerCommandMenu{Target: "thread-composer", Unknown: "gihpy", Suggest: "giphy"}}}))
	for _, want := range []string{"/gihpy is not a command here. Did you mean /giphy?", "Use /giphy", "Send as text", `data-id="thread-composer"`} {
		if !strings.Contains(line, want) {
			t.Errorf("the thread's unknown-command line lacks %q", want)
		}
	}
	// The state a keystroke sets is the list the page shows for that box, and
	// for no other.
	local := localStore{box: &localUI{}}
	narrowed := composerCommandMenu{Target: "thread-composer", Query: "gi", Open: true}
	commandMenuSet(m, local, "thread-composer", narrowed)
	if got := local.get().commandMenu; got != narrowed {
		t.Fatalf("the list's state after a keystroke: %+v", got)
	}
	if shown := composerCommandShown(m, narrowed, "thread-composer"); len(shown) != 1 || shown[0] != "giphy" {
		t.Fatalf("after /gi the thread's list shows %v", shown)
	}
	if shown := composerCommandShown(m, narrowed, "chat-composer"); shown != nil {
		t.Fatalf("the conversation's list shows %v for the thread's state", shown)
	}
	commandMenuSet(m, local, "thread-composer", composerCommandMenu{})
	if local.get().commandMenu.Open {
		t.Fatal("the list stayed open")
	}
	// A poll typed in a thread is a reply to its parent.
	if !chatcmd003Follow(m, local, "thread-composer", "/poll Where? Here, There") || local.get().chatcmd003.Parent != "p1" {
		t.Fatalf("a /poll line in the reply box: %+v", local.get().chatcmd003)
	}
	preview := renderNode(t, threadPane(m, handlers{local: local.get()}))
	if !strings.Contains(preview, "Poll preview") || !strings.Contains(preview, "Post poll") {
		t.Fatal("the thread pane does not draw the preview of its own box")
	}
}
