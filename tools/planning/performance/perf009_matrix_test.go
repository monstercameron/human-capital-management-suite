package performance

import (
	"encoding/json"
	"errors"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// liveUnits is the test-side statement of the live-projection formula:
// daily ingest accumulates up to the live cap (retention, or the archive
// point when archiving), while held stock and daily holds persist the
// full horizon and are never disposed.
func liveUnits(dim GrowthDimension, horizon int64) int64 {
	liveCap := dim.RetentionDays
	if dim.ArchiveAfterDays > 0 && dim.ArchiveAfterDays < liveCap {
		liveCap = dim.ArchiveAfterDays
	}
	accumulate := horizon
	if accumulate > liveCap {
		accumulate = liveCap
	}
	return dim.DailyUnits*accumulate + dim.HeldStockUnits + dim.HeldDailyUnits*horizon
}

// archivedUnits is the test-side statement of the archive-flow formula:
// daily ingest older than the archive point but younger than retention
// sits in the archive pool.
func archivedUnits(dim GrowthDimension, horizon int64) int64 {
	if dim.ArchiveAfterDays <= 0 {
		return 0
	}
	span := horizon
	if span > dim.RetentionDays {
		span = dim.RetentionDays
	}
	span -= dim.ArchiveAfterDays
	if span < 0 {
		span = 0
	}
	return dim.DailyUnits * span
}

// disposedUnits is the test-side statement of disposition: daily ingest
// older than retention is gone, held bytes never are.
func disposedUnits(dim GrowthDimension, horizon int64) int64 {
	past := horizon - dim.RetentionDays
	if past < 0 {
		past = 0
	}
	return dim.DailyUnits * past
}

func assertNoTenantIdentity(t *testing.T, model RetentionModel, report RetentionReport) {
	t.Helper()
	for label, value := range map[string]any{"model": model, "report": report} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("encode %s: %v", label, err)
		}
		if strings.Contains(strings.ToLower(string(data)), "tenant") {
			t.Fatalf("%s carries tenant identity: %s", label, data)
		}
	}
}

// TestTodo_PERF_009_Property proves horizon and hold monotonicity plus
// determinism: longer horizons never project less, more held data never
// projects less, and identical models digest identically.
func TestTodo_PERF_009_Property(t *testing.T) {
	model := healthyRetentionModel()
	report, err := EvaluateRetention(model)
	if err != nil {
		t.Fatalf("healthy retention model rejected: %v", err)
	}
	projectionOf := func(name string) DimensionProjection {
		for _, projection := range report.Dimensions {
			if projection.Name == name {
				return projection
			}
		}
		t.Fatalf("report omits dimension %s", name)
		return DimensionProjection{}
	}
	for _, dim := range model.Dimensions {
		projection := projectionOf(dim.Name)
		var last int64 = -1
		for _, horizon := range projection.Horizons {
			if horizon.LiveUnits < last {
				t.Fatalf("dimension %s shrinks from %d to %d at %d days", dim.Name, last, horizon.LiveUnits, horizon.HorizonDays)
			}
			last = horizon.LiveUnits
			if want := liveUnits(dim, horizon.HorizonDays); horizon.LiveUnits != want {
				t.Fatalf("dimension %s at %d days projects %d, want %d", dim.Name, horizon.HorizonDays, horizon.LiveUnits, want)
			}
		}
	}

	held := cloneRetentionModel(model)
	for i := range held.Dimensions {
		// Holds attach to retained data, not to the vacuum
		// maintenance load, which holds can never join.
		if held.Dimensions[i].Name == DimVacuumSeconds {
			continue
		}
		held.Dimensions[i].HeldStockUnits += 1000
		held.Dimensions[i].HeldDailyUnits += 7
	}
	heldReport, err := EvaluateRetention(held)
	if err != nil {
		t.Fatalf("held model rejected: %v", err)
	}
	for _, projection := range heldReport.Dimensions {
		base := projectionOf(projection.Name)
		for i, horizon := range projection.Horizons {
			if horizon.LiveUnits < base.Horizons[i].LiveUnits {
				t.Fatalf("more held data projected less on %s at %d days", projection.Name, horizon.HorizonDays)
			}
		}
	}

	again, err := EvaluateRetention(model)
	if err != nil {
		t.Fatalf("second evaluation rejected: %v", err)
	}
	if !reflect.DeepEqual(report, again) {
		t.Fatal("identical models evaluated to different reports")
	}
}

