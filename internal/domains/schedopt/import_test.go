package schedopt

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/demand"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/matching"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func importedAggregation(t *testing.T, windows ...DemandWindow) demand.DemandAggregation {
	t.Helper()
	for i := range windows {
		windows[i].SourceRef = "demand-source-" + strings.TrimPrefix(windows[i].SignalID, "window-")
	}
	rule := demand.BucketRule{
		ID: "hourly", Version: "bucket/v1", Origin: values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		Width: time.Hour, ResultScale: 0, Rounding: values.RoundingHalfEven,
	}
	aggregation, err := demand.AggregateSignals(windows, rule)
	if err != nil {
		t.Fatal(err)
	}
	return aggregation
}

func importRequest(t *testing.T, aggregation demand.DemandAggregation, population matching.CandidatePopulation) ProblemImport {
	t.Helper()
	problem, err := NewProblem(validProblem(t))
	if err != nil {
		t.Fatal(err)
	}
	return ProblemImport{
		Problem: problem, DemandAggregation: aggregation, CandidatePopulation: population,
		AsOf: population.AsOf,
	}
}

func TestTodo_SCHED_OPT_002(t *testing.T) {
	population := schedoptPopulation(t)
	aggregation := importedAggregation(t, schedoptDemand(t, "window-1", 9, 10))
	instance, err := ImportProblem(importRequest(t, aggregation, population))
	if err != nil {
		t.Fatal(err)
	}
	if len(instance.DecisionVariables) != 1 || instance.CanonicalDigest == "" {
		t.Fatalf("instance = %+v", instance)
	}
	if instance.DemandAggregationDigest != aggregation.CanonicalDigest || instance.CandidatePopulationDigest != population.CanonicalDigest {
		t.Fatalf("instance input bindings = %+v", instance)
	}
	if instance.DecisionVariables[0].CandidateRef != population.Candidates[0].CandidateRef {
		t.Fatalf("variable candidate = %+v", instance.DecisionVariables[0])
	}
	if _, err := instance.Explain(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_SCHED_OPT_002_Property(t *testing.T) {
	population := schedoptPopulation(t)
	firstWindow := schedoptDemand(t, "window-a", 9, 10)
	secondWindow := schedoptDemand(t, "window-b", 9, 10)
	secondWindow.Role = "assistant"
	firstAggregation := importedAggregation(t, firstWindow, secondWindow)
	secondAggregation := importedAggregation(t, secondWindow, firstWindow)
	first, err := ImportProblem(importRequest(t, firstAggregation, population))
	if err != nil {
		t.Fatal(err)
	}
	second, err := ImportProblem(importRequest(t, secondAggregation, population))
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatalf("input order changed instance digest: %s != %s", first.CanonicalDigest, second.CanonicalDigest)
	}
	if len(first.DecisionVariables) != 2 {
		t.Fatalf("variables = %+v", first.DecisionVariables)
	}
}

func TestTodo_SCHED_OPT_002_Golden(t *testing.T) {
	instance, err := ImportProblem(importRequest(t, importedAggregation(t, schedoptDemand(t, "window-1", 9, 10)), schedoptPopulation(t)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(instance.CanonicalDigest, "sha256:") || len(instance.Canonical()) == 0 {
		t.Fatalf("canonical instance = %q", instance.CanonicalDigest)
	}
	explanation, err := instance.Explain()
	if err != nil || explanation.DecisionVariableCount != 1 || explanation.CandidateCount != 1 || explanation.DemandWindowCount != 1 {
		t.Fatalf("explanation = %+v, err=%v", explanation, err)
	}
}

func TestTodo_SCHED_OPT_002_Race(t *testing.T) {
	instance, err := ImportProblem(importRequest(t, importedAggregation(t, schedoptDemand(t, "window-1", 9, 10)), schedoptPopulation(t)))
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	digests := make(chan string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			digest, err := instance.Digest()
			if err != nil {
				errs <- err
				return
			}
			digests <- digest
		}()
	}
	wg.Wait()
	close(errs)
	close(digests)
	for err := range errs {
		t.Errorf("concurrent imported problem digest: %v", err)
	}
	for digest := range digests {
		if digest != instance.CanonicalDigest {
			t.Errorf("concurrent digest = %q, want %q", digest, instance.CanonicalDigest)
		}
	}
}

func TestTodo_SCHED_OPT_002_Fault(t *testing.T) {
	population := schedoptPopulation(t)
	firstWindow := schedoptDemand(t, "window-a", 9, 10)
	secondWindow := schedoptDemand(t, "window-b", 9, 10)
	secondWindow.Role = "assistant"
	aggregation := importedAggregation(t, firstWindow, secondWindow)
	req := importRequest(t, aggregation, population)
	boundedProblem := validProblem(t)
	boundedProblem.Bounds.MaxDecisionVariables = 1
	var err error
	req.Problem, err = NewProblem(boundedProblem)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ImportProblem(req)
	if !errors.Is(err, ErrProblemInstanceVariableBound) || !strings.Contains(err.Error(), "MaxDecisionVariables") {
		t.Fatalf("bound error = %v", err)
	}

	unsafe := aggregation
	unsafe.CanonicalDigest = ""
	if _, err := ImportProblem(importRequest(t, unsafe, population)); !errors.Is(err, ErrDemandAggregationUnfrozen) {
		t.Fatalf("unfrozen aggregation error = %v", err)
	}
	if _, err := ImportProblem(importRequest(t, aggregation, population.Supersede())); !errors.Is(err, matching.ErrPopulationSuperseded) {
		t.Fatalf("superseded population error = %v", err)
	}
}

func TestTodo_SCHED_OPT_002_Mutation(t *testing.T) {
	population := schedoptPopulation(t)
	aggregation := importedAggregation(t, schedoptDemand(t, "window-1", 9, 10))
	instance, err := ImportProblem(importRequest(t, aggregation, population))
	if err != nil {
		t.Fatal(err)
	}
	instance.DecisionVariables[0].DemandWindowID = "mutated"
	if _, err := instance.Digest(); !errors.Is(err, ErrInvalidProblemImport) {
		t.Fatalf("mutated instance error = %v", err)
	}

	outOfValidity := importRequest(t, aggregation, population)
	outOfValidity.AsOf = values.NewInstant(time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC))
	if _, err := ImportProblem(outOfValidity); !errors.Is(err, ErrProblemInstanceAsOf) {
		t.Fatalf("out-of-validity error = %v", err)
	}

	demandValidity := importRequest(t, aggregation, population)
	demandValidity.DemandValidity, err = values.NewInstantInterval(
		values.NewInstant(time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)),
		values.NewInstant(time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ImportProblem(demandValidity); !errors.Is(err, ErrProblemInstanceAsOf) {
		t.Fatalf("demand validity error = %v", err)
	}
}
