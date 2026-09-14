package performance

import (
	"errors"
	"reflect"
	"testing"
)

const gb = int64(1) << 30

// healthyRetentionModel is the known-good PERF-009 model every test starts
// from: every dimension clears its 365-day projection with headroom, the
// archive pool holds, restore fits its window and cost clears budget with
// no alarms.
func healthyRetentionModel() RetentionModel {
	dim := func(name string, daily int64, unit string, retention int64, capacity int64, archiveAfter int64) GrowthDimension {
		return GrowthDimension{
			Name: name, DailyUnits: daily, Unit: unit, RetentionDays: retention,
			Capacity: Limit{Value: capacity, Unit: unit}, ArchiveAfterDays: archiveAfter,
		}
	}
	return RetentionModel{
		SchemaVersion: SchemaVersion,
		ID:            "PLACEHOLDER_RETENTION_LARGE",
		EnvelopeID:    "PLACEHOLDER_LARGE_ENVELOPE",
		Dimensions: []GrowthDimension{
			dim(DimPartitionBytes, 10*gb, "bytes", 90, 2*gb*1024, 30),
			dim(DimWALBytes, 50*gb, "bytes", 7, 1024*gb, 0),
			dim(DimIndexBytes, 5*gb, "bytes", 90, 1024*gb, 30),
			dim(DimObjectBytes, 20*gb, "bytes", 365, 10*1024*gb, 90),
			dim(DimTelemetryBytes, 100*gb, "bytes", 14, 2*1024*gb, 0),
			dim(DimVacuumSeconds, 600, "seconds", 1, 3600, 0),
			{
				Name: DimLegalHoldBytes, DailyUnits: gb, Unit: "bytes", RetentionDays: 365,
				HeldStockUnits: 50 * gb, HeldDailyUnits: 0,
				Capacity: Limit{Value: 1024 * gb, Unit: "bytes"},
			},
		},
		Archive: ArchiveModel{
			CapacityBytes:     Limit{Value: 10 * 1024 * gb, Unit: "bytes"},
			RestoreDailyBytes: 100 * gb,
			RestoreWindowDays: 90,
		},
		Rates:       RetentionCostRates{CentsPerLiveGBMonth: 200, CentsPerArchiveGBMonth: 50},
		BudgetCents: 50_000_000,
	}
}

func cloneRetentionModel(model RetentionModel) RetentionModel {
	out := model
	out.Dimensions = append([]GrowthDimension(nil), model.Dimensions...)
	return out
}

// TestLongHorizonRetentionStorageGrowthAndLifecycleStayWithinBudget is the
// PERF-009 primary: a healthy 30/90/365-day model evaluates with headroom,
// a held-data surge is never disposed away, and every seeded breach —
// headroom, archive, cost — fails closed with a typed rejection.
func TestLongHorizonRetentionStorageGrowthAndLifecycleStayWithinBudget(t *testing.T) {
	healthy := healthyRetentionModel()
	before := cloneRetentionModel(healthy)
	report, err := EvaluateRetention(healthy)
	if err != nil {
		t.Fatalf("healthy retention model rejected: %v", err)
	}
	if len(report.Dimensions) != len(RequiredGrowthDimensions) {
		t.Fatalf("report has %d dimensions, want %d", len(report.Dimensions), len(RequiredGrowthDimensions))
	}
	for _, projection := range report.Dimensions {
		if len(projection.Horizons) != len(RetentionHorizons) {
			t.Fatalf("dimension %s has %d horizons, want %d", projection.Name, len(projection.Horizons), len(RetentionHorizons))
		}
		for _, horizon := range projection.Horizons {
			if !horizon.WithinCapacity || horizon.HeadroomRatio <= 0 {
				t.Fatalf("healthy dimension %s at %d days has no headroom: %+v", projection.Name, horizon.HorizonDays, horizon)
			}
		}
	}
	if len(report.Archive) != len(RetentionHorizons) {
		t.Fatalf("report has %d archive horizons, want %d", len(report.Archive), len(RetentionHorizons))
	}
	if !report.Cost.WithinBudget || report.Cost.TotalCents <= 0 {
		t.Fatalf("healthy cost not within budget: %+v", report.Cost)
	}
	if len(report.Alarms) != 0 {
		t.Fatalf("healthy model raised alarms: %+v", report.Alarms)
	}
	if report.Digest == "" {
		t.Fatal("healthy report has no digest")
	}
	if !reflect.DeepEqual(before, healthy) {
		t.Fatal("EvaluateRetention mutated its input model")
	}

	// Seeded defect 1: 365-day partition growth exceeds headroom.
	breach := cloneRetentionModel(healthy)
	for i := range breach.Dimensions {
		if breach.Dimensions[i].Name == DimPartitionBytes {
			breach.Dimensions[i].Capacity.Value = 100 * gb
		}
	}
	beforeBreach := cloneRetentionModel(breach)
	_, err = EvaluateRetention(breach)
	var rejection *RetentionRejection
	if !errors.As(err, &rejection) {
		t.Fatalf("headroom breach did not return a typed rejection: %v", err)
	}
	if rejection.Code != RetentionRejectedCode || rejection.ModelVersion != SchemaVersion {
		t.Fatalf("breach rejection = %+v", rejection)
	}
	if rejection.Field != "dimensions.partition_bytes.horizons.30" || rejection.State != StateBreach {
		t.Fatalf("breach rejection = %+v", rejection)
	}
	if !errors.Is(err, ErrRetentionRejected) {
		t.Fatalf("breach error does not unwrap to ErrRetentionRejected: %v", err)
	}
	if !reflect.DeepEqual(beforeBreach, breach) {
		t.Fatal("EvaluateRetention mutated the breached model")
	}

	// Seeded defect 2: held-data surge is carried, never disposed away,
	// and flips the verdict to breach on the legal-hold dimension.
	held := cloneRetentionModel(healthy)
	for i := range held.Dimensions {
		if held.Dimensions[i].Name == DimLegalHoldBytes {
			held.Dimensions[i].HeldStockUnits = held.Dimensions[i].Capacity.Value + 1
		}
	}
	_, err = EvaluateRetention(held)
	rejection = nil
	if !errors.As(err, &rejection) {
		t.Fatalf("held-data surge did not return a typed rejection: %v", err)
	}
	if rejection.Field != "dimensions.legal_hold_bytes.horizons.30" || rejection.State != StateBreach {
		t.Fatalf("held-data rejection = %+v", rejection)
	}

	// Seeded defect 3: archive pool exhaustion at the 90-day horizon.
	full := cloneRetentionModel(healthy)
	full.Archive.CapacityBytes.Value = 100 * gb
	_, err = EvaluateRetention(full)
	rejection = nil
	if !errors.As(err, &rejection) {
		t.Fatalf("archive exhaustion did not return a typed rejection: %v", err)
	}
	if rejection.Field != "archive.horizons.90" || rejection.State != StateBreach {
		t.Fatalf("archive rejection = %+v", rejection)
	}

	// Seeded defect 4: lifecycle cost over budget rejects instead of
	// reporting pass.
	expensive := cloneRetentionModel(healthy)
	expensive.BudgetCents = 1
	_, err = EvaluateRetention(expensive)
	rejection = nil
	if !errors.As(err, &rejection) {
		t.Fatalf("cost overrun did not return a typed rejection: %v", err)
	}
	if rejection.Field != "cost.total_cents" || rejection.State != StateBreach {
		t.Fatalf("cost rejection = %+v", rejection)
	}
}

