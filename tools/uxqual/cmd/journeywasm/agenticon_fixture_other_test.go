//go:build !(js && wasm)

package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"

// agentIconFixture is the stored icon a test gives an agent. On the server it
// is the generated icon, which is what an agent's stored icon is. The keyword
// generator is not built for the browser, so the _js twin of this file returns
// the agent's fallback icon instead: a valid icon from the same set, which is
// all these tests need of a stored one.
func agentIconFixture(input agenticon.Input) agenticon.Value {
	return agenticon.Generate(input)
}
