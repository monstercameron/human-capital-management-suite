package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func chatbug074Model() Model {
	m := chatcmd003UIFixture()
	m.CurrentTenantID = "t"
	m.ChannelTodo = ChannelTodoList{Revision: 3, Items: []ChannelTodoItem{
		{ID: "open", Text: "Order pizza", CanToggle: true, CanManageCompletionPolicy: true, CompletionMode: "EVERYONE"},
		{ID: "done", Text: "Book the room", Completed: true, CompletedBySubjectID: "dana", CompletedByHomeTenantID: "t", CanToggle: true, CompletionMode: "EVERYONE"},
		{ID: "mine", Text: "Sign the contract", CompletionMode: "ME"},
	}}
	m.Callbacks.AddChannelTodo = func(string, string) {}
	m.Callbacks.SetChannelTodoCompleted = func(string, bool) {}
	m.Callbacks.DeleteChannelTodo = func(string) {}
	m.Callbacks.SetChannelTodoPolicy = func(string, string, []ChannelTodoSelectedMember) {}
	return m
}

// chatbug074Tag returns the opening tag that holds needle.
func chatbug074Tag(t *testing.T, markup, needle string) string {
	t.Helper()
	at := strings.Index(markup, needle)
	if at < 0 {
		t.Fatalf("%s is not in the markup", needle)
	}
	start := strings.LastIndex(markup[:at], "<")
	return markup[start : at+strings.Index(markup[at:], ">")+1]
}

// TestTodo_CHATBUG_074: Enter did not add a task, every task carried a two-line
// permission row, and the list had three ways in.
func TestTodo_CHATBUG_074(t *testing.T) {
	m := chatbug074Model()
	markup := renderNode(t, channelTodoSection(m, handlers{}))

	// Enter adds the task: a form submits on Enter only while its submit button
	// is enabled, and the button was disabled whenever the last render had seen
	// an empty draft, which is every render before the first keystroke.
	form := markup[strings.Index(markup, `class="channel-todo-form"`):]
	if button := chatbug074Tag(t, form, `type="submit"`); strings.Contains(button, "disabled") {
		t.Fatalf("Add is disabled with an empty draft, so Enter cannot submit: %s", button)
	}
	if field := chatbug074Tag(t, form, `id="chat-todo-new"`); !strings.Contains(field, "required") {
		t.Fatalf("the task field no longer declines a blank task: %s", field)
	}
	// The field keeps the caret while a task is being saved.
	m.ChannelTodoPending = true
	saving := renderNode(t, channelTodoSection(m, handlers{}))
	if field := chatbug074Tag(t, saving, `id="chat-todo-new"`); strings.Contains(field, "disabled") {
		t.Fatalf("the task field is disabled while saving, which drops the caret: %s", field)
	}
	m.ChannelTodoPending = false

	// A task is one line. Who may complete it is not printed for "Everyone".
	if strings.Contains(markup, "Who can complete or reopen this task: Everyone</") {
		t.Fatalf("the permission is printed under a task open to everyone: %s", markup)
	}
	rows := strings.Split(markup, `class="channel-todo-row"`)[1:]
	if len(rows) != 3 {
		t.Fatalf("%d task rows", len(rows))
	}
	// The creator's rule lives behind the row's menu button.
	if !strings.Contains(rows[0], `class="icon-button channel-todo-rule-toggle"`) || !strings.Contains(rows[0], `aria-label="Who can complete or reopen this task: Everyone"`) || !strings.Contains(rows[0], `data-chat-disclosure-toggle="true"`) || !strings.Contains(rows[0], "channel-todo-rule-body") || strings.Contains(rows[0], "channel-todo-rule-chip") {
		t.Fatalf("the creator's row: %s", rows[0])
	}
	// A finished task says who finished it; a reader who cannot change its
	// rule gets no menu and no chip while the rule is Everyone.
	if !strings.Contains(rows[1], "Dana") || strings.Contains(rows[1], "channel-todo-rule-toggle") || strings.Contains(rows[1], "channel-todo-rule-chip") {
		t.Fatalf("the finished row: %s", rows[1])
	}
	// A restricted task says so in a short chip.
	if !strings.Contains(rows[2], `class="channel-todo-rule-chip"`) || !strings.Contains(rows[2], "Creator only") || strings.Contains(rows[2], "channel-todo-rule-toggle") {
		t.Fatalf("the restricted row: %s", rows[2])
	}
	for _, want := range []string{".channel-tray-card .channel-todo-row{display:flex", ".channel-todo-rule{display:contents", ".channel-todo-options label"} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("stylesheet lacks %q", want)
		}
	}
	// The header button and the pin are gone from the markup, not hidden here.
	for _, gone := range []string{".conversation-header .channel-todo-trigger{display:none}", ".channel-tray-card .details-section-head{display:none}"} {
		if strings.Contains(Stylesheet, gone) {
			t.Errorf("the stylesheet still hides %q", gone)
		}
	}
	if !strings.Contains(Stylesheet, ChatLane2Styles) {
		t.Error("the one-line row rules are not part of the stylesheet")
	}
	if got := s31LaterOverrides(t, ChatLane2Styles); len(got) != 0 {
		t.Errorf("a later block redraws the one-line row rules, so they lose: %v", got)
	}
}

