package otelmw

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

// tracerName identifies this package's own instrumentation library, per
// trace.TracerProvider's documented convention (see
// internal/platform/telemetry/otel/provider.go's Tracer method).
const tracerName = "github.com/monstercameron/human-capital-management-suite/internal/transport/otelmw"

// attrOutcome and attrErrorType are the two span attributes this package
// attaches directly. Both are registered in
// definitions/telemetry/resource-contract.yaml's attribute_allowlist for
// the span signal, so both survive the frozen telemetry.Evaluator alongside
// whatever context-propagation attributes Provider.Tracer's Start already
// attempted (see doc.go).
const (
	attrOutcome   = "outcome"
	attrErrorType = "error_type"
)

// instrument starts one span named by procedure — the manifest-format
// gRPC/connect procedure path (RPCDescriptor.GRPCProcedure's own format:
// "/hcmnext.<pkg>.v1.<Service>/<Method>", identical to
// grpc.UnaryServerInfo.FullMethod and connect.Spec.Procedure per
// internal/transport/manifest/types.go's GRPCProcedure field comment),
// invokes next with the span's context, then sets the span's status and
// outcome attributes from whatever next returned and ends the span.
//
// The context next receives already carries whatever deadline ctx itself
// carried (transport's own CapDeadline ran upstream, in admission, before
// this package's interceptor is ever reached per doc.go's ordering
// requirement): nothing in this function replaces ctx with one derived from
// context.Background, so a caller's remaining deadline and cancellation
// propagate into the span and into next unchanged.
func instrument[T any](ctx context.Context, provider *hcmotel.Provider, procedure string, next func(context.Context) (T, error)) (T, error) {
	ctx = deriveLoggingContext(ctx)
	if provider == nil {
		// No exporter configured (the local-dev default). The request and
		// correlation ids still belong on every log line the call writes;
		// only the span is skipped.
		return next(ctx)
	}
	inv, _ := transport.InvocationFromContext(ctx)

	tracer := provider.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, procedure, trace.WithSpanKind(trace.SpanKindServer))
	defer span.End()

	resp, err := next(ctx)
	setSpanOutcome(span, err, inv)
	return resp, err
}

// setSpanOutcome sets span's status and outcome attributes from err.
//
// err is projected through transport.OwnedError first — the same frozen
// projection both transports' own admission interceptors use to turn a
// handler failure into the canonical owned condition — rather than this
// package inventing a second, possibly divergent classification of the
// same error. That is also why item (4)'s "set span status from the
// envelope.Error code" holds even when next returned a raw, not-yet-owned
// error: OwnedError's classify step (internal/transport/errors.go) is what
// turns it into one before this function ever inspects its Code.
func setSpanOutcome(span trace.Span, err error, inv *transport.Invocation) {
	if err == nil {
		span.SetStatus(codes.Ok, "")
		span.SetAttributes(attribute.String(attrOutcome, "success"))
		return
	}
	owned := transport.OwnedError(err, inv)
	span.SetStatus(codes.Error, owned.Message())
	span.SetAttributes(
		attribute.String(attrOutcome, "error"),
		attribute.String(attrErrorType, owned.Code().String()),
	)
}
