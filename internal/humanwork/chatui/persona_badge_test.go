package chatui

import (
	"strings"
	"testing"
)

func TestPersonaBadgeRequiresTrustedActorAndShowsAttribution(t *testing.T) {
	valid := render(t, Model{State: StateReady, SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "Room"}}, Messages: []Message{{ID: "post", Author: "Policy Helper", PersonaActor: &PersonaActor{PersonaID: "persona-1", AgentID: "agent-1", InvokerHandle: "dana-ruiz", Trusted: true}, Body: "answer"}}})
	for _, want := range []string{"Agent", "acting for @dana-ruiz", "agent-badge", "aria-label=\"Agent; acting for @dana-ruiz\""} {
		if !strings.Contains(valid, want) {
			t.Fatalf("trusted persona post missing %q: %s", want, valid)
		}
	}
	missing := render(t, Model{State: StateReady, SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "Room"}}, Messages: []Message{{ID: "post", Author: "Policy Helper", PersonaActor: &PersonaActor{PersonaID: "persona-1"}, Body: "answer"}}})
	if !strings.Contains(missing, "Agent identity unavailable") || strings.Contains(missing, "acting for @Policy") {
		t.Fatalf("incomplete actor was inferred or hidden: %s", missing)
	}
}

func TestPersonaSearchResultCarriesSameBadge(t *testing.T) {
	markup := render(t, Model{State: StateReady, Search: "answer", SearchMessages: []SearchMessage{{ConversationID: "room", ConversationName: "Room", Message: Message{ID: "post", Author: "Policy Helper", Body: "answer", PersonaActor: &PersonaActor{PersonaID: "p", AgentID: "a", InvokerHandle: "manager", Trusted: true}}}}, Callbacks: Callbacks{OpenSearchMessage: func(string, string, uint64) {}}})
	if !strings.Contains(markup, "acting for @manager") || !strings.Contains(markup, "agent-badge") {
		t.Fatalf("search result omitted persona attribution: %s", markup)
	}
}
