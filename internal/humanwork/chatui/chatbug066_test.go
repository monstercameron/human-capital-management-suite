package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	xhtml "golang.org/x/net/html"
)

// chatbug066Buttons renders the answered card under the fixture's question and
// returns its Helpful and Not right buttons.
func chatbug066Buttons(t *testing.T, m Model, local localUI) (helpful, notRight *xhtml.Node) {
	t.Helper()
	markup := chatPolishMarkup(t, html.Div(html.Props{}, personaReplyRowsForPost(m, local, "question", time.Now())...), 1440, "light")
	buttons := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
		return n.Data == "button" && strings.HasPrefix(chatPolishAttr(n, "data-action"), "agent-feedback") && chatPolishAttr(n, "data-extra") != ""
	})
	if len(buttons) != 2 {
		t.Fatalf("%d rating buttons on the answered card", len(buttons))
	}
	return buttons[0], buttons[1]
}

// The answer card is drawn with the reader's own stored rating from the first
// paint after a load, and pressing the filled rating again removes it.
func TestTodo_CHATBUG_066(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		// After a reload nothing was clicked on this page: only the stored rating.
		m := chat4Fixture(locale, "answered", false)
		m.PersonaActivityReady = true
		helpful, notRight := chatbug066Buttons(t, m, localUI{})
		if chatPolishAttr(helpful, "aria-pressed") != "false" || chatPolishAttr(notRight, "aria-pressed") != "false" {
			t.Fatalf("%s: an unrated answer shows a rating", locale)
		}
		m.AgentFeedbackSaved = map[string]string{"run": AgentFeedbackHelpful}
		helpful, notRight = chatbug066Buttons(t, m, localUI{})
		if chatPolishAttr(helpful, "aria-pressed") != "true" || chatPolishAttr(notRight, "aria-pressed") != "false" {
			t.Fatalf("%s: the stored Helpful is not filled after a load", locale)
		}
		// Pressing the filled one removes the rating; the other one changes it.
		if chatPolishAttr(helpful, "data-action") != "agent-feedback-undo" || chatPolishAttr(helpful, "data-id") != "run" {
			t.Fatalf("%s: pressing the filled Helpful does not remove the rating: %q", locale, chatPolishAttr(helpful, "data-action"))
		}
		if chatPolishAttr(notRight, "data-action") != "agent-feedback" {
			t.Fatalf("%s: the other rating no longer rates", locale)
		}
		m.AgentFeedbackSaved = map[string]string{"run": AgentFeedbackNotRight}
		helpful, notRight = chatbug066Buttons(t, m, localUI{})
		if chatPolishAttr(helpful, "aria-pressed") != "false" || chatPolishAttr(notRight, "aria-pressed") != "true" || chatPolishAttr(notRight, "data-action") != "agent-feedback-undo" {
			t.Fatalf("%s: the stored Not right is not filled after a load", locale)
		}
	}

	// What the person chooses on this page shows over the stored rating, and a
	// change the server refused shows what the server still holds.
	m := chat4Fixture("en-US", "answered", false)
	m.AgentFeedbackSaved = map[string]string{"run": AgentFeedbackHelpful}
	if got := agentFeedbackState(m, localUI{agentFeedback: map[string]string{"run": AgentFeedbackNotRight}}, "run"); got != AgentFeedbackNotRight {
		t.Fatalf("a rating just chosen shows %q", got)
	}
	m.AgentFeedbackRestored = map[string]string{"run": AgentFeedbackHelpful}
	if got := agentFeedbackState(m, localUI{agentFeedback: map[string]string{"run": AgentFeedbackNotRight}}, "run"); got != AgentFeedbackHelpful {
		t.Fatalf("a refused change shows %q, want what the server holds", got)
	}
	m.AgentFeedbackRestored = map[string]string{"run": ""}
	if got := agentFeedbackState(m, localUI{}, "run"); got != "" {
		t.Fatalf("a refused first rating shows %q, want none", got)
	}
	// Without a way to remove a rating the filled button rates again and removes nothing.
	m = chat4Fixture("en-US", "answered", false)
	m.AgentFeedbackSaved = map[string]string{"run": AgentFeedbackHelpful}
	m.Callbacks.UndoAgentFeedback = nil
	if helpful, _ := chatbug066Buttons(t, m, localUI{}); chatPolishAttr(helpful, "data-action") != "agent-feedback" {
		t.Fatal("a page that cannot remove a rating offers to remove it")
	}
}

// The stored rating is read by assistive technology as a pressed button with
// its own name, in a named group.
func TestTodo_CHATBUG_066_Browser(t *testing.T) {
	m := chat4Fixture("en-US", "answered", false)
	m.AgentFeedbackSaved = map[string]string{"run": AgentFeedbackHelpful}
	helpful, notRight := chatbug066Buttons(t, m, localUI{})
	if chatPolishAttr(helpful, "aria-label") != "Helpful" || chatPolishAttr(notRight, "aria-label") != "Not right" {
		t.Fatalf("rating buttons are named %q and %q", chatPolishAttr(helpful, "aria-label"), chatPolishAttr(notRight, "aria-label"))
	}
	if group := helpful.Parent; group == nil || chatPolishAttr(group, "role") != "group" || !strings.Contains(chatPolishAttr(group, "aria-label"), "Rate this answer") {
		t.Fatal("the rating buttons are not in a named group")
	}
}
