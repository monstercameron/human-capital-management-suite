package chatui_test

import "testing"

// TestTodo_AGENTUX_067_Browser reads the cards an agent offers under a message:
// the private card only its person sees ("Add to my tasks"), and the public card
// of the channel to-do list, in each language, at each width and in each theme.
func TestTodo_AGENTUX_067_Browser(t *testing.T) {
	TestAgentUXAmbient_Cards_Browser(t)
	TestAgentUXAmbient_Cards_Security(t)
}

// TestTodo_AGENTUX_068_Browser reads the reminder cards: offered once in the
// channel, set, needing a time and for a source message that changed.
func TestTodo_AGENTUX_068_Browser(t *testing.T) {
	TestAgentUXAmbient_Cards_Browser(t)
	TestAgentUXAmbient_Cards_Security(t)
}
