// Package timecardservice is the application-service half of the
// time-keeping backlog: FTIME-004 (review, correct, approve, reopen
// timecards), WF-CAP-007 (submit/attest/approve/lock the timecard
// aggregate), FTIME-005 (allocate approved time to work orders), FTIME-006/
// FTIME-007 (create, publish, cancel and reassign crew shifts, and
// reconcile them against punches), TCLOCK-011 (missed-punch requests routed
// to a scoped supervisor with segregation of duties), WTIME-001/002 (resolve
// and pin a per-assignment time profile), WTIME-009/010/011/012 and
// TCLOCK-015 (route approved time to its profile's destination) and
// WTIME-013/014 (compute premiums and classification via timecalc).
//
// Every exported Service method authorizes before any side effect, checks
// an idempotency key against a content digest, requires the caller's
// expected revision to match the stored one, and reads the tenant from the
// authenticated actor rather than from caller-supplied data. The service
// depends only on the small ports declared in ports.go; concrete storage
// (internal/data/timestore) is wired by the orchestrator, never imported
// here.
package timecardservice

import (
	"errors"
	"fmt"
)

const contractVersion = 1

// Version reports this package's contract version (ARCH-GO-009). A change to
// the meaning or shape of a command or its result must advance this value.
func Version() int { return contractVersion }

var (
	// ErrInvalidPrincipal reports a call with no authenticated, tenant-scoped
	// actor.
	ErrInvalidPrincipal = errors.New("timecardservice: trusted principal required")
	// ErrInvalidRequest reports a request that fails its own shape checks,
	// independent of authorization or storage.
	ErrInvalidRequest = errors.New("timecardservice: invalid request")
	// ErrUnavailable reports a required port left unset by the caller.
	ErrUnavailable = errors.New("timecardservice: required port unavailable")
	// ErrForbidden reports an authorization refusal: cross-tenant access, a
	// manager outside their scope, or self-approval.
	ErrForbidden = errors.New("timecardservice: forbidden")
	// ErrNotFound reports a lookup for a record that either never existed or
	// belongs to a different tenant; the two are indistinguishable to the
	// caller by design.
	ErrNotFound = errors.New("timecardservice: not found")
	// ErrRevisionConflict reports a caller's expected revision that no longer
	// matches the stored revision.
	ErrRevisionConflict = errors.New("timecardservice: revision conflict")
	// ErrIdempotencyConflict reports the same idempotency key replayed with a
	// different command digest.
	ErrIdempotencyConflict = errors.New("timecardservice: idempotency key reused with a different request")
	// ErrUnresolvedException reports an approval attempt blocked by an open
	// exception.
	ErrUnresolvedException = errors.New("timecardservice: unresolved exception blocks approval")
	// ErrDestinationRejected reports a destination dispatcher's rejection of
	// routed time.
	ErrDestinationRejected = errors.New("timecardservice: destination rejected routed time")
	// ErrNoDestination reports a profile destination with no bound
	// Dispatcher.
	ErrNoDestination = errors.New("timecardservice: no dispatcher bound for destination")
)

// Rejection is the stable failure shape every command-level refusal in this
// package can carry: which field, in what state, and why.
type Rejection struct {
	Field  string
	State  string
	Reason string
	Err    error
}

func (r *Rejection) Error() string {
	return fmt.Sprintf("%v: field=%s state=%s: %s", r.Err, r.Field, r.State, r.Reason)
}

// Unwrap exposes the wrapped sentinel to errors.Is.
func (r *Rejection) Unwrap() error { return r.Err }

func reject(err error, field, state, reason string) error {
	return &Rejection{Field: field, State: state, Reason: reason, Err: err}
}
