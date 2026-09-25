package projectservice

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// WorkforceInviteeEligibility resolves invitations against the current,
// tenant-scoped worker directory. journey_worker has no individual project
// classification, so qualifying employees receive only baseline class 0;
// StoreAdapter then restricts these invitations to baseline-class projects.
type WorkforceInviteeEligibility struct {
	DB       dbport.Beginner
	TenantID func(kernelvalues.TenantId) uuid.UUID
}

const workforceInviteeBaselineClass uint8 = 0

func (a WorkforceInviteeEligibility) CheckInvitee(ctx context.Context, principal *trust.Principal, userID string) (uint8, error) {
	if a.DB == nil || a.TenantID == nil {
		return 0, ErrUnavailable
	}
	if err := validPrincipal(principal); err != nil {
		return 0, err
	}
	if strings.TrimSpace(userID) == "" {
		return 0, ErrInvalidRequest
	}
	tenantID := a.TenantID(principal.Tenant())
	if tenantID == uuid.Nil {
		return 0, projectaccess.ErrUnauthorized
	}
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return 0, err
	}
	worker, found, err := (workforce.Store{}).Get(ctx, tx, tenantID, userID)
	if err != nil {
		return 0, err
	}
	if !found || worker.WorkerKey != userID || !strings.EqualFold(worker.WorkerType, "EMPLOYEE") || !strings.EqualFold(worker.LifecycleStatus, "ACTIVE") {
		return 0, projectaccess.ErrMemberNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return workforceInviteeBaselineClass, nil
}

var _ InviteeEligibility = WorkforceInviteeEligibility{}
