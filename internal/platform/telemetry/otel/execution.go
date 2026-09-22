package otel

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ExecutionSpan is a caller-safe handle onto one span this package opened on
// a caller's behalf. It exists so a caller outside this package's own
// LIB-007 allowed root (this package and internal/transport/otelmw are the
// only two roots definitions/architecture/dependency-roles.yaml admits to
// import go.opentelemetry.io/otel directly) can still hold and end a real
// span without importing anything under go.opentelemetry.io/otel itself.
//
// internal/platform/execution's own OBS-023 [Instrumentation] implementation
// is exactly that caller: it needs OpenTelemetry's actual span behavior
// (attributes, status, End) but must not add a third allowed root to the
// firewall's manifest to get it.
type ExecutionSpan struct {
	span trace.Span
	ctx  context.Context
}

// SetAttributes adds identifiers learned after start through the same policy
// filter as initial attributes. No raw SDK handle leaves this package.
func (s ExecutionSpan) SetAttributes(attrs map[string]string) {
	if s.span == nil {
		return
	}
	kvs := make([]attribute.KeyValue, 0, len(attrs))
	for key, value := range attrs {
		kvs = append(kvs, attribute.String(key, value))
	}
	s.span.SetAttributes(kvs...)
}

// End sets the span's outcome attribute and status and ends it. failed marks
// the span as an error span; outcome is recorded as a plain "outcome"
// attribute (never a payload, principal or authority-bearing field —
// OBS-023's own bounded attribute set).
func (s ExecutionSpan) End(outcome string, failed bool) {
	s.span.SetAttributes(attribute.String("outcome", outcome))
	if failed {
		s.span.SetStatus(codes.Error, "")
	} else {
		s.span.SetStatus(codes.Ok, "")
	}
	s.span.End()
}

// TraceID reports this span's own trace id, or "" if the span carries none
// (should not happen for a span this package itself started, but guarded
// rather than asserted).
func (s ExecutionSpan) TraceID() string {
	sc := trace.SpanContextFromContext(s.ctx)
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}

// Context returns the child context [Provider.StartExecutionSpan] returned
// alongside this span, so a caller that stored only the ExecutionSpan value
// (rather than threading the context itself) can still recover it — for
// example to pass to a structured logger that reads correlation values off
// context.
func (s ExecutionSpan) Context() context.Context { return s.ctx }

// StartExecutionSpan opens a span named spanName on the tracer named
// tracerName (trace.TracerProvider's own documented convention: an
// importable code path, not a human label) under this Provider's own
// attribute policy, with attrs as bounded string key/value pairs. It
// returns the child context and a caller-safe [ExecutionSpan].
//
// This is the seam a caller outside this package's LIB-007 allowed root
// uses to get a real, policy-filtered span without ever importing
// go.opentelemetry.io/otel itself: every argument and return value here is
// either a stdlib type, a plain string, or this package's own
// [ExecutionSpan].
func (p *Provider) StartExecutionSpan(ctx context.Context, tracerName, spanName string, attrs map[string]string) (context.Context, ExecutionSpan) {
	tracer := p.Tracer(tracerName)
	kvs := make([]attribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		kvs = append(kvs, attribute.String(k, v))
	}
	spanCtx, span := tracer.Start(ctx, spanName, trace.WithAttributes(kvs...))
	return spanCtx, ExecutionSpan{span: span, ctx: spanCtx}
}

// AmbientTraceID reports the W3C trace id already active on ctx (whatever
// propagation a transport or a prior span establishes), or "" when ctx
// carries none. Like [Provider.StartExecutionSpan], this is a plain-string
// seam so a caller outside this package's LIB-007 allowed root never needs
// to import go.opentelemetry.io/otel/trace itself just to read this value.
func AmbientTraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}
