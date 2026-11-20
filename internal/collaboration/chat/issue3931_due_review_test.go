package chat

import (
	"testing"
	"time"
)

func issue3931Now() time.Time {
	// Thursday 2026-11-12.
	return time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
}

func issue3931Day(t *testing.T, at *time.Time) string {
	t.Helper()
	if at == nil {
		return ""
	}
	return at.Format("2006-01-02")
}

// TestIssue3931_InferredDueCanBeCleared: a date read out of the task is
// reported with its phrase, and choosing "none" keeps the words in the task
// and leaves it with no deadline.
func TestIssue3931_InferredDueCanBeCleared(t *testing.T) {
	d, err := Chatcmd004ParseTodo("Discuss the schedule for Friday", nil, "me", issue3931Now(), Chatcmd004ResolveDate)
	item := d.Card.Todo.Items[0]
	if err != nil || item.Text != "Discuss the schedule for" || issue3931Day(t, item.DueAt) != "2026-11-13" {
		t.Fatalf("default reading changed: %+v %v", item, err)
	}
	if len(d.DueReads) != 1 || d.DueReads[0].Phrase != "Friday" || d.DueReads[0].Default != "infer" {
		t.Fatalf("inference not reported: %+v", d.DueReads)
	}
	d, err = Chatcmd004ParseTodoReviewed("Discuss the schedule for Friday", nil, "me", issue3931Now(), Chatcmd004ResolveDate, false, Chatcmd004Review{Due: map[int]string{0: "none"}})
	item = d.Card.Todo.Items[0]
	if err != nil || item.Text != "Discuss the schedule for Friday" || item.DueAt != nil {
		t.Fatalf("not cleared: %+v %v", item, err)
	}
	for _, change := range d.Changes {
		if change.Kind == Chatcmd004ChangeDue {
			t.Fatalf("a cleared date is still listed as a change: %+v", d.Changes)
		}
	}
	if len(d.DueReads) != 1 {
		t.Fatalf("the choice must stay reachable so it can be undone: %+v", d.DueReads)
	}
}

// TestIssue3931_SameWeekdayOffersToday: on a Thursday, "by Thursday" reads as
// next week and offers today as an explicit alternative.
func TestIssue3931_SameWeekdayOffersToday(t *testing.T) {
	line := "Review by Thursday"
	d, _ := Chatcmd004ParseTodo(line, nil, "me", issue3931Now(), Chatcmd004ResolveDate)
	if got := issue3931Day(t, d.Card.Todo.Items[0].DueAt); got != "2026-11-19" {
		t.Fatalf("default changed: %s", got)
	}
	var keys []string
	for _, o := range d.DueReads[0].Options {
		keys = append(keys, o.Key)
	}
	if len(keys) != 3 || keys[0] != "infer" || keys[1] != "today" || keys[2] != "none" {
		t.Fatalf("options %v", keys)
	}
	d, _ = Chatcmd004ParseTodoReviewed(line, nil, "me", issue3931Now(), Chatcmd004ResolveDate, false, Chatcmd004Review{Due: map[int]string{0: "today"}})
	if item := d.Card.Todo.Items[0]; issue3931Day(t, item.DueAt) != "2026-11-12" || item.Text != "Review" {
		t.Fatalf("today: %+v", item)
	}
	// A weekday that is not today has no today option.
	d, _ = Chatcmd004ParseTodo("Review by Friday", nil, "me", issue3931Now(), Chatcmd004ResolveDate)
	if len(d.DueReads[0].Options) != 2 {
		t.Fatalf("Friday options: %+v", d.DueReads[0].Options)
	}
}

// TestIssue3931_QualifiedWeekdayKeepsItsQualifier: "next Friday" is not split
// into a dangling "next" and a Friday; both readings are offered, nothing is
// chosen for the author, and the whole phrase stays in the task until they do.
func TestIssue3931_QualifiedWeekdayKeepsItsQualifier(t *testing.T) {
	for _, tc := range []struct{ line, text, a, b string }{
		{"Submit timecard by next Friday", "Submit timecard", "2026-11-13", "2026-11-20"},
		{"Submit timecard by next Monday", "Submit timecard", "2026-11-16", "2026-11-23"},
		{"Submit timecard next Friday", "Submit timecard", "2026-11-13", "2026-11-20"},
		{"Zeiterfassung bis nächsten Freitag", "Zeiterfassung", "2026-11-13", "2026-11-20"},
		{"قدّم الجدول بحلول الجمعة القادمة", "قدّم الجدول", "2026-11-13", "2026-11-20"},
	} {
		d, err := Chatcmd004ParseTodo(tc.line, nil, "me", issue3931Now(), Chatcmd004ResolveDate)
		item := d.Card.Todo.Items[0]
		if err != nil || item.DueAt != nil || item.Text != tc.line {
			t.Fatalf("%q: a qualified weekday was resolved or cut up: %+v", tc.line, item)
		}
		if len(d.DueReads) != 1 || d.DueReads[0].Default != "none" || len(d.DueReads[0].Options) != 3 {
			t.Fatalf("%q: reads %+v", tc.line, d.DueReads)
		}
		for key, want := range map[string]string{"a": tc.a, "b": tc.b} {
			d, _ = Chatcmd004ParseTodoReviewed(tc.line, nil, "me", issue3931Now(), Chatcmd004ResolveDate, false, Chatcmd004Review{Due: map[int]string{0: key}})
			item = d.Card.Todo.Items[0]
			if issue3931Day(t, item.DueAt) != want || item.Text != tc.text {
				t.Fatalf("%q choice %s: %+v", tc.line, key, item)
			}
		}
	}
	// The control reading is unchanged.
	d, _ := Chatcmd004ParseTodo("Submit timecard by Friday", nil, "me", issue3931Now(), Chatcmd004ResolveDate)
	if item := d.Card.Todo.Items[0]; item.Text != "Submit timecard" || issue3931Day(t, item.DueAt) != "2026-11-13" {
		t.Fatalf("control: %+v", item)
	}
}

// TestIssue3931_InferredAssigneeCanBeKeptAsText: a name taken as the owner is
// reported, and keeping it as text leaves the words in the task, no owner.
func TestIssue3931_InferredAssigneeCanBeKeptAsText(t *testing.T) {
	members := []Chatcmd004Member{{HomeTenantID: "t", ID: "sofia", Name: "Sofia Petrov"}}
	line := "Confirm benefit renewal @Sofia Petrov tomorrow"
	d, _ := Chatcmd004ParseTodo(line, members, "me", issue3931Now(), Chatcmd004ResolveDate)
	item := d.Card.Todo.Items[0]
	if item.AssigneeID != "sofia" || len(d.OwnerReads) != 1 || d.OwnerReads[0].Name != "Sofia Petrov" || d.OwnerReads[0].Kept {
		t.Fatalf("%+v %+v", item, d.OwnerReads)
	}
	d, _ = Chatcmd004ParseTodoReviewed(line, members, "me", issue3931Now(), Chatcmd004ResolveDate, false, Chatcmd004Review{KeepOwner: map[int]bool{0: true}})
	item = d.Card.Todo.Items[0]
	if item.AssigneeID != "" || item.AssigneeHomeTenantID != "" || item.Text == "" || len(d.OwnerReads) != 1 || !d.OwnerReads[0].Kept || len(d.Unresolved) != 0 {
		t.Fatalf("kept: %+v %+v %+v", item, d.OwnerReads, d.Unresolved)
	}
	if want := "Confirm benefit renewal @Sofia Petrov"; item.Text != want || item.DueAt == nil {
		t.Fatalf("text %q due %v", item.Text, item.DueAt)
	}
}
