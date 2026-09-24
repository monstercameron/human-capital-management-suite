package schedopt

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/matching"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/qualification"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var rev04001At = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func rev04001Problem(t *testing.T) WorkforceOptimizationProblem {
	t.Helper()
	window := schedoptDemand(t, "rev04001-window", 9, 10)
	request, err := matching.NewMatchRequest(matching.MatchRequest{
		RequestID: "rev04001-schedule", Revision: 1,
		RequesterScope: schedoptRef("organization", "001"), TargetRef: schedoptRef("position", "002"),
		CandidateSourceRef: schedoptRef("candidate_source", "003"),
		Constraints: []matching.Constraint{
			{Kind: matching.ConstraintAvailabilityWindow, Mode: matching.ConstraintHard, Window: window.Work},
			{Kind: matching.ConstraintLocation, Mode: matching.ConstraintHard, Location: "new-york"},
		},
		Fairness:    matching.FairnessPolicy{PolicyRef: "fairness", Version: "v1"},
		SnapshotRef: schedoptRef("snapshot", "004"),
		AsOf:        values.NewInstant(rev04001At),
		Ranking:     matching.RankingPolicy{PolicyRef: "ranking", Version: "v1", ScoreVersion: 1, TieBreak: matching.TieBreakCandidateRef},
		Explanation: matching.ExplanationPolicy{PolicyRef: "explain", Version: "v1", IncludeConstraintReasons: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	fact := matching.CandidateFacts{
		CandidateRef: schedoptRef("candidate", "005"), SourceRef: request.CandidateSourceRef,
		Location: "new-york", Availability: []values.EffectiveInterval{window.Work}, Cost: schedoptMoney(t, "80.00"),
	}
	population, err := matching.FreezeCandidatePopulation(request, []matching.CandidateFacts{fact}, nil, matching.CandidateCompletenessComplete)
	if err != nil {
		t.Fatal(err)
	}
	problem := validProblem(t)
	problem.Population = population
	problem.DemandWindows = []DemandWindow{window}
	problem.HardConstraints = []Constraint{{Kind: ConstraintAvailability}, {Kind: ConstraintLocation, Location: "new-york"}}
	problem, err = NewProblem(problem)
	if err != nil {
		t.Fatal(err)
	}
	return problem
}

func rev04001QualificationInput(candidate values.EntityRef) qualification.CrossQualInput {
	evidence := func(ref, kind string, restricted bool) qualification.QualEvidence {
		return qualification.QualEvidence{
			Ref: ref, Kind: kind, IssuedAt: rev04001At.Add(-24 * time.Hour), ExpiresAt: rev04001At.AddDate(0, 1, 0),
			Verified: true, Restricted: restricted,
		}
	}
	return qualification.CrossQualInput{
		Tenant: string(candidate.Tenant), WorkerRef: candidate.String(), AsOf: rev04001At,
		Evidence: []qualification.QualEvidence{
			evidence("job-cert", "JOB_CERT", false), evidence("availability", "AVAILABILITY", false),
			evidence("safety", "SAFETY_CARD", false), evidence("access", "ACCESS_CLEARANCE", false),
			evidence("rtw", "FITNESS_NOTE", true),
		},
		Requirements: []qualification.DomainRequirement{
			{Domain: qualification.CrossJob, Kinds: []string{"JOB_CERT"}},
			{Domain: qualification.CrossScheduling, Kinds: []string{"AVAILABILITY"}},
			{Domain: qualification.CrossSafety, Kinds: []string{"SAFETY_CARD"}},
			{Domain: qualification.CrossAccess, Kinds: []string{"ACCESS_CLEARANCE"}},
			{Domain: qualification.CrossRTW, Kinds: []string{"FITNESS_NOTE"}},
		},
	}
}

func rev04001MatchRuns(t *testing.T, p WorkforceOptimizationProblem) []matching.DomainRun {
	t.Helper()
	domains := []matching.MatchDomain{matching.DomainScheduling, matching.DomainRecruiting, matching.DomainMobility, matching.DomainLearning}
	runs := make([]matching.DomainRun, 0, len(domains))
	facts := p.Population.CandidateFactsList()
	for _, domain := range domains {
		var request matching.MatchRequest
		if domain == matching.DomainScheduling {
			request = matching.MatchRequest{
				RequestID: p.Population.RequestID, Revision: 1,
				RequesterScope: p.Population.RequesterScope, TargetRef: schedoptRef("position", "002"),
				CandidateSourceRef: p.Population.CandidateSourceRef,
				Constraints: []matching.Constraint{
					{Kind: matching.ConstraintAvailabilityWindow, Mode: matching.ConstraintHard, Window: p.DemandWindows[0].Work},
					{Kind: matching.ConstraintLocation, Mode: matching.ConstraintHard, Location: "new-york"},
				},
				Fairness: matching.FairnessPolicy{PolicyRef: "fairness", Version: "v1"}, SnapshotRef: schedoptRef("snapshot", "004"),
				AsOf:        values.NewInstant(rev04001At),
				Ranking:     matching.RankingPolicy{PolicyRef: "ranking", Version: "v1", ScoreVersion: 1, TieBreak: matching.TieBreakCandidateRef},
				Explanation: matching.ExplanationPolicy{PolicyRef: "explain", Version: "v1", IncludeConstraintReasons: true},
			}
		} else {
			request = matching.MatchRequest{
				RequestID: "rev04001-" + string(domain), Revision: 1,
				RequesterScope:     p.Population.RequesterScope,
				TargetRef:          schedoptRef("position", map[matching.MatchDomain]string{matching.DomainRecruiting: "012", matching.DomainMobility: "013", matching.DomainLearning: "014"}[domain]),
				CandidateSourceRef: p.Population.CandidateSourceRef,
				Constraints:        []matching.Constraint{{Kind: matching.ConstraintLocation, Mode: matching.ConstraintHard, Location: "new-york"}},
				Fairness:           matching.FairnessPolicy{PolicyRef: "fairness", Version: "v1"}, SnapshotRef: schedoptRef("snapshot", "004"),
				AsOf:        values.NewInstant(rev04001At),
				Ranking:     matching.RankingPolicy{PolicyRef: "ranking", Version: "v1", ScoreVersion: 1, TieBreak: matching.TieBreakCandidateRef},
				Explanation: matching.ExplanationPolicy{PolicyRef: "explain", Version: "v1", IncludeConstraintReasons: true},
			}
		}
		var err error
		request, err = matching.NewMatchRequest(request)
		if err != nil {
			t.Fatal(err)
		}
		port, err := matching.NewInMemoryCandidateFacts(facts)
		if err != nil {
			t.Fatal(err)
		}
		result, err := matching.Match(context.Background(), port, request)
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, matching.DomainRun{Domain: domain, Request: request, Result: result})
	}
	return runs
}

func TestTodo_REV_040_01(t *testing.T) {
	problem := rev04001Problem(t)
	candidate := problem.Population.CandidateFactsList()[0].CandidateRef
	qualificationProof, err := EvaluateQualificationForWindow(problem, candidate, problem.DemandWindows[0].SignalID, rev04001QualificationInput(candidate))
	if err != nil {
		t.Fatal(err)
	}
	if !qualificationProof.Feasibility.Feasible || len(qualificationProof.Qualification.Domains) != 5 {
		t.Fatalf("qualification proof did not bind the scheduled worker/window: %+v", qualificationProof)
	}
	if _, err := ProveMatchingForSchedule(problem, rev04001MatchRuns(t, problem)); err != nil {
		t.Fatalf("schedule consumer matching proof: %v", err)
	}
}

func TestTodo_REV_040_01_Conformance(t *testing.T) {
	problem := rev04001Problem(t)
	candidate := problem.Population.CandidateFactsList()[0].CandidateRef
	input := rev04001QualificationInput(candidate)
	input.WorkerRef = schedoptRef("candidate", "999").String()
	if _, err := EvaluateQualificationForWindow(problem, candidate, problem.DemandWindows[0].SignalID, input); err == nil {
		t.Fatal("qualification accepted a worker outside the scheduling binding")
	}
	input = rev04001QualificationInput(candidate)
	input.Tenant = "foreign-tenant"
	if _, err := EvaluateQualificationForWindow(problem, candidate, problem.DemandWindows[0].SignalID, input); err == nil {
		t.Fatal("qualification accepted a tenant outside the scheduling population")
	}
	runs := rev04001MatchRuns(t, problem)
	for i := range runs {
		if runs[i].Domain == matching.DomainScheduling {
			runs[i].Request.Constraints[0].Window = schedoptInterval(t, 11, 12)
			runs[i].Request.CanonicalDigest = ""
			var err error
			runs[i].Request, err = matching.NewMatchRequest(runs[i].Request)
			if err != nil {
				t.Fatal(err)
			}
			port, err := matching.NewInMemoryCandidateFacts(problem.Population.CandidateFactsList())
			if err != nil {
				t.Fatal(err)
			}
			runs[i].Result, err = matching.Match(context.Background(), port, runs[i].Request)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := ProveMatchingForSchedule(problem, runs); err == nil {
		t.Fatal("matching proof accepted a scheduling run detached from the demand window")
	}
}

func TestTodo_REV_040_01_Integration(t *testing.T) {
	problem := rev04001Problem(t)
	candidate := problem.Population.CandidateFactsList()[0].CandidateRef
	input := rev04001QualificationInput(candidate)
	proof, err := EvaluateQualificationForWindow(problem, candidate, problem.DemandWindows[0].SignalID, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, verdict := range proof.Qualification.Domains {
		if verdict.Domain == qualification.CrossRTW && (!verdict.RestrictedUsed || len(verdict.EvidenceRefs) != 0) {
			t.Fatalf("restricted RTW evidence leaked through scheduling consumer: %+v", verdict)
		}
	}
	runs := rev04001MatchRuns(t, problem)
	conformance, err := ProveMatchingForSchedule(problem, runs)
	if err != nil {
		t.Fatal(err)
	}
	if len(conformance.Domains) != 4 || len(conformance.RankingDigests) != 4 || conformance.CanonicalDigest == "" {
		t.Fatalf("consumer returned incomplete four-domain proof: %+v", conformance)
	}
	if _, err := ProveMatchingForSchedule(problem, runs[:3]); err == nil {
		t.Fatalf("incomplete consumer runs error = %v", err)
	}
}
