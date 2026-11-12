// Package observe is the workflow engine's one telemetry seam.
//
// Every state-changing workflow operation -- a start, an advancement, a lease
// acquisition or takeover, a timer fire, a human task transition, a recovery,
// a migration, an intervention -- opens one [Operation] through [Start] and
// ends it with its outcome. The recorder that turns an operation into an
// OpenTelemetry span and a structured log line travels on the context
// ([WithRecorder]), so no workflow package imports an exporter
// (definitions/architecture/dependency-roles.yaml LIB-007 admits only
// internal/platform/telemetry/otel to import OTel), no package holds a
// package-level recorder (the composition-root rule forbids shared mutable
// registries), and a caller that attaches none pays one context lookup.
//
// Attributes are bounded identifiers only: tenant, workflow, instance, node,
// attempt, fence token, work item, timer and a short reason code. Payloads,
// principals, proposal digests and error message text never become an
// attribute; an error contributes only its classified code ([ErrorCode]).
//
// The implementation lives in internal/telemetry/observe, a leaf the browser
// client may link (designerpalette and draftcompile open their operations
// through it directly). This package keeps the workflow-facing name: its
// types and constants are aliases, and its functions delegate to the leaf.
package observe

import (
	"context"

	seam "github.com/monstercameron/human-capital-management-suite/internal/telemetry/observe"
)

// Outcomes an operation ends with.
const (
	OutcomeSuccess = seam.OutcomeSuccess
	OutcomeFailure = seam.OutcomeFailure
	// OutcomeRefused is a governed refusal (a stale fence, an overloaded
	// start, a separation-of-duties denial): the system worked as designed and
	// said no. It is logged at WARN, not ERROR.
	OutcomeRefused = seam.OutcomeRefused
	// OutcomeNoop is an idempotent replay or an operation with nothing to do.
	OutcomeNoop = seam.OutcomeNoop
)

// Attribute keys. Recorders may drop any key their export policy does not
// admit; these are the only keys workflow code ever sets.
const (
	KeyTenant      = seam.KeyTenant
	KeyWorkflow    = seam.KeyWorkflow
	KeyInstance    = seam.KeyInstance
	KeyNode        = seam.KeyNode
	KeyAttempt     = seam.KeyAttempt
	KeyFence       = seam.KeyFence
	KeyWorkItem    = seam.KeyWorkItem
	KeyTimer       = seam.KeyTimer
	KeyResource    = seam.KeyResource
	KeyStatus      = seam.KeyStatus
	KeyCode        = seam.KeyCode
	KeyMode        = seam.KeyMode
	KeyDisposition = seam.KeyDisposition
	// KeyCorrelation is the business correlation id an instance carries
	// from the request that started it, so every engine log line of one run
	// can be joined to that request and to the intent.
	KeyCorrelation = seam.KeyCorrelation
)

// Attrs is a bounded set of string attributes.
type Attrs = seam.Attrs

// Operation is one open operation. End must be called exactly once.
type Operation = seam.Operation

// Recorder opens operations. Implementations live outside internal/workflow.
type Recorder = seam.Recorder

// Clock is the monotonic-duration source a recorder uses; exported so tests
// can pin durations.
type Clock = seam.Clock

// Int formats an integer attribute value.
func Int(n int64) string { return seam.Int(n) }

// Of extracts bounded attributes from request, receipt and identity values.
// See the leaf package for the allowlist and traversal rules.
func Of(values ...any) Attrs { return seam.Of(values...) }

// DoneWith sets the allowlisted attributes of results (a minted fence, the
// instance a start created, a terminal status) on op and then ends it like
// [Done].
func DoneWith(op Operation, err error, results ...any) error {
	return seam.DoneWith(op, err, results...)
}

// Begin opens name with the attributes [Of] extracts from values. When ctx
// carries no recorder it does no reflection at all, so an uninstrumented
// caller pays one context lookup.
func Begin(ctx context.Context, name string, values ...any) (context.Context, Operation) {
	return seam.Begin(ctx, name, values...)
}

// WithRecorder returns ctx carrying r. A nil r leaves ctx unchanged.
func WithRecorder(ctx context.Context, r Recorder) context.Context {
	return seam.WithRecorder(ctx, r)
}

// RecorderFrom returns the recorder ctx carries, or nil.
func RecorderFrom(ctx context.Context) Recorder {
	return seam.RecorderFrom(ctx)
}

// Start opens name on the recorder ctx carries, or returns a no-op operation.
func Start(ctx context.Context, name string, attrs Attrs) (context.Context, Operation) {
	return seam.Start(ctx, name, attrs)
}

// ErrorCode classifies err as a bounded code. It never returns message text.
func ErrorCode(err error) string { return seam.ErrorCode(err) }

// Finish ends op with SUCCESS, REFUSED or FAILURE according to err and
// refused, and returns err.
func Finish(op Operation, err error, refused func(error) bool) error {
	return seam.Finish(op, err, refused)
}

// Refused is the default governed-refusal classifier.
func Refused(err error) bool { return seam.Refused(err) }

// Done ends op with [Finish] using the default [Refused] classifier and
// returns err.
func Done(op Operation, err error) error { return seam.Done(op, err) }
