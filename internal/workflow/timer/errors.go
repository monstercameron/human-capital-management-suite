package timer

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Sentinels. Classify with [errors.Is]; read [CodeOf] for the exact reason.
var (
	// ErrTimer is the sentinel every refusal from this package unwraps to.
	ErrTimer = errors.New("timer: refused")

	// ErrInvalid reports a request that is not internally consistent. It is
	// returned before any statement runs.
	ErrInvalid = errors.New("timer: invalid request")

	// ErrReviewRequired reports a wake requirement that could not be resolved
	// to an instant at all -- a DST-ambiguous or non-existent local time
	// under REJECT_GAP. It is not a timer: there is no instant to promise, and
	// internal/workflow/steps/wait's own TIMER_REVIEW_REQUIRED route is where
	// such a node goes.
	ErrReviewRequired = errors.New("timer: wake requirement needs review")

	// ErrRequirementDrift reports a settle whose caller-held requirement
	// digest does not match the durable timer's key: the dataset or the
	// compiled wake condition changed under the promise.
	ErrRequirementDrift = errors.New("timer: requirement digest drift")

	// ErrNotFound reports a timer that does not exist for the tenant.
	ErrNotFound = errors.New("timer: not found")

	// ErrAlreadySettled reports a timer that already fired or was cancelled.
	// It is what a losing concurrent [Scheduler.Fire] gets, and it is the
	// reason a fired timer wakes a node once rather than twice.
	ErrAlreadySettled = errors.New("timer: already settled")

	// ErrMisfirePolicyRequired reports a fire with no declared misfire
	// policy. What to do with an overdue promise is a decision, and this
	// package refuses to make it silently.
	ErrMisfirePolicyRequired = errors.New("timer: misfire policy required")

	// ErrStorage reports a database failure underneath a well-formed request.
	ErrStorage = errors.New("timer: storage failed")
)

// Stable refusal codes.
const (
	CodeInvalid                = "INVALID_TIMER_REQUEST"
	CodeReviewRequired         = "TIMER_REVIEW_REQUIRED"
	CodeRequirementDrift       = "TIMER_REQUIREMENT_DRIFT"
	CodeNotFound               = "TIMER_NOT_FOUND"
	CodeAlreadySettled         = "TIMER_ALREADY_SETTLED"
	CodeMisfirePolicyRequired  = "MISFIRE_POLICY_REQUIRED"
	CodeFenceRefused           = "FENCE_REFUSED"
	CodeStorageFailed          = "STORAGE_FAILED"
	CodeReadyWorkConflict      = "READY_WORK_CONFLICT"
	CodeAttemptResolutionError = "ATTEMPT_RESOLUTION_FAILED"
)

// Error is one typed refusal naming the code, the timer it happened to and
// the node the timer belongs to.
type Error struct {
	Code       string
	TimerID    uuid.UUID
	InstanceID uuid.UUID
	NodeID     string
	Detail     string

	sentinel error
	err      error
}

func (e *Error) Error() string {
	loc := ""
	if e.InstanceID != uuid.Nil {
		loc = " for instance " + e.InstanceID.String()
	}
	if e.NodeID != "" {
		loc += " at node " + e.NodeID
	}
	if e.TimerID != uuid.Nil {
		loc += " (timer " + e.TimerID.String() + ")"
	}
	msg := fmt.Sprintf("timer: %s%s: %s", e.Code, loc, e.Detail)
	if e.err != nil {
		msg += ": " + e.err.Error()
	}
	return msg
}

// Unwrap exposes the classifying sentinel, the package sentinel and the
// underlying cause.
func (e *Error) Unwrap() []error {
	out := []error{ErrTimer}
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

type location struct {
	timerID    uuid.UUID
	instanceID uuid.UUID
	nodeID     string
}

func refuse(code string, sentinel error, loc location, format string, args ...any) *Error {
	return &Error{
		Code: code, TimerID: loc.timerID, InstanceID: loc.instanceID, NodeID: loc.nodeID,
		Detail: fmt.Sprintf(format, args...), sentinel: sentinel,
	}
}

func wrapErr(code string, sentinel error, loc location, cause error, format string, args ...any) *Error {
	e := refuse(code, sentinel, loc, format, args...)
	e.err = cause
	return e
}

func invalid(loc location, format string, args ...any) *Error {
	return refuse(CodeInvalid, ErrInvalid, loc, format, args...)
}

// ErrorCode reports the refusal's stable code for telemetry classification
// (internal/workflow/observe.ErrorCode); it never carries message text.
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}