// TestTodo_PERF_009_Golden pins the healthy-report digest: any silent
// change to the projection math moves the digest and fails here.
func TestTodo_PERF_009_Golden(t *testing.T) {
	report, err := EvaluateRetention(healthyRetentionModel())
	if err != nil {
		t.Fatalf("healthy retention model rejected: %v", err)
	}
	const wantDigest = "sha256:9c26149afd78a89690713b3bc8973fcf690312fccadbd50e078acaaf15d399b4"
	if report.Digest != wantDigest {
		t.Fatalf("golden digest = %s, want %s", report.Digest, wantDigest)
	}
}

// retentionModelFromEnvelope derives every PERF-009 dimension from a real
// workload envelope, a caller-supplied retention table and PERF-007 cost
// rates: commands per minute drive rows and payload bytes, fanout scales
// derived effects, and the stored-GB-month rate prices lifecycle cost. No
// tenant-specific constant enters the model.
func retentionModelFromEnvelope(id string, envelope WorkloadEnvelope, retention map[string]int64, rates RetentionCostRates, budgetCents int64) RetentionModel {
	rowsPerDay := envelope.CommandsPerMinute.Value * 1440
	payloadPerDay := rowsPerDay * envelope.PayloadBytes.Value
	objectPerDay := rowsPerDay * envelope.ObjectBytes.Value / 100
	// Capacities carry 2x headroom over the 365-day steady-state
	// projection so the registry-derived model is the healthy case;
	// the breach direction is pinned by the primary and mutation tests.
	headroom := func(units int64) int64 { return units*2 + 1 }
	partitionAfter, indexAfter, objectAfter := int64(30), int64(30), int64(90)
	archive365 := payloadPerDay*(retention[DimPartitionBytes]-partitionAfter) +
		payloadPerDay/10*(retention[DimIndexBytes]-indexAfter) +
		objectPerDay*(retention[DimObjectBytes]-objectAfter)
	dim := func(name string, daily int64, unit string, capacity int64, archiveAfter int64) GrowthDimension {
		return GrowthDimension{
			Name: name, DailyUnits: daily, Unit: unit, RetentionDays: retention[name],
			Capacity: Limit{Value: capacity, Unit: unit}, ArchiveAfterDays: archiveAfter,
		}
	}
	return RetentionModel{
		SchemaVersion: SchemaVersion, ID: id, EnvelopeID: envelope.ID,
		Dimensions: []GrowthDimension{
			dim(DimPartitionBytes, payloadPerDay, "bytes", headroom(payloadPerDay*partitionAfter), partitionAfter),
			dim(DimWALBytes, payloadPerDay/2, "bytes", headroom(payloadPerDay/2*retention[DimWALBytes]), 0),
			dim(DimIndexBytes, payloadPerDay/10, "bytes", headroom(payloadPerDay/10*indexAfter), indexAfter),
			dim(DimObjectBytes, objectPerDay, "bytes", headroom(objectPerDay*objectAfter), objectAfter),
			dim(DimTelemetryBytes, payloadPerDay, "bytes", headroom(payloadPerDay*retention[DimTelemetryBytes]), 0),
			dim(DimVacuumSeconds, 600, "seconds", 3600, 0),
			{Name: DimLegalHoldBytes, DailyUnits: 0, Unit: "bytes", RetentionDays: retention[DimLegalHoldBytes], Capacity: Limit{Value: headroom(payloadPerDay), Unit: "bytes"}},
		},
		Archive: ArchiveModel{
			CapacityBytes:     Limit{Value: headroom(archive365), Unit: "bytes"},
			RestoreDailyBytes: archive365/45 + 1,
			RestoreWindowDays: 90,
		},
		Rates:       rates,
		BudgetCents: budgetCents,
	}
}

