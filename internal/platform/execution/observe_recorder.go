package execution

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/logging"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// ObserveRecorder is the production [observe.Recorder]: every workflow
// operation opened through internal/workflow/observe becomes one span named
// "hcmnext.<operation>" on the policy-filtered provider and one structured log
// line named after the operation, carrying its bounded attributes, outcome,
// classified error code, duration and trace id. Successful operations log at
// DEBUG so a busy engine does not flood INFO; refusals log at WARN and
// failures at ERROR, so status and issues are always visible.
type ObserveRecorder struct {
	provider *hcmotel.Provider
	logger   *slog.Logger
	clock    func() time.Time
}

var _ observe.Recorder = (*ObserveRecorder)(nil)

// NewObserveRecorder builds an ObserveRecorder. A nil provider records no
// spans; a nil logger falls back to slog.Default; a nil clock uses time.Now.
func NewObserveRecorder(provider *hcmotel.Provider, logger *slog.Logger, clock func() time.Time) *ObserveRecorder {
	if logger == nil {
		logger = slog.Default()
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &ObserveRecorder{provider: provider, logger: logger, clock: clock}
}

// Start implements observe.Recorder.
func (r *ObserveRecorder) Start(ctx context.Context, name string, attrs observe.Attrs) (context.Context, observe.Operation) {
	op := &recordedOperation{rec: r, name: name, attrs: map[string]string{}, started: r.clock()}
	for k, v := range attrs {
		op.attrs[k] = v
	}
	// One run's correlation id joins every engine line to the request that
	// started it. An operation that names one puts it into the logging
	// context -- replacing the current request's own, so a later approval's
	// engine lines join the run rather than that approval request (its
	// request_id still names the request) -- and every nested operation
	// inherits it; one that does not inherits the enclosing operation's.
	if id := op.attrs[observe.KeyCorrelation]; id != "" {
		ctx = logging.WithCorrelationID(ctx, id)
	} else if id, ok := logging.CorrelationID(ctx); ok {
		op.attrs[observe.KeyCorrelation] = id
	}
	if r.provider != nil {
		ctx, op.span = r.provider.StartExecutionSpan(ctx, tracerName, "hcmnext."+name, op.attrs)
		op.hasSpan = true
	}
	op.ctx = ctx
	return ctx, op
}

type recordedOperation struct {
	rec     *ObserveRecorder
	name    string
	ctx     context.Context
	span    hcmotel.ExecutionSpan
	hasSpan bool
	started time.Time

	mu    sync.Mutex
	attrs map[string]string
	ended bool
}

// Set implements observe.Operation. Attributes set after Start reach the log
// line; the span keeps the attributes it was opened with plus its outcome.
func (o *recordedOperation) Set(key, value string) {
	if value == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.attrs[key] = value
}

// End implements observe.Operation. A second End is ignored.
func (o *recordedOperation) End(outcome string, err error) {
	o.mu.Lock()
	if o.ended {
		o.mu.Unlock()
		return
	}
	o.ended = true
	attrs := make(map[string]string, len(o.attrs))
	for k, v := range o.attrs {
		attrs[k] = v
	}
	o.mu.Unlock()

	failed := outcome == observe.OutcomeFailure
	if o.hasSpan {
		o.span.End(outcome, failed)
	}
	level := slog.LevelDebug
	switch outcome {
	case observe.OutcomeRefused:
		level = slog.LevelWarn
	case observe.OutcomeFailure:
		level = slog.LevelError
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]slog.Attr, 0, len(keys)+5)
	for _, k := range keys {
		fields = append(fields, slog.String(k, attrs[k]))
	}
	fields = append(fields,
		slog.String("outcome", outcome),
		slog.Int64("duration_ms", o.rec.clock().Sub(o.started).Milliseconds()),
	)
	if code := observe.ErrorCode(err); code != "" {
		fields = append(fields, slog.String("error_code", code))
	}
	if o.hasSpan {
		fields = append(fields, slog.String("trace_id", o.span.TraceID()))
	} else if id := hcmotel.AmbientTraceID(o.ctx); id != "" {
		fields = append(fields, slog.String("trace_id", id))
	}
	o.rec.logger.LogAttrs(o.ctx, level, o.name, fields...)
}
