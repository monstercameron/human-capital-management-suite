package matching

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// explainFixture006 builds one eligible and one excluded candidate so the
// explanation must cover inclusion, ranking and exclusion in one result.
func explainFixture006(t *testing.T) (MatchRequest, MatchResult) {
	t.Helper()
	request, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	port, err := NewInMemoryCandidateFacts([]CandidateFacts{
		candidateFact(t, "006", "new-york", "90.00", true),
		candidateFact(t, "007", "boston", "150.00", false),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Match(context.Background(), port, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 2 || !result.Matches[0].Eligible || result.Matches[1].Eligible {
		t.Fatalf("fixture matches = %+v", result.Matches)
	}
	return request, result
}

// reseed006 deep-copies a result and recomputes every digest so a seeded
// defect keeps a valid binding and only the defect itself can fail.
func reseed006(result MatchResult, mutate func(*MatchResult)) MatchResult {
	out := result
	out.Matches = append([]CandidateMatch(nil), result.Matches...)
	for i := range out.Matches {
		out.Matches[i].Satisfactions = append([]ConstraintSatisfaction(nil), result.Matches[i].Satisfactions...)
		out.Matches[i].Score.Factors = append([]ScoreFactor(nil), result.Matches[i].Score.Factors...)
	}
	mutate(&out)
	for i := range out.Matches {
		out.Matches[i].CanonicalDigest = ""
		out.Matches[i].CanonicalDigest = out.Matches[i].computedDigest()
	}
	out.CanonicalDigest = out.computedDigest()
	return out
}

func mustExplain006(t *testing.T, request MatchRequest, result MatchResult) ResultExplanation {
	t.Helper()
	explained, err := ExplainResult(request, result)
	if err != nil {
		t.Fatal(err)
	}
	return explained
}

// TestTodo_MATCH_006: the explanation covers candidate source, every hard
// constraint, score components and tie/fairness rules with explicit
// unknowns, while a protected-attribute factor or a missing hard-constraint
// explanation rejects as MATCH_006_REJECTED with zero side effects.
func TestTodo_MATCH_006(t *testing.T) {
	request, result := explainFixture006(t)
	before := result.CanonicalDigest

	explained := mustExplain006(t, request, result)
	if explained.RequestID != request.RequestID || explained.RequestDigest != request.CanonicalDigest {
		t.Fatalf("explanation does not bind the request: %+v", explained)
	}
	if explained.RankingDigest != result.CanonicalDigest {
		t.Fatal("explanation does not bind the assessed ranking")
	}
	if explained.CandidateSource != request.CandidateSourceRef.String() {
		t.Fatalf("candidate source = %q, want %q", explained.CandidateSource, request.CandidateSourceRef.String())
	}
	for _, want := range []ConstraintKind{ConstraintLocation, ConstraintAvailabilityWindow, ConstraintQualification} {
		found := false
		for _, kind := range explained.HardConstraints {
			if kind == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("hard constraints = %v, missing %v", explained.HardConstraints, want)
		}
	}
	if explained.ScoreVersion != request.Ranking.ScoreVersion || explained.TieBreak != request.Ranking.TieBreak {
		t.Fatalf("ranking contract = %v/%v", explained.ScoreVersion, explained.TieBreak)
	}
	if explained.FairnessRef != request.Fairness.PolicyRef || explained.FairnessVersion != request.Fairness.Version {
		t.Fatalf("fairness contract = %v@%v", explained.FairnessRef, explained.FairnessVersion)
	}
	if len(explained.Decisions) != 2 || explained.Decisions[0].Rank != 1 || explained.Decisions[1].Rank != 2 {
		t.Fatalf("decisions = %+v", explained.Decisions)
	}
	if !explained.Decisions[0].Eligible || explained.Decisions[1].Eligible {
		t.Fatalf("eligibility = %+v", explained.Decisions)
	}
	excluded := explained.Decisions[1]
	hardMiss := false
	for _, satisfaction := range excluded.Satisfactions {
		if satisfaction.Mode == ConstraintHard && satisfaction.Status == SatisfactionUnsatisfied {
			hardMiss = true
		}
	}
	if !hardMiss {
		t.Fatalf("excluded decision names no hard miss: %+v", excluded)
	}
	var total int64
	for _, factor := range excluded.Score.Factors {
		total += factor.Points
	}
	if total != excluded.Score.Total {
		t.Fatal("score components do not sum to the score total")
	}
	foundUnknown := false
	for _, unknown := range explained.Unknowns {
		if unknown == "soft-unsatisfied:COST_CEILING" {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Fatalf("unknowns = %v, want the soft cost miss", explained.Unknowns)
	}
	if explained.CanonicalDigest == "" || explained.Explain() == "" {
		t.Fatal("explanation carries no digest or summary")
	}
	if err := explained.Validate(); err != nil {
		t.Fatal(err)
	}

	// Seeded defect: a protected attribute changes a factor reason, so the
	// explanation must refuse instead of ranking on it.
	proxied := reseed006(result, func(out *MatchResult) {
		out.Matches[0].Score.Factors[0].Reason = "zip-code proxy"
	})
	_, err := ExplainResult(request, proxied)
	rejected, ok := AsMatchExplanationRejected(err)
	if !ok {
		t.Fatalf("proxied factors err = %v, want MATCH_006_REJECTED", err)
	}
	if rejected.Code != MatchExplanationRejectedCode || rejected.Field != "match.score" || rejected.Version != Version() {
		t.Fatalf("rejected = %+v", rejected)
	}
	if !errors.Is(err, ErrMatchExplanationRejected) {
		t.Fatal("rejection does not match ErrMatchExplanationRejected with errors.Is")
	}

	// Seeded defect: a hard constraint without an explanation refuses.
	unexplained := reseed006(result, func(out *MatchResult) {
		kept := out.Matches[1].Satisfactions[:0]
		for _, satisfaction := range out.Matches[1].Satisfactions {
			if satisfaction.Kind != ConstraintLocation {
				kept = append(kept, satisfaction)
			}
		}
		out.Matches[1].Satisfactions = kept
	})
	_, err = ExplainResult(request, unexplained)
	rejected, ok = AsMatchExplanationRejected(err)
	if !ok {
		t.Fatalf("unexplained hard constraint err = %v, want MATCH_006_REJECTED", err)
	}
	if rejected.Field != "match.satisfactions" {
		t.Fatalf("rejected = %+v", rejected)
	}

	// Zero-effect: refused explanations persist nothing and change neither
	// input; both still validate with their original digests.
	if result.CanonicalDigest != before {
		t.Fatal("refused explanation mutated the result")
	}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := result.Validate(); err != nil {
		t.Fatal(err)
	}

	// Comparison leakage: no reason may name another candidate.
	leaked := reseed006(result, func(out *MatchResult) {
		other := out.Matches[1].CandidateRef.Id
		out.Matches[0].Satisfactions[0].Detail = "compared against " + other
	})
	_, err = ExplainResult(request, leaked)
	if _, ok := AsMatchExplanationRejected(err); !ok {
		t.Fatalf("leaked comparison err = %v, want MATCH_006_REJECTED", err)
	}
	for _, decision := range explained.Decisions {
		for _, factor := range decision.Score.Factors {
			for _, match := range result.Matches {
				if strings.Contains(factor.Reason, match.CandidateRef.Id) {
					t.Fatalf("factor reason leaks a candidate: %q", factor.Reason)
				}
			}
		}
	}
}