// TestTodo_PERF_009_Integration builds the model from the real workload,
// retention and cost registries and proves cost follows the supplied
// PERF-007 rate.
func TestTodo_PERF_009_Integration(t *testing.T) {
	var envelope WorkloadEnvelope
	for _, candidate := range EnvelopeFixtures() {
		if candidate.Tier == LargeTier {
			envelope = candidate
		}
	}
	if envelope.ID == "" {
		t.Fatal("large placeholder envelope missing")
	}
	retention := map[string]int64{
		DimPartitionBytes: 90, DimWALBytes: 7, DimIndexBytes: 90, DimObjectBytes: 365,
		DimTelemetryBytes: 14, DimVacuumSeconds: 1, DimLegalHoldBytes: 365,
	}
	rates := RetentionCostRates{CentsPerLiveGBMonth: 120, CentsPerArchiveGBMonth: 30}
	model := retentionModelFromEnvelope("PLACEHOLDER_RETENTION_INTEGRATION", envelope, retention, rates, math.MaxInt64)
	report, err := EvaluateRetention(model)
	if err != nil {
		t.Fatalf("registry-derived model rejected: %v", err)
	}
	if len(report.Dimensions) != len(RequiredGrowthDimensions) {
		t.Fatalf("report has %d dimensions", len(report.Dimensions))
	}
	if report.Cost.TotalCents != report.Cost.LiveCents+report.Cost.ArchiveCents {
		t.Fatalf("cost does not add up: %+v", report.Cost)
	}

	halved := retentionModelFromEnvelope("PLACEHOLDER_RETENTION_HALVED", envelope, retention,
		RetentionCostRates{CentsPerLiveGBMonth: 60, CentsPerArchiveGBMonth: 15}, math.MaxInt64)
	halvedReport, err := EvaluateRetention(halved)
	if err != nil {
		t.Fatalf("halved-rate model rejected: %v", err)
	}
	if halvedReport.Cost.LiveCents*2 < report.Cost.LiveCents-1 || halvedReport.Cost.LiveCents*2 > report.Cost.LiveCents+1 {
		t.Fatalf("live cost %d does not follow the supplied rate from %d", halvedReport.Cost.LiveCents, report.Cost.LiveCents)
	}
}

// TestTodo_PERF_009_ModelBased drives 50 seeded random models and proves
// the verdict always matches an independent recheck of the projection
// math, with deterministic digests.
func TestTodo_PERF_009_ModelBased(t *testing.T) {
	rng := rand.New(rand.NewSource(0x009009))
	retentions := []int64{1, 7, 14, 30, 90, 365}
	for i := 0; i < 50; i++ {
		model := healthyRetentionModel()
		model.ID = "PLACEHOLDER_RETENTION_MODELBASED"
		for j := range model.Dimensions {
			dim := &model.Dimensions[j]
			dim.DailyUnits = rng.Int63n(50*gb + 1)
			dim.RetentionDays = retentions[rng.Intn(len(retentions))]
			dim.HeldStockUnits = rng.Int63n(10*gb + 1)
			dim.HeldDailyUnits = rng.Int63n(gb + 1)
			// Capacity lands on either side of the projection so both
			// verdicts are exercised.
			projection := liveUnits(*dim, Horizon365Days)
			if rng.Intn(2) == 0 {
				dim.Capacity.Value = projection + rng.Int63n(gb+1) + 1
			} else if projection > 0 {
				dim.Capacity.Value = rng.Int63n(projection)
				if dim.Capacity.Value == 0 {
					dim.Capacity.Value = 1
				}
			} else {
				dim.Capacity.Value = 1
			}
			dim.Capacity.Unit = dim.Unit
			if dim.ArchiveAfterDays > dim.RetentionDays {
				dim.ArchiveAfterDays = 0
			}
		}
		report, err := EvaluateRetention(model)
		again, errAgain := EvaluateRetention(model)
		if (err == nil) != (errAgain == nil) {
			t.Fatalf("model %d verdict unstable: %v vs %v", i, err, errAgain)
		}
		if err == nil && report.Digest != again.Digest {
			t.Fatalf("model %d digest unstable", i)
		}
		if err == nil {
			for _, dim := range model.Dimensions {
				for _, horizon := range RetentionHorizons {
					if liveUnits(dim, horizon) > dim.Capacity.Value {
						t.Fatalf("model %d passed with %s over capacity at %d days", i, dim.Name, horizon)
					}
				}
			}
		} else {
			var rejection *RetentionRejection
			if !errors.As(err, &rejection) || rejection.Code != RetentionRejectedCode {
				t.Fatalf("model %d error is not a typed rejection: %v", i, err)
			}
		}
	}
}

