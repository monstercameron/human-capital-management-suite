package chatui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// #3991: the preview's radio groups are one Tab stop at the chosen option and
// the arrow keys move the choice and the focus inside the group.

func issue3991Preview(t *testing.T, locale, line string) (Model, localStore) {
	t.Helper()
	m := chatcmd003UIFixture()
	m.Locale = locale
	m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
	local := localStore{box: &localUI{}}
	if !chatcmd003Follow(m, local, "chat-composer", line) {
		t.Fatal("the line did not open a preview")
	}
	return m, local
}

func TestIssue3991_RadioGroupsAreOneTabStopAtTheChosenOption(t *testing.T) {
	m, local := issue3991Preview(t, "en-US", "/poll Which time works? Morning or Afternoon")
	chatcmd003Action(m, local, "chatcmd003-set", "closes", "week")
	markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
	button := regexp.MustCompile(`<button[^>]*role="radio"[^>]*>`)
	var tabbable, skipped int
	for _, tag := range button.FindAllString(markup, -1) {
		switch {
		case strings.Contains(tag, `tabIndex="-1"`):
			skipped++
			if strings.Contains(tag, `aria-checked="true"`) {
				t.Fatalf("the chosen radio is out of the Tab order: %s", tag)
			}
		default:
			tabbable++
			if !strings.Contains(tag, `aria-checked="true"`) {
				t.Fatalf("an unchosen radio is a Tab stop: %s", tag)
			}
		}
	}
	// Show results, Closes and Who can add options: one Tab stop each, every
	// other option skipped.
	// "Pick date and time" (#3927) is one more option in the Closes group.
	if tabbable != 3 || skipped != 8 {
		t.Fatalf("tab stops %d skipped %d in %s", tabbable, skipped, markup)
	}
	if !strings.Contains(markup, `id="chatcmd003-closes-week"`) {
		t.Fatalf("radios carry no id to return focus to: %s", markup)
	}
}

func TestIssue3991_TodoPolicyGroupSharesTheComponent(t *testing.T) {
	m, local := issue3991Preview(t, "en-US", "/todo Prepare employee guide by tomorrow")
	markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
	// The deadline choice of #3931 is a group of its own; this counts the policy group.
	markup = markup[strings.Index(markup, `id="chatcmd003-tick"`):]
	if got := strings.Count(markup, `role="radio"`); got != 3 || strings.Count(markup, `tabIndex="-1"`) != 2 {
		t.Fatalf("to-do policy group: %d radios, %d skipped: %s", got, strings.Count(markup, `tabIndex="-1"`), markup)
	}
	focus, ok := chatcmd003RadioKey(m, local, "ArrowRight", "tick", "anyone", []string{"anyone", "assignee", "author"})
	if !ok || focus != "chatcmd003-tick-assignee" || local.get().chatcmd003.Draft.Card.Todo.Tick != "assignee" {
		t.Fatalf("focus %q ok %v tick %q", focus, ok, local.get().chatcmd003.Draft.Card.Todo.Tick)
	}
}

func TestIssue3991_ArrowKeysMoveSelectionAndFocus(t *testing.T) {
	results := []string{"always", "after-voting", "after-closing"}
	cases := []struct {
		locale, key, from, want string
	}{
		{"en-US", "ArrowRight", "always", "after-voting"},
		{"en-US", "ArrowDown", "always", "after-voting"},
		{"en-US", "ArrowLeft", "always", "after-closing"},
		{"en-US", "ArrowUp", "after-voting", "always"},
		{"en-US", "ArrowRight", "after-closing", "always"},
		{"en-US", "Home", "after-closing", "always"},
		{"en-US", "End", "always", "after-closing"},
		{"de-DE", "ArrowRight", "always", "after-voting"},
		// Right to left: the options run from the right, so Right goes back.
		{"ar", "ArrowRight", "after-voting", "always"},
		{"ar", "ArrowLeft", "after-voting", "after-closing"},
		{"ar", "ArrowDown", "always", "after-voting"},
	}
	for _, c := range cases {
		m, local := issue3991Preview(t, c.locale, "/poll Which time works? Morning or Afternoon")
		chatcmd003Action(m, local, "chatcmd003-set", "results", c.from)
		focus, ok := chatcmd003RadioKey(m, local, c.key, "results", c.from, results)
		if got := local.get().chatcmd003.Draft.Card.Poll.Results; !ok || got != c.want || focus != "chatcmd003-results-"+c.want {
			t.Errorf("%s %s from %s: selected %q focus %q ok %v, want %s", c.locale, c.key, c.from, got, focus, ok, c.want)
		}
	}
}

func TestIssue3991_OtherKeysAreLeftAlone(t *testing.T) {
	m, local := issue3991Preview(t, "en-US", "/poll Which time works? Morning or Afternoon")
	for _, key := range []string{" ", "Enter", "Tab", "a", "Escape"} {
		if focus, ok := chatcmd003RadioKey(m, local, key, "results", "always", []string{"always", "after-voting", "after-closing"}); ok || focus != "" {
			t.Errorf("%q was taken by the radio group", key)
		}
	}
	if got := local.get().chatcmd003.Draft.Card.Poll.Results; got != "always" {
		t.Fatalf("results changed to %q", got)
	}
}
