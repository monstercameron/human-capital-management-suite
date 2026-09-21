package otel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

// Catalog metric names (telemetry.MetricCatalog()). Declared as constants so
// newMetrics's exhaustiveness check and every Record* method reference the
// same literal the catalog itself publishes, rather than a second
// hand-typed copy that could drift from it.
const (
	metricIntentCreated         = "intent.created"
	metricIntentSimulated       = "intent.simulated"
	metricLedgerAppend          = "ledger.append"
	metricOutboxLag             = "outbox.lag"
	metricEdgeParity            = "edge.parity"
	metricEffectDispatchLatency = "effect.dispatch.duration"

	// Provider integration metrics (telemetry.ProviderIntegrationMetrics).
	metricProviderDeliveryAttempts    = "provider.delivery.attempts"
	metricProviderDeliveryDuration    = "provider.delivery.duration"
	metricProviderDeliveryAbandoned   = "provider.delivery.abandoned"
	metricProviderRetryDelay          = "provider.retry.delay"
	metricProviderBreakerTransitions  = "provider.breaker.transitions"
	metricProviderCallbackReceived    = "provider.callback.received"
	metricProviderCallbackSecretIndex = "provider.callback.secret_index"
	metricProviderTokenRefreshes      = "provider.token.refreshes"
	metricProviderWaitNearTimeout     = "provider.wait.near_timeout"
)

// Explicit histogram boundaries, in milliseconds, for the provider
// integration histograms: one HTTP attempt is bounded by the client
// timeout (15s by default), while a scheduled retry delay can reach the
// queue's backoff ceiling.
var (
	providerDeliveryBucketsMS = []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 15000, 30000}
	providerRetryBucketsMS    = []float64{100, 500, 1000, 5000, 15000, 60000, 300000, 900000, 3600000}
)

// Metrics registers exactly the P1A cell's catalog instruments
// (telemetry.MetricCatalog) against a metric.Meter and exposes one typed,
// narrow recording method per instrument. It is the only way this package
// lets a caller record a metric: there is no generic "record an arbitrary
// named metric with arbitrary labels" method, so cardinality/label
// governance stays centralized in the Evaluator rather than something each
// call site could invent its own (weaker) version of
// (internal/platform/telemetry/policy.go OBS-004 REFACTOR).
type Metrics struct {
	eval *telemetry.Evaluator

	intentCreated   metric.Int64Counter
	intentSimulated metric.Int64Counter
	ledgerAppend    metric.Int64Counter
	outboxLag       metric.Float64Gauge
	edgeParity      metric.Float64Gauge
	dispatchLatency metric.Float64Histogram

	providerAttempts     metric.Int64Counter
	providerDuration     metric.Float64Histogram
	providerAbandoned    metric.Int64Counter
	providerRetryDelay   metric.Float64Histogram
	providerBreaker      metric.Int64Counter
	providerCallback     metric.Int64Counter
	providerSecretIndex  metric.Int64Counter
	providerTokenRefresh metric.Int64Counter
	providerNearTimeout  metric.Int64Counter

	mu      sync.Mutex
	emitted map[string]struct{}
}

