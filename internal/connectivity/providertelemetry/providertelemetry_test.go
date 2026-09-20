package providertelemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerdelivery"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/buildinfo"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
)

// harness is one Recorder over a real hcmotel.Provider wired to an
// in-memory span exporter, a manual metric reader and a logging.Handler
// writing to a buffer.
type harness struct {
	rec      *Recorder
	provider *hcmotel.Provider
	spans    *tracetest.InMemoryExporter
	reader   *sdkmetric.ManualReader
	logs     *bytes.Buffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	eval := telemetry.NewEvaluator(allow, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy())
	res := telemetry.NewResourceFromBuild(buildinfo.Info{Revision: "abc123"}, "hcm-provider-test", "instance-1", "test", "cell-p1a", "us-east-1",
		telemetry.ProcessRoleAPI, telemetry.TenantClassStandard)
	spans := tracetest.NewInMemoryExporter()
	reader := sdkmetric.NewManualReader()
	p, err := hcmotel.NewProvider(context.Background(), hcmotel.Config{
		Resource: res, Evaluator: eval, ShutdownTimeout: 5 * time.Second,
		Trace:  hcmotel.TraceConfig{Exporter: spans},
		Metric: hcmotel.MetricConfig{Reader: reader},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Shutdown(context.Background()) })
	var buf bytes.Buffer
	logger := slog.New(logging.NewHandler(&buf, logging.WithService("hcm-test")))
	return &harness{rec: New(p, logger), provider: p, spans: spans, reader: reader, logs: &buf}
}

func (h *harness) flushSpans(t *testing.T) tracetest.SpanStubs {
	t.Helper()
	if rep := h.provider.ForceFlush(context.Background()); rep.Err() != nil {
		t.Fatalf("ForceFlush: %v", rep.Err())
	}
	return h.spans.GetSpans()
}

// points returns name's datapoint attribute sets and (for counters) values.
func (h *harness) points(t *testing.T, name string) ([]attribute.Set, []int64) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			var sets []attribute.Set
			var vals []int64
			switch d := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range d.DataPoints {
					sets, vals = append(sets, dp.Attributes), append(vals, dp.Value)
				}
			case metricdata.Histogram[float64]:
				for _, dp := range d.DataPoints {
					sets, vals = append(sets, dp.Attributes), append(vals, int64(dp.Count))
				}
			}
			return sets, vals
		}
	}
	return nil, nil
}

type logLine struct {
	Level         string         `json:"level"`
	Message       string         `json:"message"`
	RequestID     string         `json:"request_id"`
	CorrelationID string         `json:"correlation_id"`
	Attrs         map[string]any `json:"attrs"`
}

func (h *harness) lines(t *testing.T) []logLine {
	t.Helper()
	var out []logLine
	for _, raw := range strings.Split(strings.TrimSpace(h.logs.String()), "\n") {
		if raw == "" {
			continue
		}
		var l logLine
		if err := json.Unmarshal([]byte(raw), &l); err != nil {
			t.Fatalf("log line %q: %v", raw, err)
		}
		out = append(out, l)
	}
	return out
}

func attr(attrs []attribute.KeyValue, key string) (string, bool) {
	for _, kv := range attrs {
		if string(kv.Key) == key {
			return kv.Value.Emit(), true
		}
	}
	return "", false
}

func ctxWithIDs() context.Context {
	ctx := logging.WithCorrelationID(context.Background(), "corr-42")
	return logging.WithRequestID(ctx, "req-7")
}

