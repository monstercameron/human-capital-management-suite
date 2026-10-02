package chat

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func chatcmd004Fixture() ([]Chatcmd004Member, time.Time) {
	return []Chatcmd004Member{{"t", "me", "Author"}, {"t", "dana-smith", "Dana Smith"}, {"t", "dana-jones", "Dana Jones"}, {"t", "omar", "Omar"}}, time.Date(2026, 10, 1, 12, 0, 0, 0, time.FixedZone("Author", 2*60*60))
}
func TestTodo_CHATCMD_004(t *testing.T) {
	t.Run("posting", chatcmd004PostService)
	members, now := chatcmd004Fixture()
	d, err := Chatcmd004ParseTodo(`"Launch checklist" 1="Book the room" 2="Send invites @Dana Smith by friday"`, members, "me", now, Chatcmd004ResolveDate)
	if err != nil || d.Card.Validate() != nil || len(d.Card.Todo.Items) != 2 || d.Card.Todo.Items[1].AssigneeID != "dana-smith" || d.Card.Todo.Items[1].Text != "Send invites" || d.Card.Todo.Items[1].DueAt.Format("2006-01-02T15:04Z07:00") != "2026-10-02T23:59+02:00" {
		t.Fatalf("todo %+v %v", d, err)
	}
	body, err := d.Card.Body()
	card, ok := Chatcmd002Decode(body)
	if err != nil || !ok || card.Todo.Items[1].AssigneeID != "dana-smith" {
		t.Fatalf("body %q %v", body, err)
	}
}