// newMetrics builds every instrument telemetry.MetricCatalog() declares. It
// fails if the catalog names an instrument this switch does not know how to
// construct, or if this switch expects an instrument the catalog no longer
// declares: either direction is the catalog and this adapter drifting apart,
// which OBS-002's "registers exactly the metric catalog's instruments"
// requirement exists to catch at construction time rather than silently at
// the completeness checker, much later.
func newMetrics(meter metric.Meter, eval *telemetry.Evaluator) (*Metrics, error) {
	m := &Metrics{eval: eval, emitted: make(map[string]struct{})}
	seen := make(map[string]bool, len(telemetry.P1ACellMetrics)+len(telemetry.ProviderIntegrationMetrics))
	counter := func(def telemetry.MetricDefinition, dst *metric.Int64Counter) error {
		c, err := meter.Int64Counter(def.Name, metric.WithUnit(def.Unit), metric.WithDescription(def.Description))
		if err != nil {
			return fmt.Errorf("otel: creating counter %q: %w", def.Name, err)
		}
		*dst = c
		return nil
	}
	histogram := func(def telemetry.MetricDefinition, dst *metric.Float64Histogram, bounds []float64) error {
		h, err := meter.Float64Histogram(def.Name,
			metric.WithUnit(def.Unit),
			metric.WithDescription(def.Description),
			metric.WithExplicitBucketBoundaries(bounds...))
		if err != nil {
			return fmt.Errorf("otel: creating histogram %q: %w", def.Name, err)
		}
		*dst = h
		return nil
	}

	for _, def := range telemetry.MetricCatalog() {
		seen[def.Name] = true
		switch def.Name {
		case metricIntentCreated:
			c, err := meter.Int64Counter(def.Name, metric.WithUnit(def.Unit), metric.WithDescription(def.Description))
			if err != nil {
				return nil, fmt.Errorf("otel: creating counter %q: %w", def.Name, err)
			}
			m.intentCreated = c
		case metricIntentSimulated:
			c, err := meter.Int64Counter(def.Name, metric.WithUnit(def.Unit), metric.WithDescription(def.Description))
			if err != nil {
				return nil, fmt.Errorf("otel: creating counter %q: %w", def.Name, err)
			}
			m.intentSimulated = c
		case metricLedgerAppend:
			c, err := meter.Int64Counter(def.Name, metric.WithUnit(def.Unit), metric.WithDescription(def.Description))
			if err != nil {
				return nil, fmt.Errorf("otel: creating counter %q: %w", def.Name, err)
			}
			m.ledgerAppend = c
		case metricOutboxLag:
			g, err := meter.Float64Gauge(def.Name, metric.WithUnit(def.Unit), metric.WithDescription(def.Description))
			if err != nil {
				return nil, fmt.Errorf("otel: creating gauge %q: %w", def.Name, err)
			}
			m.outboxLag = g
		case metricEffectDispatchLatency:
			h, err := meter.Float64Histogram(def.Name,
				metric.WithUnit(def.Unit),
				metric.WithDescription(def.Description),
				metric.WithExplicitBucketBoundaries(0.5, 1, 5, 10, 30, 60, 300, 900, 3600))
			if err != nil {
				return nil, fmt.Errorf("otel: creating histogram %q: %w", def.Name, err)
			}
			m.dispatchLatency = h
		case metricEdgeParity:
			g, err := meter.Float64Gauge(def.Name, metric.WithUnit(def.Unit), metric.WithDescription(def.Description))
			if err != nil {
				return nil, fmt.Errorf("otel: creating gauge %q: %w", def.Name, err)
			}
			m.edgeParity = g
		case metricProviderDeliveryAttempts:
			if err := counter(def, &m.providerAttempts); err != nil {
				return nil, err
			}
		case metricProviderDeliveryDuration:
			if err := histogram(def, &m.providerDuration, providerDeliveryBucketsMS); err != nil {
				return nil, err
			}
		case metricProviderDeliveryAbandoned:
			if err := counter(def, &m.providerAbandoned); err != nil {
				return nil, err
			}
		case metricProviderRetryDelay:
			if err := histogram(def, &m.providerRetryDelay, providerRetryBucketsMS); err != nil {
				return nil, err
			}
		case metricProviderBreakerTransitions:
			if err := counter(def, &m.providerBreaker); err != nil {
				return nil, err
			}
		case metricProviderCallbackReceived:
			if err := counter(def, &m.providerCallback); err != nil {
				return nil, err
			}
		case metricProviderCallbackSecretIndex:
			if err := counter(def, &m.providerSecretIndex); err != nil {
				return nil, err
			}
		case metricProviderTokenRefreshes:
			if err := counter(def, &m.providerTokenRefresh); err != nil {
				return nil, err
			}
		case metricProviderWaitNearTimeout:
			if err := counter(def, &m.providerNearTimeout); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("otel: metric catalog declares %q, which this adapter does not know how to register (update internal/platform/telemetry/otel/metrics.go)", def.Name)
		}
	}
	for _, want := range []string{
		metricIntentCreated, metricIntentSimulated, metricLedgerAppend, metricOutboxLag, metricEdgeParity, metricEffectDispatchLatency,
		metricProviderDeliveryAttempts, metricProviderDeliveryDuration, metricProviderDeliveryAbandoned, metricProviderRetryDelay,
		metricProviderBreakerTransitions, metricProviderCallbackReceived, metricProviderCallbackSecretIndex,
		metricProviderTokenRefreshes, metricProviderWaitNearTimeout,
	} {
		if !seen[want] {
			return nil, fmt.Errorf("otel: metric catalog no longer declares %q, which this adapter expects to register", want)
		}
	}
	return m, nil
}

// metricAttr classifies one metric label through eval and returns the
// attribute to attach plus whether it survived. A label the Evaluator drops
// is simply omitted from the recorded point rather than substituted with a
// placeholder: an omitted label still lets telemetry.Check see the metric
// name as emitted (see EmittedMetricNames), it just carries one fewer
// dimension, exactly like any other Evaluator-denied attribute elsewhere in
// this package.
func metricAttr(eval *telemetry.Evaluator, key, value string) (attribute.KeyValue, bool) {
	d := eval.EvaluateAttribute(telemetry.SignalMetric, key, value)
	if !d.Kept {
		return attribute.KeyValue{}, false
	}
	return attribute.String(key, d.Value), true
}

