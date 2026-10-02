package main

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The row under a question survives leaving the conversation and coming back,
// and a reload. Leaving clears what the page held; the first read of the
// activity on return brings the finished run and its private answer together,
// so the row is the answer again without waiting for the event stream.
func TestTodo_AGENTUX_026_Browser(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	cfg := journeyclient.Config{Tenant: "northwind", Subject: "avery", Locale: "en-US"}
	// What the server sends as the first event of the activity in #general.
	opening := personaChatActivity{
		Invocations: []personaChatInvocation{{InvocationID: "run", PostID: "question", ThreadID: "question", ConversationID: "general", InvokerID: "avery", AgentName: "Policy Helper", Status: "COMPLETED", PrivateConversationID: "policy", PrivatePostID: "copy"}},
		Answers:     []personaChatAnswer{{ID: "answer", ThreadID: "question", Body: "Carry over up to 40 hours.", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(23 * time.Hour), InvocationID: "run"}},
	}
	open := func(state *chatState) {
		state.mutate(func(model *chatui.Model) {
			chatbug040ApplyAnswers(model, opening.Answers, now)
			model.PersonaInvocations = chatbug079StoredAnswers(model.PersonaInvocations, personaChatInvocations(opening.Invocations, cfg, "general"), now)
			model.PersonaActivityReady = true
		})
	}
	answered := func(what string, model chatui.Model) {
		t.Helper()
		if model.SelectedID != "general" || len(model.PersonaInvocations) != 1 || model.PersonaInvocations[0].PostID != "question" || !model.PersonaInvocations[0].Projection.AnswerStored {
			t.Fatalf("%s: the finished run is not under its question: %+v", what, model.PersonaInvocations)
		}
		visible := chatui.VisibleEphemeralMessages(model.EphemeralMessages, now)
		if len(visible) != 1 || visible[0].ThreadID != "question" || visible[0].Body != "Carry over up to 40 hours." {
			t.Fatalf("%s: the answer is not on the page: %+v", what, model.EphemeralMessages)
		}
	}

	state := newChatStateForTest(t)
	state.selectChatConversation("general")
	open(state)
	answered("on opening", state.snapshot())

	// Leaving the conversation takes its agent rows off the page.
	away, _ := state.selectChatConversation("random")
	if len(away.EphemeralMessages) != 0 || len(away.PersonaInvocations) != 0 || away.PersonaActivityReady {
		t.Fatalf("another conversation shows the first one's agent state: %+v %+v", away.EphemeralMessages, away.PersonaInvocations)
	}
	// Coming back: the first read of the activity restores the row as the answer.
	back, _ := state.selectChatConversation("general")
	if len(back.EphemeralMessages) != 0 || back.PersonaActivityReady {
		t.Fatalf("the page kept agent state across the switch that it should read again: %+v", back.EphemeralMessages)
	}
	open(state)
	answered("after switching away and back", state.snapshot())

	// A reload is a page that holds nothing: the same first read, the same row.
	reloaded := newChatStateForTest(t)
	reloaded.selectChatConversation("general")
	open(reloaded)
	answered("after a reload", reloaded.snapshot())

	// The stream's own delivery of the same answer afterwards adds nothing.
	reloaded.mutate(func(model *chatui.Model) {
		if chatbug040ApplyAnswers(model, opening.Answers, now) {
			t.Fatal("the same answer was applied twice")
		}
	})
	answered("after the same answer arrives again", reloaded.snapshot())
}
