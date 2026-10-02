//go:build !(js && wasm)

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Loading Chat never asks an agent anything. The page is opened on a question
// whose run ended with only a document's name, the conversation is left and
// entered again, and the stored activity is read again: no question is marked as
// asked again, the card stands as it was drawn, and what it offers is one button
// that waits for a person to press it. (The AST half of this clause, that the
// retry endpoint is reachable from the button's trusted click alone, is
// TestTodo_CHATBUG_018.)
func TestTodo_CHATBUG_018_Browser(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	cfg := journeyclient.Config{Tenant: "northwind", Subject: "avery", Locale: "en-US"}
	ref := chatui.ChatReference{Kind: "AGENT_MENTION", ID: "policy-helper", TenantID: "northwind", Display: "Policy Helper", ConversationID: "general"}
	opening := personaChatActivity{Invocations: []personaChatInvocation{{InvocationID: "run", PostID: "question", ThreadID: "question", ConversationID: "general", InvokerID: "avery", AgentName: "Policy Helper", Status: "FAILED", FailureCode: chatcore.AgentAnswerTitleOnlyCode}}}

	state := newChatStateForTest(t)
	open := func() {
		state.mutate(func(model *chatui.Model) {
			model.State, model.Locale, model.CurrentUser, model.CurrentTenantID = chatui.StateReady, "en-US", "avery", "northwind"
			model.Conversations = []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true}, {ID: "random", Name: "random", Kind: chatui.PublicChannel, Joined: true}}
			model.Messages = []chatui.Message{{ID: "question", AuthorID: "avery", Author: "Avery", Body: "@Policy Helper give me a list of the top 5 policies here", TimeLabel: "8:05", SentAt: now.Add(-time.Hour), PersonaReferences: []chatui.ChatReference{ref}}}
			model.ResolvedPersonaMentions = []chatui.ResolvedPersonaMention{{Reference: ref, Handle: "policy-helper", Initials: "PH"}}
			model.PersonaInvocations = chatbug079StoredAnswers(model.PersonaInvocations, personaChatInvocations(opening.Invocations, cfg, "general"), now)
			model.PersonaActivityReady = true
		})
	}
	state.selectChatConversation("general")
	open()
	state.selectChatConversation("random")
	state.selectChatConversation("general")
	open()
	model := state.snapshot()
	if len(model.AgentRetries) != 0 {
		t.Fatalf("loading Chat marked a question as asked again: %+v", model.AgentRetries)
	}
	if len(model.PersonaInvocations) != 1 || model.PersonaInvocations[0].Projection.Failure == nil || model.PersonaInvocations[0].Projection.Failure.Code != chatcore.AgentAnswerTitleOnlyCode {
		t.Fatalf("the finished run was not read back as it ended: %+v", model.PersonaInvocations)
	}

	model.Callbacks = chatui.Callbacks{SelectConversation: func(string) {}}
	markup, err := ui.RenderToString(chatui.Build(model))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(markup, `data-agent-reply-state="failed"`) != 1 || strings.Contains(markup, `data-agent-reply-state="working"`) {
		t.Fatalf("a loaded page does not hold the card exactly as the run ended: %s", markup)
	}
	if !strings.Contains(markup, "answered with only the name of a document, so the answer is not shown.") {
		t.Fatalf("the title-only run is not said in one sentence: %s", markup)
	}
	// What is on the page is an answer's refusal, never the title posted as an answer.
	if strings.Contains(markup, "agent-reply-answer") {
		t.Fatalf("a refused title-only reply is drawn as an answer: %s", markup)
	}
	if strings.Count(markup, `data-agent-action="retry"`) != 1 {
		t.Fatalf("the card does not offer one button to ask again: %s", markup)
	}
}
