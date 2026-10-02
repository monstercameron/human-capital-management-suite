package main

import "testing"

// TestTodo_AGENTUX_054_Browser reads the owner's "Simulate a customer email"
// control: the form, its fault and replay states and the client that posts the
// email to the server, which never carries a tenant or a verified sender.
func TestTodo_AGENTUX_054_Browser(t *testing.T) {
	TestAgentUXDemo_Inbox_Browser(t)
	TestAgentUXDemo_InboxClient(t)
}
