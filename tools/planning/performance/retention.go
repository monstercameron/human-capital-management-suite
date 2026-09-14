// ---------------------------------------------------------------------------
// PERF-009: long-horizon retention, storage-growth and lifecycle-cost model.
//
// This is a pure, versioned projection model, not a load test. The caller
// supplies daily ingest rates per growth dimension (from the workload
// envelope registry), retention periods (from the retention registry),
// legal-hold stocks and flows, archive/restore policy and GB-month rates
// (from the PERF-007 cost registry). EvaluateRetention projects live,
// archived and disposed bytes at the fixed 30/90/365-day horizons,
// checks every projection against declared headroom, prices the year in
// GB-months against budget and digests the report. It performs no I/O,
// touches no database, object store or ledger, and persists nothing.
//
// Live bytes at day H for one dimension are daily ingest accumulated up
// to the live cap (retention, or the earlier archive point when the
// dimension archives) plus held stock and daily holds carried the full
// horizon: held data is never disposed, so adding holds can only move a
// verdict toward breach, never toward pass.
// ---------------------------------------------------------------------------

package performance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	// RetentionRejectedCode identifies every PERF-009 rejection.
	RetentionRejectedCode = "PERF_009_REJECTED"

	// RetentionHorizons are the only evaluated horizons, in days.
	Horizon30Days  int64 = 30
	Horizon90Days  int64 = 90
	Horizon365Days int64 = 365

	// BytesPerGBMonth converts byte-days to GB-months at 30 days.
	BytesPerGBMonth = float64(int64(1)<<30) * 30

	// Dimension names in the fixed PERF-009 registry.
	DimPartitionBytes = "partition_bytes"
	DimWALBytes       = "wal_bytes"
	DimIndexBytes     = "index_bytes"
	DimObjectBytes    = "object_bytes"
	DimTelemetryBytes = "telemetry_bytes"
	DimVacuumSeconds  = "vacuum_seconds"
	DimLegalHoldBytes = "legal_hold_bytes"

	// AlarmRolloverWithinHorizon marks a dimension crossing its
	// expansion threshold inside an evaluated horizon.
	AlarmRolloverWithinHorizon = "ROLLOVER_WITHIN_HORIZON"
	// AlarmRestoreWindowExceeded marks a restore drill that cannot
	// drain the 365-day archive inside its window.
	AlarmRestoreWindowExceeded = "RESTORE_WINDOW_EXCEEDED"
)

// RetentionHorizons lists the evaluated horizons in ascending order.
var RetentionHorizons = []int64{Horizon30Days, Horizon90Days, Horizon365Days}

// RequiredGrowthDimensions is the fixed dimension registry: a model
// missing one is incomplete, and an undeclared dimension is rejected,
// so evaluations can never partially complete.
var RequiredGrowthDimensions = []string{
	DimPartitionBytes,
	DimWALBytes,
	DimIndexBytes,
	DimObjectBytes,
	DimTelemetryBytes,
	DimVacuumSeconds,
	DimLegalHoldBytes,
}

// ErrRetentionRejected is the sentinel every PERF-009 rejection unwraps to.
var ErrRetentionRejected = errors.New("performance: PERF_009_REJECTED")

// RetentionRejection is the typed PERF-009 rejection: Field, State and
// ModelVersion are machine-readable without parsing error prose.
type RetentionRejection struct {
	Code         string `json:"code"`
	Field        string `json:"field"`
	State        string `json:"state"`
	ModelVersion int    `json:"model_version"`
	Reason       string `json:"reason"`
}

func (r *RetentionRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s model_version=%d: %s", r.Code, r.Field, r.State, r.ModelVersion, r.Reason)
}

// Unwrap lets callers test errors.Is(err, ErrRetentionRejected).
func (r *RetentionRejection) Unwrap() error { return ErrRetentionRejected }

