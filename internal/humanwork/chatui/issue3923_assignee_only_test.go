package chatui

import (
	"strings"
	"testing"
)

const issue3923Line = "/todo Confirm benefit renewal @Sofia Petrov tomorrow; Prepare employee guide Friday"

// TestIssue3923_AssigneeOnlyNeedsAnOwnerForEveryItem: with "The assignee"
// chosen, the preview says an item has no owner, Post is off, an owner can be
// picked from this conversation's members, or anyone may be allowed to complete
// items; the draft survives and the posted card matches.
func TestIssue3923_AssigneeOnlyNeedsAnOwnerForEveryItem(t *testing.T) {
	m, local, sent := issue3931Model()
	chatcmd003Follow(m, local, "chat-composer", issue3923Line)
	// Before the policy is chosen nothing is wrong.
	if markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003)); strings.Contains(markup, `data-id="pick-1"`) || strings.Contains(markup, "has no owner, so nobody") {
		t.Fatalf("warned with anyone-completes:\n%s", markup)
	}
	chatcmd003Action(m, local, "chatcmd003-set", "tick", "assignee")
	state := local.get().chatcmd003
	markup := renderNode(t, chatcmd003PreviewView(m, state))
	for _, want := range []string{"Prepare employee guide", "has no owner, so nobody could complete it", `data-id="pick-1"`, `data-extra="sofia"`, `id="chatcmd003-pick-anyone"`, "If that person leaves this conversation, nobody else can complete the item"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("lacks %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, `data-id="pick-0"`) {
		t.Fatalf("an item that has an owner is asked for one:\n%s", markup)
	}
	if !strings.Contains(markup, `id="chatcmd003-post"`) || !strings.Contains(markup, `disabled`) || chatcmd003Problem(state.Draft.Card) != "assignee-needed" {
		t.Fatalf("Post must be off: problem %q\n%s", chatcmd003Problem(state.Draft.Card), markup)
	}
	chatcmd003Post(m, local)
	if sent.Todo != nil {
		t.Fatalf("posted a list nobody could finish: %+v", sent)
	}
	if got := state.Draft.Raw; got != strings.TrimPrefix(issue3923Line, "/todo ") {
		t.Fatalf("draft lost: %q", got)
	}
	// Picking an owner clears it.
	chatcmd003Action(m, local, "chatcmd003-set", "pick-1", "sofia")
	state = local.get().chatcmd003
	if item := state.Draft.Card.Todo.Items[1]; item.AssigneeID != "sofia" || item.Text != "Prepare employee guide" || item.DueAt == nil {
		t.Fatalf("picked: %+v", item)
	}
	if chatcmd003Problem(state.Draft.Card) != "" {
		t.Fatal("still blocked")
	}
	// The choice stays on screen, marked, so it can be changed and focus has
	// somewhere to stay; the "no owner" note is gone.
	markup = renderNode(t, chatcmd003PreviewView(m, state))
	if !strings.Contains(markup, `id="chatcmd003-pick-1-sofia"`) || strings.Contains(markup, "has no owner, so nobody") {
		t.Fatalf("pick vanished or note stayed:\n%s", markup)
	}
	if !strings.Contains(markup, `aria-checked="true"`) {
		t.Fatalf("pick not marked:\n%s", markup)
	}
	chatcmd003Post(m, local)
	if sent.Todo == nil || sent.Todo.Tick != "assignee" || sent.Todo.Items[1].AssigneeID != "sofia" || sent.Todo.Items[0].AssigneeID != "sofia" {
		t.Fatalf("posted %+v", sent)
	}
}

// TestIssue3923_AnyoneIsAnExplicitAlternative: the alternative is a button the
// author presses, and it turns the policy to anyone and lets the list post.
func TestIssue3923_AnyoneIsAnExplicitAlternative(t *testing.T) {
	m, local, sent := issue3931Model()
	chatcmd003Follow(m, local, "chat-composer", issue3923Line)
	chatcmd003Action(m, local, "chatcmd003-set", "tick", "assignee")
	chatcmd003Action(m, local, "chatcmd003-set", "tick", "anyone")
	state := local.get().chatcmd003
	if state.Draft.Card.Todo.Tick != "anyone" || chatcmd003Problem(state.Draft.Card) != "" {
		t.Fatalf("%+v", state.Draft.Card.Todo)
	}
	chatcmd003Post(m, local)
	if sent.Todo == nil || sent.Todo.Items[1].AssigneeID != "" {
		t.Fatalf("posted %+v", sent)
	}
}

// TestIssue3923_NonMemberNameIsTheSameState: a name typed for someone who is
// not a member leaves the task unassigned, so under the assignee policy the
// preview blocks and asks, and picking a member is the only way an owner is set
// (no one is added to the conversation).
func TestIssue3923_NonMemberNameIsTheSameState(t *testing.T) {
	m, local, _ := issue3931Model()
	chatcmd003Follow(m, local, "chat-composer", "/todo Review budget @Evelyn Morgan by 2027-03-15")
	chatcmd003Action(m, local, "chatcmd003-set", "tick", "assignee")
	state := local.get().chatcmd003
	if chatcmd003Problem(state.Draft.Card) != "assignee-needed" {
		t.Fatalf("problem %q", chatcmd003Problem(state.Draft.Card))
	}
	markup := renderNode(t, chatcmd003PreviewView(m, state))
	if !strings.Contains(markup, `data-id="pick-0"`) {
		t.Fatalf("no owner choice:\n%s", markup)
	}
	if len(m.Members) != 3 {
		t.Fatalf("members changed: %+v", m.Members)
	}
}

// TestIssue3923_Copy: every locale words the warning with nothing unfilled.
func TestIssue3923_Copy(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m, local, _ := issue3931Model()
		m.Locale = locale
		chatcmd003Follow(m, local, "chat-composer", issue3923Line)
		chatcmd003Action(m, local, "chatcmd003-set", "tick", "assignee")
		markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
		if strings.Contains(markup, "{") || !strings.Contains(markup, `data-id="pick-1"`) || !strings.Contains(markup, `id="chatcmd003-pick-anyone"`) {
			t.Fatalf("%s: %s", locale, markup)
		}
		if locale != "en-US" && strings.Contains(markup, "could complete it") {
			t.Fatalf("%s: English left in: %s", locale, markup)
		}
	}
}
