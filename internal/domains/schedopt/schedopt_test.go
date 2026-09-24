package schedopt

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/matching"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schedoptUUIDBase = "00000000-0000-4000-8000-000000000"

func schedoptRef(kind values.Kind, suffix string) values.EntityRef {
	return values.EntityRef{Tenant: "tenant-a", Kind: kind, Id: schedoptUUIDBase + suffix}
}

func schedoptInterval(t *testing.T, startHour, endHour int) values.EffectiveInterval {
	t.Helper()
	start := values.NewInstant(time.Date(2026, 9, 10, startHour, 0, 0, 0, time.UTC))
	end := values.NewInstant(time.Date(2026, 9, 10, endHour, 0, 0, 0, time.UTC))
	window, err := values.NewInstantInterval(start, end)
	if err != nil {
		t.Fatal(err)
	}
	return window
}

func schedoptMoney(t *testing.T, amount string) values.Money {
	t.Helper()
	money, err := values.NewMoney(amount, "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return money
}

func schedoptPopulation(t *testing.T) matching.CandidatePopulation {
	t.Helper()
	request := matching.MatchRequest{
		RequestID:          "match-1",
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
	facts := []matching.CandidateFacts{{
		CandidateRef: schedoptRef("candidate", "005"), SourceRef: schedoptRef("candidate_source", "003"), Location: "new-york",
		Availability: []values.EffectiveInterval{schedoptInterval(t, 9, 10)}, Cost: schedoptMoney(t, "80.00"),
	}}
	population, err := matching.FreezeCandidatePopulation(request, facts, nil, matching.CandidateCompletenessComplete)
	if err != nil {
		t.Fatal(err)
	}
	return population
}

func schedoptDemand(t *testing.T, id string, startHour, endHour int) DemandWindow {
	t.Helper()
	quantity, err := values.NewQuantity("1", "FTE", 0, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return DemandWindow{SignalID: id, Work: schedoptInterval(t, startHour, endHour), Location: "new-york", Role: "support", Quantity: quantity, Unit: "FTE", Source: "forecast", ConfidenceClass: "HIGH", Scenario: "base", Version: "v1"}
}

func validProblem(t *testing.T) WorkforceOptimizationProblem {
	t.Helper()
	return WorkforceOptimizationProblem{
		ProblemID:       "workforce-plan-1",
		Version:         "v1",
		Population:      schedoptPopulation(t),
		DemandWindows:   []DemandWindow{schedoptDemand(t, "window-1", 9, 10)},
		Horizon:         schedoptInterval(t, 8, 18),
		Objective:       Objective{Kind: ObjectiveMaximizeCoverage, Weight: 1},
		HardConstraints: []Constraint{{Kind: ConstraintAvailability}, {Kind: ConstraintLocation, Location: "new-york"}},
		SoftConstraints: []Constraint{{Kind: ConstraintCostCeiling, Weight: 2, Cost: schedoptMoney(t, "100.00")}},
		Bounds:          Bounds{MaxCandidates: 10, MaxDemandWindows: 10, MaxDecisionVariables: 100, MaxHardConstraints: 10, MaxSoftConstraints: 10},
		Solver:          SolverSpec{Name: "external-descriptive-solver", Version: "v1"},
	}
}

func TestTodo_SCHED_OPT_001(t *testing.T) {
	problem, err := NewProblem(validProblem(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(problem.DecisionVariables) != 1 || problem.CanonicalDigest == "" {
		t.Fatalf("problem = %+v", problem)
	}
	report, err := problem.FeasibilityPrecheck()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Feasible || len(report.Unmet) != 0 || report.CanonicalDigest == "" {
		t.Fatalf("feasibility report = %+v", report)
	}
	if _, err := Explain(problem); err != nil {
		t.Fatal(err)
	}
	infeasibleInput := validProblem(t)
	infeasibleInput.DemandWindows = []DemandWindow{schedoptDemand(t, "window-uncovered", 11, 12)}
	infeasible, err := NewProblem(infeasibleInput)
	if err != nil {
		t.Fatal(err)
	}
	failure, err := infeasible.FeasibilityPrecheck()
	if err != nil || failure.Feasible || len(failure.Unmet) == 0 || failure.Unmet[0].ConstraintKind != ConstraintAvailability {
		t.Fatalf("infeasible precheck = %+v, err=%v", failure, err)
	}
}

func TestTodo_SCHED_OPT_001_Property(t *testing.T) {
	first, err := NewProblem(validProblem(t))
	if err != nil {
		t.Fatal(err)
	}
	input := validProblem(t)
	input.DemandWindows = append([]DemandWindow(nil), input.DemandWindows...)
	second, err := NewProblem(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatal("equivalent problem definitions produced different digests")
	}
	if first.DecisionVariables[0] != second.DecisionVariables[0] {
		t.Fatal("decision variable generation is not deterministic")
	}
}

func TestTodo_SCHED_OPT_001_Race(t *testing.T) {
	problem, err := NewProblem(validProblem(t))
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	var wg sync.WaitGroup
	reports := make(chan FeasibilityReport, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			report, err := problem.FeasibilityPrecheck()
			if err != nil {
				errs <- err
				return
			}
			reports <- report
		}()
	}
	wg.Wait()
	close(reports)
	close(errs)
	for err := range errs {
		t.Errorf("concurrent feasibility precheck: %v", err)
	}
	for report := range reports {
		if !report.Feasible {
			t.Errorf("concurrent report is infeasible: %+v", report)
		}
	}
}

func TestTodo_SCHED_OPT_001_Fault(t *testing.T) {
	unbounded := validProblem(t)
	unbounded.Bounds.MaxDecisionVariables = 0
	if _, err := NewProblem(unbounded); !errors.Is(err, ErrUnboundedProblem) {
		t.Fatalf("unbounded error = %v", err)
	}
	unknown := validProblem(t)
	unknown.HardConstraints[0].Kind = ConstraintKind("UNDECLARED")
	if _, err := NewProblem(unknown); !errors.Is(err, ErrUnknownConstraint) {
		t.Fatalf("unknown constraint error = %v", err)
	}
}

func TestTodo_SCHED_OPT_001_Security(t *testing.T) {
	problem, err := NewProblem(validProblem(t))
	if err != nil {
		t.Fatal(err)
	}
	explanation, err := problem.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if explanation.Authority == "" || explanation.Objective == "" {
		t.Fatalf("explanation = %+v", explanation)
	}
	if explanation.CandidateCount != 1 || explanation.DemandWindowCount != 1 {
		t.Fatalf("explanation counts = %+v", explanation)
	}
}

func TestTodo_SCHED_OPT_001_Conformance(t *testing.T) {
	problem, err := NewProblem(validProblem(t))
	if err != nil {
		t.Fatal(err)
	}
	if problem.Objective.Kind != ObjectiveMaximizeCoverage || len(problem.HardConstraints) != 2 || len(problem.SoftConstraints) != 1 {
		t.Fatalf("problem contract = %+v", problem)
	}
}

func TestTodo_SCHED_OPT_001_Mutation(t *testing.T) {
	problem, err := NewProblem(validProblem(t))
	if err != nil {
		t.Fatal(err)
	}
	problem.DemandWindows[0].SignalID = "mutated"
	if _, err := problem.Digest(); !errors.Is(err, ErrInvalidProblem) {
		t.Fatalf("mutated problem error = %v", err)
	}
}
