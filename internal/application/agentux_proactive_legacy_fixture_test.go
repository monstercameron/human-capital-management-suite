package application

// Hide the newer audience-policy extension when reproducing the original
// malformed preparation row. TestTodo_AGENTUX_005_Integration exercises repair.
type proactiveLegacyPolicyStore struct{ LocalDevPersonaChatPolicyStore }