// GrowthDimension is one retained growth series: daily ingest in Unit,
// live accumulation capped by RetentionDays (or the earlier
// ArchiveAfterDays when positive), held stock and daily holds carried
// the full horizon without disposition, and the headroom Capacity the
// live projection must fit.
type GrowthDimension struct {
	Name             string `json:"name"`
	DailyUnits       int64  `json:"daily_units"`
	Unit             string `json:"unit"`
	RetentionDays    int64  `json:"retention_days"`
	HeldStockUnits   int64  `json:"held_stock_units"`
	HeldDailyUnits   int64  `json:"held_daily_units"`
	Capacity         Limit  `json:"capacity"`
	ArchiveAfterDays int64  `json:"archive_after_days"`
}

// ArchiveModel is the shared archive pool: everything a dimension sheds
// at its archive point accumulates here against CapacityBytes, and the
// 365-day pool must restore at RestoreDailyBytes inside RestoreWindowDays.
type ArchiveModel struct {
	CapacityBytes     Limit `json:"capacity_bytes"`
	RestoreDailyBytes int64 `json:"restore_daily_bytes"`
	RestoreWindowDays int64 `json:"restore_window_days"`
}

// RetentionCostRates prices live and archive GB-months. Rates come from
// the PERF-007 cost registry supplied by the caller; this model invents
// no prices and no tenant-specific constants.
type RetentionCostRates struct {
	CentsPerLiveGBMonth    int64 `json:"cents_per_live_gb_month"`
	CentsPerArchiveGBMonth int64 `json:"cents_per_archive_gb_month"`
}

// RetentionModel is the versioned PERF-009 long-horizon model for one
// workload envelope. Evaluating it is pure: no I/O and no persistence.
type RetentionModel struct {
	SchemaVersion int                `json:"schema_version"`
	ID            string             `json:"id"`
	EnvelopeID    string             `json:"envelope_id"`
	Dimensions    []GrowthDimension  `json:"dimensions"`
	Archive       ArchiveModel       `json:"archive"`
	Rates         RetentionCostRates `json:"rates"`
	BudgetCents   int64              `json:"budget_cents"`
}

// HorizonProjection is one dimension's projection at one horizon:
// live units, headroom against capacity, the first rollover day inside
// the horizon (0 when none) and whether the horizon fits.
type HorizonProjection struct {
	HorizonDays    int64   `json:"horizon_days"`
	LiveUnits      int64   `json:"live_units"`
	HeadroomRatio  float64 `json:"headroom_ratio"`
	RolloverDay    int64   `json:"rollover_day"`
	WithinCapacity bool    `json:"within_capacity"`
}

// DimensionProjection is one dimension's projections across horizons.
type DimensionProjection struct {
	Name     string              `json:"name"`
	Unit     string              `json:"unit"`
	Horizons []HorizonProjection `json:"horizons"`
}

// ArchiveProjection is the shared pool's projection at one horizon.
type ArchiveProjection struct {
	HorizonDays    int64 `json:"horizon_days"`
	ArchivedBytes  int64 `json:"archived_bytes"`
	RolloverDay    int64 `json:"rollover_day"`
	WithinCapacity bool  `json:"within_capacity"`
}

// RetentionAlarm is a non-rejecting early warning: rollover inside a
// horizon or a restore drill that misses its window. Alarms never pass
// silently, but only headroom, archive, cost and model violations reject.
type RetentionAlarm struct {
	Code   string `json:"code"`
	Field  string `json:"field"`
	Detail string `json:"detail"`
}

// RetentionCost is the priced 365-day lifecycle: live and archive
// GB-months with their cent totals against budget.
type RetentionCost struct {
	LiveGBMonths    float64 `json:"live_gb_months"`
	ArchiveGBMonths float64 `json:"archive_gb_months"`
	LiveCents       int64   `json:"live_cents"`
	ArchiveCents    int64   `json:"archive_cents"`
	TotalCents      int64   `json:"total_cents"`
	BudgetCents     int64   `json:"budget_cents"`
	WithinBudget    bool    `json:"within_budget"`
}