func TestNilRecorderAndNilProviderAreNoops(t *testing.T) {
	for name, rec := range map[string]*Recorder{"nil": nil, "zero": {}, "nil provider": New(nil, nil)} {
		t.Run(name, func(t *testing.T) {
			ctx, span := rec.StartDelivery(context.Background(), ProviderPayroll, OpDeliver, "payroll:1", 1)
			if ctx == nil || span == nil {
				t.Fatal("StartDelivery returned nil")
			}
			span.End(providerdelivery.Result{Outcome: providerdelivery.Delivered, Class: providerdelivery.ClassDelivered}, nil)
			rec.RetryScheduled(ctx, ProviderPayroll, "payroll:1", 1, time.Second)
			rec.Abandoned(ctx, ProviderPayroll, "payroll:1", providerdelivery.ClassTransientStatus)
			rec.BreakerTransition(ctx, ProviderPayroll, BreakerClosed, BreakerOpen)
			rec.CallbackReceived(ctx, ProviderPayroll, CallbackAccepted, 0, "evt", "payroll:1")
			rec.TokenRefresh(ctx, TokenSuccess)
			rec.WaitNearTimeout(ctx, ProviderIAM, "iam:1")
			cctx, end := rec.StartCallback(ctx, ProviderIAM)
			end()
			if trace.SpanContextFromContext(cctx).IsValid() {
				t.Fatal("a no-op recorder minted a valid span context")
			}
			if h := OutboundHeaders(ctx); h.Get("traceparent") != "" {
				t.Fatalf("no-op span produced traceparent %q", h.Get("traceparent"))
			}
		})
	}
	var s *Span
	s.End(providerdelivery.Result{}, nil) // must not panic
}

func TestDeliveredAttemptRecordsSpanMetricsAndInfoLog(t *testing.T) {
	h := newHarness(t)
	ctx, span := h.rec.StartDelivery(ctxWithIDs(), ProviderPayroll, OpDeliver, "payroll:0b8f", 2)
	if hdr := OutboundHeaders(ctx); hdr.Get("traceparent") == "" {
		t.Fatal("OutboundHeaders inside a delivery span has no traceparent")
	}
	span.End(providerdelivery.Result{Outcome: providerdelivery.Delivered, Class: providerdelivery.ClassDelivered, Status: 202}, nil)
	span.End(providerdelivery.Result{Outcome: providerdelivery.Rejected, Class: providerdelivery.ClassRejectedAuth}, nil) // ignored

	stubs := h.flushSpans(t)
	if len(stubs) != 1 {
		t.Fatalf("exported %d spans, want 1", len(stubs))
	}
	sp := stubs[0]
	if sp.Name != string(telemetry.SpanProviderCall) || sp.SpanKind != trace.SpanKindClient {
		t.Fatalf("span = %q kind %v", sp.Name, sp.SpanKind)
	}
	for key, want := range map[string]string{
		"provider": "payroll", "provider_operation": "deliver", "change_ref": "payroll:0b8f",
		"attempt_id": "2", "outcome_class": "delivered", "status": "2xx", "outcome": "SUCCESS",
		"correlation_id": "corr-42",
	} {
		if got, ok := attr(sp.Attributes, key); !ok || got != want {
			t.Errorf("span %s = %q (present %v), want %q", key, got, ok, want)
		}
	}
	if _, ok := attr(sp.Attributes, "request_id"); ok {
		t.Error("request_id reached a span; it is not a registered span attribute")
	}
	if sp.Status.Code != codes.Ok {
		t.Errorf("span status = %v, want Ok", sp.Status)
	}

	sets, vals := h.points(t, "provider.delivery.attempts")
	if len(sets) != 1 || vals[0] != 1 {
		t.Fatalf("attempts points = %v %v, want one point of 1 (End is idempotent)", sets, vals)
	}
	for _, kv := range sets[0].ToSlice() {
		switch string(kv.Key) {
		case "provider", "provider_operation", "outcome_class":
		default:
			t.Errorf("attempts carries unexpected label %q", kv.Key)
		}
	}
	if sets, _ := h.points(t, "provider.delivery.duration"); len(sets) != 1 {
		t.Fatalf("duration points = %v", sets)
	}

	lines := h.lines(t)
	if len(lines) != 1 {
		t.Fatalf("log lines = %d, want 1", len(lines))
	}
	l := lines[0]
	if l.Level != "INFO" || l.CorrelationID != "corr-42" || l.RequestID != "req-7" {
		t.Fatalf("log line = %+v", l)
	}
	if l.Attrs["change_ref"] != "payroll:0b8f" || l.Attrs["outcome_class"] != "delivered" || l.Attrs["attempt"] != float64(2) {
		t.Fatalf("log attrs = %v", l.Attrs)
	}
}

