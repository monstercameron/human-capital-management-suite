// Package paymethodchange coordinates ACH policy evaluation, authorization,
// and persistence for a direct-deposit destination change.
package paymethodchange

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/paymethodstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod/achrisk"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod/achriskadapter"
)

// Service composes the domain policy with tenant-scoped destination storage.
// The caller supplies its open transaction so destination activation is part
// of the caller's transaction boundary.
type Service struct {
	store paymethodstore.Store
}

func New(store paymethodstore.Store) Service { return Service{store: store} }

// Result records the policy evidence and authorization result for one
// attempted change. Destination is populated only after authorization allows
// activation and the new revision is persisted successfully.
type Result struct {
	Authorization paymethod.AuthorizationDecision
	Assessment    achrisk.RiskAssessment
	Destination   paymethod.Destination
}

// AuthorizeAndActivate evaluates real ACH signals before writing the proposed
// destination. A rejected or incomplete authorization returns its decision
// with no destination write.
func (s Service) AuthorizeAndActivate(
	ctx context.Context,
	ex paymethodstore.Executor,
	tenantID uuid.UUID,
	policy achrisk.Policy,
	input achrisk.EvaluationInput,
	request paymethod.ChangeAuthorizationRequest,
	validUntil time.Time,
) (Result, error) {
	if tenantID == uuid.Nil || request.BankDetailChange.TenantID != tenantID.String() {
		return Result{}, fmt.Errorf("paymethod change: tenant does not match transaction")
	}
	decision, assessment, err := achriskadapter.AuthorizeDirectDepositChange(policy, input, request, validUntil)
	result := Result{Authorization: decision, Assessment: assessment}
	if err != nil || !decision.Activated {
		return result, err
	}
	destination, err := s.store.PutDestination(ctx, ex, tenantID, request.ProposedDestination)
	if err != nil {
		return result, err
	}
	result.Destination = destination
	return result, nil
}
