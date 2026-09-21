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
package observe

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Outcomes an operation ends with.
const (
	OutcomeSuccess = "SUCCESS"
	OutcomeFailure = "FAILURE"
	// OutcomeRefused is a governed refusal (a stale fence, an overloaded
	// start, a separation-of-duties denial): the system worked as designed and
	// said no. It is logged at WARN, not ERROR.
	OutcomeRefused = "REFUSED"
	// OutcomeNoop is an idempotent replay or an operation with nothing to do.
	OutcomeNoop = "NOOP"
)

// Attribute keys. Recorders may drop any key their export policy does not
// admit; these are the only keys workflow code ever sets.
const (
	KeyTenant      = "tenant_id"
	KeyWorkflow    = "workflow_id"
	KeyInstance    = "instance_id"
	KeyNode        = "node_id"
	KeyAttempt     = "attempt"
	KeyFence       = "fence_token"
	KeyWorkItem    = "work_item_id"
	KeyTimer       = "timer_id"
	KeyResource    = "resource"
	KeyStatus      = "status"
	KeyCode        = "code"
	KeyMode        = "execution_mode"
	KeyDisposition = "disposition"
	// KeyCorrelation is the business correlation id an instance carries
	// from the request that started it, so every engine log line of one run
	// can be joined to that request and to the intent.
	KeyCorrelation = "correlation_id"
)

// Attrs is a bounded set of string attributes.
type Attrs map[string]string

// With returns a copy of a with key set to value, skipping empty values.
func (a Attrs) With(key, value string) Attrs {
	out := make(Attrs, len(a)+1)
	for k, v := range a {
		out[k] = v
	}
	if value != "" {
		out[key] = value
	}
	return out
}

// Int formats an integer attribute value.
func Int(n int64) string { return strconv.FormatInt(n, 10) }

// fieldKey is the allowlist [Of] reads: exported struct field name to
// attribute key. Nothing outside this list -- no payload, principal, reason
// text or digest -- can become an attribute.
func fieldKey(name string) (string, bool) {
	switch name {
	case "TenantID":
		return KeyTenant, true
	case "WorkflowID":
		return KeyWorkflow, true
	case "InstanceID":
		return KeyInstance, true
	case "NodeID":
		return KeyNode, true
	case "Attempt":
		return KeyAttempt, true
	case "WorkItemID":
		return KeyWorkItem, true
	case "TimerID":
		return KeyTimer, true
	case "Token":
		return KeyFence, true
	case "Status":
		return KeyStatus, true
	case "TerminalCode":
		return KeyCode, true
	case "Disposition":
		return KeyDisposition, true
	case "CorrelationID":
		return KeyCorrelation, true
	}
	return "", false
}

// ofDepth bounds how far [Of] descends into nested request structs (a fenced
// advance request carries its fence and its inner advance request).
const ofDepth = 3

// Of extracts bounded attributes from request, receipt and identity values:
// an [Attrs] value is merged as-is; a struct (or pointer to one) contributes
// its allowlisted id fields, searched breadth-first to a small depth so the
// outermost occurrence of a field wins; anything else is ignored. Zero
// values (the nil UUID, "", 0) are skipped.
func Of(values ...any) Attrs {
	out := Attrs{}
	for _, v := range values {
		if a, ok := v.(Attrs); ok {
			for k, s := range a {
				if s != "" {
					out[k] = s
				}
			}
			continue
		}
		collect(out, reflect.ValueOf(v))
	}
	return out
}

func collect(out Attrs, root reflect.Value) {
	level := []reflect.Value{root}
	for depth := 0; depth < ofDepth && len(level) > 0; depth++ {
		var next []reflect.Value
		for _, v := range level {
			for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
				if v.IsNil() {
					v = reflect.Value{}
					break
				}
				v = v.Elem()
			}
			if !v.IsValid() || v.Kind() != reflect.Struct {
				continue
			}
			t := v.Type()
			if t.Name() == "Resource" {
				if kind := v.FieldByName("Kind"); kind.IsValid() && kind.Kind() == reflect.String && out[KeyResource] == "" && kind.String() != "" {
					out[KeyResource] = kind.String()
				}
			}
			for i := range t.NumField() {
				f := t.Field(i)
				if !f.IsExported() {
					continue
				}
				fv := v.Field(i)
				if key, ok := fieldKey(f.Name); ok {
					if _, set := out[key]; !set {
						if s := scalar(fv); s != "" {
							out[key] = s
						}
					}
					continue
				}
				if f.Name == "Resource" && fv.Kind() == reflect.Struct {
					if kind := fv.FieldByName("Kind"); kind.IsValid() && kind.Kind() == reflect.String && out[KeyResource] == "" && kind.String() != "" {
						out[KeyResource] = kind.String()
					}
				}
				switch fv.Kind() {
				case reflect.Struct, reflect.Pointer:
					next = append(next, fv)
				}
			}
		}
		level = next
	}
}

