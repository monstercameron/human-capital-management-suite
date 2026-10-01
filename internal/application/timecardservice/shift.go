// FTIME-006 (create and publish revisioned crew shifts) and FTIME-007
// (reconcile schedule changes and attendance exceptions) share the
// crewshift.Shift aggregate and its ShiftStore port.
package timecardservice

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/crewshift"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// CreateShiftRequest opens a new draft shift.
type CreateShiftRequest struct {
	Shift          crewshift.Shift
	IdempotencyKey string
}

// CreateShift authorizes publish-class access (drafting is the first step
// toward publication) and persists a new draft.
func (s Service) CreateShift(ctx context.Context, p *trust.Principal, req CreateShiftRequest) (crewshift.Shift, error) {
	if err := validPrincipal(p); err != nil {
		return crewshift.Shift{}, err
	}
	if s.Shifts == nil {
		return crewshift.Shift{}, ErrUnavailable
	}
	tenant := tenantOf(p)
	if err := s.authorize(ctx, p, tenant, req.Shift.WorkerRef.String(), CapPublishShift); err != nil {
		return crewshift.Shift{}, err
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return crewshift.Shift{}, ErrInvalidRequest
	}
	if err := req.Shift.Validate(); err != nil {
		return crewshift.Shift{}, err
	}
	return s.Shifts.Create(ctx, tenant, req.Shift, req.IdempotencyKey)
}

// PublishShiftRequest carries a draft's publication attempt. Checks is the
// caller-assembled list including crewshift.DefaultPublishChecks() plus any
// plugged-in checks (minors, EU rest); the service does not choose them.
type PublishShiftRequest struct {
	WorkerRef        string
	ShiftID          string
	ExpectedRevision int64
	Input            crewshift.PublishInput
	Checks           []crewshift.PublishCheck
	IdempotencyKey   string
}

func (s Service) PublishShift(ctx context.Context, p *trust.Principal, req PublishShiftRequest) (crewshift.Shift, error) {
	if err := validPrincipal(p); err != nil {
		return crewshift.Shift{}, err
	}
	result, _, err := s.shiftLifecycle(ctx, p, req.WorkerRef, req.ShiftID, req.ExpectedRevision, req.IdempotencyKey, CapPublishShift, req,
		func(cur crewshift.Shift, hist crewshift.History) (crewshift.LifecycleResult, error) {
			in := req.Input
			in.Shift = cur
			in.Now = s.now()
			outcome, err := crewshift.Publish(in, req.Checks)
			if err != nil {
				return crewshift.LifecycleResult{}, err
			}
			return crewshift.LifecycleResult{Updated: outcome.Shift, History: hist.Append(cur)}, nil
		})
	return result, err
}

// RepublishShiftRequest re-approves an already published shift (for example
// after a break-plan correction) through ApplyLifecycle's ActionPublish
// branch, which advances revision, approver and instant without changing
// status.
type RepublishShiftRequest struct {
	WorkerRef        string
	ShiftID          string
	ExpectedRevision int64
	Grant            crewshift.Grant
	Reason           string
	IdempotencyKey   string
}

func (s Service) RepublishShift(ctx context.Context, p *trust.Principal, req RepublishShiftRequest) (crewshift.Shift, error) {
	if err := validPrincipal(p); err != nil {
		return crewshift.Shift{}, err
	}
	result, _, err := s.applyLifecycle(ctx, p, req.WorkerRef, req.ShiftID, req.ExpectedRevision, req.IdempotencyKey, CapPublishShift, req,
		crewshift.ActionPublish, req.Grant, req.Reason, crewshift.EligibilityFacts{}, "")
	return result, err
}

// CancelShiftRequest cancels a published shift.
type CancelShiftRequest struct {
	WorkerRef        string
	ShiftID          string
	ExpectedRevision int64
	Grant            crewshift.Grant
	Reason           string
	IdempotencyKey   string
}

func (s Service) CancelShift(ctx context.Context, p *trust.Principal, req CancelShiftRequest) (crewshift.Shift, error) {
	if err := validPrincipal(p); err != nil {
		return crewshift.Shift{}, err
	}
	result, notifications, err := s.applyLifecycle(ctx, p, req.WorkerRef, req.ShiftID, req.ExpectedRevision, req.IdempotencyKey, CapCancelShift, req,
		crewshift.ActionCancel, req.Grant, req.Reason, crewshift.EligibilityFacts{}, "")
	if err == nil {
		s.emitShiftNotifications(ctx, tenantOf(p), notifications)
	}
	return result, err
}

// ReassignShiftRequest moves a published shift to a new worker.
type ReassignShiftRequest struct {
	WorkerRef            string
	ShiftID              string
	ExpectedRevision     int64
	Grant                crewshift.Grant
	NewWorkerRef         string
	NewWorkerEligibility crewshift.EligibilityFacts
	Reason               string
	IdempotencyKey       string
}

