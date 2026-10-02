package chat

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// chatcmd003TidyGolden pins the loose form of /poll and the model-free tidy:
// what is typed, the question and options it becomes, whether the split had to
// guess, and whether the result can be posted.
func chatcmd003TidyGolden(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		raw, title string
		options    []string
		guessed    bool
		valid      bool
	}{
		{"where should we go lisbon, porto or stay remote?", "Where should we go?", []string{"Lisbon", "Porto", "Stay remote"}, true, true},
		{"where for lunch? tacos, pho or pizza", "Where for lunch?", []string{"Tacos", "Pho", "Pizza"}, false, true},
		{"Where for lunch? Tacos, pho, or pizza", "Where for lunch?", []string{"Tacos", "Pho", "Pizza"}, false, true},
		{"where for lunch? tacos, pho\npizza", "Where for lunch?", []string{"Tacos", "Pho", "Pizza"}, false, true},
		{"lunch spot: tacos, pho", "Lunch spot?", []string{"Tacos", "Pho"}, false, true},
		{"Offsite city\nLisbon\nPorto\nremote", "Offsite city?", []string{"Lisbon", "Porto", "Remote"}, false, true},
		{"which day?\n- monday\n- friday\n- Friday", "Which day?", []string{"Monday", "Friday"}, false, true},
		{"tacos, pho, pizza", "", []string{"Tacos", "Pho", "Pizza"}, true, false},
		{"just a question", "Just a question?", nil, true, false},
		{"meet at 10:30 or 11:00?", "Meet at?", []string{"10:30", "11:00"}, true, true},
		{"who presents? @Dana, @Omar", "Who presents?", []string{"@Dana", "@Omar"}, false, true},
		{"أين نذهب؟ لشبونة، بورتو", "أين نذهب؟", []string{"لشبونة", "بورتو"}, false, true},
		{"wohin? köln oder münchen", "Wohin?", []string{"Köln", "München"}, false, true},
		{"which phone? iPhone, pixel", "Which phone?", []string{"iPhone", "Pixel"}, false, true},
		{"pick one? a, b multiple=yes", "Pick one?", []string{"A", "B"}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			parsed, err := Chatcmd003ParsePoll(tc.raw, now, Chatcmd004ResolveDate)
			if err != nil {
				t.Fatal(err)
			}
			d := Chatcmd003Tidy(parsed)
			guessed := strings.Join(d.Issues, ",") == "separate-question"
			if d.Card.Title != tc.title || !reflect.DeepEqual(chatcmd003PollTexts(d.Card), tc.options) || guessed != tc.guessed || (d.Card.Validate() == nil) != tc.valid {
				t.Fatalf("title %q options %q issues %v valid %v", d.Card.Title, chatcmd003PollTexts(d.Card), d.Issues, d.Card.Validate() == nil)
			}
			if !reflect.DeepEqual(d.Original, parsed.Card) {
				t.Fatal("tidy changed the card as typed")
			}
		})
	}
	t.Run("changes are listed and explicit text keeps its words", func(t *testing.T) {
		parsed, _ := Chatcmd003ParsePoll(`"where   to" 1="lisbon" 2="Lisbon" 3="stay  remote"`, now, nil)
		d := Chatcmd003Tidy(parsed)
		want := []Chatcmd003Change{{Before: "where   to", After: "Where to?"}, {Before: "lisbon", After: "Lisbon"}, {Before: "Lisbon"}, {Before: "stay  remote", After: "Stay remote"}}
		if !reflect.DeepEqual(d.Changes, want) || len(d.Card.Poll.Options) != 2 {
			t.Fatalf("changes %+v card %+v", d.Changes, d.Card.Poll)
		}
		clean, _ := Chatcmd003ParsePoll(`"Where?" 1="Lisbon" 2="Porto"`, now, nil)
		if again := Chatcmd003Tidy(clean); len(again.Changes) != 0 || !reflect.DeepEqual(again.Card, clean.Card) {
			t.Fatalf("clean poll changed %+v", again.Changes)
		}
	})
}

// chatcmd004TidyGolden pins the loose form of /todo.
func chatcmd004TidyGolden(t *testing.T) {
	members, now := chatcmd004Fixture()
	members = []Chatcmd004Member{members[0], members[1], members[3]}
	parsed, err := Chatcmd004ParseTodo("book the room, dana sends invites by friday, I'll draft the agenda tick=assignee", members, "me", now, Chatcmd004ResolveDate)
	if err != nil {
		t.Fatal(err)
	}
	d := Chatcmd003Tidy(parsed)
	items := d.Card.Todo.Items
	if len(items) != 3 || d.Card.Todo.Tick != "assignee" || len(d.Issues) != 0 {
		t.Fatalf("items %+v issues %v", items, d.Issues)
	}
	if items[0].Text != "Book the room" || items[0].AssigneeID != "" {
		t.Fatalf("first %+v", items[0])
	}
	if items[1].Text != "Sends invites" || items[1].AssigneeID != "dana-smith" || items[1].DueAt == nil || items[1].DueAt.Weekday() != time.Friday {
		t.Fatalf("second %+v", items[1])
	}
	if items[2].Text != "Draft the agenda" || items[2].AssigneeID != "me" {
		t.Fatalf("third %+v", items[2])
	}
	lines, err := Chatcmd004ParseTodo("- say \"hello\" to the caterer\n- ship it", members, "me", now, Chatcmd004ResolveDate)
	if err != nil {
		t.Fatal(err)
	}
	if tidy := Chatcmd003Tidy(lines); len(tidy.Card.Todo.Items) != 2 || tidy.Card.Todo.Items[0].Text != `Say "hello" to the caterer` || tidy.Card.Todo.Items[1].Text != "Ship it" {
		t.Fatalf("lines %+v", tidy.Card.Todo.Items)
	}
}
