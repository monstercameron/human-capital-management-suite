package chat

import (
	"testing"
	"time"
)

// TestIssue3952_SecondMentionIsReportedNotAssigned: with one assignee per task,
// the first mention is the assignee and every other mentioned person is
// reported by name, so the preview can say they are not assigned and offer a
// choice. Nobody is added or dropped silently.
func TestIssue3952_SecondMentionIsReportedNotAssigned(t *testing.T) {
	members := []Chatcmd004Member{{HomeTenantID: "t", ID: "nia", Name: "Nia Brooks"}, {HomeTenantID: "t", ID: "rosa", Name: "Rosa Santos"}, {HomeTenantID: "t", ID: "me", Name: "Author"}}
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	d, err := Chatcmd004ParseTodo("@Nia Brooks @Rosa Santos review the plan", members, "me", now, Chatcmd004ResolveDate)
	item := d.Card.Todo.Items[0]
	if err != nil || item.AssigneeID != "nia" {
		t.Fatalf("assignee: %+v %v", item, err)
	}
	if len(d.Unresolved) != 1 || d.Unresolved[0].Token != "@Rosa Santos" || d.Unresolved[0].Owner != "Nia Brooks" || d.Unresolved[0].Item != 0 || item.Text != "@Rosa Santos review the plan" {
		t.Fatalf("second mention: %+v text=%q", d.Unresolved, item.Text)
	}
	// Choosing the other person makes them the owner and leaves the plan's words.
	card := d.Card
	if !Chatcmd004AssignUnresolved(&card, d.Unresolved[0], members[1]) || card.Todo.Items[0].AssigneeID != "rosa" || card.Todo.Items[0].Text != "review the plan" {
		t.Fatalf("choose owner: %+v", card.Todo.Items[0])
	}
	// One mention, a mention in another task, and an address are not extra people.
	for _, line := range []string{"@Nia Brooks review the plan with @Nia Brooks", "@Nia Brooks review the plan with @nia", "@Nia Brooks review the plan", "@Nia Brooks review; @Rosa Santos book", "@Nia Brooks mail ops@example.com"} {
		d, _ := Chatcmd004ParseTodo(line, members, "me", now, Chatcmd004ResolveDate)
		if len(d.Unresolved) != 0 {
			t.Fatalf("%q: %+v", line, d.Unresolved)
		}
	}
	// A mention that fits nobody is reported too, with the same owner named.
	d, _ = Chatcmd004ParseTodo("@Nia Brooks @Nobody review the plan", members, "me", now, Chatcmd004ResolveDate)
	if len(d.Unresolved) != 1 || d.Unresolved[0].Token != "@Nobody" || d.Unresolved[0].Owner != "Nia Brooks" {
		t.Fatalf("unknown second mention: %+v", d.Unresolved)
	}
}