const zeroUUID = "00000000-0000-0000-0000-000000000000"

func scalar(v reflect.Value) string {
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return ""
	}
	if v.CanInterface() {
		if s, ok := v.Interface().(fmt.Stringer); ok && v.Kind() != reflect.String {
			if out := s.String(); out != zeroUUID {
				return out
			}
			return ""
		}
	}
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Int() != 0 {
			return strconv.FormatInt(v.Int(), 10)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v.Uint() != 0 {
			return strconv.FormatUint(v.Uint(), 10)
		}
	}
	return ""
}

// DoneWith sets the allowlisted attributes of results (a minted fence, the
// instance a start created, a terminal status) on op and then ends it like
// [Done].
func DoneWith(op Operation, err error, results ...any) error {
	if _, off := op.(noop); !off {
		for k, v := range Of(results...) {
			op.Set(k, v)
		}
	}
	return Done(op, err)
}

// Begin opens name with the attributes [Of] extracts from values. When ctx
// carries no recorder it does no reflection at all, so an uninstrumented
// caller pays one context lookup.
func Begin(ctx context.Context, name string, values ...any) (context.Context, Operation) {
	r := RecorderFrom(ctx)
	if r == nil {
		return ctx, noop{}
	}
	return r.Start(ctx, name, Of(values...))
}

// Operation is one open operation. End must be called exactly once.
type Operation interface {
	// Set adds an attribute learned during the operation (a fence token
	// minted, a node chosen).
	Set(key, value string)
	// End closes the operation with outcome. err, when non-nil, contributes
	// only its classified code.
	End(outcome string, err error)
}

// Recorder opens operations. Implementations live outside internal/workflow.
type Recorder interface {
	Start(ctx context.Context, name string, attrs Attrs) (context.Context, Operation)
}

type recorderKey struct{}

// WithRecorder returns ctx carrying r. A nil r leaves ctx unchanged.
func WithRecorder(ctx context.Context, r Recorder) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, recorderKey{}, r)
}

// RecorderFrom returns the recorder ctx carries, or nil.
func RecorderFrom(ctx context.Context) Recorder {
	r, _ := ctx.Value(recorderKey{}).(Recorder)
	return r
}

// Start opens name on the recorder ctx carries, or returns a no-op operation.
func Start(ctx context.Context, name string, attrs Attrs) (context.Context, Operation) {
	if r := RecorderFrom(ctx); r != nil {
		return r.Start(ctx, name, attrs)
	}
	return ctx, noop{}
}

type noop struct{}

func (noop) Set(string, string) {}
func (noop) End(string, error)  {}

// coded is implemented by every workflow error type that carries a stable
// machine code.
type coded interface{ ErrorCode() string }

// ErrorCode classifies err as a bounded code: the workflow error's own code
// when it carries one, "CONTEXT_CANCELED" or "DEADLINE_EXCEEDED" for context
// errors, and "ERROR" otherwise. It never returns message text.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var c coded
	if errors.As(err, &c) && c.ErrorCode() != "" {
		return c.ErrorCode()
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "CONTEXT_CANCELED"
	case errors.Is(err, context.DeadlineExceeded):
		return "DEADLINE_EXCEEDED"
	}
	return "ERROR"
}

// Finish ends op with SUCCESS when err is nil, REFUSED when refused(err)
// reports a governed refusal, and FAILURE otherwise, and returns err so a
// caller can write `return observe.Finish(op, err, nil)`.
func Finish(op Operation, err error, refused func(error) bool) error {
	switch {
	case err == nil:
		op.End(OutcomeSuccess, nil)
	case refused != nil && refused(err):
		op.End(OutcomeRefused, err)
	default:
		op.End(OutcomeFailure, err)
	}
	return err
}

// Refused is the default governed-refusal classifier: an error carrying a
// workflow code is a refusal (a stale fence, an illegal transition, a held
// lease) unless that code reports a storage failure; an uncoded error or a
// context error is a failure.
func Refused(err error) bool {
	var c coded
	if !errors.As(err, &c) {
		return false
	}
	code := c.ErrorCode()
	return code != "" && !strings.Contains(code, "STORAGE")
}

// Done ends op with [Finish] using the default [Refused] classifier and
// returns err.
func Done(op Operation, err error) error { return Finish(op, err, Refused) }

// Clock is the monotonic-duration source a recorder uses; exported so tests
// can pin durations.
type Clock func() time.Time
