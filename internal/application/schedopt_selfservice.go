package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/schedopt"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrShiftSelfServiceIdentity = errors.New("application: authenticated worker identity unavailable")
var ErrShiftSelfServiceFacts = errors.New("application: authoritative scheduling facts unavailable")

// ShiftSelfServiceWorkerResolver binds an admitted human principal to the
// tenant's authoritative worker record. It must not read an EntityRef from
// request data or infer a worker from a display name.
type ShiftSelfServiceWorkerResolver func(context.Context, *trust.Principal) (values.EntityRef, error)
type ShiftSelfServiceStandingResolver func(context.Context, values.TenantId) ([]schedopt.WorkerStanding, error)

type ShiftSelfServicePort interface {
	OfferOpenShift(string, string, values.EntityRef, uint64, string) (schedopt.ShiftOffer, error)
	OfferTrade(string, string, string, values.EntityRef, uint64, string) (schedopt.ShiftOffer, error)
	ClaimOpenShift(string, values.EntityRef, uint64, []schedopt.WorkerStanding, time.Time) (schedopt.ShiftSelfServiceResult, error)
	AcceptTrade(string, values.EntityRef, uint64, []schedopt.WorkerStanding, time.Time) (schedopt.ShiftSelfServiceResult, error)
}

// ShiftSelfServiceActor is the authenticated application boundary for worker
// offers, claims and trades. The worker reference always comes from resolver
// output bound to the verifier-created principal.
type ShiftSelfServiceActor struct {
	Service          ShiftSelfServicePort
	ResolveWorker    ShiftSelfServiceWorkerResolver
	ResolveStandings ShiftSelfServiceStandingResolver
	Now              func() time.Time
}

func (s ShiftSelfServiceActor) OfferOpenShift(ctx context.Context, principal *trust.Principal, offerID, assignmentID string, fence uint64, reason string) (schedopt.ShiftOffer, error) {
	worker, err := s.worker(ctx, principal)
	if err != nil {
		return schedopt.ShiftOffer{}, err
	}
	return s.Service.OfferOpenShift(offerID, assignmentID, worker, fence, reason)
}

func (s ShiftSelfServiceActor) OfferTrade(ctx context.Context, principal *trust.Principal, offerID, assignmentID, requestedAssignmentID string, fence uint64, reason string) (schedopt.ShiftOffer, error) {
	worker, err := s.worker(ctx, principal)
	if err != nil {
		return schedopt.ShiftOffer{}, err
	}
	return s.Service.OfferTrade(offerID, assignmentID, requestedAssignmentID, worker, fence, reason)
}

func (s ShiftSelfServiceActor) ClaimOpenShift(ctx context.Context, principal *trust.Principal, offerID string, fence uint64) (schedopt.ShiftSelfServiceResult, error) {
	worker, err := s.worker(ctx, principal)
	if err != nil {
		return schedopt.ShiftSelfServiceResult{}, err
	}
	standings, err := s.standings(ctx, principal)
	if err != nil {
		return schedopt.ShiftSelfServiceResult{}, err
	}
	return s.Service.ClaimOpenShift(offerID, worker, fence, standings, s.now())
}

func (s ShiftSelfServiceActor) AcceptTrade(ctx context.Context, principal *trust.Principal, offerID string, fence uint64) (schedopt.ShiftSelfServiceResult, error) {
	worker, err := s.worker(ctx, principal)
	if err != nil {
		return schedopt.ShiftSelfServiceResult{}, err
	}
	standings, err := s.standings(ctx, principal)
	if err != nil {
		return schedopt.ShiftSelfServiceResult{}, err
	}
	return s.Service.AcceptTrade(offerID, worker, fence, standings, s.now())
}

func (s ShiftSelfServiceActor) standings(ctx context.Context, principal *trust.Principal) ([]schedopt.WorkerStanding, error) {
	if s.ResolveStandings == nil {
		return nil, ErrShiftSelfServiceFacts
	}
	standings, err := s.ResolveStandings(ctx, principal.Tenant())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrShiftSelfServiceFacts, err)
	}
	if len(standings) == 0 {
		return nil, ErrShiftSelfServiceFacts
	}
	return standings, nil
}

func (s ShiftSelfServiceActor) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s ShiftSelfServiceActor) worker(ctx context.Context, principal *trust.Principal) (values.EntityRef, error) {
	if ctx == nil || principal == nil || principal.Subject() == "" || principal.SubjectKind() != trust.SubjectKindHuman || s.Service == nil || s.ResolveWorker == nil {
		return values.EntityRef{}, ErrShiftSelfServiceIdentity
	}
	worker, err := s.ResolveWorker(ctx, principal)
	if err != nil {
		return values.EntityRef{}, fmt.Errorf("%w: %v", ErrShiftSelfServiceIdentity, err)
	}
	if worker.Validate() != nil || string(worker.Tenant) != principal.Tenant().String() || worker.Kind != "candidate" {
		return values.EntityRef{}, ErrShiftSelfServiceIdentity
	}
	return worker, nil
}
