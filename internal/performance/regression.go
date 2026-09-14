// Regression gate: PERF-008 compares measured operation snapshots
// against pinned baselines and fails the release on regression.
//
// Breach rules: p95 or p99 growth beyond 10%, throughput drop beyond
// 10%, memory/DB/WAL growth beyond 15%, goroutine growth beyond 2x
// (unsafe scale: a leak signature), or any plan-budget breach. A
// breach fails the verdict instead of erroring, so CI reports what
// regressed. Exceptions waive only their listed metrics while live:
// each needs an owner, a rationale, an unexpired expiry and a tested
// recovery reference, validated up front — malformed, phantom or
// expired exceptions never waive. Structural problems (missing
// snapshots, unknown operations) error fail-closed.
package performance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Gated metric names.
const (
	MetricP95        = "p95"
	MetricP99        = "p99"
	MetricThroughput = "throughput"
	MetricMemory     = "memory"
	MetricDB         = "db"
	MetricWAL        = "wal"
	MetricGoroutines = "goroutines"
)

// Breach bounds. Comparisons carry a small epsilon so an exact
// boundary ratio passes and only genuine excess breaches.
const (
	maxLatencyRatio   = 1.10
	minThroughputRate = 0.90
	maxResourceGrowth = 1.15
	maxGoroutineRatio = 2.0
	ratioEpsilon      = 1e-9
)

// MetricSnapshot is the measured fact set for one operation. DB and
// WAL dimensions are zero when unmeasured: growth ratios against an
// unmeasured baseline are skipped, never zero-divided.
type MetricSnapshot struct {
	Name             string
	P50              time.Duration
	P95              time.Duration
	P99              time.Duration
	ThroughputPerSec float64
	AllocBytes       uint64
	DBP95Millis      int64
	WALBytes         int64
	Goroutines       int
}

// Validate implements validation.
func (s MetricSnapshot) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("performance: metric snapshot name is required")
	}
	if s.P50 < 0 || s.P95 < 0 || s.P99 < 0 || s.ThroughputPerSec < 0 || s.DBP95Millis < 0 || s.WALBytes < 0 || s.Goroutines < 0 {
		return fmt.Errorf("performance: negative metric in %q", s.Name)
	}
	return nil
}

// SnapshotFromOperation lifts a recorder operation summary into a
// snapshot with direct resource dimensions.
func SnapshotFromOperation(operation OperationSummary, allocBytes uint64, goroutines int) MetricSnapshot {
	return MetricSnapshot{
		Name: operation.Name, P50: operation.P50, P95: operation.P95, P99: operation.P99,
		ThroughputPerSec: operation.ThroughputPerSec, AllocBytes: allocBytes, Goroutines: goroutines,
	}
}

// RegressionBaseline pins the snapshots one release is measured
// against, with provenance instead of embedded magic numbers.
type RegressionBaseline struct {
	BaselineID string
	RecordedAt string
	Operations []MetricSnapshot
}

// Validate implements validation.
func (b RegressionBaseline) Validate() error {
	if strings.TrimSpace(b.BaselineID) == "" || strings.TrimSpace(b.RecordedAt) == "" {
		return errors.New("performance: baseline id and recorded-at are required")
	}
	if len(b.Operations) == 0 {
		return errors.New("performance: baseline carries no operations")
	}
	seen := make(map[string]struct{}, len(b.Operations))
	for _, operation := range b.Operations {
		if err := operation.Validate(); err != nil {
			return err
		}
		if _, ok := seen[operation.Name]; ok {
			return fmt.Errorf("performance: duplicate baseline operation %q", operation.Name)
		}
		seen[operation.Name] = struct{}{}
	}
	return nil
}

// Exception waives listed metrics for one operation while live.
type Exception struct {
	Operation        string
	Metrics          []string
	Owner            string
	Rationale        string
	ExpiresUnixMilli int64
	RecoveryRef      string
}

// Validate implements validation.
func (e Exception) Validate() error {
	if strings.TrimSpace(e.Operation) == "" {
		return errors.New("performance: exception operation is required")
	}
	if len(e.Metrics) == 0 {
		return fmt.Errorf("performance: exception for %q waives no metric", e.Operation)
	}
	for _, metric := range e.Metrics {
		switch metric {
		case MetricP95, MetricP99, MetricThroughput, MetricMemory, MetricDB, MetricWAL, MetricGoroutines:
		default:
			return fmt.Errorf("performance: exception for %q names unknown metric %q", e.Operation, metric)
		}
	}
	if strings.TrimSpace(e.Owner) == "" || strings.TrimSpace(e.Rationale) == "" {
		return fmt.Errorf("performance: exception for %q needs owner and rationale", e.Operation)
	}
	if e.ExpiresUnixMilli <= 0 {
		return fmt.Errorf("performance: exception for %q needs an expiry", e.Operation)
	}
	if strings.TrimSpace(e.RecoveryRef) == "" {
		return fmt.Errorf("performance: exception for %q needs a tested recovery ref", e.Operation)
	}
	return nil
}

// GateFinding is one evaluated metric comparison.
type GateFinding struct {
	Operation string
	Metric    string
	Baseline  float64
	Current   float64
	Ratio     float64
	Waived    bool
	Breached  bool
}

// GateVerdict is the deterministic gate account.
type GateVerdict struct {
	Passed   bool
	Findings []GateFinding
	Digest   string
}

