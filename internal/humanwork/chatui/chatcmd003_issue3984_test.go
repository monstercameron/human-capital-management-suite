package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func issue3984Model() (Model, localStore, *chat.Chatcmd002Card) {
	m := chatcmd003UIFixture()
	m.Members = append(m.Members, Member{ID: "darius", Name: "Darius Bennett", HomeTenantID: "t"}, Member{ID: "maria", Name: "Maria Lopez", HomeTenantID: "t"})
	var sent chat.Chatcmd002Card
	m.Chatcmd002.Post = func(_, _ string, c chat.Chatcmd002Card, done func(error)) { sent = c; done(nil) }
	return m, localStore{box: &localUI{}}, &sent
}

// TestIssue3984_UnresolvedMentionIsFlaggedAsUnassigned: an @ name that fits no
// member names the unassigned task, offers members and a keep-as-text choice,
// guesses nobody, and the chosen owner is explicit in the posted card.
func TestIssue3984_UnresolvedMentionIsFlaggedAsUnassigned(t *testing.T) {
	for _, line := range []string{"/todo Review the budget @Dariuss by 2027-03-15", "/todo Review the budget @Nobody by 2027-03-15", "/todo Review plan @Da by 2027-03-15"} {
		m, local, _ := issue3984Model()
		chatcmd003Follow(m, local, "chat-composer", line)
		state := local.get().chatcmd003
		if item := state.Draft.Card.Todo.Items[0]; item.AssigneeID != "" {
			t.Fatalf("%q guessed an assignee: %+v", line, item)
		}
		markup := renderNode(t, chatcmd003PreviewView(m, state))
		token := line[strings.Index(line, "@"):strings.Index(line, " by")]
		for _, want := range []string{"“" + token + "” does not match anyone in this conversation", "has no owner yet", `role="radiogroup"`, `data-id="assign-0"`, "Keep " + token + " as text, no owner"} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%q: preview lacks %q:\n%s", line, want, markup)
			}
		}
	}
	m, local, sent := issue3984Model()
	chatcmd003Follow(m, local, "chat-composer", "/todo Review the budget @Dariuss by 2027-03-15")
	if markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003)); !strings.Contains(markup, `data-extra="darius"`) || strings.Contains(markup, `aria-checked="true"`+`" data-extra="darius"`) {
		t.Fatalf("Darius not offered: %s", markup)
	}
	chatcmd003Action(m, local, "chatcmd003-set", "assign-0", "darius")
	state := local.get().chatcmd003
	item := state.Draft.Card.Todo.Items[0]
	if item.AssigneeID != "darius" || item.AssigneeName != "Darius Bennett" || item.Text != "Review the budget" || item.DueAt == nil {
		t.Fatalf("chosen owner not applied: %+v", item)
	}
	markup := renderNode(t, chatcmd003PreviewView(m, state))
	if strings.Contains(markup, "has no owner yet") || !strings.Contains(markup, "assigned to Darius Bennett") {
		t.Fatalf("chosen owner not explicit:\n%s", markup)
	}
	// Keeping the word as text restores the unassigned state, and choosing again assigns.
	chatcmd003Action(m, local, "chatcmd003-set", "assign-0", "text")
	if item := local.get().chatcmd003.Draft.Card.Todo.Items[0]; item.AssigneeID != "" || !strings.Contains(item.Text, "@Dariuss") {
		t.Fatalf("keep as text: %+v", item)
	}
	chatcmd003Action(m, local, "chatcmd003-set", "assign-0", "darius")
	chatcmd003Post(m, local)
	if sent.Todo == nil || len(sent.Todo.Items) != 1 || sent.Todo.Items[0].AssigneeID != "darius" || sent.Todo.Items[0].Text != "Review the budget" {
		t.Fatalf("posted %+v", sent)
	}
}

