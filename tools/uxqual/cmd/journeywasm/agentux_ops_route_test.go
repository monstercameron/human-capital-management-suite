package main

import (
	"os"
	"strings"
	"testing"
)

func TestAgentControlsAgentRolloutRouteGate(t *testing.T) {
	if !agentOperationsRoute("/workspace/app/admin/agents") {
		t.Fatal("agent operations route was not admitted")
	}
	for _, path := range []string{"/workspace/app/chat/agents", "/workspace/app/admin/personas", "/workspace/app/admin/agents/", ""} {
		if agentOperationsRoute(path) {
			t.Fatalf("owner APIs admitted from %q", path)
		}
	}
}

func TestAgentUXOps3_SelectedTabPersistsInURL(t *testing.T) {
	for query, want := range map[string]string{
		"tab=running":  "running",
		"tab=rollout":  "rollout",
		"tab=move":     "move",
		"locale=de-DE": "running",
		"tab=unknown":  "running",
		"%zz":          "running",
	} {
		if got := agentOperationsSelectedTab(query); got != want {
			t.Errorf("agentOperationsSelectedTab(%q) = %q, want %q", query, got, want)
		}
	}
}

func TestAgentControlsAgentRolloutWiringChecksRouteBeforeMount(t *testing.T) {
	for _, test := range []struct{ file, finder string }{{"agent_controls_wasm.go", "func findAgentControlsMount()"}, {"agent_rollout_portable_wasm.go", "func findRolloutPortableMount()"}} {
		source, err := os.ReadFile(test.file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		start := strings.Index(text, test.finder)
		if start < 0 {
			t.Fatalf("%s is missing %s", test.file, test.finder)
		}
		text = text[start:]
		gate := strings.Index(text, `agentOperationsRoute(js.Global().Get("location").Get("pathname").String())`)
		mount := strings.Index(text, `Call("getElementById", "agent-`)
		if gate < 0 || mount < 0 || gate > mount {
			t.Fatalf("%s does not reject non-operations routes before finding an API-backed mount", test.file)
		}
	}
}
