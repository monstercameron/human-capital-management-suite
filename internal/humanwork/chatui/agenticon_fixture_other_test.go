//go:build !(js && wasm)

package chatui

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"

// AgentIconFixture is the stored icon a test gives an agent. On the server it
// is the generated icon, which is what an agent's stored icon is. The keyword
// generator is not built for the browser, so the _js twin of this file returns
// the agent's fallback icon instead: a valid icon from the same set, which is
// all these tests need of a stored one. It is exported so the tests in package
// chatui_test use the same fixture.
func AgentIconFixture(input agenticon.Input) agenticon.Value {
	return agenticon.Generate(input)
}
