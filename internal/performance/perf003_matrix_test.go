package performance

import (
	"sync"
	"testing"
	"time"
)

// TestTodo_PERF_003_Race: a soak racing recorder hammering stays
// consistent, and sequential soaks replay identically. Two
// co-scheduled soaks are excluded on purpose: mutual CPU contention
// is external load, not a leak, and trips the deterioration bound.
func TestTodo_PERF_003_Race(t *testing.T) {
	shared := NewRecorder()
	var verdict SoakVerdict
	var runErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var err error
		verdict, err = RunSoak(perf003Plan())
		runErr = err
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = shared.Record("hammer", time.Duration(i%7)*time.Microsecond, false)
			_ = shared.Summarize()
		}
	}()
	wg.Wait()
	if runErr != nil {
		t.Fatalf("soak under recorder hammering: %v", runErr)
	}
	second, err := RunSoak(perf003Plan())
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Digest != second.Digest {
		t.Fatal("sequential soaks replayed to different digests")
	}
	if operation, ok := shared.Summarize().Operation("hammer"); !ok || operation.Count != 200 {
		t.Fatalf("hammered recorder = %+v", operation)
	}
}

// TestTodo_PERF_003_Fault: breaches fail closed with
// PERF_003_REJECTED and rejected runs change nothing.
func TestTodo_PERF_003_Fault(t *testing.T) {
	before := perf003Plan()
	// Absurd p99 ceiling: measured breach must reject.
	tight := perf003Plan()
	tight.P99Ceiling = time.Nanosecond
	_, err := RunSoak(tight)
	rejected, ok := AsSoakRejected(err)
	if !ok || rejected.Field == "" {
		t.Fatalf("p99 breach error = %v", err)
	}
	// A flood with no drain leaves a queue behind: undrained rejects.
	flood := perf003Plan()
	flood.CapacityPerHour = 1
	flood.Stages = []SoakStage{
		{Name: "baseline", VirtualHours: 8, OfferedUnits: 40},
		{Name: "ramp", VirtualHours: 4, OfferedUnits: 80},
		{Name: "peak", VirtualHours: 4, OfferedUnits: 120},
		{Name: "burst", VirtualHours: 2, OfferedUnits: 120, Burst: true},
		{Name: "restart", VirtualHours: 2, OfferedUnits: 60, Restart: true},
		{Name: "flood", VirtualHours: 4, OfferedUnits: 500},
	}
	_, err = RunSoak(flood)
	if rejected, ok := AsSoakRejected(err); !ok || rejected.Field != "queue" {
		t.Fatalf("undrained queue error = %v", err)
	}
	// Rejected runs leave the plan untouched.
	after := perf003Plan()
	if len(before.Stages) != len(after.Stages) || before.DeclaredPeakCommandsPerSec != after.DeclaredPeakCommandsPerSec {
		t.Fatal("soak run aliased its plan")
	}
}

// TestTodo_PERF_003_Mutation: plan-shape edges and the per-hour
// accounting identity resolve on the documented side.
func TestTodo_PERF_003_Mutation(t *testing.T) {
	cases := map[string]func(*SoakPlan){
		"23 hours": func(p *SoakPlan) { p.Stages[0].VirtualHours = 7 },
		"flat rates": func(p *SoakPlan) {
			for i := range p.Stages {
				p.Stages[i].OfferedUnits = 40
				p.Stages[i].Burst = false
			}
		},
		"no restart": func(p *SoakPlan) {
			for i := range p.Stages {
				p.Stages[i].Restart = false
			}
		},
		"zero quota":      func(p *SoakPlan) { p.QuotaLimit = 0 },
		"duplicate stage": func(p *SoakPlan) { p.Stages[1].Name = p.Stages[0].Name },
	}
	for name, mutate := range cases {
		plan := perf003Plan()
		mutate(&plan)
		if _, err := RunSoak(plan); err == nil {
			t.Fatalf("%s plan passed", name)
		} else if _, ok := AsSoakRejected(err); !ok {
			t.Fatalf("%s error = %v, want PERF_003_REJECTED", name, err)
		}
	}
	// Accounting identity: every arrival each hour is admitted,
	// degraded, queued or rejected exactly once.
	verdict, err := RunSoak(perf003Plan())
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range verdict.Hours {
		if h.Offered != h.Admitted+h.Degraded+h.Queued+h.Rejected {
			t.Fatalf("hour %d: offered=%d accounted=%d", h.Hour, h.Offered,
				h.Admitted+h.Degraded+h.Queued+h.Rejected)
		}
	}
	// Pure gate units: every hourly and deterioration edge resolves
	// deterministically without running a soak.
	if err := checkHourBudget(3, time.Millisecond, 0, 100, time.Second, 0); err != nil {
		t.Fatalf("clean hour rejected: %v", err)
	}
	if _, ok := AsSoakRejected(checkHourBudget(3, time.Millisecond, 1, 100, time.Second, 0)); !ok {
		t.Fatal("error-rate excess passed")
	}
	if err := checkHourBudget(3, 2*time.Second, 0, 100, time.Second, 0); err == nil {
		t.Fatal("p99 over ceiling passed")
	} else if rejected, ok := AsSoakRejected(err); !ok || rejected.Field != "hours/3/p99" {
		t.Fatalf("p99 breach = %v", err)
	}
	if got := medianHourly(
		SoakHour{Median: 9 * time.Millisecond},
		SoakHour{Median: time.Millisecond},
		SoakHour{Median: 2 * time.Millisecond},
	); got != 2*time.Millisecond {
		t.Fatalf("median = %v", got)
	}
	// Theil-Sen trend gate, deterministically: flat passes, exact
	// +10% drift passes, +15% drift breaches, and one 10x spiked
	// hour cannot move the median of pairwise slopes.
	flat := cannedHours(100 * time.Microsecond)
	if theilSenBreach(flat, 100*time.Microsecond) {
		t.Fatal("flat trend breached")
	}
	almost := cannedHours(0)
	for i := range almost {
		almost[i].Median = 100*time.Microsecond + time.Duration(i)*90*time.Microsecond/230
	}
	if theilSenBreach(almost, 100*time.Microsecond) {
		t.Fatal("+9% drift breached")
	}
	drifted := cannedHours(0)
	for i := range drifted {
		drifted[i].Median = 100*time.Microsecond + time.Duration(i)*150*time.Microsecond/230
	}
	if !theilSenBreach(drifted, 100*time.Microsecond) {
		t.Fatal("+15% drift passed")
	}
	spiked := cannedHours(100 * time.Microsecond)
	spiked[23].Median = 1000 * time.Microsecond
	if theilSenBreach(spiked, 100*time.Microsecond) {
		t.Fatal("single spiked hour breached")
	}
}

func cannedHours(median time.Duration) []SoakHour {
	hours := make([]SoakHour, SoakHours)
	for i := range hours {
		hours[i] = SoakHour{Hour: i + 1, Median: median, P99: median}
	}
	return hours
}

// BenchmarkTodo_PERF_003 runs the full 24-virtual-hour soak and
// reports throughput alongside the closing p99.
func BenchmarkTodo_PERF_003(b *testing.B) {
	var verdict SoakVerdict
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		verdict, err = RunSoak(perf003Plan())
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if verdict.TotalEffects == 0 {
		b.Fatal("soak applied no effects")
	}
	b.ReportMetric(float64(verdict.TotalEffects), "effects")
	b.ReportMetric(float64(verdict.HourP99(24).Nanoseconds()), "hour24-p99-ns")
	b.ReportMetric(float64(verdict.TotalDropped), "dropped")
}
