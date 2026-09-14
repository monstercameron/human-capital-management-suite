package execution

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Span names OBS-023 opens. These match the OBS-012 topology contract
// (internal/platform/telemetry.CanonicalPromotionTopology): the advance
// and terminal names below equal their contract rows, and the resume span
// uses telemetry.SpanTimerResume directly so the published name has one
// source of truth.
const (
	spanWorkflowNode     = "hcmnext.workflow.node"
	spanWorkflowAdvance  = "hcmnext.workflow.advance"
	spanWorkflowTerminal = "hcmnext.workflow.terminal"

	// logEventAdvance and logEventTerminal are the structured-log
	// envelope's event names (message) for the one required line per
	// advancement and per terminal write.
	logEventAdvance  = "workflow.advance"
	logEventTerminal = "workflow.terminal.write"
)

// tracerName identifies this package's own instrumentation scope, per
// trace.TracerProvider's documented convention (an importable code path,
// not a human label).
const tracerName = "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"

// OTelInstrumentation is OBS-023's production [execute.Instrumentation]: it
// opens real spans through internal/platform/telemetry/otel.Provider —
// already filtered through the frozen attribute allow-list, so this package
// never has to re-implement OBS-004's redaction policy — and emits one
// log/slog envelope line per advancement and terminal write through a
// caller-supplied *slog.Logger (internal/platform/logging.NewHandler in
// production; internal/platform/telemetry/testexport.LogRecorder in a
// test).
//
// It deliberately never imports go.opentelemetry.io/otel itself:
// definitions/architecture/dependency-roles.yaml admits only
// internal/platform/telemetry/otel and internal/transport/otelmw to import
// OTel directly (LIB-007), so every actual span operation here goes through
// that package's own caller-safe [hcmotel.ExecutionSpan]/
// [hcmotel.Provider.StartExecutionSpan]/[hcmotel.AmbientTraceID] seam.
//
// It never opens a node span's SpanAttributes.Attempt as a metric label and
// it never places req/response payloads, proposal digests, principal
// identities or authority-bearing baggage onto a span or a log record —
// only the bounded SpanAttributes fields OBS-023 names.
type OTelInstrumentation struct {
	provider *hcmotel.Provider
	logger   *slog.Logger
	clock    func() time.Time
}

var _ execute.Instrumentation = (*OTelInstrumentation)(nil)

// NewOTelInstrumentation builds an OTelInstrumentation over provider and
// logger. A nil logger disables the required log line (spans are still
// recorded); a nil clock defaults to time.Now in UTC.
func NewOTelInstrumentation(provider *hcmotel.Provider, logger *slog.Logger, clock func() time.Time) *OTelInstrumentation {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &OTelInstrumentation{provider: provider, logger: logger, clock: clock}
}

// TraceID implements execute.Instrumentation.
func (o *OTelInstrumentation) TraceID(ctx context.Context) string {
	return hcmotel.AmbientTraceID(ctx)
}

// StartNodeSpan implements execute.Instrumentation. A node span carries no
// log line: OBS-023 requires one log/slog line per advancement and per
// terminal write, not per node run.
func (o *OTelInstrumentation) StartNodeSpan(ctx context.Context, attrs execute.SpanAttributes) (context.Context, execute.Span) {
	return o.startSpan(ctx, spanWorkflowNode, attrs, "")
}

// StartAdvanceSpan implements execute.Instrumentation.
func (o *OTelInstrumentation) StartAdvanceSpan(ctx context.Context, attrs execute.SpanAttributes) (context.Context, execute.Span) {
	return o.startSpan(ctx, spanWorkflowAdvance, attrs, logEventAdvance)
}

// StartTerminalSpan implements execute.Instrumentation.
func (o *OTelInstrumentation) StartTerminalSpan(ctx context.Context, attrs execute.SpanAttributes) (context.Context, execute.Span) {
	return o.startSpan(ctx, spanWorkflowTerminal, attrs, logEventTerminal)
}

// zeroUUID is the execute.TimerID zero value rendered by uuid.String. A
// resume request carrying it has no timer to attribute, so the timer_id
// attribute is omitted rather than recorded as a nil UUID.
const zeroUUID = "00000000-0000-0000-0000-000000000000"

