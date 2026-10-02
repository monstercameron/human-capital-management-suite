package main

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The pending card ends when the server reports a final state, after a reload
// as much as in a live stream, and never counts for an invocation that is over.
func TestTodo_AGENTRUN_004_Client(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "viewer"}
	server := func(status, code string, retryable bool) []personaChatInvocation {
		return []personaChatInvocation{{InvocationID: "invoke", ConversationID: "room", PostID: "post", ThreadID: "post", InvokerID: "viewer", Status: status, FailureCode: code, Retryable: retryable}}
	}

	// The question was just sent: the client's own waiting card, with a deadline.
	model := &chatui.Model{CurrentUser: "viewer", CurrentTenantID: "tenant", SelectedID: "room"}
	provisional := chatui.PersonaThreadInvocation{PostID: "post", Projection: chatui.PersonaProgressProjection{InvocationID: "pending:post", ViewerID: "viewer", InvokerID: "viewer", Progress: agentPendingProgress("viewer", "Policy Helper")}}
	model.PersonaInvocations = []chatui.PersonaThreadInvocation{provisional}
	progress := model.PersonaInvocations[0].Projection.Progress
	if !progress.Provisional || progress.Deadline.IsZero() || time.Until(progress.Deadline) > agentPendingWindow || time.Until(progress.Deadline) < agentPendingWindow-5*time.Second {
		t.Fatalf("the provisional card has no deadline: %+v", progress)
	}
	if !advancePersonaElapsed(model) || progress.ElapsedSeconds != 1 {
		t.Fatalf("a card inside its deadline did not count: %+v", progress)
	}

	// The server says the answer was interrupted. The provisional card is replaced
	// by the failure with Try again, and nothing counts.
	model.PersonaInvocations = reconcileAgentPending(model.PersonaInvocations, personaChatInvocations(server("FAILED", "ANSWER_INTERRUPTED", true), cfg, "room"))
	if len(model.PersonaInvocations) != 1 {
		t.Fatalf("cards=%+v", model.PersonaInvocations)
	}
	card := model.PersonaInvocations[0].Projection
	if card.Progress != nil || card.Failure == nil || card.Failure.Code != "ANSWER_INTERRUPTED" || !card.Failure.Retryable || card.InvocationID != "invoke" {
		t.Fatalf("a final state from the server did not end the card: %+v", card)
	}
	if advancePersonaElapsed(model) {
		t.Fatal("a card for an invocation in a final state is still counting")
	}

	// After a reload there is no provisional card; the stream's first event is the
	// whole truth. Every final status ends the card; a working one does not.
	for _, final := range []struct{ status, code string }{{"FAILED", "ANSWER_INTERRUPTED"}, {"EXPIRED", "EXPIRED"}, {"CANCELLED", "CANCELLED"}, {"NEEDS_REPAIR", "DELIVERY_FAILED"}, {"COMPLETED", ""}} {
		reloaded := &chatui.Model{CurrentUser: "viewer", CurrentTenantID: "tenant", SelectedID: "room"}
		reloaded.PersonaInvocations = reconcileAgentPending(nil, personaChatInvocations(server(final.status, final.code, false), cfg, "room"))
		if len(reloaded.PersonaInvocations) != 1 || reloaded.PersonaInvocations[0].Projection.Progress != nil || advancePersonaElapsed(reloaded) {
			t.Fatalf("%s after a reload is still waiting: %+v", final.status, reloaded.PersonaInvocations)
		}
	}
	working := &chatui.Model{CurrentUser: "viewer", CurrentTenantID: "tenant", SelectedID: "room"}
	working.PersonaInvocations = reconcileAgentPending(nil, personaChatInvocations(server("RUNNING", "", false), cfg, "room"))
	if len(working.PersonaInvocations) != 1 || working.PersonaInvocations[0].Projection.Progress == nil || !advancePersonaElapsed(working) {
		t.Fatalf("work in progress is not shown as such: %+v", working.PersonaInvocations)
	}

	// A question the server never reports on ends on its own: no run stands behind it.
	stale := &chatui.Model{}
	stale.PersonaInvocations = []chatui.PersonaThreadInvocation{provisional}
	staleProgress := *provisional.Projection.Progress
	counted := staleProgress.ElapsedSeconds
	staleProgress.Deadline = time.Now().Add(-time.Second)
	stale.PersonaInvocations[0].Projection.Progress = &staleProgress
	changed := advancePersonaElapsed(stale)
	if !changed || staleProgress.ElapsedSeconds != counted {
		t.Fatalf("the expired card must be drawn once as interrupted and stop counting: changed=%t %+v", changed, staleProgress)
	}
	staleProgress.Deadline = time.Now().Add(-time.Minute)
	if advancePersonaElapsed(stale) || staleProgress.ElapsedSeconds != counted {
		t.Fatalf("an expired card keeps counting: %+v", staleProgress)
	}
}
