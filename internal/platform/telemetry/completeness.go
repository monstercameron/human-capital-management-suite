package telemetry

import (
	"sort"
	"time"
)

// HealthState is telemetry completeness evidence's own typed result. It is
// never conflated with a business outcome: a HealthState describes whether
// telemetry itself can be trusted, not whether the business operation it
// would have described succeeded (structured-logging-and-opentelemetry.md
// "Scope").
type HealthState string

// Published health states. There is no "healthy on missing data" state:
// Check only ever returns HealthHealthy when every required signal was
// observed and no fault was reported.
const (
	HealthUnknown  HealthState = "UNKNOWN"
	HealthDegraded HealthState = "DEGRADED"
	HealthHealthy  HealthState = "HEALTHY"
)

// Sink is what a completeness check observes: the signal names a sink has
// actually recorded emission for. It is read-only by construction — the
// interface has no method that could mutate ledger, outbox, business-event
// or provider-request state, so a completeness check can never acquire
// business-side-effect authority no matter how it is implemented
// (OBS-006 GREEN: telemetry authority stays separate from ledger/activity/
// decision authority).
type Sink interface {
	EmittedMetricNames() []string
	EmittedLogEventNames() []string
}

// FakeSink is a deterministic, in-memory Sink. It stands in for the
// eventual Collector/exporter (OBS-002/OBS-003), which are out of this
// lane.
type FakeSink struct {
	Metrics   []string
	LogEvents []string
}

// EmittedMetricNames implements Sink.
func (s FakeSink) EmittedMetricNames() []string { return append([]string(nil), s.Metrics...) }

// EmittedLogEventNames implements Sink.
func (s FakeSink) EmittedLogEventNames() []string { return append([]string(nil), s.LogEvents...) }

// RequiredSignals is the versioned, minimum set of metric and log-event
// names the P1A cell must observe for telemetry to certify complete.
type RequiredSignals struct {
	Version           int
	RequiredMetrics   []string
	RequiredLogEvents []string
}

// DefaultRequiredSignals returns the required-signal set derived from the
// P1A cell's own metric catalog, plus the log events that name the same
// lifecycle transitions. Deriving RequiredMetrics from P1ACellMetrics
// keeps the two lists from drifting: a metric added to the cell's core
// catalog is required for completeness by construction. The event-driven
// ProviderIntegrationMetrics are deliberately not required: a cell with no
// provider traffic emits none of them, and that silence is not missing
// telemetry.
func DefaultRequiredSignals() RequiredSignals {
	return RequiredSignals{
		Version:         1,
		RequiredMetrics: sortedNames(P1ACellMetrics),
		RequiredLogEvents: []string{
			"intent.created",
			"intent.simulated",
			"ledger.append",
		},
	}
}

// PipelineFaults reports degraded pipeline conditions Check must weigh
// alongside plain signal presence/absence
// (structured-logging-and-opentelemetry.md "Export, failure and shutdown":
// "Drops, queue saturation, redaction rejection, exporter failure and
// Collector/backend lag ... make affected assurance UNKNOWN or DEGRADED").
// The zero value reports no fault.
type PipelineFaults struct {
	RedactionFailures int
	ClockSkew         time.Duration
	ClockSkewBound    time.Duration
	BackendLag        time.Duration
	BackendLagBound   time.Duration
}

func (f PipelineFaults) degraded() bool {
	if f.RedactionFailures > 0 {
		return true
	}
	if f.ClockSkewBound > 0 && f.ClockSkew > f.ClockSkewBound {
		return true
	}
	if f.BackendLagBound > 0 && f.BackendLag > f.BackendLagBound {
		return true
	}
	return false
}

// Report is the completeness evidence for one evaluation.
type Report struct {
	Health           HealthState
	MissingMetrics   []string
	MissingLogEvents []string
	Faults           PipelineFaults
	EvaluatedAt      time.Time
	RequiredVersion  int
}

// CompletenessError is returned when Check cannot even evaluate its input —
// a malformed RequiredSignals or a nil Sink. Its Code mirrors the
// `<CODE>_REJECTED` convention planning/todos.md names for OBS-006's
// seeded-defect primary test.
type CompletenessError struct {
	Code    string
	Field   string
	State   string
	Version int
}

// Error implements error.
func (e *CompletenessError) Error() string {
	return e.Code + ": field " + e.Field + " state " + e.State
}

// Check cross-references what sink actually emitted against required, and
// weighs any reported pipeline fault. It never reports HealthHealthy on a
// missing or failed signal (OBS-006 GREEN): a nil sink (collector/exporter
// entirely lost) reports HealthUnknown, a present-but-incomplete or
// faulted sink reports HealthDegraded, and only a fully-observed, fault-free
// evaluation reports HealthHealthy.
func Check(required RequiredSignals, sink Sink, faults PipelineFaults, now time.Time) (Report, error) {
	if required.Version <= 0 {
		return Report{}, &CompletenessError{Code: "OBS_006_REJECTED", Field: "required.version", State: "missing", Version: required.Version}
	}
	if sink == nil {
		return Report{
			Health:           HealthUnknown,
			MissingMetrics:   append([]string(nil), required.RequiredMetrics...),
			MissingLogEvents: append([]string(nil), required.RequiredLogEvents...),
			Faults:           faults,
			EvaluatedAt:      now,
			RequiredVersion:  required.Version,
		}, nil
	}

	missingMetrics := diffSorted(required.RequiredMetrics, sink.EmittedMetricNames())
	missingLogs := diffSorted(required.RequiredLogEvents, sink.EmittedLogEventNames())

	health := HealthHealthy
	if len(missingMetrics) > 0 || len(missingLogs) > 0 || faults.degraded() {
		health = HealthDegraded
	}

	return Report{
		Health:           health,
		MissingMetrics:   missingMetrics,
		MissingLogEvents: missingLogs,
		Faults:           faults,
		EvaluatedAt:      now,
		RequiredVersion:  required.Version,
	}, nil
}

// diffSorted returns the entries of required not present in emitted,
// sorted, deduplicated and independent of either input's order.
func diffSorted(required, emitted []string) []string {
	have := make(map[string]bool, len(emitted))
	for _, e := range emitted {
		have[e] = true
	}
	seen := make(map[string]bool)
	var missing []string
	for _, r := range required {
		if have[r] || seen[r] {
			continue
		}
		seen[r] = true
		missing = append(missing, r)
	}
	sort.Strings(missing)
	return missing
}
