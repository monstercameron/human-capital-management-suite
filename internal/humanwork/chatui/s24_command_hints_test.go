package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// The "/" list says what /poll and /todo take in the viewer's language, with
// the keywords the server reads in that language, not the English syntax.
func TestS24_CommandArgumentHintsAreLocalised(t *testing.T) {
	cases := []struct {
		locale string
		want   []string
		absent []string
	}{
		{"en-US", []string{">question? option, option or option<", ">task, task @Name by Friday, task<"}, nil},
		{"de-DE", []string{">Frage? Option, Option oder Option<", ">Aufgabe, Aufgabe @Name bis Freitag, Aufgabe<"}, []string{"question?", "by Friday"}},
		{"ar", []string{">سؤال؟ خيار، خيار أو خيار<", ">مهمة, مهمة @الاسم بحلول الجمعة, مهمة<"}, []string{"question?", "by Friday"}},
	}
	for _, c := range cases {
		m := composerToolsModel(c.locale, false)
		m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
		open := composerCommandMenuNext(m, composerCommandMenu{}, "chat-composer", "/", 1)
		markup := renderNode(t, composerCommandMenuView(m, open, "chat-composer", 1))
		for _, want := range c.want {
			if !strings.Contains(markup, want) {
				t.Errorf("%s: the list lacks %q: %s", c.locale, want, markup)
			}
		}
		for _, bad := range c.absent {
			if strings.Contains(markup, bad) {
				t.Errorf("%s: the list still shows the English %q", c.locale, bad)
			}
		}
	}
}

// Every keyword a localised hint teaches is one the shared parser reads, and
// the sample lines parse to the due date and the assignee they show.
func TestS24_LocalisedTodoExamplesAreUnderstoodByTheServerParser(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	members := []chat.Chatcmd004Member{{HomeTenantID: "tenant", ID: "dana", Name: "Dana"}}
	for _, locale := range []string{"de-DE", "ar"} {
		m := Model{Locale: locale}
		example := s24CommandExample(m, composerCommand{Example: `/todo "Launch checklist" 1="Book the room" 2="Send invites @Dana by Friday"`})
		if example == `/todo "Launch checklist" 1="Book the room" 2="Send invites @Dana by Friday"` {
			t.Fatalf("%s: the example is still English", locale)
		}
		line := chat.Chatcmd001ParseLine(example)
		if !line.Command || line.Unparsed {
			t.Fatalf("%s: example does not parse as a command: %q", locale, example)
		}
		draft, err := chat.Chatcmd004ParseTodo(line.Raw, members, "me", now, chat.Chatcmd004ResolveDate)
		if err != nil || draft.Card.Todo == nil || len(draft.Card.Todo.Items) < 2 {
			t.Fatalf("%s: the example read as %+v (%v)", locale, draft, err)
		}
		if item := draft.Card.Todo.Items[1]; item.DueAt == nil || item.AssigneeID != "dana" {
			t.Errorf("%s: the example's second task read as %+v, want Dana and a due date", locale, item)
		}
	}
}
