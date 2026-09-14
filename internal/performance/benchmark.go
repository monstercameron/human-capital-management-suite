// Latency benchmark harness: PERF-002 records per-operation latency
// distributions (p50/p95/p99), throughput and errors, then checks them
// against declared plan budgets.
//
// The harness measures the local pilot path only: read validation,
// admission preflight, local commit hashing and timer wake. Database
// and queue dimensions belong to the soak and admission proofs
// (PERF-003, ADMISSION-001) and the query-plan proofs (DB-020), not to
// this recorder — it reports exactly what it measures.
package performance

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// OperationBudget is the declared latency and error ceiling for one
// named pilot-path operation.
type OperationBudget struct {
	Name         string
	P50          time.Duration
	P95          time.Duration
	P99          time.Duration
	MaxErrorRate float64
}

// Validate implements validation.
func (b OperationBudget) Validate() error {
	if strings.TrimSpace(b.Name) == "" {
		return errors.New("performance: operation budget name is required")
	}
	if b.P50 <= 0 || b.P95 <= 0 || b.P99 <= 0 {
		return fmt.Errorf("performance: operation %q needs positive p50/p95/p99 ceilings", b.Name)
	}
	if b.MaxErrorRate < 0 || b.MaxErrorRate > 1 {
		return fmt.Errorf("performance: operation %q needs an error rate in [0,1]", b.Name)
	}
	return nil
}

type sample struct {
	at      time.Time
	latency time.Duration
	failed  bool
}

// Recorder gathers latency samples per named operation. It is safe
// for concurrent use.
type Recorder struct {
	mu      sync.Mutex
	samples map[string][]sample
}

// NewRecorder returns an empty recorder.
func NewRecorder() *Recorder { return &Recorder{samples: make(map[string][]sample)} }

// Record adds one sample. A negative latency is rejected.
func (r *Recorder) Record(name string, latency time.Duration, failed bool) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("performance: sample operation name is required")
	}
	if latency < 0 {
		return fmt.Errorf("performance: negative latency for %q", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.samples[name] = append(r.samples[name], sample{at: time.Now(), latency: latency, failed: failed})
	return nil
}

// Run executes fn iterations times, recording each call latency. A
// failing call is recorded as an error sample and Run continues, so
// the summary reports the error rate instead of hiding it.
func (r *Recorder) Run(name string, iterations int, fn func() error) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("performance: run operation name is required")
	}
	if iterations <= 0 {
		return fmt.Errorf("performance: run needs a positive iteration count for %q", name)
	}
	if fn == nil {
		return fmt.Errorf("performance: run needs a function for %q", name)
	}
	for i := 0; i < iterations; i++ {
		start := time.Now()
		err := fn()
		if recErr := r.Record(name, time.Since(start), err != nil); recErr != nil {
			return recErr
		}
	}
	return nil
}

// OperationSummary is the recorded distribution for one operation.
// Percentiles use the nearest-rank method over the recorded samples.
type OperationSummary struct {
	Name             string
	Count            int
	Errors           int
	P50              time.Duration
	P95              time.Duration
	P99              time.Duration
	ThroughputPerSec float64
}

// ResourceFootnote reports process resources observed at summary
// time: heap allocation and goroutine count.
type ResourceFootnote struct {
	AllocBytes uint64
	Goroutines int
}

// Summary is the per-operation benchmark report with its resource
// footnote.
type Summary struct {
	Operations []OperationSummary
	Resources  ResourceFootnote
}

// Operation returns the summary for one operation.
func (s Summary) Operation(name string) (OperationSummary, bool) {
	for _, operation := range s.Operations {
		if operation.Name == name {
			return operation, true
		}
	}
	return OperationSummary{}, false
}

// Summarize freezes the recorded samples into a deterministic
// report: operations sort by name and percentiles derive from sorted
// latencies.
func (r *Recorder) Summarize() Summary {
	r.mu.Lock()
	names := make([]string, 0, len(r.samples))
	for name := range r.samples {
		names = append(names, name)
	}
	sort.Strings(names)
	summary := Summary{}
	for _, name := range names {
		summary.Operations = append(summary.Operations, summarizeOperation(name, r.samples[name]))
	}
	r.mu.Unlock()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	summary.Resources = ResourceFootnote{AllocBytes: mem.Alloc, Goroutines: runtime.NumGoroutine()}
	return summary
}

func summarizeOperation(name string, samples []sample) OperationSummary {
	latencies := make([]time.Duration, 0, len(samples))
	operation := OperationSummary{Name: name, Count: len(samples)}
	var first, last time.Time
	for i, s := range samples {
		if s.failed {
			operation.Errors++
		}
		latencies = append(latencies, s.latency)
		if i == 0 || s.at.Before(first) {
			first = s.at
		}
		if i == 0 || s.at.After(last) {
			last = s.at
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	operation.P50 = nearestRank(latencies, 50)
	operation.P95 = nearestRank(latencies, 95)
	operation.P99 = nearestRank(latencies, 99)
	if elapsed := last.Sub(first); elapsed > 0 {
		operation.ThroughputPerSec = float64(len(samples)) / elapsed.Seconds()
	} else {
		operation.ThroughputPerSec = float64(len(samples))
	}
	return operation
}

func nearestRank(sorted []time.Duration, percent float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(percent / 100 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// CheckBudget fails closed: every budgeted operation must have
// samples, and every recorded percentile and error rate must sit at
// or under its ceiling. All breaches are reported together.
func CheckBudget(summary Summary, budgets []OperationBudget) error {
	seen := make(map[string]struct{}, len(budgets))
	var breaches []string
	for _, budget := range budgets {
		if err := budget.Validate(); err != nil {
			return err
		}
		if _, ok := seen[budget.Name]; ok {
			return fmt.Errorf("performance: duplicate budget for %q", budget.Name)
		}
		seen[budget.Name] = struct{}{}
		operation, ok := summary.Operation(budget.Name)
		if !ok || operation.Count == 0 {
			return fmt.Errorf("performance: no samples for budgeted operation %q", budget.Name)
		}
		if operation.P50 > budget.P50 {
			breaches = append(breaches, fmt.Sprintf("%s p50 %v exceeds %v", budget.Name, operation.P50, budget.P50))
		}
		if operation.P95 > budget.P95 {
			breaches = append(breaches, fmt.Sprintf("%s p95 %v exceeds %v", budget.Name, operation.P95, budget.P95))
		}
		if operation.P99 > budget.P99 {
			breaches = append(breaches, fmt.Sprintf("%s p99 %v exceeds %v", budget.Name, operation.P99, budget.P99))
		}
		if rate := float64(operation.Errors) / float64(operation.Count); rate > budget.MaxErrorRate {
			breaches = append(breaches, fmt.Sprintf("%s error rate %.4f exceeds %.4f", budget.Name, rate, budget.MaxErrorRate))
		}
	}
	if len(breaches) > 0 {
		return fmt.Errorf("performance: budget breached: %s", strings.Join(breaches, "; "))
	}
	return nil
}

var commitPayload = make([]byte, 1024)

// digestPayload hashes a fixed payload: the local-commit stand-in
// for the pilot path.
func digestPayload() error {
	_ = sha256.Sum256(commitPayload)
	return nil
}