// TestTodo_PERF_009_Fault proves malformed models fail closed with typed
// rejections and rejected evaluations change nothing.
func TestTodo_PERF_009_Fault(t *testing.T) {
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
		{"missing_dimension", func(m *RetentionModel) { m.Dimensions = m.Dimensions[:len(m.Dimensions)-1] }, "dimensions.legal_hold_bytes", StateMissing, SchemaVersion},
		{"unknown_dimension", func(m *RetentionModel) {
			m.Dimensions = append(m.Dimensions, GrowthDimension{Name: "scratch_bytes", DailyUnits: 1, Unit: "bytes", RetentionDays: 1, Capacity: Limit{Value: 10, Unit: "bytes"}})
		}, "dimensions.scratch_bytes", StateMissing, SchemaVersion},
		{"negative_daily", func(m *RetentionModel) {
			for i := range m.Dimensions {
				if m.Dimensions[i].Name == DimWALBytes {
					m.Dimensions[i].DailyUnits = -1
				}
			}
		}, "dimensions.wal_bytes.daily_units", StateMissing, SchemaVersion},
		{"zero_retention", func(m *RetentionModel) {
			for i := range m.Dimensions {
				if m.Dimensions[i].Name == DimWALBytes {
					m.Dimensions[i].RetentionDays = 0
				}
			}
		}, "dimensions.wal_bytes.retention_days", StateMissing, SchemaVersion},
		{"archive_after_retention", func(m *RetentionModel) {
			for i := range m.Dimensions {
				if m.Dimensions[i].Name == DimWALBytes {
					m.Dimensions[i].ArchiveAfterDays = m.Dimensions[i].RetentionDays + 1
				}
			}
		}, "dimensions.wal_bytes.archive_after_days", StateMissing, SchemaVersion},
		{"zero_restore_rate", func(m *RetentionModel) { m.Archive.RestoreDailyBytes = 0 }, "archive.restore_daily_bytes", StateMissing, SchemaVersion},
		{"zero_restore_window", func(m *RetentionModel) { m.Archive.RestoreWindowDays = 0 }, "archive.restore_window_days", StateMissing, SchemaVersion},
		{"negative_rate", func(m *RetentionModel) { m.Rates.CentsPerLiveGBMonth = -1 }, "rates.cents_per_live_gb_month", StateMissing, SchemaVersion},
		{"negative_budget", func(m *RetentionModel) { m.BudgetCents = -1 }, "budget_cents", StateMissing, SchemaVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := healthyRetentionModel()
			tc.mutate(&model)
			before := cloneRetentionModel(model)
			_, err := EvaluateRetention(model)
			var rejection *RetentionRejection
			if !errors.As(err, &rejection) {
				t.Fatalf("malformed model did not return a typed rejection: %v", err)
			}
			if rejection.Code != RetentionRejectedCode || rejection.Field != tc.field || rejection.State != tc.state || rejection.ModelVersion != tc.version {
				t.Fatalf("rejection = %+v, want field=%s state=%s version=%d", rejection, tc.field, tc.state, tc.version)
			}
			if !errors.Is(err, ErrRetentionRejected) {
				t.Fatalf("error does not unwrap to ErrRetentionRejected: %v", err)
			}
			if !reflect.DeepEqual(before, model) {
				t.Fatal("EvaluateRetention mutated the malformed model")
			}
		})
	}
}

