package admission

import (
	"errors"
	"sync"
	"testing"
)

func TestTodo_ADMISSION_002(t *testing.T) {
	signal := BackpressureSignal{Source: "workflow", Dependency: "connector", State: BackpressureThrottled, QueueDepth: 8, QueueLimit: 10, RetryAfter: 4}
	decision := DecideBackpressure(signal, []string{"message", "workflow", "workflow"})
	if decision.Action != BackpressureQueue || decision.RetryAfter != 4 || len(decision.Targets) != 2 {
		t.Fatalf("backpressure decision = %+v", decision)
	}

	budget := RetryBudget{ID: "budget-1", TenantID: "tenant-a", Dependency: "connector", LogicalOperationID: "op-1", OperationKind: "sync", Allowed: 1, Retryable: []FailureClass{FailureUnavailable}, Version: "v1"}
	attempt := RetryAttempt{LogicalOperationID: "op-1", OperationKind: "sync", TenantID: "tenant-a", Dependency: "connector", Failure: FailureUnavailable, Attempt: 1}
	receipt, err := ConsumeRetry(budget, attempt)
	if err != nil || receipt.Disposition != RetryAllowed || receipt.Remaining != 0 || receipt.Consumed != 1 {
		t.Fatalf("retry receipt = %+v, err=%v", receipt, err)
	}
	if receipt.LogicalOperationID != attempt.LogicalOperationID || receipt.OperationKind != attempt.OperationKind || receipt.BudgetVersion != budget.Version || receipt.Attempt != attempt.Attempt {
		t.Fatalf("retry receipt lost exact operation binding: %+v", receipt)
	}
	budget.Consumed = 1
	receipt, err = ConsumeRetry(budget, attempt)
	if err != nil || receipt.Disposition != RetryBudgetExhausted || receipt.RepairRoute == "" {
		t.Fatalf("exhausted receipt = %+v, err=%v", receipt, err)
	}
}

func TestTodo_ADMISSION_002_Race(t *testing.T) {
	signal := BackpressureSignal{Source: "workflow", Dependency: "messages", State: BackpressureSlow, RecommendedRate: 12}
	want := DecideBackpressure(signal, []string{"b", "a"})
	const workers = 32
	var wg sync.WaitGroup
	results := make(chan BackpressureDecision, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- DecideBackpressure(signal, []string{"a", "b"})
		}()
	}
	wg.Wait()
	close(results)
	for got := range results {
		if got.Action != want.Action || got.Reason != want.Reason || got.RecommendedRate != want.RecommendedRate || len(got.Targets) != len(want.Targets) || got.Targets[0] != want.Targets[0] || got.Targets[1] != want.Targets[1] {
			t.Fatalf("non-deterministic concurrent propagation: got=%+v want=%+v", got, want)
		}
	}
}

func TestTodo_ADMISSION_002_Integration(t *testing.T) {
	decision := DecideBackpressure(BackpressureSignal{Source: "workflow", Dependency: "connector", QueueDepth: 11, QueueLimit: 10}, []string{"connector", "workflow"})
	if decision.Action != BackpressureDefer || decision.Reason != "QUEUE_WATERMARK_EXCEEDED" {
		t.Fatalf("watermark decision = %+v", decision)
	}
}

func TestTodo_ADMISSION_002_Fault(t *testing.T) {
	decision := DecideBackpressure(BackpressureSignal{Source: "", Dependency: "connector", State: BackpressureHealthy}, nil)
	if decision.Action != BackpressureStop || decision.Reason != "INVALID_SIGNAL" {
		t.Fatalf("invalid signal decision = %+v", decision)
	}
}

func BenchmarkTodo_ADMISSION_002(b *testing.B) {
	signal := BackpressureSignal{Source: "workflow", Dependency: "connector", State: BackpressureSlow, RecommendedRate: 12}
	for i := 0; i < b.N; i++ {
		_ = DecideBackpressure(signal, []string{"workflow", "connector"})
	}
}

func TestBackpressureDecisionStatesAndBounds(t *testing.T) {
	base := BackpressureSignal{Source: "workflow", Dependency: "connector", QueueLimit: 10, Capacity: 20}
	for _, tc := range []struct {
		name   string
		state  BackpressureState
		depth  int
		retry  int
		want   BackpressureAction
		reason string
	}{
		{"empty state invalid", "", 0, 0, BackpressureStop, "UNKNOWN_DOWNSTREAM_STATE"},
		{"healthy", BackpressureHealthy, 0, 0, BackpressureContinue, "DOWNSTREAM_HEALTHY"},
		{"slow", BackpressureSlow, 0, 0, BackpressureSlowUpstream, "DOWNSTREAM_SLOW"},
		{"throttled fallback", BackpressureThrottled, 0, 0, BackpressureQueue, "DOWNSTREAM_THROTTLED"},
		{"unavailable fallback", BackpressureUnavailable, 0, 0, BackpressureDefer, "DOWNSTREAM_UNAVAILABLE"},
		{"unknown state", BackpressureState("BROKEN"), 0, 0, BackpressureStop, "UNKNOWN_DOWNSTREAM_STATE"},
		{"watermark takes precedence", BackpressureHealthy, 11, 4, BackpressureDefer, "QUEUE_WATERMARK_EXCEEDED"},
		{"negative signal", BackpressureHealthy, -1, 4, BackpressureStop, "INVALID_SIGNAL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			s.State, s.QueueDepth, s.RetryAfter = tc.state, tc.depth, tc.retry
			got := DecideBackpressure(s, []string{" workflow ", "", "workflow", "connector"})
			if got.Action != tc.want || got.Reason != tc.reason {
				t.Fatalf("decision=%+v", got)
			}
			if len(got.Targets) != 2 || got.Targets[0] != "connector" || got.Targets[1] != "workflow" {
				t.Fatalf("targets=%v", got.Targets)
			}
		})
	}
}

