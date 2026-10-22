package chatui_test

import "testing"

// TestTodo_AGENTUX_067_Browser checks the Task Catcher card's private and public
// offers, localized copy and action states.
func TestTodo_AGENTUX_067_Browser(t *testing.T) {
	runAgentUXAmbientCardsBrowser(t, "TASK")
}

// TestTodo_AGENTUX_068_Browser checks Reminder's private and public offers,
// missing-time/source-change prompts, and the scheduled reminder actions.
func TestTodo_AGENTUX_068_Browser(t *testing.T) {
	runAgentUXAmbientCardsBrowser(t, "REMINDER")
}
