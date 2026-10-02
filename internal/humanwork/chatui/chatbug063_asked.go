package chatui

import "strings"

// CHATBUG-063: an ordinary message was followed by a failure card from an agent
// called "Persona", with a button that would have started a run. The server had
// recorded an outcome for a message that asked no agent, and the card filled in
// the name it was not given. Two rules end that here, whatever the server sends:
// an agent's state is drawn only under a message that itself asked an agent,
// read from the message as stored; and a card with no agent to name is not
// drawn at all.

// chatbug063AskedAgent reports whether the message postID asked an agent. In a
// conversation with an agent every message of the person does. Anywhere else
// the stored message must carry a mention of an agent placed in this
// conversation; a draft, another conversation or a guess from the text never
// counts. A message the page does not hold (its thread is still loading) cannot
// be judged, and the server's word stands for it.
func chatbug063AskedAgent(model Model, postID string) bool {
	message, found := chatbug040MessageByID(model, postID)
	if !found || model.selected().Agent {
		return true
	}
	for _, reference := range message.PersonaReferences {
		if reference.Kind != "AGENT_MENTION" || strings.TrimSpace(reference.ID) == "" {
			continue
		}
		if reference.ConversationID == "" || reference.ConversationID == model.SelectedID {
			return true
		}
	}
	return false
}
