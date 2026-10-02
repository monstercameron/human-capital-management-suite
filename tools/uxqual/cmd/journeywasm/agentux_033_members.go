package main

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// AGENTUX-033: an agent that is a member of a channel came back from the member
// read like any other member, so the details panel listed it among the people
// (under whatever the people directory made of its identifier) and again among
// the agents. The agents of the conversation are known from the agent
// directory; a member whose identity is one of them is an agent.

// agentUX033MarkAgentMembers marks the members that are agents of the
// conversation, by identity, and gives each the agent's own name and icon. It
// returns the slice it was given when nothing changed, and a new one otherwise:
// a render in flight may be reading the old.
func agentUX033MarkAgentMembers(members []chatui.Member, agents []chatui.ResolvedPersonaMention) ([]chatui.Member, bool) {
	if len(members) == 0 || len(agents) == 0 {
		return members, false
	}
	byID := make(map[string]chatui.ResolvedPersonaMention, len(agents))
	for _, agent := range agents {
		if id := agent.Reference.ID; id != "" && strings.TrimSpace(agent.Reference.Display) != "" {
			byID[id] = agent
		}
	}
	var marked []chatui.Member
	for index, member := range members {
		agent, is := byID[member.ID]
		if !is {
			continue
		}
		next := member
		next.Agent, next.Name = true, agent.Reference.Display
		if !next.Icon.Valid() && agent.Icon.Valid() {
			next.Icon, next.IconRevision = agent.Icon, agent.IconRevision
		}
		if next == member {
			continue
		}
		if marked == nil {
			marked = append([]chatui.Member(nil), members...)
		}
		marked[index] = next
	}
	if marked == nil {
		return members, false
	}
	return marked, true
}
