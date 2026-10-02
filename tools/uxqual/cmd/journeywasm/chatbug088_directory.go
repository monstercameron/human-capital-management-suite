package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"

// personaDirectoryHeld reports whether the model still holds the agent
// directory of conversation: a read is under way, or has ended with an answer or
// a failure. Choosing a conversation clears the directory the model held, so a
// start that sees the same conversation already started must still read again
// when the model no longer holds what that start read, or the room's messages
// keep a generic author and icon for good (CHATBUG-088).
func personaDirectoryHeld(model chatui.Model, conversation string) bool {
	if model.SelectedID != conversation || model.PersonaLookupConversationID != conversation {
		return false
	}
	switch model.PersonaLookup {
	case chatui.PersonaLookupLoading, chatui.PersonaLookupReady, chatui.PersonaLookupFailed:
		return true
	}
	return false
}
