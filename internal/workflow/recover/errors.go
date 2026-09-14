package recover

import (
	"errors"
	"fmt"
)

// Sentinels. Classify with [errors.Is]; read [CodeOf] for the exact reason.
// Never match message text.
var (
	// ErrRecover is the sentinel every refusal from this package unwraps to.
	ErrRecover = errors.New("recover: refused")

	// ErrInvalid reports a request or a configuration that is not internally
	// consistent. It is returned before any statement runs.
	ErrInvalid = errors.New("recover: invalid request")

	// ErrLeaseLive reports a recovery attempted against a node whose lease has
	// not lapsed by the caller's own clock reading. This package refuses to
	// declare a live worker dead.
	ErrLeaseLive = errors.New("recover: lease has not lapsed")

	// ErrNotRecoverable reports a node that has no attempt to recover: no
	// attempt was ever recorded, or the latest one already finished.
	ErrNotRecoverable = errors.New("recover: node has no recoverable attempt")

	// ErrFenceRefused reports a fenced step whose fence the verifier rejected.
	// The verifier's own typed refusal is wrapped, so LEASE_LOST and
	// FENCE_STALE stay readable off the same error.
	ErrFenceRefused = errors.New("recover: lease fence refused")

	// ErrCrashed reports a deterministic [Failpoint] that fired at a declared
	// [Phase]. It is the injected process death, not a fault in the code
	// under it: read [Receipt.CrashedAt] (or [PhaseOf]) for which boundary.
	ErrCrashed = errors.New("recover: crashed at a failpoint")

	// ErrEffect reports the [Effect] port itself failing. The port's own
	// error is wrapped unchanged.
	ErrEffect = errors.New("recover: effect failed")

	// ErrStorage reports a database failure underneath a well-formed request.
	ErrStorage = errors.New("recover: storage failed")
)

// Stable refusal codes.
const (
	CodeInvalid        = "INVALID_RECOVERY_REQUEST"
	CodeLeaseLive      = "RECOVERY_LEASE_LIVE"
	CodeNotRecoverable = "NODE_NOT_RECOVERABLE"
	CodeFenceRefused   = "RECOVERY_FENCE_REFUSED"
	CodeCrashInjected  = "RECOVERY_CRASH_INJECTED"
	CodeEffectFailed   = "RECOVERY_EFFECT_FAILED"
	CodeStorageFailed  = "RECOVERY_STORAGE_FAILED"
)

// Error is one typed refusal, naming the code and the node execution it
// happened on. A crash additionally carries the [Phase] it fired at.
type Error struct {
	Code       string
	InstanceID string
	NodeID     string
	Phase      Phase
	Detail     string

	// sentinel is the package sentinel this refusal classifies as.
	sentinel error
	// err is the underlying cause, when there was one.
	err error
}

func (e *Error) Error() string {
	loc := ""
	if e.InstanceID != "" {
		loc = " on instance " + e.InstanceID
	}
	if e.NodeID != "" {
		loc += " node " + e.NodeID
	}
	if e.Phase != "" {
		loc += " at " + string(e.Phase)
	}
	msg := fmt.Sprintf("recover: %s%s: %s", e.Code, loc, e.Detail)
	if e.err != nil {
		msg += ": " + e.err.Error()
	}
	return msg
}

// Unwrap exposes the classifying sentinel, the package sentinel and any
// underlying cause.
func (e *Error) Unwrap() []error {
	out := []error{ErrRecover}
	if e.sentinel != nil {
		out = append(out, e.sentinel)
	}
	if e.err != nil {
		out = append(out, e.err)
	}
	return out
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

// PhaseOf returns the persistence boundary a crash refusal fired at, or ""
// when err is not a crash from this package.
func PhaseOf(err error) Phase {
	var e *Error
	if errors.As(err, &e) {
		return e.Phase
	}
	return ""
}

func refuse(code string, sentinel error, instanceID, nodeID, format string, args ...any) *Error {
	return &Error{
		Code: code, InstanceID: instanceID, NodeID: nodeID,
		Detail: fmt.Sprintf(format, args...), sentinel: sentinel,
	}
}

func wrap(code string, sentinel error, instanceID, nodeID string, cause error, format string, args ...any) *Error {
	e := refuse(code, sentinel, instanceID, nodeID, format, args...)
	e.err = cause
	return e
}

func invalid(format string, args ...any) *Error {
	return refuse(CodeInvalid, ErrInvalid, "", "", format, args...)
}

// ErrorCode reports the refusal's stable code for telemetry classification
// (internal/workflow/observe.ErrorCode); it never carries message text.
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}
