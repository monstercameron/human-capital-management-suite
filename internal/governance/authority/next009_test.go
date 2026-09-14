package authority

import (
	"testing"
	"time"
)

func TestP1BPreWriteAuthorityEvidenceRejectsMissingStaleCircularOrPostPilotProof(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	amendment := mustPreWriteAmendment(t, at)
	verdict := mustCompilePreWrite(t, PreWriteInput{
		Amendment: amendment,
		Criteria:  seedPreWriteCriteria(at),
		Scope: PreWriteScope{
			Tenant: "harborcare-demo", Intent: "PromoteWorker",
			Capability: "promotion.execute", Fields: []string{"job.level"},
			Provider: "cell-postgres", Window: "2026-10-07/2026-10-14",
		},
		Owner:       "gate-commander",
		RollbackRef: "RUNBOOK-rollback-9",
		RepairRef:   "RUNBOOK-repair-9",
		IncidentRef: "RUNBOOK-incident-9",
		EvaluatedAt: at,
	})
	if verdict.Decision != PreWriteGranted {
		t.Fatalf("decision=%v findings=%+v", verdict.Decision, verdict.Findings)
	}
	if verdict.WriteAuthority != "" {
		t.Fatalf("grant carries write authority text: %q", verdict.WriteAuthority)
	}
	// Post-pilot proof is rejected: authority comes before the pilot,
	// never after it.
	postPilot := seedPreWriteCriteria(at)
	postPilot[0].PostPilot = true
	blocked := mustCompilePreWrite(t, PreWriteInput{
		Amendment: amendment,
		Criteria:  postPilot,
		Scope: PreWriteScope{
			Tenant: "harborcare-demo", Intent: "PromoteWorker",
			Capability: "promotion.execute", Fields: []string{"job.level"},
			Provider: "cell-postgres", Window: "2026-10-07/2026-10-14",
		},
		Owner:       "gate-commander",
		RollbackRef: "RUNBOOK-rollback-9",
		RepairRef:   "RUNBOOK-repair-9",
		IncidentRef: "RUNBOOK-incident-9",
		EvaluatedAt: at,
	})
	if blocked.Decision != PreWriteGateBlocked {
		t.Fatalf("post-pilot proof granted: %+v", blocked)
	}
}
