package chat

import (
	"errors"
	"testing"
	"time"
)

func issue3947Card(tick string) Chatcmd002Card {
	due := time.Date(2026, 12, 1, 23, 59, 0, 0, time.UTC)
	return Chatcmd002Card{Kind: "todo", Title: "Launch", Todo: &Chatcmd002Todo{Tick: tick, Items: []Chatcmd002Task{
		{ChannelTodoItem: ChannelTodoItem{ID: "a", Text: "Book room", Completed: true, CompletedBySubjectID: "ana", CompletedByHomeTenantID: "t", CompletedAtUnix: 5}, AssigneeID: "ana", AssigneeHomeTenantID: "t", AssigneeName: "Ana", DueAt: &due},
		{ChannelTodoItem: ChannelTodoItem{ID: "b", Text: "Order food"}, AssigneeID: "bo", AssigneeHomeTenantID: "t"},
	}}}
}

// TestIssue3947_ReassignKeepsCompletionAndRecordsWho: handing a finished task to
// someone else leaves it finished by the person who finished it, and the task
// says who changed it and when.
func TestIssue3947_ReassignKeepsCompletionAndRecordsWho(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	next, changed, err := issue3947Card("anyone").ChangeTask(Chatcmd002TaskChange{ItemID: "a", SetAssignee: true, AssigneeID: "cy", AssigneeHomeTenantID: "t"}, Principal{TenantID: "t", SubjectID: "owner"}, now)
	if err != nil || !changed {
		t.Fatalf("reassign: changed=%v err=%v", changed, err)
	}
	task := next.Todo.Items[0]
	if task.AssigneeID != "cy" || task.AssigneeName != "" || !task.Completed || task.CompletedBySubjectID != "ana" || task.CompletedAtUnix != 5 {
		t.Fatalf("completion or assignee wrong: %+v", task)
	}
	if task.DueAt == nil || task.Changed != Chatcmd002ChangedAssignee || task.ChangedBySubjectID != "owner" || task.ChangedByHomeTenantID != "t" || task.ChangedAtUnix != now.Unix() {
		t.Fatalf("the due date was touched or the change is not recorded: %+v", task)
	}
	if next.Todo.Items[1].Changed != "" {
		t.Fatalf("another task was marked changed: %+v", next.Todo.Items[1])
	}
	if issue3947Card("anyone").Todo.Items[0].AssigneeID != "ana" {
		t.Fatal("the original card was modified in place")
	}
}

// TestIssue3947_SetAndClearDue: a date can be moved or cleared and the assignee
// is left alone; a date that is already there is no change.
func TestIssue3947_SetAndClearDue(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	by := Principal{TenantID: "t", SubjectID: "owner"}
	moved := time.Date(2027, 1, 5, 9, 30, 0, 0, time.UTC)
	next, changed, err := issue3947Card("anyone").ChangeTask(Chatcmd002TaskChange{ItemID: "a", SetDue: true, DueAt: &moved, DueTimed: true}, by, now)
	if err != nil || !changed || !next.Todo.Items[0].DueAt.Equal(moved) || !next.Todo.Items[0].DueTimed || next.Todo.Items[0].AssigneeID != "ana" || next.Todo.Items[0].Changed != Chatcmd002ChangedDue {
		t.Fatalf("move: %+v %v %v", next.Todo.Items[0], changed, err)
	}
	cleared, changed, err := next.ChangeTask(Chatcmd002TaskChange{ItemID: "a", SetDue: true}, by, now)
	if err != nil || !changed || cleared.Todo.Items[0].DueAt != nil || cleared.Todo.Items[0].DueTimed {
		t.Fatalf("clear: %+v %v %v", cleared.Todo.Items[0], changed, err)
	}
	if _, changed, err = cleared.ChangeTask(Chatcmd002TaskChange{ItemID: "a", SetDue: true}, by, now); err != nil || changed {
		t.Fatalf("clearing a date that is already clear changed something: %v %v", changed, err)
	}
	if _, changed, err = next.ChangeTask(Chatcmd002TaskChange{ItemID: "a", SetDue: true, DueAt: &moved, DueTimed: true}, by, now); err != nil || changed {
		t.Fatalf("the same date changed something: %v %v", changed, err)
	}
	both, changed, err := issue3947Card("anyone").ChangeTask(Chatcmd002TaskChange{ItemID: "b", SetAssignee: true, SetDue: true, DueAt: &moved}, by, now)
	if err != nil || !changed || both.Todo.Items[1].AssigneeID != "" || both.Todo.Items[1].DueAt == nil || both.Todo.Items[1].Changed != Chatcmd002ChangedAssigneeDue {
		t.Fatalf("both: %+v %v %v", both.Todo.Items[1], changed, err)
	}
}

