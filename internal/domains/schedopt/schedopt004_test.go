// SCHED-OPT-004 RED: the same problem, seed and solver must return an
// identical schedule and score, and the result must report the weights,
// tie-breaks, approximations and optimality gap behind it.
package schedopt

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/matching"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func schedopt004Population(t *testing.T) matching.CandidatePopulation {
	t.Helper()
	request := matching.MatchRequest{
		RequestID:          "match-cost-pref",
		Revision:           1,
		RequesterScope:     schedoptRef("organization", "001"),
		TargetRef:          schedoptRef("position", "002"),
		CandidateSourceRef: schedoptRef("candidate_source", "003"),
		Constraints:        []matching.Constraint{{Kind: matching.ConstraintLocation, Mode: matching.ConstraintHard, Location: "new-york"}},
		Fairness:           matching.FairnessPolicy{PolicyRef: "fairness", Version: "v1"},
		SnapshotRef:        schedoptRef("snapshot", "004"),
		AsOf:               values.NewInstant(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)),
		Ranking:            matching.RankingPolicy{PolicyRef: "ranking", Version: "v1", ScoreVersion: 1, TieBreak: matching.TieBreakCandidateRef},
		Explanation:        matching.ExplanationPolicy{PolicyRef: "explain", Version: "v1", IncludeConstraintReasons: true},
	}
	request, err := matching.NewMatchRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	preferred := matching.CandidateFacts{
		CandidateRef: schedoptRef("candidate", "005"), SourceRef: schedoptRef("candidate_source", "003"), Location: "new-york",
		Availability: []values.EffectiveInterval{schedoptInterval(t, 8, 18)}, Cost: schedoptMoney(t, "100.00"),
	}
	cheaper := matching.CandidateFacts{
		CandidateRef: schedoptRef("candidate", "006"), SourceRef: schedoptRef("candidate_source", "003"), Location: "new-york",
		Availability: []values.EffectiveInterval{schedoptInterval(t, 8, 18)}, Cost: schedoptMoney(t, "10.00"),
	}
	population, err := matching.FreezeCandidatePopulation(request, []matching.CandidateFacts{preferred, cheaper}, nil, matching.CandidateCompletenessComplete)
	if err != nil {
		t.Fatal(err)
	}
	return population
}

func schedopt004Problem(t *testing.T, unique bool) WorkforceOptimizationProblem {
	t.Helper()
	hard := []Constraint{
		{Kind: ConstraintAvailability},
		{Kind: ConstraintLocation, Location: "new-york"},
	}
	if unique {
		hard = append(hard, Constraint{Kind: ConstraintCandidateUniqueness})
	}
	return WorkforceOptimizationProblem{
		ProblemID:       "workforce-plan-cost-pref",
		Version:         "v1",
		Population:      schedopt004Population(t),
		DemandWindows:   []DemandWindow{schedoptDemand(t, "window-1", 9, 10), schedoptDemand(t, "window-2", 11, 12)},
		Horizon:         schedoptInterval(t, 8, 18),
		Objective:       Objective{Kind: ObjectiveMinimizeCost, Weight: 1},
		HardConstraints: hard,
		SoftConstraints: []Constraint{{Kind: ConstraintCostCeiling, Weight: 2, Cost: schedoptMoney(t, "1000.00")}},
		Bounds:          Bounds{MaxCandidates: 10, MaxDemandWindows: 10, MaxDecisionVariables: 100, MaxHardConstraints: 10, MaxSoftConstraints: 10},
		Solver:          SolverSpec{Name: "external-descriptive-solver", Version: "v1"},
	}
}

func schedopt004Preferences() []Preference {
	return []Preference{
		{CandidateRef: schedoptRef("candidate", "005"), Weight: 100},
	}
}

