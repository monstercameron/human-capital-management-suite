package chatui

import "strings"

// agentLimitConversation is the name of the open conversation as a sentence
// names it: "#general" for a channel, the conversation's own name otherwise. A
// direct conversation with the agent is named in the sentence already, so it
// gives "" and the card says the limit for the agent alone.
func agentLimitConversation(m Model) string {
	if m.SelectedID == "" {
		return ""
	}
	c := m.selected()
	name := strings.TrimSpace(displayName(m, c))
	switch {
	case name == "" || c.Kind == DirectMessage:
		return ""
	case c.Kind == PublicChannel:
		return "#" + strings.TrimPrefix(name, "#")
	}
	return name
}
