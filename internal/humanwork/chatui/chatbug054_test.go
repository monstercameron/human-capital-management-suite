package chatui

import (
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"
)

// chatbug054Card renders the one row under the fixture's question and returns
// the failed card in it.
func chatbug054Card(t *testing.T, m Model) *xhtml.Node {
	t.Helper()
	return chatbug035Failure(t, m)
}

func chatbug054Children(n *xhtml.Node) []*xhtml.Node {
	var out []*xhtml.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode {
			out = append(out, c)
		}
	}
	return out
}

// A failed card in a channel is laid out like an answered one and always ends
// with "Ask again" and "Dismiss" for the person who asked, whatever the age of
// the question and whatever ended the answer.
func TestTodo_CHATBUG_054(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, code := range []string{"ANSWER_INTERRUPTED", "MODEL_UNAVAILABLE", "ADMISSION_REFUSED", "OUTPUT_REJECTED", "CANCELLED", "EXPIRED", "DAILY_LIMIT_REACHED"} {
			for _, age := range []time.Duration{time.Minute, 26 * time.Hour} {
				m := chat4Fixture(locale, "failed", false)
				m.Messages[0].SentAt = time.Now().Add(-age)
				m.PersonaInvocations[0].Projection.Failure = &PersonaProgressFailure{InvocationID: "run", InvokerID: "alice", Code: code}
				card := chatbug054Card(t, m)
				parts := chatbug054Children(card)
				if len(parts) < 3 || parts[0].Data != "header" || !chatPolishHasClass(parts[0], "agent-reply-head") {
					t.Fatalf("%s %s: the card does not start with the header line", locale, code)
				}
				// The header line: icon, name, badge, time, and the visibility note last.
				head := chatbug054Children(parts[0])
				if last := head[len(head)-1]; !chatPolishHasClass(last, "agent-reply-private") {
					t.Fatalf("%s %s: the header line does not end with the visibility note", locale, code)
				}
				if !strings.Contains(chatbug030Text(parts[0]), "Policy Helper") || !strings.Contains(chatbug030Text(parts[0]), "9:30") {
					t.Fatalf("%s %s: header line reads %q", locale, code, chatbug030Text(parts[0]))
				}
				// No line of its own above the agent, and the reason is the second part.
				if !chatPolishHasClass(parts[1], "agent-failure-heading") || chatPolishAttr(parts[1], "role") != "status" {
					t.Fatalf("%s %s: the reason does not follow the header", locale, code)
				}
				// The card ends with its one action row: Ask again first, then Dismiss.
				row := parts[len(parts)-1]
				if !chatPolishHasClass(row, "agent-reply-actions") {
					t.Fatalf("%s %s %v: the card does not end with an action row", locale, code, age)
				}
				buttons := chatPolishNodesIn(row, func(n *xhtml.Node) bool { return n.Data == "button" })
				if len(buttons) < 2 || chatPolishAttr(buttons[0], "data-agent-action") != "retry" || chatPolishAttr(buttons[0], "data-agent-invocation-id") != "run" || chatPolishAttr(buttons[0], "data-agent-question") != "question" {
					t.Fatalf("%s %s %v: the first action is not Ask again for this run and question", locale, code, age)
				}
				if chatPolishAttr(buttons[1], "data-agent-action") != "dismiss" || chatPolishAttr(buttons[1], "data-agent-invocation-id") != "run" {
					t.Fatalf("%s %s %v: the second action is not Dismiss for this card", locale, code, age)
				}
				if chatPolishHasClass(buttons[1], "persona-progress-retry") || !chatPolishHasClass(buttons[0], "persona-progress-retry") {
					t.Fatalf("%s %s: Ask again is not the one primary action", locale, code)
				}
			}
		}
	}

	// English copy, as read: one sentence for the reason, and the two actions.
	m := chat4Fixture("en-US", "failed", false)
	m.PersonaInvocations[0].Projection.Failure = &PersonaProgressFailure{InvocationID: "run", InvokerID: "alice", Code: "ANSWER_INTERRUPTED"}
	card := chatbug054Card(t, m)
	text := chatbug030Text(card)
	for _, want := range []string{"Policy Helper's answer was interrupted.", "Ask again", "Dismiss", "Only visible to you"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the failed card does not read %q: %q", want, text)
		}
	}
	if strings.Contains(text, "Try again") {
		t.Fatalf("the reason is more than one sentence: %q", text)
	}
	// A failure asking again cannot mend says what would, in a second, muted line.
	m.PersonaInvocations[0].Projection.Failure.Code = "ADMISSION_REFUSED"
	refused := chatbug054Card(t, m)
	if next := chatPolishNodesIn(refused, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-failure-next") }); len(next) != 1 || !strings.Contains(chatbug030Text(next[0]), "Ask about a document it can read") {
		t.Fatalf("a refusal does not say what would help: %q", chatbug030Text(refused))
	}

	// Dismissed, the card is gone for the person, and only that card.
	m.PersonaActivityReady = true
	m.AgentDismissed = map[string]bool{"run": true}
	if rows := personaReplyRowsForPost(m, localUI{}, "question", time.Now()); len(rows) != 0 {
		t.Fatalf("a dismissed failure still draws %d rows", len(rows))
	}
	m.AgentDismissed = map[string]bool{"another-run": true}
	if rows := personaReplyRowsForPost(m, localUI{}, "question", time.Now()); len(rows) != 1 {
		t.Fatalf("dismissing one card removed another: %d rows", len(rows))
	}

	// Asking again that came to nothing says so on the card, beside the action.
	m.AgentDismissed = nil
	m.AgentRetries = map[string]AgentRetryState{"question": {Failed: true}}
	if note := chatbug030Text(chatbug054Card(t, m)); !strings.Contains(note, "The question could not be asked again. Try again in a moment.") {
		t.Fatalf("a failed Ask again is silent: %q", note)
	}

	// The frame is the answered card's.
	for _, want := range []string{".persona-progress-failure.agent-reply-row{", "max-width:var(--chat-measure)", "border-inline-start:3px solid var(--hcm-color-warning)"} {
		if !strings.Contains(Stylesheet, want) {
			t.Fatalf("the failed card's frame is missing %q", want)
		}
	}
}

// In the agent's own conversation the failed answer is the agent's message; it
// ends with the same two actions, and nobody but the asker is offered them.
func TestTodo_CHATBUG_054_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "failed", true)
		m.Messages[0].SentAt = time.Now().Add(-48 * time.Hour)
		card := chatbug054Card(t, m)
		actions := chatPolishNodesIn(card, func(n *xhtml.Node) bool { return chatPolishAttr(n, "data-agent-action") != "" })
		if len(actions) != 2 || chatPolishAttr(actions[0], "data-agent-action") != "retry" || chatPolishAttr(actions[1], "data-agent-action") != "dismiss" {
			t.Fatalf("%s: the direct failed answer has %d actions", locale, len(actions))
		}
	}
	other := chat4Fixture("en-US", "failed", false)
	other.CurrentUser = "bob"
	if rows := personaReplyRowsForPost(other, localUI{}, "question", time.Now()); len(rows) != 0 {
		t.Fatal("somebody who did not ask sees the failed card")
	}
}