// The channel's standing poll can be closed, which makes room for the next.
func TestTodo_CHATBUG_074_Poll(t *testing.T) {
	m := chatcmd003UIFixture()
	m.ChannelPoll = ChannelPoll{Revision: 4, Question: "Lunch spot?", Options: []ChannelPollOption{{ID: "a", Text: "Tacos"}, {ID: "b", Text: "Pho"}}}
	m.Callbacks.VoteChannelPoll = func(string) {}
	closed := 0
	m.Chatcmd002.CloseChannelPoll = func() { closed++ }
	if strings.Contains(renderNode(t, channelPollSection(m, handlers{})), "poll-close") {
		t.Fatal("a member who does not manage the channel is offered Close poll")
	}
	m.ChannelTeam.CanPin = true
	markup := renderNode(t, channelPollSection(m, handlers{}))
	if !strings.Contains(markup, `data-action="poll-close"`) || !strings.Contains(markup, "Close poll") {
		t.Fatalf("no Close poll for a manager: %s", markup)
	}
	local := localStore{box: &localUI{tray: "poll"}}
	if !chatcmd003Action(m, local, "poll-close", "", "") || closed != 1 || local.get().tray != "" {
		t.Fatalf("close pressed: closed=%d tray=%q", closed, local.get().tray)
	}
	m.ChannelPollPending = true
	chatcmd003Action(m, local, "poll-close", "", "")
	m.ChannelPollPending, m.ChannelTeam.CanPin = false, false
	chatcmd003Action(m, local, "poll-close", "", "")
	if closed != 1 {
		t.Fatalf("a pending or unoffered close was sent (%d)", closed)
	}
	// A message poll is closed and reopened by its author on the card itself,
	// and any number of them can be posted (TestTodo_CHATBUG_057_States).
	var sent chat.Chatcmd002Mutation
	m.Chatcmd002.Mutate = func(_ string, _ uint64, mutation chat.Chatcmd002Mutation) { sent = mutation }
	m.Chatcmd002Views = map[string]Chatcmd002View{"post": {Card: chatbug057Poll(nil), CanManage: true, Revision: 2}}
	chatcmd003Action(m, local, "chatcmd002-close", "post", "CLOSE")
	if sent.Operation != "CLOSE" {
		t.Fatalf("author close sent %+v", sent)
	}
	chatcmd003Action(m, local, "chatcmd002-close", "post", "EDIT")
	if sent.Operation != "CLOSE" {
		t.Fatal("the close button's operation is not limited to closing and reopening")
	}
}
