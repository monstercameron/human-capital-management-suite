package journey

import (
	"context"
	"errors"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/workerids"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// workerIDStore authorizes worker identifier administration from the
// principal's server-side role set: a revoked administrator whose durable
// assignment no longer holds comp_admin is refused even while the credential
// still signs it.
func (s *server) workerIDStore(ctx context.Context, principal *trust.Principal, requestID string) (workerids.Store, error) {
	if !s.hasEffectiveRole(ctx, principal, "comp_admin") {
		return nil, envelope.New(envelope.CodePermissionDenied, "journey.worker_ids.role_required", "worker identifier policy requires the compensation administrator role").WithCorrelation(requestID).WithEvidence(evidence(principal))
	}
	if s.deps.WorkerIDs == nil {
		return nil, envelope.New(envelope.CodeUnavailable, "journey.worker_ids.store_unconfigured", "worker identifier policy is not configured").WithCorrelation(requestID).WithEvidence(evidence(principal))
	}
	return s.deps.WorkerIDs, nil
}

func workerIDError(err error, principal *trust.Principal, requestID, operation string) error {
	code, reason := envelope.CodeUnspecified, "store_failed"
	switch {
	case errors.Is(err, workerids.ErrVersionConflict):
		code, reason = envelope.CodeAborted, "version_conflict"
	case errors.Is(err, workerids.ErrInvalid):
		code, reason = envelope.CodeInvalidArgument, "invalid"
	case errors.Is(err, workerids.ErrExhausted):
		code, reason = envelope.CodeFailedPrecondition, "exhausted"
	case errors.Is(err, workerids.ErrUnavailable):
		code, reason = envelope.CodeUnavailable, "store_unavailable"
	}
	return envelope.New(code, "journey.worker_ids."+operation+"."+reason, "the worker identifier policy operation could not be completed").WithCorrelation(requestID).WithEvidence(evidence(principal))
}

func (s *server) GetWorkerIDPolicy(ctx context.Context, _ *journeyv1.GetWorkerIDPolicyRequest) (*journeyv1.GetWorkerIDPolicyResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requireServedCall(ctx, principal, inv, "GetWorkerIDPolicy"); err != nil {
		return nil, err
	}
	store, dependencyErr := s.workerIDStore(ctx, principal, inv.RequestID())
	if dependencyErr != nil {
		return nil, dependencyErr
	}
	p, storeErr := store.Load(ctx, principal.Tenant(), principal.OrganizationScopeID())
	if storeErr != nil {
		return nil, workerIDError(storeErr, principal, inv.RequestID(), "load")
	}
	return &journeyv1.GetWorkerIDPolicyResponse{Policy: toWorkerIDPolicy(p), Previews: workerIDPreviews(p, s.now())}, nil
}

func (s *server) SaveWorkerIDPolicy(ctx context.Context, req *journeyv1.SaveWorkerIDPolicyRequest) (*journeyv1.SaveWorkerIDPolicyResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requirePageAction(ctx, principal, inv, "worker-ids", roleaccess.ActionUpdate); err != nil {
		return nil, err
	}
	store, dependencyErr := s.workerIDStore(ctx, principal, inv.RequestID())
	if dependencyErr != nil {
		return nil, dependencyErr
	}
	p, storeErr := store.Save(ctx, principal.Tenant(), principal.OrganizationScopeID(), principal.Subject(), fromWorkerIDPolicy(req.GetPolicy()))
	if storeErr != nil {
		return nil, workerIDError(storeErr, principal, inv.RequestID(), "save")
	}
	return &journeyv1.SaveWorkerIDPolicyResponse{Policy: toWorkerIDPolicy(p), Previews: workerIDPreviews(p, s.now())}, nil
}

func (s *server) now() time.Time {
	if s.deps.Now != nil {
		return s.deps.Now().UTC()
	}
	return time.Now().UTC()
}

func toWorkerIDPolicy(p workerids.Policy) *journeyv1.WorkerIDPolicy {
	return &journeyv1.WorkerIDPolicy{Version: p.Version, Prefix: p.Prefix, Suffix: p.Suffix, Separator: p.Separator, SequenceDigits: int32(p.SequenceDigits), StartAt: p.StartAt, NextSequence: p.NextSequence, IncrementBy: p.IncrementBy, ZeroPad: p.ZeroPad, YearFormat: p.YearFormat, IncludeUnitCode: p.IncludeUnitCode, CheckDigit: p.CheckDigit, ExcludedRanges: p.ExcludedRanges, IssuedCount: p.IssuedCount}
}

func fromWorkerIDPolicy(p *journeyv1.WorkerIDPolicy) workerids.Policy {
	if p == nil {
		return workerids.DefaultPolicy()
	}
	return workerids.Normalize(workerids.Policy{Version: p.GetVersion(), Prefix: p.GetPrefix(), Suffix: p.GetSuffix(), Separator: p.GetSeparator(), SequenceDigits: int(p.GetSequenceDigits()), StartAt: p.GetStartAt(), NextSequence: p.GetNextSequence(), IncrementBy: p.GetIncrementBy(), ZeroPad: p.GetZeroPad(), YearFormat: p.GetYearFormat(), IncludeUnitCode: p.GetIncludeUnitCode(), CheckDigit: p.GetCheckDigit(), ExcludedRanges: p.GetExcludedRanges(), IssuedCount: p.GetIssuedCount()})
}

func workerIDPreviews(p workerids.Policy, at time.Time) []string {
	result, _ := workerids.Preview(p, workerids.FormatContext{At: at, UnitCode: "CARE"})
	return result
}