// RetentionReport is the deterministic PERF-009 evaluation.
type RetentionReport struct {
	SchemaVersion  int                   `json:"schema_version"`
	ID             string                `json:"id"`
	EnvelopeID     string                `json:"envelope_id"`
	Dimensions     []DimensionProjection `json:"dimensions"`
	Archive        []ArchiveProjection   `json:"archives"`
	RestoreDays365 int64                 `json:"restore_days_365"`
	Cost           RetentionCost         `json:"cost"`
	Alarms         []RetentionAlarm      `json:"alarms"`
	Digest         string                `json:"digest"`
}

type retentionViolation struct {
	field  string
	state  string
	reason string
	// breach sorts after every model defect so a malformed model is
	// never reported as merely over budget.
	breach bool
}

// ValidateRetention returns every PERF-009 violation in deterministic
// order: model defects first, then horizon breaches by dimension and
// horizon, archive breaches, and the cost breach last. Empty means the
// model is complete and every horizon fits with cost within budget.
func ValidateRetention(model RetentionModel) []RetentionRejection {
	var defects []retentionViolation
	var breaches []retentionViolation
	defect := func(field, state, reason string) {
		defects = append(defects, retentionViolation{field: field, state: state, reason: reason})
	}
	breach := func(field, reason string) {
		breaches = append(breaches, retentionViolation{field: field, state: StateBreach, reason: reason, breach: true})
	}

	if model.SchemaVersion != SchemaVersion {
		defect("schema_version", StateUnsupportedVersion, fmt.Sprintf("retention model schema version must be %d", SchemaVersion))
	}
	if strings.TrimSpace(model.ID) == "" {
		defect("id", StateMissing, "retention model id is required")
	}
	if strings.TrimSpace(model.EnvelopeID) == "" {
		defect("envelope_id", StateMissing, "retention model must reference its source envelope id")
	}

	required := make(map[string]bool, len(RequiredGrowthDimensions))
	for _, name := range RequiredGrowthDimensions {
		required[name] = true
	}
	byName := make(map[string]GrowthDimension, len(model.Dimensions))
	for _, dim := range model.Dimensions {
		if !required[dim.Name] {
			defect(fmt.Sprintf("dimensions.%s", dim.Name), StateMissing, fmt.Sprintf("undeclared growth dimension %q", dim.Name))
			continue
		}
		if _, dup := byName[dim.Name]; dup {
			defect(fmt.Sprintf("dimensions.%s", dim.Name), StateMissing, fmt.Sprintf("duplicate growth dimension %q", dim.Name))
			continue
		}
		byName[dim.Name] = dim
	}
	for _, name := range RequiredGrowthDimensions {
		if _, ok := byName[name]; !ok {
			defect(fmt.Sprintf("dimensions.%s", name), StateMissing, fmt.Sprintf("required growth dimension %q is missing", name))
		}
	}
	checkDimension := func(dim GrowthDimension) {
		prefix := "dimensions." + dim.Name
		if dim.DailyUnits < 0 {
			defect(prefix+".daily_units", StateMissing, "daily ingest cannot be negative")
		}
		if strings.TrimSpace(dim.Unit) == "" {
			defect(prefix+".unit", StateMissing, "a unit is required")
		}
		if dim.RetentionDays <= 0 {
			defect(prefix+".retention_days", StateMissing, "a positive retention period is required")
		}
		if dim.HeldStockUnits < 0 {
			defect(prefix+".held_stock_units", StateMissing, "held stock cannot be negative")
		}
		if dim.HeldDailyUnits < 0 {
			defect(prefix+".held_daily_units", StateMissing, "daily holds cannot be negative")
		}
		if dim.Capacity.Value <= 0 || strings.TrimSpace(dim.Capacity.Unit) == "" {
			defect(prefix+".capacity", StateMissing, "a positive headroom capacity and unit are required")
		}
		if dim.ArchiveAfterDays < 0 || (dim.ArchiveAfterDays > 0 && dim.ArchiveAfterDays > dim.RetentionDays) {
			defect(prefix+".archive_after_days", StateMissing, "the archive point must fall inside retention")
		}
	}
	for _, name := range RequiredGrowthDimensions {
		if dim, ok := byName[name]; ok {
			checkDimension(dim)
		}
	}

	if model.Archive.CapacityBytes.Value <= 0 || strings.TrimSpace(model.Archive.CapacityBytes.Unit) == "" {
		defect("archive.capacity_bytes", StateMissing, "a positive archive capacity and unit are required")
	}
	if model.Archive.RestoreDailyBytes <= 0 {
		defect("archive.restore_daily_bytes", StateMissing, "a positive restore rate is required")
	}
	if model.Archive.RestoreWindowDays <= 0 {
		defect("archive.restore_window_days", StateMissing, "a positive restore window is required")
	}
	if model.Rates.CentsPerLiveGBMonth < 0 {
		defect("rates.cents_per_live_gb_month", StateMissing, "live GB-month rate cannot be negative")
	}
	if model.Rates.CentsPerArchiveGBMonth < 0 {
		defect("rates.cents_per_archive_gb_month", StateMissing, "archive GB-month rate cannot be negative")
	}
	if model.BudgetCents < 0 {
		defect("budget_cents", StateMissing, "lifecycle budget cannot be negative")
	}

	ordered := orderedDimensions(model.Dimensions, byName)
	for _, dim := range ordered {
		for _, horizon := range RetentionHorizons {
			if live := projectLive(dim, horizon); live > dim.Capacity.Value {
				breach(fmt.Sprintf("dimensions.%s.horizons.%d", dim.Name, horizon),
					fmt.Sprintf("projected %d %s exceeds capacity %d at %d days", live, dim.Unit, dim.Capacity.Value, horizon))
			}
		}
	}
	for _, horizon := range RetentionHorizons {
		if archived := projectArchive(ordered, horizon); archived > model.Archive.CapacityBytes.Value {
			breach(fmt.Sprintf("archive.horizons.%d", horizon),
				fmt.Sprintf("projected archive %d bytes exceeds capacity %d at %d days", archived, model.Archive.CapacityBytes.Value, horizon))
		}
	}
	if cost := priceLifecycle(ordered, model.Rates, model.BudgetCents); cost.TotalCents > model.BudgetCents {
		breach("cost.total_cents",
			fmt.Sprintf("lifecycle cost %d cents exceeds budget %d cents", cost.TotalCents, model.BudgetCents))
	}

	// Defects sort by field so a malformed model reports deterministically;
	// breaches keep evaluation order — dimensions in registry order by
	// ascending horizon, then archive horizons ascending, then cost — so
	// the first breach is always the earliest horizon of the earliest
	// dimension, never a lexicographic accident.
	sort.SliceStable(defects, func(i, j int) bool {
		if defects[i].field != defects[j].field {
			return defects[i].field < defects[j].field
		}
		return defects[i].state < defects[j].state
	})
	out := make([]RetentionRejection, 0, len(defects)+len(breaches))
	for _, violation := range append(defects, breaches...) {
		out = append(out, RetentionRejection{
			Code: RetentionRejectedCode, Field: violation.field, State: violation.state,
			ModelVersion: model.SchemaVersion, Reason: violation.reason,
		})
	}
	return out
}