func TestAttemptLevelsAndErrorStatus(t *testing.T) {
	for _, tc := range []struct {
		name      string
		result    providerdelivery.Result
		err       error
		level     string
		class     string
		wantError bool
	}{
		{"retry", providerdelivery.Result{Outcome: providerdelivery.Retry, Class: providerdelivery.ClassTransientTimeout}, nil, "WARN", "transient_timeout", true},
		{"rejected", providerdelivery.Result{Outcome: providerdelivery.Rejected, Class: providerdelivery.ClassRejectedBusiness, Status: 422}, nil, "ERROR", "rejected_business", true},
		{"invalid input", providerdelivery.Result{}, errors.Join(providerdelivery.ErrInvalidPayload, errors.New("payload tenant is missing")), "ERROR", ClassInvalidInput, true},
		{"unknown class", providerdelivery.Result{Outcome: providerdelivery.Delivered, Class: "brand-new-class"}, nil, "INFO", ClassUnknown, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			_, span := h.rec.StartDelivery(context.Background(), ProviderIAM, OpReverse, "iam:1", 1)
			span.End(tc.result, tc.err)
			sp := h.flushSpans(t)[0]
			if got, _ := attr(sp.Attributes, "outcome_class"); got != tc.class {
				t.Fatalf("outcome_class = %q, want %q", got, tc.class)
			}
			if (sp.Status.Code == codes.Error) != tc.wantError {
				t.Fatalf("span status = %v, wantError %v", sp.Status, tc.wantError)
			}
			if tc.wantError {
				if got, _ := attr(sp.Attributes, "error_type"); got != tc.class {
					t.Fatalf("error_type = %q, want %q", got, tc.class)
				}
			}
			if l := h.lines(t)[0]; l.Level != tc.level {
				t.Fatalf("log level = %q, want %q", l.Level, tc.level)
			}
		})
	}
}

func TestEventsRecordBoundedMetricsAndLogs(t *testing.T) {
	h := newHarness(t)
	ctx, span := h.rec.StartDelivery(ctxWithIDs(), ProviderPayroll, OpDeliver, "payroll:1", 3)
	h.rec.RetryScheduled(ctx, ProviderPayroll, "payroll:1", 3, 1500*time.Millisecond)
	h.rec.Abandoned(ctx, "acme-payroll-vendor", "payroll:1", "made-up-cause")
	h.rec.BreakerTransition(ctx, ProviderPayroll, BreakerClosed, BreakerOpen)
	h.rec.TokenRefresh(ctx, "exploded")
	h.rec.WaitNearTimeout(ctx, ProviderIAM, "iam:9")
	span.End(providerdelivery.Result{Outcome: providerdelivery.Retry, Class: providerdelivery.ClassTransientStatus, Status: 503}, nil)

	check := func(metric string, want map[string]string) {
		t.Helper()
		sets, _ := h.points(t, metric)
		if len(sets) != 1 {
			t.Fatalf("%s points = %v, want 1", metric, sets)
		}
		if sets[0].Len() != len(want) {
			t.Fatalf("%s labels = %v, want %v", metric, sets[0].ToSlice(), want)
		}
		for k, v := range want {
			if got, ok := sets[0].Value(attribute.Key(k)); !ok || got.AsString() != v {
				t.Fatalf("%s %s = %v, want %q", metric, k, got, v)
			}
		}
	}
	check("provider.retry.delay", map[string]string{"provider": "payroll"})
	check("provider.delivery.abandoned", map[string]string{"provider": "other", "outcome_class": "unknown"})
	check("provider.breaker.transitions", map[string]string{"provider": "payroll", "to_state": "open"})
	check("provider.token.refreshes", map[string]string{"result": "failure"})
	check("provider.wait.near_timeout", map[string]string{"provider": "iam"})

	sp := h.flushSpans(t)[0]
	events := map[string]bool{}
	for _, e := range sp.Events {
		events[e.Name] = true
		if e.Name == "provider.retry.scheduled" {
			if got, _ := attr(e.Attributes, "retry_delay_ms"); got != "1500" {
				t.Errorf("retry_delay_ms = %q", got)
			}
		}
	}
	for _, name := range []string{"provider.retry.scheduled", "provider.delivery.abandoned", "provider.breaker.transition", "provider.token.refresh", "provider.wait.near_timeout"} {
		if !events[name] {
			t.Errorf("span event %q missing (have %v)", name, events)
		}
	}

	levels := map[string]string{}
	for _, l := range h.lines(t) {
		levels[l.Message] = l.Level
		if l.CorrelationID != "corr-42" || l.RequestID != "req-7" {
			t.Errorf("log %q lost context ids: %+v", l.Message, l)
		}
	}
	for msg, want := range map[string]string{
		"provider retry scheduled":          "WARN",
		"provider delivery abandoned":       "ERROR",
		"provider breaker transition":       "WARN",
		"provider token refresh":            "WARN",
		"provider result wait near timeout": "WARN",
	} {
		if levels[msg] != want {
			t.Errorf("log %q level = %q, want %q", msg, levels[msg], want)
		}
	}
}

