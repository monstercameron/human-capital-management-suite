package otel_test

import (
	"context"
	"sort"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

// metricPointAttrs returns the attribute sets of every datapoint recorded
// for name, whatever the aggregation.
func metricPointAttrs(t *testing.T, rm metricdata.ResourceMetrics, name string) []attribute.Set {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			var out []attribute.Set
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					out = append(out, dp.Attributes)
				}
			case metricdata.Histogram[float64]:
				for _, dp := range data.DataPoints {
					out = append(out, dp.Attributes)
				}
			default:
				t.Fatalf("metric %q has unexpected data %T", name, m.Data)
			}
			return out
		}
	}
	t.Fatalf("metric %q not collected", name)
	return nil
}

// TestProviderIntegrationMetricsRecordAndStayLowCardinality records every
// provider integration instrument once and proves each lands under its
// catalog name with exactly its declared labels, and that the provider
// metrics are catalog members the adapter registered at construction.
func TestProviderIntegrationMetricsRecordAndStayLowCardinality(t *testing.T) {
	h := newTestHarness(t, testEvaluator(t))
	m := h.Provider.Metrics()
	ctx := context.Background()

	m.RecordProviderDeliveryAttempt(ctx, "payroll", "deliver", "delivered")
	m.RecordProviderDeliveryDuration(ctx, "payroll", "deliver", "delivered", 42)
	m.RecordProviderDeliveryAbandoned(ctx, "iam", "transient_status")
	m.RecordProviderRetryDelay(ctx, "iam", 1500)
	m.RecordProviderBreakerTransition(ctx, "payroll", "open")
	m.RecordProviderCallbackReceived(ctx, "payroll", "accepted")
	m.RecordProviderCallbackSecretIndex(ctx, "payroll", "previous")
	m.RecordProviderTokenRefresh(ctx, "success")
	m.RecordProviderWaitNearTimeout(ctx, "iam")

	rm := collectMetrics(t, h.MetricReader)
	catalog := telemetry.CatalogByName(telemetry.ProviderIntegrationMetrics)
	for name, def := range catalog {
		points := metricPointAttrs(t, rm, name)
		if len(points) != 1 {
			t.Fatalf("metric %q has %d datapoints, want 1", name, len(points))
		}
		var keys []string
		for _, kv := range points[0].ToSlice() {
			keys = append(keys, string(kv.Key))
		}
		want := append([]string(nil), def.Labels...)
		sort.Strings(keys)
		sort.Strings(want)
		if len(keys) != len(want) {
			t.Fatalf("metric %q labels = %v, want %v", name, keys, want)
		}
		for i := range want {
			if keys[i] != want[i] {
				t.Fatalf("metric %q labels = %v, want %v", name, keys, want)
			}
		}
	}

	attempts := metricPointAttrs(t, rm, "provider.delivery.attempts")[0]
	if v, ok := attempts.Value("outcome_class"); !ok || v.AsString() != "delivered" {
		t.Fatalf("attempts outcome_class = %v, %v", v, ok)
	}
	secret := metricPointAttrs(t, rm, "provider.callback.secret_index")[0]
	if v, ok := secret.Value("secret_slot"); !ok || v.AsString() != "previous" {
		t.Fatalf("secret_index secret_slot = %v, %v", v, ok)
	}

	emitted := map[string]bool{}
	for _, name := range m.EmittedMetricNames() {
		emitted[name] = true
	}
	for name := range catalog {
		if !emitted[name] {
			t.Errorf("EmittedMetricNames() omits %q after it was recorded", name)
		}
	}
}

// TestProviderIntegrationMetricsCardinalityCapped proves the Evaluator's
// cardinality budget still governs provider labels: flooding the provider
// label with distinct values collapses the overflow into the shared
// bucket instead of minting one series per value.
func TestProviderIntegrationMetricsCardinalityCapped(t *testing.T) {
	h := newTestHarness(t, testEvaluator(t))
	m := h.Provider.Metrics()
	ctx := context.Background()
	for i := 0; i < 64; i++ {
		m.RecordProviderWaitNearTimeout(ctx, "provider-"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	points := metricPointAttrs(t, collectMetrics(t, h.MetricReader), "provider.wait.near_timeout")
	if len(points) > 9 {
		t.Fatalf("provider label produced %d series, want at most max_cardinality (8) plus the overflow bucket", len(points))
	}
}
