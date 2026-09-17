package matching

import (
	"context"
	"strings"
	"testing"
)

// TestTodo_MATCH_006_Property: the explanation is deterministic, bound to
// the exact ranking it explains, closed-vocabulary and rank-preserving.
func TestTodo_MATCH_006_Property(t *testing.T) {
	request, result := explainFixture006(t)

	first := mustExplain006(t, request, result)
	second := mustExplain006(t, request, result)
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatal("identical explanation inputs replayed to different digests")
	}

	// Every explanation binds the exact ranking it gated.
	if first.RankingDigest != result.CanonicalDigest {
		t.Fatal("explanation did not preserve the ranking digest")
	}

	// Reasons stay inside the closed satisfaction vocabulary and never
	// name a candidate; unknowns stay sorted and distinct.
	seenUnknown := make(map[string]struct{}, len(first.Unknowns))
	previous := ""
	for _, unknown := range first.Unknowns {
		kind, ok := strings.CutPrefix(unknown, "soft-unsatisfied:")
		if !ok || !ConstraintKind(kind).Valid() {
			t.Fatalf("unknown outside the closed vocabulary: %q", unknown)
		}
		if _, dup := seenUnknown[unknown]; dup {
			t.Fatalf("duplicate unknown: %q", unknown)
		}
		seenUnknown[unknown] = struct{}{}
		if unknown < previous {
			t.Fatalf("unknowns are not sorted: %v", first.Unknowns)
		}
		previous = unknown
	}
	for _, decision := range first.Decisions {
		for _, satisfaction := range decision.Satisfactions {
			if !satisfaction.Reason.Valid() {
				t.Fatalf("reason outside the closed vocabulary: %q", satisfaction.Reason)
			}
		}
		for _, factor := range decision.Score.Factors {
			if !SatisfactionReason(factor.Reason).Valid() {
				t.Fatalf("factor reason outside the closed vocabulary: %q", factor.Reason)
			}
			for _, match := range result.Matches {
				if strings.Contains(factor.Reason, match.CandidateRef.Id) {
					t.Fatalf("factor reason leaks a candidate: %q", factor.Reason)
				}
			}
		}
	}

	// Decisions preserve ranking order with dense ranks.
	for i, decision := range first.Decisions {
		if decision.Rank != i+1 {
			t.Fatalf("decision %d has rank %d", i, decision.Rank)
		}
		if decision.CandidateRef != result.Matches[i].CandidateRef {
			t.Fatal("decisions do not preserve ranking order")
		}
	}
}

// TestTodo_MATCH_006_Mutation: seeded semantic mutants are killed — a
// rebound ranking, a flipped mode edge and a fabricated ranking all refuse
// or resolve on the documented side.
func TestTodo_MATCH_006_Mutation(t *testing.T) {
	request, result := explainFixture006(t)

	// A ranking rebound without a fresh digest refuses: ranks no longer
	// match positions, so the result itself is invalid.
	rebound := reseed006(result, func(out *MatchResult) {
		out.Matches[0], out.Matches[1] = out.Matches[1], out.Matches[0]
		out.Matches[0].Rank, out.Matches[1].Rank = 1, 2
	})
	if _, err := ExplainResult(request, rebound); err == nil {
		t.Fatal("rebound ranking explained without error")
	}

	// Mode edge: the same cost overrun as HARD excludes instead of
	// staying eligible with an unknown.
	hard := validMatchRequest(t)
	hard.Constraints[2].Mode = ConstraintHard
	hardRequest, err := NewMatchRequest(hard)
	if err != nil {
		t.Fatal(err)
	}
	port, err := NewInMemoryCandidateFacts([]CandidateFacts{
		candidateFact(t, "006", "new-york", "150.00", true),
	})
	if err != nil {
		t.Fatal(err)
	}
	hardResult, err := Match(context.Background(), port, hardRequest)
	if err != nil {
		t.Fatal(err)
	}
	hardExplained := mustExplain006(t, hardRequest, hardResult)
	if hardExplained.Decisions[0].Eligible {
		t.Fatal("hard cost overrun stayed eligible")
	}
	for _, unknown := range hardExplained.Unknowns {
		if strings.Contains(unknown, "COST_CEILING") {
			t.Fatalf("hard miss reported as unknown: %v", hardExplained.Unknowns)
		}
	}

	// Cost exactly at the ceiling satisfies, even as HARD.
	edge := validMatchRequest(t)
	edge.Constraints[2].Mode = ConstraintHard
	edgeRequest, err := NewMatchRequest(edge)
	if err != nil {
		t.Fatal(err)
	}
	edgePort, err := NewInMemoryCandidateFacts([]CandidateFacts{
		candidateFact(t, "006", "new-york", "100.00", true),
	})
	if err != nil {
		t.Fatal(err)
	}
	edgeResult, err := Match(context.Background(), edgePort, edgeRequest)
	if err != nil {
		t.Fatal(err)
	}
	edgeExplained := mustExplain006(t, edgeRequest, edgeResult)
	if !edgeExplained.Decisions[0].Eligible {
		t.Fatal("at-ceiling hard cost excluded the candidate")
	}
}
