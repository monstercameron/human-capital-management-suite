package execution

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/buildinfo"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	otelExport "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel/testexport"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/testexport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

// fixedTraceID/fixedSpanID pin the parent span context every test below
// injects into ctx before calling a Start*Span method, so the child span
// OTelInstrumentation opens — and the trace_id it logs — is byte-stable
// rather than a freshly randomized root trace.
var (
	fixedTraceID = trace.TraceID{0x0a, 0xf7, 0x65, 0x19, 0x16, 0xcd, 0x43, 0xdd, 0x84, 0x48, 0xeb, 0x21, 0x1c, 0x80, 0x31, 0x9a}
	fixedSpanID  = trace.SpanID{0xb7, 0xad, 0x6b, 0x71, 0x69, 0x20, 0x33, 0x31}
)

func fixedTraceContext(ctx context.Context) context.Context {
	return trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: fixedTraceID, SpanID: fixedSpanID, TraceFlags: trace.FlagsSampled, Remote: true,
	}))
}

// testTelemetryResource and testTelemetryEvaluator build the same minimal,
// valid internal/platform/telemetry.Resource/Evaluator
// internal/platform/telemetry/otel's own test suite does (see that
// package's helpers_test.go): this package's tests cannot import those
// unexported test helpers across a package boundary, so this is a narrow,
// deliberate duplication of the same recipe.
func testTelemetryResource(t *testing.T) telemetry.Resource {
	t.Helper()
	res := telemetry.NewResourceFromBuild(
		buildinfo.Info{Revision: "abc123"},
		"hcm-execution-test", "instance-1", "test", "cell-p1a", "us-east-1",
		telemetry.ProcessRoleAPI, telemetry.TenantClassStandard,
	)
	if err := res.Validate(); err != nil {
		t.Fatalf("test resource does not validate: %v", err)
	}
	return res
}

func testTelemetryEvaluator(t *testing.T) *telemetry.Evaluator {
	t.Helper()
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatalf("DefaultAllowlist: %v", err)
	}
	return telemetry.NewEvaluator(allow, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy())
}

