package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func issue3947Fixture(tick string) (Model, chat.Chatcmd002View, *[]chat.Chatcmd002Mutation) {
	due := time.Date(2026, 12, 1, 23, 59, 0, 0, time.UTC)
	var sent []chat.Chatcmd002Mutation
	m := chatcmd003UIFixture()
	m.Chatcmd002TimeZone = "UTC"
	m.Members = append(m.Members, Member{ID: "bot", Name: "Helper bot", HomeTenantID: "t", Agent: true})
	m.Chatcmd002.Mutate = func(_ string, _ uint64, mutation chat.Chatcmd002Mutation) { sent = append(sent, mutation) }
	card := chat.Chatcmd002Card{Kind: "todo", Title: "Launch", Todo: &chat.Chatcmd002Todo{Tick: tick, Items: []chat.Chatcmd002Task{
		{ChannelTodoItem: chat.ChannelTodoItem{ID: "one", Text: "Send invites"}, AssigneeID: "dana", AssigneeHomeTenantID: "t", DueAt: &due},
		{ChannelTodoItem: chat.ChannelTodoItem{ID: "two", Text: "Book room", Completed: true}, AssigneeID: "gone", AssigneeHomeTenantID: "t"},
	}}}
	view := chat.Chatcmd002View{Card: card, CanManage: true, Revision: 4, CanTick: map[string]bool{}}
	m.Chatcmd002Views = map[string]Chatcmd002View{"post": view}
	return m, view, &sent
}

// TestIssue3947_ControlOnlyForTheManager: "Change owner or date" is on every task
// of an open list for the person who manages it, and nowhere else: not for a
// reader, not on a preview, not on a closed list, not on a poll.
func TestIssue3947_ControlOnlyForTheManager(t *testing.T) {
	m, view, _ := issue3947Fixture("anyone")
	got := renderNode(t, Chatcmd002RenderCard(m, "post", view, false))
	for _, want := range []string{`data-action="chatcmd002-task-edit"`, `data-extra="one"`, `data-extra="two"`, `id="issue3947-open-post-one"`, `aria-label="Change owner or date: Send invites"`} {
		if !strings.Contains(got, want) {
			t.Errorf("manager card lacks %s: %s", want, got)
		}
	}
	reader := view
	reader.CanManage = false
	closedAt := time.Now().Add(-time.Hour)
	closed := view
	closed.Card.ClosedAt = &closedAt
	for name, markup := range map[string]string{
		"reader":  renderNode(t, Chatcmd002RenderCard(m, "post", reader, false)),
		"preview": renderNode(t, Chatcmd002RenderCard(m, "", view, true)),
		"closed":  renderNode(t, Chatcmd002RenderCard(m, "post", closed, false)),
	} {
		if strings.Contains(markup, "chatcmd002-task-edit") {
			t.Errorf("%s is offered the control: %s", name, markup)
		}
	}
}

// TestIssue3947_FormShowsCurrentValues: the open form names the people of the
// conversation (not its agents), selects the present owner, holds the present
// date, keeps a former member as the choice so Save cannot move the task by
// accident, and wires Enter and Escape.
func TestIssue3947_FormShowsCurrentValues(t *testing.T) {
	m, view, _ := issue3947Fixture("anyone")
	m.issue3947 = issue3947State{Post: "post", Item: "one"}
	got := renderNode(t, Chatcmd002RenderCard(m, "post", view, false))
	for _, want := range []string{`class="chatcmd002-edit chatcmd002-task-form"`, `data-chatcmd002-field="task-owner"`, `data-chatcmd002-field="task-date"`, `data-chat-value="2026-12-01"`, `data-chatcmd002-field="task-time"`, `type="date"`, `type="time"`, `>Dana<`, `>Nobody<`, `data-chatcmd002-enter="chatcmd002-task-save"`, `data-chatcmd002-escape="chatcmd002-task-cancel"`, `data-action="chatcmd002-task-save"`, `data-action="chatcmd002-task-cancel"`, "A finished task stays finished.", `id="issue3947-owner-post-one"`, `role="group"`} {
		if !strings.Contains(got, want) {
			t.Errorf("form lacks %s: %s", want, got)
		}
	}
	if strings.Contains(got, "Helper bot") {
		t.Errorf("an agent is offered as an owner: %s", got)
	}
	if strings.Contains(got, `data-action="chatcmd002-task-edit" data-extra="one"`) {
		t.Errorf("the open task still offers to open: %s", got)
	}
	m.issue3947 = issue3947State{Post: "post", Item: "two"}
	if got := renderNode(t, Chatcmd002RenderCard(m, "post", view, false)); !strings.Contains(got, ">Former member<") {
		t.Errorf("a former owner is not kept as the choice: %s", got)
	}
}

