package main

import "testing"

// TestTodo_AGENTUX_054_Browser checks the rendered owner control and the
// authenticated transport contract used to submit its untrusted email.
func TestTodo_AGENTUX_054_Browser(t *testing.T) {
	assertAgentUXDemoInboxBrowser(t)
	assertAgentUXDemoInboxTransport(t)
}
