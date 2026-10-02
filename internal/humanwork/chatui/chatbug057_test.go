package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func chatbug057Poll(change func(*chat.Chatcmd002Poll)) chat.Chatcmd002Card {
	poll := &chat.Chatcmd002Poll{Results: "always", AddOptions: "author", Options: []chat.ChannelPollOption{{ID: "tacos", Text: "Tacos"}, {ID: "pho", Text: "Pho"}}}
	if change != nil {
		change(poll)
	}
	return chat.Chatcmd002Card{Kind: "poll", Title: "Lunch spot?", Poll: poll}
}

// TestTodo_CHATBUG_057: Poll in the add menu used to open the channel's old
// floating panel, the poll it made was not in the conversation, and pressing
// Vote did nothing. The add menu now opens the same preview /poll does, the
// poll is a card in the conversation, and a press on it reaches the service.
func TestTodo_CHATBUG_057(t *testing.T) {
	m := chatcmd003UIFixture()
	var posted []chat.Chatcmd002Card
	m.Chatcmd002.Post = func(_, _ string, card chat.Chatcmd002Card, done func(error)) {
		posted = append(posted, card)
		done(nil)
	}
	opened := 0
	m.Callbacks.OpenChannelPoll = func(string) { opened++ }
	local := localStore{box: &localUI{tray: "todo"}}

	// The add menu's Poll makes the composer the draft of a poll: it opens the
	// preview above the composer, not the channel panel, and closes a channel
	// card that was open.
	if !chatcmd003Action(m, local, "composer-add", "chat-composer", "poll") {
		t.Fatal("the add menu's Poll was not handled by the preview")
	}
	state := local.get()
	if !state.chatcmd003.Open || state.chatcmd003.Draft.Card.Kind != "poll" || state.chatcmd003.Draft.Raw != "" || state.tray != "" || opened != 0 {
		t.Fatalf("add menu: preview open=%v of a %q, tray=%q, channel panel opened %d times", state.chatcmd003.Open, state.chatcmd003.Draft.Card.Kind, state.tray, opened)
	}
	empty := renderNode(t, chatcmd003PreviewView(m, state.chatcmd003))
	for _, want := range []string{"Poll preview", "Write the question, then the options", `data-action="chatcmd003-cancel"`, `id="chatcmd003-post"`} {
		if !strings.Contains(empty, want) {
			t.Errorf("empty preview lacks %q", want)
		}
	}
	if strings.Contains(empty, "<textarea") || strings.Contains(empty, "Write your draft") {
		t.Errorf("the preview has a second box to write in: %s", empty)
	}

	// Typed loosely into the composer after the command word the menu wrote,
	// the poll is tidied without a model and with no press: the preview shows
	// the card, every change, and the way back to what was typed.
	typed := "/poll where for lunch? tacos, pho or pizza"
	if !chatcmd003Follow(m, local, "chat-composer", typed) || len(posted) != 0 {
		t.Fatal("typing did not draw the preview, or posted")
	}
	preview := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
	for _, want := range []string{"Where for lunch?", "Tacos", "Pho", "Pizza", "Tidied: 4 changes", "<del>tacos</del>", "<ins>Tacos</ins>", `data-action="chatcmd003-original"`, "Use what I typed", `id="chatcmd003-post"`, `id="chatcmd003-anonymous"`, "Keep votes anonymous"} {
		if !strings.Contains(preview, want) {
			t.Errorf("preview lacks %q", want)
		}
	}
	if strings.Contains(preview, "Tidy with AI") {
		t.Error("the model pass is offered though no client bound one")
	}
	chatcmd003Action(m, local, "chatcmd003-original", "", "typed")
	if card := local.get().chatcmd003.Draft.Card; card.Title != "where for lunch?" || card.Poll.Options[0].Text != "tacos" || len(local.get().chatcmd003.Draft.Changes) != 0 {
		t.Fatalf("Use what I typed left %+v", card)
	}
	if again := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003)); !strings.Contains(again, "Tidy the wording") || !strings.Contains(again, `data-extra="tidied"`) {
		t.Fatalf("no way back to the tidied wording: %s", again)
	}
	// Enter in the composer is Post, the default action, and what the composer
	// holds is never sent as a message while the preview is open.
	if !chatcmd003Send(m, local, "chat-composer", typed) || len(posted) != 1 || local.get().chatcmd003.Open {
		t.Fatalf("Enter posted %d cards, preview open %v", len(posted), local.get().chatcmd003.Open)
	}
	if posted[0].Title != "where for lunch?" || len(posted[0].Poll.Options) != 3 {
		t.Fatalf("posted card %+v", posted[0])
	}

	// The card in the conversation: one button per option, counts from the
	// server's view, and a press that sends the vote.
	card := chatbug057Poll(nil)
	body, err := card.Body()
	if err != nil {
		t.Fatal(err)
	}
	m.Messages = []Message{{ID: "post", Revision: 3, Body: body}}
	unread := renderNode(t, chatcmd002ProjectedBody(m, m.Messages[0], nil))
	if !strings.Contains(unread, "Lunch spot?") || strings.Count(unread, "disabled") < 2 {
		t.Fatalf("a card whose view is not read yet must show and take no press: %s", unread)
	}
	view := chat.Chatcmd002View{Card: card, ResultsVisible: true, Revision: 3, Voters: map[string][]chat.Chatcmd002Voter{"tacos": {{HomeTenantID: "t", SubjectID: "dana"}}}}
	view.Card.Poll.Options[0].Count = 1
	m.Chatcmd002Views = map[string]Chatcmd002View{"post": view}
	var mutations []chat.Chatcmd002Mutation
	m.Chatcmd002.Mutate = func(post string, revision uint64, mutation chat.Chatcmd002Mutation) {
		if post != "post" || revision != 3 {
			t.Errorf("mutation for %q at revision %d", post, revision)
		}
		mutations = append(mutations, mutation)
	}
	shown := renderNode(t, chatcmd002ProjectedBody(m, m.Messages[0], nil))
	for _, want := range []string{`data-action="chatcmd002-vote"`, `data-extra="tacos"`, `aria-label="Vote: Tacos"`, `aria-pressed="false"`, "1 vote", "1 · 100%", "0 · 0%", "Dana"} {
		if !strings.Contains(shown, want) {
			t.Errorf("card lacks %q: %s", want, shown)
		}
	}
	if strings.Contains(shown, "disabled") {
		t.Errorf("a readable open poll has a disabled control: %s", shown)
	}
	if !chatcmd003Action(m, local, "chatcmd002-vote", "post", "pho") || len(mutations) != 1 || mutations[0].Operation != "VOTE" || strings.Join(mutations[0].Options, ",") != "pho" {
		t.Fatalf("vote press sent %+v", mutations)
	}
	// Pressing one's own choice withdraws it; pressing another moves it.
	view.MyOptions = []string{"pho"}
	m.Chatcmd002Views["post"] = view
	mine := renderNode(t, chatcmd002ProjectedBody(m, m.Messages[0], nil))
	if !strings.Contains(mine, `aria-pressed="true"`) || !strings.Contains(mine, "Your choice: Pho. Press to withdraw your vote") {
		t.Fatalf("the reader's choice is not marked: %s", mine)
	}
	chatcmd003Action(m, local, "chatcmd002-vote", "post", "pho")
	chatcmd003Action(m, local, "chatcmd002-vote", "post", "tacos")
	if len(mutations) != 3 || len(mutations[1].Options) != 0 || strings.Join(mutations[2].Options, ",") != "tacos" {
		t.Fatalf("withdraw and move sent %+v", mutations[1:])
	}
	// A press the service refused is said on the card.
	view.Notice = "failed"
	if notice := renderNode(t, Chatcmd002RenderCard(m, "post", view, false)); !strings.Contains(notice, "Could not save that. Try again.") || !strings.Contains(notice, `role="alert"`) {
		t.Fatalf("refused press not shown: %s", notice)
	}
}