// CheckRetention rejects a defective or over-budget model with its first
// violation in deterministic order.
func CheckRetention(model RetentionModel) error {
	if violations := ValidateRetention(model); len(violations) != 0 {
		rejection := violations[0]
		return &rejection
	}
	return nil
}

// orderedDimensions returns the valid required dimensions by registry
// name so reports and digests are input-order independent.
func orderedDimensions(dims []GrowthDimension, byName map[string]GrowthDimension) []GrowthDimension {
	ordered := make([]GrowthDimension, 0, len(RequiredGrowthDimensions))
	for _, name := range RequiredGrowthDimensions {
		if dim, ok := byName[name]; ok {
			ordered = append(ordered, dim)
		}
	}
	return ordered
}

// liveCap is the accumulation cap: retention, or the earlier archive
// point when the dimension archives.
func liveCap(dim GrowthDimension) int64 {
	if dim.ArchiveAfterDays > 0 && dim.ArchiveAfterDays < dim.RetentionDays {
		return dim.ArchiveAfterDays
	}
	return dim.RetentionDays
}

// projectLive is the live projection at day H: daily ingest accumulated
// to the live cap plus held stock and daily holds carried uncapped —
// held data is never disposed at any horizon.
func projectLive(dim GrowthDimension, horizon int64) int64 {
	accumulate := horizon
	if cap := liveCap(dim); accumulate > cap {
		accumulate = cap
	}
	return dim.DailyUnits*accumulate + dim.HeldStockUnits + dim.HeldDailyUnits*horizon
}