// TestIssue3947_AssigneeOnlyListKeepsAnAssignee: on a list that lets only the
// assignee tick, a task is handed on and never left with nobody who could
// finish it; on any other list it may be unassigned.
func TestIssue3947_AssigneeOnlyListKeepsAnAssignee(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	by := Principal{TenantID: "t", SubjectID: "owner"}
	if _, _, err := issue3947Card("assignee").ChangeTask(Chatcmd002TaskChange{ItemID: "b", SetAssignee: true}, by, now); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("unassigning on an assignee-only list: %v", err)
	}
	if _, changed, err := issue3947Card("assignee").ChangeTask(Chatcmd002TaskChange{ItemID: "b", SetAssignee: true, AssigneeID: "cy", AssigneeHomeTenantID: "t"}, by, now); err != nil || !changed {
		t.Fatalf("handing on: %v %v", changed, err)
	}
	if next, changed, err := issue3947Card("author").ChangeTask(Chatcmd002TaskChange{ItemID: "b", SetAssignee: true}, by, now); err != nil || !changed || next.Todo.Items[1].AssigneeID != "" {
		t.Fatalf("unassigning on an author list: %v %v", changed, err)
	}
}

// TestIssue3947_RefusesWhatItCannotApply: a poll, a closed list, a missing task,
// half a person and an empty request are all refused.
func TestIssue3947_RefusesWhatItCannotApply(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	by := Principal{TenantID: "t", SubjectID: "owner"}
	closedAt := now.Add(-time.Hour)
	closed := issue3947Card("anyone")
	closed.ClosedAt = &closedAt
	cases := map[string]struct {
		card   Chatcmd002Card
		change Chatcmd002TaskChange
		want   error
	}{
		"poll":        {Chatcmd002Card{Kind: "poll", Poll: &Chatcmd002Poll{}}, Chatcmd002TaskChange{ItemID: "a", SetDue: true}, ErrPermissionDenied},
		"closed":      {closed, Chatcmd002TaskChange{ItemID: "a", SetDue: true}, ErrPermissionDenied},
		"missing":     {issue3947Card("anyone"), Chatcmd002TaskChange{ItemID: "zz", SetDue: true}, ErrNotFound},
		"half person": {issue3947Card("anyone"), Chatcmd002TaskChange{ItemID: "a", SetAssignee: true, AssigneeID: "cy"}, ErrInvalidArgument},
		"nothing":     {issue3947Card("anyone"), Chatcmd002TaskChange{ItemID: "a"}, ErrInvalidArgument},
		"stray owner": {issue3947Card("anyone"), Chatcmd002TaskChange{ItemID: "a", SetDue: true, AssigneeID: "cy", AssigneeHomeTenantID: "t"}, ErrInvalidArgument},
		"zero date":   {issue3947Card("anyone"), Chatcmd002TaskChange{ItemID: "a", SetDue: true, DueAt: &time.Time{}}, ErrInvalidArgument},
	}
	for name, c := range cases {
		if _, changed, err := c.card.ChangeTask(c.change, by, now); !errors.Is(err, c.want) || changed {
			t.Errorf("%s: changed=%v err=%v, want %v", name, changed, err, c.want)
		}
	}
}

// TestIssue3947_ChangedCardStillDecodes: the record of the change survives the
// card's own body round trip.
func TestIssue3947_ChangedCardStillDecodes(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	next, _, err := issue3947Card("anyone").ChangeTask(Chatcmd002TaskChange{ItemID: "b", SetAssignee: true, AssigneeID: "cy", AssigneeHomeTenantID: "t"}, Principal{TenantID: "t", SubjectID: "owner"}, now)
	if err != nil {
		t.Fatal(err)
	}
	body, err := next.Body()
	if err != nil {
		t.Fatal(err)
	}
	back, ok := Chatcmd002Decode(body)
	if !ok || back.Todo.Items[1].ChangedBySubjectID != "owner" || back.Todo.Items[1].AssigneeID != "cy" {
		t.Fatalf("decoded %+v %v", back, ok)
	}
}
