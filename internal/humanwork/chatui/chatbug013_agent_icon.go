package chatui

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// storedAgentIcon finds the icon the directory stored for one agent, wherever
// this model carries it: the post's own receipt, the selected conversation,
// the conversation and member lists, and the resolved mention directory. The
// ids come from trusted actor data; the name is used only when no id matches,
// because the private answer and progress rows know their agent by name alone.
// The zero value means the agent truly has no stored icon.
func storedAgentIcon(m Model, ids []string, name string) agenticon.Value {
	name = strings.TrimSpace(name)
	known := func(id string) bool {
		id = strings.TrimSpace(id)
		if id == "" {
			return false
		}
		for _, candidate := range ids {
			if strings.TrimSpace(candidate) == id {
				return true
			}
		}
		return false
	}
	named := func(candidate string) bool {
		return name != "" && strings.EqualFold(strings.TrimSpace(candidate), name)
	}
	selected := m.selected()
	conversations := make([]Conversation, 0, len(m.Conversations)+1)
	conversations = append(conversations, selected)
	conversations = append(conversations, m.Conversations...)
	for _, byID := range []bool{true, false} {
		for _, c := range conversations {
			if c.Agent && c.Icon.Valid() && (byID && known(c.AgentID) || !byID && named(c.Name)) {
				return c.Icon
			}
		}
		for _, member := range m.Members {
			if member.Agent && member.Icon.Valid() && (byID && known(member.ID) || !byID && named(member.Name)) {
				return member.Icon
			}
		}
		for _, persona := range m.ResolvedPersonaMentions {
			if persona.Icon.Valid() && (byID && known(persona.Reference.ID) || !byID && named(persona.Reference.Display)) {
				return persona.Icon
			}
		}
	}
	return agenticon.Value{}
}
