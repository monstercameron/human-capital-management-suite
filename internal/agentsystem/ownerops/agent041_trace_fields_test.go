package ownerops

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

// TestTodo_AGENT_041_TaskTraceFields: the task trace carries what caused the
// run and which incident its stop evidence references. The cause is a bounded
// code; the incident reference passes through for API correlation while the
// page renders only its presence, never the free text.
func TestTodo_AGENT_041_TaskTraceFields(t *testing.T) {
	records := []TaskRecord{
		{
			Task:       agentrun.AgentTask{ID: "task-a", TenantID: "tenant-a", UserID: "user-a", State: agentrun.StatePaused},
			OwnerID:    "owner-a",
			AgentID:    "agent-a",
			Version:    "4",
			CauseKind:  " schedule ",
			IncidentID: "INC-1042",
		},
		{
			Task:       agentrun.AgentTask{ID: "task-b", TenantID: "tenant-a", UserID: "user-a", State: agentrun.StateRunning},
			OwnerID:    "owner-a",
			AgentID:    "agent-a",
			Version:    "4",
			CauseKind:  "<script>alert(1)</script>",
			IncidentID: "task-b has no incident",
		},
	}
	scope := Scope{Audience: AudienceOwner, TenantID: "tenant-a", SubjectID: "owner-a", Purpose: PurposeOwnerDashboard, Capabilities: []string{CapabilityRead, CapabilityPause}}
	views, err := ProjectTasks(scope, records)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 {
		t.Fatalf("views = %d, want 2", len(views))
	}
	if views[0].CauseKind != "SCHEDULE" || views[0].IncidentID != "INC-1042" {
		t.Fatalf("trace fields = %q %q, want SCHEDULE INC-1042", views[0].CauseKind, views[0].IncidentID)
	}
	if views[1].CauseKind != "OTHER" {
		t.Fatalf("hostile cause = %q, want OTHER", views[1].CauseKind)
	}
}
