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

func hardConstraintQualRef() values.EntityRef { return schedoptRef("qualification", "101") }
func hardConstraintAuthRef() values.EntityRef { return schedoptRef("authorization", "102") }

func hardConstraintPopulation(t *testing.T) matching.CandidatePopulation {
	t.Helper()
	request := matching.MatchRequest{
		RequestID:          "match-hard",
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
	rested := matching.CandidateFacts{
		CandidateRef: schedoptRef("candidate", "005"), SourceRef: schedoptRef("candidate_source", "003"), Location: "new-york",
		Availability: []values.EffectiveInterval{schedoptInterval(t, 8, 18)}, Cost: schedoptMoney(t, "100.00"),
		QualificationRefs: []values.EntityRef{hardConstraintQualRef()},
	}
	// The fatigued candidate is cheaper, so a score-chasing solver would
	// prefer it under MINIMIZE_COST. The hard constraints must still win.
	cheaper := matching.CandidateFacts{
		CandidateRef: schedoptRef("candidate", "006"), SourceRef: schedoptRef("candidate_source", "003"), Location: "new-york",
		Availability: []values.EffectiveInterval{schedoptInterval(t, 8, 18)}, Cost: schedoptMoney(t, "10.00"),
	}
	population, err := matching.FreezeCandidatePopulation(request, []matching.CandidateFacts{rested, cheaper}, nil, matching.CandidateCompletenessComplete)
	if err != nil {
		t.Fatal(err)
	}
	return population
}

func hardConstraintProblem(t *testing.T) WorkforceOptimizationProblem {
	t.Helper()
	return WorkforceOptimizationProblem{
		ProblemID:     "workforce-plan-hard",
		Version:       "v1",
		Population:    hardConstraintPopulation(t),
		DemandWindows: []DemandWindow{schedoptDemand(t, "window-1", 9, 10)},
		Horizon:       schedoptInterval(t, 8, 18),
		Objective:     Objective{Kind: ObjectiveMinimizeCost, Weight: 1},
		HardConstraints: []Constraint{
			{Kind: ConstraintAvailability},
			{Kind: ConstraintLocation, Location: "new-york"},
			{Kind: ConstraintQualification, QualificationRefs: []values.EntityRef{hardConstraintQualRef()}},
			{Kind: ConstraintLegalAuthorization, AuthorizationRefs: []values.EntityRef{hardConstraintAuthRef()}},
			{Kind: ConstraintFatigueLimit, MaxFatigueMinutes: 480},
		},
		SoftConstraints: []Constraint{{Kind: ConstraintCostCeiling, Weight: 2, Cost: schedoptMoney(t, "1000.00")}},
		Bounds:          Bounds{MaxCandidates: 10, MaxDemandWindows: 10, MaxDecisionVariables: 100, MaxHardConstraints: 10, MaxSoftConstraints: 10},
		Solver:          SolverSpec{Name: "external-descriptive-solver", Version: "v1"},
	}
}

func hardConstraintStandings() []WorkerStanding {
	return []WorkerStanding{
		{CandidateRef: schedoptRef("candidate", "005"), LegalAuthorizations: []values.EntityRef{hardConstraintAuthRef()}, AccruedFatigueMinutes: 60},
		{CandidateRef: schedoptRef("candidate", "006"), AccruedFatigueMinutes: 460},
	}
}

// TestTodo_SCHED_OPT_003 proves hard-constraint application: the eligible
// worker stays assignable while the cheaper but unqualified, unauthorized
// and fatigued worker is blocked under exactly those kinds, even though a
// score-chasing solver would prefer it.
func TestTodo_SCHED_OPT_003(t *testing.T) {
	problem, err := NewProblem(hardConstraintProblem(t))
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := problem.EvaluateHardConstraints(hardConstraintStandings())
	if err != nil {
		t.Fatalf("evaluate hard constraints: %v", err)
	}
	if !evaluation.Feasible || len(evaluation.Conflicts) != 0 || evaluation.CanonicalDigest == "" {
		t.Fatalf("covered window reported infeasible: %+v", evaluation)
	}
	if len(evaluation.Evaluations) != 2 {
		t.Fatalf("expected one verdict per decision variable, got %+v", evaluation)
	}
	rested := evaluation.Evaluations[0]
	if rested.Verdict != VerdictAssignable || len(rested.BlockingKinds) != 0 {
		t.Fatalf("eligible worker blocked: %+v", rested)
	}
	// Candidate 006 sorts after 005, so index 1 is the cheaper worker.
	cheaper := evaluation.Evaluations[1]
	if cheaper.Verdict != VerdictBlocked {
		t.Fatalf("ineligible cheaper worker assignable: %+v", cheaper)
	}
	for _, kind := range []ConstraintKind{ConstraintQualification, ConstraintLegalAuthorization, ConstraintFatigueLimit} {
		found := false
		for _, blocking := range cheaper.BlockingKinds {
			if blocking == kind {
				found = true
			}
		}
		if !found {
			t.Fatalf("cheaper worker not blocked by %s: %+v", kind, cheaper)
		}
	}
	if err := evaluation.Verify(); err != nil {
		t.Fatalf("sealed evaluation Verify: %v", err)
	}
	// Removing the eligible worker's standing leaves the window uncovered
	// with the exact conflict set and coverage gap.
	blocked, err := problem.EvaluateHardConstraints([]WorkerStanding{hardConstraintStandings()[1]})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Feasible || len(blocked.Conflicts) != 1 {
		t.Fatalf("unsatisfiable window lacks exact conflict: %+v", blocked)
	}
	conflict := blocked.Conflicts[0]
	if conflict.DemandWindowID != "window-1" {
		t.Fatalf("conflict names wrong window: %+v", conflict)
	}
	if len(conflict.DefeatedBy) == 0 {
		t.Fatalf("conflict names no defeating kinds: %+v", conflict)
	}
	for _, defeat := range conflict.DefeatedBy {
		if defeat.EliminatedCandidates == 0 {
			t.Fatalf("conflict kind with zero eliminations: %+v", conflict)
		}
	}
}

func TestTodo_SCHED_OPT_003_Property(t *testing.T) {
	problem, err := NewProblem(hardConstraintProblem(t))
	if err != nil {
		t.Fatal(err)
	}
	first, err := problem.EvaluateHardConstraints(hardConstraintStandings())
	if err != nil {
		t.Fatal(err)
	}
	// Standing order is not an input signal: reversing it changes nothing.
	reversed := []WorkerStanding{hardConstraintStandings()[1], hardConstraintStandings()[0]}
	second, err := problem.EvaluateHardConstraints(reversed)
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatal("standing order changed the evaluation digest")
	}
	for i := range first.Evaluations {
		a, b := first.Evaluations[i], second.Evaluations[i]
		if a.CandidateRef != b.CandidateRef || a.DemandWindowID != b.DemandWindowID || a.Verdict != b.Verdict || !slices.Equal(a.BlockingKinds, b.BlockingKinds) {
			t.Fatalf("standing order changed verdict %d: %+v vs %+v", i, a, b)
		}
	}
	// The fatigue boundary is exact: accrued minutes plus the 60-minute
	// window at the 480 cap stays assignable, one minute more blocks.
	atCap := hardConstraintStandings()
	atCap[0].AccruedFatigueMinutes = 420
	capped, err := problem.EvaluateHardConstraints(atCap)
	if err != nil {
		t.Fatal(err)
	}
	if capped.Evaluations[0].Verdict != VerdictAssignable {
		t.Fatalf("at-cap worker blocked: %+v", capped.Evaluations[0])
	}
	overCap := hardConstraintStandings()
	overCap[0].AccruedFatigueMinutes = 421
	over, err := problem.EvaluateHardConstraints(overCap)
	if err != nil {
		t.Fatal(err)
	}
	if over.Evaluations[0].Verdict != VerdictBlocked {
		t.Fatalf("over-cap worker assignable: %+v", over.Evaluations[0])
	}
}

