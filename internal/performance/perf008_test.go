package performance

import (
	"testing"
	"time"
)

func perf008Baseline() RegressionBaseline {
	return RegressionBaseline{
		BaselineID: "workload-peak@v1",
		RecordedAt: "ci-baseline-2026-09-01",
		Operations: []MetricSnapshot{
			{
				Name: "read", P50: 10 * time.Millisecond, P95: 100 * time.Millisecond,
				P99:              200 * time.Millisecond,
				ThroughputPerSec: 1000,
				AllocBytes:       1 << 20,
				Goroutines:       50,
			},
		},
	}
}

func perf008Current() []MetricSnapshot {
	return []MetricSnapshot{
		{
			Name: "read", P50: 10 * time.Millisecond, P95: 100 * time.Millisecond,
			P99:              200 * time.Millisecond,
			ThroughputPerSec: 1000,
			AllocBytes:       1 << 20,
			Goroutines:       50,
		},
	}
}

// TestTodo_PERF_008 gates the release on measured regressions: a
// >10% p99 regression fails the gate unless a live, owned exception
// with tested recovery waives exactly that metric.
func TestTodo_PERF_008(t *testing.T) {
	budgets := []OperationBudget{
		{Name: "read", P50: 250 * time.Millisecond, P95: 500 * time.Millisecond, P99: time.Second},
	}
	verdict, err := CheckRegression(perf008Current(), perf008Baseline(), nil, 0, budgets)
	if err != nil {
		t.Fatal(err)
	}
	if !verdict.Passed || len(verdict.Findings) == 0 {
		t.Fatalf("clean bill = %+v", verdict)
	}
	for _, finding := range verdict.Findings {
		if finding.Breached {
			t.Fatalf("clean bill breached: %+v", finding)
		}
	}
	// Seeded defect: a 12% p99 regression must fail the gate rather
	// than passing quietly.
	regressed := perf008Current()
	regressed[0].P99 = 224 * time.Millisecond
	verdict, err = CheckRegression(regressed, perf008Baseline(), nil, 0, budgets)
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Passed {
		t.Fatal("12% p99 regression passed the gate")
	}
	found := false
	for _, finding := range verdict.Findings {
		if finding.Operation == "read" && finding.Metric == "p99" && !finding.Waived {
			found = true
		}
	}
	if !found {
		t.Fatalf("no p99 finding: %+v", verdict.Findings)
	}
	// A live exception with owner, rationale, expiry and tested
	// recovery waives exactly the breaching metric.
	verdict, err = CheckRegression(regressed, perf008Baseline(), []Exception{{
		Operation: "read", Metrics: []string{"p99"},
		Owner: "perf-owner", Rationale: "known allocator regression, fix in flight",
		ExpiresUnixMilli: 1893456000000, RecoveryRef: "TestTodo_PERF_008_Fault",
	}}, 1756684800000, budgets)
	if err != nil {
		t.Fatal(err)
	}
	if !verdict.Passed {
		t.Fatalf("waived regression failed: %+v", verdict.Findings)
	}
	if verdict.Digest == "" {
		t.Fatal("verdict is not digested")
	}
}
