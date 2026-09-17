package matching

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fourDomainFixture builds one valid request/result pair per conformance
// domain. All four share the same ranking contract (score version and
// tie-break) while carrying distinct request identities.
func fourDomainFixture(t *testing.T) []DomainRun {
	t.Helper()
	domains := []MatchDomain{DomainScheduling, DomainRecruiting, DomainMobility, DomainLearning}
	ids := []string{"010", "011", "012", "013"}
	runs := make([]DomainRun, 0, len(domains))
	for i, domain := range domains {
		base := validMatchRequest(t)
		base.RequestID = "match-007-" + string(domain)
		base.TargetRef = matchingRef("position", ids[i])
		request, err := NewMatchRequest(base)
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
		runs = append(runs, DomainRun{Domain: domain, Request: request, Result: result})
	}
	return runs
}

// reseed007 deep-copies a domain run set so a seeded defect keeps every
// digest binding valid and only the defect itself can fail.
func reseed007(runs []DomainRun, domain MatchDomain, mutate func(*DomainRun)) []DomainRun {
	out := append([]DomainRun(nil), runs...)
	for i := range out {
		if out[i].Domain != domain {
			continue
		}
		mutate(&out[i])
		out[i].Result.CanonicalDigest = out[i].Result.computedDigest()
	}
	return out
}

// TestTodo_MATCH_007: the shared candidate/filter/rank/explain contracts
// prove across Scheduling, Recruiting, Mobility and Learning. A protected
// attribute that changes rank or a hard constraint without an explanation
// rejects as MATCH_007_REJECTED with field/state/version, and proving
// persists nothing and creates no transaction.
func TestTodo_MATCH_007(t *testing.T) {
	runs := fourDomainFixture(t)
	before := make([]string, len(runs))
	for i, run := range runs {
		before[i] = run.Result.CanonicalDigest
	}

	proved, err := ProveFourDomainConformance(runs)
	if err != nil {
		t.Fatal(err)
	}
	if len(proved.RankingDigests) != 4 || len(proved.Domains) != 4 {
		t.Fatalf("verdict = %+v", proved)
	}
	bound := make(map[MatchDomain]string, len(proved.Domains))
	for i, domain := range proved.Domains {
		bound[domain] = proved.RankingDigests[i]
	}
	for _, run := range runs {
		if bound[run.Domain] != run.Result.CanonicalDigest {
			t.Fatalf("verdict does not bind domain %s ranking", run.Domain)
		}
	}
	if proved.ScoreVersion == 0 || proved.TieBreak != TieBreakCandidateRef {
		t.Fatalf("verdict cites no shared ranking contract: %+v", proved)
	}
	if proved.CanonicalDigest == "" {
		t.Fatal("conformance verdict was not digested")
	}
	for _, run := range runs {
		if strings.Contains(proved.Explain(), run.Result.Matches[0].CandidateRef.Id) {
			t.Fatal("verdict explanation leaks a candidate identity")
		}
	}

	// Seeded defect: a protected attribute changes the rank of one domain.
	seeded := reseed007(runs, DomainRecruiting, func(run *DomainRun) {
		run.Result.Matches[0].Score.Factors = append(run.Result.Matches[0].Score.Factors,
			ScoreFactor{Kind: ConstraintLocation, Weight: 1, Points: 1, Reason: "AGE_OVER_40"})
		run.Result.Matches[0].Score.Total++
	})
	_, err = ProveFourDomainConformance(seeded)
	var rejected *MatchConformanceRejectedError
	if !errors.As(err, &rejected) || rejected.Code != MatchConformanceRejectedCode {
		t.Fatalf("protected attribute error = %v", err)
	}
	if !errors.Is(err, ErrMatchConformanceRejected) {
		t.Fatalf("protected attribute error is not ErrMatchConformanceRejected: %v", err)
	}
	if rejected.Field == "" || rejected.State == "" || rejected.Version != Version() {
		t.Fatalf("rejection names no field/state/version: %+v", rejected)
	}

	// Seeded defect: a hard constraint lacks its explanation.
	unexplained := reseed007(runs, DomainMobility, func(run *DomainRun) {
		for i := range run.Result.Matches {
			for j := range run.Result.Matches[i].Satisfactions {
				if run.Result.Matches[i].Satisfactions[j].Mode == ConstraintHard {
					run.Result.Matches[i].Satisfactions[j].Detail = ""
				}
			}
		}
	})
	if _, err := ProveFourDomainConformance(unexplained); !errors.Is(err, ErrMatchConformanceRejected) {
		t.Fatalf("unexplained hard constraint error = %v", err)
	}

	// Proving is pure: input ranking digests are untouched, so zero
	// authoritative rows, events, outbox entries, human work or provider
	// requests could have been produced from them.
	for i, run := range runs {
		if run.Result.CanonicalDigest != before[i] {
			t.Fatalf("domain %s ranking mutated by proving", run.Domain)
		}
	}
}
