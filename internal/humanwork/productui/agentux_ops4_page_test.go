package productui

import (
	"html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestAgentUXOps4_FailedRecentRunShowsGateActionAndTechnicalDetails(t *testing.T) {
	run := AgentControlRun{
		ID:           "failed-run",
		Name:         "Policy Helper",
		Version:      "2",
		State:        "FAILED",
		Started:      "2026-10-01T12:58:00Z",
		Duration:     "45s",
		RequestedBy:  "Walt Brennan",
		Location:     "#benefits",
		Failure:      "TOOL_EXECUTION_FAILED",
		FailureGate:  "tool_scope",
		FailureOwner: "internal/application",
		FailurePlace: "persona_runtime_tools.go:37",
	}
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		reason, action := agentOpsFailureCopy(locale, "tool_scope")
		markup := html.UnescapeString(renderAgentOperationsTest(t, agentOpsRecentRun(locale, run)))
		for _, want := range []string{reason, action, "Policy Helper", "Walt Brennan", "#benefits"} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s failed run missing %q: %s", localeID, want, markup)
			}
		}
		for _, forbidden := range []string{"request body", "document body", "model output", "tool_scope", "internal/application", "persona_runtime_tools.go:37"} {
			if strings.Contains(markup, forbidden) {
				t.Fatalf("%s failed run leaked %q: %s", localeID, forbidden, markup)
			}
		}
	}
}

func TestAgentUXOps4_RolloutCanaryEmptyAndOnlySelectedRows(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	markup, err := ui.RenderToString(AgentRolloutPortableMount(locale, AgentRolloutSnapshot{
		Available:  true,
		CanPreview: true,
		Personas:   []AgentRolloutPersona{{ID: "policy-helper", Name: "Policy Helper"}},
		Versions:   []AgentRolloutVersion{{PersonaID: "policy-helper", Version: 3, PublishedAt: "2026-09-30", Current: true}},
		Installations: []AgentRolloutInstallation{
			{ID: "general", PersonaID: "policy-helper", Name: "general", ConversationKind: "PUBLIC_CHANNEL", MemberCount: 18, Visible: true, Version: 2, CanaryEligible: true},
			{ID: "direct", PersonaID: "policy-helper", Name: "Policy Helper", ConversationKind: "DIRECT", MemberCount: 2, Visible: true, Version: 2, CanaryEligible: true},
		},
	}, AgentPortableSnapshot{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "At least one conversation is updated first.") {
		t.Fatalf("canary empty sentence missing: %s", markup)
	}
	for _, id := range []string{"general", "direct"} {
		if !strings.Contains(markup, `data-rollout-canary-row="`+id+`" hidden`) {
			t.Fatalf("canary row %s should be hidden until its conversation is ticked: %s", id, markup)
		}
	}
	if !strings.Contains(markup, "Your conversation with Policy Helper") || strings.Contains(markup, "A conversation you cannot see") {
		t.Fatalf("direct agent conversation label regressed: %s", markup)
	}
}
