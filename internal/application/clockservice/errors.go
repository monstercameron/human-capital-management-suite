// Package clockservice is the application-service half of the time-clock
// backlog: FTIME-003 (authenticated ClockIn/StartBreak/EndBreak/
// TransferJob/ClockOut), TCLOCK-002 (device enrollment and credential
// lifecycle), TCLOCK-003 (batched punch ingest with per-punch receipts),
// TCLOCK-004 (roster/policy sync to devices), TCLOCK-005 (shared-device
// worker identification), TCLOCK-008 (device fleet heartbeat), TCLOCK-010's
// service part (attestations, tips, job transfer consequences) and
// WTIME-004 (synchronous punch acknowledgement over the workflow runtime).
//
// Every use case here follows the same shape: resolve identity and
// authorization, apply the pure domain transition
// (internal/domains/timesession, internal/domains/punchpolicy,
// internal/domains/clock, internal/domains/geofence), commit it through a
// port, and hand any further consequence to another port. The service
// package declares every port it depends on; it never imports a concrete
// store, and the composition root (not this package) supplies the
// adapters that bind a real database.
package clockservice

import (
	"errors"
	"fmt"
)

// Sentinel errors. Every rejection wraps exactly one of these so a caller
// can fail closed with errors.Is instead of matching on message text.
var (
	// ErrInvalidPrincipal identifies a call with no authenticated tenant or
	// subject.
	ErrInvalidPrincipal = errors.New("clockservice: trusted principal required")
	// ErrInvalidRequest identifies a structurally incomplete or malformed
	// request.
	ErrInvalidRequest = errors.New("clockservice: invalid request")
	// ErrUnavailable identifies a required port that was not wired.
	ErrUnavailable = errors.New("clockservice: required port unavailable")
	// ErrWorkerNotEligible identifies a claimed worker that does not
	// resolve to an active, eligible worker.
	ErrWorkerNotEligible = errors.New("clockservice: worker is not eligible")
	// ErrAssignmentNotFound identifies an assignment that does not resolve
	// for the resolved worker.
	ErrAssignmentNotFound = errors.New("clockservice: assignment does not resolve for this worker")
	// ErrDelegationRequired identifies a punch whose actor differs from its
	// claimed worker with no current delegation authorizing it.
	ErrDelegationRequired = errors.New("clockservice: clocking in another worker requires delegation")
	// ErrDeviceNotEligible identifies a device that is suspended, revoked,
	// or otherwise not able to accept the requested action.
	ErrDeviceNotEligible = errors.New("clockservice: device is not eligible for this action")
	// ErrWorkerNotOnRoster identifies a device submitting a punch for a
	// worker outside its synced roster.
	ErrWorkerNotOnRoster = errors.New("clockservice: worker is not on this device's roster")
	// ErrLockedOut identifies a device or worker identification scope that
	// is currently rate-limit locked out.
	ErrLockedOut = errors.New("clockservice: identification scope is locked out")
	// ErrRetryLater is the typed overload signal WTIME-004 requires: the
	// punch was never committed and never lost, and the caller (device)
	// must keep it queued and retry with the same idempotency key.
	ErrRetryLater = errors.New("clockservice: overloaded, keep the punch queued and retry")
	// ErrSelfApproval identifies a supervisor override or missed-punch
	// decision where the deciding actor is the affected worker.
	ErrSelfApproval = errors.New("clockservice: an actor cannot approve their own request")
)

// Rejection is the stable, machine-readable failure shape returned by every
// use case in this package. Field/State/Reason make the exact governance
// fact explainable without leaking credential material; Err is always one
// of this package's sentinels so callers can use errors.Is.
type Rejection struct {
	Err    error
	Field  string
	State  string
	Reason string
}

func (r *Rejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s: %s", r.Err, r.Field, r.State, r.Reason)
}

func (r *Rejection) Unwrap() error { return r.Err }

func reject(err error, field, state, reason string) error {
	return &Rejection{Err: err, Field: field, State: state, Reason: reason}
}
