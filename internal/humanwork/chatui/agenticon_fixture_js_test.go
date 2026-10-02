//go:build js && wasm

package chatui

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"

// AgentIconFixture is the stored icon a test gives an agent; see the _other
// twin of this file. agenticon.Generate needs the keyword vocabulary, which
// is left out of the browser build, so here the icon is the fallback for the
// agent's name.
func AgentIconFixture(input agenticon.Input) agenticon.Value {
	return agenticon.Fallback(input.Name)
}
