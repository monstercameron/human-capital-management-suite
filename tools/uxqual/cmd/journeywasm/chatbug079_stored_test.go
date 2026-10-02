package main

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// A conversation loaded with a stored private answer never puts that answer in
// the working state: the projection has no progress for any status that is not
// a run in flight, and a stored answer never counts seconds.
func TestTodo_CHATBUG_079(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "viewer"}
	stored := personaChatInvocation{InvocationID: "run", ConversationID: "general", PostID: "question", InvokerID: "viewer", AgentName: "Policy Helper", Status: "COMPLETED", Activity: "Delivered", CurrentStep: 4, TotalSteps: 4, PrivateConversationID: "agent-dm", PrivatePostID: "answer"}

	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	model := &chatui.Model{CurrentUser: "viewer", CurrentTenantID: "tenant", SelectedID: "general"}
	model.PersonaInvocations = chatbug079StoredAnswers(nil, reconcileAgentPending(nil, personaChatInvocations([]personaChatInvocation{stored}, cfg, "general")), now)
	if len(model.PersonaInvocations) != 1 {
		t.Fatalf("rows=%+v", model.PersonaInvocations)
	}
	row := model.PersonaInvocations[0].Projection
	if row.Progress != nil || row.Failure != nil || !row.AnswerStored || row.PrivateReplyHref == "" {
		t.Fatalf("a stored private answer is projected as work in progress: %+v", row)
	}
	if !row.AnswerDue.Equal(now.Add(chatbug079AnswerWait)) {
		t.Fatalf("the stored answer waits until %v, want %v", row.AnswerDue, now.Add(chatbug079AnswerWait))
	}
	// Thirty idle seconds of the elapsed ticker: nothing counts, nothing redraws.
	for second := 0; second < 30; second++ {
		if advancePersonaElapsed(model) {
			t.Fatalf("second %d: a stored answer is counting like a run", second)
		}
	}
	// The same record arriving again keeps the first deadline; it does not wait afresh.
	later := chatbug079StoredAnswers(model.PersonaInvocations, personaChatInvocations([]personaChatInvocation{stored}, cfg, "general"), now.Add(20*time.Second))
	if !later[0].Projection.AnswerDue.Equal(row.AnswerDue) {
		t.Fatalf("the wait was restarted: %v then %v", row.AnswerDue, later[0].Projection.AnswerDue)
	}
	// The ticker redraws once when the wait ends, and not again.
	if chatbug079AnswerJustDue(later, row.AnswerDue.Add(-time.Second)) || !chatbug079AnswerJustDue(later, row.AnswerDue.Add(time.Second)) || chatbug079AnswerJustDue(later, row.AnswerDue.Add(time.Minute)) {
		t.Fatal("the saved-copy row is not drawn exactly once, when the wait ends")
	}

	// Only a run in flight is drawn as working.
	for status, working := range map[string]bool{"CLAIMED": true, "STARTED": true, "READY": true, "RUNNING": true, "WAITING": true, "RECONCILING": true, "running": true,
		"COMPLETED": false, "FAILED": false, "CANCELLED": false, "EXPIRED": false, "NEEDS_REPAIR": false, "": false, "SOMETHING_NEW": false} {
		invocation := stored
		invocation.Status, invocation.PrivateConversationID, invocation.PrivatePostID = status, "", ""
		rows := personaChatInvocations([]personaChatInvocation{invocation}, cfg, "general")
		if len(rows) != 1 || (rows[0].Projection.Progress != nil) != working || personaRunInFlight(status) != working {
			t.Fatalf("status %q: working=%t, want %t", status, rows[0].Projection.Progress != nil, working)
		}
	}
	// A public answer and the agent's own conversation hold nothing.
	public := stored
	public.PrivateConversationID, public.PrivatePostID, public.PublicPostID = "", "", "answer"
	if row := personaChatInvocations([]personaChatInvocation{public}, cfg, "general")[0].Projection; row.AnswerStored || row.Progress != nil {
		t.Fatalf("a public answer keeps a placeholder: %+v", row)
	}
	if row := personaChatInvocations([]personaChatInvocation{stored}, cfg, "agent-dm")[0].Projection; row.AnswerStored || row.Progress != nil {
		t.Fatalf("the agent's own conversation keeps a placeholder: %+v", row)
	}
}
