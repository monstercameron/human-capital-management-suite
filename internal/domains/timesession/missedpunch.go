package timesession

import (
	"fmt"
	"strings"
	"time"
)

// PeriodState is whether the pay period a missed-punch request targets is
// still open for direct correction or has already closed.
type PeriodState string

const (
	PeriodOpen   PeriodState = "OPEN"
	PeriodClosed PeriodState = "CLOSED"
)

func (p PeriodState) Valid() bool { return p == PeriodOpen || p == PeriodClosed }

// MissedPunchRequestState is the closed vocabulary of a worker's
// missed/wrong-punch request.
type MissedPunchRequestState string

const (
	RequestPending        MissedPunchRequestState = "PENDING"
	RequestApproved       MissedPunchRequestState = "APPROVED"
	RequestRejected       MissedPunchRequestState = "REJECTED"
	RequestRoutedToReopen MissedPunchRequestState = "ROUTED_TO_REOPEN"
)

// MissedPunchRequest is a worker's self-reported missed or wrong punch
// (TCLOCK-011). Its ClaimedTime never becomes ApprovedTime on its own: only
// Decide's APPROVED branch sets ApprovedTime, and it never changes it while
// the request stays PENDING.
type MissedPunchRequest struct {
	Tenant      string
	Worker      string
	SessionID   string
	Claimed     PunchKind
	ClaimedTime time.Time
	Reason      string
	RequestedBy string

	State        MissedPunchRequestState
	ApprovedTime time.Time
	Decision     string
}

// Validate checks the request is well-formed. It does not check period
// state or approver scope; those belong to Decide.
func (r MissedPunchRequest) Validate() error {
	if strings.TrimSpace(r.Tenant) == "" || strings.TrimSpace(r.Worker) == "" || strings.TrimSpace(r.SessionID) == "" {
		return fmt.Errorf("%w: tenant, worker and session id are required", ErrInvalidPunch)
	}
	if !r.Claimed.Valid() {
		return fmt.Errorf("%w: claimed punch kind %q is not declared", ErrInvalidPunch, r.Claimed)
	}
	if r.ClaimedTime.IsZero() || strings.TrimSpace(r.Reason) == "" {
		return fmt.Errorf("%w: claimed time and reason are required", ErrInvalidPunch)
	}
	if strings.TrimSpace(r.RequestedBy) == "" {
		return fmt.Errorf("%w: requested_by is required", ErrInvalidPunch)
	}
	return nil
}

// NewMissedPunchRequest validates and returns a PENDING request. Its
// approved time and decision stay zero: only a later Decide call can set
// them, and never while the request is still pending.
func NewMissedPunchRequest(r MissedPunchRequest) (MissedPunchRequest, error) {
	if err := r.Validate(); err != nil {
		return MissedPunchRequest{}, err
	}
	r.State = RequestPending
	r.ApprovedTime = time.Time{}
	r.Decision = ""
	return r, nil
}

// Approver is the resolved supervisor deciding a request. Whether Actor
// actually supervises the worker is resolved by the caller (Human Work
// scope); this package only enforces that the resolved approver is not the
// requester or worker, and that HasApprovalScope is true.
type Approver struct {
	Actor            string
	HasApprovalScope bool
}

// Decide applies segregation of duties and the closed-period reopen route
// to one pending request. Self-approval is refused before anything else,
// including for a closed period: it is a security invariant, not a routing
// concern. A request against a closed period is neither approved nor
// rejected here; it is routed to reopen, and the caller must complete that
// path before Decide can be called again for it. A request that is not
// PENDING cannot be decided again.
func Decide(request MissedPunchRequest, approver Approver, period PeriodState, approve bool, decisionNote string, now time.Time) (MissedPunchRequest, error) {
	if request.State != RequestPending {
		return request, fmt.Errorf("%w: request for session %s is %s", ErrRequestNotPending, request.SessionID, request.State)
	}
	if !period.Valid() {
		return request, fmt.Errorf("%w: period state %q is not declared", ErrInvalidPunch, period)
	}
	if strings.TrimSpace(approver.Actor) == "" {
		return request, fmt.Errorf("%w: approver is required", ErrInvalidPunch)
	}
	if now.IsZero() {
		return request, fmt.Errorf("%w: now is required", ErrInvalidPunch)
	}
	if approver.Actor == request.RequestedBy || approver.Actor == request.Worker {
		return request, fmt.Errorf("%w: actor=%s", ErrSelfApprovalForbidden, approver.Actor)
	}
	next := request
	if period == PeriodClosed {
		next.State = RequestRoutedToReopen
		next.Decision = "closed period: routed through reopen before this request can be decided"
		return next, nil
	}
	if !approver.HasApprovalScope {
		return request, fmt.Errorf("%w: actor=%s", ErrApproverOutOfScope, approver.Actor)
	}
	if approve {
		next.State = RequestApproved
		next.ApprovedTime = request.ClaimedTime
		next.Decision = decisionNote
		return next, nil
	}
	next.State = RequestRejected
	next.ApprovedTime = time.Time{}
	next.Decision = decisionNote
	return next, nil
}
