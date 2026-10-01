package application

import (
	"context"
	"time"

	"github.com/google/uuid"
	paymethodchange "github.com/monstercameron/human-capital-management-suite/internal/application/paymethodchange"
	"github.com/monstercameron/human-capital-management-suite/internal/data/paymethodstore"
	paymethoddomain "github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod/achrisk"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ServedPayMethodSurface exposes the payment-destination lifecycle through
// the application boundary used by hcmnext serve. The paymethod domain and
// paymethodchange service remain the semantic and transactional owners; this
// surface only makes their existing operations reachable from the composed
// process.
type ServedPayMethodSurface struct {
	Version                    func() int
	NewDestination             func(paymethoddomain.Destination) (paymethoddomain.Destination, error)
	NewChallenge               func(paymethoddomain.Destination, paymethoddomain.ChallengeSpec) (paymethoddomain.VerificationChallenge, error)
	ApplyVerification          func(paymethoddomain.Destination, paymethoddomain.VerificationEvent) (paymethoddomain.Destination, error)
	NewSplitPlan               func(paymethoddomain.SplitPlan) (paymethoddomain.SplitPlan, error)
	AuthorizeChange            func(paymethoddomain.ChangeAuthorizationRequest) (paymethoddomain.AuthorizationDecision, error)
	NewChangeService           func(paymethodstore.Store) paymethodchange.Service
	AuthorizeAndActivate       func(paymethodchange.Service, context.Context, paymethodstore.Executor, uuid.UUID, achrisk.Policy, achrisk.EvaluationInput, paymethoddomain.ChangeAuthorizationRequest, time.Time) (paymethodchange.Result, error)
	RecordPrenoteObservation   func(paymethoddomain.Destination, paymethoddomain.PrenoteObservation) (paymethoddomain.PrenoteRecord, error)
	RecordSettlementSubmission func(paymethoddomain.SettlementSubmission, []paymethoddomain.RecordedSubmission) (paymethoddomain.RecordedSubmission, error)
	ReconcileSettlement        func(paymethoddomain.Destination, paymethoddomain.RecordedSubmission, paymethoddomain.SettlementObservation, values.Instant, int64) (paymethoddomain.ReconciliationReport, error)
}

// NewServedPayMethodSurface returns the payment-method capabilities reachable
// from a composed serving application. It creates no shared state and does
// not bypass the tenant-scoped transaction supplied to the change service.
func NewServedPayMethodSurface() ServedPayMethodSurface {
	return ServedPayMethodSurface{
		Version:           paymethoddomain.Version,
		NewDestination:    paymethoddomain.NewDestination,
		NewChallenge:      paymethoddomain.NewChallenge,
		ApplyVerification: paymethoddomain.ApplyVerification,
		NewSplitPlan:      paymethoddomain.NewSplitPlan,
		AuthorizeChange:   paymethoddomain.AuthorizeChange,
		NewChangeService:  paymethodchange.New,
		AuthorizeAndActivate: func(service paymethodchange.Service, ctx context.Context, ex paymethodstore.Executor, tenantID uuid.UUID, policy achrisk.Policy, input achrisk.EvaluationInput, request paymethoddomain.ChangeAuthorizationRequest, validUntil time.Time) (paymethodchange.Result, error) {
			return service.AuthorizeAndActivate(ctx, ex, tenantID, policy, input, request, validUntil)
		},
		RecordPrenoteObservation:   paymethoddomain.RecordPrenoteObservation,
		RecordSettlementSubmission: paymethoddomain.RecordSettlementSubmission,
		ReconcileSettlement:        paymethoddomain.ReconcileSettlement,
	}
}

// PayMethod returns the payment-method capabilities exposed by a composed
// application. A nil application exposes no served capabilities.
func (a *App) PayMethod() ServedPayMethodSurface {
	if a == nil {
		return ServedPayMethodSurface{}
	}
	return NewServedPayMethodSurface()
}
