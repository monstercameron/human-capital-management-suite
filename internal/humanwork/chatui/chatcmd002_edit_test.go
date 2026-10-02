package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func chatcmd002EditFixture(t *testing.T, addOptions string) (Model, chat.Chatcmd002View, *[]chat.Chatcmd002Mutation) {
	t.Helper()
	d, err := chat.Chatcmd003ParsePoll(`"Where?" 1="Here" 2="There" add=`+addOptions, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	d.Card.Poll.Options[0].ID, d.Card.Poll.Options[1].ID = "a", "b"
	var sent []chat.Chatcmd002Mutation
	m := chatcmd003UIFixture()
	m.Chatcmd002.Mutate = func(_ string, _ uint64, mutation chat.Chatcmd002Mutation) { sent = append(sent, mutation) }
	view := chat.Chatcmd002View{Card: d.Card, ResultsVisible: true, Revision: 3}
	m.Chatcmd002Views = map[string]Chatcmd002View{"post": view}
	return m, view, &sent
}

// TestTodo_CHATCMD_002_AddOptionCard: a member sees the add-option row exactly
// when the server's view says they may, and pressing Add sends only the text.
func TestTodo_CHATCMD_002_AddOptionCard(t *testing.T) {
	m, view, sent := chatcmd002EditFixture(t, "members")
	view.CanAddOption = true
	if markup := renderNode(t, Chatcmd002RenderCard(m, "post", view, false)); !strings.Contains(markup, `data-action="chatcmd002-add-option"`) || !strings.Contains(markup, `data-chatcmd002-enter="chatcmd002-add-option"`) || !strings.Contains(markup, `aria-label="Add an option"`) || !strings.Contains(markup, ">Add<") {
		t.Fatalf("add row: %s", markup)
	}
	view.CanAddOption = false
	if markup := renderNode(t, Chatcmd002RenderCard(m, "post", view, false)); strings.Contains(markup, "chatcmd002-add") {
		t.Fatalf("an add row for a reader the server did not allow: %s", markup)
	}
	if markup := renderNode(t, Chatcmd002RenderCard(m, "", chat.Chatcmd002View{Card: view.Card, CanAddOption: true}, true)); strings.Contains(markup, "chatcmd002-add") {
		t.Fatalf("a preview takes no options: %s", markup)
	}
	view.CanAddOption = true
	if chatcmd002AddOption(m, "post", view, 3, "   ") || len(*sent) != 0 {
		t.Fatal("an empty field sent something")
	}
	if !chatcmd002AddOption(m, "post", view, 3, "  Elsewhere ") || len(*sent) != 1 || (*sent)[0].Operation != "ADD_OPTION" || (*sent)[0].Text != "Elsewhere" {
		t.Fatalf("sent %+v", *sent)
	}
	view.CanAddOption = false
	if chatcmd002AddOption(m, "post", view, 3, "Again") || len(*sent) != 1 {
		t.Fatal("an option was sent for a reader who may not add")
	}
}

// TestTodo_CHATCMD_002_EditCard: the author is offered "Edit poll" until the
// first vote or tick, the form holds every word, and Save sends the card with
// the words put in.
func TestTodo_CHATCMD_002_EditCard(t *testing.T) {
	m, view, sent := chatcmd002EditFixture(t, "author")
	view.CanManage = true
	card := renderNode(t, Chatcmd002RenderCard(m, "post", view, false))
	if !strings.Contains(card, `data-action="chatcmd002-edit"`) || !strings.Contains(card, "Edit poll") || !strings.Contains(card, "Close poll") || strings.Index(card, "Edit poll") > strings.Index(card, "Close poll") {
		t.Fatalf("author's controls: %s", card)
	}
	voted := view
	votedCard := view.Card
	votedCard.Interacted = true
	voted.Card = votedCard
	if got := renderNode(t, Chatcmd002RenderCard(m, "post", voted, false)); strings.Contains(got, "chatcmd002-edit") || !strings.Contains(got, "Close poll") {
		t.Fatalf("a voted card is offered an edit: %s", got)
	}
	reader := view
	reader.CanManage = false
	if got := renderNode(t, Chatcmd002RenderCard(m, "post", reader, false)); strings.Contains(got, "chatcmd002-edit") {
		t.Fatalf("a reader is offered an edit: %s", got)
	}
	// The form replaces the body while the card is being reworded.
	m.cardEditing = "post"
	form := renderNode(t, Chatcmd002RenderCard(m, "post", view, false))
	for _, want := range []string{`class="chatcmd002-edit"`, `data-chatcmd002-field="title"`, `data-chatcmd002-field="opt:0"`, `data-chatcmd002-field="opt:1"`, `value="Here"`, "Question", "Option 1", "Option 2", `data-action="chatcmd002-edit-save"`, `data-action="chatcmd002-edit-cancel"`, "Save changes", "until the first vote", `data-chatcmd002-escape="chatcmd002-edit-cancel"`} {
		if !strings.Contains(form, want) {
			t.Errorf("edit form lacks %s: %s", want, form)
		}
	}
	if strings.Contains(form, "chatcmd002-vote") || strings.Contains(form, "Close poll") {
		t.Errorf("the body and the foot stay out of the form: %s", form)
	}
	// Once the card has a vote, an open form is not drawn.
	if got := renderNode(t, Chatcmd002RenderCard(m, "post", voted, false)); strings.Contains(got, `class="chatcmd002-edit"`) {
		t.Errorf("a form for a voted card: %s", got)
	}

	if chatcmd002SaveEdit(m, "post", view, 3, map[string]string{"title": "Where?", "opt:0": "Here"}) || len(*sent) != 0 {
		t.Fatal("an unchanged card was sent")
	}
	if chatcmd002SaveEdit(m, "post", view, 3, map[string]string{"opt:0": "   "}) || chatcmd002SaveEdit(m, "post", view, 3, map[string]string{"opt:0": "there"}) || len(*sent) != 0 {
		t.Fatal("an empty or repeated option was sent")
	}
	if !chatcmd002SaveEdit(m, "post", view, 3, map[string]string{"title": " Where for lunch? ", "opt:1": "Over there"}) || len(*sent) != 1 {
		t.Fatalf("save sent %+v", *sent)
	}
	got := (*sent)[0]
	if got.Operation != "EDIT" || got.Card == nil || got.Card.Title != "Where for lunch?" || got.Card.Poll.Options[0].Text != "Here" || got.Card.Poll.Options[1].Text != "Over there" || got.Card.Poll.Options[1].ID != "b" {
		t.Fatalf("mutation %+v", got)
	}
	if view.Card.Poll.Options[1].Text != "There" || view.Card.Title != "Where?" {
		t.Fatal("the card on the page changed before the server answered")
	}
	if chatcmd002SaveEdit(m, "post", voted, 3, map[string]string{"title": "Changed"}) {
		t.Fatal("a voted card was reworded")
	}
}

func TestTodo_CHATCMD_002_EditList(t *testing.T) {
	list, err := chat.Chatcmd004ParseTodo(`"Launch" 1="Send invites" 2="Book the room"`, nil, "me", time.Now(), chat.Chatcmd004ResolveDate)
	if err != nil {
		t.Fatal(err)
	}
	list.Card.Todo.Items[0].ID, list.Card.Todo.Items[1].ID = "one", "two"
	m := chatcmd003UIFixture()
	m.Chatcmd002.Mutate = func(string, uint64, chat.Chatcmd002Mutation) {}
	view := chat.Chatcmd002View{Card: list.Card, CanManage: true}
	if got := renderNode(t, Chatcmd002RenderCard(m, "post", view, false)); !strings.Contains(got, "Edit list") || strings.Contains(got, "Edit poll") {
		t.Fatalf("list controls: %s", got)
	}
	m.cardEditing = "post"
	form := renderNode(t, Chatcmd002RenderCard(m, "post", view, false))
	for _, want := range []string{`data-chatcmd002-field="item:1"`, `value="Book the room"`, "Task 2", ">Title<"} {
		if !strings.Contains(form, want) {
			t.Errorf("list form lacks %s: %s", want, form)
		}
	}
	next, changed, ok := chatcmd002EditedCard(list.Card, map[string]string{"item:1": " Book the big room "})
	if !ok || !changed || next.Todo.Items[1].Text != "Book the big room" || list.Card.Todo.Items[1].Text != "Book the room" {
		t.Fatalf("edited %+v changed %v ok %v", next.Todo.Items, changed, ok)
	}
}

// TestTodo_CHATCMD_002_EditState: the form is local to the page; pressing Edit
// opens it for the card, Cancel closes it, a card the reader may not edit never
// opens, and leaving the conversation drops it.
func TestTodo_CHATCMD_002_EditState(t *testing.T) {
	m, view, _ := chatcmd002EditFixture(t, "author")
	local := localStore{box: &localUI{}}
	chatcmd002ActionLocal(m, local, "chatcmd002-edit", "post", "")
	if local.get().cardEdit != "" {
		t.Fatal("a reader who may not edit opened the form")
	}
	view.CanManage = true
	m.Chatcmd002Views["post"] = view
	chatcmd002ActionLocal(m, local, "chatcmd002-edit", "post", "")
	if local.get().cardEdit != "post" {
		t.Fatal("Edit did not open the form")
	}
	chatcmd002ActionLocal(m, local, "chatcmd002-edit-cancel", "post", "")
	if local.get().cardEdit != "" {
		t.Fatal("Cancel did not close the form")
	}
	chatcmd002ActionLocal(m, local, "chatcmd002-edit", "post", "")
	chatcmd002ActionLocal(m, local, "chatcmd002-edit-save", "post", "")
	if local.get().cardEdit != "" {
		t.Fatal("Save left the form open")
	}
	chatcmd002ActionLocal(m, local, "chatcmd002-edit", "post", "")
	local.forRoom("elsewhere")
	if local.get().cardEdit != "" {
		t.Fatal("the form followed the person to another conversation")
	}
}

// TestTodo_CHATCMD_002_StandingPoll: the poll from before polls were messages
// shows no vote rows. Its managers are offered "Move it here" beside "Close
// poll"; anyone else sees the result only.
func TestTodo_CHATCMD_002_StandingPoll(t *testing.T) {
	m := chatcmd003UIFixture()
	m.ChannelPoll = ChannelPoll{Question: "Lunch spot?", TotalVotes: 3, MyOptionID: "o1", Options: []ChannelPollOption{{ID: "o1", Text: "Tacos", Count: 2}, {ID: "o2", Text: "Pho", Count: 1}}}
	m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
	m.Callbacks.VoteChannelPoll = func(string) {}
	reader := renderNode(t, channelPollSection(m, handlers{}))
	if strings.Contains(reader, `data-action="poll-vote"`) || strings.Contains(reader, "channel-poll-vote") || strings.Contains(reader, "chatbug057-move") || strings.Contains(reader, "poll-close") {
		t.Fatalf("a reader's standing poll: %s", reader)
	}
	for _, want := range []string{"Tacos", "67%", "Pho", "33%", "<progress", "read here but not voted on"} {
		if !strings.Contains(reader, want) {
			t.Errorf("the result lacks %s: %s", want, reader)
		}
	}
	m.ChannelTeam.CanPin = true
	m.Chatcmd002.CloseChannelPoll = func() {}
	manager := renderNode(t, channelPollSection(m, handlers{}))
	move, closeAt := strings.Index(manager, `data-action="chatbug057-move"`), strings.Index(manager, `data-action="poll-close"`)
	if move < 0 || closeAt < 0 || move > closeAt || !strings.Contains(manager, "Move it here") || !strings.Contains(manager, "Close poll") {
		t.Fatalf("manager's standing poll: %s", manager)
	}
	m.ChannelPollPending = true
	if busy := renderNode(t, channelPollSection(m, handlers{})); strings.Contains(busy, "chatbug057-move") {
		t.Fatalf("Move offered while the poll is changing: %s", busy)
	}
}

// TestTodo_CHATCMD_002_EditCopy: the new words exist in the three languages and
// none is printed as a key.
func TestTodo_CHATCMD_002_EditCopy(t *testing.T) {
	for key, row := range chatcmd002EditCopy {
		for i, text := range row {
			if strings.TrimSpace(text) == "" || strings.Contains(text, "chatcmd002") {
				t.Errorf("%s has no text in language %d", key, i)
			}
		}
	}
	for key, row := range chatcmd002TrayCopy {
		for i, text := range row {
			if strings.TrimSpace(text) == "" {
				t.Errorf("tray %s has no text in language %d", key, i)
			}
		}
	}
	m := chatcmd003UIFixture()
	m.Locale = "de-DE"
	if got := chatcmd002EditText(m, "edit-poll"); got != "Umfrage bearbeiten" {
		t.Fatalf("de-DE %q", got)
	}
	m.Locale = "ar"
	if got := chatcmd002EditText(m, "save"); got != "حفظ التغييرات" {
		t.Fatalf("ar %q", got)
	}
}
