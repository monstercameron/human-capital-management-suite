package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func issue3931Model() (Model, localStore, *chat.Chatcmd002Card) {
	m := chatcmd003UIFixture()
	m.Members = append(m.Members, Member{ID: "sofia", Name: "Sofia Petrov", HomeTenantID: "t"})
	var sent chat.Chatcmd002Card
	m.Chatcmd002.Post = func(_, _ string, c chat.Chatcmd002Card, done func(error)) { sent = c; done(nil) }
	return m, localStore{box: &localUI{}}, &sent
}

// TestIssue3931_ClearInferredDeadline: the preview offers a choice for a date it
// read, "No deadline" puts the words back, preview and posted card agree, and
// the choice can be undone.
func TestIssue3931_ClearInferredDeadline(t *testing.T) {
	m, local, sent := issue3931Model()
	chatcmd003Follow(m, local, "chat-composer", "/todo Discuss the schedule for Friday")
	state := local.get().chatcmd003
	if item := state.Draft.Card.Todo.Items[0]; item.Text != "Discuss the schedule for" || item.DueAt == nil {
		t.Fatalf("default reading: %+v", item)
	}
	markup := renderNode(t, chatcmd003PreviewView(m, state))
	for _, want := range []string{`role="radiogroup"`, `data-id="due-0"`, `data-extra="none"`, "Deadline for “Discuss the schedule for Friday”", "read from “Friday”", "No deadline, keep “Friday” in the task"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("preview lacks %q:\n%s", want, markup)
		}
	}
	chatcmd003Action(m, local, "chatcmd003-set", "due-0", "none")
	state = local.get().chatcmd003
	if item := state.Draft.Card.Todo.Items[0]; item.Text != "Discuss the schedule for Friday" || item.DueAt != nil {
		t.Fatalf("not cleared: %+v", item)
	}
	markup = renderNode(t, chatcmd003PreviewView(m, state))
	if !strings.Contains(markup, `data-id="due-0"`) || !strings.Contains(markup, "read from “Friday”") {
		t.Fatalf("cannot undo:\n%s", markup)
	}
	chatcmd003Post(m, local)
	if sent.Todo == nil || sent.Todo.Items[0].Text != "Discuss the schedule for Friday" || sent.Todo.Items[0].DueAt != nil {
		t.Fatalf("posted card disagrees with the preview: %+v", sent)
	}
	// Undo, on a fresh preview of the same text.
	m, local, sent = issue3931Model()
	chatcmd003Follow(m, local, "chat-composer", "/todo Discuss the schedule for Friday")
	chatcmd003Action(m, local, "chatcmd003-set", "due-0", "none")
	chatcmd003Action(m, local, "chatcmd003-set", "due-0", "infer")
	if item := local.get().chatcmd003.Draft.Card.Todo.Items[0]; item.DueAt == nil || item.Text != "Discuss the schedule for" {
		t.Fatalf("undo: %+v", item)
	}
}

// TestIssue3931_QualifiedWeekdayAsksTheAuthor: "next Friday" keeps its words and
// has no deadline until the author picks one of the two readings.
func TestIssue3931_QualifiedWeekdayAsksTheAuthor(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m, local, sent := issue3931Model()
		m.Locale = locale
		chatcmd003Follow(m, local, "chat-composer", "/todo Submit timecard by next Friday")
		state := local.get().chatcmd003
		item := state.Draft.Card.Todo.Items[0]
		if item.Text != "Submit timecard by next Friday" || item.DueAt != nil {
			t.Fatalf("%s: %+v", locale, item)
		}
		markup := renderNode(t, chatcmd003PreviewView(m, state))
		for _, want := range []string{`data-id="due-0"`, `data-extra="a"`, `data-extra="b"`, `data-extra="none"`, `role="status"`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s: lacks %q:\n%s", locale, want, markup)
			}
		}
		if strings.Contains(markup, "{") {
			t.Fatalf("%s: unfilled copy: %s", locale, markup)
		}
		chatcmd003Action(m, local, "chatcmd003-set", "due-0", "b")
		item = local.get().chatcmd003.Draft.Card.Todo.Items[0]
		if item.Text != "Submit timecard" || item.DueAt == nil {
			t.Fatalf("%s: chosen: %+v", locale, item)
		}
		if locale == "en-US" {
			chatcmd003Post(m, local)
			if sent.Todo == nil || sent.Todo.Items[0].Text != "Submit timecard" || sent.Todo.Items[0].DueAt == nil {
				t.Fatalf("posted %+v", sent)
			}
		}
	}
}

// TestIssue3931_InferredOwnerCanBeKeptAsText: a name read as the owner has its
// own choice, and keeping it as text leaves the task without an owner.
func TestIssue3931_InferredOwnerCanBeKeptAsText(t *testing.T) {
	m, local, sent := issue3931Model()
	chatcmd003Follow(m, local, "chat-composer", "/todo Confirm benefit renewal @Sofia Petrov")
	state := local.get().chatcmd003
	if state.Draft.Card.Todo.Items[0].AssigneeID != "sofia" {
		t.Fatalf("default changed: %+v", state.Draft.Card.Todo.Items[0])
	}
	markup := renderNode(t, chatcmd003PreviewView(m, state))
	for _, want := range []string{`data-id="owner-0"`, "Assigned to Sofia Petrov", "No owner, keep the name in the task"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("lacks %q:\n%s", want, markup)
		}
	}
	chatcmd003Action(m, local, "chatcmd003-set", "owner-0", "none")
	item := local.get().chatcmd003.Draft.Card.Todo.Items[0]
	if item.AssigneeID != "" || item.Text != "Confirm benefit renewal @Sofia Petrov" {
		t.Fatalf("kept as text: %+v", item)
	}
	chatcmd003Post(m, local)
	if sent.Todo == nil || sent.Todo.Items[0].AssigneeID != "" {
		t.Fatalf("posted %+v", sent)
	}
}

// TestIssue3931_NoChoiceWithoutReading: plain tasks get no due or owner choice.
func TestIssue3931_NoChoiceWithoutReading(t *testing.T) {
	m, local, _ := issue3931Model()
	chatcmd003Follow(m, local, "chat-composer", "/todo Book the room")
	if markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003)); strings.Contains(markup, `data-id="due-`) || strings.Contains(markup, `data-id="owner-`) {
		t.Fatalf("choice shown with nothing read:\n%s", markup)
	}
}

// TestIssue3931_Copy: every locale words the choices with nothing unfilled.
func TestIssue3931_Copy(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m, local, _ := issue3931Model()
		m.Locale = locale
		chatcmd003Follow(m, local, "chat-composer", "/todo Confirm renewal @Sofia Petrov by Friday")
		markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
		if strings.Contains(markup, "{") || !strings.Contains(markup, `data-id="due-0"`) || !strings.Contains(markup, `data-id="owner-0"`) {
			t.Fatalf("%s: %s", locale, markup)
		}
	}
}