func (m *Metrics) markEmitted(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emitted[name] = struct{}{}
}

// RecordIntentCreated records one BusinessIntent creation attempt.
func (m *Metrics) RecordIntentCreated(ctx context.Context, cellID, tenantClass, outcome string) {
	attrs := m.filterLabels(
		kv{"cell_id", cellID},
		kv{"tenant_class", tenantClass},
		kv{"outcome", outcome},
	)
	m.intentCreated.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.markEmitted(metricIntentCreated)
}

// RecordIntentSimulated records one BusinessIntent simulation run.
func (m *Metrics) RecordIntentSimulated(ctx context.Context, cellID, tenantClass, outcome string) {
	attrs := m.filterLabels(
		kv{"cell_id", cellID},
		kv{"tenant_class", tenantClass},
		kv{"outcome", outcome},
	)
	m.intentSimulated.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.markEmitted(metricIntentSimulated)
}

// RecordLedgerAppend records one ledger append attempt.
func (m *Metrics) RecordLedgerAppend(ctx context.Context, cellID, outcome string) {
	attrs := m.filterLabels(
		kv{"cell_id", cellID},
		kv{"outcome", outcome},
	)
	m.ledgerAppend.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.markEmitted(metricLedgerAppend)
}

// RecordOutboxLag records the age, in milliseconds, of the oldest
// unpublished outbox entry.
func (m *Metrics) RecordOutboxLag(ctx context.Context, cellID string, ms float64) {
	attrs := m.filterLabels(kv{"cell_id", cellID})
	m.outboxLag.Record(ctx, ms, metric.WithAttributes(attrs...))
	m.markEmitted(metricOutboxLag)
}

// RecordEffectDispatchLatency records one effect dispatch duration in
// milliseconds (telemetry.P1ACellMetrics's own unit for
// effect.dispatch.duration). Record inside the dispatch span's context:
// the SDK attaches that span's trace and span IDs as the observation's
// exemplar exactly when the context carries a sampled span, which is what
// lets a slow dispatch be joined back to its trace (OBS-016).
func (m *Metrics) RecordEffectDispatchLatency(ctx context.Context, cellID, outcome string, ms float64) {
	attrs := m.filterLabels(
		kv{"cell_id", cellID},
		kv{"outcome", outcome},
	)
	m.dispatchLatency.Record(ctx, ms, metric.WithAttributes(attrs...))
	m.markEmitted(metricEffectDispatchLatency)
}

// Exemplar is one sampled effect.dispatch.duration observation carrying
// the trace context it was recorded with: the value and time of the
// observation plus the trace and span IDs that name the dispatch span.
// A zero TraceID marks an observation recorded without a sampled span in
// context, which carries no exemplar and therefore joins to no trace.
type Exemplar struct {
	Value   float64
	Time    time.Time
	TraceID trace.TraceID
	SpanID  trace.SpanID
}

// ExemplarsOf retrieves the trace correlation attached to one collected
// effect.dispatch.duration histogram datapoint: each returned Exemplar
// names the dispatch span one sampled observation was recorded in.
// Unsampled observations carry no exemplar and contribute no entry.
func ExemplarsOf(dp metricdata.HistogramDataPoint[float64]) []Exemplar {
	out := make([]Exemplar, 0, len(dp.Exemplars))
	for _, e := range dp.Exemplars {
		var traceID trace.TraceID
		copy(traceID[:], e.TraceID)
		var spanID trace.SpanID
		copy(spanID[:], e.SpanID)
		out = append(out, Exemplar{
			Value:   e.Value,
			Time:    e.Time,
			TraceID: traceID,
			SpanID:  spanID,
		})
	}
	return out
}

// RecordEdgeParity records 1 when the named replication/consistency edge is
// in parity, 0 otherwise (telemetry.P1ACellMetrics's own description for
// edge.parity).
func (m *Metrics) RecordEdgeParity(ctx context.Context, cellID, edge string, inParity bool) {
	attrs := m.filterLabels(
		kv{"cell_id", cellID},
		kv{"edge", edge},
	)
	value := 0.0
	if inParity {
		value = 1.0
	}
	m.edgeParity.Record(ctx, value, metric.WithAttributes(attrs...))
	m.markEmitted(metricEdgeParity)
}

// RecordProviderDeliveryAttempt records one provider delivery, reversal or
// status attempt. provider, operation and outcomeClass are closed
// vocabularies; a change ref or correlation id is never a label here.
func (m *Metrics) RecordProviderDeliveryAttempt(ctx context.Context, provider, operation, outcomeClass string) {
	attrs := m.filterLabels(
		kv{"provider", provider},
		kv{"provider_operation", operation},
		kv{"outcome_class", outcomeClass},
	)
	m.providerAttempts.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.markEmitted(metricProviderDeliveryAttempts)
}

