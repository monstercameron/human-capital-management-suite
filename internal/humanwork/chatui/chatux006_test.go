package chatui

import (
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"
)

// TestTodo_CHATUX_006: the hint that an agent was not mentioned belongs to the
// person's own message, only while it is the newest message in the conversation
// or less than ten minutes old, and never to another reader.
func TestTodo_CHATUX_006(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	base := chat4Fixture("en-US", "sent", false)
	base.Messages = nil
	own := func(id string, age time.Duration) Message {
		message := Message{ID: id, AuthorID: "alice", Author: "Alice", Body: "@pol explain our PTO policy in detail"}
		if age >= 0 {
			message.SentAt = now.Add(-age)
		}
		return message
	}
	for _, tc := range []struct {
		name     string
		messages []Message
		subject  string
		viewer   string
		want     bool
	}{
		{name: "own newest message, however old", messages: []Message{own("m1", 3*time.Hour)}, subject: "m1", want: true},
		{name: "own newest message, no time known", messages: []Message{own("m1", -1)}, subject: "m1", want: true},
		{name: "own older message, sent a minute ago", messages: []Message{own("m1", time.Minute), {ID: "m2", AuthorID: "bob", Body: "ok"}}, subject: "m1", want: true},
		{name: "own older message, nine minutes fifty-nine seconds ago", messages: []Message{own("m1", 10*time.Minute-time.Second), {ID: "m2", AuthorID: "bob", Body: "ok"}}, subject: "m1", want: true},
		{name: "own older message, exactly ten minutes ago", messages: []Message{own("m1", 10*time.Minute), {ID: "m2", AuthorID: "bob", Body: "ok"}}, subject: "m1"},
		{name: "own older message, three hours ago", messages: []Message{own("m1", 3*time.Hour), {ID: "m2", AuthorID: "bob", Body: "ok"}}, subject: "m1"},
		{name: "own older message, no time known", messages: []Message{own("m1", -1), {ID: "m2", AuthorID: "bob", Body: "ok"}}, subject: "m1"},
		{name: "somebody else reading the newest message", messages: []Message{own("m1", time.Minute)}, subject: "m1", viewer: "bob"},
		{name: "somebody else's message, newest", messages: []Message{{ID: "m1", AuthorID: "bob", Body: "@pol explain our PTO policy in detail", SentAt: now}}, subject: "m1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := base
			model.Messages = tc.messages
			if tc.viewer != "" {
				model.CurrentUser = tc.viewer
			}
			var subject Message
			for _, message := range tc.messages {
				if message.ID == tc.subject {
					subject = message
				}
			}
			if got := chatux006HintVisible(model, subject, now); got != tc.want {
				t.Fatalf("hint visible = %v, want %v", got, tc.want)
			}
		})
	}
	// The newest message of an open thread counts as the newest there.
	thread := base
	thread.ThreadMessages = []Message{own("t1", 2*time.Hour)}
	if !chatux006HintVisible(thread, thread.ThreadMessages[0], now) {
		t.Fatal("the newest reply in the thread lost its hint")
	}
}

// TestTodo_CHATUX_006_Browser renders #general with three old messages that name
// an agent without mentioning it and one just sent: only the one just sent
// carries the hint and its button, and a second member sees none, in each
// language and at each width.
func TestTodo_CHATUX_006_Browser(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model := chat4Fixture(locale, "sent", false)
		model.PersonaInvocations = nil
		question := model.Messages[0]
		old := func(id string, age time.Duration) Message {
			return Message{ID: id, AuthorID: "alice", Author: "Alice", Body: "@pol explain our PTO policy in detail", TimeLabel: "9:00", SentAt: time.Now().Add(-age)}
		}
		model.Messages = []Message{old("old-1", 26*time.Hour), old("old-2", 25*time.Hour), old("old-3", 2*time.Hour), {ID: "reply", AuthorID: "bob", Author: "Bob", Body: "thanks", TimeLabel: "9:20", SentAt: time.Now().Add(-90 * time.Minute)}, old("fresh", 20*time.Second)}
		_ = question
		hint := agentUXChat4Format(model, "chat.agent.not_mentioned", map[string]string{"name": "Policy Helper"})
		ask := agentUXChat4Format(model, "chat.agent.ask_named", map[string]string{"name": "Policy Helper"})
		markup := renderAgentUXChat3Node(t, Build(model), width)
		if strings.Count(markup, hint) != 1 || strings.Count(markup, ask) != 1 {
			t.Fatalf("hint shown %d times and its button %d times, want once each (on the message just sent)", strings.Count(markup, hint), strings.Count(markup, ask))
		}
		if !strings.Contains(markup[strings.Index(markup, `data-message-id="fresh"`):], hint) {
			t.Fatal("the hint is not under the message just sent")
		}
		model.CurrentUser = "bob"
		if other := renderAgentUXChat3Node(t, Build(model), width); strings.Contains(other, hint) {
			t.Fatal("another reader was shown the hint")
		}
	})
}

// TestTodo_CHATUX_006_Accessibility: the hint is a private, named paragraph with a
// button that names the agent.
func TestTodo_CHATUX_006_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		model := chat4Fixture(locale, "sent", false)
		message := Message{ID: "fresh", AuthorID: "alice", Body: "@pol explain our PTO policy in detail", SentAt: time.Now()}
		model.Messages = append(model.Messages, message)
		markup := renderNode(t, unresolvedMessageRecovery(model, message))
		nodes := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "button" })
		if len(nodes) != 1 || !strings.Contains(chatbug030Text(nodes[0]), "Policy Helper") || chatPolishAttr(nodes[0], "data-action") != "agent-suggest-mention" {
			t.Fatalf("%s: hint button = %+v", locale, nodes)
		}
		if !strings.Contains(markup, `data-recipient-only="true"`) || !strings.Contains(markup, "Policy Helper") {
			t.Fatalf("%s: hint lost its private marker or the agent's name: %s", locale, markup)
		}
	}
}
