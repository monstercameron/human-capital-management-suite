package matching

import (
	"testing"
)

// TestTodo_MATCH_004_Property: ties break by candidate reference in a
// stable order and satisfied weights reproduce exactly.
func TestTodo_MATCH_004_Property(t *testing.T) {
	request, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	result := mustMatch(t, request, []CandidateFacts{
		candidateFact(t, "008", "new-york", "90.00", true),
		candidateFact(t, "006", "new-york", "90.00", true),
		candidateFact(t, "007", "new-york", "90.00", true),
	})
	if len(result.Matches) != 3 {
		t.Fatalf("matches=%d", len(result.Matches))
	}
	for i := 1; i < len(result.Matches); i++ {
		prev, curr := result.Matches[i-1], result.Matches[i]
		if prev.Score.Total < curr.Score.Total {
			t.Fatal("ranking is not score-descending")
		}
		if prev.Score.Total == curr.Score.Total &&
			prev.CandidateRef.String() >= curr.CandidateRef.String() {
			t.Fatal("tied scores did not break by candidate reference")
		}
	}
	// Satisfied soft weight reproduces exactly: 1 qualification point
	// plus the full cost-ceiling weight.
	if got := result.Matches[0].Score.Total; got != 6 {
		t.Fatalf("total=%d, want 6", got)
	}
	if result.ScoreVersion != 1 || result.TieBreak != TieBreakCandidateRef {
		t.Fatalf("ranking contract = %d/%v", result.ScoreVersion, result.TieBreak)
	}
}

// TestTodo_MATCH_004_Security: the fairness reference is descriptive
// only — swapping it never changes a score, and no factor references
// an undeclared kind that could proxy a sensitive attribute.
func TestTodo_MATCH_004_Security(t *testing.T) {
	request, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	facts := []CandidateFacts{
		candidateFact(t, "006", "new-york", "90.00", true),
		candidateFact(t, "007", "new-york", "90.00", false),
	}
	first := mustMatch(t, request, facts)
	relabeled := validMatchRequest(t)
	relabeled.Fairness = FairnessPolicy{PolicyRef: "policy:other-fairness", Version: "v9"}
	otherRequest, err := NewMatchRequest(relabeled)
	if err != nil {
		t.Fatal(err)
	}
	second := mustMatch(t, otherRequest, facts)
	for i := range first.Matches {
		if first.Matches[i].Score.Total != second.Matches[i].Score.Total {
			t.Fatal("fairness reference changed a score")
		}
	}
	declared := map[ConstraintKind]bool{ConstraintQualification: true}
	for _, c := range request.Constraints {
		declared[c.Kind] = true
	}
	for _, match := range first.Matches {
		for _, factor := range match.Score.Factors {
			if !declared[factor.Kind] {
				t.Fatalf("factor references undeclared kind %v", factor.Kind)
			}
		}
	}
}