func (s Service) ReassignShift(ctx context.Context, p *trust.Principal, req ReassignShiftRequest) (crewshift.Shift, error) {
	if err := validPrincipal(p); err != nil {
		return crewshift.Shift{}, err
	}
	result, notifications, err := s.applyLifecycle(ctx, p, req.WorkerRef, req.ShiftID, req.ExpectedRevision, req.IdempotencyKey, CapReassignShift, req,
		crewshift.ActionReassign, req.Grant, req.Reason, req.NewWorkerEligibility, req.NewWorkerRef)
	if err == nil {
		s.emitShiftNotifications(ctx, tenantOf(p), notifications)
	}
	return result, err
}

// shiftLifecycle is the common authorize/load/execute path for both
// PublishShift's first publication and every later lifecycle action.
func (s Service) shiftLifecycle(ctx context.Context, p *trust.Principal, workerRef, shiftID string, expectedRevision int64, idempotencyKey string, cap Capability, cmd any,
	mutate func(crewshift.Shift, crewshift.History) (crewshift.LifecycleResult, error)) (crewshift.Shift, []crewshift.Notification, error) {
	if s.Shifts == nil {
		return crewshift.Shift{}, nil, ErrUnavailable
	}
	if strings.TrimSpace(shiftID) == "" || strings.TrimSpace(idempotencyKey) == "" {
		return crewshift.Shift{}, nil, ErrInvalidRequest
	}
	tenant := tenantOf(p)
	if err := s.authorize(ctx, p, tenant, workerRef, cap); err != nil {
		return crewshift.Shift{}, nil, err
	}
	current, err := s.Shifts.Get(ctx, tenant, shiftID)
	if err != nil {
		return crewshift.Shift{}, nil, err
	}
	if current.Tenant.String() != tenant {
		return crewshift.Shift{}, nil, ErrNotFound
	}
	d := digest(cmd)
	result, err := s.Shifts.Execute(ctx, tenant, shiftID, p.Subject(), idempotencyKey, d, current.Revision, mutate)
	if err != nil {
		return crewshift.Shift{}, nil, err
	}
	return result.Updated, result.Notifications, nil
}

// applyLifecycle drives crewshift.ApplyLifecycle for cancel, reassign and
// republish through the shared shiftLifecycle path.
func (s Service) applyLifecycle(ctx context.Context, p *trust.Principal, workerRef, shiftID string, expectedRevision int64, idempotencyKey string, cap Capability, cmd any,
	action crewshift.LifecycleAction, grant crewshift.Grant, reason string, newEligibility crewshift.EligibilityFacts, newWorkerRef string) (crewshift.Shift, []crewshift.Notification, error) {
	return s.shiftLifecycle(ctx, p, workerRef, shiftID, expectedRevision, idempotencyKey, cap, cmd,
		func(cur crewshift.Shift, hist crewshift.History) (crewshift.LifecycleResult, error) {
			req := crewshift.LifecycleRequest{
				Action: action, Current: cur, ExpectedRevision: expectedRevision, Grant: grant, Now: s.now(), Reason: reason,
				NewWorkerEligibility: newEligibility,
			}
			if newWorkerRef != "" {
				req.NewWorkerRef = cur.WorkerRef // tenant/kind carried from current; id replaced by caller-supplied ref below
				req.NewWorkerRef.Id = newWorkerRef
			}
			return crewshift.ApplyLifecycle(req, hist)
		})
}

func (s Service) emitShiftNotifications(ctx context.Context, tenant string, notifications []crewshift.Notification) {
	if s.Notify == nil {
		return
	}
	for _, n := range notifications {
		_ = s.Notify.NotifyShift(ctx, tenant, n)
	}
}

// ReconcileShiftRequest compares observed punches against a published
// shift.
type ReconcileShiftRequest struct {
	WorkerRef string
	ShiftID   string
	Tolerance crewshift.Tolerance
	Punches   []crewshift.PunchInterval
}

// ReconcileShift authorizes read/reconcile access and reports FTIME-007
// findings. It never rewrites the shift or a punch.
func (s Service) ReconcileShift(ctx context.Context, p *trust.Principal, req ReconcileShiftRequest) (crewshift.ReconcileResult, error) {
	if err := validPrincipal(p); err != nil {
		return crewshift.ReconcileResult{}, err
	}
	if s.Shifts == nil {
		return crewshift.ReconcileResult{}, ErrUnavailable
	}
	tenant := tenantOf(p)
	if err := s.authorize(ctx, p, tenant, req.WorkerRef, CapReconcileShift); err != nil {
		return crewshift.ReconcileResult{}, err
	}
	shift, err := s.Shifts.Get(ctx, tenant, req.ShiftID)
	if err != nil {
		return crewshift.ReconcileResult{}, err
	}
	if shift.Tenant.String() != tenant {
		return crewshift.ReconcileResult{}, ErrNotFound
	}
	return crewshift.Reconcile(shift, req.Tolerance, req.Punches)
}
