package telemetry

import (
	"fmt"
	"sort"
	"time"
)

// MetricType names a metric's aggregation shape
// (structured-logging-and-opentelemetry.md "Metrics and exemplars").
type MetricType string

// Published metric types.
const (
	MetricCounter   MetricType = "counter"
	MetricGauge     MetricType = "gauge"
	MetricHistogram MetricType = "histogram"
)

func (t MetricType) valid() bool {
	switch t {
	case MetricCounter, MetricGauge, MetricHistogram:
		return true
	default:
		return false
	}
}

// MetricDefinition is one immutable, versioned catalog entry
// (OBS-005 GREEN).
type MetricDefinition struct {
	Name        string
	Type        MetricType
	Unit        string
	Version     int
	Labels      []string
	Description string
}

// P1ACellMetrics is the versioned metric catalog for the P1A cell's
// buildable slice: BusinessIntent creation and simulation, ledger append,
// outbox publish lag and cross-edge parity
// (structured-logging-and-opentelemetry.md "Trace topology" span families
// Intent.Create/Advance, Transaction.*, Outbox.Publish and Reconciliation
// give the corresponding metric names their vocabulary).
var P1ACellMetrics = []MetricDefinition{
	{
		Name: "intent.created", Type: MetricCounter, Unit: "1", Version: 1,
		Labels:      []string{"cell_id", "tenant_class", "outcome"},
		Description: "BusinessIntent creation attempts, by outcome.",
	},
	{
		Name: "intent.simulated", Type: MetricCounter, Unit: "1", Version: 1,
		Labels:      []string{"cell_id", "tenant_class", "outcome"},
		Description: "BusinessIntent simulation runs, by outcome.",
	},
	{
		Name: "ledger.append", Type: MetricCounter, Unit: "1", Version: 1,
		Labels:      []string{"cell_id", "outcome"},
		Description: "Ledger append attempts, by outcome.",
	},
	{
		Name: "outbox.lag", Type: MetricGauge, Unit: "ms", Version: 1,
		Labels:      []string{"cell_id"},
		Description: "Age of the oldest unpublished outbox entry.",
	},
	{
		Name: "edge.parity", Type: MetricGauge, Unit: "1", Version: 1,
		Labels:      []string{"cell_id", "edge"},
		Description: "1 when a replication/consistency edge is in parity, 0 otherwise.",
	},
	{
		Name: "effect.dispatch.duration", Type: MetricHistogram, Unit: "ms", Version: 1,
		Labels:      []string{"cell_id", "outcome"},
		Description: "Effect-dispatch handler latency; exemplars link sampled observations back to their trace.",
	},
}

// ProviderIntegrationMetrics is the versioned catalog for the third-party
// provider hand-off (payroll and IAM delivery from the outbox, provider
// callbacks, the OAuth token source and the circuit breaker). Every label is
// a closed, low-cardinality vocabulary: change refs, correlation ids, event
// ids and tenants are never metric labels.
//
// These are event-driven signals: a cell with no provider traffic
// legitimately emits none of them, so DefaultRequiredSignals derives its
// completeness requirement from P1ACellMetrics only and never reports a
// quiet provider integration as missing telemetry.
var ProviderIntegrationMetrics = []MetricDefinition{
	{
		Name: "provider.delivery.attempts", Type: MetricCounter, Unit: "1", Version: 1,
		Labels:      []string{"provider", "provider_operation", "outcome_class"},
		Description: "Provider delivery, reversal and status attempts, by outcome class.",
	},
	{
		Name: "provider.delivery.duration", Type: MetricHistogram, Unit: "ms", Version: 1,
		Labels:      []string{"provider", "provider_operation", "outcome_class"},
		Description: "Latency of one provider delivery, reversal or status attempt.",
	},
	{
		Name: "provider.delivery.abandoned", Type: MetricCounter, Unit: "1", Version: 1,
		Labels:      []string{"provider", "outcome_class"},
		Description: "Provider changes abandoned after the retry budget, by the last outcome class.",
	},
	{
		Name: "provider.retry.delay", Type: MetricHistogram, Unit: "ms", Version: 1,
		Labels:      []string{"provider"},
		Description: "Delay scheduled before the next provider delivery attempt.",
	},
	{
		Name: "provider.breaker.transitions", Type: MetricCounter, Unit: "1", Version: 1,
		Labels:      []string{"provider", "to_state"},
		Description: "Provider circuit-breaker state transitions, by the state entered.",
	},
	{
		Name: "provider.callback.received", Type: MetricCounter, Unit: "1", Version: 1,
		Labels:      []string{"provider", "result"},
		Description: "Signed provider callbacks received, by intake result.",
	},
	{
		Name: "provider.callback.secret_index", Type: MetricCounter, Unit: "1", Version: 1,
		Labels:      []string{"provider", "secret_slot"},
		Description: "Verified provider callbacks by which signing secret matched (current or previous), for rotation tracking.",
	},
	{
		Name: "provider.token.refreshes", Type: MetricCounter, Unit: "1", Version: 1,
		Labels:      []string{"result"},
		Description: "OAuth client-credentials token refreshes, by result.",
	},
	{
		Name: "provider.wait.near_timeout", Type: MetricCounter, Unit: "1", Version: 1,
		Labels:      []string{"provider"},
		Description: "Provider result waits that came close to their timeout before a callback arrived.",
	},
}