// chatcmd004LooseGolden pins how a loosely typed list is read (page finding 2
// of 2026-10-02): tasks split at semicolons and lines, a date read from the
// task that carries it, an assignee only on the task that names them, and a
// change listed only when something a reader can see changed.
func chatcmd004LooseGolden(t *testing.T) {
	members, now := chatcmd004Fixture()
	members = append(members, Chatcmd004Member{"t", "loretta", "Loretta Haynes"})
	type task struct{ text, assignee, due string }
	cases := []struct {
		raw     string
		tasks   []task
		changes []Chatcmd003Change
	}{
		{"Order pizza @Loretta Haynes friday; Book room", []task{{"Order pizza", "loretta", "2026-10-02"}, {"Book room", "", ""}},
			[]Chatcmd003Change{{Kind: "split", Before: "Order pizza @Loretta Haynes friday; Book room", After: "2"}, {Kind: "assignee", Before: "Order pizza", After: "Loretta Haynes"}, {Kind: "due", Before: "Order pizza", After: "2026-10-02"}}},
		{"order pizza tomorrow\nbook room oct 9\nship 2026-10-05", []task{{"Order pizza", "", "2026-10-02"}, {"Book room", "", "2026-10-09"}, {"Ship", "", "2026-10-05"}},
			[]Chatcmd003Change{{Kind: "due", Before: "Order pizza", After: "2026-10-02"}, {Kind: "due", Before: "Book room", After: "2026-10-09"}, {Kind: "due", Before: "Ship", After: "2026-10-05"},
				{Before: "order pizza", After: "Order pizza"}, {Before: "book room", After: "Book room"}, {Before: "ship", After: "Ship"}}},
		{"Book the room, Send invites", []task{{"Book the room", "", ""}, {"Send invites", "", ""}}, []Chatcmd003Change{{Kind: "split", Before: "Book the room, Send invites", After: "2"}}},
		// A semicolon outranks a comma: the commas stay in their task.
		{"Call Lisbon, Porto; Book room", []task{{"Call Lisbon, Porto", "", ""}, {"Book room", "", ""}}, []Chatcmd003Change{{Kind: "split", Before: "Call Lisbon, Porto; Book room", After: "2"}}},
		{"Order pizza @Loretta Haynes; Book room; Omar sends invites", []task{{"Order pizza", "loretta", ""}, {"Book room", "", ""}, {"Sends invites", "omar", ""}},
			[]Chatcmd003Change{{Kind: "split", Before: "Order pizza @Loretta Haynes; Book room; Omar sends invites", After: "3"}, {Kind: "assignee", Before: "Order pizza", After: "Loretta Haynes"}, {Kind: "assignee", Before: "Sends invites", After: "Omar"}, {Before: "sends invites", After: "Sends invites"}}},
		{"Ship it by fri @Omar", []task{{"Ship it", "omar", "2026-10-02"}}, []Chatcmd003Change{{Kind: "assignee", Before: "Ship it", After: "Omar"}, {Kind: "due", Before: "Ship it", After: "2026-10-02"}}},
		{"9 oct: Budget review", []task{{"Budget review", "", "2026-10-09"}}, []Chatcmd003Change{{Kind: "due", Before: "Budget review", After: "2026-10-09"}}},
		{"Plan offsite Jan 5", []task{{"Plan offsite", "", "2027-01-05"}}, []Chatcmd003Change{{Kind: "due", Before: "Plan offsite", After: "2027-01-05"}}},
		{"Plan offsite 5. Oktober 2026", []task{{"Plan offsite", "", "2026-10-05"}}, []Chatcmd003Change{{Kind: "due", Before: "Plan offsite", After: "2026-10-05"}}},
		// Words that only look like dates are left in the task.
		{"Review the sat images", []task{{"Review the sat images", "", ""}}, nil},
		{"Check Friday's numbers", []task{{"Check Friday's numbers", "", ""}}, nil},
		{"Ship Feb 31", []task{{"Ship Feb 31", "", ""}}, nil},
		{"Work on 2 tickets", []task{{"Work on 2 tickets", "", ""}}, nil},
		{"Stand by the door", []task{{"Stand by the door", "", ""}}, nil},
		// Spacing alone is not a change: this used to be listed as "X → X".
		{"Order   pizza", []task{{"Order pizza", "", ""}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			parsed, err := Chatcmd004ParseTodo(tc.raw, members, "me", now, Chatcmd004ResolveDate)
			if err != nil {
				t.Fatal(err)
			}
			d := Chatcmd003Tidy(parsed)
			var got []task
			for _, item := range d.Card.Todo.Items {
				due := ""
				if item.DueAt != nil {
					due = item.DueAt.Format("2006-01-02")
				}
				got = append(got, task{item.Text, item.AssigneeID, due})
			}
			if !reflect.DeepEqual(got, tc.tasks) || !reflect.DeepEqual(d.Changes, tc.changes) || len(d.Issues) != 0 {
				t.Fatalf("tasks %+v\nchanges %+v\nissues %v", got, d.Changes, d.Issues)
			}
			for _, change := range d.Changes {
				if change.Kind == "" && strings.Join(strings.Fields(change.Before), " ") == change.After {
					t.Fatalf("a change with the same text on both sides: %+v", change)
				}
			}
		})
	}
}

