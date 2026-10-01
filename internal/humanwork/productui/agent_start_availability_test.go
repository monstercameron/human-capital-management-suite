package productui

import (
	"strings"
	"testing"
)

func TestTodo_AGENT_041_ComposerUsesRuntimeStartAvailability(t *testing.T) {
	for _, ready := range []bool{false, true} {
		markup := personaAdminRender(t, tagAgentComposer(ResolveProductLocale("en-US"), AgentSnapshot{StartAvailable: ready, StartUnavailableReason: "model_unavailable"}))
		if strings.Count(markup, " disabled") != map[bool]int{false: 2, true: 0}[ready] {
			t.Fatalf("ready=%v markup=%s", ready, markup)
		}
		if !ready && !strings.Contains(markup, "Connect a model provider") {
			t.Fatal("missing configuration reason")
		}
	}
}
