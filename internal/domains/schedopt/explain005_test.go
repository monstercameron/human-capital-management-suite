// SCHED-OPT-005 RED: every assigned/unassigned demand and worker must trace
// hard constraints, preferences, cost and alternatives without disclosing
// other workers' protected facts; a stale or unsealed input is rejected.
package schedopt

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func schedOpt005Request(t *testing.T) ExplainRequest {
	t.Helper()
	problem, err := NewProblem(schedopt004Problem(t, false))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Optimize(OptimizeRequest{
		Problem:     problem,
		Preferences: schedopt004Preferences(),
		Seed:        "seed-explain-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return ExplainRequest{
		Problem:     problem,
		Result:      result,
		Preferences: schedopt004Preferences(),
	}
}

// TestTodo_SCHED_OPT_005 is the PRIMARY contract: each demand window traces
// its assignment (or its exact conflict set), and no worker's protected
// facts leak into another worker's explanation.
func TestTodo_SCHED_OPT_005(t *testing.T) {
	explained, err := ExplainAssignments(schedOpt005Request(t))
	if err != nil {
		t.Fatalf("ExplainAssignments: %v", err)
	}
	if explained.ResultDigest == "" || explained.CanonicalDigest == "" {
		t.Fatalf("explanation is not sealed: %+v", explained)
	}
	if len(explained.Windows) != 2 {
		t.Fatalf("every demand window must be explained: %+v", explained)
	}
	for _, window := range explained.Windows {
		if window.DemandWindowID == "" || window.Reason == "" {
			t.Fatalf("window explanation is incomplete: %+v", window)
		}
		if window.AssignedCandidate == "" {
			t.Fatalf("window %q should be assigned: %+v", window.DemandWindowID, window)
		}
		if window.PreferenceWeight != 100 || window.AssignmentScore != 100 {
			t.Fatalf("preference/cost trace is wrong: %+v", window)
		}
		if window.AlternativesEliminated != 1 {
			t.Fatalf("one alternative must be traced as eliminated: %+v", window)
		}
		if window.OutscoredAlternatives != 1 || window.HardEliminated != 0 {
			t.Fatalf("the loser lost on preference, not on a hard rule: %+v", window)
		}
	}
	if err := explained.Verify(); err != nil {
		t.Fatalf("sealed explanation Verify: %v", err)
	}

	t.Run("unassigned demand traces exact conflict set", func(t *testing.T) {
		problem, err := NewProblem(schedopt004Problem(t, false))
		if err != nil {
			t.Fatal(err)
		}
		// A long far window is uncovered: the fatigue cap admits the
		// one-hour window-1 but blocks every candidate on the two-hour
		// window-far, so FATIGUE_LIMIT eliminates the whole field there.
		infeasibleInput := schedopt004Problem(t, false)
		infeasibleInput.HardConstraints = append(infeasibleInput.HardConstraints,
			Constraint{Kind: ConstraintFatigueLimit, MaxFatigueMinutes: 100})
		infeasibleInput.DemandWindows = []DemandWindow{
			schedoptDemand(t, "window-1", 9, 10),
			schedoptDemand(t, "window-far", 16, 18),
		}
		_ = problem
		infeasible, err := NewProblem(infeasibleInput)
		if err != nil {
			t.Fatal(err)
		}
		standings := []WorkerStanding{
			{CandidateRef: schedoptRef("candidate", "005")},
			{CandidateRef: schedoptRef("candidate", "006")},
		}
		result, err := Optimize(OptimizeRequest{
			Problem:     infeasible,
			Standings:   standings,
			Preferences: schedopt004Preferences(),
			Seed:        "seed-explain-2",
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.UnassignedWindows) != 1 || result.UnassignedWindows[0] != "window-far" {
			t.Fatalf("expected window-far unassigned: %+v", result)
		}
		explained, err := ExplainAssignments(ExplainRequest{
			Problem:     infeasible,
			Result:      result,
			Standings:   standings,
			Preferences: schedopt004Preferences(),
		})
		if err != nil {
			t.Fatalf("ExplainAssignments: %v", err)
		}
		var found *WindowExplanation
		for i := range explained.Windows {
			if explained.Windows[i].DemandWindowID == "window-far" {
				found = &explained.Windows[i]
			}
		}
		if found == nil || found.AssignedCandidate != "" {
			t.Fatalf("far window must be explained as unassigned: %+v", explained)
		}
		if len(found.BlockingKinds) == 0 {
			t.Fatalf("unassigned window must carry its conflict set: %+v", found)
		}
	})

	t.Run("no other worker's facts are disclosed", func(t *testing.T) {
		req := schedOpt005Request(t)
		explained, err := ExplainAssignments(req)
		if err != nil {
			t.Fatal(err)
		}
		var rendered strings.Builder
		for _, window := range explained.Windows {
			fmt.Fprintf(&rendered, "%s|%s|%d|%v|%s|",
				window.DemandWindowID, window.AssignedCandidate,
				window.AssignmentScore, window.BlockingKinds, window.Reason)
		}
		blob := rendered.String()
		// The cheaper candidate loses both windows; its identity must never
		// appear in any explanation entry.
		loser := schedoptRef("candidate", "006").String()
		if strings.Contains(blob, loser) {
			t.Fatalf("explanation discloses another worker's facts: %s", blob)
		}
	})
}

func TestTodo_SCHED_OPT_005_Property(t *testing.T) {
	req := schedOpt005Request(t)
	first, err := ExplainAssignments(req)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		next, err := ExplainAssignments(req)
		if err != nil {
			t.Fatal(err)
		}
		if next.CanonicalDigest != first.CanonicalDigest {
			t.Fatalf("explanation is not deterministic: %s vs %s",
				first.CanonicalDigest, next.CanonicalDigest)
		}
	}
	// Windows are reported in canonical order and eliminated counts
	// reconcile with the decision variables of the sealed problem.
	for i := 1; i < len(first.Windows); i++ {
		if first.Windows[i-1].DemandWindowID >= first.Windows[i].DemandWindowID {
			t.Fatalf("windows are not canonical: %+v", first.Windows)
		}
	}
	variables := 0
	for _, variable := range req.Problem.DecisionVariables {
		if variable.DemandWindowID == first.Windows[0].DemandWindowID {
			variables++
		}
	}
	got := first.Windows[0].AlternativesEliminated
	if first.Windows[0].AssignedCandidate != "" {
		got++
	}
	if got != variables {
		t.Fatalf("eliminated+assigned=%d, variables=%d", got, variables)
	}
}

func TestTodo_SCHED_OPT_005_Race(t *testing.T) {
	req := schedOpt005Request(t)
	const racers = 16
	digests := make([]string, racers)
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			explained, err := ExplainAssignments(req)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = explained.CanonicalDigest
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d: %v", i, err)
		}
		if digests[i] != digests[0] || digests[0] == "" {
			t.Fatalf("concurrent explanations diverged: %v", digests)
		}
	}
}

