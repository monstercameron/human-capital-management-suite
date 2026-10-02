package chatui

import (
	"strings"
	"testing"
	"time"
)

// AGENTUX-027 clause table (the GREEN line, one row each):
//
//	the private path writes no durable post in the channel ........ TestTodo_AGENTUX_027_Security (internal/application, real chat store)
//	another member's projection, search, unread, threads .......... TestTodo_AGENTUX_027_Security; TestTodo_AGENTUX_027 and _Browser here
//	no visible text uses the word "persona" ....................... TestTodo_AGENTUX_027 (every state of the card, three languages)
//	a receipt stored before this change reads as a plain line ..... TestTodo_AGENTUX_027_Browser

// The agent's row under a question is the asker's alone, and nothing on it, in
// any state or language, says "persona" or prints the catalogue key the first
// receipt leaked.
func TestTodo_AGENTUX_027(t *testing.T) {
	asked := time.Date(2026, 10, 1, 11, 59, 0, 0, time.UTC)
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		states := map[string]Model{}
		working := agentUXReplyModel(locale)
		states["working"] = working
		private := agentUXReplyModel(locale)
		private.EphemeralMessages = []EphemeralMessage{{ID: "answer", ThreadID: "question", Body: "Carry over up to five days.", OnlyVisibleToYou: true, CreatedAt: asked, ExpiresAt: asked.Add(12 * time.Hour)}}
		private.PersonaInvocations[0].Projection.Progress = nil
		private.PersonaInvocations[0].Projection.PrivateReplyHref = "/workspace/app/chat#channel=agent-dm"
		states["private"] = private
		failed := agentUXReplyModel(locale)
		failed.PersonaInvocations[0].Projection.Progress = nil
		failed.PersonaInvocations[0].Projection.Failure = &PersonaProgressFailure{InvocationID: "invocation", InvokerID: "alice", Code: "MODEL_UNAVAILABLE"}
		states["failed"] = failed
		for name, m := range states {
			text := strings.ToLower(agentUX026Text(renderAgentUXReplyRows(t, m, "question", 1440)))
			for _, word := range []string{"persona", "chat.persona", "chat.agent", "open_private_reply", "sent privately to you"} {
				if strings.Contains(text, word) {
					t.Fatalf("%s/%s: the row reads %q: %s", locale, name, word, text)
				}
			}
			// Nobody but the asker is shown any of it.
			for _, other := range []string{"bob", "carol"} {
				m.CurrentUser = other
				if rows := personaReplyRowsForPost(m, localUI{}, "question", asked); len(rows) != 0 {
					t.Fatalf("%s/%s: %s is shown %d rows of Alice's", locale, name, other, len(rows))
				}
			}
		}
	}
}

// Another member's page: the channel holds the question and nothing about the
// answer. A receipt that an older build stored in the channel reads as a plain
// line that does not call the agent a persona.
func TestTodo_AGENTUX_027_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, state := range []string{"sent", "working2", "answered", "failed"} {
			m := chat4Fixture(locale, state, false)
			m.CurrentUser = "bob"
			m.PersonaActivityReady = true
			markup := strings.ToLower(render(t, m))
			for _, trace := range []string{"data-agent-reply-state", "agent-reply-row", "only visible to you", "nur für sie sichtbar"} {
				if strings.Contains(markup, trace) {
					t.Fatalf("%s/%s: another member's page carries %q", locale, state, trace)
				}
			}
			if text := strings.ToLower(chatbug021Visible(markup)); strings.Contains(text, "persona") || strings.Contains(text, "chat.persona") {
				t.Fatalf("%s/%s: another member's page reads %q", locale, state, text)
			}
			if strings.Contains(markup, "chatbug040-reserve") {
				t.Fatalf("%s/%s: room is kept under a question another member asked", locale, state)
			}
		}
	}

	// A receipt stored by the build that wrote one: the line is neutral.
	m := chat4Fixture("en-US", "sent", false)
	m.Messages = append(m.Messages, Message{ID: "receipt", AuthorID: "alice", Author: "Alice", Body: legacyPrivateAnswerReceiptBody, TimeLabel: "9:31"})
	m.CurrentUser = "bob"
	text := strings.ToLower(chatbug021Visible(render(t, m)))
	if strings.Contains(text, "persona") || strings.Contains(text, "chat.persona") || strings.Contains(text, "open_private_reply") {
		t.Fatalf("a stored receipt prints internal text: %s", text)
	}
	if !strings.Contains(text, "the answer was sent privately.") {
		t.Fatalf("a stored receipt is not the neutral line: %s", text)
	}
}