// TestIssue3947_AssigneeOnlyListHasNoNobody: where only the assignee ticks, the
// form does not offer "Nobody" and says why.
func TestIssue3947_AssigneeOnlyListHasNoNobody(t *testing.T) {
	m, view, _ := issue3947Fixture("assignee")
	m.issue3947 = issue3947State{Post: "post", Item: "one"}
	got := renderNode(t, Chatcmd002RenderCard(m, "post", view, false))
	if strings.Contains(got, ">Nobody<") || !strings.Contains(got, "only the person a task is assigned to can tick it") {
		t.Fatalf("assignee-only form: %s", got)
	}
}

// TestIssue3947_SaveSendsOnlyWhatChanged: Save sends one CHANGE_TASK carrying
// only the owner, the date, or both that differ; unchanged values send nothing.
func TestIssue3947_SaveSendsOnlyWhatChanged(t *testing.T) {
	m, view, sent := issue3947Fixture("anyone")
	local := localStore{box: &localUI{}}
	save := func(values map[string]string) {
		t.Helper()
		issue3947Fields = func(string) map[string]string { return values }
		defer func() { issue3947Fields = chatcmd002FieldValues }()
		chatcmd002ActionLocal(m, local, "chatcmd002-task-edit", "post", "one")
		if local.get().task.Item != "one" {
			t.Fatal("Change owner or date did not open the form")
		}
		chatcmd002ActionLocal(m, local, "chatcmd002-task-save", "post", "")
	}
	save(map[string]string{"task-owner": "t\x1fdana", "task-date": "2026-12-01", "task-time": ""})
	if len(*sent) != 0 || local.get().task.Item != "" {
		t.Fatalf("an unchanged form sent %+v or stayed open", *sent)
	}
	save(map[string]string{"task-owner": "t\x1f" + "me", "task-date": "2026-12-01"})
	if len(*sent) != 1 {
		t.Fatalf("sent %+v", *sent)
	}
	got := (*sent)[0]
	if got.Operation != "CHANGE_TASK" || got.ItemID != "one" || !got.SetAssignee || got.AssigneeID != "me" || got.AssigneeHomeTenantID != "t" || got.SetDue {
		t.Fatalf("owner change %+v", got)
	}
	save(map[string]string{"task-owner": "t\x1fdana", "task-date": "2027-01-05", "task-time": "09:30"})
	got = (*sent)[1]
	want := time.Date(2027, 1, 5, 9, 30, 0, 0, time.UTC)
	if got.SetAssignee || !got.SetDue || got.DueAt == nil || !got.DueAt.Equal(want) || !got.DueTimed {
		t.Fatalf("date change %+v", got)
	}
	save(map[string]string{"task-owner": "\x1f", "task-date": ""})
	got = (*sent)[2]
	if !got.SetAssignee || got.AssigneeID != "" || !got.SetDue || got.DueAt != nil {
		t.Fatalf("unassign and clear %+v", got)
	}
	// A date alone means the end of that day, not a time.
	save(map[string]string{"task-owner": "t\x1fdana", "task-date": "2027-03-04"})
	got = (*sent)[3]
	if got.DueTimed || got.DueAt == nil || got.DueAt.Hour() != 23 || got.DueAt.Minute() != 59 {
		t.Fatalf("date only %+v", got)
	}
	if view.Card.Todo.Items[0].AssigneeID != "dana" {
		t.Fatal("the card on the page changed before the server answered")
	}
}

// TestIssue3947_SaveRefusesWhatCannotWork: a time without a date and an
// assignee-only task left with nobody keep the form open with a note and send
// nothing; a reader who cannot manage the card cannot open the form at all.
func TestIssue3947_SaveRefusesWhatCannotWork(t *testing.T) {
	m, _, sent := issue3947Fixture("assignee")
	local := localStore{box: &localUI{}}
	issue3947Fields = func(string) map[string]string {
		return map[string]string{"task-owner": "\x1f", "task-date": "", "task-time": "10:00"}
	}
	defer func() { issue3947Fields = chatcmd002FieldValues }()
	chatcmd002ActionLocal(m, local, "chatcmd002-task-edit", "post", "one")
	chatcmd002ActionLocal(m, local, "chatcmd002-task-save", "post", "")
	if len(*sent) != 0 || local.get().task.Item != "one" || local.get().task.Note != "note-owner" {
		t.Fatalf("sent %+v state %+v", *sent, local.get().task)
	}
	m.issue3947 = local.get().task
	view := m.Chatcmd002Views["post"]
	if got := renderNode(t, Chatcmd002RenderCard(m, "post", view, false)); !strings.Contains(got, `role="alert"`) || !strings.Contains(got, "Choose who this task is for.") {
		t.Fatalf("no note on the form: %s", got)
	}
	issue3947Fields = func(string) map[string]string { return map[string]string{"task-date": "", "task-time": "10:00"} }
	chatcmd002ActionLocal(m, local, "chatcmd002-task-save", "post", "")
	if len(*sent) != 0 || local.get().task.Note != "note-time" {
		t.Fatalf("a time without a date: %+v %+v", *sent, local.get().task)
	}
	chatcmd002ActionLocal(m, local, "chatcmd002-task-cancel", "post", "")
	if local.get().task.Item != "" {
		t.Fatal("Cancel left the form open")
	}
	view.CanManage = false
	m.Chatcmd002Views["post"] = view
	chatcmd002ActionLocal(m, local, "chatcmd002-task-edit", "post", "one")
	if local.get().task.Item != "" {
		t.Fatal("a reader opened the form")
	}
	m, view, _ = issue3947Fixture("anyone")
	chatcmd002ActionLocal(m, local, "chatcmd002-task-edit", "post", "nope")
	if local.get().task.Item != "" {
		t.Fatal("the form opened for a task the card does not have")
	}
	chatcmd002ActionLocal(m, local, "chatcmd002-task-edit", "post", "one")
	local.forRoom("elsewhere")
	if local.get().task.Item != "" {
		t.Fatal("the form followed the person to another conversation")
	}
}

