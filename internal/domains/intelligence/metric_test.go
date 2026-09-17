package intelligence

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func metricDecimal(t *testing.T, s string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(s, 4, values.RoundingHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func metricDefinition() MetricDefinition {
	return MetricDefinition{
		ID: "metric:attrition-rate", Version: "v2",
		Kind: MetricRatio, NullPolicy: NullSkip,
		Rounding: values.RoundingHalfUp, Scale: 4,
		Unit:          "per worker",
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EffectiveTo:   time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func metricRows(t *testing.T) []MetricRow {
	t.Helper()
	return []MetricRow{
		{Included: true, Numerator: metricDecimal(t, "3"), Denominator: metricDecimal(t, "100"), NumeratorSet: true, DenominatorSet: true, Watermark: "wm:hr-2026-06"},
		{Included: true, Numerator: metricDecimal(t, "5"), Denominator: metricDecimal(t, "120"), NumeratorSet: true, DenominatorSet: true, Watermark: "wm:hr-2026-06"},
		{Included: false, Watermark: "wm:hr-2026-06"},
		{Included: true, Watermark: "wm:hr-2026-06"},
	}
}

// TestTodo_METRIC_001 is the primary METRIC-001 contract test: denominator,
// population, time, null, zero and rounding policies are explicit, and
// missing data is UNKNOWN — never silent zero.
func TestTodo_METRIC_001(t *testing.T) {
	t.Run("ratio returns typed components with watermarks", func(t *testing.T) {
		compiled, err := CompileMetric(metricDefinition())
		if err != nil {
			t.Fatalf("CompileMetric: %v", err)
		}
		got, err := compiled.Execute(metricRows(t))
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		// 8 leavers over 220 workers, one null skipped.
		if got.Numerator.String() != "8.0000" || got.Denominator.String() != "220.0000" {
			t.Fatalf("components = %s/%s, want 8.0000/220.0000", got.Numerator, got.Denominator)
		}
		if got.Value.String() != "0.0364" {
			t.Fatalf("value = %s, want 0.0364", got.Value)
		}
		if got.SampleSize != 2 || got.NullCount != 1 {
			t.Fatalf("sample = %d nulls = %d, want 2 and 1", got.SampleSize, got.NullCount)
		}
		if got.Quality != QualityPartial {
			t.Fatalf("quality = %v, want PARTIAL", got.Quality)
		}
		if len(got.SourceWatermarks) != 1 || got.SourceWatermarks[0] != "wm:hr-2026-06" {
			t.Fatalf("watermarks must be reported: %+v", got)
		}
		if got.PopulationDigest == "" || got.CalculationDigest == "" {
			t.Fatalf("result must carry both digests: %+v", got)
		}
	})

	t.Run("missing data is unknown, never zero", func(t *testing.T) {
		compiled, err := CompileMetric(metricDefinition())
		if err != nil {
			t.Fatal(err)
		}
		got, err := compiled.Execute(nil)
		if err != nil {
			t.Fatalf("empty population must return UNKNOWN, not an error: %v", err)
		}
		if got.Quality != QualityUnknown {
			t.Fatalf("quality = %v, want UNKNOWN", got.Quality)
		}
		if !got.Value.IsZero() {
			t.Fatalf("unknown value must be zero with UNKNOWN quality, got %s", got.Value)
		}
		allNull := []MetricRow{{Included: true, Watermark: "w"}, {Included: true, Watermark: "w"}}
		got, err = compiled.Execute(allNull)
		if err != nil {
			t.Fatal(err)
		}
		if got.Quality != QualityUnknown {
			t.Fatalf("all-null population must be UNKNOWN, got %v", got.Quality)
		}
	})

	t.Run("zero denominator is unknown, not a panic", func(t *testing.T) {
		def := metricDefinition()
		def.NullPolicy = NullUnknownIfAny
		compiled, err := CompileMetric(def)
		if err != nil {
			t.Fatal(err)
		}
		zero := []MetricRow{{
			Included: true, Numerator: metricDecimal(t, "1"), Denominator: metricDecimal(t, "0"),
			NumeratorSet: true, DenominatorSet: true, Watermark: "w",
		}}
		got, err := compiled.Execute(zero)
		if err != nil {
			t.Fatalf("zero denominator must return UNKNOWN, not an error: %v", err)
		}
		if got.Quality != QualityUnknown {
			t.Fatalf("quality = %v, want UNKNOWN", got.Quality)
		}
	})

	t.Run("implicit policies are refused at compile time", func(t *testing.T) {
		for name, mutate := range map[string]func(*MetricDefinition){
			"identity": func(d *MetricDefinition) { d.ID = "" },
			"version":  func(d *MetricDefinition) { d.Version = "" },
			"kind":     func(d *MetricDefinition) { d.Kind = "" },
			"null":     func(d *MetricDefinition) { d.NullPolicy = "" },
			"rounding": func(d *MetricDefinition) { d.Rounding = values.RoundingUnspecified },
			"window":   func(d *MetricDefinition) { d.EffectiveTo = d.EffectiveFrom },
		} {
			def := metricDefinition()
			mutate(&def)
			if _, err := CompileMetric(def); err == nil {
				t.Fatalf("%s: implicit policy must be refused", name)
			}
		}
	})
}

// TestTodo_METRIC_001_Property holds the metric algebra: sample accounting
// closes, PARTIAL implies skipped nulls, and execution is deterministic.
func TestTodo_METRIC_001_Property(t *testing.T) {
	compiled, err := CompileMetric(metricDefinition())
	if err != nil {
		t.Fatal(err)
	}
	rows := metricRows(t)
	first, err := compiled.Execute(rows)
	if err != nil {
		t.Fatal(err)
	}
	second, err := compiled.Execute(rows)
	if err != nil {
		t.Fatal(err)
	}
	if first.CalculationDigest != second.CalculationDigest {
		t.Fatal("same input must calculate identically")
	}
	presented := 0
	for _, row := range rows {
		if row.Included {
			presented++
		}
	}
	if first.SampleSize+first.NullCount != presented {
		t.Fatalf("sample %d + nulls %d != presented %d", first.SampleSize, first.NullCount, presented)
	}
	if first.Quality == QualityPartial && first.NullCount == 0 {
		t.Fatal("PARTIAL without skipped nulls is dishonest")
	}
	if first.Quality == QualityOK && first.NullCount != 0 {
		t.Fatal("OK with skipped nulls hides data quality")
	}
	var _ = errors.Is
}

// TestTodo_METRIC_001_Golden pins the canonical calculation digest.
func TestTodo_METRIC_001_Golden(t *testing.T) {
	compiled, err := CompileMetric(metricDefinition())
	if err != nil {
		t.Fatal(err)
	}
	got, err := compiled.Execute(metricRows(t))
	if err != nil {
		t.Fatal(err)
	}
	again, err := compiled.Execute(metricRows(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.CalculationDigest != again.CalculationDigest || got.CalculationDigest == "" {
		t.Fatalf("calculation digest must be stable: %q", got.CalculationDigest)
	}
	const golden = "sha256:5a5d292ca4a89e09941f5609c5d7ef3a6f2633e1776433d0210d16a7c08ec383"
	if got.CalculationDigest != golden {
		t.Fatalf("golden calculation digest moved: got %q want %q", got.CalculationDigest, golden)
	}
}