func TestTodo_SCHED_OPT_005_Fault(t *testing.T) {
	t.Run("stale frozen input is rejected", func(t *testing.T) {
		req := schedOpt005Request(t)
		other, err := NewProblem(schedopt004Problem(t, true))
		if err != nil {
			t.Fatal(err)
		}
		req.Problem = other
		_, err = ExplainAssignments(req)
		var rej *ExplanationRejection
		if !errors.As(err, &rej) {
			t.Fatalf("expected *ExplanationRejection, got %v", err)
		}
		if !errors.Is(err, ErrExplanationRejected) {
			t.Fatalf("expected SCHED_OPT_005_REJECTED, got %v", err)
		}
		if rej.Field == "" || rej.State == "" || rej.Version == "" {
			t.Fatalf("rejection must name field/state/version: %+v", rej)
		}
	})

	t.Run("unsealed result is rejected", func(t *testing.T) {
		req := schedOpt005Request(t)
		req.Result.CanonicalDigest += "tampered"
		if _, err := ExplainAssignments(req); !errors.Is(err, ErrExplanationRejected) {
			t.Fatalf("tampered result must be rejected, got %v", err)
		}
	})

	t.Run("empty result is rejected", func(t *testing.T) {
		req := schedOpt005Request(t)
		req.Result = OptimizationResult{}
		if _, err := ExplainAssignments(req); !errors.Is(err, ErrExplanationRejected) {
			t.Fatalf("empty result must be rejected, got %v", err)
		}
	})
}

func TestTodo_SCHED_OPT_005_Mutation(t *testing.T) {
	explained, err := ExplainAssignments(schedOpt005Request(t))
	if err != nil {
		t.Fatal(err)
	}
	mutated := explained
	mutated.Windows = append([]WindowExplanation(nil), explained.Windows...)
	mutated.Windows[0].AssignmentScore++
	if err := mutated.Verify(); !errors.Is(err, ErrExplanationRejected) {
		t.Fatalf("score mutant must fail Verify, got %v", err)
	}
	dropped := explained
	dropped.Windows = dropped.Windows[:1]
	if err := dropped.Verify(); !errors.Is(err, ErrExplanationRejected) {
		t.Fatalf("dropped-window mutant must fail Verify, got %v", err)
	}
	redacted := explained
	redacted.Windows = append([]WindowExplanation(nil), explained.Windows...)
	redacted.Windows[0].BlockingKinds = nil
	redacted.Windows[0].HardEliminated = 1
	redacted.Windows[0].OutscoredAlternatives = 0
	if err := redacted.Verify(); !errors.Is(err, ErrExplanationRejected) {
		t.Fatalf("redacted-trace mutant must fail Verify, got %v", err)
	}
}