func TestTodo_CHATCMD_004_Golden(t *testing.T) {
	t.Run("loose text and tidy without a model", chatcmd004TidyGolden)
	t.Run("split, dates and assignees of loose text", chatcmd004LooseGolden)
	members, now := chatcmd004Fixture()
	cases := []struct{ raw, text, assignee, due, issue string }{
		{`"L" 1="Ship"`, "Ship", "", "", ""},
		{`"L" 1="Ship @Omar"`, "Ship", "omar", "", ""},
		{`"L" 1="Omar sends invites"`, "sends invites", "omar", "", ""},
		{`"L" 1="I'll draft the agenda"`, "draft the agenda", "me", "", ""},
		{`"L" 1="I send invites"`, "send invites", "me", "", ""},
		{`"L" 1="Ship @Dana"`, "Ship @Dana", "", "", "assignee"},
		{`"L" 1="Ship @Dana Smith"`, "Ship", "dana-smith", "", ""},
		{`"L" 1="Ship @Dana Jones"`, "Ship", "dana-jones", "", ""},
		{`"L" 1="Ship by friday"`, "Ship", "", "2026-10-02", ""},
		{`"L" 1="Ship by 2026-10-05"`, "Ship", "", "2026-10-05", ""},
		{`"L" 1="Ship by 2026-09-01"`, "Ship", "", "2026-09-01", "past-date"},
		{`"L" 1="Ship by 03/04"`, "Ship by 03/04", "", "", "date"},
		{`"L" 1="Ship by tomorrow"`, "Ship", "", "2026-10-02", ""},
		{`"L" 1="Ship by today"`, "Ship", "", "2026-10-01", ""},
		{`"L" 1="Ship by monday"`, "Ship", "", "2026-10-05", ""},
		{`"L" 1="Ship by sunday"`, "Ship", "", "2026-10-04", ""},
		{`"L" 1="Ship @Omar by friday"`, "Ship", "omar", "2026-10-02", ""},
		{`"L" 1="Ship @Outside"`, "Ship @Outside", "", "", "assignee"},
		{`"L" 1="أرسل الدعوات"`, "أرسل الدعوات", "", "", ""},
		{`"L" 1="Say ""hello"" 🥳"`, `Say "hello" 🥳`, "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			d, err := Chatcmd004ParseTodo(tc.raw, members, "me", now, Chatcmd004ResolveDate)
			if err != nil || len(d.Card.Todo.Items) != 1 {
				t.Fatalf("%+v %v", d, err)
			}
			item := d.Card.Todo.Items[0]
			due := ""
			if item.DueAt != nil {
				due = item.DueAt.Format("2006-01-02")
			}
			if item.Text != tc.text || item.AssigneeID != tc.assignee || due != tc.due || strings.Join(d.Issues, ",") != tc.issue {
				t.Fatalf("item %+v issues %v", item, d.Issues)
			}
		})
	}
}
func TestTodo_CHATCMD_004_Property(t *testing.T) {
	t.Run("localized dates", func(t *testing.T) {
		members, now := chatcmd004Fixture()
		for _, raw := range []string{`"L" 1="ich sende bis freitag"`, `"L" 1="أنا أرسل بحلول الجمعة"`} {
			d, err := Chatcmd004ParseTodo(raw, members, "me", now, Chatcmd004ResolveDate)
			if err != nil || d.Card.Todo.Items[0].AssigneeID != "me" || d.Card.Todo.Items[0].DueAt == nil || d.Card.Todo.Items[0].DueAt.Weekday() != time.Friday {
				t.Fatalf("localized parsing %+v %v", d, err)
			}
		}
	})
	members, now := chatcmd004Fixture()
	for day := 0; day < 7; day++ {
		at := now.AddDate(0, 0, day)
		due, err := Chatcmd004ResolveDate("friday", at)
		if err != nil || due.Weekday() != time.Friday || !due.After(at) || due.Location() != at.Location() {
			t.Fatalf("due %v %v", due, err)
		}
	}
	d, err := Chatcmd004ParseTodo("1. Book the room\n2. Send invites", members, "me", now, Chatcmd004ResolveDate)
	if err != nil || d.Card.Todo.Items[0].Text != "Book the room" || d.Card.Todo.Items[1].Text != "Send invites" {
		t.Fatalf("lines %+v %v", d, err)
	}
	if _, err := Chatcmd004ParseTodo(strings.Repeat("Task\n", 31), members, "me", now, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("31 items accepted")
	}
}
func TestTodo_CHATCMD_004_Security(t *testing.T) {
	t.Run("service", chatcmd004ServiceSecurity)
	members, now := chatcmd004Fixture()
	d, err := Chatcmd004ParseTodo(`"L" 1="Give access @stranger"`, members, "me", now, Chatcmd004ResolveDate)
	if err != nil || d.Card.Todo.Items[0].AssigneeID != "" || len(d.Issues) != 1 {
		t.Fatalf("outsider assigned %+v %v", d, err)
	}
	due, err := Chatcmd004ResolveDate("2026-10-05T08:00:00+02:00", now)
	if err != nil || due.Hour() != 8 {
		t.Fatalf("RFC3339 %v %v", due, err)
	}
	if _, err := Chatcmd004ResolveDate("10/05", now); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("ambiguous date guessed")
	}
}