func TestTodo_SCHED_OPT_003_Race(t *testing.T) {
	problem, err := NewProblem(hardConstraintProblem(t))
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
			evaluation, err := problem.EvaluateHardConstraints(hardConstraintStandings())
			if err == nil {
				digests[i] = evaluation.CanonicalDigest
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

func TestTodo_SCHED_OPT_003_Fault(t *testing.T) {
	problem, err := NewProblem(hardConstraintProblem(t))
	if err != nil {
		t.Fatal(err)
	}
	unknown := hardConstraintStandings()
	unknown[0].CandidateRef = schedoptRef("candidate", "999")
	if _, err := problem.EvaluateHardConstraints(unknown); !errors.Is(err, ErrInvalidWorkerStanding) {
		t.Fatalf("unknown candidate error = %v", err)
	}
	foreign := hardConstraintStandings()
	foreign[0].CandidateRef.Tenant = "tenant-b"
	if _, err := problem.EvaluateHardConstraints(foreign); !errors.Is(err, ErrInvalidWorkerStanding) {
		t.Fatalf("cross-tenant standing error = %v", err)
	}
	negative := hardConstraintStandings()
	negative[0].AccruedFatigueMinutes = -5
	if _, err := problem.EvaluateHardConstraints(negative); !errors.Is(err, ErrInvalidWorkerStanding) {
		t.Fatalf("negative fatigue error = %v", err)
	}
	duplicated := append(hardConstraintStandings(), hardConstraintStandings()[0])
	if _, err := problem.EvaluateHardConstraints(duplicated); !errors.Is(err, ErrInvalidWorkerStanding) {
		t.Fatalf("duplicate standing error = %v", err)
	}
	unbounded := hardConstraintProblem(t)
	unbounded.HardConstraints = append(unbounded.HardConstraints, Constraint{Kind: ConstraintKind("UNDECLARED")})
	if _, err := NewProblem(unbounded); !errors.Is(err, ErrUnknownConstraint) {
		t.Fatalf("unknown hard kind error = %v", err)
	}
	weightless := hardConstraintProblem(t)
	weightless.HardConstraints[4].Weight = 7
	if _, err := NewProblem(weightless); !errors.Is(err, ErrInvalidProblem) {
		t.Fatalf("weighted hard constraint error = %v", err)
	}
	// A worker without standing evidence cannot satisfy evidentiary kinds,
	// so it is blocked rather than admitted on missing proof.
	missing, err := problem.EvaluateHardConstraints(nil)
	if err != nil {
		t.Fatal(err)
	}
	if missing.Feasible {
		t.Fatalf("unevidenced workers feasible: %+v", missing)
	}
}

func TestTodo_SCHED_OPT_003_Security(t *testing.T) {
	problem, err := NewProblem(hardConstraintProblem(t))
	if err != nil {
		t.Fatal(err)
	}
	foreign := hardConstraintStandings()
	foreign[0].CandidateRef.Tenant = "tenant-b"
	foreign[0].LegalAuthorizations[0].Tenant = "tenant-b"
	if _, err := problem.EvaluateHardConstraints(foreign); !errors.Is(err, ErrInvalidWorkerStanding) {
		t.Fatalf("foreign tenant standing error = %v", err)
	}
	// The conflict set must never become a candidate enumeration channel.
	blocked, err := problem.EvaluateHardConstraints([]WorkerStanding{hardConstraintStandings()[1]})
	if err != nil {
		t.Fatal(err)
	}
	for _, conflict := range blocked.Conflicts {
		for _, defeat := range conflict.DefeatedBy {
			if defeat.ConstraintKind == "" || defeat.EliminatedCandidates <= 0 {
				t.Fatalf("conflict leaks shape without counts: %+v", conflict)
			}
		}
	}
	for _, verdict := range blocked.Evaluations {
		if string(verdict.CandidateRef.Tenant) != "tenant-a" {
			t.Fatalf("verdict crossed tenant: %+v", verdict)
		}
	}
}

func TestTodo_SCHED_OPT_003_Mutation(t *testing.T) {
	problem, err := NewProblem(hardConstraintProblem(t))
	if err != nil {
		t.Fatal(err)
	}
	standings := hardConstraintStandings()
	evaluation, err := problem.EvaluateHardConstraints(standings)
	if err != nil {
		t.Fatal(err)
	}
	before := evaluation.CanonicalDigest
	standings[0].AccruedFatigueMinutes = 4000
	standings[1].LegalAuthorizations = []values.EntityRef{hardConstraintAuthRef()}
	if evaluation.CanonicalDigest != before {
		t.Fatal("sealed evaluation changed after standing mutation")
	}
	if err := evaluation.Verify(); err != nil {
		t.Fatalf("unchanged evaluation Verify=%v", err)
	}
	evaluation.Evaluations[1].Verdict = VerdictAssignable
	if err := evaluation.Verify(); !errors.Is(err, ErrInvalidHardConstraintEvaluation) {
		t.Fatalf("mutated sealed evaluation Verify=%v", err)
	}
}