// newTestInstrumentation builds an OTelInstrumentation wired to a real
// internal/platform/telemetry/otel.Provider over OBS-015's in-memory
// exporters: internal/platform/telemetry/otel/testexport.SpanRecorder for
// spans (a SimpleSpanProcessor lands them the instant a span ends, no
// ForceFlush needed) and internal/platform/telemetry/testexport.LogRecorder
// for the structured log line.
func newTestInstrumentation(t *testing.T, clock func() time.Time) (*OTelInstrumentation, *hcmotel.Provider, *otelExport.SpanRecorder, *testexport.LogRecorder) {
	t.Helper()
	spanRec := otelExport.NewSpanRecorder()
	provider, err := hcmotel.NewProvider(context.Background(), hcmotel.Config{
		Resource:        testTelemetryResource(t),
		Evaluator:       testTelemetryEvaluator(t),
		ShutdownTimeout: 5 * time.Second,
		// A BatchSpanProcessor (this Provider's own default) buffers spans
		// until ForceFlush or its own timeout; a test calls
		// provider.ForceFlush(ctx) after End() to read spanRec
		// deterministically rather than waiting on that timeout.
		Trace:  hcmotel.TraceConfig{Exporter: spanRec},
		Metric: hcmotel.MetricConfig{Reader: sdkmetric.NewManualReader()},
	})
	if err != nil {
		t.Fatalf("hcmotel.NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	logRec := testexport.NewLogRecorder()
	logger := slog.New(logging.NewHandler(logRec, logging.WithService("test-workflow-execute"), logging.WithClock(clock)))

	return NewOTelInstrumentation(provider, logger, clock), provider, spanRec, logRec
}

func TestTodo_OBS_013_RealExporterAdvanceSpanProducesExactDurableLink(t *testing.T) {
	start := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	inst, provider, recorder, _ := newTestInstrumentation(t, func() time.Time { return start })
	_, span := inst.StartAdvanceSpan(fixedTraceContext(context.Background()), execute.SpanAttributes{InstanceID: "instance-approval", NodeID: "approval", Attempt: 2})
	causalSpan, ok := span.(execute.CausalSpan)
	if !ok {
		t.Fatal("real exporter advancement span does not implement durable causal metadata")
	}
	expires := start.Add(24 * time.Hour)
	got := causalSpan.CausalMetadata(execute.CausalIdentity{CorrelationID: "corr", CausationID: "cause", LogicalOperationID: "logical", AttemptID: "source-attempt", ExpiresAt: expires})
	if got == nil || got.CorrelationID != "corr" || got.CausationID != "cause" || got.LogicalOperationID != "logical" || got.AttemptID != "source-attempt" || got.TraceLink == nil || got.TraceLink.TraceID != fixedTraceID.String() || !got.TraceLink.ExpiresAt.Equal(expires) {
		t.Fatalf("durable causal metadata = %#v", got)
	}
	span.End(execute.OutcomeSuccess, nil)
	if report := provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}
	if len(recorder.Spans()) != 1 {
		t.Fatalf("exported spans = %d, want 1", len(recorder.Spans()))
	}
}

// TestTodo_OBS_023_Golden proves the OTel-backed Instrumentation's span and
// log line are byte-stable for a fixed clock and a fixed ambient trace
// context: the exact envelope fields structured-logging-and-opentelemetry.md
// requires (schema_version, level, service, message, plus the bounded
// instance_id/node_id/attempt/terminal_code/outcome/duration_ms/trace_id
// attributes) never drift silently.
func TestTodo_OBS_023_Golden(t *testing.T) {
	start := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	now := start
	clock := func() time.Time { return now }

	inst, provider, spanRec, logRec := newTestInstrumentation(t, clock)
	ctx := fixedTraceContext(context.Background())

	spanCtx, span := inst.StartAdvanceSpan(ctx, execute.SpanAttributes{
		InstanceID: "instance-golden-1", NodeID: "node-golden-1", Attempt: 1,
	})
	if got := trace.SpanContextFromContext(spanCtx).TraceID(); got != fixedTraceID {
		t.Fatalf("child span trace id = %s, want the fixed parent trace id %s", got, fixedTraceID)
	}
	now = start.Add(125 * time.Millisecond)
	span.End(execute.OutcomeSuccess, nil)
	if report := provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}

	// --- span assertions ---
	//
	// This Provider applies OBS-001/004's frozen attribute allow-list to
	// every span attribute. Workflow execution uses the canonical topology
	// vocabulary so an operator can pivot from a span to the durable
	// workflow instance, node and attempt without relying on log-only data.
	recorded, ok := spanRec.SpanNamed(spanWorkflowAdvance)
	if !ok {
		t.Fatalf("no span named %q recorded", spanWorkflowAdvance)
	}
	attrs := map[string]string{}
	for _, kv := range recorded.Attributes() {
		attrs[string(kv.Key)] = kv.Value.String()
	}
	if got := attrs["outcome"]; got != "SUCCESS" {
		t.Errorf("span attribute outcome = %q, want %q (all: %v)", got, "SUCCESS", attrs)
	}
	wantSpanFields := map[string]string{
		"logical_operation_id": "instance-golden-1",
		"node_id":              "node-golden-1",
		"attempt_id":           "1",
	}
	for key, want := range wantSpanFields {
		if got := attrs[key]; got != want {
			t.Errorf("span attribute %s = %q, want %q (all: %v)", key, got, want, attrs)
		}
	}
	if recorded.Status().Code != codes.Ok {
		t.Errorf("span status = %v, want Ok", recorded.Status())
	}

	// --- log line assertions ---
	lines := logRec.LinesNamed(logEventAdvance)
	if len(lines) != 1 {
		t.Fatalf("log lines named %q = %d, want exactly 1", logEventAdvance, len(lines))
	}
	line := lines[0]
	if v, _ := line["schema_version"].(float64); v != float64(logging.SchemaVersion) {
		t.Errorf("schema_version = %v, want %d", line["schema_version"], logging.SchemaVersion)
	}
	if v, _ := line["level"].(string); v != "INFO" {
		t.Errorf("level = %q, want INFO", v)
	}
	if v, _ := line["message"].(string); v != logEventAdvance {
		t.Errorf("message = %q, want %q", v, logEventAdvance)
	}
	if v, _ := line["timestamp"].(string); v != now.UTC().Format(time.RFC3339) {
		// now was advanced above; the record's own clock read happens at
		// End(), which reuses the same now.
		t.Errorf("timestamp = %q, want %q", v, start.Add(125*time.Millisecond).UTC().Format(time.RFC3339))
	}
	fields, ok := line["attrs"].(map[string]any)
	if !ok {
		t.Fatalf("log line carries no attrs object: %v", line)
	}
	wantFields := map[string]any{
		"instance_id": "instance-golden-1", "node_id": "node-golden-1",
		"attempt": float64(1), "terminal_code": "", "outcome": "SUCCESS",
		"duration_ms": float64(125), "trace_id": fixedTraceID.String(),
	}
	for k, want := range wantFields {
		if got := fields[k]; got != want {
			t.Errorf("log attrs[%s] = %#v, want %#v (all: %v)", k, got, want, fields)
		}
	}
}

func TestTodo_OBS_023_TerminalSpanKeepsTheInspectorPivot(t *testing.T) {
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	inst, provider, recorder, _ := newTestInstrumentation(t, func() time.Time { return at })
	_, span := inst.StartTerminalSpan(fixedTraceContext(context.Background()), execute.SpanAttributes{
		InstanceID:   "instance-terminal-1",
		NodeID:       "end",
		Attempt:      4,
		TerminalCode: "APPROVED",
	})
	span.End(execute.OutcomeSuccess, nil)
	if report := provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}
	recorded, ok := recorder.SpanNamed(spanWorkflowTerminal)
	if !ok {
		t.Fatalf("no span named %q recorded", spanWorkflowTerminal)
	}
	attrs := map[string]string{}
	for _, kv := range recorded.Attributes() {
		attrs[string(kv.Key)] = kv.Value.String()
	}
	want := map[string]string{
		"logical_operation_id": "instance-terminal-1",
		"node_id":              "end",
		"attempt_id":           "4",
		"terminal_code":        "APPROVED",
	}
	for key, value := range want {
		if attrs[key] != value {
			t.Errorf("%s = %q, want %q (all: %v)", key, attrs[key], value, attrs)
		}
	}
}
