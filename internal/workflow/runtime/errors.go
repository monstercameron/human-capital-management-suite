package runtime

import (
	"errors"
	"fmt"
)

// Sentinels. Classify with [errors.Is]; read [Error.Code] for the exact
// reason. Never match message text.
var (
	// ErrRuntime is the sentinel every refusal from this package unwraps to.
	ErrRuntime = errors.New("runtime: write refused")
)

// Stable refusal codes.
const (
	// CodeStaleInstance is the contract code WF-RUN-001 names for a writer
	// whose view of the instance version has been overtaken. It is the whole
	// concurrency story of this package: no lease, no fence, one compare-and-set.
	CodeStaleInstance = "CONFLICT_STALE_INSTANCE"
	// CodeIllegalTransition reports a state change the spec's instance or node
	// state machine does not allow.
	CodeIllegalTransition = "ILLEGAL_TRANSITION"
	// CodeInstanceNotFound reports an instance that does not exist for the
	// tenant, which is also what a cross-tenant read looks like.
	CodeInstanceNotFound = "INSTANCE_NOT_FOUND"
	// CodeNodeExecutionNotFound reports a node execution that does not exist.
	CodeNodeExecutionNotFound = "NODE_EXECUTION_NOT_FOUND"
	// CodeInvalidRecord reports a record this package refuses to store because
	// it is incomplete or self-contradictory.
	CodeInvalidRecord = "INVALID_RECORD"
	// CodeStorageFailed reports a database failure underneath a well-formed
	// request.
	CodeStorageFailed = "STORAGE_FAILED"
)

// Error is one typed refusal, naming the code, the instance and node it
// happened at, and the underlying cause.
type Error struct {
	Code       string
	InstanceID string
	NodeID     string
	Detail     string
	Err        error
}

func (e *Error) Error() string {
	loc := ""
	if e.InstanceID != "" {
		loc = " for instance " + e.InstanceID
	}
	if e.NodeID != "" {
		loc += " at node " + e.NodeID
	}
	msg := fmt.Sprintf("runtime: %s%s: %s", e.Code, loc, e.Detail)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the wrapped cause, and always the package sentinel.
func (e *Error) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrRuntime, e.Err}
	}
	return []error{ErrRuntime}
}

// refuse builds a typed refusal.
func refuse(code, instanceID, nodeID, format string, args ...any) *Error {
	return &Error{Code: code, InstanceID: instanceID, NodeID: nodeID, Detail: fmt.Sprintf(format, args...)}
}

// wrap builds a typed refusal around an underlying cause.
func wrap(code, instanceID, nodeID string, err error, format string, args ...any) *Error {
	return &Error{
		Code:       code,
		InstanceID: instanceID,
		NodeID:     nodeID,
		Detail:     fmt.Sprintf(format, args...),
		Err:        err,
	}
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

// ErrorCode reports the refusal's stable code for telemetry classification
// (internal/workflow/observe.ErrorCode); it never carries message text.
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}