// RecordProviderDeliveryDuration records the latency, in milliseconds, of
// one provider attempt. Record inside the attempt's span context so a slow
// attempt carries an exemplar pointing at its trace.
func (m *Metrics) RecordProviderDeliveryDuration(ctx context.Context, provider, operation, outcomeClass string, ms float64) {
	attrs := m.filterLabels(
		kv{"provider", provider},
		kv{"provider_operation", operation},
		kv{"outcome_class", outcomeClass},
	)
	m.providerDuration.Record(ctx, ms, metric.WithAttributes(attrs...))
	m.markEmitted(metricProviderDeliveryDuration)
}

// RecordProviderDeliveryAbandoned records one provider change given up on
// after its retry budget, by the last outcome class.
func (m *Metrics) RecordProviderDeliveryAbandoned(ctx context.Context, provider, outcomeClass string) {
	attrs := m.filterLabels(
		kv{"provider", provider},
		kv{"outcome_class", outcomeClass},
	)
	m.providerAbandoned.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.markEmitted(metricProviderDeliveryAbandoned)
}

// RecordProviderRetryDelay records the delay, in milliseconds, scheduled
// before the next provider attempt.
func (m *Metrics) RecordProviderRetryDelay(ctx context.Context, provider string, ms float64) {
	attrs := m.filterLabels(kv{"provider", provider})
	m.providerRetryDelay.Record(ctx, ms, metric.WithAttributes(attrs...))
	m.markEmitted(metricProviderRetryDelay)
}

// RecordProviderBreakerTransition records one circuit-breaker transition
// into toState.
func (m *Metrics) RecordProviderBreakerTransition(ctx context.Context, provider, toState string) {
	attrs := m.filterLabels(
		kv{"provider", provider},
		kv{"to_state", toState},
	)
	m.providerBreaker.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.markEmitted(metricProviderBreakerTransitions)
}

// RecordProviderCallbackReceived records one provider callback, by intake
// result.
func (m *Metrics) RecordProviderCallbackReceived(ctx context.Context, provider, result string) {
	attrs := m.filterLabels(
		kv{"provider", provider},
		kv{"result", result},
	)
	m.providerCallback.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.markEmitted(metricProviderCallbackReceived)
}

// RecordProviderCallbackSecretIndex records which signing secret slot
// (current or previous) verified a provider callback.
func (m *Metrics) RecordProviderCallbackSecretIndex(ctx context.Context, provider, slot string) {
	attrs := m.filterLabels(
		kv{"provider", provider},
		kv{"secret_slot", slot},
	)
	m.providerSecretIndex.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.markEmitted(metricProviderCallbackSecretIndex)
}

// RecordProviderTokenRefresh records one OAuth token refresh, by result.
func (m *Metrics) RecordProviderTokenRefresh(ctx context.Context, result string) {
	attrs := m.filterLabels(kv{"result", result})
	m.providerTokenRefresh.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.markEmitted(metricProviderTokenRefreshes)
}

// RecordProviderWaitNearTimeout records one provider result wait that came
// close to its timeout.
func (m *Metrics) RecordProviderWaitNearTimeout(ctx context.Context, provider string) {
	attrs := m.filterLabels(kv{"provider", provider})
	m.providerNearTimeout.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.markEmitted(metricProviderWaitNearTimeout)
}

// kv is an unexported label-name/value pair; filterLabels is the only
// consumer.
type kv struct {
	key   string
	value string
}

func (m *Metrics) filterLabels(labels ...kv) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(labels))
	for _, l := range labels {
		if l.value == "" {
			continue
		}
		if attr, ok := metricAttr(m.eval, l.key, l.value); ok {
			out = append(out, attr)
		}
	}
	return out
}

// EmittedMetricNames implements telemetry.Sink's metric half: every catalog
// metric name at least one Record* call has been made for, in no particular
// order. It reflects that a *recording call happened*, independent of
// whether every label on that call survived the Evaluator — recording with
// a redacted label set is still telemetry being emitted, not telemetry
// being lost.
func (m *Metrics) EmittedMetricNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.emitted))
	for name := range m.emitted {
		out = append(out, name)
	}
	return out
}

// EmittedLogEventNames implements the other half of telemetry.Sink. This
// package emits no logs (that is internal/platform/logging's lane, and
// OBS-010's event-name registry it would report by does not exist yet), so
// it always reports none. A caller checking full OBS-006 completeness
// composes a telemetry.Sink that reports both halves once that registry
// lands; a metrics-only telemetry.RequiredSignals (omitting
// RequiredLogEvents) is what this package's own tests certify HEALTHY
// against.
func (m *Metrics) EmittedLogEventNames() []string { return nil }
