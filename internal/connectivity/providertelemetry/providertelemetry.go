// Package providertelemetry is the observability facade for the
// third-party provider hand-off: HCM delivering pay and access changes from
// its outbox to the payroll and IAM providers, and the providers calling
// back with signed receipts.
//
// Semantic owner: connectivity.
//
// Call sites stay one-liners and consistent: StartDelivery/End wraps one
// provider attempt in an hcmnext.provider.call span, records the attempt
// and latency metrics and writes one structured log line; the event methods
// (RetryScheduled, Abandoned, BreakerTransition, CallbackReceived,
// TokenRefresh, WaitNearTimeout) each record their metric, annotate the
// span in the context and log. OutboundHeaders and InboundContext carry the
// trace and correlation context across the provider boundary.
//
// Span-name choice: every provider operation (deliver, reverse, status,
// token, callback) is the one registered hcmnext.provider.call span with a
// provider_operation attribute, rather than one span name per operation.
// That is the name boundary.KindProvider already maps to, the topology's
// provider_operation attribute exists for exactly this split, and a closed
// attribute keeps the span-name vocabulary from growing with each provider
// verb.
//
// Metric labels are closed vocabularies only (provider, provider_operation,
// outcome_class, result, to_state, secret_slot); change refs, event ids and
// correlation ids reach spans and logs, never metrics. No method logs a
// secret, token, header value or body.
package providertelemetry

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerdelivery"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
)

// TracerName is the instrumentation scope of every span this package starts.
const TracerName = "github.com/monstercameron/human-capital-management-suite/internal/connectivity/providertelemetry"

// Providers.
const (
	ProviderPayroll = "payroll"
	ProviderIAM     = "iam"
)

// Provider operations (the provider_operation attribute).
const (
	OpDeliver  = "deliver"
	OpReverse  = "reverse"
	OpStatus   = "status"
	OpToken    = "token"
	OpCallback = "callback"
)

// Callback intake results (the result label of provider.callback.received).
const (
	CallbackAccepted          = "accepted"
	CallbackDuplicate         = "duplicate"
	CallbackRejectedSignature = "rejected_signature"
	CallbackRejectedWindow    = "rejected_window"
	CallbackRejectedOther     = "rejected_other"
	CallbackWaitNotOpen       = "wait_not_open"
)

// Token refresh results (the result label of provider.token.refreshes).
const (
	TokenSuccess  = "success"
	TokenRejected = "rejected"
	TokenFailure  = "failure"
	TokenTimeout  = "timeout"
)

// Breaker states (the to_state label of provider.breaker.transitions).
const (
	BreakerClosed   = "closed"
	BreakerOpen     = "open"
	BreakerHalfOpen = "half_open"
)

// Bounded fallbacks for values outside a closed vocabulary.
const (
	other = "other"
	// ClassInvalidInput is End's outcome class when the client refused the
	// input and sent nothing (a non-nil error from Deliver/Reverse/Status).
	ClassInvalidInput = "invalid_input"
	// ClassUnknown is End's outcome class for a Result with no Class.
	ClassUnknown = "unknown"
)

// Secret slots (the secret_slot label of provider.callback.secret_index).
const (
	SecretCurrent  = "current"
	SecretPrevious = "previous"
)

var (
	providers  = map[string]bool{ProviderPayroll: true, ProviderIAM: true}
	operations = map[string]bool{OpDeliver: true, OpReverse: true, OpStatus: true, OpToken: true, OpCallback: true}
	classes    = map[string]bool{
		providerdelivery.ClassDelivered: true, providerdelivery.ClassTransientNetwork: true,
		providerdelivery.ClassTransientTimeout: true, providerdelivery.ClassTransientStatus: true,
		providerdelivery.ClassAuthRefreshFailed: true, providerdelivery.ClassRejectedRequest: true,
		providerdelivery.ClassRejectedConflict: true, providerdelivery.ClassRejectedBusiness: true,
		providerdelivery.ClassRejectedAuth: true, providerdelivery.ClassSettledNotReversible: true,
		ClassInvalidInput: true, ClassUnknown: true,
	}
	callbackResults = map[string]bool{
		CallbackAccepted: true, CallbackDuplicate: true, CallbackRejectedSignature: true,
		CallbackRejectedWindow: true, CallbackRejectedOther: true, CallbackWaitNotOpen: true,
	}
	tokenResults  = map[string]bool{TokenSuccess: true, TokenRejected: true, TokenFailure: true, TokenTimeout: true}
	breakerStates = map[string]bool{BreakerClosed: true, BreakerOpen: true, BreakerHalfOpen: true}
)

