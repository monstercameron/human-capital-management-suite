package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func chatcmd002CardMessage(t *testing.T) Message {
	t.Helper()
	draft, err := chat.Chatcmd003ParsePoll(`"Where for lunch?" 1="Tacos" 2="Pho"`, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := draft.Card.Body()
	if err != nil {
		t.Fatal(err)
	}
	return Message{ID: "card", AuthorID: "me", Author: "Author", Body: body, Revision: 3}
}

// TestTodo_CHATCMD_002_CardBody: a card message shows its words, never its
// data, in the Saved panel, and its menu offers no Edit.
func TestTodo_CHATCMD_002_CardBody(t *testing.T) {
	card := chatcmd002CardMessage(t)
	m := chatcmd003UIFixture()
	m.Messages = []Message{card, {ID: "plain", AuthorID: "me", Author: "Author", Body: "hello", Revision: 1}}
	m.Callbacks.BeginEdit = func(string) {}
	m.Callbacks.OpenMenu = func(string) {}

	// The Saved panel prints the question and the options, not the card data.
	body, _ := chatsave002Body(m, card)
	saved := renderNode(t, body)
	if !strings.Contains(saved, "Where for lunch?") || !strings.Contains(saved, "Tacos") || strings.Contains(saved, "hcm-message-card") || strings.Contains(saved, `"kind"`) {
		t.Fatalf("the Saved panel shows card data: %s", saved)
	}

	// Edit is offered for an ordinary message of one's own and not for a card.
	menu := renderNode(t, spanOf(chatux022MenuItems(m, m.Messages[1])))
	if !strings.Contains(menu, `data-action="edit"`) {
		t.Fatalf("an ordinary message lost Edit: %s", menu)
	}
	menu = renderNode(t, spanOf(chatux022MenuItems(m, card)))
	if strings.Contains(menu, `data-action="edit"`) {
		t.Fatalf("a card offers Edit: %s", menu)
	}
	if !strings.Contains(menu, `data-action="copy-contents"`) {
		t.Fatalf("a card lost Copy message contents: %s", menu)
	}
}

// TestTodo_CHATCMD_002_AddMenuInDirectMessages: the Add menu offers Poll and
// To-do list in direct messages and groups too, worded for the conversation, and
// opens the same preview /poll opens.
func TestTodo_CHATCMD_002_AddMenuInDirectMessages(t *testing.T) {
	for _, kind := range []ConversationKind{PublicChannel, DirectMessage, GroupChat} {
		m := chatcmd003UIFixture()
		m.Conversations[0].Kind = kind
		m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
		items := composerAddItems(m)
		names := map[string]string{}
		for _, item := range items {
			names[item.kind] = composerText(m, item.noteKey)
		}
		if names["poll"] == "" || names["todo"] == "" {
			t.Fatalf("kind %v: the Add menu lacks Poll or To-do list: %v", kind, names)
		}
		if kind != PublicChannel && (strings.Contains(names["poll"], "channel") || strings.Contains(names["todo"], "channel")) {
			t.Errorf("kind %v: the notes talk about a channel: %v", kind, names)
		}
	}
	// An agent's conversation takes no commands, so it offers neither.
	agent := chatcmd003UIFixture()
	agent.Conversations[0].Kind = DirectMessage
	agent.Conversations[0].Agent = true
	agent.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
	for _, item := range composerAddItems(agent) {
		if item.kind == "poll" || item.kind == "todo" {
			t.Errorf("an agent conversation offers %s", item.kind)
		}
	}
}

// TestTodo_CHATUX_021_SystemLineInSaved: an "added people" line that is in the
// Saved panel is read as its sentence, never as its marker and identifier.
func TestTodo_CHATUX_021_SystemLineInSaved(t *testing.T) {
	m := chatcmd003UIFixture()
	line := Message{ID: "line", AuthorID: "me", Author: "Author", Body: chat.MembershipAddedBody("dana"), Revision: 1}
	body, _ := chatsave002Body(m, line)
	got := renderNode(t, body)
	if !strings.Contains(got, "Author added Dana") || strings.Contains(got, "member-added") || strings.Contains(got, "dana") {
		t.Fatalf("the Saved panel prints the system line's data: %s", got)
	}
}
