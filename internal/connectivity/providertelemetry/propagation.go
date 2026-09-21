package providertelemetry

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providertelemetry/providerwire"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/boundary"
)

// OutboundHeaders returns the headers an outbound provider request carries:
// traceparent for the span in ctx (omitted when ctx has no valid span
// context) and X-Correlation-Id from the logging correlation id in ctx
// (omitted unless it is at most 128 visible ASCII characters). It has the
// providerdelivery.HeaderSource shape, so it can be passed as a client's
// Headers directly. It never returns baggage: the provider is a third
// party and gets nothing beyond these two values.
func OutboundHeaders(ctx context.Context) http.Header {
	h := http.Header{}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		tp, err := boundary.FormatTraceParent(boundary.Trace{
			TraceID: sc.TraceID().String(),
			SpanID:  sc.SpanID().String(),
			Sampled: sc.IsSampled(),
		})
		if err == nil {
			h.Set(providerwire.TraceParentHeader, tp)
		}
	}
	if id, ok := logging.CorrelationID(ctx); ok && providerwire.ValidCorrelationID(id) {
		h.Set(providerwire.CorrelationIDHeader, id)
	}
	return h
}

// InboundTrace is what a provider callback echoed back. Both values are
// UNTRUSTED: the provider (or anyone who can reach the callback URL) chose
// them. The echoed trace is only ever a span link, never a parent, and the
// echoed correlation id must be checked against the stored delivery before
// it is trusted.
type InboundTrace struct {
	// Link references the echoed span; valid only when HasLink.
	Link    trace.Link
	HasLink bool
	// EchoedCorrelationID is the well-formed X-Correlation-Id the provider
	// sent, or "". It is not placed in the logging context.
	EchoedCorrelationID string
	// SecuritySignal is non-empty when a traceparent was present but
	// malformed (boundary.Policy.Inbound's signal).
	SecuritySignal string
}

type inboundKey struct{}

// InboundContext parses a provider callback's echoed traceparent through
// boundary.Policy{}.Inbound as untrusted input and its X-Correlation-Id
// through the same validation as outbound. The returned context carries
// the InboundTrace for StartCallback; it does not make the echoed span a
// parent and does not adopt the echoed correlation id.
func InboundContext(ctx context.Context, r *http.Request) (context.Context, InboundTrace) {
	var in InboundTrace
	if r == nil {
		return ctx, in
	}
	if raw := r.Header.Get(providerwire.TraceParentHeader); raw != "" {
		d := boundary.Policy{}.Inbound(raw, "")
		if d.FreshTrace {
			in.SecuritySignal = d.SecuritySignal
		} else if sc, ok := spanContextOf(d.Trace); ok {
			in.Link = trace.Link{SpanContext: sc, Attributes: []attribute.KeyValue{attribute.String("link_kind", "provider_echo")}}
			in.HasLink = true
		}
	}
	if id := r.Header.Get(providerwire.CorrelationIDHeader); providerwire.ValidCorrelationID(id) {
		in.EchoedCorrelationID = id
	}
	return context.WithValue(ctx, inboundKey{}, in), in
}

// InboundFrom returns the InboundTrace InboundContext stored in ctx.
func InboundFrom(ctx context.Context) (InboundTrace, bool) {
	in, ok := ctx.Value(inboundKey{}).(InboundTrace)
	return in, ok
}

func spanContextOf(t boundary.Trace) (trace.SpanContext, bool) {
	tid, err := trace.TraceIDFromHex(t.TraceID)
	if err != nil {
		return trace.SpanContext{}, false
	}
	sid, err := trace.SpanIDFromHex(t.SpanID)
	if err != nil {
		return trace.SpanContext{}, false
	}
	var flags trace.TraceFlags
	if t.Sampled {
		flags = trace.FlagsSampled
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: flags, Remote: true})
	return sc, sc.IsValid()
}

// StartCallback starts the server-side hcmnext.provider.call span
// (provider_operation callback) for one provider callback. The echoed
// trace from InboundContext becomes a link. A remote span context already
// in ctx (for example one a transport layer extracted from the same
// untrusted request) is not trusted as parent either: the span starts a new
// root and links that context instead. A local parent span is kept. Call
// CallbackReceived with the returned context to record the intake result,
// then the returned end function.
func (r *Recorder) StartCallback(ctx context.Context, provider string) (context.Context, func()) {
	provider = closed(providers, provider, other)
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("provider", provider),
			attribute.String("provider_operation", OpCallback),
		),
	}
	var links []trace.Link
	if in, ok := InboundFrom(ctx); ok && in.HasLink {
		links = append(links, in.Link)
	}
	if parent := trace.SpanContextFromContext(ctx); parent.IsValid() && parent.IsRemote() {
		opts = append(opts, trace.WithNewRoot())
		if len(links) == 0 || !links[0].SpanContext.Equal(parent) {
			links = append(links, trace.Link{SpanContext: parent, Attributes: []attribute.KeyValue{attribute.String("link_kind", "remote_parent")}})
		}
	}
	if len(links) > 0 {
		opts = append(opts, trace.WithLinks(links...))
	}
	ctx, span := r.tracerOrNoop().Start(ctx, string(telemetry.SpanProviderCall), opts...)
	return ctx, func() { span.End() }
}
