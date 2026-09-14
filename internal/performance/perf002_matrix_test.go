package performance

import (
	"sync"
	"testing"
	"time"
)

// TestTodo_PERF_002_Race: concurrent recording never corrupts the
// report.
func TestTodo_PERF_002_Race(t *testing.T) {
	recorder := NewRecorder()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := recorder.Run("read", 25, func() error { return Small.Validate() }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = recorder.Summarize()
			time.Sleep(time.Millisecond)
		}
	}()
	wg.Wait()
	summary, ok := recorder.Summarize().Operation("read")
	if !ok || summary.Count != 200 || summary.Errors != 0 {
		t.Fatalf("summary = %+v, ok=%v", summary, ok)
	}
	if err := CheckBudget(recorderSummary(summary), []OperationBudget{
		{Name: "read", P50: 250 * time.Millisecond, P95: 500 * time.Millisecond, P99: time.Second},
	}); err != nil {
		t.Fatal(err)
	}
}

func recorderSummary(operation OperationSummary) Summary {
	return Summary{Operations: []OperationSummary{operation}}
}

// BenchmarkTodo_PERF_002 measures the local-commit stand-in and
// reports its distribution alongside throughput. Each recorded sample
// batches enough hashes to sit above coarse platform clock ticks.
func BenchmarkTodo_PERF_002(b *testing.B) {
	const batch = 500
	recorder := NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		for j := 0; j < batch; j++ {
			if err := digestPayload(); err != nil {
				b.Fatal(err)
			}
		}
		if err := recorder.Record("local-commit", time.Since(start), false); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	operation, ok := recorder.Summarize().Operation("local-commit")
	if !ok || operation.Count != b.N {
		b.Fatalf("count = %+v, want %d", operation, b.N)
	}
	b.ReportMetric(float64(operation.P50.Nanoseconds()), "p50-ns")
	b.ReportMetric(float64(operation.P95.Nanoseconds()), "p95-ns")
	b.ReportMetric(float64(operation.P99.Nanoseconds()), "p99-ns")
	b.ReportMetric(operation.ThroughputPerSec, "ops-per-sec")
}
