package performance

import (
	"testing"
	"time"
)

func perf008Mutate(name string, mutate func(*MetricSnapshot)) []MetricSnapshot {
	current := perf008Current()
	mutate(&current[0])
	_ = name
	return current
}

// TestTodo_PERF_008_Fault: every breach class fails the gate, and
// exception discipline fails closed.
func TestTodo_PERF_008_Fault(t *testing.T) {
	baseline := perf008Baseline()
	breaches := map[string]struct {
		mutate func(*MetricSnapshot)
		metric string
	}{
		"throughput -15%": {func(s *MetricSnapshot) { s.ThroughputPerSec = 850 }, MetricThroughput},
		"memory +20%":     {func(s *MetricSnapshot) { s.AllocBytes = (1 << 20) * 12 / 10 }, MetricMemory},
		"goroutines 3x":   {func(s *MetricSnapshot) { s.Goroutines = 150 }, MetricGoroutines},
		"p95 +11%":        {func(s *MetricSnapshot) { s.P95 = 111 * time.Millisecond }, MetricP95},
	}
	for name, tc := range breaches {
		verdict, err := CheckRegression(perf008Mutate(name, tc.mutate), baseline, nil, 0, nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if verdict.Passed {
			t.Fatalf("%s passed the gate", name)
		}
		found := false
		for _, finding := range verdict.Findings {
			if finding.Metric == tc.metric && finding.Breached && !finding.Waived {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: no %s finding in %+v", name, tc.metric, verdict.Findings)
		}
	}
	// Exactly +10% passes: only genuine excess breaches.
	exact := perf008Current()
	exact[0].P99 = 220 * time.Millisecond
	verdict, err := CheckRegression(exact, baseline, nil, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !verdict.Passed {
		t.Fatalf("exact +10%% failed: %+v", verdict.Findings)
	}
	// An expired exception waives nothing.
	expired := perf008Current()
	expired[0].P99 = 224 * time.Millisecond
	live := []Exception{{
		Operation: "read", Metrics: []string{"p99"},
		Owner: "perf-owner", Rationale: "expired waiver",
		ExpiresUnixMilli: 1000, RecoveryRef: "TestTodo_PERF_008_Fault",
	}}
	verdict, err = CheckRegression(expired, baseline, live, 1756684800000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Passed {
		t.Fatal("expired exception waived a breach")
	}
	// Malformed and phantom exceptions error fail-closed.
	bad := []Exception{
		{Operation: "read", Metrics: []string{"p99"}, Rationale: "no owner", ExpiresUnixMilli: 1893456000000, RecoveryRef: "r"},
		{Operation: "read", Metrics: []string{"p99"}, Owner: "o", Rationale: "no recovery", ExpiresUnixMilli: 1893456000000},
		{Operation: "read", Metrics: []string{"nope"}, Owner: "o", Rationale: "r", ExpiresUnixMilli: 1893456000000, RecoveryRef: "r"},
		{Operation: "ghost", Metrics: []string{"p99"}, Owner: "o", Rationale: "r", ExpiresUnixMilli: 1893456000000, RecoveryRef: "r"},
	}
	for i, exception := range bad {
		if _, err := CheckRegression(perf008Current(), baseline, []Exception{exception}, 0, nil); err == nil {
			t.Fatalf("exception %d waived without discipline", i)
		}
	}
	// Missing and duplicate snapshots error.
	if _, err := CheckRegression(nil, baseline, nil, 0, nil); err == nil {
		t.Fatal("missing snapshot evaluated")
	}
	dup := append(perf008Current(), perf008Current()[0])
	if _, err := CheckRegression(dup, baseline, nil, 0, nil); err == nil {
		t.Fatal("duplicate snapshot evaluated")
	}
	// An SLO budget breach fails the gate even inside regression
	// bounds: baseline p99 200ms, current 210ms (+5%), budget 150ms.
	slo := perf008Current()
	slo[0].P99 = 210 * time.Millisecond
	tight := []OperationBudget{{Name: "read", P50: 250 * time.Millisecond, P95: 500 * time.Millisecond, P99: 150 * time.Millisecond}}
	verdict, err = CheckRegression(slo, baseline, nil, 0, tight)
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Passed {
		t.Fatal("SLO breach passed the gate")
	}
}

// BenchmarkTodo_PERF_008 evaluates the gate repeatedly and reports
// evaluation throughput.
func BenchmarkTodo_PERF_008(b *testing.B) {
	baseline := perf008Baseline()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		verdict, err := CheckRegression(perf008Current(), baseline, nil, 0, nil)
		if err != nil {
			b.Fatal(err)
		}
		if !verdict.Passed {
			b.Fatal("clean bill failed in benchmark")
		}
	}
	b.ReportMetric(float64(len(perf008Baseline().Operations)), "ops-per-eval")
}
