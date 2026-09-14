package performance

import (
	"testing"
	"time"
)

// perf002Budgets pins the plan targets: read p95 <500ms, preflight p95
// <2s, local commit p99 <1s and timer wake p95 <5s.
func perf002Budgets() []OperationBudget {
	return []OperationBudget{
		{Name: "read", P50: 250 * time.Millisecond, P95: 500 * time.Millisecond, P99: time.Second},
		{Name: "preflight", P50: time.Second, P95: 2 * time.Second, P99: 4 * time.Second},
		{Name: "local-commit", P50: 500 * time.Millisecond, P95: 750 * time.Millisecond, P99: time.Second},
		{Name: "timer-wake", P50: 2500 * time.Millisecond, P95: 5 * time.Second, P99: 10 * time.Second},
	}
}

// TestTodo_PERF_002 benchmarks the local pilot path and proves the
// harness records p50/p95/p99, throughput and errors per operation.
func TestTodo_PERF_002(t *testing.T) {
	recorder := NewRecorder()
	if err := recorder.Run("read", 100, func() error { return Small.Validate() }); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Run("preflight", 100, func() error { return Check(Small, DefaultHardLimits) }); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Run("local-commit", 100, func() error { return digestPayload() }); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Run("timer-wake", 50, func() error { time.Sleep(time.Millisecond); return nil }); err != nil {
		t.Fatal(err)
	}
	summary := recorder.Summarize()
	for _, budget := range perf002Budgets() {
		operation, ok := summary.Operation(budget.Name)
		if !ok {
			t.Fatalf("no summary for %q", budget.Name)
		}
		if operation.Count == 0 || operation.Errors != 0 {
			t.Fatalf("%q: count=%d errors=%d", budget.Name, operation.Count, operation.Errors)
		}
		if !(operation.P50 <= operation.P95 && operation.P95 <= operation.P99) {
			t.Fatalf("%q: unordered percentiles %v/%v/%v", budget.Name, operation.P50, operation.P95, operation.P99)
		}
		if operation.ThroughputPerSec <= 0 {
			t.Fatalf("%q: no throughput recorded", budget.Name)
		}
	}
	// Seeded defect: budget enforcement must reject a breach rather
	// than reporting pass on recorded numbers alone.
	breacher := NewRecorder()
	breacher.Record("read", 10*time.Second, false)
	if err := CheckBudget(breacher.Summarize(), perf002Budgets()); err == nil {
		t.Fatal("p95 breach reported pass")
	}
	if err := CheckBudget(summary, perf002Budgets()); err != nil {
		t.Fatal(err)
	}
}
