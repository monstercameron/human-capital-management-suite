package chatui

// chatux001AgentCount is the number of agents the header subtitle states. The
// roster of the open conversation decides while it holds agents; while it does
// not (the agent read has not arrived, lost a race, or failed and was cleared)
// the count the last good read left is used, so the subtitle draws the agents as
// soon as the roster arrives and never drops them afterwards.
func chatux001AgentCount(m Model) int {
	if n := len(chatConversationAgents(m)); n > 0 {
		return n
	}
	return m.AgentCounts[m.SelectedID]
}

// ConversationAgentCount is how many agents a model's roster holds for its open
// conversation, counted the way the header counts them. The browser build stores
// it in AgentCounts when an agent read succeeds.
func ConversationAgentCount(m Model) int { return len(chatConversationAgents(m)) }