// TestTodo_SCHED_OPT_004 is the PRIMARY contract: same problem/seed/solver
// returns an identical schedule and score, and the result reports weights,
// tie-breaks, approximations and the optimality gap.
func TestTodo_SCHED_OPT_004(t *testing.T) {
	problem, err := NewProblem(schedopt004Problem(t, false))
	if err != nil {
		t.Fatal(err)
	}
	req := OptimizeRequest{Problem: problem, Preferences: schedopt004Preferences(), Seed: "seed-1"}

	first, err := Optimize(req)
	if err != nil {
		t.Fatalf("optimize: %v", err)
	}
	second, err := Optimize(req)
	if err != nil {
		t.Fatalf("optimize: %v", err)
	}
	if first.CanonicalDigest == "" || first.CanonicalDigest != second.CanonicalDigest {
		t.Fatalf("same seed diverged: %+v vs %+v", first, second)
	}
	if len(first.Assignments) != 2 || first.TotalScore != 200 || first.OptimalityGap != 0 {
		t.Fatalf("preferred schedule=%+v", first)
	}
	for _, a := range first.Assignments {
		if a.CandidateRef != schedoptRef("candidate", "005") {
			t.Fatalf("preferred candidate not assigned: %+v", first.Assignments)
		}
	}
	if len(first.Weights) != 1 || first.TieBreak == "" || first.Approximation == "" {
		t.Fatalf("weights/tie-breaks/approximations unreported: %+v", first)
	}
	if first.Solver != "external-descriptive-solver/v1" || first.Seed != "seed-1" {
		t.Fatalf("solver/seed unbound: %+v", first)
	}
	if err := first.Verify(); err != nil {
		t.Fatalf("sealed result Verify: %v", err)
	}

	t.Run("cost-breaks-preference-ties", func(t *testing.T) {
		tied := OptimizeRequest{Problem: problem, Preferences: []Preference{
			{CandidateRef: schedoptRef("candidate", "005"), Weight: 50},
			{CandidateRef: schedoptRef("candidate", "006"), Weight: 50},
		}, Seed: "seed-1"}
		got, err := Optimize(tied)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range got.Assignments {
			if a.CandidateRef != schedoptRef("candidate", "006") {
				t.Fatalf("cheaper candidate lost tied window: %+v", got.Assignments)
			}
		}
	})

	t.Run("uniqueness-spills-to-second-choice-with-gap", func(t *testing.T) {
		unique, err := NewProblem(schedopt004Problem(t, true))
		if err != nil {
			t.Fatal(err)
		}
		got, err := Optimize(OptimizeRequest{Problem: unique, Preferences: schedopt004Preferences(), Seed: "seed-1"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Assignments) != 2 || len(got.UnassignedWindows) != 0 {
			t.Fatalf("unique schedule=%+v", got)
		}
		if got.Assignments[0].CandidateRef != schedoptRef("candidate", "005") || got.Assignments[1].CandidateRef != schedoptRef("candidate", "006") {
			t.Fatalf("uniqueness spill=%+v", got.Assignments)
		}
		if got.TotalScore != 100 || got.UpperBound != 200 || got.OptimalityGap != 100 {
			t.Fatalf("gap unreported: %+v", got)
		}
	})

	t.Run("unknown-preference-refused", func(t *testing.T) {
		bad := OptimizeRequest{Problem: problem, Preferences: []Preference{
			{CandidateRef: schedoptRef("candidate", "999"), Weight: 10},
		}, Seed: "seed-1"}
		if _, err := Optimize(bad); !errors.Is(err, ErrInvalidOptimization) {
			t.Fatalf("unknown preference err=%v", err)
		}
	})
}

// TestTodo_SCHED_OPT_004_Property proves input-order invariance and score/gap
// reconciliation across preference levels.
func TestTodo_SCHED_OPT_004_Property(t *testing.T) {
	problem, err := NewProblem(schedopt004Problem(t, false))
	if err != nil {
		t.Fatal(err)
	}
	base, err := Optimize(OptimizeRequest{Problem: problem, Preferences: schedopt004Preferences(), Seed: "seed-1"})
	if err != nil {
		t.Fatal(err)
	}
	reordered := []Preference{
		{CandidateRef: schedoptRef("candidate", "006"), Weight: 0},
		{CandidateRef: schedoptRef("candidate", "005"), Weight: 100},
	}
	again, err := Optimize(OptimizeRequest{Problem: problem, Preferences: reordered, Seed: "seed-1"})
	if err != nil {
		t.Fatal(err)
	}
	if base.CanonicalDigest != again.CanonicalDigest {
		t.Fatal("preference order changed the sealed result")
	}
	for _, weight := range []int64{0, 1, 100} {
		got, err := Optimize(OptimizeRequest{Problem: problem, Preferences: []Preference{
			{CandidateRef: schedoptRef("candidate", "005"), Weight: weight},
		}, Seed: "seed-1"})
		if err != nil {
			t.Fatal(err)
		}
		var sum int64
		for _, a := range got.Assignments {
			sum += a.Score
		}
		if sum != got.TotalScore || got.OptimalityGap != got.UpperBound-got.TotalScore {
			t.Fatalf("weight %d: score/gap do not reconcile: %+v", weight, got)
		}
		if !slices.IsSortedFunc(got.Assignments, func(a, b Assignment) int {
			if a.DemandWindowID != b.DemandWindowID {
				if a.DemandWindowID < b.DemandWindowID {
					return -1
				}
				return 1
			}
			return 0
		}) {
			t.Fatalf("weight %d: assignments not canonical: %+v", weight, got.Assignments)
		}
	}
}

// TestTodo_SCHED_OPT_004_Golden pins the exact deterministic schedule, score
// and gap for the canonical preference fixture.
func TestTodo_SCHED_OPT_004_Golden(t *testing.T) {
	problem, err := NewProblem(schedopt004Problem(t, true))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Optimize(OptimizeRequest{Problem: problem, Preferences: schedopt004Preferences(), Seed: "seed-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Assignments) != 2 || got.Assignments[0].DemandWindowID != "window-1" || got.Assignments[1].DemandWindowID != "window-2" {
		t.Fatalf("golden windows=%+v", got.Assignments)
	}
	if got.Assignments[0].Score != 100 || got.Assignments[1].Score != 0 {
		t.Fatalf("golden scores=%+v", got.Assignments)
	}
	if got.TotalScore != 100 || got.UpperBound != 200 || got.OptimalityGap != 100 {
		t.Fatalf("golden totals=%+v", got)
	}
	again, err := Optimize(OptimizeRequest{Problem: problem, Preferences: schedopt004Preferences(), Seed: "seed-1"})
	if err != nil || again.CanonicalDigest != got.CanonicalDigest {
		t.Fatalf("golden digest unstable: %q vs %q", got.CanonicalDigest, again.CanonicalDigest)
	}
	reseeded, err := Optimize(OptimizeRequest{Problem: problem, Preferences: schedopt004Preferences(), Seed: "seed-2"})
	if err != nil {
		t.Fatal(err)
	}
	if reseeded.CanonicalDigest == got.CanonicalDigest {
		t.Fatal("seed does not bind the sealed digest")
	}
	for i := range got.Assignments {
		if got.Assignments[i] != reseeded.Assignments[i] {
			t.Fatalf("seed changed the assignment rule: %+v vs %+v", got.Assignments, reseeded.Assignments)
		}
	}
}

// TestTodo_SCHED_OPT_004_Race proves concurrent optimization converges on one
// sealed digest.
func TestTodo_SCHED_OPT_004_Race(t *testing.T) {
	problem, err := NewProblem(schedopt004Problem(t, false))
	if err != nil {
		t.Fatal(err)
	}
	const racers = 16
	var wg sync.WaitGroup
	digests := make([]string, racers)
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := Optimize(OptimizeRequest{Problem: problem, Preferences: schedopt004Preferences(), Seed: "seed-1"})
			if err == nil {
				digests[i] = got.CanonicalDigest
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i := 0; i < racers; i++ {
		if errs[i] != nil || digests[i] != digests[0] || digests[0] == "" {
			t.Fatalf("racer %d diverged: digest=%q err=%v", i, digests[i], errs[i])
		}
	}
}

// TestTodo_SCHED_OPT_004_Fault proves malformed optimization inputs fail
// closed and uncovered windows are reported, not guessed.
func TestTodo_SCHED_OPT_004_Fault(t *testing.T) {
	problem, err := NewProblem(schedopt004Problem(t, false))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*OptimizeRequest){
		"empty seed": func(r *OptimizeRequest) { r.Seed = "" },
		"negative weight": func(r *OptimizeRequest) {
			r.Preferences = []Preference{{CandidateRef: schedoptRef("candidate", "005"), Weight: -1}}
		},
		"duplicate weights": func(r *OptimizeRequest) { r.Preferences = append(r.Preferences, r.Preferences...) },
		"unsealed problem": func(r *OptimizeRequest) {
			raw := schedopt004Problem(t, false)
			raw.CanonicalDigest = "forged"
			r.Problem = raw
		},
	} {
		t.Run(name, func(t *testing.T) {
			req := OptimizeRequest{Problem: problem, Preferences: schedopt004Preferences(), Seed: "seed-1"}
			mutate(&req)
			if _, err := Optimize(req); !errors.Is(err, ErrInvalidOptimization) && !errors.Is(err, ErrInvalidProblem) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	narrow := schedopt004Problem(t, false)
	narrow.HardConstraints = append(narrow.HardConstraints, Constraint{Kind: ConstraintQualification, QualificationRefs: []values.EntityRef{schedoptRef("qualification", "999")}})
	sealed, err := NewProblem(narrow)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Optimize(OptimizeRequest{Problem: sealed, Seed: "seed-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Assignments) != 0 || len(got.UnassignedWindows) != 2 || got.UnassignedWindows[0] != "window-1" || got.UnassignedWindows[1] != "window-2" {
		t.Fatalf("uncovered windows guessed: %+v", got)
	}
}
