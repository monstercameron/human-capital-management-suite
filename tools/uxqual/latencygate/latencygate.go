// Package latencygate provides a small, deterministic contract for measured
// UI interaction budgets. It deliberately reports percentiles rather than a
// single run so the gate tolerates isolated scheduler noise while still
// rejecting consistently slow interactions.
package latencygate

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// DefaultCLS is the frontend cumulative-layout-shift ceiling used by route
// and loading visual checks.
const DefaultCLS = 0.1

// Budget describes one named interaction and its maximum allowed p95.
type Budget struct {
	Name    string
	P95     time.Duration
	Warmups int
	Samples int
}

// Result is the compact evidence emitted by a measured interaction gate.
type Result struct {
	Name    string
	Samples int
	P50     time.Duration
	P95     time.Duration
	Max     time.Duration
}

// LayoutShiftBudget describes the maximum cumulative layout-shift score for
// one measured surface. CLS is a unitless score; the usual product budget is
// 0.1 for a page load or route transition.
type LayoutShiftBudget struct {
	Name string
	Max  float64
}

// LayoutShiftResult is the deterministic summary of layout-shift entries.
// Entries are summed because CLS is cumulative, while Max is retained to make
// one pathological movement visible in diagnostic output.
type LayoutShiftResult struct {
	Name    string
	Samples int
	Score   float64
	Max     float64
	Invalid bool
}

func (result LayoutShiftResult) String() string {
	return fmt.Sprintf("%s: entries=%d cls=%.4f max=%.4f", result.Name, result.Samples, result.Score, result.Max)
}

func (result Result) String() string {
	return fmt.Sprintf("%s: n=%d p50=%s p95=%s max=%s", result.Name, result.Samples, result.P50, result.P95, result.Max)
}

// EvaluateLayoutShift summarizes layout-shift entries without mutating the
// caller's slice. Negative, NaN, and infinite values are retained as an
// invalid result so telemetry adapters cannot silently turn malformed data
// into a passing zero score.
func EvaluateLayoutShift(name string, entries []float64) LayoutShiftResult {
	result := LayoutShiftResult{Name: name, Samples: len(entries)}
	for _, entry := range entries {
		if entry < 0 || math.IsNaN(entry) || math.IsInf(entry, 0) {
			result.Invalid = true
			continue
		}
		result.Score += entry
		if entry > result.Max {
			result.Max = entry
		}
	}
	return result
}

// CheckLayoutShift rejects a malformed or over-budget cumulative layout-shift
// result. The name and entry count are checked as well as the score so a
// result from another surface cannot accidentally satisfy this gate.
func CheckLayoutShift(budget LayoutShiftBudget, result LayoutShiftResult) error {
	if err := validateLayoutShiftBudget(budget); err != nil {
		return err
	}
	if result.Name != budget.Name {
		return fmt.Errorf("latencygate: layout-shift result %q does not match budget %q", result.Name, budget.Name)
	}
	if result.Invalid || math.IsNaN(result.Score) || math.IsInf(result.Score, 0) || result.Score < 0 {
		return fmt.Errorf("latencygate: %s layout-shift result is malformed", budget.Name)
	}
	if result.Score > budget.Max {
		return fmt.Errorf("latencygate: %s CLS %.4f exceeds budget %.4f (entries=%d max=%.4f)", budget.Name, result.Score, budget.Max, result.Samples, result.Max)
	}
	return nil
}

// CLS aliases keep the browser metric easy to discover for callers that use
// the web-vitals name directly.
type CLSBudget = LayoutShiftBudget
type CLSResult = LayoutShiftResult

func EvaluateCLS(name string, entries []float64) CLSResult {
	return EvaluateLayoutShift(name, entries)
}

func CheckCLS(budget CLSBudget, result CLSResult) error {
	return CheckLayoutShift(budget, result)
}

// Measure warms the operation, records the requested samples, and returns a
// percentile result. Operation errors stop the measurement immediately.
func Measure(budget Budget, operation func() error) (Result, error) {
	if err := validate(budget, operation); err != nil {
		return Result{}, err
	}
	for index := 0; index < budget.Warmups; index++ {
		if err := operation(); err != nil {
			return Result{}, fmt.Errorf("latencygate: %s warmup %d: %w", budget.Name, index+1, err)
		}
	}

	durations := make([]time.Duration, budget.Samples)
	for index := range durations {
		started := time.Now()
		if err := operation(); err != nil {
			return Result{}, fmt.Errorf("latencygate: %s sample %d: %w", budget.Name, index+1, err)
		}
		durations[index] = time.Since(started)
	}
	return Evaluate(budget.Name, durations), nil
}

// Evaluate summarizes an existing duration corpus. It is exported so callers
// with browser or telemetry timings can apply the same nearest-rank policy.
func Evaluate(name string, durations []time.Duration) Result {
	if len(durations) == 0 {
		return Result{Name: name}
	}
	ordered := slices.Clone(durations)
	slices.Sort(ordered)
	return Result{
		Name: name, Samples: len(ordered),
		P50: nearestRank(ordered, 0.50),
		P95: nearestRank(ordered, 0.95),
		Max: ordered[len(ordered)-1],
	}
}

// Check rejects a result whose p95 exceeds the declared budget.
func Check(budget Budget, result Result) error {
	if err := validateBudget(budget); err != nil {
		return err
	}
	if result.Samples != budget.Samples {
		return fmt.Errorf("latencygate: %s collected %d samples, want %d", budget.Name, result.Samples, budget.Samples)
	}
	if result.Name != budget.Name {
		return fmt.Errorf("latencygate: result %q does not match budget %q", result.Name, budget.Name)
	}
	if result.P50 < 0 || result.P95 < 0 || result.Max < 0 || result.P50 > result.P95 || result.P95 > result.Max {
		return fmt.Errorf("latencygate: %s result percentiles are malformed (p50=%s p95=%s max=%s)", budget.Name, result.P50, result.P95, result.Max)
	}
	if result.P95 > budget.P95 {
		return fmt.Errorf("latencygate: %s p95 %s exceeds budget %s (p50=%s max=%s n=%d)",
			budget.Name, result.P95, budget.P95, result.P50, result.Max, result.Samples)
	}
	return nil
}

func validate(budget Budget, operation func() error) error {
	if err := validateBudget(budget); err != nil {
		return err
	}
	if operation == nil {
		return errors.New("latencygate: operation is required")
	}
	return nil
}

func validateBudget(budget Budget) error {
	if strings.TrimSpace(budget.Name) == "" {
		return errors.New("latencygate: budget name is required")
	}
	if budget.P95 <= 0 {
		return fmt.Errorf("latencygate: %s p95 budget must be positive", budget.Name)
	}
	if budget.Samples < 20 {
		return fmt.Errorf("latencygate: %s needs at least 20 samples", budget.Name)
	}
	if budget.Warmups < 0 {
		return fmt.Errorf("latencygate: %s warmups cannot be negative", budget.Name)
	}
	return nil
}

func validateLayoutShiftBudget(budget LayoutShiftBudget) error {
	if strings.TrimSpace(budget.Name) == "" {
		return errors.New("latencygate: layout-shift budget name is required")
	}
	if math.IsNaN(budget.Max) || math.IsInf(budget.Max, 0) || budget.Max <= 0 {
		return fmt.Errorf("latencygate: %s layout-shift budget must be finite and positive", budget.Name)
	}
	return nil
}

func nearestRank(ordered []time.Duration, percentile float64) time.Duration {
	index := int(math.Ceil(percentile*float64(len(ordered)))) - 1
	return ordered[max(0, min(index, len(ordered)-1))]
}
