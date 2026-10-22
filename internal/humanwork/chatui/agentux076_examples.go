package chatui

// AgentExampleQuestions are every built-in example question the empty
// conversation with an agent offers, in the reader's language: the general set
// and the policy set, in that order (AGENTUX-076). The server-side golden suite
// asks each of them of a scripted agent, so a question the page suggests can
// never be one the agent was not tested on.
func AgentExampleQuestions(locale string) []string {
	keys := []string{"general_1", "general_2", "general_3", "policy_1", "policy_2", "policy_3"}
	questions := make([]string, 0, len(keys))
	for _, key := range keys {
		questions = append(questions, chatux024Text(locale, key))
	}
	return questions
}