// projectArchive is the shared pool at day H: daily ingest older than
// each dimension's archive point but younger than its retention.
func projectArchive(dims []GrowthDimension, horizon int64) int64 {
	var archived int64
	for _, dim := range dims {
		if dim.ArchiveAfterDays <= 0 {
			continue
		}
		span := horizon
		if span > dim.RetentionDays {
			span = dim.RetentionDays
		}
		span -= dim.ArchiveAfterDays
		if span > 0 {
			archived += dim.DailyUnits * span
		}
	}
	return archived
}

// rolloverDay is the first day in 1..horizon at which live bytes reach
// the expansion threshold, or 0 when the horizon never gets there.
func rolloverDay(dim GrowthDimension, horizon int64) int64 {
	threshold := int64(math.Ceil(float64(dim.Capacity.Value) * RolloverThreshold))
	for day := int64(1); day <= horizon; day++ {
		if projectLive(dim, day) >= threshold {
			return day
		}
	}
	return 0
}

// archiveRolloverDay is the first day in 1..horizon at which the pool
// reaches the expansion threshold, or 0 when it never does.
func archiveRolloverDay(dims []GrowthDimension, capacity, horizon int64) int64 {
	threshold := int64(math.Ceil(float64(capacity) * RolloverThreshold))
	for day := int64(1); day <= horizon; day++ {
		if projectArchive(dims, day) >= threshold {
			return day
		}
	}
	return 0
}

// isByteDimension reports whether the dimension prices into GB-months.
// Vacuum seconds are maintenance load, not stored bytes.
func isByteDimension(dim GrowthDimension) bool {
	return dim.Name != DimVacuumSeconds
}

// priceLifecycle prices the 365-day lifecycle in GB-months against
// budget from the caller-supplied PERF-007 rates.
func priceLifecycle(dims []GrowthDimension, rates RetentionCostRates, budget int64) RetentionCost {
	var liveByteDays, archiveByteDays float64
	for _, dim := range dims {
		if !isByteDimension(dim) {
			continue
		}
		for day := int64(1); day <= Horizon365Days; day++ {
			liveByteDays += float64(projectLive(dim, day))
		}
	}
	for day := int64(1); day <= Horizon365Days; day++ {
		archiveByteDays += float64(projectArchive(dims, day))
	}
	cost := RetentionCost{
		LiveGBMonths:    liveByteDays / BytesPerGBMonth,
		ArchiveGBMonths: archiveByteDays / BytesPerGBMonth,
		BudgetCents:     budget,
	}
	cost.LiveCents = int64(math.Round(cost.LiveGBMonths * float64(rates.CentsPerLiveGBMonth)))
	cost.ArchiveCents = int64(math.Round(cost.ArchiveGBMonths * float64(rates.CentsPerArchiveGBMonth)))
	cost.TotalCents = cost.LiveCents + cost.ArchiveCents
	cost.WithinBudget = cost.TotalCents <= budget
	return cost
}