// closed returns v when it is in vocab and fallback otherwise, so a caller
// bug can never mint a new metric series.
func closed(vocab map[string]bool, v, fallback string) string {
	if vocab[v] {
		return v
	}
	return fallback
}

// Recorder is the facade. The zero value, a nil *Recorder and one built
// from a nil Provider are all safe: without a Provider no span or metric is
// recorded, and without a Logger nothing is logged.
type Recorder struct {
	tracer  trace.Tracer
	metrics *hcmotel.Metrics
	log     *slog.Logger
	now     func() time.Time
}

// New builds a Recorder over the process's telemetry Provider and logger
// (normally one built on internal/platform/logging.NewHandler, so request
// and correlation ids from the context land on every line). Either may be
// nil.
func New(provider *hcmotel.Provider, logger *slog.Logger) *Recorder {
	r := &Recorder{log: logger, now: time.Now}
	if provider != nil {
		r.tracer = provider.Tracer(TracerName)
		r.metrics = provider.Metrics()
	}
	return r
}

func (r *Recorder) tracerOrNoop() trace.Tracer {
	if r == nil || r.tracer == nil {
		return noop.NewTracerProvider().Tracer(TracerName)
	}
	return r.tracer
}

func (r *Recorder) clock() time.Time {
	if r == nil || r.now == nil {
		return time.Now()
	}
	return r.now()
}

func (r *Recorder) logAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	if r == nil || r.log == nil {
		return
	}
	r.log.LogAttrs(ctx, level, msg, attrs...)
}

// Span is one provider attempt started by StartDelivery. End it exactly
// once; later calls are ignored. A nil *Span is safe.
type Span struct {
	rec       *Recorder
	ctx       context.Context
	span      trace.Span
	provider  string
	operation string
	changeRef string
	attempt   int
	start     time.Time
	once      sync.Once
}

// StartDelivery starts the hcmnext.provider.call client span for one
// provider attempt (operation deliver, reverse, status or token) and
// returns the context to hand to the provider client, so OutboundHeaders
// propagates this span. attempt is 1-based; changeRef is the opaque outbox
// change reference (span and log attribute only, never a metric label).
func (r *Recorder) StartDelivery(ctx context.Context, provider, operation, changeRef string, attempt int) (context.Context, *Span) {
	provider = closed(providers, provider, other)
	operation = closed(operations, operation, other)
	attrs := []attribute.KeyValue{
		attribute.String("provider", provider),
		attribute.String("provider_operation", operation),
	}
	if changeRef != "" {
		attrs = append(attrs, attribute.String("change_ref", changeRef))
	}
	if attempt > 0 {
		attrs = append(attrs, attribute.String("attempt_id", strconv.Itoa(attempt)))
	}
	ctx, span := r.tracerOrNoop().Start(ctx, string(telemetry.SpanProviderCall),
		trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	return ctx, &Span{
		rec: r, ctx: ctx, span: span,
		provider: provider, operation: operation, changeRef: changeRef, attempt: attempt,
		start: r.clock(),
	}
}

// End finishes the attempt: span status and attributes, the
// provider.delivery.attempts and provider.delivery.duration metrics, and
// one log line at INFO (delivered), WARN (retry) or ERROR (rejected, or a
// non-nil err meaning the client refused the input and sent nothing).
// err's text is never recorded: providerdelivery errors name fields, and
// the class is what operators need.
func (s *Span) End(result providerdelivery.Result, err error) {
	if s == nil {
		return
	}
	s.once.Do(func() { s.end(result, err) })
}

func (s *Span) end(result providerdelivery.Result, err error) {
	class := closed(classes, result.Class, ClassUnknown)
	outcome, level := telemetry.OutcomeSuccess, slog.LevelInfo
	switch {
	case err != nil:
		class, outcome, level = ClassInvalidInput, telemetry.OutcomeFailure, slog.LevelError
	case result.Outcome == providerdelivery.Retry:
		outcome, level = telemetry.OutcomeFailure, slog.LevelWarn
	case result.Outcome == providerdelivery.Rejected:
		outcome, level = telemetry.OutcomeFailure, slog.LevelError
	case result.Outcome != providerdelivery.Delivered:
		outcome, level = telemetry.OutcomeUnknown, slog.LevelWarn
	}
	status := statusFamily(result.Status)

	s.span.SetAttributes(
		attribute.String("outcome_class", class),
		attribute.String("outcome", string(outcome)),
		attribute.String("status", status),
	)
	if outcome == telemetry.OutcomeSuccess {
		s.span.SetStatus(codes.Ok, "")
	} else {
		s.span.SetAttributes(attribute.String("error_type", class))
		s.span.SetStatus(codes.Error, class)
	}

	elapsed := s.rec.clock().Sub(s.start)
	if m := s.rec.metricsOrNil(); m != nil {
		m.RecordProviderDeliveryAttempt(s.ctx, s.provider, s.operation, class)
		m.RecordProviderDeliveryDuration(s.ctx, s.provider, s.operation, class, float64(elapsed)/float64(time.Millisecond))
	}
	attrs := []slog.Attr{
		slog.String("provider", s.provider),
		slog.String("provider_operation", s.operation),
		slog.String("outcome", string(outcome)),
		slog.String("outcome_class", class),
		slog.String("status", status),
	}
	if s.changeRef != "" {
		attrs = append(attrs, slog.String("change_ref", s.changeRef))
	}
	if s.attempt > 0 {
		attrs = append(attrs, slog.Int("attempt", s.attempt))
	}
	s.rec.logAttrs(s.ctx, level, "provider attempt finished", attrs...)
	s.span.End()
}

func (r *Recorder) metricsOrNil() *hcmotel.Metrics {
	if r == nil {
		return nil
	}
	return r.metrics
}

// statusFamily buckets an HTTP status into 1xx..5xx, or "none" when no
// response was received.
func statusFamily(code int) string {
	if code < 100 || code > 599 {
		return "none"
	}
	return strconv.Itoa(code/100) + "xx"
}

// addEvent annotates the span in ctx, if it is recording.
func addEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.AddEvent(name, trace.WithAttributes(attrs...))
	}
}

