package matching

import (
	"context"
	"strings"
	"testing"
)

func fairGate(t *testing.T) FairnessGate {
	t.Helper()
	return FairnessGate{
		ProhibitedProxies: []string{"protected-class", "zip-code"},
		MinCohort:         2,
		Policy:            FairnessPolicy{PolicyRef: "policy:fairness", Version: "v1"},
	}
}

// cloneMatchResult deep-copies a result so proxy seeding never mutates
// the clean ranking it derives from.
func cloneMatchResult(result MatchResult) MatchResult {
	out := result
	out.Matches = append([]CandidateMatch(nil), result.Matches...)
	for i := range out.Matches {
		out.Matches[i].Satisfactions = append([]ConstraintSatisfaction(nil), result.Matches[i].Satisfactions...)
		out.Matches[i].Score.Factors = append([]ScoreFactor(nil), result.Matches[i].Score.Factors...)
	}
	return out
}

func fairResult(t *testing.T, request MatchRequest) MatchResult {
	t.Helper()
	port, err := NewInMemoryCandidateFacts([]CandidateFacts{
		candidateFact(t, "006", "new-york", "90.00", true),
		candidateFact(t, "007", "new-york", "80.00", true),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Match(context.Background(), port, request)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// TestTodo_MATCH_005: fairness and policy gating blocks prohibited
// proxies, small cohorts and policy breaches for human review —
// without accusing anyone and without touching the ranking.
func TestTodo_MATCH_005(t *testing.T) {
	request, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	clean := fairResult(t, request)

	// A clean ranking against the declared policy is allowed.
	allowed, err := AssessFairness(request, clean, fairGate(t))
	if err != nil {
		t.Fatal(err)
	}
	if allowed.Verdict != FairnessAllow || len(allowed.Reasons) != 0 {
		t.Fatalf("clean assessment = %+v", allowed)
	}

	// Seeded defect: a prohibited proxy blocks, naming the factor kind.
	proxied := cloneMatchResult(clean)
	proxied.Matches[0].Score.Factors[0].Reason = "zip-code proxy"
	for i := range proxied.Matches {
		proxied.Matches[i].CanonicalDigest = ""
		proxied.Matches[i].CanonicalDigest = proxied.Matches[i].computedDigest()
	}
	proxied.CanonicalDigest = proxied.computedDigest()
	blocked, err := AssessFairness(request, proxied, fairGate(t))
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Verdict != FairnessBlocked {
		t.Fatalf("proxied assessment verdict = %v, want BLOCKED", blocked.Verdict)
	}

	// Seeded defect: a cohort below the minimum goes to review.
	smallGate := fairGate(t)
	smallGate.MinCohort = len(clean.Matches) + 1
	review, err := AssessFairness(request, clean, smallGate)
	if err != nil {
		t.Fatal(err)
	}
	if review.Verdict != FairnessNeedsReview {
		t.Fatalf("small-cohort verdict = %v, want NEEDS_REVIEW", review.Verdict)
	}

	// Seeded defect: a fairness policy breach goes to review.
	breachGate := fairGate(t)
	breachGate.Policy = FairnessPolicy{PolicyRef: "policy:fairness", Version: "v2"}
	breach, err := AssessFairness(request, clean, breachGate)
	if err != nil {
		t.Fatal(err)
	}
	if breach.Verdict != FairnessNeedsReview {
		t.Fatalf("policy-breach verdict = %v, want NEEDS_REVIEW", breach.Verdict)
	}

	// The engine never auto-accuses: reasons name factor kinds and
	// policy tokens only, never a candidate.
	for _, assessment := range []FairnessAssessment{blocked, review, breach} {
		for _, reason := range assessment.Reasons {
			for _, match := range clean.Matches {
				if strings.Contains(reason, match.CandidateRef.Id) {
					t.Fatalf("reason accuses a candidate: %q", reason)
				}
			}
		}
	}
	// The engine never silently changes the ranking: the assessment
	// binds the exact input digest and the input still validates.
	if blocked.RankingDigest != proxied.CanonicalDigest {
		t.Fatal("assessment did not bind the assessed ranking")
	}
	if err := proxied.Validate(); err != nil {
		t.Fatal(err)
	}

	// Malformed gates reject with MATCH_005_REJECTED naming
	// field/version and persist nothing (assessment is pure).
	badGate := fairGate(t)
	badGate.MinCohort = 0
	if _, err := AssessFairness(request, clean, badGate); err == nil {
		t.Fatal("zero-cohort gate assessed")
	} else if rejected, ok := AsFairnessRejected(err); !ok || rejected.Code != FairnessRejectedCode {
		t.Fatalf("expected MATCH_005_REJECTED, got %v", err)
	} else if rejected.Field == "" || rejected.Version == 0 {
		t.Fatalf("rejection lacks field/version: %+v", rejected)
	}
}