// EvaluateRetention evaluates a RetentionModel into a RetentionReport.
// It is a pure function: no I/O, no persistence. A model that fails
// CheckRetention is rejected outright — there is no partial evaluation —
// and rejected runs leave the caller's model untouched.
func EvaluateRetention(model RetentionModel) (RetentionReport, error) {
	if err := CheckRetention(model); err != nil {
		return RetentionReport{}, err
	}
	byName := make(map[string]GrowthDimension, len(model.Dimensions))
	for _, dim := range model.Dimensions {
		byName[dim.Name] = dim
	}
	ordered := orderedDimensions(model.Dimensions, byName)

	report := RetentionReport{SchemaVersion: model.SchemaVersion, ID: model.ID, EnvelopeID: model.EnvelopeID}
	var alarms []RetentionAlarm
	for _, dim := range ordered {
		projection := DimensionProjection{Name: dim.Name, Unit: dim.Unit}
		for _, horizon := range RetentionHorizons {
			live := projectLive(dim, horizon)
			rollover := rolloverDay(dim, horizon)
			projection.Horizons = append(projection.Horizons, HorizonProjection{
				HorizonDays:    horizon,
				LiveUnits:      live,
				HeadroomRatio:  ratio(dim.Capacity.Value-live, dim.Capacity.Value),
				RolloverDay:    rollover,
				WithinCapacity: live <= dim.Capacity.Value,
			})
			if rollover > 0 {
				alarms = append(alarms, RetentionAlarm{
					Code: AlarmRolloverWithinHorizon, Field: "dimensions." + dim.Name,
					Detail: fmt.Sprintf("expansion threshold reached at day %d inside the %d-day horizon", rollover, horizon),
				})
			}
		}
		report.Dimensions = append(report.Dimensions, projection)
	}
	for _, horizon := range RetentionHorizons {
		archived := projectArchive(ordered, horizon)
		rollover := archiveRolloverDay(ordered, model.Archive.CapacityBytes.Value, horizon)
		report.Archive = append(report.Archive, ArchiveProjection{
			HorizonDays:    horizon,
			ArchivedBytes:  archived,
			RolloverDay:    rollover,
			WithinCapacity: archived <= model.Archive.CapacityBytes.Value,
		})
		if rollover > 0 {
			alarms = append(alarms, RetentionAlarm{
				Code: AlarmRolloverWithinHorizon, Field: "archive",
				Detail: fmt.Sprintf("expansion threshold reached at day %d inside the %d-day horizon", rollover, horizon),
			})
		}
	}
	archived365 := projectArchive(ordered, Horizon365Days)
	report.RestoreDays365 = (archived365 + model.Archive.RestoreDailyBytes - 1) / model.Archive.RestoreDailyBytes
	if report.RestoreDays365 > model.Archive.RestoreWindowDays {
		alarms = append(alarms, RetentionAlarm{
			Code: AlarmRestoreWindowExceeded, Field: "archive.restore_days",
			Detail: fmt.Sprintf("365-day archive needs %d restore days in a %d-day window", report.RestoreDays365, model.Archive.RestoreWindowDays),
		})
	}
	report.Cost = priceLifecycle(ordered, model.Rates, model.BudgetCents)
	report.Alarms = alarms
	digest, err := retentionDigest(report)
	if err != nil {
		return RetentionReport{}, err
	}
	report.Digest = digest
	return report, nil
}

func retentionDigest(report RetentionReport) (string, error) {
	copyReport := report
	copyReport.Digest = ""
	data, err := json.Marshal(copyReport)
	if err != nil {
		return "", fmt.Errorf("performance: encode retention report: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
