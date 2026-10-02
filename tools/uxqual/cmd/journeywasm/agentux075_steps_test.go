package main

import (
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// AGENTUX-075: a post in the person's own conversation with an agent names no
// agent, and still stands a working message under it at once, in that agent's
// name, and the step the server reports reaches the working line.
func TestTodo_AGENTUX_075_ClientSteps(t *testing.T) {
	model := chatui.Model{CurrentUser: "walt", CurrentTenantID: "northwind", SelectedID: "dm",
		Conversations: []chatui.Conversation{{ID: "dm", Name: "Assistant", Agent: true, AgentID: "assistant"}}}
	post := &chatv1.Post{Id: "question", AuthorId: "walt", ConversationId: "dm", Body: "What can you help me with here?"}
	applyCommittedAgentPending(&model, post)
	if len(model.PersonaInvocations) != 1 {
		t.Fatalf("no working message stands under the question: %+v", model.PersonaInvocations)
	}
	waiting := model.PersonaInvocations[0].Projection
	if waiting.AgentName != "Assistant" || waiting.ViewerID != "walt" || waiting.Progress == nil || !waiting.Progress.Visible || !waiting.Progress.Provisional || waiting.Progress.InvocationID != "" {
		t.Fatalf("the waiting state lost the agent's name, the asker or its provisional mark: %+v", waiting)
	}
	// The same post again (the stream echo) adds nothing.
	applyCommittedAgentPending(&model, post)
	if len(model.PersonaInvocations) != 1 {
		t.Fatal("the stream echo duplicated the waiting message")
	}
	// A conversation between two people, and another person's post, draw nothing.
	people := chatui.Model{CurrentUser: "walt", SelectedID: "dm", Conversations: []chatui.Conversation{{ID: "dm", Name: "Priya"}}}
	applyCommittedAgentPending(&people, post)
	other := model
	other.PersonaInvocations = nil
	applyCommittedAgentPending(&other, &chatv1.Post{Id: "q2", AuthorId: "priya", ConversationId: "dm"})
	if len(people.PersonaInvocations) != 0 || len(other.PersonaInvocations) != 0 {
		t.Fatal("a working message was drawn under a message that asked no agent")
	}

	// The step the server reports reaches the invocation's progress.
	cfg := journeyclient.Config{Tenant: "northwind", Subject: "walt"}
	rows := personaChatInvocations([]personaChatInvocation{{InvocationID: "inv", PostID: "question", ConversationID: "dm", InvokerID: "walt", AgentName: "Assistant", Status: "RUNNING", Activity: "Preparing answer", StepKind: "reading_document", StepSubject: "2026 holiday guide"}}, cfg, "dm")
	if len(rows) != 1 || rows[0].Projection.Progress == nil || rows[0].Projection.Progress.StepKind != "reading_document" || rows[0].Projection.Progress.StepSubject != "2026 holiday guide" {
		t.Fatalf("the step did not reach the working line: %+v", rows)
	}
}
