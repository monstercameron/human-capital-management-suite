//go:build !(js && wasm)

package chatui

import (
	"strings"
	"testing"
	"time"
)

func issue3871Model(locale string) Model {
	m := chat4Fixture(locale, "none", false)
	base := time.Now().Add(-72 * time.Hour)
	at := func(days int, minutes int) time.Time {
		return base.AddDate(0, 0, days).Add(time.Duration(minutes) * time.Minute)
	}
	m.Messages = []Message{
		{ID: "d1a", AuthorID: "bob", Author: "Bob", Body: "one", TimeLabel: "9:00", SentAt: at(0, 0)},
		{ID: "d1b", AuthorID: "cy", Author: "Cy", Body: "two", TimeLabel: "9:10", SentAt: at(0, 10)},
		{ID: "d2a", AuthorID: "bob", Author: "Bob", Body: "three", TimeLabel: "9:00", SentAt: at(1, 0)},
		{ID: "d3a", AuthorID: "cy", Author: "Cy", Body: "four", TimeLabel: "9:00", SentAt: at(2, 0)},
		{ID: "d3b", AuthorID: "bob", Author: "Bob", Body: "five", TimeLabel: "9:05", SentAt: at(2, 5)},
	}
	return m
}

// TestIssue3871_EachDayPillSticksOnlyInsideItsOwnDay: sticky siblings in one flat
// list all stick at the same offset and paint over each other. Each day's pill
// and messages share a wrapper, so a pill stops sticking where its day ends and
// the next pill takes its place.
func TestIssue3871_EachDayPillSticksOnlyInsideItsOwnDay(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup := render(t, issue3871Model(locale))
		parts := strings.Split(markup, `class="day-group"`)
		if len(parts) != 4 {
			t.Fatalf("%s: want 3 day groups, got %d", locale, len(parts)-1)
		}
		want := [][]string{{"d1a", "d1b"}, {"d2a"}, {"d3a", "d3b"}}
		for i, ids := range want {
			group := parts[i+1]
			if strings.Count(group, `class="day-divider"`) != 1 {
				t.Fatalf("%s: group %d must hold exactly one day pill", locale, i)
			}
			if got := strings.Count(group, `data-message-id="`); got != len(ids) {
				t.Fatalf("%s: group %d holds %d messages, want %d", locale, i, got, len(ids))
			}
			for _, id := range ids {
				if !strings.Contains(group, `data-message-id="`+id+`"`) {
					t.Fatalf("%s: group %d lacks its message %s", locale, i, id)
				}
			}
		}
		if got := strings.Count(markup, `class="day-divider"`); got != 3 {
			t.Fatalf("%s: want 3 day pills, got %d", locale, got)
		}
	}
}

// TestIssue3871_DayGroupStyles: the wrapper keeps the list's column layout and the
// pill stays sticky, below the header fade.
func TestIssue3871_DayGroupStyles(t *testing.T) {
	if got := chatbugCascadeValue(Stylesheet, ".chat-workspace .message-list .day-group", "flex"); got != "none" {
		t.Fatalf("a day group may shrink inside the list: flex=%q", got)
	}
	if got := chatbugCascadeValue(Stylesheet, ".chat-workspace .message-list .day-group", "display"); got != "flex" {
		t.Fatalf("a day group does not keep the list's column layout: display=%q", got)
	}
	if got := chatbugCascadeValue(Stylesheet, ".chat-workspace .message-list .day-divider", "position"); got != "sticky" {
		t.Fatalf("the day pill no longer sticks: %q", got)
	}
}