// MetricCatalog returns a defensive, fully independent copy of the P1A
// cell's metric catalog (P1ACellMetrics followed by
// ProviderIntegrationMetrics): each entry's Labels slice is copied too, so
// a caller mutating its own copy can never reach back into either
// catalog's backing array.
func MetricCatalog() []MetricDefinition {
	out := make([]MetricDefinition, 0, len(P1ACellMetrics)+len(ProviderIntegrationMetrics))
	for _, catalog := range [][]MetricDefinition{P1ACellMetrics, ProviderIntegrationMetrics} {
		for _, m := range catalog {
			m.Labels = append([]string(nil), m.Labels...)
			out = append(out, m)
		}
	}
	return out
}

// CatalogByName returns the catalog indexed by metric name.
func CatalogByName(catalog []MetricDefinition) map[string]MetricDefinition {
	out := make(map[string]MetricDefinition, len(catalog))
	for _, m := range catalog {
		out[m.Name] = m
	}
	return out
}

// Metric-catalog validation errors.
var (
	ErrMetricNameEmpty      = fmt.Errorf("telemetry: metric name is empty")
	ErrMetricDuplicateName  = fmt.Errorf("telemetry: metric name is registered twice")
	ErrMetricInvalidType    = fmt.Errorf("telemetry: metric type is not published")
	ErrMetricUnitEmpty      = fmt.Errorf("telemetry: metric unit is empty")
	ErrMetricLabelUnbounded = fmt.Errorf("telemetry: metric label is not a bounded-cardinality allow-listed key")
)

// ValidateCatalog checks name uniqueness, a published type, a declared
// unit and that every label is a bounded-cardinality key registered for
// SignalMetric in allow — an unbounded or unregistered label on a metric
// definition is the cardinality-explosion defect OBS-004/OBS-005 exist to
// stop before it ever reaches a real backend.
func ValidateCatalog(catalog []MetricDefinition, allow *Allowlist) error {
	seen := make(map[string]bool, len(catalog))
	for _, m := range catalog {
		if m.Name == "" {
			return ErrMetricNameEmpty
		}
		if seen[m.Name] {
			return fmt.Errorf("%w: %q", ErrMetricDuplicateName, m.Name)
		}
		seen[m.Name] = true
		if !m.Type.valid() {
			return fmt.Errorf("%w: %q for metric %q", ErrMetricInvalidType, m.Type, m.Name)
		}
		if m.Unit == "" {
			return fmt.Errorf("%w: metric %q", ErrMetricUnitEmpty, m.Name)
		}
		for _, label := range m.Labels {
			def, ok := allow.Lookup(label)
			if !ok || def.MaxCardinality <= 0 || !allow.AllowsSignal(label, SignalMetric) {
				return fmt.Errorf("%w: label %q on metric %q", ErrMetricLabelUnbounded, label, m.Name)
			}
		}
	}
	return nil
}

// Aggregation names a supported panel/query aggregation.
type Aggregation string

// Published aggregations.
const (
	AggregationSum  Aggregation = "sum"
	AggregationAvg  Aggregation = "avg"
	AggregationMax  Aggregation = "max"
	AggregationMin  Aggregation = "min"
	AggregationRate Aggregation = "rate"
	AggregationP95  Aggregation = "p95"
	AggregationP99  Aggregation = "p99"
)

func (a Aggregation) valid() bool {
	switch a {
	case AggregationSum, AggregationAvg, AggregationMax, AggregationMin, AggregationRate, AggregationP95, AggregationP99:
		return true
	default:
		return false
	}
}

// DashboardPanel is one generated dashboard panel (OBS-005 GREEN: "drill
// through evidence").
type DashboardPanel struct {
	Title       string
	Metric      string
	Aggregation Aggregation
	GroupBy     []string
}