func TestConsumeRetryRejectsInvalidAndHandlesNonRetryableFailures(t *testing.T) {
	valid := RetryBudget{ID: "budget", TenantID: "tenant", Dependency: "connector", LogicalOperationID: "op", OperationKind: "sync", Allowed: 2, Retryable: []FailureClass{FailureUnavailable}, Version: "v1"}
	attempt := RetryAttempt{LogicalOperationID: "op", OperationKind: "sync", TenantID: "tenant", Dependency: "connector", Failure: FailureUnavailable, Attempt: 1}
	for _, tc := range []struct {
		name   string
		mutate func(*RetryBudget, *RetryAttempt)
	}{
		{"missing budget id", func(b *RetryBudget, _ *RetryAttempt) { b.ID = "" }},
		{"missing budget version", func(b *RetryBudget, _ *RetryAttempt) { b.Version = "" }},
		{"tenant mismatch", func(_ *RetryBudget, a *RetryAttempt) { a.TenantID = "other" }},
		{"dependency mismatch", func(_ *RetryBudget, a *RetryAttempt) { a.Dependency = "other" }},
		{"logical operation mismatch", func(_ *RetryBudget, a *RetryAttempt) { a.LogicalOperationID = "other" }},
		{"operation kind mismatch", func(_ *RetryBudget, a *RetryAttempt) { a.OperationKind = "other" }},
		{"unknown failure", func(_ *RetryBudget, a *RetryAttempt) { a.Failure = FailureClass("OTHER") }},
		{"unknown retry policy class", func(b *RetryBudget, _ *RetryAttempt) { b.Retryable = []FailureClass{"OTHER"} }},
		{"duplicate retry policy class", func(b *RetryBudget, _ *RetryAttempt) {
			b.Retryable = []FailureClass{FailureUnavailable, FailureUnavailable}
		}},
		{"zero attempt", func(_ *RetryBudget, a *RetryAttempt) { a.Attempt = 0 }},
		{"negative consumed", func(b *RetryBudget, _ *RetryAttempt) { b.Consumed = -1 }},
		{"counter exceeds budget", func(b *RetryBudget, _ *RetryAttempt) { b.Consumed = 3 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, a := valid, attempt
			tc.mutate(&b, &a)
			if _, err := ConsumeRetry(b, a); !errors.Is(err, ErrInvalidRetryInput) {
				t.Fatalf("ConsumeRetry=%v", err)
			}
		})
	}
	notRetryable := attempt
	notRetryable.Failure = FailureTimeout
	receipt, err := ConsumeRetry(valid, notRetryable)
	if err != nil || receipt.Disposition != RetryNotAllowed || receipt.Remaining != 2 || receipt.Consumed != 0 {
		t.Fatalf("non-retryable receipt=%+v err=%v", receipt, err)
	}
}

func TestTodo_ADMISSION_002_Security(t *testing.T) {
	b := RetryBudget{ID: "b", TenantID: "t", Dependency: "d", LogicalOperationID: "logical", OperationKind: "op", Allowed: 1, Retryable: []FailureClass{FailureUnavailable}, Version: "v1"}
	a := RetryAttempt{LogicalOperationID: "logical", OperationKind: "other", TenantID: "t", Dependency: "d", Failure: FailureUnavailable, Attempt: 1}
	if _, err := ConsumeRetry(b, a); !errors.Is(err, ErrInvalidRetryInput) {
		t.Fatalf("operation mismatch err=%v", err)
	}
	maxInt := int(^uint(0) >> 1)
	b.OperationKind, a.OperationKind, b.Allowed, b.Refunded = "op", "op", maxInt, 1
	if _, err := ConsumeRetry(b, a); !errors.Is(err, ErrInvalidRetryInput) {
		t.Fatalf("counter overflow err=%v", err)
	}
	b.Allowed, b.Consumed, b.Refunded = maxInt, maxInt, 0
	if receipt, err := ConsumeRetry(b, a); err != nil || receipt.Disposition != RetryBudgetExhausted || receipt.Remaining != 0 {
		t.Fatalf("safe max-int exhausted budget=%+v err=%v", receipt, err)
	}
}

func TestTodo_ADMISSION_002_Mutation(t *testing.T) {
	r, s := base()
	r.Criticality = Criticality("P00")
	if got := Decide(r, s, Policy{}); got.Outcome != Reject {
		t.Fatalf("invalid criticality admitted: %+v", got)
	}
}