// RetryScheduled records that the queue will retry changeRef after delay
// (attempt is the attempt that just failed): the provider.retry.delay
// histogram, a span event and a WARN line.
func (r *Recorder) RetryScheduled(ctx context.Context, provider, changeRef string, attempt int, delay time.Duration) {
	provider = closed(providers, provider, other)
	delay = max(delay, 0)
	ms := delay.Milliseconds()
	if m := r.metricsOrNil(); m != nil {
		m.RecordProviderRetryDelay(ctx, provider, float64(delay)/float64(time.Millisecond))
	}
	addEvent(ctx, "provider.retry.scheduled",
		attribute.String("provider", provider), attribute.Int64("retry_delay_ms", ms))
	r.logAttrs(ctx, slog.LevelWarn, "provider retry scheduled",
		slog.String("provider", provider), slog.String("change_ref", changeRef),
		slog.Int("attempt", attempt), slog.Int64("retry_delay_ms", ms))
}

// Abandoned records that changeRef was given up on after its retry budget;
// cause is the last outcome class. It is an ERROR: an abandoned change
// needs an operator.
func (r *Recorder) Abandoned(ctx context.Context, provider, changeRef, cause string) {
	provider = closed(providers, provider, other)
	cause = closed(classes, cause, ClassUnknown)
	if m := r.metricsOrNil(); m != nil {
		m.RecordProviderDeliveryAbandoned(ctx, provider, cause)
	}
	addEvent(ctx, "provider.delivery.abandoned",
		attribute.String("provider", provider), attribute.String("outcome_class", cause))
	r.logAttrs(ctx, slog.LevelError, "provider delivery abandoned",
		slog.String("provider", provider), slog.String("change_ref", changeRef), slog.String("outcome_class", cause))
}

// BreakerTransition records a provider circuit-breaker transition. Opening
// is a WARN; closing and half-opening are INFO.
func (r *Recorder) BreakerTransition(ctx context.Context, provider, from, to string) {
	provider = closed(providers, provider, other)
	from = closed(breakerStates, from, other)
	to = closed(breakerStates, to, other)
	if m := r.metricsOrNil(); m != nil {
		m.RecordProviderBreakerTransition(ctx, provider, to)
	}
	addEvent(ctx, "provider.breaker.transition",
		attribute.String("provider", provider), attribute.String("from_state", from),
		attribute.String("to_state", to), attribute.String("breaker_state", to))
	level := slog.LevelInfo
	if to == BreakerOpen {
		level = slog.LevelWarn
	}
	r.logAttrs(ctx, level, "provider breaker transition",
		slog.String("provider", provider), slog.String("from_state", from),
		slog.String("to_state", to), slog.String("breaker_state", to))
}

