package edge

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// admissionInterceptor is the HTTP edge's half of the trusted request
// boundary. It is deliberately a near-transcription of the gRPC interceptor:
// the same CapDeadline, the same transport.Admit, the same
// transport.OwnedError, the same log record. Only the metadata adapter and the
// wire error type differ, because only those are actually protocol-specific.
type admissionInterceptor struct {
	cfg transport.Config
}

// WrapUnary implements [connect.Interceptor].
func (i admissionInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if req.Spec().IsClient {
			return next(ctx, req)
		}
		start := time.Now()

		ctx, cancel := transport.CapDeadline(ctx, i.cfg)
		defer cancel()

		message, _ := req.Any().(proto.Message)
		procedure := req.Spec().Procedure

		admitted, inv, admitErr := transport.Admit(ctx, i.cfg, transport.AdmissionRequest{
			Metadata: transport.MapMetadata(req.Header()),
			Method:   procedure,
			Kind:     transport.KindHTTPEdge,
			Message:  message,
		})
		if admitErr != nil {
			return nil, i.finish(ctx, procedure, nil, admitErr.CorrelationID(), start, admitErr)
		}

		resp, err := next(admitted, req)
		if err != nil {
			return nil, i.finish(ctx, procedure, inv, inv.RequestID(), start, transport.OwnedError(err, inv))
		}
		if ctxErr := admitted.Err(); ctxErr != nil {
			return nil, i.finish(ctx, procedure, inv, inv.RequestID(), start, transport.OwnedError(ctxErr, inv))
		}
		i.finish(ctx, procedure, inv, inv.RequestID(), start, nil)
		return resp, nil
	}
}

// WrapStreamingClient implements [connect.Interceptor]. Streaming is out of
// scope for P1A (TOOL-009 owns it), so the interceptor passes it through
// rather than pretending to govern it.
func (i admissionInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler implements [connect.Interceptor]. See
// [admissionInterceptor.WrapStreamingClient].
func (i admissionInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// finish records the request, projects the HTTP status and returns the wire
// error.
func (i admissionInterceptor) finish(ctx context.Context, procedure string, inv *transport.Invocation, requestID string, start time.Time, err *envelope.Error) error {
	if i.cfg.Logger != nil {
		i.cfg.Logger.LogRequest(transport.NewLogRecord(procedure, transport.KindHTTPEdge, inv, requestID, time.Since(start), err))
	}
	if err == nil {
		return nil
	}
	setStatusOverride(ctx, err.HTTPStatus())
	setRetryAfterOverride(ctx, err.RetryAfter())
	return ToConnectError(err)
}

// ToConnectError projects an owned error onto the connect wire error, carrying
// the canonical hcmnext.common.v1.ErrorDetail as a typed detail. The projected
// code is the same [envelope.Code] mapping the gRPC side uses: connect's codes
// are the gRPC codes by construction.
func ToConnectError(owned *envelope.Error) error {
	if owned == nil {
		return nil
	}
	connectErr := connect.NewError(connect.Code(owned.GRPCCode()), errors.New(owned.Message()))
	if retryAfter := owned.RetryAfter(); retryAfter > 0 {
		connectErr.Meta().Set("Retry-After", strconv.Itoa(retryAfter))
	}
	if detail, err := connect.NewErrorDetail(owned.Detail()); err == nil {
		connectErr.AddDetail(detail)
	}
	return connectErr
}

// FromConnectError extracts the owned error from a connect client error. It is
// the HTTP-edge twin of envelope.FromGRPC, and both funnel into
// envelope.FromDetail, so "the same failure means the same thing on both
// channels" is one code path rather than two agreements.
func FromConnectError(err error) (*envelope.Error, bool) {
	if err == nil {
		return nil, false
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return nil, false
	}
	var detail *commonv1.ErrorDetail
	for _, d := range connectErr.Details() {
		value, valueErr := d.Value()
		if valueErr != nil {
			continue
		}
		if ed, ok := value.(*commonv1.ErrorDetail); ok {
			detail = ed
			break
		}
	}
	return envelope.FromDetail(envelope.CodeFromGRPC(codes.Code(connectErr.Code())), connectErr.Message(), detail), true
}

// statusOverrideKey is the unexported context key for the per-request HTTP
// status projection.
type statusOverrideKey struct{}

// statusOverride carries the HTTP status the canonical projection table
// requires for this request's outcome.
type statusOverride struct {
	status     int
	retryAfter int
}

// setStatusOverride records the canonical HTTP status for the current request.
func setStatusOverride(ctx context.Context, status int) {
	if override, ok := ctx.Value(statusOverrideKey{}).(*statusOverride); ok {
		override.status = status
	}
}

func setRetryAfterOverride(ctx context.Context, seconds int) {
	if override, ok := ctx.Value(statusOverrideKey{}).(*statusOverride); ok && seconds > 0 {
		override.retryAfter = seconds
	}
}

// statusOverrideMiddleware makes the response's HTTP status follow the
// canonical error projection table rather than connect's own mapping.
//
// connect maps FAILED_PRECONDITION to 400; the contract requires 412. Rather
// than accept a status that contradicts the published table, the edge records
// the canonical status per request and rewrites the failure status on the way
// out. Success statuses are untouched, and a failure for which nothing was
// recorded keeps whatever connect chose.
func statusOverrideMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		override := &statusOverride{}
		writer := &overrideResponseWriter{ResponseWriter: w, override: override}
		next.ServeHTTP(writer, r.WithContext(context.WithValue(r.Context(), statusOverrideKey{}, override)))
	})
}

// overrideResponseWriter applies the recorded canonical status to an error
// response.
type overrideResponseWriter struct {
	http.ResponseWriter

	override *statusOverride
	written  bool
}

// WriteHeader implements [http.ResponseWriter].
func (w *overrideResponseWriter) WriteHeader(status int) {
	if w.written {
		return
	}
	w.written = true
	if w.override.status != 0 && status >= http.StatusBadRequest {
		status = w.override.status
	}
	if status >= http.StatusBadRequest && w.override.retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(w.override.retryAfter))
	}
	w.ResponseWriter.WriteHeader(status)
}

// Write implements [http.ResponseWriter].
func (w *overrideResponseWriter) Write(p []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

// Flush implements [http.Flusher] so that connect's streaming paths keep
// working through the wrapper.
func (w *overrideResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
