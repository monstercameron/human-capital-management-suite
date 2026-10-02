package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// agentIdentityKnown reports whether the model can say who an agent is: it is
// in the room's agent directory, has a receipt for a post, is the agent of a
// direct conversation, or is a member marked as an agent, or the actor carries
// its own stored icon. A message row from an agent the model cannot yet name
// draws a neutral slot and name instead of a name and picture made up from the
// author's identifier, which are another agent's whenever the stored ones arrive
// later (CHATBUG-088).
func agentIdentityKnown(m Model, actor *PersonaActor) bool {
	if !actor.valid() {
		return false
	}
	if actor.Icon.Valid() {
		return true
	}
	ids := map[string]bool{strings.TrimSpace(actor.AgentID): true, strings.TrimSpace(actor.PersonaID): true}
	for _, persona := range m.ResolvedPersonaMentions {
		if ids[strings.TrimSpace(persona.Reference.ID)] {
			return true
		}
	}
	for _, receipt := range m.PersonaPostActors {
		if receipt.Actor.valid() && (ids[strings.TrimSpace(receipt.Actor.AgentID)] || ids[strings.TrimSpace(receipt.Actor.PersonaID)]) {
			return true
		}
	}
	for _, c := range m.Conversations {
		if c.Agent && ids[strings.TrimSpace(c.AgentID)] {
			return true
		}
	}
	for _, member := range m.Members {
		if member.Agent && ids[strings.TrimSpace(member.ID)] {
			return true
		}
	}
	return false
}

// agentDirectoryArrived is true once the selected conversation's agent
// directory read has ended, with an answer or with a failure: after that the
// page has nothing more to wait for, and an author the directory did not name
// keeps the generic label.
func agentDirectoryArrived(m Model) bool {
	if m.PersonaLookupConversationID != m.SelectedID {
		return false
	}
	return m.PersonaLookup == PersonaLookupReady || m.PersonaLookup == PersonaLookupFailed
}

// agentMessageAuthor is the author line of an agent's message: its name, or a
// neutral mark while the name is not known. The word "Agent" is the badge's, not
// the author's.
func agentMessageAuthor(msg Message) ui.Node {
	name := strings.TrimSpace(msg.Author)
	if name == "" {
		return html.Strong(html.Props{Class: "message-author agent-name-pending", Text: "…", Aria: map[string]string{"busy": "true"}})
	}
	return html.Strong(html.Props{Class: "message-author", Text: msg.Author})
}