// An anonymous ballot is final, so it is chosen first and cast with its own
// button; after that the card says it is recorded and takes no more.
func TestTodo_CHATBUG_057_Anonymous(t *testing.T) {
	m := chatcmd003UIFixture()
	card := chatbug057Poll(func(p *chat.Chatcmd002Poll) { p.Anonymous = true })
	view := chat.Chatcmd002View{Card: card, ResultsVisible: true, Revision: 1}
	m.Chatcmd002Views = map[string]Chatcmd002View{"post": view}
	sent := 0
	m.Chatcmd002.Mutate = func(string, uint64, chat.Chatcmd002Mutation) { sent++ }
	markup := renderNode(t, Chatcmd002RenderCard(m, "post", view, false))
	for _, want := range []string{`type="radio"`, `data-chatcmd002-choice="post"`, `value="tacos"`, `data-action="chatcmd002-cast"`, "Cast vote", "An anonymous vote cannot be changed once it is cast.", "Anonymous"} {
		if !strings.Contains(markup, want) {
			t.Errorf("anonymous card lacks %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, `data-action="chatcmd002-vote"`) {
		t.Error("an anonymous poll votes on a single press")
	}
	// Selecting is not voting, and nothing chosen is nothing cast.
	local := localStore{box: &localUI{}}
	chatcmd003Action(m, local, "chatcmd002-vote", "post", "tacos")
	chatcmd003Action(m, local, "chatcmd002-cast", "post", "")
	if sent != 0 {
		t.Fatalf("an empty or uncast ballot was sent %d times", sent)
	}
	if ballot, ok := chatcmd002Ballot(*card.Poll, []string{"pho"}); !ok || strings.Join(ballot, ",") != "pho" {
		t.Fatalf("ballot %v %v", ballot, ok)
	}
	if _, ok := chatcmd002Ballot(*card.Poll, []string{"tacos", "pho"}); ok {
		t.Fatal("two choices cast in a one-choice poll")
	}
	if _, ok := chatcmd002Ballot(*card.Poll, []string{"forged"}); ok {
		t.Fatal("an option the poll does not have was cast")
	}
	several := *card.Poll
	several.Multiple = true
	if ballot, ok := chatcmd002Ballot(several, []string{"tacos", "pho"}); !ok || len(ballot) != 2 {
		t.Fatalf("several choices %v %v", ballot, ok)
	}
	view.Voted = true
	done := renderNode(t, Chatcmd002RenderCard(m, "post", view, false))
	if !strings.Contains(done, "Your vote is recorded. Nobody can see what you chose.") || strings.Contains(done, `data-action="chatcmd002-cast"`) || strings.Count(done, "disabled") != 2 {
		t.Fatalf("a cast anonymous ballot can be cast again: %s", done)
	}
}

// Results that are held back, a closed poll, and the author's Close button.
func TestTodo_CHATBUG_057_States(t *testing.T) {
	m := chatcmd003UIFixture()
	m.Chatcmd002.Mutate = func(string, uint64, chat.Chatcmd002Mutation) {}
	held := chat.Chatcmd002View{Card: chatbug057Poll(func(p *chat.Chatcmd002Poll) { p.Results = "after-voting" }), Revision: 1}
	markup := renderNode(t, Chatcmd002RenderCard(m, "post", held, false))
	if strings.Contains(markup, "<progress") || strings.Contains(markup, "votes") || !strings.Contains(markup, "Vote to see results") {
		t.Fatalf("held-back result shown: %s", markup)
	}
	closedAt := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	card := chatbug057Poll(nil)
	card.ClosedAt = &closedAt
	m.Chatcmd002TimeZone = "UTC"
	closed := renderNode(t, Chatcmd002RenderCard(m, "post", chat.Chatcmd002View{Card: card, ResultsVisible: true, CanManage: true, Revision: 2}, false))
	for _, want := range []string{"Closed 2026-10-01 09:30", `data-extra="REOPEN"`, "Reopen poll", "disabled"} {
		if !strings.Contains(closed, want) {
			t.Errorf("closed poll lacks %q: %s", want, closed)
		}
	}
	open := renderNode(t, Chatcmd002RenderCard(m, "post", chat.Chatcmd002View{Card: chatbug057Poll(nil), ResultsVisible: true, CanManage: true, Revision: 2}, false))
	if !strings.Contains(open, `data-extra="CLOSE"`) || !strings.Contains(open, "Close poll") {
		t.Fatalf("the author cannot close: %s", open)
	}
	other := renderNode(t, Chatcmd002RenderCard(m, "post", chat.Chatcmd002View{Card: chatbug057Poll(nil), ResultsVisible: true, Revision: 2}, false))
	if strings.Contains(other, "chatcmd002-close") {
		t.Fatal("a member who did not post the poll is offered Close")
	}
}

// The standing channel poll keeps its card for a poll that exists. With none,
// the card no longer holds the old create form: it says where polls are posted
// and starts the same preview.
func TestTodo_CHATBUG_057_Standing(t *testing.T) {
	m := chatcmd003UIFixture()
	m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
	empty := renderNode(t, channelPollSection(m, handlers{}))
	if strings.Contains(empty, "channel-poll-form") || !strings.Contains(empty, "A poll is posted in the conversation") || !strings.Contains(empty, `data-action="composer-add"`) || !strings.Contains(empty, `data-extra="poll"`) {
		t.Fatalf("empty channel poll card: %s", empty)
	}
	m.Chatcmd002.Post = nil
	if legacy := renderNode(t, channelPollSection(m, handlers{})); !strings.Contains(legacy, "channel-poll-form") {
		t.Fatal("without the card service the channel poll lost its form")
	}
}