// CheckRegression evaluates current snapshots against a baseline.
// nowUnixMilli dates exception expiries without a clock dependency;
// budgets, when supplied, add plan-ceiling SLO checks per operation.
func CheckRegression(current []MetricSnapshot, baseline RegressionBaseline, exceptions []Exception, nowUnixMilli int64, budgets []OperationBudget) (GateVerdict, error) {
	if err := baseline.Validate(); err != nil {
		return GateVerdict{}, err
	}
	for _, exception := range exceptions {
		if err := exception.Validate(); err != nil {
			return GateVerdict{}, err
		}
	}
	live := make(map[string]map[string]bool, len(exceptions))
	for _, exception := range exceptions {
		known := false
		for _, operation := range baseline.Operations {
			if operation.Name == exception.Operation {
				known = true
			}
		}
		if !known {
			return GateVerdict{}, fmt.Errorf("performance: exception for unknown operation %q", exception.Operation)
		}
		if nowUnixMilli >= exception.ExpiresUnixMilli {
			continue
		}
		waived, ok := live[exception.Operation]
		if !ok {
			waived = make(map[string]bool)
			live[exception.Operation] = waived
		}
		for _, metric := range exception.Metrics {
			waived[metric] = true
		}
	}
	measured := make(map[string]MetricSnapshot, len(current))
	for _, snapshot := range current {
		if err := snapshot.Validate(); err != nil {
			return GateVerdict{}, err
		}
		if _, ok := measured[snapshot.Name]; ok {
			return GateVerdict{}, fmt.Errorf("performance: duplicate current snapshot %q", snapshot.Name)
		}
		measured[snapshot.Name] = snapshot
	}
	names := make([]string, 0, len(baseline.Operations))
	for _, operation := range baseline.Operations {
		names = append(names, operation.Name)
	}
	sort.Strings(names)
	verdict := GateVerdict{Passed: true}
	for _, name := range names {
		snapshot, ok := measured[name]
		if !ok {
			return GateVerdict{}, fmt.Errorf("performance: no current snapshot for %q", name)
		}
		verdict.Findings = append(verdict.Findings, findingsGate(snapshot, baselineOperation(baseline, name), live[name])...)
	}
	if len(budgets) > 0 {
		summary := Summary{}
		for _, snapshot := range current {
			summary.Operations = append(summary.Operations, OperationSummary{
				Name: snapshot.Name, Count: 1, P50: snapshot.P50, P95: snapshot.P95, P99: snapshot.P99,
				ThroughputPerSec: snapshot.ThroughputPerSec,
			})
		}
		if err := CheckBudget(summary, budgets); err != nil {
			verdict.Passed = false
			verdict.Findings = append(verdict.Findings, GateFinding{Operation: "*", Metric: "slo", Breached: true})
		}
	}
	for _, finding := range verdict.Findings {
		if finding.Breached && !finding.Waived {
			verdict.Passed = false
		}
	}
	verdict.Digest = digestGateVerdict(baseline.BaselineID, verdict.Findings)
	return verdict, nil
}

func baselineOperation(baseline RegressionBaseline, name string) MetricSnapshot {
	for _, operation := range baseline.Operations {
		if operation.Name == name {
			return operation
		}
	}
	return MetricSnapshot{}
}

func findingsGate(current, base MetricSnapshot, waived map[string]bool) []GateFinding {
	var findings []GateFinding
	add := func(metric string, baseline, now, bound float64, over bool) {
		if baseline == 0 && over {
			return
		}
		r := now / baseline
		breached := r > bound+ratioEpsilon
		if !over {
			breached = baseline > 0 && r < bound-ratioEpsilon
		}
		findings = append(findings, GateFinding{
			Operation: current.Name, Metric: metric,
			Baseline: baseline, Current: now, Ratio: r,
			Waived: waived[metric], Breached: breached,
		})
	}
	add(MetricP95, float64(base.P95), float64(current.P95), maxLatencyRatio, true)
	add(MetricP99, float64(base.P99), float64(current.P99), maxLatencyRatio, true)
	if base.ThroughputPerSec > 0 {
		add(MetricThroughput, base.ThroughputPerSec, current.ThroughputPerSec, minThroughputRate, false)
	}
	if base.AllocBytes > 0 {
		add(MetricMemory, float64(base.AllocBytes), float64(current.AllocBytes), maxResourceGrowth, true)
	}
	if base.DBP95Millis > 0 {
		add(MetricDB, float64(base.DBP95Millis), float64(current.DBP95Millis), maxResourceGrowth, true)
	}
	if base.WALBytes > 0 {
		add(MetricWAL, float64(base.WALBytes), float64(current.WALBytes), maxResourceGrowth, true)
	}
	if base.Goroutines > 0 {
		add(MetricGoroutines, float64(base.Goroutines), float64(current.Goroutines), maxGoroutineRatio, true)
	}
	return findings
}

func digestGateVerdict(baselineID string, findings []GateFinding) string {
	parts := []string{"perf008-gate", baselineID}
	for _, finding := range findings {
		parts = append(parts, strings.Join([]string{
			finding.Operation, finding.Metric,
			strconv.FormatFloat(finding.Baseline, 'f', 6, 64),
			strconv.FormatFloat(finding.Current, 'f', 6, 64),
			strconv.FormatFloat(finding.Ratio, 'f', 6, 64),
			strconv.FormatBool(finding.Waived),
			strconv.FormatBool(finding.Breached),
		}, "\x00"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
