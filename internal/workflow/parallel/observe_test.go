package parallel

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
)

// TestParallelBranchesAreObservable proves every branch runs under its own
// operation, so one failing branch is visible as a FAILURE beside its
// succeeding siblings inside the parallel execution.
func TestParallelBranchesAreObservable(t *testing.T) {
	var rec observetest.Recorder
	spec := boundedSpec()
	spec.Branches[1].Work = func(context.Context) (string, error) { return "", errors.New("provider down") }
	if _, err := Execute(rec.Context(context.Background()), spec); err != nil {
		t.Fatal(err)
	}
	branches := rec.Named("workflow.parallel.branch")
	if len(branches) != 3 || len(rec.Named("workflow.parallel.execute")) != 1 {
		t.Fatalf("recorded ops = %+v", rec.Ops())
	}
	outcomes := map[string]int{}
	for _, op := range branches {
		if op.Ended != 1 {
			t.Fatalf("branch op ended %d times", op.Ended)
		}
		outcomes[op.Outcome]++
	}
	if outcomes[observe.OutcomeSuccess] != 2 || outcomes[observe.OutcomeFailure] != 1 {
		t.Fatalf("branch outcomes = %v", outcomes)
	}
}
