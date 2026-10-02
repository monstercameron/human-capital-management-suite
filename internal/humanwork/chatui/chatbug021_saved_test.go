package chatui

import (
	"strings"
	"testing"
	"time"
)

// CHATBUG-021 names the places internal text must never reach: a message, a
// result, a notification and a saved item. TestTodo_CHATBUG_021 covers the first
// two; this one covers the Saved panel, where the stored body of an agent answer
// (with its context token, share address and the model's own Sources list) is
// the row's text. No code path in the chat page or the chat services builds a
// notification from a message body (the page's notification controls are
// settings), so a notification cannot print it.
func TestTodo_CHATBUG_021_Saved(t *testing.T) {
	actor := &PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}
	for name, body := range map[string]string{"stored": chatbug021Stored, "projected": chatbug021Projected} {
		row := SavedMessageRow{TenantID: "tenant", ConversationID: "policy", PostID: "answer", AuthorID: "policy-helper", Author: "Policy Helper", Channel: "Policy Helper", SentAt: chatsave002Now.Add(-time.Hour), Body: body, Availability: "readable", Sequence: 5, Revision: 1}
		view := chatsave002View("todo", []SavedMessageRow{row})
		view.Model.ResolvedPersonaMentions = []ResolvedPersonaMention{{Reference: ChatReference{Kind: "AGENT_MENTION", ID: "policy-helper", Display: "Policy Helper", TenantID: "tenant"}, Handle: "policy-helper", Initials: "PH"}}
		view.Model.PersonaPostActors = map[string]PersonaPostActor{"answer": {Display: "Policy Helper", Actor: *actor}}
		markup := renderNode(t, RenderSavedMessages(view))
		chatbug021Printed(t, "Saved item of the "+name+" answer", chatbug021Visible(markup))
		if !strings.Contains(chatbug021Visible(markup), "Employees may carry over up to 40 hours") {
			t.Fatalf("%s: the saved answer lost its statement: %s", name, markup)
		}
		if strings.Count(markup, "agent-reply-sources") > 1 || strings.Count(chatbug021Visible(markup), "Sources") > 1 {
			t.Fatalf("%s: the saved answer lists its sources more than once: %s", name, markup)
		}
		if strings.Contains(markup, "chat-agent-question") || strings.Contains(markup, "/chat/share/") {
			t.Fatalf("%s: the saved item carries the context token or share address: %s", name, markup)
		}
	}
}
