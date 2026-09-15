package frontier_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
)

// retryPlan is the promotion reference plan whose start node declares a
// three-attempt retry budget and a failure route. The plan digest is minted at
// compilation, so the edited node still addresses the same pinned state.
func retryPlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	plan := promotionPlan(t)
	for i := range plan.Nodes {
		if plan.Nodes[i].ID == plan.StartNodeID {
			plan.Nodes[i].Retry = &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.effect.bounded/v1"}
			plan.Nodes[i].FailureRoute = workflow.PromotionNodeEndApproval
		}
	}
	return plan
}

// TestRetryTerminalOutcomeSkipsTheRetryBudget pins WF-RUN-006's frontier half:
// a failure inside the declared budget retries, but the same failure marked
// RetryTerminal by the caller's retry policy takes the failure route at once.
func TestRetryTerminalOutcomeSkipsTheRetryBudget(t *testing.T) {
	plan := retryPlan(t)
	failed := frontier.NodeOutcome{NodeID: plan.StartNodeID, Failed: true, ErrorClass: "TRANSIENT"}

	retry := step(t, plan, seed(t, plan), failed)
	if retry.CompletedState != frontier.NodeRetrying || len(retry.Intents) != 1 || retry.Intents[0].Kind != frontier.IntentReady {
		t.Fatalf("retryable failure = %s %+v, want RETRYING with one READY intent", retry.CompletedState, retry.Intents)
	}

	failed.RetryTerminal = true
	terminal := step(t, plan, seed(t, plan), failed)
	if terminal.CompletedState != frontier.NodeFailed {
		t.Fatalf("retry-terminal failure = %s, want FAILED", terminal.CompletedState)
	}
	if len(terminal.Successors) != 1 || terminal.Successors[0].NodeID != workflow.PromotionNodeEndApproval {
		t.Fatalf("retry-terminal successors = %+v, want the failure route", terminal.Successors)
	}
	if retry.Digest() == terminal.Digest() {
		t.Fatal("RetryTerminal did not change the transition")
	}

	// Without a failure route the terminal failure is refused, never retried.
	for i := range plan.Nodes {
		if plan.Nodes[i].ID == plan.StartNodeID {
			plan.Nodes[i].FailureRoute = ""
		}
	}
	refuses(t, plan, seed(t, plan), failed, frontier.CodeNoFailureRoute)
}
