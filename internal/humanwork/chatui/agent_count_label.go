package chatui

// agentCountLabel names how many agents are in a conversation, with the noun
// the language uses for that count ("1 agent", never "1 agents").
func agentCountLabel(m Model, n int) string {
	return chatPlural(m.Locale, "agents", n)
}
