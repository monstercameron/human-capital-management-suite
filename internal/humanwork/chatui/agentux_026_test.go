package chatui

import (
	"strings"
	"testing"
	"time"
)

// agentUX026Rows renders the rows under the fixture's question.
func agentUX026Rows(t *testing.T, m Model) string {
	t.Helper()
	return renderAgentUXReplyRows(t, m, "question", 1440)
}

// agentUX026Internal are words and codes a person must never read on the row.
var agentUX026Internal = []string{"MODEL_UNAVAILABLE", "OUTPUT_REJECTED", "ANSWER_INTERRUPTED", "invocation", "persona", "chat.agent.", "⟦", "{name}", "run_id"}

// Under the message that asked, the asker sees one row: the agent at work, then
// what came of it (the reply posted to the channel, the private answer, or one
// plain sentence with a way to ask again). The row is drawn from what the
// server reports alone, so it is the same after a reload, and nobody else ever
// sees it.
func TestTodo_AGENTUX_026(t *testing.T) {
	asked := time.Date(2026, 10, 1, 11, 59, 0, 0, time.UTC)
	working := agentUXReplyModel("en-US")

	// Working: one row, the agent named, one live region.
	row := agentUX026Rows(t, working)
	if strings.Count(row, `data-agent-reply-state=`) != 1 || !strings.Contains(row, `data-agent-reply-state="working"`) || !strings.Contains(row, "Policy Helper") || strings.Count(row, `aria-live="polite"`) != 1 {
		t.Fatalf("the working row: %s", row)
	}

	// The answer was posted to the channel: the reply is the answer, and no row
	// of the asker's own stands beside it.
	public := agentUXReplyModel("en-US")
	public.PersonaInvocations[0].Projection = PersonaProgressProjection{InvocationID: "invocation", ViewerID: "alice", InvokerID: "alice", AgentName: "Policy Helper", DurablePostID: "public-answer"}
	if row = agentUX026Rows(t, public); strings.Contains(row, `data-agent-reply-state=`) || strings.Contains(row, "agent-reply-row") || strings.TrimSpace(agentUX026Text(row)) != "" {
		t.Fatalf("a publicly answered question still shows a row of the asker's own: %s", row)
	}

	// The private answer: the same one row, now the answer, marked private, with
	// the way to the conversation with the agent.
	private := agentUXReplyModel("en-US")
	private.EphemeralMessages = []EphemeralMessage{{ID: "answer", ThreadID: "question", Body: "Carry over up to five days.", OnlyVisibleToYou: true, CreatedAt: asked, ExpiresAt: asked.Add(12 * time.Hour)}}
	private.PersonaInvocations[0].Projection.Progress = nil
	private.PersonaInvocations[0].Projection.PrivateReplyHref = "/workspace/app/chat#channel=agent-dm"
	private.MenuID = "agent-card:answer"
	row = agentUX026Rows(t, private)
	if strings.Count(row, `data-agent-reply-state=`) != 1 || !strings.Contains(row, `data-agent-reply-state="answered-private"`) || !strings.Contains(row, "Carry over up to five days.") || !strings.Contains(row, "Only visible to you") || !strings.Contains(row, `href="/workspace/app/chat#channel=agent-dm"`) {
		t.Fatalf("the private answer row: %s", row)
	}

	// It failed: one plain sentence and a way to ask again.
	failed := agentUXReplyModel("en-US")
	failed.SelectedID, failed.Conversations = "general", []Conversation{{ID: "general", Name: "general", Kind: PublicChannel}}
	failed.PersonaInvocations[0].Projection.Progress = nil
	failed.PersonaInvocations[0].Projection.Failure = &PersonaProgressFailure{InvocationID: "invocation", InvokerID: "alice", Code: "MODEL_UNAVAILABLE", Message: "upstream 503 from provider", Retryable: true}
	row = agentUX026Rows(t, failed)
	if strings.Count(row, `data-agent-reply-state=`) != 1 || !strings.Contains(row, `data-agent-reply-state="failed"`) || !strings.Contains(row, "Policy Helper could not answer because the service had a problem.") || !strings.Contains(row, `data-agent-action="retry"`) || strings.Contains(row, "upstream 503") {
		t.Fatalf("the failed row: %s", row)
	}

	for name, m := range map[string]Model{"working": working, "private": private, "failed": failed} {
		row := agentUX026Rows(t, m)
		for _, internal := range agentUX026Internal {
			// Attribute values name the run for the page's own handlers; what a
			// person reads is the text between the tags.
			if strings.Contains(strings.ToLower(agentUX026Text(row)), strings.ToLower(internal)) {
				t.Fatalf("%s: the row reads %q: %s", name, internal, agentUX026Text(row))
			}
		}
		// Somebody else in the channel sees none of it.
		other := m
		other.CurrentUser = "bob"
		if rows := personaReplyRowsForPost(other, localUI{}, "question", asked); len(rows) != 0 {
			t.Fatalf("%s: another member sees %d rows of the asker's", name, len(rows))
		}
		// The same data on a page that was just loaded draws the same row: the
		// row holds nothing the page had to remember.
		reloaded := Model{Locale: m.Locale, CurrentUser: m.CurrentUser, SelectedID: m.SelectedID, Conversations: m.Conversations, Messages: m.Messages, ResolvedPersonaMentions: m.ResolvedPersonaMentions, PersonaInvocations: m.PersonaInvocations, EphemeralMessages: m.EphemeralMessages, MenuID: m.MenuID}
		if again := agentUX026Rows(t, reloaded); again != row {
			t.Fatalf("%s: the row differs after a reload:\n%s\n%s", name, row, again)
		}
	}
}

