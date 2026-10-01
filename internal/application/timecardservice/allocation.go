// FTIME-005: allocate one approved timecard's minutes exactly across
// authorized work-order/project lines, and reconcile the payroll handoff.
// The pure arithmetic and the "no source charged twice" invariant live in
// internal/domains/timecard (AllocateApprovedTime, CorrectAllocation); this
// file adds authorization, the requirement that the source timecard is
// actually APPROVED, idempotent persistence, and the accepted/rejected
// payroll receipt gate.
package timecardservice

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timecard"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AllocateTimeRequest carries one approved timecard's request to allocate.
// AuthorityDecided must be true: FTIME-001 (out of this lane's scope) is the
// authority that decides whether payroll export may run at all, and this
// service never allocates as a proxy for that decision -- a caller that has
// not yet resolved FTIME-001 authority is refused rather than defaulted to
// "allowed".
type AllocateTimeRequest struct {
	WorkerRef        string
	TimecardID       string
	AuthorityDecided bool
	Request          timecard.TimeAllocationRequest
	IdempotencyKey   string
}

// AllocateApprovedTime authorizes, verifies the timecard is APPROVED at the
// declared revision, allocates exactly across the requested lines, and
// persists the result idempotently.
func (s Service) AllocateApprovedTime(ctx context.Context, p *trust.Principal, req AllocateTimeRequest) (timecard.TimeAllocation, error) {
	if err := validPrincipal(p); err != nil {
		return timecard.TimeAllocation{}, err
	}
	if s.Timecards == nil || s.Allocations == nil {
		return timecard.TimeAllocation{}, ErrUnavailable
	}
	if !req.AuthorityDecided {
		return timecard.TimeAllocation{}, reject(ErrForbidden, "authority", "UNDECIDED", "FTIME-001 payroll authority has not been decided for this tenant")
	}
	if strings.TrimSpace(req.TimecardID) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return timecard.TimeAllocation{}, ErrInvalidRequest
	}
	tenant := tenantOf(p)
	if err := s.authorize(ctx, p, tenant, req.WorkerRef, CapAllocateTime); err != nil {
		return timecard.TimeAllocation{}, err
	}
	tc, err := s.Timecards.Get(ctx, tenant, req.TimecardID)
	if err != nil {
		return timecard.TimeAllocation{}, err
	}
	if tc.Tenant != tenant || tc.WorkerID != req.WorkerRef {
		return timecard.TimeAllocation{}, ErrNotFound
	}
	if tc.State != timecard.Approved && tc.State != timecard.Locked {
		return timecard.TimeAllocation{}, reject(ErrInvalidRequest, "state", string(tc.State), "only approved or locked time can be allocated")
	}
	if req.Request.ApprovedRevision != tc.Revision {
		return timecard.TimeAllocation{}, ErrRevisionConflict
	}
	if existing, ok, err := s.Allocations.Get(ctx, tenant, req.TimecardID); err != nil {
		return timecard.TimeAllocation{}, err
	} else if ok && existing.ApprovedRevision == req.Request.ApprovedRevision {
		return existing, nil // idempotent replay at the same revision
	}
	alloc, err := timecard.AllocateApprovedTime(req.Request)
	if err != nil {
		return timecard.TimeAllocation{}, err
	}
	return s.Allocations.Save(ctx, tenant, alloc, req.IdempotencyKey)
}

// CorrectAllocationRequest re-derives an allocation from a corrected
// timecard revision and records the exact delta against the prior one.
type CorrectAllocationRequest struct {
	WorkerRef        string
	TimecardID       string
	AuthorityDecided bool
	Request          timecard.TimeAllocationRequest
	IdempotencyKey   string
}

func (s Service) CorrectAllocation(ctx context.Context, p *trust.Principal, req CorrectAllocationRequest) (timecard.AllocationCorrection, error) {
	if err := validPrincipal(p); err != nil {
		return timecard.AllocationCorrection{}, err
	}
	if s.Allocations == nil || s.Ledger == nil {
		return timecard.AllocationCorrection{}, ErrUnavailable
	}
	if !req.AuthorityDecided {
		return timecard.AllocationCorrection{}, reject(ErrForbidden, "authority", "UNDECIDED", "FTIME-001 payroll authority has not been decided for this tenant")
	}
	tenant := tenantOf(p)
	if err := s.authorize(ctx, p, tenant, req.WorkerRef, CapAllocateTime); err != nil {
		return timecard.AllocationCorrection{}, err
	}
	prev, ok, err := s.Allocations.Get(ctx, tenant, req.TimecardID)
	if err != nil {
		return timecard.AllocationCorrection{}, err
	}
	if !ok {
		return timecard.AllocationCorrection{}, ErrNotFound
	}
	correction, err := timecard.CorrectAllocation(prev, req.Request)
	if err != nil {
		return timecard.AllocationCorrection{}, err
	}
	if _, err := s.Allocations.Save(ctx, tenant, correction.Next, req.IdempotencyKey); err != nil {
		return timecard.AllocationCorrection{}, err
	}
	if err := s.Ledger.RecordAllocationCorrection(ctx, tenant, correction, req.IdempotencyKey); err != nil {
		return timecard.AllocationCorrection{}, err
	}
	return correction, nil
}

// RecordPayrollReceiptRequest carries the accepted/rejected outcome of a
// downstream payroll export attempt for an already-allocated timecard.
// FTIME-005's GREEN requires this receipt to exist and be explicit; there is
// no implicit "assume accepted" path.
type RecordPayrollReceiptRequest struct {
	WorkerRef      string
	TimecardID     string
	Receipt        DestinationReceipt
	IdempotencyKey string
}

func (s Service) RecordPayrollReceipt(ctx context.Context, p *trust.Principal, req RecordPayrollReceiptRequest) (DestinationReceipt, error) {
	if err := validPrincipal(p); err != nil {
		return DestinationReceipt{}, err
	}
	if s.Ledger == nil {
		return DestinationReceipt{}, ErrUnavailable
	}
	tenant := tenantOf(p)
	if err := s.authorize(ctx, p, tenant, req.WorkerRef, CapAllocateTime); err != nil {
		return DestinationReceipt{}, err
	}
	if strings.TrimSpace(req.Receipt.ReceiptRef) == "" && req.Receipt.Accepted {
		return DestinationReceipt{}, reject(ErrInvalidRequest, "receipt.receipt_ref", "MISSING", "an accepted receipt requires a receipt reference")
	}
	if !req.Receipt.Accepted && strings.TrimSpace(req.Receipt.Reason) == "" {
		return DestinationReceipt{}, reject(ErrInvalidRequest, "receipt.reason", "MISSING", "a rejected receipt requires a reason")
	}
	if err := s.Ledger.RecordReceipt(ctx, tenant, req.Receipt, req.IdempotencyKey); err != nil {
		return DestinationReceipt{}, err
	}
	return req.Receipt, nil
}
