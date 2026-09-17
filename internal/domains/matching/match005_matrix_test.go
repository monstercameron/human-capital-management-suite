package matching

import (
	"strings"
	"testing"
)

// TestTodo_MATCH_005_Property: gating is deterministic, monotone in
// cohort size, and ranking-preserving — the same ranking always gates
// to the same digest, growing the cohort never regresses a verdict,
// and the gated digest is always the input digest.
func TestTodo_MATCH_005_Property(t *testing.T) {
	request, err := NewMatchRequest(validMatchRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	clean := fairResult(t, request)
	gate := fairGate(t)

	first, err := AssessFairness(request, clean, gate)
	if err != nil {
		t.Fatal(err)
	}
	second, err := AssessFairness(request, clean, gate)
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatal("identical gating inputs replayed to different digests")
	}
	for _, reason := range first.Reasons {
		switch {
		case strings.HasPrefix(reason, "prohibited-proxy:"):
		case reason == "cohort-below-minimum":
		case reason == "fairness-policy-mismatch":
		default:
			t.Fatalf("reason outside the closed vocabulary: %q", reason)
		}
	}

	// Growing the cohort past the minimum lifts the cohort review
	// without touching the ranking contract.
	tight := gate
	tight.MinCohort = len(clean.Matches) + 1
	small, err := AssessFairness(request, clean, tight)
	if err != nil {
		t.Fatal(err)
	}
	if small.Verdict != FairnessNeedsReview {
		t.Fatalf("tight cohort verdict = %v, want NEEDS_REVIEW", small.Verdict)
	}
	loose := gate
	loose.MinCohort = len(clean.Matches)
	grown, err := AssessFairness(request, clean, loose)
	if err != nil {
		t.Fatal(err)
	}
	if grown.Verdict != FairnessAllow {
		t.Fatalf("grown cohort verdict = %v, want ALLOW", grown.Verdict)
	}

	// Every assessment binds the exact ranking it gated.
	for _, assessment := range []FairnessAssessment{first, small, grown} {
		if assessment.RankingDigest != clean.CanonicalDigest {
			t.Fatal("assessment did not preserve the ranking digest")
		}
		if !assessment.Verdict.Valid() {
			t.Fatalf("verdict outside the closed vocabulary: %v", assessment.Verdict)
		}
	}
}
