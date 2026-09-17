package matching

import (
	"errors"
	"strings"
	"testing"
)

// TestTodo_MATCH_007_Property: the four-domain verdict is deterministic,
// domain-order independent and preserves eligibility dominance per domain.
func TestTodo_MATCH_007_Property(t *testing.T) {
	runs := fourDomainFixture(t)

	first, err := ProveFourDomainConformance(runs)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ProveFourDomainConformance(runs)
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatal("identical four-domain inputs replayed to different digests")
	}

	// Domain input order never changes the verdict digest.
	reversed := []DomainRun{runs[3], runs[2], runs[1], runs[0]}
	ordered, err := ProveFourDomainConformance(reversed)
	if err != nil {
		t.Fatal(err)
	}
	if ordered.CanonicalDigest != first.CanonicalDigest {
		t.Fatal("domain input order changed the verdict digest")
	}

	// Eligibility still dominates score inside every proven domain.
	for _, run := range runs {
		seenIneligible := false
		for _, match := range run.Result.Matches {
			if !match.Eligible {
				seenIneligible = true
			} else if seenIneligible {
				t.Fatalf("domain %s ranks an eligible candidate below an ineligible one", run.Domain)
			}
		}
	}
}

// TestTodo_MATCH_007_Conformance: every domain proves exactly once under
// one shared ranking contract; anything else rejects as MATCH_007_REJECTED.
func TestTodo_MATCH_007_Conformance(t *testing.T) {
	runs := fourDomainFixture(t)

	if _, err := ProveFourDomainConformance(runs[:3]); !errors.Is(err, ErrMatchConformanceRejected) {
		t.Fatalf("missing domain error = %v", err)
	}
	dup := append(append([]DomainRun(nil), runs[:3]...), runs[0])
	if _, err := ProveFourDomainConformance(dup); !errors.Is(err, ErrMatchConformanceRejected) {
		t.Fatalf("duplicate domain error = %v", err)
	}
	unknown := append([]DomainRun(nil), runs...)
	unknown[0].Domain = MatchDomain("UNKNOWN_DOMAIN")
	if _, err := ProveFourDomainConformance(unknown); !errors.Is(err, ErrMatchConformanceRejected) {
		t.Fatalf("unknown domain error = %v", err)
	}

	// A divergent score version breaks the shared contract.
	diverged := append([]DomainRun(nil), runs...)
	diverged[1].Result.ScoreVersion++
	diverged[1].Result.CanonicalDigest = diverged[1].Result.computedDigest()
	if _, err := ProveFourDomainConformance(diverged); !errors.Is(err, ErrMatchConformanceRejected) {
		t.Fatalf("divergent ranking contract error = %v", err)
	}

	// Closed domain vocabulary only.
	for _, domain := range []MatchDomain{DomainScheduling, DomainRecruiting, DomainMobility, DomainLearning} {
		if !domain.Valid() {
			t.Fatalf("domain %q is not declared", domain)
		}
	}
}

// TestTodo_MATCH_007_Mutation: seeded semantic mutants are killed — a
// tampered binding, a reordered rank and a cross-candidate reason refuse.
func TestTodo_MATCH_007_Mutation(t *testing.T) {
	runs := fourDomainFixture(t)

	tampered := reseed007(runs, DomainLearning, func(run *DomainRun) {
		run.Result.RequestDigest = "sha256:" + strings.Repeat("0", 64)
	})
	if _, err := ProveFourDomainConformance(tampered); !errors.Is(err, ErrMatchConformanceRejected) {
		t.Fatalf("tampered binding error = %v", err)
	}

	swapped := reseed007(runs, DomainScheduling, func(run *DomainRun) {
		a, b := run.Result.Matches[0], run.Result.Matches[1]
		a.Rank, b.Rank = b.Rank, a.Rank
		run.Result.Matches[0], run.Result.Matches[1] = b, a
	})
	if _, err := ProveFourDomainConformance(swapped); !errors.Is(err, ErrMatchConformanceRejected) {
		t.Fatalf("reordered rank error = %v", err)
	}

	leaked := reseed007(runs, DomainRecruiting, func(run *DomainRun) {
		other := run.Result.Matches[1].CandidateRef.Id
		run.Result.Matches[0].Score.Factors = append(run.Result.Matches[0].Score.Factors,
			ScoreFactor{Kind: ConstraintCostCeiling, Weight: 1, Points: 1, Reason: "SATISFIED-vs-" + other})
		run.Result.Matches[0].Score.Total++
	})
	if _, err := ProveFourDomainConformance(leaked); !errors.Is(err, ErrMatchConformanceRejected) {
		t.Fatalf("cross-candidate reason error = %v", err)
	}
}
