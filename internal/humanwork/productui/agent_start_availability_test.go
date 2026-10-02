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
		// The Agents page is an employee's page: it says agents cannot answer and
		// who to turn to, not how to configure a model provider.
		if !ready && (!strings.Contains(markup, "Agents cannot answer right now.") || !strings.Contains(markup, `data-start-unavailable-reason="model_unavailable"`) || strings.Contains(markup, "model provider")) {
			t.Fatalf("missing or owner-worded unavailable reason: %s", markup)
		}
	}
}