// TestIssue3984_ValidMentionUnchanged: a member who resolves is still assigned
// at once with no warning and no picker.
func TestIssue3984_ValidMentionUnchanged(t *testing.T) {
	m, local, _ := issue3984Model()
	chatcmd003Follow(m, local, "chat-composer", "/todo Review the budget @Darius by 2027-03-15")
	state := local.get().chatcmd003
	markup := renderNode(t, chatcmd003PreviewView(m, state))
	if item := state.Draft.Card.Todo.Items[0]; item.AssigneeID != "darius" || strings.Contains(markup, "has no owner yet") || strings.Contains(markup, `data-id="assign-0"`) {
		t.Fatalf("%+v\n%s", item, markup)
	}
}

// TestIssue3984_Copy: every locale has the new words.
func TestIssue3984_Copy(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m, local, _ := issue3984Model()
		m.Locale = locale
		chatcmd003Follow(m, local, "chat-composer", "/todo Review @Nobody")
		markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
		if strings.Contains(markup, "{token}") || strings.Contains(markup, "{task}") || !strings.Contains(markup, `data-id="assign-0"`) {
			t.Fatalf("%s: %s", locale, markup)
		}
	}
}

// TestIssue3976_KeepAsOneTask: a comma-containing line previews as two tasks
// with a "Keep as one task" choice; choosing it restores one task, the posted
// card agrees, and the choice can be undone. "Use what I typed" is separate.
func TestIssue3976_KeepAsOneTask(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m, local, sent := issue3984Model()
		m.Locale = locale
		chatcmd003Follow(m, local, "chat-composer", "/todo Review wage, tax and benefit changes")
		state := local.get().chatcmd003
		markup := renderNode(t, chatcmd003PreviewView(m, state))
		if len(state.Draft.Card.Todo.Items) != 2 || !strings.Contains(markup, `data-id="split"`) || !strings.Contains(markup, `data-extra="one"`) {
			t.Fatalf("%s: no split choice: %s", locale, markup)
		}
		// "Use what I typed" alone does not merge the tasks.
		chatcmd003Action(m, local, "chatcmd003-original", "", "typed")
		if n := len(local.get().chatcmd003.Draft.Card.Todo.Items); n != 2 {
			t.Fatalf("%d", n)
		}
		chatcmd003Action(m, local, "chatcmd003-set", "split", "one")
		state = local.get().chatcmd003
		if items := state.Draft.Card.Todo.Items; len(items) != 1 || items[0].Text != "Review wage, tax and benefit changes" {
			t.Fatalf("%s: %+v", locale, items)
		}
		if markup = renderNode(t, chatcmd003PreviewView(m, state)); !strings.Contains(markup, `data-extra="auto"`) || strings.Contains(markup, `{`) && strings.Contains(markup, "{n}") {
			t.Fatalf("%s: cannot undo: %s", locale, markup)
		}
		if locale != "en-US" {
			continue
		}
		chatcmd003Post(m, local)
		if sent.Todo == nil || len(sent.Todo.Items) != 1 || sent.Todo.Items[0].Text != "Review wage, tax and benefit changes" {
			t.Fatalf("posted card disagrees with preview: %+v", sent)
		}
	}
	m, local, _ := issue3984Model()
	chatcmd003Follow(m, local, "chat-composer", "/todo Review wage, tax and benefit changes")
	chatcmd003Action(m, local, "chatcmd003-set", "split", "one")
	chatcmd003Action(m, local, "chatcmd003-set", "split", "auto")
	if n := len(local.get().chatcmd003.Draft.Card.Todo.Items); n != 2 {
		t.Fatalf("undo: %d", n)
	}
	// A number with a thousands comma is never split, so there is nothing to undo.
	chatcmd003Follow(m, local, "chat-composer", "/todo Review $5,000 training budget")
	state := local.get().chatcmd003
	if len(state.Draft.Card.Todo.Items) != 1 || state.Draft.Card.Todo.Items[0].Text != "Review $5,000 training budget" {
		t.Fatalf("%+v", state.Draft.Card.Todo.Items)
	}
}
