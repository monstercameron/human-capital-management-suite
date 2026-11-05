package application

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/ownerops"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
)

// TestTodo_AGENT_041_RunCauseIncidentEvals: the owner's run row carries the
// durable cause, the incident correlation and the bound version's evaluation
// outcome. Unknown causes and evaluations are withheld, never printed raw,
// and the incident reference passes through for API correlation while the
// page renders only its presence.
func TestTodo_AGENT_041_RunCauseIncidentEvals(t *testing.T) {
	started := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	full := projectAgentControlRun(AgentOwnerRunProjection{
		View:      ownerops.TaskView{TaskID: "run-1", State: "COMPLETED", CauseKind: "SCHEDULE", IncidentID: "INC-1042"},
		StartedAt: started, UpdatedAt: started.Add(9 * time.Second),
	}, AgentControlIdentity{Name: "Policy Helper", EvalStatus: "passed"})
	if full.Cause != "SCHEDULE" || full.Incident != "INC-1042" || len(full.Evals) != 1 || full.Evals[0] != "passed" {
		t.Fatalf("cause=%q incident=%q evals=%v", full.Cause, full.Incident, full.Evals)
	}
	expired := projectAgentControlRun(AgentOwnerRunProjection{
		View:      ownerops.TaskView{TaskID: "run-2", State: "COMPLETED"},
		StartedAt: started, UpdatedAt: started,
	}, AgentControlIdentity{EvalStatus: "expired"})
	if len(expired.Evals) != 1 || expired.Evals[0] != "expired" {
		t.Fatalf("expired evals=%v", expired.Evals)
	}
	unknown := projectAgentControlRun(AgentOwnerRunProjection{
		View:      ownerops.TaskView{TaskID: "run-3", State: "COMPLETED", CauseKind: "<script>alert(1)</script>"},
		StartedAt: started, UpdatedAt: started,
	}, AgentControlIdentity{EvalStatus: "<img src=x>"})
	if unknown.Cause != "" || len(unknown.Evals) != 0 || unknown.Incident != "" {
		t.Fatalf("hostile trace shows cause=%q incident=%q evals=%v", unknown.Cause, unknown.Incident, unknown.Evals)
	}
	empty := projectAgentControlRun(AgentOwnerRunProjection{
		View:      ownerops.TaskView{TaskID: "run-4", State: "COMPLETED"},
		StartedAt: started, UpdatedAt: started,
	}, AgentControlIdentity{})
	if empty.Cause != "" || len(empty.Evals) != 0 || empty.Incident != "" {
		t.Fatalf("a run with no trace shows cause=%q incident=%q evals=%v", empty.Cause, empty.Incident, empty.Evals)
	}
}

// TestTodo_AGENT_041_EvalStatusFromPublicationPin: the dashboard reads the
// evaluation the publication event pinned for the exact version: passed while
// its evidence is current, expired once it lapses, and unknown when the
// version was never published or the read fails. The display never gates.
func TestTodo_AGENT_041_EvalStatusFromPublicationPin(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	current := agentpersonastore.PublicationEvidenceDetails{EvaluationPassedAt: now.Add(-time.Hour), EvaluationExpiresAt: now.Add(time.Hour)}
	if got := agent041EvalStatus(current, nil, now); got != "passed" {
		t.Fatalf("current pin = %q, want passed", got)
	}
	lapsed := agentpersonastore.PublicationEvidenceDetails{EvaluationPassedAt: now.Add(-2 * time.Hour), EvaluationExpiresAt: now.Add(-time.Hour)}
	if got := agent041EvalStatus(lapsed, nil, now); got != "expired" {
		t.Fatalf("lapsed pin = %q, want expired", got)
	}
	if got := agent041EvalStatus(agentpersonastore.PublicationEvidenceDetails{}, agentpersonastore.ErrPublicationEvidenceRequired, now); got != "" {
		t.Fatalf("unpublished version = %q, want empty", got)
	}
}