// CallbackReceived records one provider callback's intake result on the
// provider.callback.received counter and, when a signing secret verified
// it (secretIndex >= 0: 0 is the current secret, 1 the previous), on
// provider.callback.secret_index, so a rotation can be retired once the
// previous secret stops matching. It annotates the callback span in ctx
// (see StartCallback) and logs at INFO for accepted and duplicate, WARN
// otherwise. eventID and changeRef are span/log attributes only.
func (r *Recorder) CallbackReceived(ctx context.Context, provider, result string, secretIndex int, eventID, changeRef string) {
	provider = closed(providers, provider, other)
	result = closed(callbackResults, result, CallbackRejectedOther)
	m := r.metricsOrNil()
	if m != nil {
		m.RecordProviderCallbackReceived(ctx, provider, result)
	}
	attrs := []attribute.KeyValue{
		attribute.String("provider", provider),
		attribute.String("provider_operation", OpCallback),
		attribute.String("result", result),
	}
	logAttrs := []slog.Attr{
		slog.String("provider", provider),
		slog.String("provider_operation", OpCallback),
		slog.String("result", result),
	}
	if secretIndex >= 0 {
		slot := secretSlot(secretIndex)
		if m != nil {
			m.RecordProviderCallbackSecretIndex(ctx, provider, slot)
		}
		attrs = append(attrs, attribute.Int("secret_index", secretIndex), attribute.String("secret_slot", slot))
		logAttrs = append(logAttrs, slog.Int("secret_index", secretIndex), slog.String("secret_slot", slot))
	}
	if eventID != "" {
		attrs = append(attrs, attribute.String("event_id", eventID))
		logAttrs = append(logAttrs, slog.String("event_id", eventID))
	}
	if changeRef != "" {
		attrs = append(attrs, attribute.String("change_ref", changeRef))
		logAttrs = append(logAttrs, slog.String("change_ref", changeRef))
	}
	level := slog.LevelInfo
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.SetAttributes(attrs...)
		if result == CallbackAccepted || result == CallbackDuplicate {
			span.SetStatus(codes.Ok, "")
		} else {
			span.SetStatus(codes.Error, result)
		}
	}
	if result != CallbackAccepted && result != CallbackDuplicate {
		level = slog.LevelWarn
	}
	r.logAttrs(ctx, level, "provider callback received", logAttrs...)
}

func secretSlot(index int) string {
	switch index {
	case 0:
		return SecretCurrent
	case 1:
		return SecretPrevious
	default:
		return other
	}
}

// TokenRefresh records one OAuth client-credentials token refresh by
// result (success, rejected, failure, timeout): INFO on success, WARN
// otherwise. The token itself is never seen here.
func (r *Recorder) TokenRefresh(ctx context.Context, result string) {
	result = closed(tokenResults, result, TokenFailure)
	if m := r.metricsOrNil(); m != nil {
		m.RecordProviderTokenRefresh(ctx, result)
	}
	addEvent(ctx, "provider.token.refresh", attribute.String("result", result))
	level := slog.LevelInfo
	if result != TokenSuccess {
		level = slog.LevelWarn
	}
	r.logAttrs(ctx, level, "provider token refresh",
		slog.String("provider_operation", OpToken), slog.String("result", result))
}

// WaitNearTimeout records that the wait for changeRef's provider result
// came close to its timeout: a WARN, because the next step is a timeout
// and a status poll.
func (r *Recorder) WaitNearTimeout(ctx context.Context, provider, changeRef string) {
	provider = closed(providers, provider, other)
	if m := r.metricsOrNil(); m != nil {
		m.RecordProviderWaitNearTimeout(ctx, provider)
	}
	addEvent(ctx, "provider.wait.near_timeout", attribute.String("provider", provider))
	r.logAttrs(ctx, slog.LevelWarn, "provider result wait near timeout",
		slog.String("provider", provider), slog.String("change_ref", changeRef))
}
