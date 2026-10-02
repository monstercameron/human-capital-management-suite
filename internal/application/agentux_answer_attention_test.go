package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

func TestAgentUXQuality_OwnerAttention(t *testing.T) {
	base := runstate.Run{State: runstate.StateFailed, TenantID: "tenant", AgentID: "agent", AgentVersion: "1", TerminalCode: "MODEL_BINDING_INVALID", FailureGate: string(runstate.FailureGateModelRoute), FailureOwner: "application", FailureLocation: "route.go:10"}
	newVersion := base
	newVersion.AgentVersion = "2"
	newCause := base
	newCause.TerminalCode, newCause.FailureGate = "TOOL_POLICY_UNAVAILABLE", string(runstate.FailureGateToolScope)
	working := base
	working.State = runstate.StateRunning
	rows := AgentUXAnswerNeedsAttention([]runstate.Run{base, base, newVersion, newCause, working})
	if len(rows) != 3 {
		t.Fatalf("repeated cause/version not collapsed: %+v", rows)
	}
	for _, row := range rows {
		if row.Owner != "application" || row.Location != "route.go:10" || row.TenantID != "tenant" {
			t.Fatalf("owner detail lost: %+v", row)
		}
	}
}
