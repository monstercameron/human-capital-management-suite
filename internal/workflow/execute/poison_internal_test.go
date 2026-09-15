package execute

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestPoisonSpecRoutesEveryErrorClass pins how an exhausted node's error class
// and retry budget map onto runtime.Admit's three routes.
func TestPoisonSpecRoutesEveryErrorClass(t *testing.T) {
	retrying := workflow.CompiledNode{ID: "observe", Type: workflow.StepObserve, Retry: &workflow.RetryPolicy{MaxAttempts: 3}}
	single := workflow.CompiledNode{ID: "capability", Type: workflow.StepCapability}
	policy := PoisonWorkPolicy{Store: runtime.QuarantineStore{}, Owner: "principal:ops", SLA: time.Hour, RepairRoute: "operations.repair.retry_budget"}
	noRepair := policy
	noRepair.RepairRoute = ""
	for name, tc := range map[string]struct {
		policy PoisonWorkPolicy
		node   workflow.CompiledNode
		class  string
		reason string
		route  string
		action string
	}{
		"attempts exhausted":          {policy, retrying, "TIMEOUT", runtime.ReasonAttemptsExhausted, runtime.WorkflowBlocked, "operator-decision"},
		"no retry budget":             {policy, single, "PERMANENT", runtime.ReasonNonretryable, runtime.WorkflowBlocked, "operator-decision"},
		"do not retry kept":           {policy, retrying, runtime.ReasonDoNotRetry, runtime.ReasonDoNotRetry, runtime.WorkflowBlocked, "operator-decision"},
		"deadline kept":               {policy, retrying, runtime.ReasonDeadlineExceeded, runtime.ReasonDeadlineExceeded, runtime.WorkflowBlocked, "operator-decision"},
		"budget with repair route":    {policy, retrying, runtime.ReasonBudgetExhausted, runtime.ReasonBudgetExhausted, runtime.WorkflowRepairRequired, "repair:operations.repair.retry_budget"},
		"budget without repair route": {noRepair, retrying, runtime.ReasonBudgetExhausted, runtime.ReasonAttemptsExhausted, runtime.WorkflowBlocked, "operator-decision"},
		"ambiguous":                   {policy, retrying, "COMMIT_AMBIGUOUS", runtime.ReasonAttemptsExhausted, runtime.WorkflowQuarantined, "reconcile-outcome:observe"},
		"unknown outcome":             {policy, single, "UNKNOWN_TASK_OUTCOME", runtime.ReasonNonretryable, runtime.WorkflowQuarantined, "reconcile-outcome:capability"},
	} {
		spec := poisonSpec(tc.policy, "wf/poison", tc.node, frontier.NodeOutcome{NodeID: tc.node.ID, Failed: true, ErrorClass: tc.class}, 3)
		if spec.Terminal.Decision != runtime.RouteTerminal || spec.Terminal.Reason != tc.reason {
			t.Errorf("%s: terminal = %+v, want TERMINAL/%s", name, spec.Terminal, tc.reason)
		}
		spec.IdempotencyKey = "wfq:" + name
		work, err := runtime.Admit(spec)
		if err != nil {
			t.Errorf("%s: Admit = %v", name, err)
			continue
		}
		if work.Route != tc.route || work.NextAction != tc.action || work.Attempts != 3 || work.LastError != tc.class || work.Owner != "principal:ops" {
			t.Errorf("%s: work = %+v, want route %s action %s", name, work, tc.route, tc.action)
		}
	}
	if spec := poisonSpec(policy, "wf", single, frontier.NodeOutcome{NodeID: "capability", Failed: true, ErrorClass: "  "}, 1); spec.LastError != "NO_OUTCOME" {
		t.Fatalf("blank error class retained as %q, want NO_OUTCOME", spec.LastError)
	}
}

// TestPoisonWorkErrorAndSettledAttempt pins the typed error and the guard that
// stops a settled FAILED attempt from running again.
func TestPoisonWorkErrorAndSettledAttempt(t *testing.T) {
	cause := errors.New("frontier: NO_FAILURE_ROUTE")
	err := error(&PoisonWorkError{
		Work:   runtime.QuarantinedWork{NodeID: "observe", Attempts: 2, LastError: "TIMEOUT", NextAction: "operator-decision"},
		Status: runtime.InstanceBlocked, cause: cause,
	})
	if !errors.Is(err, ErrPoisonWorkQuarantined) || errors.Is(err, cause) {
		t.Fatalf("PoisonWorkError unwraps to %v; want only the sentinel", errors.Unwrap(err))
	}
	for _, want := range []string{"observe", "2 attempts", "BLOCKED", "TIMEOUT", "operator-decision", "NO_FAILURE_ROUTE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not name %q", err.Error(), want)
		}
	}
	for _, status := range []runtime.NodeStatus{runtime.NodeReady, runtime.NodeRetrying, runtime.NodeRunning} {
		if err := refuseSettledAttempt(runtime.NodeExecution{NodeID: "observe", Attempt: 2, Status: status}); err != nil {
			t.Errorf("%s attempt refused: %v", status, err)
		}
	}
	if err := refuseSettledAttempt(runtime.NodeExecution{NodeID: "observe", Attempt: 2, Status: runtime.NodeFailed}); !errors.Is(err, ErrPoisonWorkQuarantined) {
		t.Fatalf("settled FAILED attempt = %v, want ErrPoisonWorkQuarantined", err)
	}
	if poisonOutcome(err) != OutcomeDenied || poisonOutcome(cause) != OutcomeFailure {
		t.Fatal("a committed quarantine must end the span DENIED and a failed filing FAILURE")
	}
	var nilPolicy *PoisonWorkPolicy
	if err := nilPolicy.validate(); err != nil {
		t.Fatalf("absent policy refused: %v", err)
	}
	if (&Driver{}).poisonable(cause) {
		t.Fatal("a driver without a policy claimed the refusal")
	}
	d := &Driver{opts: Options{PoisonWork: &PoisonWorkPolicy{}}}
	if d.poisonable(nil) || d.poisonable(cause) {
		t.Fatal("a nil error or a non-frontier error was treated as poison")
	}
	if !d.poisonable(&frontier.Error{Code: frontier.CodeNoFailureRoute}) {
		t.Fatal("the NO_FAILURE_ROUTE refusal was not treated as poison")
	}
}