// Dashboard is one generated, versioned dashboard.
type Dashboard struct {
	ID      string
	Title   string
	Version int
	Panels  []DashboardPanel
}

// Dashboard/panel validation errors.
var (
	ErrDashboardIDEmpty        = fmt.Errorf("telemetry: dashboard id is empty")
	ErrPanelMetricUnknown      = fmt.Errorf("telemetry: panel metric is not in the catalog")
	ErrPanelAggregationUnknown = fmt.Errorf("telemetry: panel aggregation is not published")
	ErrPanelGroupByUnbounded   = fmt.Errorf("telemetry: panel groups by an unbounded or unregistered label")
	ErrPanelGroupByNotOnMetric = fmt.Errorf("telemetry: panel groups by a label the metric does not declare")
)

// ValidatePanel rejects an "unbounded query" (OBS-005 RED): every group-by
// dimension must be one of the metric's own declared labels, and that
// label must itself be bounded-cardinality in allow.
func ValidatePanel(p DashboardPanel, catalog map[string]MetricDefinition, allow *Allowlist) error {
	m, ok := catalog[p.Metric]
	if !ok {
		return fmt.Errorf("%w: %q", ErrPanelMetricUnknown, p.Metric)
	}
	if !p.Aggregation.valid() {
		return fmt.Errorf("%w: %q", ErrPanelAggregationUnknown, p.Aggregation)
	}
	metricLabels := make(map[string]bool, len(m.Labels))
	for _, l := range m.Labels {
		metricLabels[l] = true
	}
	for _, g := range p.GroupBy {
		if !metricLabels[g] {
			return fmt.Errorf("%w: %q on metric %q", ErrPanelGroupByNotOnMetric, g, p.Metric)
		}
		def, ok := allow.Lookup(g)
		if !ok || def.MaxCardinality <= 0 {
			return fmt.Errorf("%w: %q", ErrPanelGroupByUnbounded, g)
		}
	}
	return nil
}

// ValidateDashboard validates a dashboard's identity and every panel.
func ValidateDashboard(d Dashboard, catalog map[string]MetricDefinition, allow *Allowlist) error {
	if d.ID == "" {
		return ErrDashboardIDEmpty
	}
	for _, p := range d.Panels {
		if err := ValidatePanel(p, catalog, allow); err != nil {
			return fmt.Errorf("dashboard %q: %w", d.ID, err)
		}
	}
	return nil
}

// AlertSeverity names an alert's escalation weight.
type AlertSeverity string

// Published alert severities.
const (
	AlertWarning  AlertSeverity = "warning"
	AlertCritical AlertSeverity = "critical"
)

func (s AlertSeverity) valid() bool {
	switch s {
	case AlertWarning, AlertCritical:
		return true
	default:
		return false
	}
}

// AlertRule is one generated, versioned alert rule. It is either
// metric-based (Metric set) or ratio-based (both Numerator and Denominator
// set): exactly one shape is legal, so a ratio can never have an implicit,
// ambiguous denominator (OBS-005 RED: "Ambiguous denominator ... fails").
type AlertRule struct {
	ID          string
	Version     int
	Metric      string
	Numerator   string
	Denominator string
	Condition   string // ">", "<", ">=", "<="
	Threshold   float64
	For         time.Duration
	// MaxStaleness bounds how old the underlying observation may be before
	// the rule refuses to evaluate as healthy (OBS-005 RED: "stale/unknown-
	// as-healthy ... fails"). Required whenever the referenced metric is a
	// gauge, since a gauge that stops updating otherwise reads as
	// "unchanged" rather than "missing".
	MaxStaleness time.Duration
	Severity     AlertSeverity
	Description  string
}

// Alert-rule validation errors.
var (
	ErrAlertIDEmpty             = fmt.Errorf("telemetry: alert id is empty")
	ErrAlertShapeAmbiguous      = fmt.Errorf("telemetry: alert must be exactly one of metric-based or ratio-based")
	ErrAlertDenominatorMissing  = fmt.Errorf("telemetry: ratio alert has no explicit denominator")
	ErrAlertMetricUnknown       = fmt.Errorf("telemetry: alert references a metric not in the catalog")
	ErrAlertConditionUnknown    = fmt.Errorf("telemetry: alert condition is not published")
	ErrAlertForNotPositive      = fmt.Errorf("telemetry: alert For duration must be positive")
	ErrAlertSeverityUnknown     = fmt.Errorf("telemetry: alert severity is not published")
	ErrAlertGaugeNeedsStaleness = fmt.Errorf("telemetry: alert on a gauge metric has no MaxStaleness bound")
)

