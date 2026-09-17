package intelligence

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func metricCorrectionDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 4, values.RoundingHalfUp)
	if err != nil {
		t.Fatalf("metricCorrectionDecimal %q: %v", text, err)
	}
	return d
}

func metricCorrectionWindow() (time.Time, time.Time) {
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	return from, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
}

func publishMetricOriginal(t *testing.T, log *CorrectionLog) MetricValue {
	t.Helper()
	from, to := metricCorrectionWindow()
	original, err := log.Publish(MetricValue{
		MetricID: "metric:attrition-rate", Version: "v2",
		Value: metricCorrectionDecimal(t, "0.0364"), Quality: QualityPartial,
		WindowStart: from, WindowEnd: to,
		PopulationDigest: "sha256:population-1",
		Authority:        "people-analytics", Reason: "june close",
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return original
}

func linkMetricDependents(t *testing.T, log *CorrectionLog, digest string) {
	t.Helper()
	for id, kind := range map[string]DependentKind{
		"report:attrition-june":   DependentReport,
		"dashboard:workforce":     DependentDashboard,
		"decision:retention-plan": DependentDecision,
	} {
		if err := log.LinkDependent(id, kind, digest); err != nil {
			t.Fatalf("LinkDependent %s: %v", id, err)
		}
	}
}

func correctionRequest(original MetricValue, key, value string, t *testing.T) CorrectionRequest {
	from, to := metricCorrectionWindow()
	return CorrectionRequest{
		IdempotencyKey: key, OriginalDigest: original.Digest,
		NewValue: metricCorrectionDecimal(t, value), Quality: QualityOK,
		WindowStart: from, WindowEnd: to,
		PopulationDigest: "sha256:population-2",
		Authority:        "people-analytics", Reason: "late leaver record added",
		ObservedAt: original.WindowEnd.Add(time.Hour),
	}
}

// TestTodo_METRIC_002 is the primary METRIC-002 contract test: a correction
// appends a new revision that preserves old and new values with authority,
// reason, window and population, and every dependent report, dashboard and
// decision link on the superseded value goes stale. The original is never
// overwritten.
func TestTodo_METRIC_002(t *testing.T) {
	t.Run("correction preserves both values with governance", func(t *testing.T) {
		log := NewCorrectionLog()
		original := publishMetricOriginal(t, log)
		linkMetricDependents(t, log, original.Digest)
		corrected, err := log.AppendCorrection(correctionRequest(original, "corr-1", "0.0371", t))
		if err != nil {
			t.Fatalf("AppendCorrection: %v", err)
		}
		if corrected.Value.String() != "0.0371" {
			t.Fatalf("corrected value = %s, want 0.0371", corrected.Value)
		}
		if corrected.Revision != original.Revision+1 || corrected.Supersedes != original.Digest {
			t.Fatalf("correction must chain the original: %+v vs %+v", corrected, original)
		}
		if corrected.Authority != "people-analytics" || corrected.Reason != "late leaver record added" {
			t.Fatalf("correction must carry authority and reason: %+v", corrected)
		}
		if corrected.PopulationDigest != "sha256:population-2" {
			t.Fatalf("correction must carry the new population: %+v", corrected)
		}
		if !corrected.WindowStart.Equal(original.WindowStart) || !corrected.WindowEnd.Equal(original.WindowEnd) {
			t.Fatalf("correction must carry the metric window: %+v", corrected)
		}
		stored, ok := log.Get(original.Digest)
		if !ok {
			t.Fatal("original must remain retrievable after correction")
		}
		if stored.Value.String() != "0.0364" || stored.Revision != 1 || stored.Supersedes != "" {
			t.Fatalf("original must be preserved unmodified: %+v", stored)
		}
	})

	t.Run("every dependent goes stale with a supersession chain", func(t *testing.T) {
		log := NewCorrectionLog()
		original := publishMetricOriginal(t, log)
		linkMetricDependents(t, log, original.Digest)
		for _, id := range []string{"report:attrition-june", "dashboard:workforce", "decision:retention-plan"} {
			if got := log.DependentStatus(id); got != DependentFresh {
				t.Fatalf("dependent %s must start FRESH, got %s", id, got)
			}
		}
		corrected, err := log.AppendCorrection(correctionRequest(original, "corr-1", "0.0371", t))
		if err != nil {
			t.Fatalf("AppendCorrection: %v", err)
		}
		_ = corrected
		for _, id := range []string{"report:attrition-june", "dashboard:workforce", "decision:retention-plan"} {
			if got := log.DependentStatus(id); got != DependentStale {
				t.Fatalf("dependent %s must be STALE after correction, got %s", id, got)
			}
		}
		chain := log.SupersessionChain(corrected.Digest)
		if len(chain) != 2 || chain[0] != original.Digest || chain[1] != corrected.Digest {
			t.Fatalf("supersession chain must link original to correction, got %v", chain)
		}
	})

	t.Run("replay is idempotent", func(t *testing.T) {
		log := NewCorrectionLog()
		original := publishMetricOriginal(t, log)
		linkMetricDependents(t, log, original.Digest)
		first, err := log.AppendCorrection(correctionRequest(original, "corr-1", "0.0371", t))
		if err != nil {
			t.Fatalf("AppendCorrection: %v", err)
		}
		second, err := log.AppendCorrection(correctionRequest(original, "corr-1", "0.0371", t))
		if err != nil {
			t.Fatalf("correction replay must be accepted: %v", err)
		}
		if second.Digest != first.Digest || second.Revision != first.Revision {
			t.Fatalf("replay must return the same revision: %+v vs %+v", first, second)
		}
	})

	t.Run("ungoverned corrections are refused", func(t *testing.T) {
		log := NewCorrectionLog()
		original := publishMetricOriginal(t, log)
		req := correctionRequest(original, "corr-bad", "0.0371", t)
		req.Authority = ""
		if _, err := log.AppendCorrection(req); !errors.Is(err, ErrMetricCorrection) {
			t.Fatalf("correction without authority must be refused, got %v", err)
		}
		req = correctionRequest(original, "corr-bad", "0.0371", t)
		req.Reason = ""
		if _, err := log.AppendCorrection(req); !errors.Is(err, ErrMetricCorrection) {
			t.Fatal("correction without reason must be refused")
		}
		req = correctionRequest(original, "corr-bad", "0.0371", t)
		req.OriginalDigest = "sha256:unknown"
		if _, err := log.AppendCorrection(req); !errors.Is(err, ErrMetricCorrection) {
			t.Fatal("correction of an unknown original must be refused")
		}
		if got := log.DependentStatus("report:attrition-june"); got != DependentUnknown {
			t.Fatalf("unlinked dependent must read UNKNOWN, got %s", got)
		}
	})

	t.Run("superseded values cannot be corrected again", func(t *testing.T) {
		log := NewCorrectionLog()
		original := publishMetricOriginal(t, log)
		first, err := log.AppendCorrection(correctionRequest(original, "corr-1", "0.0371", t))
		if err != nil {
			t.Fatalf("AppendCorrection: %v", err)
		}
		_ = first
		stale := correctionRequest(original, "corr-2", "0.0380", t)
		if _, err := log.AppendCorrection(stale); !errors.Is(err, ErrMetricCorrection) {
			t.Fatal("correcting a superseded value must be refused: history appends forward only")
		}
	})
}

// TestTodo_METRIC_002_Golden pins the canonical correction digest so any
// drift in the correction envelope is caught byte-for-byte.
func TestTodo_METRIC_002_Golden(t *testing.T) {
	build := func(t *testing.T) MetricValue {
		log := NewCorrectionLog()
		original := publishMetricOriginal(t, log)
		corrected, err := log.AppendCorrection(correctionRequest(original, "corr-1", "0.0371", t))
		if err != nil {
			t.Fatalf("AppendCorrection: %v", err)
		}
		return corrected
	}
	first := build(t)
	second := build(t)
	if first.Digest == "" || first.Digest != second.Digest {
		t.Fatalf("correction digest must be stable, got %q vs %q", first.Digest, second.Digest)
	}
	const golden = "sha256:b0ff3358b01e5a3608093df1765fd96dbbae5cab473d3fb471a3c455f2434843"
	if first.Digest != golden {
		t.Fatalf("golden correction digest moved: got %q want %q", first.Digest, golden)
	}
}

// TestTodo_METRIC_002_Mutation kills the seeded semantic mutants that would
// silently rewrite history or leave decisions fresh on invalid input.
func TestTodo_METRIC_002_Mutation(t *testing.T) {
	t.Run("mutant: correction overwrites the original", func(t *testing.T) {
		log := NewCorrectionLog()
		original := publishMetricOriginal(t, log)
		if _, err := log.AppendCorrection(correctionRequest(original, "corr-1", "0.0371", t)); err != nil {
			t.Fatalf("AppendCorrection: %v", err)
		}
		stored, _ := log.Get(original.Digest)
		if stored.Value.String() != "0.0364" {
			t.Fatal("mutant survived: correction overwrote the original value")
		}
	})

	t.Run("mutant: dependents stay fresh on invalid input", func(t *testing.T) {
		log := NewCorrectionLog()
		original := publishMetricOriginal(t, log)
		linkMetricDependents(t, log, original.Digest)
		if _, err := log.AppendCorrection(correctionRequest(original, "corr-1", "0.0371", t)); err != nil {
			t.Fatalf("AppendCorrection: %v", err)
		}
		for _, id := range []string{"report:attrition-june", "dashboard:workforce", "decision:retention-plan"} {
			if log.DependentStatus(id) != DependentStale {
				t.Fatalf("mutant survived: dependent %s stayed fresh on corrected input", id)
			}
		}
	})

	t.Run("mutant: supersession link dropped", func(t *testing.T) {
		log := NewCorrectionLog()
		original := publishMetricOriginal(t, log)
		corrected, err := log.AppendCorrection(correctionRequest(original, "corr-1", "0.0371", t))
		if err != nil {
			t.Fatalf("AppendCorrection: %v", err)
		}
		if corrected.Supersedes == "" || corrected.Supersedes != original.Digest {
			t.Fatal("mutant survived: correction dropped the supersession link")
		}
		if len(log.SupersessionChain(corrected.Digest)) != 2 {
			t.Fatal("mutant survived: supersession chain is broken")
		}
	})
}
