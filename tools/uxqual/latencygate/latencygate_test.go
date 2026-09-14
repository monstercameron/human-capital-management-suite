package latencygate

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestEvaluateUsesNearestRankWithoutMutatingCorpus(t *testing.T) {
	durations := make([]time.Duration, 100)
	for index := range durations {
		durations[index] = time.Duration(100-index) * time.Millisecond
	}
	result := Evaluate("fixture", durations)
	if result.Samples != 100 || result.P50 != 50*time.Millisecond || result.P95 != 95*time.Millisecond || result.Max != 100*time.Millisecond {
		t.Fatalf("result = %+v", result)
	}
	if durations[0] != 100*time.Millisecond || durations[99] != time.Millisecond {
		t.Fatal("Evaluate mutated the caller's duration corpus")
	}
}

func TestCheckReportsActionableBreach(t *testing.T) {
	budget := Budget{Name: "people sort", P95: 50 * time.Millisecond, Samples: 20}
	err := Check(budget, Result{Name: budget.Name, Samples: 20, P50: 20 * time.Millisecond, P95: 51 * time.Millisecond, Max: 70 * time.Millisecond})
	if err == nil {
		t.Fatal("expected breached budget to fail")
	}
	for _, want := range []string{"people sort", "51ms", "50ms", "p50=20ms", "max=70ms", "n=20"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("breach error %q missing %q", err, want)
		}
	}
}

func TestMeasureWarmsUpAndPropagatesOperationFailure(t *testing.T) {
	budget := Budget{Name: "fixture", P95: time.Second, Warmups: 2, Samples: 20}
	calls := 0
	result, err := Measure(budget, func() error {
		calls++
		if calls == 7 {
			return errors.New("render failed")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "sample 5") || !strings.Contains(err.Error(), "render failed") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestBudgetValidationRejectsStatisticallyWeakGate(t *testing.T) {
	_, err := Measure(Budget{Name: "fixture", P95: time.Second, Samples: 19}, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "at least 20 samples") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckRejectsMismatchedOrForgedPercentiles(t *testing.T) {
	budget := Budget{Name: "route", P95: 20 * time.Millisecond, Samples: 20}
	for name, result := range map[string]Result{
		"wrong name":             {Name: "other", Samples: 20, P50: time.Millisecond, P95: time.Millisecond, Max: time.Millisecond},
		"descending percentiles": {Name: budget.Name, Samples: 20, P50: 3 * time.Millisecond, P95: 2 * time.Millisecond, Max: 4 * time.Millisecond},
		"p95 above max":          {Name: budget.Name, Samples: 20, P50: time.Millisecond, P95: 4 * time.Millisecond, Max: 2 * time.Millisecond},
		"negative":               {Name: budget.Name, Samples: 20, P50: -time.Millisecond, P95: time.Millisecond, Max: time.Millisecond},
	} {
		if err := Check(budget, result); err == nil {
			t.Errorf("%s: forged result passed", name)
		}
	}
}

func TestLayoutShiftGateSumsEntriesAndRejectsMalformedTelemetry(t *testing.T) {
	budget := LayoutShiftBudget{Name: "route transition", Max: DefaultCLS}
	result := EvaluateLayoutShift(budget.Name, []float64{0.02, 0.03, 0.01})
	if result.Samples != 3 || math.Abs(result.Score-0.06) > 1e-12 || math.Abs(result.Max-0.03) > 1e-12 || result.Invalid {
		t.Fatalf("layout-shift result = %+v", result)
	}
	if err := CheckLayoutShift(budget, result); err != nil {
		t.Fatal(err)
	}
	if err := CheckCLS(budget, EvaluateCLS(budget.Name, []float64{0.08, 0.03})); err == nil {
		t.Fatal("over-budget CLS passed")
	}
	malformed := EvaluateLayoutShift(budget.Name, []float64{0.01, -0.01})
	if !malformed.Invalid {
		t.Fatal("negative layout-shift entry was not marked invalid")
	}
	if err := CheckLayoutShift(budget, malformed); err == nil {
		t.Fatal("malformed layout-shift result passed")
	}
}
