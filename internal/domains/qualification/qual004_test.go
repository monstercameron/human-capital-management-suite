package qualification

import (
	"testing"
)

func TestTodo_QUAL_004(t *testing.T) {
	satisfied := Evaluation{
		RequirementID: "req-1", Revision: 1,
		Results: []RequirementResult{
			{Kind: RequirementCredential, Ref: "credential:nursing-license", RequiredLevel: 2, Status: StatusSatisfied, EvidenceRef: "evidence:license-1"},
			{Kind: RequirementSkill, Ref: "skill:triage", RequiredLevel: 1, Status: StatusSatisfied, EvidenceRef: "evidence:assessment-1"},
		},
	}
	status := mustOverallStatus(t, satisfied)
	if status.Overall != OverallQualified {
		t.Fatalf("overall=%v", status.Overall)
	}
	if len(status.Satisfied) != 2 || len(status.Missing) != 0 {
		t.Fatalf("explanation: %+v", status)
	}
	// Seeded defect: expired evidence must never return QUALIFIED, and
	// the rejection names field/state/version with zero side effects.
	expired := Evaluation{
		RequirementID: "req-1", Revision: 1,
		Results: []RequirementResult{
			{Kind: RequirementCredential, Ref: "credential:nursing-license", RequiredLevel: 2, Status: StatusUnsatisfied, Gap: "evidence expired"},
		},
	}
	got, err := Summarize(expired)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if got.Overall != OverallNotQualified {
		t.Fatalf("expired evidence overall=%v", got.Overall)
	}
	if _, err := Summarize(Evaluation{}); err == nil {
		t.Fatal("empty evaluation summarized")
	} else if rejected, ok := AsStatusRejected(err); !ok || rejected.Code != StatusRejectedCode {
		t.Fatalf("expected QUAL_004_REJECTED, got %v", err)
	} else if rejected.Field == "" || rejected.Version == 0 {
		t.Fatalf("rejection lacks field/version: %+v", rejected)
	}
}
