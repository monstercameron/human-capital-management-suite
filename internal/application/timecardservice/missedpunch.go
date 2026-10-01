// TCLOCK-011: workers request missed or wrong punches and the request
// routes to a scoped supervisor with segregation of duties. The pure
// request/decide state machine lives in internal/domains/timesession
// (NewMissedPunchRequest, Decide); this file adds authorization, the
// worker-directory scope check, closed-period routing to the typed reopen
// path, and idempotent persistence.
package timecardservice

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// SubmitMissedPunchRequest is one worker's self-reported missed or wrong
// punch (TCLOCK-011). SupervisorRef is the scoped supervisor the request
// routes to; the service verifies through WorkerDirectory that the
// supervisor actually supervises the worker before storing the request, so a
// worker cannot route their own request to an arbitrary approver.
type SubmitMissedPunchRequest struct {
	WorkerRef      string
	SessionID      string
	SupervisorRef  string
	Claimed        timesession.PunchKind
	ClaimedTime    time.Time
	Reason         string
	IdempotencyKey string
}

// SubmitMissedPunch validates and stores a new PENDING request.
func (s Service) SubmitMissedPunch(ctx context.Context, p *trust.Principal, req SubmitMissedPunchRequest) (timesession.MissedPunchRequest, error) {
	if err := validPrincipal(p); err != nil {
		return timesession.MissedPunchRequest{}, err
	}
	if s.MissedPunch == nil {
		return timesession.MissedPunchRequest{}, ErrUnavailable
	}
	tenant := tenantOf(p)
	if strings.TrimSpace(req.WorkerRef) == "" || strings.TrimSpace(req.SupervisorRef) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return timesession.MissedPunchRequest{}, ErrInvalidRequest
	}
	if s.Workers != nil {
		exists, err := s.Workers.ResolveWorker(ctx, tenant, req.WorkerRef)
		if err != nil {
			return timesession.MissedPunchRequest{}, err
		}
		if !exists {
			return timesession.MissedPunchRequest{}, ErrNotFound
		}
		inScope, err := s.Workers.InScope(ctx, tenant, req.SupervisorRef, req.WorkerRef)
		if err != nil {
			return timesession.MissedPunchRequest{}, err
		}
		if !inScope {
			return timesession.MissedPunchRequest{}, reject(ErrForbidden, "supervisor_ref", "OUT_OF_SCOPE", "the named supervisor does not supervise this worker")
		}
	}
	pending, err := timesession.NewMissedPunchRequest(timesession.MissedPunchRequest{
		Tenant: tenant, Worker: req.WorkerRef, SessionID: req.SessionID,
		Claimed: req.Claimed, ClaimedTime: req.ClaimedTime, Reason: req.Reason, RequestedBy: p.Subject(),
	})
	if err != nil {
		return timesession.MissedPunchRequest{}, err
	}
	stored, _, err := s.MissedPunch.Create(ctx, tenant, pending, req.SessionID, req.IdempotencyKey)
	return stored, err
}

// DecideMissedPunchRequest is a supervisor's approve/reject decision.
type DecideMissedPunchRequest struct {
	WorkerRef      string
	RequestID      string
	PeriodClosed   bool
	ReopenRef      string
	Approve        bool
	DecisionNote   string
	IdempotencyKey string
}

// DecideMissedPunch enforces segregation of duties (the deciding actor may
// never be the requester or the worker), scope (the actor must currently
// supervise WorkerRef), and the closed-period reopen route: a request
// against a closed period is neither approved nor rejected, it is routed to
// reopen, and the caller must complete that path -- named by ReopenRef --
// before a later call can decide it.
func (s Service) DecideMissedPunch(ctx context.Context, p *trust.Principal, req DecideMissedPunchRequest) (timesession.MissedPunchRequest, error) {
	if err := validPrincipal(p); err != nil {
		return timesession.MissedPunchRequest{}, err
	}
	if s.MissedPunch == nil {
		return timesession.MissedPunchRequest{}, ErrUnavailable
	}
	tenant := tenantOf(p)
	if err := s.authorize(ctx, p, tenant, req.WorkerRef, CapDecideMissedPunch); err != nil {
		return timesession.MissedPunchRequest{}, err
	}
	if err := s.requireInScope(ctx, tenant, p.Subject(), req.WorkerRef); err != nil {
		return timesession.MissedPunchRequest{}, err
	}
	current, err := s.MissedPunch.Get(ctx, tenant, req.RequestID)
	if err != nil {
		return timesession.MissedPunchRequest{}, err
	}
	if current.Tenant != tenant || current.Worker != req.WorkerRef {
		return timesession.MissedPunchRequest{}, ErrNotFound
	}
	period := timesession.PeriodOpen
	if req.PeriodClosed {
		period = timesession.PeriodClosed
	}
	decided, err := timesession.Decide(current, timesession.Approver{Actor: p.Subject(), HasApprovalScope: true}, period, req.Approve, req.DecisionNote, s.now())
	if err != nil {
		return timesession.MissedPunchRequest{}, err
	}
	if decided.State == timesession.RequestRoutedToReopen && req.ReopenRef == "" {
		return timesession.MissedPunchRequest{}, reject(ErrInvalidRequest, "reopen_ref", "MISSING", "a closed-period request routed to reopen requires a reopen reference")
	}
	stored, err := s.MissedPunch.Decide(ctx, tenant, req.RequestID, decided, p.Subject(), req.ReopenRef)
	if err != nil {
		return timesession.MissedPunchRequest{}, err
	}
	if s.Notify != nil {
		_ = s.Notify.NotifyMissedPunch(ctx, tenant, req.WorkerRef, req.RequestID, string(stored.State))
	}
	return stored, nil
}