func TestCallbackReceivedRecordsResultAndSecretSlot(t *testing.T) {
	h := newHarness(t)
	ctx, end := h.rec.StartCallback(ctxWithIDs(), ProviderPayroll)
	h.rec.CallbackReceived(ctx, ProviderPayroll, CallbackAccepted, 1, "evt_1", "payroll:1")
	end()
	ctx, end = h.rec.StartCallback(context.Background(), ProviderIAM)
	h.rec.CallbackReceived(ctx, ProviderIAM, CallbackRejectedSignature, -1, "evt_2", "")
	end()

	sets, _ := h.points(t, "provider.callback.received")
	if len(sets) != 2 {
		t.Fatalf("callback.received points = %d, want 2", len(sets))
	}
	secret, vals := h.points(t, "provider.callback.secret_index")
	if len(secret) != 1 || vals[0] != 1 {
		t.Fatalf("secret_index points = %v %v, want one (unverified callbacks are not counted)", secret, vals)
	}
	if v, _ := secret[0].Value("secret_slot"); v.AsString() != SecretPrevious {
		t.Fatalf("secret_slot = %v, want previous", v)
	}

	stubs := h.flushSpans(t)
	if len(stubs) != 2 {
		t.Fatalf("spans = %d, want 2", len(stubs))
	}
	accepted, rejected := stubs[0], stubs[1]
	if accepted.SpanKind != trace.SpanKindServer || accepted.Status.Code != codes.Ok {
		t.Fatalf("accepted callback span kind %v status %v", accepted.SpanKind, accepted.Status)
	}
	for key, want := range map[string]string{"result": "accepted", "secret_index": "1", "event_id": "evt_1", "change_ref": "payroll:1", "provider_operation": "callback"} {
		if got, ok := attr(accepted.Attributes, key); !ok || got != want {
			t.Errorf("callback span %s = %q (%v), want %q", key, got, ok, want)
		}
	}
	if rejected.Status.Code != codes.Error {
		t.Fatalf("rejected callback span status = %v", rejected.Status)
	}
	lines := h.lines(t)
	if lines[0].Level != "INFO" || lines[1].Level != "WARN" || lines[0].CorrelationID != "corr-42" {
		t.Fatalf("callback log lines = %+v", lines)
	}
}

func TestLogsNeverCarrySecretsOrBodies(t *testing.T) {
	h := newHarness(t)
	const secret = "sk_live_TOPSECRET"
	_, span := h.rec.StartDelivery(ctxWithIDs(), ProviderPayroll, OpDeliver, "payroll:1", 1)
	span.End(providerdelivery.Result{Outcome: providerdelivery.Rejected, Class: providerdelivery.ClassRejectedAuth, Status: 401,
		Reason: "invalid api key " + secret, ProviderRef: "body:" + secret}, errors.New("request carried "+secret))
	h.rec.TokenRefresh(context.Background(), TokenRejected)
	out := h.logs.String()
	if strings.Contains(out, secret) {
		t.Fatalf("log output leaked a secret: %s", out)
	}
	for _, sp := range h.flushSpans(t) {
		for _, kv := range sp.Attributes {
			if strings.Contains(kv.Value.Emit(), secret) {
				t.Fatalf("span attribute %s leaked a secret", kv.Key)
			}
		}
		if strings.Contains(sp.Status.Description, secret) {
			t.Fatal("span status leaked a secret")
		}
	}
}

func okResult() providerdelivery.Result {
	return providerdelivery.Result{Outcome: providerdelivery.Delivered, Class: providerdelivery.ClassDelivered, Status: 200}
}
