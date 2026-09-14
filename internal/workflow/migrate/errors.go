package migrate

import (
	"errors"
	"fmt"
)

// ErrMigrate is the sentinel every refusal from this package unwraps to.
// Classify with [errors.Is]; read [CodeOf] for the exact reason. Never match
// message text.
var ErrMigrate = errors.New("workflow/migrate: rejected")

// Stable refusal codes.
const (
	// CodeInvalidRequest reports a malformed [Request] or [Approval]: this
	// package refuses before touching storage.
	CodeInvalidRequest = "INVALID_REQUEST"
	// CodePlanMismatch reports a presented plan whose workflow id, digest or
	// declared target step disagrees with what the instance, the preview or
	// the target plan itself actually carries.
	CodePlanMismatch = "PLAN_MISMATCH"
	// CodeUnsafePoint reports an instance that is not PAUSED, is PAUSED but
	// not at an eligible safe point, or carries no durable checkpoint
	// describing its exact current version and frontier.
	CodeUnsafePoint = "UNSAFE_POINT"
	// CodeMultiNodeFrontier reports an instance whose frontier carries more
	// than one node. This package migrates only a single-node frontier.
	CodeMultiNodeFrontier = "MULTI_NODE_FRONTIER"
	// CodeNotPreviewed reports an instance the presented [PreviewRecord]
	// never classified.
	CodeNotPreviewed = "NOT_PREVIEWED"
	// CodeChangedInstance reports an instance whose current frontier node or
	// stage no longer matches what the preview classified: the instance moved
	// on after the preview was taken.
	CodeChangedInstance = "CHANGED_INSTANCE"
	// CodeStalePreview reports a source or target compiled-plan digest that
	// has moved since the preview was taken.
	CodeStalePreview = "STALE_PREVIEW"
	// CodeUnapprovedDigest reports an [Approval] naming a preview digest that
	// does not match the presented [PreviewRecord].
	CodeUnapprovedDigest = "UNAPPROVED_DIGEST"
	// CodeSeparationOfDuties reports an approver who is also the principal
	// executing the migration.
	CodeSeparationOfDuties = "SEPARATION_OF_DUTIES"
	// CodeStranded reports a preview classification of STRANDED (IMPOSSIBLE):
	// no legal target state exists, so there is nothing to migrate to.
	CodeStranded = "STRANDED"
	// CodeRequiresRepair reports a preview classification of REQUIRES_REPAIR,
	// or a SAFE/TRANSFORMABLE assessment naming no target state: neither
	// migrates without a fresh, deterministic bridge.
	CodeRequiresRepair = "REQUIRES_REPAIR"
	// CodeInvalidBridgeStage reports a declared target stage that is not a
	// declared, active runtime node status -- a bridge cannot land an
	// instance somewhere [runtime.Advance] could never resume it from.
	CodeInvalidBridgeStage = "INVALID_BRIDGE_STAGE"
	// CodeUnsupportedBridgeSource reports a cross-node bridge attempted from
	// a RETRYING attempt, which this package refuses rather than guess how to
	// retire it.
	CodeUnsupportedBridgeSource = "UNSUPPORTED_BRIDGE_SOURCE"
)

// Error is one typed refusal, naming the code, the instance it concerns and
// the underlying cause when there is one.
type Error struct {
	Code   string
	Ref    string
	Detail string
	Err    error
}

func (e *Error) Error() string {
	msg := "workflow/migrate: " + e.Code
	if e.Ref != "" {
		msg += " [" + e.Ref + "]"
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
		return []error{ErrMigrate, e.Err}
	}
	return []error{ErrMigrate}
}

// CodeOf returns the refusal code carried by err, or "" when err is not a
// refusal from this package -- including when it is a refusal from
// internal/workflow/runtime or internal/data/runtimestate that this package
// deliberately returns unwrapped; check those with their own CodeOf/sentinel.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func refuse(code, ref, format string, args ...any) *Error {
	return &Error{Code: code, Ref: ref, Detail: fmt.Sprintf(format, args...)}
}

func wrap(code, ref string, err error, format string, args ...any) *Error {
	return &Error{Code: code, Ref: ref, Detail: fmt.Sprintf(format, args...), Err: err}
}

// ErrorCode reports the refusal's stable code for telemetry classification
// (internal/workflow/observe.ErrorCode); it never carries message text.
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}
