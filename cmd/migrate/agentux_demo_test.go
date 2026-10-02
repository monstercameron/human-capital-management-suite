package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
)

func TestTodo_AGENTUX_005_Browser(t *testing.T) {
	line := formatAgentDemoSummary(application.LocalAgentDemoSummary{DisplayName: "Policy Helper", Version: 4, State: "PUBLISHED", AssistantVersion: 1, AssistantState: "PUBLISHED"})
	if line != "Policy Helper v4: PUBLISHED. Assistant v1: PUBLISHED. already prepared; no changes. Documents: Paid time off policy; 2026 holiday guide." {
		t.Fatalf("idempotent human summary=%q", line)
	}
	if strings.Contains(line, "hcmnext.local") || strings.Contains(line, "AGENTUX") {
		t.Fatalf("summary exposes internal identifiers: %q", line)
	}
	repaired := formatAgentDemoSummary(application.LocalAgentDemoSummary{DisplayName: "Policy Helper", Version: 4, State: "PUBLISHED", ChatConversationRepaired: true})
	if !strings.Contains(repaired, "chat_repair=true") || strings.Contains(repaired, "already prepared") {
		t.Fatalf("repair receipt=%q", repaired)
	}
	placed := formatAgentDemoSummary(application.LocalAgentDemoSummary{DisplayName: "Policy Helper", Version: 4, State: "PUBLISHED", PolicyDocumentPlacements: 2})
	if !strings.Contains(placed, "policy_document_placements=2") || strings.Contains(placed, "already prepared") {
		t.Fatalf("policy placement receipt=%q", placed)
	}
	refreshed := formatAgentDemoSummary(application.LocalAgentDemoSummary{DisplayName: "Policy Helper", Version: 4, State: "PUBLISHED", InstallationsRefreshed: 1})
	if !strings.Contains(refreshed, "installations_refreshed=1") || strings.Contains(refreshed, "already prepared") {
		t.Fatalf("refresh receipt=%q", refreshed)
	}
	holiday := formatAgentDemoSummary(application.LocalAgentDemoSummary{DisplayName: "Policy Helper", Version: 4, State: "PUBLISHED", HolidayDocumentPlacements: 1})
	if !strings.Contains(holiday, "holiday_guide_placements=1") || !strings.Contains(holiday, "2026 holiday guide") {
		t.Fatalf("holiday receipt=%q", holiday)
	}
}

func TestTodo_AGENTUX_005_Command(t *testing.T) {
	command, rest := splitCommand([]string{agentDemoCommand, "-profile", "local-dev"})
	if command != agentDemoCommand || len(rest) != 2 {
		t.Fatalf("agent-demo command=%q rest=%q", command, rest)
	}
	spec := agentDemoSpec(rest)
	want := map[string]string{
		fieldAgentDatabaseURL:           "HCMNEXT_AGENT_DATABASE_URL",
		fieldChatDatabaseURL:            EnvChatDatabaseURL,
		fieldDocumentDatabaseURL:        EnvDocumentDatabaseURL,
		fieldReviewAuthorityDatabaseURL: "HCMNEXT_PERSONA_REVIEW_AUTHORITY_DATABASE_URL",
		fieldPolicyAuthorityDatabaseURL: application.EnvLocalAgentPolicyAuthorityDatabaseURL,
		fieldRouteAuthorityDatabaseURL:  "HCMNEXT_PERSONA_MODEL_ROUTE_AUTHORITY_DATABASE_URL",
		fieldEvalProvisionerDatabaseURL: "HCMNEXT_AGENT_EVAL_PROVISIONER_DATABASE_URL",
		fieldAgentOwnerDatabaseURL:      "HCMNEXT_AGENT_OWNER_DATABASE_URL",
	}
	for _, field := range spec.ConfigFields {
		if expected, ok := want[field.Name]; ok && field.Env == expected {
			delete(want, field.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("agent-demo is missing database environment fallbacks: %v", want)
	}
}
