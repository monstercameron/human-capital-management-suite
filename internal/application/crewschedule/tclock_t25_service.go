package crewschedule

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/crewshift"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Service coordinates crewshift commands. It owns no mutable schedule state;
// revision and idempotency state belong to ShiftStore.
type Service struct {
	Shifts   ShiftStore
	Schedule PublishedScheduleReader
	Auth     Authorizer
	Notify   Notifier
	Clock    func() time.Time
}

func (s Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func validPrincipal(p *trust.Principal) error {
	if p == nil || p.Subject() == "" || p.Tenant().String() == "" {
		return ErrInvalidPrincipal
	}
	return nil
}

func tenantOf(p *trust.Principal) string { return p.Tenant().String() }

func (s Service) authorize(ctx context.Context, p *trust.Principal, worker string, cap Capability) error {
	if s.Auth == nil {
		return ErrUnavailable
	}
	if strings.TrimSpace(worker) == "" {
		return ErrInvalidRequest
	}
	return s.Auth.Authorize(ctx, p, tenantOf(p), worker, cap)
}

func commandDigest(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// The request still needs a stable receipt binding when an opaque
		// domain value declines JSON marshaling. fmt's value form is only the
		// fallback; domain validation remains responsible for rejecting bad
		// references before a mutation can commit.
		b = []byte(fmt.Sprintf("%#v", v))
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// publishDigestInput excludes PublishCheck function values, which cannot be
// JSON encoded. Check names remain part of the receipt binding so changing a
// plugged-in rule set cannot replay an earlier command under the same key.
type publishDigestInput struct {
	WorkerRef        string
	ShiftID          string
	ExpectedRevision int64
	Input            crewshift.PublishInput
	Checks           []string
	IdempotencyKey   string
}

func digestPublishRequest(req PublishShiftRequest) string {
	names := make([]string, 0, len(req.Checks))
	for _, check := range req.Checks {
		if check == nil {
			names = append(names, "<nil>")
			continue
		}
		names = append(names, check.Name())
	}
	return commandDigest(publishDigestInput{
		WorkerRef: req.WorkerRef, ShiftID: req.ShiftID, ExpectedRevision: req.ExpectedRevision,
		Input: req.Input, Checks: names, IdempotencyKey: req.IdempotencyKey,
	})
}

func (s Service) ready(p *trust.Principal) error {
	if err := validPrincipal(p); err != nil {
		return err
	}
	if s.Shifts == nil {
		return ErrUnavailable
	}
	return nil
}

func sameTenant(tenant string, shift crewshift.Shift) error {
	if shift.Tenant.String() != tenant {
		return ErrNotFound
	}
	return nil
}

// CreateShiftRequest opens a draft. Drafting still requires publish-class
// authorization because it creates the record that can later be published.
type CreateShiftRequest struct {
	Shift          crewshift.Shift
	IdempotencyKey string
}

func (s Service) CreateShift(ctx context.Context, p *trust.Principal, req CreateShiftRequest) (crewshift.Shift, error) {
	if err := s.ready(p); err != nil {
		return crewshift.Shift{}, err
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return crewshift.Shift{}, ErrInvalidRequest
	}
	if err := sameTenant(tenantOf(p), req.Shift); err != nil {
		return crewshift.Shift{}, err
	}
	if err := req.Shift.Validate(); err != nil {
		return crewshift.Shift{}, err
	}
	if req.Shift.Status != crewshift.StatusDraft {
		return crewshift.Shift{}, ErrInvalidRequest
	}
	if err := s.authorize(ctx, p, req.Shift.WorkerRef.String(), CapabilityPublish); err != nil {
		return crewshift.Shift{}, err
	}
	return s.Shifts.Create(ctx, tenantOf(p), req.Shift, req.IdempotencyKey)
}

// PublishShiftRequest supplies the current standing evidence needed by the
// pure publication checks. Existing is replaced by Schedule when that port
// is wired, preventing a stale caller snapshot from bypassing overlap checks.
type PublishShiftRequest struct {
	WorkerRef        string
	ShiftID          string
	ExpectedRevision int64
	Input            crewshift.PublishInput
	Checks           []crewshift.PublishCheck
	IdempotencyKey   string
}

func (s Service) PublishShift(ctx context.Context, p *trust.Principal, req PublishShiftRequest) (crewshift.Shift, error) {
	if err := s.ready(p); err != nil {
		return crewshift.Shift{}, err
	}
	if req.ExpectedRevision <= 0 || strings.TrimSpace(req.ShiftID) == "" || strings.TrimSpace(req.IdempotencyKey) == "" {
		return crewshift.Shift{}, ErrInvalidRequest
	}
	if err := s.authorize(ctx, p, req.WorkerRef, CapabilityPublish); err != nil {
		return crewshift.Shift{}, err
	}
	current, err := s.Shifts.Get(ctx, tenantOf(p), req.ShiftID)
	if err != nil {
		return crewshift.Shift{}, err
	}
	if err := sameTenant(tenantOf(p), current); err != nil {
		return crewshift.Shift{}, err
	}
	if current.WorkerRef.String() != req.WorkerRef {
		return crewshift.Shift{}, ErrNotFound
	}
	digest := digestPublishRequest(req)
	if digest == "" {
		return crewshift.Shift{}, ErrInvalidRequest
	}
	result, err := s.Shifts.Execute(ctx, tenantOf(p), req.ShiftID, p.Subject(), req.IdempotencyKey, digest, req.ExpectedRevision,
		func(cur crewshift.Shift, history crewshift.History) (crewshift.LifecycleResult, error) {
			input := req.Input
			input.Shift = cur
			input.Now = s.now()
			if s.Schedule != nil {
				input.Existing, err = s.Schedule.PublishedForWorker(ctx, tenantOf(p), cur.WorkerRef.String())
				if err != nil {
					return crewshift.LifecycleResult{}, err
				}
			}
			checks := append(crewshift.DefaultPublishChecks(), req.Checks...)
			out, err := crewshift.Publish(input, checks)
			if err != nil {
				return crewshift.LifecycleResult{}, err
			}
			return crewshift.LifecycleResult{
				Updated: out.Shift,
				History: history.Append(cur),
				Notifications: []crewshift.Notification{{
					WorkerRef: cur.WorkerRef, ShiftID: cur.ID, Revision: out.Shift.Revision,
					Action: crewshift.ActionPublish, Reason: "shift published",
				}},
			}, nil
		})
	if err != nil {
		return crewshift.Shift{}, err
	}
	s.emit(ctx, tenantOf(p), result.Notifications)
	return result.Updated, nil
}

type CancelShiftRequest struct {
	WorkerRef        string
	ShiftID          string
	ExpectedRevision int64
	Grant            crewshift.Grant
	Reason           string
	IdempotencyKey   string
}

func (s Service) CancelShift(ctx context.Context, p *trust.Principal, req CancelShiftRequest) (crewshift.Shift, error) {
	return s.lifecycle(ctx, p, req.WorkerRef, req.ShiftID, req.ExpectedRevision, req.IdempotencyKey, CapabilityCancel, crewshift.ActionCancel, req.Grant, req.Reason, "", crewshift.EligibilityFacts{}, req)
}

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
	return s.lifecycle(ctx, p, req.WorkerRef, req.ShiftID, req.ExpectedRevision, req.IdempotencyKey, CapabilityReassign, crewshift.ActionReassign, req.Grant, req.Reason, req.NewWorkerRef, req.NewWorkerEligibility, req)
}

func (s Service) lifecycle(ctx context.Context, p *trust.Principal, worker, shiftID string, expected int64, key string, cap Capability, action crewshift.LifecycleAction, grant crewshift.Grant, reason, newWorker string, eligibility crewshift.EligibilityFacts, cmd any) (crewshift.Shift, error) {
	if err := s.ready(p); err != nil {
		return crewshift.Shift{}, err
	}
	if expected <= 0 || strings.TrimSpace(shiftID) == "" || strings.TrimSpace(key) == "" || strings.TrimSpace(reason) == "" {
		return crewshift.Shift{}, ErrInvalidRequest
	}
	if err := s.authorize(ctx, p, worker, cap); err != nil {
		return crewshift.Shift{}, err
	}
	current, err := s.Shifts.Get(ctx, tenantOf(p), shiftID)
	if err != nil {
		return crewshift.Shift{}, err
	}
	if err := sameTenant(tenantOf(p), current); err != nil {
		return crewshift.Shift{}, err
	}
	if current.WorkerRef.String() != worker {
		return crewshift.Shift{}, ErrNotFound
	}
	if grant.ActorRef.Validate() != nil || grant.ActorRef.Tenant != current.Tenant {
		return crewshift.Shift{}, ErrForbidden
	}
	digest := commandDigest(cmd)
	if digest == "" {
		return crewshift.Shift{}, ErrInvalidRequest
	}
	result, err := s.Shifts.Execute(ctx, tenantOf(p), shiftID, p.Subject(), key, digest, expected,
		func(cur crewshift.Shift, history crewshift.History) (crewshift.LifecycleResult, error) {
			request := crewshift.LifecycleRequest{Action: action, Current: cur, ExpectedRevision: expected, Grant: grant, Now: s.now(), Reason: reason}
			if newWorker != "" {
				request.NewWorkerRef = cur.WorkerRef
				if strings.HasPrefix(newWorker, "eref:v1:") {
					if err := request.NewWorkerRef.UnmarshalText([]byte(newWorker)); err != nil {
						return crewshift.LifecycleResult{}, ErrInvalidRequest
					}
				} else {
					request.NewWorkerRef.Id = newWorker
				}
				request.NewWorkerEligibility = eligibility
			}
			return crewshift.ApplyLifecycle(request, history)
		})
	if err != nil {
		return crewshift.Shift{}, err
	}
	s.emit(ctx, tenantOf(p), result.Notifications)
	return result.Updated, nil
}

func (s Service) emit(ctx context.Context, tenant string, notifications []crewshift.Notification) {
	if s.Notify == nil {
		return
	}
	for _, n := range notifications {
		_ = s.Notify.NotifyShift(ctx, tenant, n)
	}
}

type ReconcileShiftRequest struct {
	WorkerRef string
	ShiftID   string
	Tolerance crewshift.Tolerance
	Punches   []crewshift.PunchInterval
}

// ReconcileShift is deliberately read-only: it passes immutable shift and
// punch evidence to the domain and returns findings pinned to the shift
// revision. It never edits a punch or a schedule revision.
func (s Service) ReconcileShift(ctx context.Context, p *trust.Principal, req ReconcileShiftRequest) (crewshift.ReconcileResult, error) {
	if err := s.ready(p); err != nil {
		return crewshift.ReconcileResult{}, err
	}
	if strings.TrimSpace(req.ShiftID) == "" {
		return crewshift.ReconcileResult{}, ErrInvalidRequest
	}
	if err := s.authorize(ctx, p, req.WorkerRef, CapabilityReconcile); err != nil {
		return crewshift.ReconcileResult{}, err
	}
	shift, err := s.Shifts.Get(ctx, tenantOf(p), req.ShiftID)
	if err != nil {
		return crewshift.ReconcileResult{}, err
	}
	if err := sameTenant(tenantOf(p), shift); err != nil {
		return crewshift.ReconcileResult{}, err
	}
	if shift.WorkerRef.String() != req.WorkerRef {
		return crewshift.ReconcileResult{}, ErrNotFound
	}
	return crewshift.Reconcile(shift, req.Tolerance, req.Punches)
}
