package chat

import (
	"testing"
	"time"
)

// TestIssue3951_TimedDeadlineIsMarked: a deadline with a time of day is marked
// as timed and keeps its instant; a date alone is not, so it is never shown as
// a late-night time nobody typed.
func TestIssue3951_TimedDeadlineIsMarked(t *testing.T) {
	zone := time.FixedZone("EST", -5*3600)
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, zone)
	for _, tc := range []struct {
		line  string
		timed bool
		text  string
	}{
		{"Review the plan due in an hour", true, "Review the plan"},
		{"Review the plan due in one hour", true, "Review the plan"},
		{"Review the plan by in an hour", true, "Review the plan"},
		{"Review the plan due 2026-11-13T12:30:00-05:00", true, "Review the plan"},
		{"Review the plan due 2026-11-13", false, "Review the plan"},
		{"Review the plan by tomorrow", false, "Review the plan"},
	} {
		d, err := Chatcmd004ParseTodo(tc.line, nil, "me", now, Chatcmd004ResolveDate)
		item := d.Card.Todo.Items[0]
		if err != nil || item.DueAt == nil || item.DueTimed != tc.timed || item.Text != tc.text {
			t.Fatalf("%q: %+v %v", tc.line, item, err)
		}
	}
	d, _ := Chatcmd004ParseTodo("Review the plan due 2026-11-13T12:30:00-05:00", nil, "me", now, Chatcmd004ResolveDate)
	if got := d.Card.Todo.Items[0].DueAt; got.Hour() != 12 || got.Minute() != 30 {
		t.Fatalf("instant lost: %v", got)
	}
	// The change line carries the instant, so the preview can show its time.
	if len(d.Changes) != 1 || d.Changes[0].Kind != Chatcmd004ChangeDue || d.Changes[0].After != "2026-11-13T12:30:00-05:00" {
		t.Fatalf("timed change: %+v", d.Changes)
	}
	d, _ = Chatcmd004ParseTodo("Review the plan due 2026-11-13", nil, "me", now, Chatcmd004ResolveDate)
	if d.Changes[0].After != "2026-11-13" {
		t.Fatalf("date-only change: %+v", d.Changes)
	}
}

// TestIssue3951_UnreadDateWarnedForEveryLeadIn: the warning for figures that
// are not a date this can read follows by, due, until, before and their German
// and Arabic forms, and never guesses an ambiguous numeric date.
func TestIssue3951_UnreadDateWarnedForEveryLeadIn(t *testing.T) {
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	for _, line := range []string{
		"Submit timecard by 03/04", "Submit timecard due 03/04", "Submit timecard until 03/04", "Submit timecard before 03/04",
		"Submit payroll by tomorrow at 9am", "Submit payroll due tomorrow at 9am",
		"Submit payroll due February 30", "Submit payroll due 2027-02-30",
		"Zeitkarte bis 03/04", "Zeitkarte vor 03/04", "بطاقة الوقت بحلول 03/04", "بطاقة الوقت قبل 03/04",
	} {
		d, err := Chatcmd004ParseTodo(line, nil, "me", now, Chatcmd004ResolveDate)
		item := d.Card.Todo.Items[0]
		if err != nil || item.DueAt != nil || item.Text != line || len(d.Issues) != 1 || d.Issues[0] != "date" {
			t.Fatalf("%q: %+v issues=%v %v", line, item, d.Issues, err)
		}
	}
	// Plain prepositions with figures are not deadlines and raise no warning.
	for _, line := range []string{"Meet on 5th floor", "Buy 2 chairs for the room", "Print 3 copies"} {
		if d, _ := Chatcmd004ParseTodo(line, nil, "me", now, Chatcmd004ResolveDate); len(d.Issues) != 0 {
			t.Fatalf("%q warned: %v", line, d.Issues)
		}
	}
}
