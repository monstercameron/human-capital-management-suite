package lease

import (
	"errors"
	"fmt"
)

// Sentinels. Classify with [errors.Is]; read [CodeOf] for the exact reason.
// Never match message text.
var (
	// ErrLease is the sentinel every refusal from this package unwraps to.
	ErrLease = errors.New("lease: refused")

	// ErrInvalid reports a request that is not internally consistent. It is
	// returned before any statement runs.
	ErrInvalid = errors.New("lease: invalid request")

	// ErrHeld reports an acquire against a resource whose lease is still live
	// in someone else's hands.
	ErrHeld = errors.New("lease: resource is held")

	// ErrLeaseLost reports a fenced operation whose holder no longer holds the
	// resource at all: the lease was released, expired or revoked, or the
	// holder's own expiry has passed by the caller's clock reading. It is
	// WF-RUN-002's LEASE_LOST.
	ErrLeaseLost = errors.New("lease: lease lost")

	// ErrFenceStale reports a fenced operation presented with a token behind
	// the resource's current one -- a superseded holder that came back. A
	// fence naming a different lease line entirely is reported with
	// [CodeFenceForeign] and also unwraps to this sentinel, because the
	// consequence is identical: the write is refused and nothing is mutated.
	ErrFenceStale = errors.New("lease: fence token stale")

	// ErrLeaseLive reports an expire attempt against a lease that has not
	// lapsed yet by the caller's own clock reading. Expiry is an observation
	// this package refuses to fake.
	ErrLeaseLive = errors.New("lease: lease has not lapsed")

	// ErrStorage reports a database failure underneath a well-formed request.
	ErrStorage = errors.New("lease: storage failed")
)

// Stable refusal codes. WF-RUN-002's GREEN clause names LEASE_LOST and
// FENCE_STALE specifically; the rest classify the refusals that happen before
// a fence is ever compared.
const (
	CodeInvalid       = "INVALID_LEASE_REQUEST"
	CodeLeaseHeld     = "LEASE_HELD"
	CodeLeaseLost     = "LEASE_LOST"
	CodeFenceStale    = "FENCE_STALE"
	CodeFenceForeign  = "FENCE_FOREIGN"
	CodeLeaseLive     = "LEASE_LIVE"
	CodeStorageFailed = "STORAGE_FAILED"
)

// Error is one typed refusal, naming the code, the resource it happened on
// and the holder that presented it.
type Error struct {
	Code         string
	ResourceKind string
	ResourceID   string
	HolderID     string
	Detail       string

	// sentinel is the package sentinel this refusal classifies as.
	sentinel error
	// err is the underlying cause, when there was one.
	err error
}

func (e *Error) Error() string {
	loc := ""
	if e.ResourceKind != "" || e.ResourceID != "" {
		loc = fmt.Sprintf(" on %s %s", e.ResourceKind, e.ResourceID)
	}
	if e.HolderID != "" {
		loc += " for holder " + e.HolderID
	}
	msg := fmt.Sprintf("lease: %s%s: %s", e.Code, loc, e.Detail)
	if e.err != nil {
		msg += ": " + e.err.Error()
	}
	return msg
}

// Unwrap exposes the classifying sentinel, the package sentinel and any
// underlying cause.
func (e *Error) Unwrap() []error {
	out := []error{ErrLease}
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

func refuse(code string, sentinel error, res Resource, holder, format string, args ...any) *Error {
	return &Error{
		Code: code, ResourceKind: res.Kind, ResourceID: res.ID, HolderID: holder,
		Detail: fmt.Sprintf(format, args...), sentinel: sentinel,
	}
}

func wrapStorage(res Resource, holder string, cause error, format string, args ...any) *Error {
	e := refuse(CodeStorageFailed, ErrStorage, res, holder, format, args...)
	e.err = cause
	return e
}

func invalid(res Resource, holder, format string, args ...any) *Error {
	return refuse(CodeInvalid, ErrInvalid, res, holder, format, args...)
}

// ErrorCode reports the refusal's stable code for telemetry classification
// (internal/workflow/observe.ErrorCode); it never carries message text.
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}
