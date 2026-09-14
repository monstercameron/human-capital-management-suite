package matching

import (
	"context"
	"testing"
)

// TestTodo_MATCH_003: a hard qualification failure excludes the
// candidate from outranking anyone — no soft score overrides a hard
// constraint.
func TestTodo_MATCH_003(t *testing.T) {
	request, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	port, err := NewInMemoryCandidateFacts([]CandidateFacts{
		// Eligible but expensive: soft cost ceiling fails, total 1.
		candidateFact(t, "006", "new-york", "150.00", true),
		// Unqualified: hard failure, yet soft cost satisfies, total 5.
		candidateFact(t, "007", "new-york", "90.00", false),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Match(context.Background(), port, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 2 {
		t.Fatalf("matches=%d", len(result.Matches))
	}
	// Seeded defect: pure score ordering ranks the ineligible
	// candidate first on soft points alone.
	if got := result.Matches[0].CandidateRef; got != matchingRef("candidate", "006") {
		t.Fatalf("rank 1 = %v, want eligible candidate 006", got)
	}
	ineligible := result.Matches[1]
	if ineligible.Eligible {
		t.Fatal("unqualified candidate remained eligible")
	}
	found := false
	for _, satisfaction := range ineligible.Satisfactions {
		if satisfaction.Status == SatisfactionUnsatisfied && satisfaction.Mode == ConstraintHard &&
			satisfaction.Reason == ReasonQualificationMissing {
			found = true
		}
	}
	if !found {
		t.Fatalf("ineligible candidate carries no hard qualification reason: %+v", ineligible.Satisfactions)
	}
	if err := result.Validate(); err != nil {
		t.Fatal(err)
	}
}
