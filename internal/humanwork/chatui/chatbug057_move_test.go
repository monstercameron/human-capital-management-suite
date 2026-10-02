package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_CHATBUG_057_Move is page finding 7 of 2026-10-02: a channel kept its
// old standing poll beside the polls posted as messages. A poll has one model,
// the card. A standing poll that still exists is moved into the conversation
// by a person who may end it: it becomes the draft of a card, that person
// posts it, and the post closes the standing poll.
func TestTodo_CHATBUG_057_Move(t *testing.T) {
	m := chatcmd003UIFixture()
	m.ChannelTeam.CanPin = true
	m.ChannelPoll = ChannelPoll{Revision: 4, Question: `Lunch "spot" = where?`, TotalVotes: 3, Options: []ChannelPollOption{{ID: "a", Text: "Tacos, the cart", Count: 2}, {ID: "b", Text: "Pho or ramen", Count: 1}}}
	var posted []chat.Chatcmd002Card
	var answer func(error)
	m.Chatcmd002.Post = func(_, _ string, card chat.Chatcmd002Card, done func(error)) {
		posted = append(posted, card)
		answer = done
	}
	closed := 0
	m.Chatcmd002.CloseChannelPoll = func() { closed++ }
	local := localStore{box: &localUI{tray: "poll"}}

	// The line that makes the same card: quotes, commas, "or" and "=" inside
	// the standing poll's words are words, not separators or settings.
	line := chatbug057MoveLine(m.ChannelPoll)
	if line != `/poll "Lunch ""spot"" = where?" 1="Tacos, the cart" 2="Pho or ramen"` {
		t.Fatalf("the standing poll as a line: %s", line)
	}

	// A poll preview in this channel offers the move to a person who may end
	// the standing poll.
	chatcmd003Follow(m, local, "chat-composer", "/poll ")
	offer := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
	for _, want := range []string{"This channel still has a standing poll from before polls were messages", "Lunch &#34;spot&#34; = where?", `data-action="chatbug057-move"`, "Move it here"} {
		if !strings.Contains(offer, want) {
			t.Errorf("the offer lacks %q: %s", want, offer)
		}
	}
	if !chatcmd003Action(m, local, "chatbug057-move", "chat-composer", "") {
		t.Fatal("the move was not taken")
	}
	state := local.get()
	card := state.chatcmd003.Draft.Card
	if !state.chatcmd003.Open || !state.chatcmd003.Standing || state.tray != "" || card.Title != `Lunch "spot" = where?` || len(card.Poll.Options) != 2 || card.Poll.Options[0].Text != "Tacos, the cart" || card.Poll.Options[1].Text != "Pho or ramen" {
		t.Fatalf("the standing poll as a draft: %+v, tray %q", state.chatcmd003, state.tray)
	}
	preview := renderNode(t, chatcmd003PreviewView(m, state.chatcmd003))
	if !strings.Contains(preview, "Posting this closes the channel’s standing poll. Its votes (3) are not carried over") || strings.Contains(preview, "Move it here") {
		t.Fatalf("the preview does not say what posting does: %s", preview)
	}

	// Nothing is closed until the card is in the conversation.
	if !chatcmd003Send(m, local, "chat-composer", line) || len(posted) != 1 || closed != 0 {
		t.Fatalf("Enter: %d posts, the standing poll closed %d times before the answer", len(posted), closed)
	}
	answer(chatcmd003Refusal("unavailable"))
	if closed != 0 || !local.get().chatcmd003.Standing {
		t.Fatal("a refused post closed the standing poll, or forgot what the draft is")
	}
	chatcmd003Send(m, local, "chat-composer", line)
	answer(nil)
	if len(posted) != 2 || closed != 1 || local.get().chatcmd003.Open {
		t.Fatalf("after the post: %d posts, closed %d times, preview open %v", len(posted), closed, local.get().chatcmd003.Open)
	}
	if posted[1].Title != `Lunch "spot" = where?` || posted[1].Poll.Options[0].Count != 0 {
		t.Fatalf("posted card %+v", posted[1])
	}

	// A poll of one's own typed in the same channel closes nothing.
	chatcmd003Follow(m, local, "chat-composer", "/poll Where? Here, There")
	chatcmd003Send(m, local, "chat-composer", "/poll Where? Here, There")
	answer(nil)
	if len(posted) != 3 || closed != 1 {
		t.Fatalf("an ordinary poll closed the standing poll: closed %d", closed)
	}

	// The move is offered only to a person who may end the standing poll, and
	// only while there is one.
	for name, change := range map[string]func(*Model){
		"a member who may not end it": func(m *Model) { m.ChannelTeam.CanPin = false },
		"no standing poll":            func(m *Model) { m.ChannelPoll = ChannelPoll{Revision: 5} },
		"the poll is being changed":   func(m *Model) { m.ChannelPollPending = true },
		"the poll could not be read":  func(m *Model) { m.ChannelPollError = "load" },
	} {
		other := m
		change(&other)
		local := localStore{box: &localUI{}}
		chatcmd003Follow(other, local, "chat-composer", "/poll ")
		if markup := renderNode(t, chatcmd003PreviewView(other, local.get().chatcmd003)); strings.Contains(markup, "chatbug057-move") {
			t.Errorf("%s: the move is offered", name)
		}
		chatcmd003Action(other, local, "chatbug057-move", "chat-composer", "")
		if local.get().chatcmd003.Standing || local.get().chatcmd003.Draft.Raw != "" {
			t.Errorf("%s: the move was carried out", name)
		}
	}
	// A list preview never offers it: the channel's to-do list stays its list.
	chatcmd003Follow(m, local, "chat-composer", "/todo Ship it")
	if markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003)); strings.Contains(markup, "chatbug057-move") {
		t.Fatal("a list preview offers to move the poll")
	}
}