// TestTodo_PERF_009_Mutation flips exactly one field at a time off a
// known-healthy model and proves ValidateRetention catches that one
// violation independently.
func TestTodo_PERF_009_Mutation(t *testing.T) {
	baseline := healthyRetentionModel()
	if err := CheckRetention(baseline); err != nil {
		t.Fatalf("baseline retention model should be healthy: %v", err)
	}
	partition := func(m *RetentionModel) *GrowthDimension {
		for i := range m.Dimensions {
			if m.Dimensions[i].Name == DimPartitionBytes {
				return &m.Dimensions[i]
			}
		}
		return nil
	}
	cases := []struct {
		name    string
		mutate  func(*RetentionModel)
		field   string
		state   string
		version int
	}{
		{"schema_version", func(m *RetentionModel) { m.SchemaVersion = 0 }, "schema_version", StateUnsupportedVersion, 0},
		{"id", func(m *RetentionModel) { m.ID = "" }, "id", StateMissing, SchemaVersion},
		{"envelope_id", func(m *RetentionModel) { m.EnvelopeID = "" }, "envelope_id", StateMissing, SchemaVersion},
		{"partition_missing_capacity", func(m *RetentionModel) { partition(m).Capacity = Limit{} }, "dimensions.partition_bytes.capacity", StateMissing, SchemaVersion},
		{"partition_daily_breach", func(m *RetentionModel) { partition(m).DailyUnits = partition(m).Capacity.Value + 1 }, "dimensions.partition_bytes.horizons.30", StateBreach, SchemaVersion},
		{"wal_missing_unit", func(m *RetentionModel) {
			for i := range m.Dimensions {
				if m.Dimensions[i].Name == DimWALBytes {
					m.Dimensions[i].Unit = ""
				}
			}
		}, "dimensions.wal_bytes.unit", StateMissing, SchemaVersion},
		{"telemetry_breach", func(m *RetentionModel) {
			for i := range m.Dimensions {
				if m.Dimensions[i].Name == DimTelemetryBytes {
					m.Dimensions[i].DailyUnits = m.Dimensions[i].Capacity.Value
				}
			}
		}, "dimensions.telemetry_bytes.horizons.30", StateBreach, SchemaVersion},
		{"vacuum_missing_retention", func(m *RetentionModel) {
			for i := range m.Dimensions {
				if m.Dimensions[i].Name == DimVacuumSeconds {
					m.Dimensions[i].RetentionDays = 0
				}
			}
		}, "dimensions.vacuum_seconds.retention_days", StateMissing, SchemaVersion},
		{"legal_hold_surge", func(m *RetentionModel) {
			for i := range m.Dimensions {
				if m.Dimensions[i].Name == DimLegalHoldBytes {
					m.Dimensions[i].HeldDailyUnits = m.Dimensions[i].Capacity.Value
				}
			}
		}, "dimensions.legal_hold_bytes.horizons.30", StateBreach, SchemaVersion},
		{"archive_capacity_missing", func(m *RetentionModel) { m.Archive.CapacityBytes = Limit{} }, "archive.capacity_bytes", StateMissing, SchemaVersion},
		{"archive_exhausted", func(m *RetentionModel) { m.Archive.CapacityBytes.Value = 1 }, "archive.horizons.90", StateBreach, SchemaVersion},
		{"archive_rate_missing", func(m *RetentionModel) { m.Archive.RestoreDailyBytes = -1 }, "archive.restore_daily_bytes", StateMissing, SchemaVersion},
		{"archive_window_missing", func(m *RetentionModel) { m.Archive.RestoreWindowDays = -1 }, "archive.restore_window_days", StateMissing, SchemaVersion},
		{"rate_missing", func(m *RetentionModel) { m.Rates.CentsPerArchiveGBMonth = -1 }, "rates.cents_per_archive_gb_month", StateMissing, SchemaVersion},
		{"budget_negative", func(m *RetentionModel) { m.BudgetCents = -1 }, "budget_cents", StateMissing, SchemaVersion},
		{"cost_breach", func(m *RetentionModel) { m.BudgetCents = 0 }, "cost.total_cents", StateBreach, SchemaVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := cloneRetentionModel(baseline)
			tc.mutate(&model)
			violations := ValidateRetention(model)
			if len(violations) == 0 {
				t.Fatalf("%s passed validation", tc.name)
			}
			first := violations[0]
			if first.Code != RetentionRejectedCode || first.Field != tc.field || first.State != tc.state || first.ModelVersion != tc.version {
				t.Fatalf("%s first violation = %+v, want field=%s state=%s version=%d", tc.name, first, tc.field, tc.state, tc.version)
			}
			if _, err := EvaluateRetention(model); err == nil {
				t.Fatalf("%s evaluated without error", tc.name)
			}
		})
	}
}

// BenchmarkTodo_PERF_009 evaluates the healthy 30/90/365-day model and
// reports lifecycle cost alongside horizon coverage.
func BenchmarkTodo_PERF_009(b *testing.B) {
	model := healthyRetentionModel()
	var report RetentionReport
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		report, err = EvaluateRetention(model)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if len(report.Dimensions) != len(RequiredGrowthDimensions) {
		b.Fatal("evaluation covered no dimensions")
	}
	b.ReportMetric(float64(report.Cost.TotalCents), "total-cents")
	b.ReportMetric(float64(len(report.Dimensions)*len(RetentionHorizons)), "horizons")
}
