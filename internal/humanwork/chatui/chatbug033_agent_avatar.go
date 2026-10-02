package chatui

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// agentIconFor is the one answer to "which icon does this agent wear here": the
// icon the caller already holds, else the stored icon found anywhere in the model
// (storedAgentIcon), else the agent's own fallback taken from its id. While the
// server's agent list is still being read (AgentRailPending) it answers the zero
// value instead, which agentDMAvatar draws as an empty slot: a glyph shared by
// every agent is never drawn, not even for a moment, and a stored icon that is
// about to arrive is not first replaced by a made-up one.
func agentIconFor(m Model, ids []string, name string, stored agenticon.Value) agenticon.Value {
	if stored.Valid() {
		return stored
	}
	if found := storedAgentIcon(m, ids, name); found.Valid() {
		return found
	}
	if m.AgentRailPending {
		return agenticon.Value{}
	}
	known := agentFallbackSeeds(m)
	if len(ids) == 0 {
		ids = agentIDsByName(m, name)
	}
	seed := ""
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if seed == "" {
			seed = id
		}
		for _, other := range known {
			if other == id {
				seed = id
				break
			}
		}
	}
	if seed == "" {
		seed = strings.TrimSpace(name)
	}
	return agenticon.ValueFor(agenticon.Value{}, seed, known)
}

// agentFallbackSeeds lists the ids of every agent the model knows, so that two
// agents with no stored icon are given two different fallbacks.
func agentFallbackSeeds(m Model) []string {
	seen := map[string]bool{}
	seeds := []string{}
	add := func(id string) {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			seeds = append(seeds, id)
		}
	}
	for _, c := range m.Conversations {
		if c.Agent {
			add(c.AgentID)
		}
	}
	for _, member := range m.Members {
		if member.Agent {
			add(member.ID)
		}
	}
	for _, persona := range m.ResolvedPersonaMentions {
		add(persona.Reference.ID)
	}
	return seeds
}

// agentIDsByName finds the id of an agent a surface knows only by name (the
// private answer and progress rows), so the same agent has the same fallback
// there as everywhere else.
func agentIDsByName(m Model, name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	for _, c := range m.Conversations {
		if c.Agent && strings.EqualFold(strings.TrimSpace(c.Name), name) {
			return []string{c.AgentID}
		}
	}
	for _, persona := range m.ResolvedPersonaMentions {
		if strings.EqualFold(strings.TrimSpace(persona.Reference.Display), name) {
			return []string{persona.Reference.ID}
		}
	}
	return nil
}

// conversationAgentIcon is the icon a direct conversation with an agent wears in
// the rail, the header and the details panel.
func conversationAgentIcon(m Model, c Conversation) agenticon.Value {
	return agentIconFor(m, []string{c.AgentID}, c.Name, c.Icon)
}

// agentConversationName is an agent conversation's name when its own record
// carries none: the display name in the resolved mention directory.
func agentConversationName(m Model, c Conversation) string {
	if strings.TrimSpace(c.AgentID) == "" {
		return ""
	}
	for _, persona := range m.ResolvedPersonaMentions {
		if persona.Reference.ID == c.AgentID {
			return strings.TrimSpace(persona.Reference.Display)
		}
	}
	return ""
}

// agentDirectHeaderLine is the line under an agent's name in a direct
// conversation with it: the agent's own description, then the fact that only the
// viewer sees the conversation. Until the description is known the line is the
// second part alone; another agent's description is never shown.
func agentDirectHeaderLine(m Model, c Conversation) string {
	note := agentReplyFallback(m.Locale, "chat.agent.private_note", "Only you can see this conversation")
	if purpose := agentConversationPurpose(m, c); purpose != "" {
		return purpose + " · " + note
	}
	return note
}

// agentConversationPurpose is the agent's own description for a direct
// conversation: the one the server stored with the conversation row, else the
// one in the resolved mention directory. It is empty until one of them is known,
// and no other agent's line is ever borrowed.
func agentConversationPurpose(m Model, c Conversation) string {
	if purpose := strings.TrimSpace(c.AgentPurpose); purpose != "" {
		return purpose
	}
	if strings.TrimSpace(c.AgentID) == "" {
		return ""
	}
	for _, persona := range m.ResolvedPersonaMentions {
		if persona.Reference.ID == c.AgentID {
			return strings.TrimSpace(persona.Purpose)
		}
	}
	return ""
}