// TestIssue3947_ShowsWhoChangedWhen: a changed task says what changed, who did
// it and when, in the reader's language.
func TestIssue3947_ShowsWhoChangedWhen(t *testing.T) {
	m, view, _ := issue3947Fixture("anyone")
	at := time.Date(2026, 11, 20, 9, 5, 0, 0, time.UTC).Unix()
	todo := *view.Card.Todo
	todo.Items = append([]chat.Chatcmd002Task(nil), todo.Items...)
	todo.Items[0].Changed, todo.Items[0].ChangedBySubjectID, todo.Items[0].ChangedByHomeTenantID, todo.Items[0].ChangedAtUnix = chat.Chatcmd002ChangedAssignee, "me", "t", at
	view.Card.Todo = &todo
	view.CanManage = false
	if got := renderNode(t, Chatcmd002RenderCard(m, "post", view, false)); !strings.Contains(got, "Owner changed by you, 2026-11-20 09:05 UTC") {
		t.Fatalf("english: %s", got)
	}
	m.Locale = "de-DE"
	if got := renderNode(t, Chatcmd002RenderCard(m, "post", view, false)); !strings.Contains(got, "Besitzer geändert von") {
		t.Fatalf("german: %s", got)
	}
	m.Locale = "ar"
	if got := renderNode(t, Chatcmd002RenderCard(m, "post", view, false)); !strings.Contains(got, "غيّر") {
		t.Fatalf("arabic: %s", got)
	}
}

// TestIssue3947_CopyInEveryLanguage: every string the form uses has an entry in
// each of the three languages.
func TestIssue3947_CopyInEveryLanguage(t *testing.T) {
	for key, copy := range issue3947Copy {
		for i, text := range copy {
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s has no text for language %d", key, i)
			}
		}
	}
}

// TestIssue3947_OverdueTaskCanStillBeHandedOn: the task a colleague cannot finish
// is often already late. Saving a new owner with the unchanged, past date must
// send the owner alone; a new date in the past is refused with a note that says
// so, not one about a clock that skips an hour.
func TestIssue3947_OverdueTaskCanStillBeHandedOn(t *testing.T) {
	m, view, sent := issue3947Fixture("anyone")
	past := time.Now().Add(-72 * time.Hour).UTC().Truncate(time.Minute)
	todo := *view.Card.Todo
	todo.Items = append([]chat.Chatcmd002Task(nil), todo.Items...)
	todo.Items[0].DueAt, todo.Items[0].DueTimed = &past, true
	view.Card.Todo = &todo
	m.Chatcmd002Views["post"] = view
	local := localStore{box: &localUI{}}
	save := func(values map[string]string) {
		t.Helper()
		issue3947Fields = func(string) map[string]string { return values }
		defer func() { issue3947Fields = chatcmd002FieldValues }()
		chatcmd002ActionLocal(m, local, "chatcmd002-task-edit", "post", "one")
		chatcmd002ActionLocal(m, local, "chatcmd002-task-save", "post", "")
	}
	save(map[string]string{"task-owner": "t\x1fme", "task-date": past.Format("2006-01-02"), "task-time": past.Format("15:04")})
	if len(*sent) != 1 || !(*sent)[0].SetAssignee || (*sent)[0].SetDue || local.get().task.Item != "" {
		t.Fatalf("an overdue task was not handed on: %+v %+v", *sent, local.get().task)
	}
	earlier := past.Add(-24 * time.Hour)
	save(map[string]string{"task-owner": "t\x1fdana", "task-date": earlier.Format("2006-01-02"), "task-time": "10:00"})
	if len(*sent) != 1 || local.get().task.Note != "note-past" {
		t.Fatalf("a new date in the past: %+v %+v", *sent, local.get().task)
	}
	if text := issue3947Text(m, "note-past"); text == "" || strings.Contains(text, "do not exist") {
		t.Fatalf("note %q", text)
	}
}
