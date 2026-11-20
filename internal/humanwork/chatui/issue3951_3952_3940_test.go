package chatui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func issue3951Preview(t *testing.T, locale, line string) (Model, localStore, string) {
	t.Helper()
	m := chatcmd003UIFixture()
	m.Locale, m.Chatcmd002TimeZone = locale, "UTC"
	m.Members = append(m.Members, Member{ID: "nia", Name: "Nia Brooks", HomeTenantID: "t"}, Member{ID: "rosa", Name: "Rosa Santos", HomeTenantID: "t"})
	m.Chatcmd002.Post = func(_, _ string, _ chat.Chatcmd002Card, done func(error)) { done(nil) }
	local := localStore{box: &localUI{}}
	chatcmd003Follow(m, local, "chat-composer", line)
	return m, local, renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
}

// TestIssue3951_TimedDeadlineShowsItsTimeAndZone: the preview chip, the list of
// changes and a posted card show the time and zone of a timed deadline and only
// the day for a date alone, in every product language.
func TestIssue3951_TimedDeadlineShowsItsTimeAndZone(t *testing.T) {
	timed := regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2} UTC`)
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, line := range []string{"/todo Review the plan due in an hour", "/todo Review the plan due 2026-11-13T12:30:00-05:00"} {
			_, _, markup := issue3951Preview(t, locale, line)
			if got := len(timed.FindAllString(markup, -1)); got < 2 {
				t.Fatalf("%s %q: time and zone shown %d times, want chip and change line:\n%s", locale, line, got, markup)
			}
			if !strings.Contains(markup, "2026-11-13 17:30 UTC") && strings.Contains(line, "2026") {
				t.Fatalf("%s: instant not shown in the viewer's zone:\n%s", locale, markup)
			}
		}
		_, _, markup := issue3951Preview(t, locale, "/todo Review the plan due 2027-03-13")
		if timed.MatchString(markup) || !strings.Contains(markup, "2027-03-13") {
			t.Fatalf("%s: a date alone must show only the day:\n%s", locale, markup)
		}
	}
	// A posted card reads the same.
	m := chatcmd003UIFixture()
	m.Chatcmd002TimeZone = "UTC"
	at := time.Date(2026, 11, 13, 17, 30, 0, 0, time.UTC)
	card := chat.Chatcmd002Card{Kind: "todo", Title: "List", Todo: &chat.Chatcmd002Todo{Tick: "anyone", Items: []chat.Chatcmd002Task{{ChannelTodoItem: chat.ChannelTodoItem{ID: "a", Text: "Timed"}, DueAt: &at, DueTimed: true}, {ChannelTodoItem: chat.ChannelTodoItem{ID: "b", Text: "Day"}, DueAt: &at}}}}
	markup := renderNode(t, Chatcmd002RenderCard(m, "post", chat.Chatcmd002View{Card: card}, false))
	if !strings.Contains(markup, "Due 2026-11-13 17:30 UTC") || !strings.Contains(markup, "Due 2026-11-13<") {
		t.Fatalf("posted card:\n%s", markup)
	}
}

// TestIssue3951_UnreadDateWarnedAfterDue: "due 03/04" warns like "by 03/04".
func TestIssue3951_UnreadDateWarnedAfterDue(t *testing.T) {
	for _, line := range []string{"/todo Submit timecard by 03/04", "/todo Submit timecard due 03/04", "/todo Submit payroll due tomorrow at 9am"} {
		_, _, markup := issue3951Preview(t, "en-US", line)
		if !strings.Contains(markup, "was not understood") || strings.Contains(markup, "chatcmd002-chip") && strings.Contains(markup, "Due ") {
			t.Fatalf("%q:\n%s", line, markup)
		}
	}
}

// TestIssue3952_SecondPersonIsNamedAsNotAssigned: the preview says who owns the
// task and that the other named person does not, and the author can pick the
// owner; the posted card carries the chosen one.
func TestIssue3952_SecondPersonIsNamedAsNotAssigned(t *testing.T) {
	for _, tc := range []struct{ locale, want string }{{"en-US", "is assigned to Nia Brooks. @Rosa Santos is named too"}, {"de-DE", "ist Nia Brooks zugewiesen. @Rosa Santos wird auch genannt"}, {"ar", "مسندة إلى Nia Brooks"}} {
		m, local, markup := issue3951Preview(t, tc.locale, "/todo @Nia Brooks @Rosa Santos review the plan")
		for _, want := range []string{tc.want, `role="radiogroup"`, `data-id="assign-0"`, `data-extra="rosa"`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s: preview lacks %q:\n%s", tc.locale, want, markup)
			}
		}
		if tc.locale != "en-US" {
			continue
		}
		var sent chat.Chatcmd002Card
		m.Chatcmd002.Post = func(_, _ string, c chat.Chatcmd002Card, done func(error)) { sent = c; done(nil) }
		if item := local.get().chatcmd003.Draft.Card.Todo.Items[0]; item.AssigneeID != "nia" {
			t.Fatalf("first mention is not the assignee: %+v", item)
		}
		chatcmd003Action(m, local, "chatcmd003-set", "assign-0", "rosa")
		state := local.get().chatcmd003
		if item := state.Draft.Card.Todo.Items[0]; item.AssigneeID != "rosa" || item.Text != "review the plan" {
			t.Fatalf("chosen owner: %+v", item)
		}
		markup = renderNode(t, chatcmd003PreviewView(m, state))
		if strings.Contains(markup, "assigned to Nia Brooks") || !strings.Contains(markup, "assigned to Rosa Santos") {
			t.Fatalf("two owners named after the choice:\n%s", markup)
		}
		chatcmd003Post(m, local)
		if sent.Todo == nil || sent.Todo.Items[0].AssigneeID != "rosa" {
			t.Fatalf("posted %+v", sent)
		}
	}
}

// TestIssue3940_ArabicIndicDigitDateShowsADueDate: in the Arabic composer a
// date written with Arabic-Indic digits is read as a due date.
func TestIssue3940_ArabicIndicDigitDateShowsADueDate(t *testing.T) {
	for _, line := range []string{"/todo مراجعة الملف 9 أكتوبر", "/todo مراجعة الملف ٩ أكتوبر"} {
		m, local, markup := issue3951Preview(t, "ar", line)
		item := local.get().chatcmd003.Draft.Card.Todo.Items[0]
		if item.DueAt == nil || item.DueAt.Month() != time.October || item.DueAt.Day() != 9 || item.Text != "مراجعة الملف" || strings.Contains(markup, "لم يُفهم") {
			t.Fatalf("%q: %+v\n%s", line, item, markup)
		}
		_ = m
	}
}