// ValidateAlertRule enforces an explicit ratio shape, a known metric
// reference, a published condition/severity, a positive evaluation window
// and — for any gauge-backed rule — a positive staleness bound, so a
// stalled or missing signal can never silently evaluate as healthy.
func ValidateAlertRule(r AlertRule, catalog map[string]MetricDefinition) error {
	if r.ID == "" {
		return ErrAlertIDEmpty
	}
	isMetricBased := r.Metric != ""
	isRatioBased := r.Numerator != "" || r.Denominator != ""
	if isMetricBased == isRatioBased {
		return fmt.Errorf("%w: alert %q", ErrAlertShapeAmbiguous, r.ID)
	}
	if isRatioBased && (r.Numerator == "" || r.Denominator == "") {
		return fmt.Errorf("%w: alert %q", ErrAlertDenominatorMissing, r.ID)
	}
	metricNames := []string{r.Metric}
	if isRatioBased {
		metricNames = []string{r.Numerator, r.Denominator}
	}
	var gaugeReferenced bool
	for _, name := range metricNames {
		m, ok := catalog[name]
		if !ok {
			return fmt.Errorf("%w: %q on alert %q", ErrAlertMetricUnknown, name, r.ID)
		}
		if m.Type == MetricGauge {
			gaugeReferenced = true
		}
	}
	switch r.Condition {
	case ">", "<", ">=", "<=":
	default:
		return fmt.Errorf("%w: %q on alert %q", ErrAlertConditionUnknown, r.Condition, r.ID)
	}
	if r.For <= 0 {
		return fmt.Errorf("%w: alert %q", ErrAlertForNotPositive, r.ID)
	}
	if !r.Severity.valid() {
		return fmt.Errorf("%w: %q on alert %q", ErrAlertSeverityUnknown, r.Severity, r.ID)
	}
	if gaugeReferenced && r.MaxStaleness <= 0 {
		return fmt.Errorf("%w: alert %q", ErrAlertGaugeNeedsStaleness, r.ID)
	}
	return nil
}

// P1ACellDashboards returns the generated dashboards for the P1A cell.
func P1ACellDashboards() []Dashboard {
	return []Dashboard{
		{
			ID: "p1a-cell-overview", Title: "P1A Cell Overview", Version: 1,
			Panels: []DashboardPanel{
				{Title: "Intent creation outcome", Metric: "intent.created", Aggregation: AggregationRate, GroupBy: []string{"cell_id", "outcome"}},
				{Title: "Intent simulation outcome", Metric: "intent.simulated", Aggregation: AggregationRate, GroupBy: []string{"cell_id", "outcome"}},
				{Title: "Ledger append outcome", Metric: "ledger.append", Aggregation: AggregationRate, GroupBy: []string{"cell_id", "outcome"}},
				{Title: "Outbox lag", Metric: "outbox.lag", Aggregation: AggregationMax, GroupBy: []string{"cell_id"}},
				{Title: "Edge parity", Metric: "edge.parity", Aggregation: AggregationMin, GroupBy: []string{"cell_id", "edge"}},
			},
		},
	}
}

// P1ACellAlertRules returns the generated alert rules for the P1A cell.
func P1ACellAlertRules() []AlertRule {
	return []AlertRule{
		{
			ID: "ledger-append-conversion-low", Version: 1,
			Numerator: "ledger.append", Denominator: "intent.created",
			Condition: "<", Threshold: 0.90, For: 5 * time.Minute,
			Severity: AlertWarning,
			Description: "Fewer than 90% of created intents reach a ledger append over 5 minutes " +
				"(explicit numerator/denominator: no implicit ratio denominator).",
		},
		{
			ID: "outbox-lag-high", Version: 1,
			Metric: "outbox.lag", Condition: ">", Threshold: 60000, For: 5 * time.Minute,
			MaxStaleness: 2 * time.Minute, Severity: AlertCritical,
			Description: "Outbox publish lag exceeds 60s.",
		},
		{
			ID: "edge-parity-lost", Version: 1,
			Metric: "edge.parity", Condition: "<", Threshold: 1, For: 5 * time.Minute,
			MaxStaleness: 2 * time.Minute, Severity: AlertCritical,
			Description: "A replication/consistency edge has lost parity.",
		},
	}
}

// sortedNames is a small helper the completeness checker and tests share.
func sortedNames(catalog []MetricDefinition) []string {
	names := make([]string, 0, len(catalog))
	for _, m := range catalog {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	return names
}
