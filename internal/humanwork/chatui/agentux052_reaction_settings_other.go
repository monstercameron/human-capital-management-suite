//go:build !js || !wasm

package chatui

// Without a browser there is no server to ask: native rendering tests draw the
// rows from a model and never run their effects.
func agentReactionSettingsCall(string, any, func(AgentReactionSettingsData, int, error)) func() {
	return nil
}
func agentReactionSettingsReset(string, bool) {}
