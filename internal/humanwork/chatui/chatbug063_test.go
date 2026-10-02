package chatui

import (
	"strings"
	"testing"
)

// chatbug063Ordinary is #announcements with one ordinary message from the
// viewer, and a stored agent failure pointing at it that names no agent the
// conversation knows: what the server held for "what the damn".
func chatbug063Ordinary(locale, name string) Model {
	m := chat4Fixture(locale, "failed", false)
	m.PersonaActivityReady = true
	m.Messages[0].Body, m.Messages[0].PersonaReferences = "what the damn", nil
	m.PersonaInvocations = []PersonaThreadInvocation{{PostID: "question", ThreadID: "question", Projection: PersonaProgressProjection{InvocationID: "post-failure:question", ViewerID: "alice", InvokerID: "alice", AgentName: name,
		Failure: &PersonaProgressFailure{InvocationID: "post-failure:question", InvokerID: "alice", Code: "MODEL_UNAVAILABLE"}}}}
	return m
}

func chatbug063AssertNoCard(t *testing.T, when, page string) {
	t.Helper()
	for _, forbidden := range []string{`data-agent-reply-state=`, `data-agent-failure=`, "agent-ask-again", `data-agent-action="retry"`, "persona-progress-failure", "could not answer", "Only visible to you"} {
		if strings.Contains(page, forbidden) {
			t.Fatalf("%s: an agent card is drawn under a message that asked no agent (%s)", when, forbidden)
		}
	}
}

// A card is drawn only under a message that itself names an agent, as stored;
// a card is never drawn with a fallback name.
func TestTodo_CHATBUG_063(t *testing.T) {
	for _, name := range []string{"", "Persona", "Agent", "Policy Helper"} {
		chatbug063AssertNoCard(t, "failure named "+name, render(t, chatbug063Ordinary("en-US", name)))
	}

	// The same failure under a question that does name the agent is drawn, with
	// the name from the question's own mention and never a stand-in.
	asked := chatbug063Ordinary("en-US", "")
	asked.Messages[0].Body = "@Policy Helper how many PTO hours carry over?"
	asked.Messages[0].PersonaReferences = []ChatReference{{Kind: "AGENT_MENTION", ID: "policy-helper", TenantID: "tenant", Display: "Policy Helper", ConversationID: "general"}}
	page := render(t, asked)
	if !strings.Contains(page, `data-agent-reply-state="failed"`) || !strings.Contains(page, "Policy Helper could not answer") {
		t.Fatal("a failed question to a named agent lost its card")
	}
	if strings.Contains(page, "Persona could not") || strings.Contains(page, ">Persona<") {
		t.Fatal("the card names a stand-in agent")
	}

	// A mention with nothing to name it by draws nothing rather than "Agent".
	nameless := chatbug063Ordinary("en-US", "")
	nameless.Messages[0].PersonaReferences = []ChatReference{{Kind: "AGENT_MENTION", ID: "gone", TenantID: "tenant", ConversationID: "general"}}
	chatbug063AssertNoCard(t, "a mention with no name", render(t, nameless))

	// A mention kept from another conversation does not count.
	elsewhere := chatbug063Ordinary("en-US", "Policy Helper")
	elsewhere.Messages[0].PersonaReferences = []ChatReference{{Kind: "AGENT_MENTION", ID: "policy-helper", TenantID: "tenant", Display: "Policy Helper", ConversationID: "another-room"}}
	chatbug063AssertNoCard(t, "a mention bound to another conversation", render(t, elsewhere))

	// A person mention is not a question to an agent.
	person := chatbug063Ordinary("en-US", "Policy Helper")
	person.Messages[0].PersonaReferences = []ChatReference{{Kind: "PERSON_MENTION", ID: "bob", TenantID: "tenant", Display: "Bob", ConversationID: "general"}}
	chatbug063AssertNoCard(t, "a person mention", render(t, person))

	// The placeholder rule of CHATBUG-040 follows the stored message too.
	waiting := chatbug063Ordinary("en-US", "")
	waiting.PersonaInvocations, waiting.PersonaActivityReady = nil, false
	chatbug063AssertNoCard(t, "before the activity arrived", render(t, waiting))

	// Mention references belong to one composer in one conversation and are gone
	// once that draft is sent or cleared.
	store := mentionStore{box: &mentionBox{}}
	reference := ChatReference{Kind: "AGENT_MENTION", ID: "policy-helper", TenantID: "tenant", Display: "Policy Helper", ConversationID: "general"}
	store.box.personas = append(store.box.personas, personaDraftMention{Target: "chat-composer", ConversationID: "general", Reference: reference, Detached: true})
	if got := store.PersonaReferences("chat-composer", "announcements", "what the damn"); len(got) != 0 {
		t.Fatalf("a mention drafted in one conversation is sent in another: %+v", got)
	}
	if got := store.PersonaReferences("thread-composer", "general", "what the damn"); len(got) != 0 {
		t.Fatalf("a mention drafted in the composer is sent from the thread box: %+v", got)
	}
	if got := store.PersonaReferences("chat-composer", "general", "how many?"); len(got) != 1 {
		t.Fatalf("the draft lost its own mention: %+v", got)
	}
}

// The thread pane follows the same rule as the conversation.
func TestTodo_CHATBUG_063_Browser(t *testing.T) {
	m := chatbug063Ordinary("en-US", "Persona")
	parent := m.Messages[0]
	m.ShowThread, m.ThreadParentID, m.ThreadParent = true, "question", &parent
	chatbug063AssertNoCard(t, "thread pane", render(t, m))
}
