package execution

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestComposedNodeRetryResolvesEveryServedBackoff proves the served policy is
// valid, resolves every BackoffRef the executable promotion plan compiles,
// retries only transient kinds, keeps DO_NOT_RETRY terminal and shares one
// budget of the node's compiled retries.
func TestComposedNodeRetryResolvesEveryServedBackoff(t *testing.T) {
	policy := composedNodeRetry()
	if _, err := execute.New(execute.Options{DB: stubBeginner{}, Steps: promotionStepRunner{}, NodeRetry: policy}); err != nil {
		t.Fatalf("served node retry policy refused: %v", err)
	}
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	retried := 0
	for _, node := range plan.Nodes {
		if node.Retry == nil {
			continue
		}
		retried++
		backoff, ok := policy.Backoffs[node.Retry.BackoffRef]
		if !ok {
			t.Fatalf("node %s backoff %q is not composed", node.ID, node.Retry.BackoffRef)
		}
		if backoff.Immediate || backoff.Deadline <= 0 || backoff.BaseDelay <= 0 {
			t.Fatalf("node %s backoff = %+v, want a bounded, parked backoff with a deadline", node.ID, backoff)
		}
	}
	if retried == 0 {
		t.Fatal("the executable plan declares no retried node; the composition check proves nothing")
	}
	for _, kind := range policy.Retryable {
		if kind == runtime.FailurePermanent {
			t.Fatal("PERMANENT is composed as retryable")
		}
	}
	if c := policy.Classify(runtime.ReasonDoNotRetry); !c.DoNotRetry {
		t.Fatalf("DO_NOT_RETRY classified %+v", c)
	}
	ref, err := policy.Budget(context.Background(), execute.RetryBudgetRequest{
		NodeID:      promotionexec.NodeObservePayroll,
		MaxAttempts: 2, Retryable: policy.Retryable,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok := ref.Provisioner.Snapshot(ref.BudgetID)
	if !ok || snapshot.Allowed != 1 || snapshot.Version != nodeRetryBudgetVersion {
		t.Fatalf("budget = %+v, want one retry for a two-attempt node", snapshot)
	}
}
