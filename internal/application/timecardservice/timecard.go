// FTIME-004 (review, correct, approve, reopen) and WF-CAP-007 (submit,
// attest, approve, lock) both operate on the one timecard.Timecard
// aggregate; this file is their shared application-service boundary.
package timecardservice

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/attendance"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timecard"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// CreateTimecardRequest opens a fresh draft timecard for one worker's
// period.
type CreateTimecardRequest struct {
	TimecardID     string
	WorkerRef      string
	AssignmentID   string
	PeriodStart    time.Time
	PeriodEnd      time.Time
	Lines          []timecard.Line
	IdempotencyKey string
}

// CreateTimecard authorizes correction-class access (opening the record is
// the first correction-adjacent act) and creates a draft timecard.
func (s Service) CreateTimecard(ctx context.Context, p *trust.Principal, req CreateTimecardRequest) (timecard.Timecard, error) {
	if err := validPrincipal(p); err != nil {
		return timecard.Timecard{}, err
	}
	if s.Timecards == nil {
		return timecard.Timecard{}, ErrUnavailable
	}
	tenant := tenantOf(p)
	if err := s.authorize(ctx, p, tenant, req.WorkerRef, CapCorrectTimecard); err != nil {
		return timecard.Timecard{}, err
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" || strings.TrimSpace(req.TimecardID) == "" {
		return timecard.Timecard{}, ErrInvalidRequest
	}
	tc, err := timecard.NewTimecard(tenant, req.WorkerRef, req.AssignmentID, req.PeriodStart, req.PeriodEnd)
	if err != nil {
		return timecard.Timecard{}, err
	}
	if len(req.Lines) > 0 {
		tc, err = timecard.SetLines(tc, req.Lines, s.now(), p.Subject())
		if err != nil {
			return timecard.Timecard{}, err
		}
	}
	return s.Timecards.Create(ctx, tenant, tc, req.IdempotencyKey)
}

// ReviewTimecardRequest carries the pinned evidence FTIME-004 assembles a
// read model from. It never writes state.
type ReviewTimecardRequest struct {
	WorkerRef   string
	TimecardID  string
	Source      string
	Timezone    string
	Punches     []clock.TimeObservation
	Breaks      []attendance.Break
	Comparison  attendance.Result
	Corrections []clock.PunchCorrection
}

// ReviewTimecard authorizes read access and returns the FTIME-004 review
// view: paired intervals, breaks, schedule comparison, exceptions and
// correction history. Building the view never mutates the stored timecard.
func (s Service) ReviewTimecard(ctx context.Context, p *trust.Principal, req ReviewTimecardRequest) (timecard.ReviewView, error) {
	if err := validPrincipal(p); err != nil {
		return timecard.ReviewView{}, err
	}
	if err := s.authorize(ctx, p, tenantOf(p), req.WorkerRef, CapReviewTimecard); err != nil {
		return timecard.ReviewView{}, err
	}
	return timecard.BuildReview(req.WorkerRef, req.Source, req.Timezone, req.Punches, req.Breaks, req.Comparison, req.Corrections)
}

// mutateTimecard is the one place every revision-guarded, idempotent
// timecard command runs: authorize, load, check idempotency/revision inside
// one store transaction, apply mutate, persist.
func (s Service) mutateTimecard(ctx context.Context, p *trust.Principal, tenant, workerRef, timecardID, idempotencyKey string, cap Capability, cmd any,
	mutate func(timecard.Timecard) (timecard.Timecard, error)) (timecard.Timecard, error) {
	if s.Timecards == nil {
		return timecard.Timecard{}, ErrUnavailable
	}
	if strings.TrimSpace(timecardID) == "" || strings.TrimSpace(idempotencyKey) == "" {
		return timecard.Timecard{}, ErrInvalidRequest
	}
	if err := s.authorize(ctx, p, tenant, workerRef, cap); err != nil {
		return timecard.Timecard{}, err
	}
	current, err := s.Timecards.Get(ctx, tenant, timecardID)
	if err != nil {
		return timecard.Timecard{}, err
	}
	if current.Tenant != tenant || current.WorkerID != workerRef {
		return timecard.Timecard{}, ErrNotFound
	}
	d := digest(cmd)
	return s.Timecards.Execute(ctx, tenant, timecardID, p.Subject(), idempotencyKey, d, current.Revision, mutate)
}

