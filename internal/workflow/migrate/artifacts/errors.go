package artifacts

import (
	"errors"
	"fmt"
)

// ErrArtifacts is the sentinel every refusal from this package unwraps to.
// Classify with [errors.Is]; read [CodeOf] for the exact reason. Never match
// message text.
var ErrArtifacts = errors.New("workflow/migrate/artifacts: rejected")

// Stable refusal codes.
const (
	// CodeInvalidRequest reports a malformed [Request], [Scope] or [Epoch]:
	// this package refuses before reading a single row.
	CodeInvalidRequest = "INVALID_REQUEST"
	// CodeMissingPort reports a [Ports] set that does not supply every port
	// the declared handlers need. A missing port would silently migrate zero
	// artifacts of that kind, which is exactly the loss this ticket exists to
	// prevent, so it is a refusal rather than an empty result.
	CodeMissingPort = "MISSING_PORT"
	// CodeStaleLease reports a lease that may not be carried onto the new
	// epoch: a fence behind the resource's current token, a fence naming a
	// different lease or holder, a lease that lapsed by the caller's own
	// instant, or a live lease with no fence presented at all.
	CodeStaleLease = "STALE_LEASE"
	// CodeNotRelocatable reports a pending artifact whose durable identity
	// binds it to the node it was raised on, presented for a migration that
	// moves the frontier off that node. Nothing is approximated: the run
	// refuses.
	CodeNotRelocatable = "NOT_RELOCATABLE"
	// CodeWakeInstantMoved reports a re-keyed timer whose replacement wake
	// requirement does not resolve to the exact instant the promise on the
	// table already named. A migration re-keys a promise; it never
	// reschedules one.
	CodeWakeInstantMoved = "WAKE_INSTANT_MOVED"
	// CodeStorageFailed reports a durable read or write this package issued
	// that the store refused for a reason of its own. The underlying error is
	// wrapped and reachable with [errors.Unwrap].
	CodeStorageFailed = "STORAGE_FAILED"
	// CodeBarrierFailed reports a [Barrier] that refused to let the run enter
	// a handler. It is the injected-fault path, and it is a refusal like any
	// other: whatever earlier handlers wrote is the caller's to roll back.
	CodeBarrierFailed = "BARRIER_FAILED"
	// CodeIllegalRepairPath reports an instance whose current runtime status
	// has no legal path to REPAIR_REQUIRED through internal/workflow/
	// runtime's own state machine -- a terminal instance, in practice.
	CodeIllegalRepairPath = "ILLEGAL_REPAIR_PATH"
)

// Error is one typed refusal, naming the code, the artifact kind it concerns
// (empty when the refusal precedes any handler), the thing it is about and
// the underlying cause when there is one.
type Error struct {
	Code string
	Kind Kind
	Ref  string

	Detail string
	Err    error
}

func (e *Error) Error() string {
	msg := "workflow/migrate/artifacts: " + e.Code
	if e.Kind != "" {
		msg += " [" + string(e.Kind) + "]"
	}
	if e.Ref != "" {
		msg += " {" + e.Ref + "}"
	}
	msg += ": " + e.Detail
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the wrapped cause, and always the package sentinel.
func (e *Error) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrArtifacts, e.Err}
	}
	return []error{ErrArtifacts}
}

// CodeOf returns the refusal code carried by err, or "" when err is not a
// refusal from this package.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// KindOf returns the artifact kind a refusal concerns, or "" when err is not
// a refusal from this package or the refusal preceded every handler.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return ""
}

func refuse(code string, kind Kind, ref, format string, args ...any) *Error {
	return &Error{Code: code, Kind: kind, Ref: ref, Detail: fmt.Sprintf(format, args...)}
}

func wrap(code string, kind Kind, ref string, err error, format string, args ...any) *Error {
	return &Error{Code: code, Kind: kind, Ref: ref, Detail: fmt.Sprintf(format, args...), Err: err}
}

// ErrorCode reports the refusal's stable code for telemetry classification
// (internal/workflow/observe.ErrorCode); it never carries message text.
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}
