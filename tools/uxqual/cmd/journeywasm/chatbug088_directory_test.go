package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Choosing a conversation clears the agent directory the model held. A start of
// the same conversation after that must read again; one made while a read is
// under way, or after one ended, need not.
func TestTodo_CHATBUG_088_DirectoryHeld(t *testing.T) {
	model := chatui.Model{SelectedID: "general"}
	for _, state := range []chatui.PersonaLookupState{"", chatui.PersonaLookupIdle} {
		model.PersonaLookup, model.PersonaLookupConversationID = state, ""
		if personaDirectoryHeld(model, "general") {
			t.Fatalf("a cleared directory (%q) counts as held", state)
		}
	}
	for _, state := range []chatui.PersonaLookupState{chatui.PersonaLookupLoading, chatui.PersonaLookupReady, chatui.PersonaLookupFailed} {
		model.PersonaLookup, model.PersonaLookupConversationID = state, "general"
		if !personaDirectoryHeld(model, "general") {
			t.Fatalf("a directory in state %q is not held", state)
		}
	}
	// Held for another conversation is not held for this one.
	model.PersonaLookup, model.PersonaLookupConversationID = chatui.PersonaLookupReady, "other"
	if personaDirectoryHeld(model, "general") || personaDirectoryHeld(model, "other") {
		t.Fatal("a directory of another conversation counts as held")
	}
}
