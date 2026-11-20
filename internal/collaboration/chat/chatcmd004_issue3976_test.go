package chat

import (
	"reflect"
	"testing"
	"time"
)

func issue3976Texts(t *testing.T, raw string, literal bool) []string {
	t.Helper()
	now := time.Date(2027, 3, 1, 9, 0, 0, 0, time.UTC)
	parse := Chatcmd004ParseTodo
	if literal {
		parse = Chatcmd004ParseTodoLiteral
	}
	d, err := parse(raw, nil, "me", now, Chatcmd004ResolveDate)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, item := range d.Card.Todo.Items {
		out = append(out, item.Text)
	}
	return out
}

func TestIssue3976_CommaSplitAndLiteral(t *testing.T) {
	cases := []struct {
		raw     string
		literal bool
		want    []string
	}{
		{"Review wage, tax and benefit changes", false, []string{"Review wage", "tax and benefit changes"}},
		{"Review wage, tax and benefit changes", true, []string{"Review wage, tax and benefit changes"}},
		{`"Review wage, tax and benefit changes"`, false, []string{"Review wage, tax and benefit changes"}},
		{`"Review wage, tax and benefit changes"`, true, []string{"Review wage, tax and benefit changes"}},
		{"Review $5,000 training budget", false, []string{"Review $5,000 training budget"}},
		{"Review 1,250,000 rows, send report", false, []string{"Review 1,250,000 rows", "send report"}},
		{"Review 5, 000 rows", false, []string{"Review 5", "000 rows"}},
		{"Submit timesheets by 2027-03-12, Review budget by 2027-03-15", false, []string{"Submit timesheets", "Review budget"}},
		{"Book room; Send invites, now", true, []string{"Book room; Send invites, now"}},
		// Several typed lines: each line is a task; Keep as one task stops the
		// split inside a line too, so the choice does something here as well.
		{"Book room; Send invites\nReview wage, tax", false, []string{"Book room", "Send invites", "Review wage, tax"}},
		{"Book room; Send invites\nReview wage, tax", true, []string{"Book room; Send invites", "Review wage, tax"}},
	}
	for _, c := range cases {
		if got := issue3976Texts(t, c.raw, c.literal); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q literal=%v: got %q want %q", c.raw, c.literal, got, c.want)
		}
	}
	d, _ := Chatcmd004ParseTodo("Submit timesheets by 2027-03-12, Review budget by 2027-03-15", nil, "me", time.Date(2027, 3, 1, 9, 0, 0, 0, time.UTC), Chatcmd004ResolveDate)
	if len(d.Card.Todo.Items) != 2 || d.Card.Todo.Items[0].DueAt == nil || d.Card.Todo.Items[1].DueAt == nil || d.Card.Todo.Items[0].DueAt.Day() != 12 || d.Card.Todo.Items[1].DueAt.Day() != 15 {
		t.Fatalf("intentional list lost its dates: %+v", d.Card.Todo.Items)
	}
}

func TestIssue3976_PollOptionBoundaries(t *testing.T) {
	now := time.Date(2027, 3, 1, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		raw   string
		title string
		want  []string
	}{
		{"Where should we meet? Boston, MA or Portland, ME", "Where should we meet?", []string{"Boston, MA", "Portland, ME"}},
		{`Where should we meet? "Boston, MA" "Portland, ME"`, "Where should we meet?", nil},
		{`Where should we meet? "Boston, MA", "Portland, ME"`, "Where should we meet?", []string{"Boston, MA", "Portland, ME"}},
		{"Lunch? Tacos, pho or pizza", "Lunch?", []string{"Tacos", "pho", "pizza"}},
		{"Budget? $5,000, $7,500", "Budget?", []string{"$5,000", "$7,500"}},
		{"Where? Austin, TX, Boston, MA", "Where?", []string{"Austin, TX", "Boston, MA"}},
	}
	for _, c := range cases {
		d, err := Chatcmd003ParsePoll(c.raw, now, Chatcmd004ResolveDate)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, o := range d.Card.Poll.Options {
			got = append(got, o.Text)
		}
		if c.want != nil && (d.Card.Title != c.title || !reflect.DeepEqual(got, c.want)) {
			t.Errorf("%q: title %q options %q, want %q", c.raw, d.Card.Title, got, c.want)
		}
	}
	// The explicit syntax is unchanged.
	d, _ := Chatcmd003ParsePoll(`"Where should we meet?" 1="Boston, MA" 2="Portland, ME"`, now, Chatcmd004ResolveDate)
	if len(d.Card.Poll.Options) != 2 || d.Card.Poll.Options[0].Text != "Boston, MA" {
		t.Fatalf("%+v", d.Card.Poll.Options)
	}
}
