package timesession

import "errors"

// Typed conflicts for session transitions (FTIME-003). Every one is
// matchable with errors.Is; none of them is a generic "invalid request".
var (
	// ErrInvalidPunch identifies a structurally malformed punch: an
	// undeclared kind, a missing tenant/worker/actor/assignment, or a punch
	// with no device or server receipt time.
	ErrInvalidPunch = errors.New("timesession: punch is invalid")
	// ErrInvalidPolicy identifies an incomplete or nonsensical policy input
	// (for example a non-positive accuracy threshold or grace duration).
	ErrInvalidPolicy = errors.New("timesession: policy is invalid")
	// ErrAlreadyOpen is returned by an IN punch against a session that is
	// already OPEN or ON_BREAK.
	ErrAlreadyOpen = errors.New("timesession: session is already open")
	// ErrNotOpen is returned by an OUT, TRANSFER or exit-evidence action
	// against a session that is not OPEN or ON_BREAK.
	ErrNotOpen = errors.New("timesession: session is not open")
	// ErrNotOnBreak is returned by a BREAK_END/MEAL_END punch against a
	// session that is not ON_BREAK, or whose open segment is the other kind.
	ErrNotOnBreak = errors.New("timesession: session is not on the matching break or meal")
	// ErrAlreadyOnBreak is returned by a BREAK_START/MEAL_START punch
	// against a session that is already ON_BREAK.
	ErrAlreadyOnBreak = errors.New("timesession: session is already on a break or meal")
	// ErrDuplicatePunch is returned, together with the original outcome,
	// when a punch repeats an idempotency key the session already applied.
	ErrDuplicatePunch = errors.New("timesession: idempotency key already applied")
	// ErrStaleRevision is returned when a punch's expected revision does
	// not match the session's current revision.
	ErrStaleRevision = errors.New("timesession: expected revision does not match the current session revision")
	// ErrDelegationRequired is returned when a punch's actor differs from
	// its claimed worker and carries no delegation.
	ErrDelegationRequired = errors.New("timesession: clocking in another worker requires delegation")

	// ErrNoGraceStarted is returned by ExpireGrace when no auto clock-out
	// grace timer is pending on the session.
	ErrNoGraceStarted = errors.New("timesession: no auto clock-out grace is pending")
	// ErrGraceNotExpired is returned by ExpireGrace before the pending
	// grace's expiry instant, and by MissingOut before its own grace has
	// expired.
	ErrGraceNotExpired = errors.New("timesession: grace has not expired")

	// ErrSelfApprovalForbidden is returned by Decide when the approver is
	// the same identity as the requester or the affected worker.
	ErrSelfApprovalForbidden = errors.New("timesession: an approver cannot decide their own missed-punch request")
	// ErrApproverOutOfScope is returned by Decide when the approver has no
	// resolved approval scope over the requesting worker.
	ErrApproverOutOfScope = errors.New("timesession: approver has no approval scope over this worker")
	// ErrRequestNotPending is returned by Decide against a request that
	// already left the PENDING state.
	ErrRequestNotPending = errors.New("timesession: missed-punch request is not pending")
)