// SubmitTimecardRequest moves a draft or reopened timecard into review
// (WF-CAP-007).
type SubmitTimecardRequest struct {
	WorkerRef        string
	TimecardID       string
	ExpectedRevision uint64
	IdempotencyKey   string
}

func (s Service) SubmitTimecard(ctx context.Context, p *trust.Principal, req SubmitTimecardRequest) (timecard.Timecard, error) {
	if err := validPrincipal(p); err != nil {
		return timecard.Timecard{}, err
	}
	now := s.now()
	return s.mutateTimecard(ctx, p, tenantOf(p), req.WorkerRef, req.TimecardID, req.IdempotencyKey, CapCorrectTimecard, req,
		func(tc timecard.Timecard) (timecard.Timecard, error) {
			if tc.Revision != req.ExpectedRevision {
				return timecard.Timecard{}, ErrRevisionConflict
			}
			return timecard.Submit(tc, now, p.Subject())
		})
}

// AttestTimecardRequest records one party's sign-off (WF-CAP-007). Party is
// WORKER or SUPERVISOR; the service does not infer it from the actor, since
// a supervisor and a worker may both submit their own attestation through
// the same actor identity in a small tenant.
type AttestTimecardRequest struct {
	WorkerRef        string
	TimecardID       string
	Party            timecard.Party
	ExpectedRevision uint64
	IdempotencyKey   string
}

func (s Service) AttestTimecard(ctx context.Context, p *trust.Principal, req AttestTimecardRequest) (timecard.Timecard, error) {
	if err := validPrincipal(p); err != nil {
		return timecard.Timecard{}, err
	}
	now := s.now()
	return s.mutateTimecard(ctx, p, tenantOf(p), req.WorkerRef, req.TimecardID, req.IdempotencyKey, CapCorrectTimecard, req,
		func(tc timecard.Timecard) (timecard.Timecard, error) {
			if tc.Revision != req.ExpectedRevision {
				return timecard.Timecard{}, ErrRevisionConflict
			}
			return timecard.Attest(tc, req.Party, p.Subject(), now)
		})
}

// CorrectTimecardRequest appends a reasoned correction as new evidence: the
// full replacement line set plus the source references that justify it.
// FTIME-004's REFACTOR is honored here: a correction never rewrites a prior
// line in place, it replaces the whole set and advances the revision, and
// SetLines' own history entry keeps a trace of the edit.
type CorrectTimecardRequest struct {
	WorkerRef        string
	TimecardID       string
	Lines            []timecard.Line
	ExpectedRevision uint64
	IdempotencyKey   string
}

func (s Service) CorrectTimecard(ctx context.Context, p *trust.Principal, req CorrectTimecardRequest) (timecard.Timecard, error) {
	if err := validPrincipal(p); err != nil {
		return timecard.Timecard{}, err
	}
	now := s.now()
	return s.mutateTimecard(ctx, p, tenantOf(p), req.WorkerRef, req.TimecardID, req.IdempotencyKey, CapCorrectTimecard, req,
		func(tc timecard.Timecard) (timecard.Timecard, error) {
			if tc.Revision != req.ExpectedRevision {
				return timecard.Timecard{}, ErrRevisionConflict
			}
			return timecard.SetLines(tc, req.Lines, now, p.Subject())
		})
}

// ApproveTimecardRequest pins the reviewed revision and applicable rules.
// OpenExceptions must be freshly computed by the caller (typically via
// ReviewTimecard + timecard.OpenExceptions against the current resolution
// state); a non-empty slice blocks approval exactly as FTIME-004's RED
// requires. The approving actor must be a supervisor currently in scope for
// WorkerRef and must not be the worker themselves (segregation of duties).
type ApproveTimecardRequest struct {
	WorkerRef        string
	TimecardID       string
	RulesRef         attendance.VersionedRef
	OpenExceptions   []attendance.Finding
	ExpectedRevision uint64
	IdempotencyKey   string
}

