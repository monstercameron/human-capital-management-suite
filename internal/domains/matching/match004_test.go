package matching

import (
	"testing"
)

// TestTodo_MATCH_004: soft-preference ranking reproduces exactly —
// the result cites the score version and tie policy that produced it,
// and identical inputs replay to the identical digest.
func TestTodo_MATCH_004(t *testing.T) {
	request, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	facts := []CandidateFacts{
		candidateFact(t, "006", "new-york", "90.00", true),
		candidateFact(t, "007", "new-york", "80.00", true),
	}
	first := mustMatch(t, request, facts)
	// Seeded defect: the result does not cite its ranking contract,
	// so score components, weights, tie policy and version cannot be
	// shown to reproduce exactly.
	if first.ScoreVersion != request.Ranking.ScoreVersion {
		t.Fatalf("score version = %d, want %d", first.ScoreVersion, request.Ranking.ScoreVersion)
	}
	if first.TieBreak != request.Ranking.TieBreak {
		t.Fatalf("tie break = %v, want %v", first.TieBreak, request.Ranking.TieBreak)
	}
	second := mustMatch(t, request, facts)
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatal("identical ranking inputs replayed to different digests")
	}
	// No hidden features: every factor traces to a declared input.
	wantFactors := len(request.RequiredQualificationRefs) + len(request.Constraints)
	for _, match := range first.Matches {
		if len(match.Score.Factors) != wantFactors {
			t.Fatalf("candidate %v carries %d factors, want %d declared inputs",
				match.CandidateRef, len(match.Score.Factors), wantFactors)
		}
	}
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}
}
