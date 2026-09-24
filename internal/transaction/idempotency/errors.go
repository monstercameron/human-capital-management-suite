package idempotency

import (
	"errors"
	"fmt"
)

// ErrIdempotency is the sentinel every refusal from this package unwraps to.
// Classify with [errors.Is]; read [CodeOf] or [Error.Code] for the exact
// reason. Never match message text.
var ErrIdempotency = errors.New("idempotency: refused")

// Stable refusal codes.
const (
	// CodeConflict is TX-006's GREEN clause: the same (tenant, capability,
	// effect scope, key) was reserved under a different canonical request
	// digest. Nothing is mutated when this code is returned.
	CodeConflict = "IDEMPOTENCY_CONFLICT"
	// CodeTombstoned means the key remains permanently bound to its request
	// digest, but its detailed result was compacted after expiry.
	CodeTombstoned = "IDEMPOTENCY_TOMBSTONED"
	// CodeRetentionPolicyMissing means a protected capability has no
	// authoritative entry in the capability retention registry.
	CodeRetentionPolicyMissing = "IDEMPOTENCY_RETENTION_POLICY_MISSING"
	// CodeRetentionPolicyConflict means a caller-provided class disagrees with
	// the class registered for that capability.
	CodeRetentionPolicyConflict = "IDEMPOTENCY_RETENTION_POLICY_CONFLICT"
	// CodeRetentionTooShort reports a [RetentionPolicy] whose Retention would
	// expire before its own declared RetryWindow closes. Refused at Reserve
	// time, before any row is written.
	CodeRetentionTooShort = "IDEMPOTENCY_RETENTION_TOO_SHORT"
	// CodeInProgress reports a still-RESERVED record under the same scope and
	// the same digest: another attempt has reserved the effect but not yet
	// completed it. [Guard]'s own atomic Reserve-run-Complete pattern never
	// leaves a committed row in this state, so a caller only ever observes it
	// when something outside Guard called [Store.Reserve] directly and has
	// not yet called [Store.Complete].
	CodeInProgress = "IDEMPOTENCY_IN_PROGRESS"
	// CodeInvalidScope reports a [Scope] missing a required dimension.
	CodeInvalidScope = "IDEMPOTENCY_INVALID_SCOPE"
	// CodeInvalidRecord reports a record this package refuses to store
	// because it is incomplete or self-contradictory (an invalid retention
	// policy, a malformed digest, a completion with no result identity).
	CodeInvalidRecord = "IDEMPOTENCY_INVALID_RECORD"
	// CodeNotReserved reports a [Store.Complete] call against a scope that
	// has no matching RESERVED record -- already completed, expired, or
	// never reserved at all.
	CodeNotReserved = "IDEMPOTENCY_NOT_RESERVED"
	// CodeStorageFailed reports a database failure underneath a well-formed
	// request.
	CodeStorageFailed = "IDEMPOTENCY_STORAGE_FAILED"
)

// Error is one typed refusal, naming the code, the scope it happened at and
// the underlying cause. A conflict additionally carries both digests so a
// caller can explain the mismatch without a second lookup.
type Error struct {
	Code           string
	Scope          Scope
	RecordedDigest string
	RequestDigest  string
	Detail         string
	Err            error
}

func (e *Error) Error() string {
	loc := ""
	if e.Scope != (Scope{}) {
		loc = " for scope " + e.Scope.String()
	}
	msg := fmt.Sprintf("idempotency: %s%s: %s", e.Code, loc, e.Detail)
	if e.Code == CodeConflict {
		msg = fmt.Sprintf("%s (recorded digest %s, request digest %s)", msg, e.RecordedDigest, e.RequestDigest)
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the wrapped cause, and always the package sentinel.
func (e *Error) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrIdempotency, e.Err}
	}
	return []error{ErrIdempotency}
}

// refuse builds a typed refusal with no underlying cause.
func refuse(code string, scope Scope, format string, args ...any) *Error {
	return &Error{Code: code, Scope: scope, Detail: fmt.Sprintf(format, args...)}
}

// wrap builds a typed refusal around an underlying cause.
func wrap(code string, scope Scope, err error, format string, args ...any) *Error {
	return &Error{Code: code, Scope: scope, Detail: fmt.Sprintf(format, args...), Err: err}
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