func (s Service) ApproveTimecard(ctx context.Context, p *trust.Principal, req ApproveTimecardRequest) (timecard.Timecard, error) {
	if err := validPrincipal(p); err != nil {
		return timecard.Timecard{}, err
	}
	tenant := tenantOf(p)
	if err := s.authorize(ctx, p, tenant, req.WorkerRef, CapApproveTimecard); err != nil {
		return timecard.Timecard{}, err
	}
	if err := s.requireInScope(ctx, tenant, p.Subject(), req.WorkerRef); err != nil {
		return timecard.Timecard{}, err
	}
	if len(req.OpenExceptions) > 0 {
		return timecard.Timecard{}, reject(ErrUnresolvedException, "exceptions", "UNRESOLVED", "approval blocked by an unresolved exception")
	}
	now := s.now()
	if s.Timecards == nil {
		return timecard.Timecard{}, ErrUnavailable
	}
	if strings.TrimSpace(req.TimecardID) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return timecard.Timecard{}, ErrInvalidRequest
	}
	current, err := s.Timecards.Get(ctx, tenant, req.TimecardID)
	if err != nil {
		return timecard.Timecard{}, err
	}
	if current.Tenant != tenant || current.WorkerID != req.WorkerRef {
		return timecard.Timecard{}, ErrNotFound
	}
	d := digest(req)
	return s.Timecards.Execute(ctx, tenant, req.TimecardID, p.Subject(), req.IdempotencyKey, d, current.Revision,
		func(tc timecard.Timecard) (timecard.Timecard, error) {
			if tc.Revision != req.ExpectedRevision {
				return timecard.Timecard{}, ErrRevisionConflict
			}
			return timecard.Approve(tc, p.Subject(), req.RulesRef, req.ExpectedRevision, req.OpenExceptions, now)
		})
}

// LockTimecardRequest finalizes an approved timecard.
type LockTimecardRequest struct {
	WorkerRef        string
	TimecardID       string
	ExpectedRevision uint64
	IdempotencyKey   string
}

func (s Service) LockTimecard(ctx context.Context, p *trust.Principal, req LockTimecardRequest) (timecard.Timecard, error) {
	if err := validPrincipal(p); err != nil {
		return timecard.Timecard{}, err
	}
	now := s.now()
	return s.mutateTimecard(ctx, p, tenantOf(p), req.WorkerRef, req.TimecardID, req.IdempotencyKey, CapApproveTimecard, req,
		func(tc timecard.Timecard) (timecard.Timecard, error) {
			if tc.Revision != req.ExpectedRevision {
				return timecard.Timecard{}, ErrRevisionConflict
			}
			return timecard.Lock(tc, now, p.Subject())
		})
}

// ReopenTimecardRequest returns an approved or locked timecard to REOPENED.
type ReopenTimecardRequest struct {
	WorkerRef        string
	TimecardID       string
	Reason           timecard.ReopenReason
	ExpectedRevision uint64
	IdempotencyKey   string
}

func (s Service) ReopenTimecard(ctx context.Context, p *trust.Principal, req ReopenTimecardRequest) (timecard.Timecard, error) {
	if err := validPrincipal(p); err != nil {
		return timecard.Timecard{}, err
	}
	now := s.now()
	return s.mutateTimecard(ctx, p, tenantOf(p), req.WorkerRef, req.TimecardID, req.IdempotencyKey, CapApproveTimecard, req,
		func(tc timecard.Timecard) (timecard.Timecard, error) {
			if tc.Revision != req.ExpectedRevision {
				return timecard.Timecard{}, ErrRevisionConflict
			}
			return timecard.Reopen(tc, req.Reason, p.Subject(), now)
		})
}
