//go:build !(js && wasm)

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The private answer reaches the page two ways: with the first read of the
// conversation's agent activity (which also says whether it was shared and names
// the run that rates it), and on the recipient's own stream. They race. Whatever
// order they arrive in, the page ends on the same card, and while only the
// stream's copy is here the card says nothing about who can see the answer.
func TestTodo_CHATUX_026_ArrivalOrder(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	cfg := journeyclient.Config{Tenant: "northwind", Subject: "avery", Locale: "en-US"}
	ref := chatui.ChatReference{Kind: "AGENT_MENTION", ID: "policy-helper", TenantID: "northwind", Display: "Policy Helper", ConversationID: "general"}
	answer := personaChatAnswer{ID: "answer", ThreadID: "question", Body: "Carry over up to 40 hours.", CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(23 * time.Hour), InvocationID: "run", SharedPostID: "copy"}
	opening := personaChatActivity{
		Invocations: []personaChatInvocation{{InvocationID: "run", PostID: "question", ThreadID: "question", ConversationID: "general", InvokerID: "avery", AgentName: "Policy Helper", Status: "COMPLETED", PrivateConversationID: "policy", PrivatePostID: "copy"}},
		Answers:     []personaChatAnswer{answer},
	}

	// stream is the answer as the recipient's stream delivers it: the card, and nothing about its share.
	stream := func(state *chatState) {
		state.mutate(func(model *chatui.Model) { chatbug040ApplyAnswers(model, []personaChatAnswer{answer}, now) })
	}
	// read is the first read of the activity: the answers, the share records, the runs.
	read := func(state *chatState) {
		state.mutate(func(model *chatui.Model) {
			chatbug040ApplyAnswers(model, opening.Answers, now)
			if share, changed := chatux026SharedOnOpen(model.AgentShare, opening.Answers); changed {
				model.AgentShare = share
			}
			model.PersonaInvocations = chatbug079StoredAnswers(model.PersonaInvocations, personaChatInvocations(opening.Invocations, cfg, "general"), now)
			model.PersonaActivityReady = true
		})
	}
	page := func(state *chatState) string {
		model := state.snapshot()
		model.Callbacks = chatui.Callbacks{SelectConversation: func(string) {}, ShareAgentAnswer: func(string) {}, RemoveSharedAgentAnswer: func(string) {}, OpenThread: func(string) {}, OpenMenu: func(string) {}, SubmitAgentFeedback: func(string, bool) {}, UndoAgentFeedback: func(string) {}}
		markup, err := ui.RenderToString(chatui.Build(model))
		if err != nil {
			t.Fatal(err)
		}
		return markup
	}
	fresh := func() *chatState {
		state := newChatStateForTest(t)
		state.selectChatConversation("general")
		state.mutate(func(model *chatui.Model) {
			model.State, model.Locale, model.CurrentUser, model.CurrentTenantID = chatui.StateReady, "en-US", "avery", "northwind"
			model.Conversations = []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true, MemberCount: 18}}
			model.Messages = []chatui.Message{{ID: "question", AuthorID: "avery", Author: "Avery", Body: "@Policy Helper how many PTO hours carry over?", TimeLabel: "11:18", SentAt: now.Add(-time.Hour), PersonaReferences: []chatui.ChatReference{ref}}}
			model.ResolvedPersonaMentions = []chatui.ResolvedPersonaMention{{Reference: ref, Handle: "policy-helper", Initials: "PH"}}
		})
		return state
	}
	// cardOf is the answer card's markup: the one article of the answer.
	cardOf := func(markup string) string {
		start := strings.Index(markup, `<article class="chat-ephemeral`)
		if start < 0 {
			return ""
		}
		end := strings.Index(markup[start:], "</article>")
		if end < 0 {
			return ""
		}
		return markup[start : start+end]
	}

	// Stream first: the card is there, and it is neutral.
	early := fresh()
	stream(early)
	neutral := cardOf(page(early))
	if neutral == "" {
		t.Fatal("the answer from the stream is not drawn")
	}
	for _, wrong := range []string{"Only visible to you", "Shared with", `class="agent-feedback"`, `data-action="agent-share`, "agent-reply-private", "agent-reply-why"} {
		if strings.Contains(neutral, wrong) {
			t.Fatalf("the card drawn before the activity was read says %q: %s", wrong, neutral)
		}
	}

	// The three orders end on the same card, which is the shared one with its rating.
	streamThenRead, readThenStream, readOnly := fresh(), fresh(), fresh()
	stream(streamThenRead)
	read(streamThenRead)
	read(readThenStream)
	stream(readThenStream)
	read(readOnly)
	finals := map[string]string{"stream then read": cardOf(page(streamThenRead)), "read then stream": cardOf(page(readThenStream)), "read only": cardOf(page(readOnly))}
	want := finals["read only"]
	if want == "" {
		t.Fatal("the answer is not drawn after the activity was read")
	}
	for order, card := range finals {
		if card != want {
			t.Fatalf("%s ends on another card:\n%s\n%s", order, card, want)
		}
	}
	if !strings.Contains(want, "Shared with") || strings.Contains(want, "Only visible to you") || !strings.Contains(want, `class="agent-feedback"`) {
		t.Fatalf("the settled card is not the shared card with its rating: %s", want)
	}
}
