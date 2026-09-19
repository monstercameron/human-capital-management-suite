package telemetry_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

// TestProviderIntegrationRegistryKeys pins the attribute keys the provider
// hand-off instrumentation relies on: the bounded vocabularies are
// metric-safe, and every identifier is span/log only so it can never become
// a metric label.
func TestProviderIntegrationRegistryKeys(t *testing.T) {
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatalf("DefaultAllowlist: %v", err)
	}
	for _, key := range []string{"provider", "provider_operation", "outcome_class", "result", "to_state", "secret_slot"} {
		def, ok := allow.Lookup(key)
		if !ok || def.MaxCardinality <= 0 || !allow.AllowsSignal(key, telemetry.SignalMetric) {
			t.Errorf("key %q must be a bounded metric label, got %+v (ok=%v)", key, def, ok)
		}
	}
	if !allow.AllowsSignal("attempt_id", telemetry.SignalSpan) || allow.AllowsSignal("attempt_id", telemetry.SignalLog) {
		t.Error("attempt_id must stay span-only (OBS-013); logs carry the attempt number as attempt")
	}
	if !allow.AllowsSignal("attempt", telemetry.SignalLog) || allow.AllowsSignal("attempt", telemetry.SignalMetric) {
		t.Error("attempt must be a log key and never a metric label")
	}
	for _, key := range []string{"change_ref", "event_id", "correlation_id", "secret_index", "retry_delay_ms", "breaker_state", "from_state", "error_type"} {
		if !allow.AllowsSignal(key, telemetry.SignalSpan) || !allow.AllowsSignal(key, telemetry.SignalLog) {
			t.Errorf("key %q must be usable on spans and logs", key)
		}
		if allow.AllowsSignal(key, telemetry.SignalMetric) {
			t.Errorf("key %q must never be a metric label", key)
		}
	}
	for _, key := range []string{"tenant", "trace_id", "intent_id", "token", "secret"} {
		if _, ok := allow.Lookup(key); ok {
			t.Errorf("key %q must not be registered", key)
		}
	}
}

// TestProviderIntegrationMetricsCatalog proves the provider metrics are in
// the live catalog, validate against the allowlist, never carry a
// high-cardinality or identity label, and are not required for
// completeness (they are event-driven).
func TestProviderIntegrationMetricsCatalog(t *testing.T) {
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatalf("DefaultAllowlist: %v", err)
	}
	if err := telemetry.ValidateCatalog(telemetry.ProviderIntegrationMetrics, allow); err != nil {
		t.Fatalf("ValidateCatalog(provider metrics) = %v", err)
	}
	catalog := telemetry.CatalogByName(telemetry.MetricCatalog())
	want := map[string]telemetry.MetricType{
		"provider.delivery.attempts":     telemetry.MetricCounter,
		"provider.delivery.duration":     telemetry.MetricHistogram,
		"provider.delivery.abandoned":    telemetry.MetricCounter,
		"provider.retry.delay":           telemetry.MetricHistogram,
		"provider.breaker.transitions":   telemetry.MetricCounter,
		"provider.callback.received":     telemetry.MetricCounter,
		"provider.callback.secret_index": telemetry.MetricCounter,
		"provider.token.refreshes":       telemetry.MetricCounter,
		"provider.wait.near_timeout":     telemetry.MetricCounter,
	}
	if len(telemetry.ProviderIntegrationMetrics) != len(want) {
		t.Fatalf("provider metrics = %d, want %d", len(telemetry.ProviderIntegrationMetrics), len(want))
	}
	for name, typ := range want {
		m, ok := catalog[name]
		if !ok {
			t.Fatalf("metric %q missing from MetricCatalog()", name)
		}
		if m.Type != typ {
			t.Errorf("metric %q type = %q, want %q", name, m.Type, typ)
		}
		for _, l := range m.Labels {
			switch l {
			case "correlation_id", "intent_id", "trace_id", "tenant", "change_ref", "event_id":
				t.Errorf("metric %q carries forbidden label %q", name, l)
			}
		}
	}
	for _, name := range telemetry.DefaultRequiredSignals().RequiredMetrics {
		if strings.HasPrefix(name, "provider.") {
			t.Errorf("event-driven metric %q must not be required for completeness", name)
		}
	}
}

// TestProviderCallTopologyAcceptsProviderAttributes proves the reused
// hcmnext.provider.call span registers the provider attributes and still
// refuses an unregistered one.
func TestProviderCallTopologyAcceptsProviderAttributes(t *testing.T) {
	attrs := map[string]string{
		"provider": "payroll", "provider_operation": "deliver", "outcome_class": "delivered",
		"change_ref": "payroll:0b8f", "attempt_id": "2", "correlation_id": "corr-1",
		"secret_index": "1", "retry_delay_ms": "250", "breaker_state": "open",
	}
	if err := telemetry.ValidateSpanAttributes(telemetry.SpanProviderCall, attrs); err != nil {
		t.Fatalf("ValidateSpanAttributes = %v", err)
	}
	if err := telemetry.CanonicalPromotionRegistry().ValidateEmission(string(telemetry.SpanProviderCall), attrs); err != nil {
		t.Fatalf("ValidateEmission = %v", err)
	}
	if err := telemetry.ValidateSpanAttributes(telemetry.SpanProviderCall, map[string]string{"tenant": "acme"}); !errors.Is(err, telemetry.ErrDynamicSpanAttribute) {
		t.Fatalf("unregistered attribute accepted: %v", err)
	}
	row, ok := telemetry.CanonicalPromotionRegistry().Lookup(telemetry.SpanProviderCall)
	if !ok || row.Parent != telemetry.SpanConnectorDispatch {
		t.Fatalf("provider.call row = %+v, %v; want parent %q", row, ok, telemetry.SpanConnectorDispatch)
	}
}
