package main

import (
	"os"
	"strings"
	"testing"
)

// TestTodo_AGENTUX_081_RetryWiring pins the other half of the shared retry
// state: every Agents-page read whose failure renders Try again answers the
// button with a fresh read. The render half is pinned by
// TestTodo_AGENTUX_081 in productui; the click itself is specified in
// tools/uxqual/browser/agentux081_agent_surfaces.spec.mjs.
func TestTodo_AGENTUX_081_RetryWiring(t *testing.T) {
	for _, test := range []struct {
		file    string
		handler string
		retry   string
		reread  string
	}{
		{"agentcost006_wasm.go", "func handleAgentCostClick", `"retry"`, "go loadAgentCost(mount)"},
		{"agent2019_wasm.go", "func handleAgentAdminClick", `case "retry":`, "runAgentAdmin(mount, http.MethodGet"},
		{"agent2018_wasm.go", "func runAgentAccessAction", `case "retry":`, "finish("},
		{"agentcost006_wasm.go", "func handleAgentSpendClick", `"retry"`, "findAgentSpendMounts()"},
	} {
		source, err := os.ReadFile(test.file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		start := strings.Index(text, test.handler)
		if start < 0 {
			t.Fatalf("%s is missing %s", test.file, test.handler)
		}
		text = text[start:]
		retry, reread := strings.Index(text, test.retry), strings.Index(text, test.reread)
		if retry < 0 || reread < 0 || reread < retry {
			t.Fatalf("%s %s does not answer Try again with a fresh read", test.file, test.handler)
		}
	}
}