// agentUX026Text is the text of a fragment, without its tags.
func agentUX026Text(markup string) string {
	var text strings.Builder
	inTag := false
	for _, r := range markup {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			text.WriteByte(' ')
		case !inTag:
			text.WriteRune(r)
		}
	}
	return text.String()
}

// What goes wrong still ends in one plain row: an answer that never came, a
// reason the page does not know, and a state for somebody else's question.
func TestTodo_AGENTUX_026_Fault(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	channel := func() Model {
		m := agentUXReplyModel("en-US")
		m.SelectedID, m.Conversations = "general", []Conversation{{ID: "general", Name: "general", Kind: PublicChannel}}
		return m
	}

	// The run was admitted and never reported again: past its deadline the row
	// stops counting and says the answer was interrupted, with Ask again.
	overdue := channel()
	overdue.PersonaInvocations[0].Projection.Progress.Deadline = now.Add(-time.Minute)
	row := renderAgentUXReplyRows(t, overdue, "question", 1440)
	if strings.Count(row, `data-agent-reply-state=`) != 1 || !strings.Contains(row, `data-agent-reply-state="failed"`) || !strings.Contains(agentUX026Text(row), "answer was interrupted.") || strings.Contains(row, "agent-working-dots") {
		t.Fatalf("a run past its deadline: %s", row)
	}
	if !strings.Contains(row, `data-agent-action="retry"`) {
		t.Fatalf("an interrupted answer cannot be asked again: %s", row)
	}

	// A reason the page has no sentence for is still a sentence, never the code.
	unknown := channel()
	unknown.PersonaInvocations[0].Projection.Progress = nil
	unknown.PersonaInvocations[0].Projection.Failure = &PersonaProgressFailure{InvocationID: "invocation", InvokerID: "alice", Code: "SOME_NEW_INTERNAL_CODE", Message: "stack trace at persona_run_executor.go:399"}
	row = renderAgentUXReplyRows(t, unknown, "question", 1440)
	text := agentUX026Text(row)
	if !strings.Contains(row, `data-agent-reply-state="failed"`) || strings.Contains(text, "SOME_NEW_INTERNAL_CODE") || strings.Contains(text, "stack trace") || strings.Contains(text, ".go:") || !strings.Contains(text, "Policy Helper") {
		t.Fatalf("an unknown failure reads %q", text)
	}

	// A failure reported for somebody else's question draws nothing for the viewer.
	foreign := channel()
	foreign.PersonaActivityReady = true
	foreign.PersonaInvocations[0].Projection.Progress = nil
	foreign.PersonaInvocations[0].Projection.InvokerID = "bob"
	foreign.PersonaInvocations[0].Projection.Failure = &PersonaProgressFailure{InvocationID: "invocation", InvokerID: "bob", Code: "MODEL_UNAVAILABLE"}
	if rows := personaReplyRowsForPost(foreign, localUI{}, "question", now); len(rows) != 0 {
		t.Fatalf("somebody else's failure is drawn for the viewer: %d rows", len(rows))
	}

	// A private answer that has expired is not drawn, and the finished run does
	// not go back to looking like work.
	expired := channel()
	expired.PersonaInvocations[0].Projection.Progress = nil
	expired.PersonaInvocations[0].Projection.AnswerStored, expired.PersonaInvocations[0].Projection.PrivateReplyHref = true, "/workspace/app/chat#channel=agent-dm"
	expired.PersonaInvocations[0].Projection.AnswerDue = now.Add(-time.Minute)
	expired.EphemeralMessages = []EphemeralMessage{{ID: "answer", ThreadID: "question", Body: "Carry over up to five days.", OnlyVisibleToYou: true, CreatedAt: now.Add(-25 * time.Hour), ExpiresAt: now.Add(-time.Hour)}}
	row = renderAgentUXReplyRows(t, expired, "question", 1440)
	if strings.Contains(row, "Carry over up to five days.") || strings.Contains(row, `data-agent-reply-state="working"`) || !strings.Contains(row, "Open the answer") {
		t.Fatalf("an expired private answer: %s", row)
	}
}