// StartResumeSpan implements execute.ResumeSpanStarter (OBS-013). It opens
// the finite hcmnext.timer.resume span for one timer-wake advancement: a
// new root linked to the parked trace when the stored causal identity is
// valid and unexpired, otherwise an identical unlinked span. Correlation,
// causation and logical-operation identity are preserved verbatim from the
// stored row; only the attempt identity is fresh for this advancement.
// Like a node span it carries no log line: the nested advancement span
// still owns the one required per-advancement line.
func (o *OTelInstrumentation) StartResumeSpan(ctx context.Context, req execute.ResumeSpanRequest) (context.Context, execute.Span) {
	attrs := execute.SpanAttributes{InstanceID: req.InstanceID, NodeID: req.NodeID, Attempt: req.Attempt}
	if req.Causal == nil {
		return o.startSpan(ctx, string(telemetry.SpanTimerResume), attrs, "")
	}
	logical := req.Causal.LogicalOperationID
	if logical == "" {
		logical = req.InstanceID
	}
	attempt := req.InstanceID + "/" + req.NodeID + "/" + strconv.Itoa(req.Attempt)
	cont := hcmotel.DurableAsyncContinuation{
		CorrelationID:      req.Causal.CorrelationID,
		CausationID:        req.Causal.CausationID,
		LogicalOperationID: logical,
		AttemptID:          attempt,
	}
	if link := req.Causal.TraceLink; link != nil {
		cont.TraceLink = &hcmotel.TraceLinkMetadata{
			TraceID: link.TraceID, SpanID: link.SpanID, TraceFlags: link.TraceFlags,
			TraceState: link.TraceState, ExpiresAt: link.ExpiresAt,
		}
	}
	linkAttrs := map[string]string{"logical_operation_id": logical, "attempt_id": attempt}
	if req.TimerID != "" && req.TimerID != zeroUUID {
		linkAttrs["timer_id"] = req.TimerID
	}
	spanCtx, span, err := o.provider.StartDurableAsyncSpan(ctx, tracerName, string(telemetry.SpanTimerResume), cont, linkAttrs, req.At)
	if err != nil {
		// Stored identity failed validation: advance unlinked with
		// identical business behavior rather than refusing the resume.
		return o.startSpan(ctx, string(telemetry.SpanTimerResume), attrs, "")
	}
	return spanCtx, &otelSpan{span: span, started: o.clock(), attrs: attrs, logger: o.logger, logEvent: "", clock: o.clock}
}

func spanAttributeMap(attrs execute.SpanAttributes) map[string]string {
	kv := make(map[string]string, 4)
	if attrs.InstanceID != "" {
		// A workflow instance is the durable logical operation callers use to
		// pivot between execution spans and the inspector. Use the canonical
		// topology key so the shared allow-list preserves it.
		kv["logical_operation_id"] = attrs.InstanceID
	}
	if attrs.NodeID != "" {
		kv["node_id"] = attrs.NodeID
	}
	if attrs.Attempt > 0 {
		kv["attempt_id"] = strconv.Itoa(attrs.Attempt)
	}
	if attrs.TerminalCode != "" {
		kv["terminal_code"] = attrs.TerminalCode
	}
	return kv
}

func (o *OTelInstrumentation) startSpan(ctx context.Context, name string, attrs execute.SpanAttributes, logEvent string) (context.Context, execute.Span) {
	spanCtx, span := o.provider.StartExecutionSpan(ctx, tracerName, name, spanAttributeMap(attrs))
	return spanCtx, &otelSpan{
		span: span, started: o.clock(),
		attrs: attrs, logger: o.logger, logEvent: logEvent, clock: o.clock,
	}
}

// otelSpan is the [execute.Span] StartNodeSpan/StartAdvanceSpan/
// StartTerminalSpan return.
type otelSpan struct {
	span     hcmotel.ExecutionSpan
	started  time.Time
	attrs    execute.SpanAttributes
	logger   *slog.Logger
	logEvent string
	clock    func() time.Time
}

var _ execute.Span = (*otelSpan)(nil)
var _ execute.CausalSpan = (*otelSpan)(nil)

func (s *otelSpan) CausalMetadata(identity execute.CausalIdentity) *runtime.CausalMetadata {
	link, ok := s.span.TraceLinkMetadata(identity.ExpiresAt)
	if !ok {
		return nil
	}
	return &runtime.CausalMetadata{
		CorrelationID: identity.CorrelationID, CausationID: identity.CausationID,
		LogicalOperationID: identity.LogicalOperationID, AttemptID: identity.AttemptID,
		TraceLink: &runtime.TraceLinkMetadata{TraceID: link.TraceID, SpanID: link.SpanID, TraceFlags: link.TraceFlags, TraceState: link.TraceState, ExpiresAt: link.ExpiresAt},
	}
}

// End implements execute.Span: it closes the span, sets its status and
// outcome attribute, and — for an advancement or terminal-write span — emits
// the one required log/slog envelope line, with the same instance/node/
// attempt/terminal-code attributes plus outcome and duration_ms. err is
// never rendered onto the span or the log line beyond its bare presence:
// this package classifies nothing about err's content, on purpose, so a
// caller cannot smuggle payload text through an error message.
func (s *otelSpan) End(outcome string, err error) {
	duration := s.clock().Sub(s.started)
	s.span.End(outcome, err != nil)

	if s.logEvent == "" || s.logger == nil {
		return
	}
	level := slog.LevelInfo
	if err != nil {
		level = slog.LevelError
	}
	s.logger.LogAttrs(s.span.Context(), level, s.logEvent,
		slog.String("trace_id", s.span.TraceID()),
		slog.String("instance_id", s.attrs.InstanceID),
		slog.String("node_id", s.attrs.NodeID),
		slog.Int("attempt", s.attrs.Attempt),
		slog.String("terminal_code", s.attrs.TerminalCode),
		slog.String("outcome", outcome),
		slog.Int64("duration_ms", duration.Milliseconds()),
	)
}
