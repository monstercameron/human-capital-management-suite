package parallel

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
)

func TestTodo_WF_STEP_022(t *testing.T) {
	maxCost := int(^uint(0) >> 1)
	for _, tc := range []struct {
		name    string
		budget  int
		costs   []int
		refused bool
	}{
		{"overflow", maxCost, []int{maxCost, 1}, true},
		{"multiple_overflow", maxCost, []int{maxCost, maxCost}, true},
		{"negative_budget_overflow", -1, []int{maxCost, maxCost}, true},
		{"negative_budget", -1, []int{0}, true},
		{"negative_cost", maxCost, []int{-1}, true},
		{"one_over_budget", 5, []int{3, 3}, true},
		{"exact_maximum", maxCost, []int{maxCost - 1, 1}, false},
		{"maximum_with_free_branch", maxCost, []int{maxCost, 0}, false},
		{"free_work", 0, []int{0, 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			spec := boundedSpec()
			spec.Budget = tc.budget
			spec.Branches = spec.Branches[:len(tc.costs)]
			for i, cost := range tc.costs {
				spec.Branches[i].Cost = cost
				spec.Branches[i].Work = func(context.Context) (string, error) {
					calls.Add(1)
					return "done", nil
				}
			}
			got, err := Execute(context.Background(), spec)
			if tc.refused {
				if !errors.Is(err, ErrBudgetExceeded) || calls.Load() != 0 || len(got.Results) != 0 {
					t.Fatalf("over-budget work ran: results=%+v calls=%d err=%v", got, calls.Load(), err)
				}
				return
			}
			if err != nil || calls.Load() != int64(len(tc.costs)) || len(got.Results) != len(tc.costs) {
				t.Fatalf("valid boundary refused: results=%+v calls=%d err=%v", got, calls.Load(), err)
			}
			for _, result := range got.Results {
				if result.Outcome != OutcomeSucceeded {
					t.Fatalf("valid branch failed: %+v", result)
				}
			}
		})
	}
}

func TestTodo_WF_STEP_022_Fault(t *testing.T) {
	var rec observetest.Recorder
	spec := boundedSpec()
	spec.Budget = int(^uint(0) >> 1)
	spec.Branches[0].Cost = spec.Budget
	spec.Branches[1].Cost = 1
	_, err := Execute(rec.Context(context.Background()), spec)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("overflow error: %v", err)
	}
	ops := rec.Named("workflow.parallel.execute")
	if len(ops) != 1 || ops[0].Ended != 1 || ops[0].Outcome == observe.OutcomeSuccess || ops[0].Code == "" {
		t.Fatalf("missing failed/refused execution telemetry: %+v", ops)
	}
	if branches := rec.Named("workflow.parallel.branch"); len(branches) != 0 {
		t.Fatalf("refused admission started %d branch operations", len(branches))
	}
}