// TestTodo_PERF_009_Security proves the growth model carries no tenant
// identity and evaluates purely: same model in, same digest out, input
// untouched.
func TestTodo_PERF_009_Security(t *testing.T) {
	model := healthyRetentionModel()
	first, err := EvaluateRetention(model)
	if err != nil {
		t.Fatalf("healthy retention model rejected: %v", err)
	}
	before := cloneRetentionModel(model)
	second, err := EvaluateRetention(model)
	if err != nil {
		t.Fatalf("second evaluation rejected: %v", err)
	}
	if first.Digest != second.Digest || !reflect.DeepEqual(first, second) {
		t.Fatal("identical models evaluated to different reports")
	}
	if !reflect.DeepEqual(before, model) {
		t.Fatal("EvaluateRetention mutated its input model")
	}
	assertNoTenantIdentity(t, model, first)
}

// TestTodo_PERF_009_Recovery proves the archive/restore round trip: live,
// archived and disposed bytes conserve the ingest total at every horizon,
// the healthy restore fits its window, and a restore that cannot fit
// raises an alarm without passing silently.
func TestTodo_PERF_009_Recovery(t *testing.T) {
	model := healthyRetentionModel()
	report, err := EvaluateRetention(model)
	if err != nil {
		t.Fatalf("healthy retention model rejected: %v", err)
	}
	for _, horizon := range RetentionHorizons {
		var live, archived, disposed int64
		for _, dim := range model.Dimensions {
			live += liveUnits(dim, horizon)
			archived += archivedUnits(dim, horizon)
			disposed += disposedUnits(dim, horizon)
		}
		var reportedArchive int64
		for _, archive := range report.Archive {
			if archive.HorizonDays == horizon {
				reportedArchive = archive.ArchivedBytes
			}
		}
		if archived != reportedArchive {
			t.Fatalf("%d-day archive %d != sum of dimension flows %d", horizon, reportedArchive, archived)
		}
		var ingested int64
		for _, dim := range model.Dimensions {
			ingested += dim.DailyUnits*horizon + dim.HeldStockUnits + dim.HeldDailyUnits*horizon
		}
		if live+archived+disposed != ingested {
			t.Fatalf("%d-day conservation: live=%d archived=%d disposed=%d ingested=%d", horizon, live, archived, disposed, ingested)
		}
	}
	if report.RestoreDays365 > model.Archive.RestoreWindowDays {
		t.Fatalf("healthy restore needs %d days in a %d-day window", report.RestoreDays365, model.Archive.RestoreWindowDays)
	}

	slow := cloneRetentionModel(model)
	slow.Archive.RestoreDailyBytes = gb
	restored, err := EvaluateRetention(slow)
	if err != nil {
		t.Fatalf("slow-restore model rejected, want alarm instead: %v", err)
	}
	found := false
	for _, alarm := range restored.Alarms {
		if alarm.Code == AlarmRestoreWindowExceeded && alarm.Field == "archive.restore_days" {
			found = true
		}
	}
	if !found {
		t.Fatalf("slow restore raised no window alarm: %+v", restored.Alarms)
	}
}

// TestTodo_PERF_009_Conformance proves the evaluated shape is fixed: the
// exact 30/90/365-day horizons on every required dimension, no partial
// completion and no undeclared dimensions.
func TestTodo_PERF_009_Conformance(t *testing.T) {
	report, err := EvaluateRetention(healthyRetentionModel())
	if err != nil {
		t.Fatalf("healthy retention model rejected: %v", err)
	}
	if len(report.Dimensions) != len(RequiredGrowthDimensions) {
		t.Fatalf("report has %d dimensions, want %d", len(report.Dimensions), len(RequiredGrowthDimensions))
	}
	seen := map[string]bool{}
	for _, projection := range report.Dimensions {
		seen[projection.Name] = true
		if len(projection.Horizons) != len(RetentionHorizons) {
			t.Fatalf("dimension %s has %d horizons", projection.Name, len(projection.Horizons))
		}
		for i, horizon := range projection.Horizons {
			if horizon.HorizonDays != RetentionHorizons[i] {
				t.Fatalf("dimension %s horizon %d = %d days", projection.Name, i, horizon.HorizonDays)
			}
		}
	}
	for _, required := range RequiredGrowthDimensions {
		if !seen[required] {
			t.Fatalf("report omits required dimension %s", required)
		}
	}
	if len(report.Archive) != len(RetentionHorizons) {
		t.Fatalf("report has %d archive horizons", len(report.Archive))
	}
}
