package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioninvalidation"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/journeyinvalidation"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// REV-091-03: every committed promotion transition -- propose, edit,
// execute, approve or reject, acknowledge, withdraw or cancel, and the
// timer- or signal-driven completion -- allocates its durable position with
// promotioninvalidation.NextSequence and hands it to the cell's invalidation
// hub, which filters it per viewer through promotion.SubscriberSequencer.
//
// The call sites are the write paths' own deferred epilogues, and each one
// publishes only when its operation returned without error, i.e. after every
// transaction it opened has committed. A refused or rolled-back operation
// publishes nothing. The allocation runs in its own short tenant-scoped
// transaction for the same reason: it must never be able to roll back, delay
// or fail the business write it describes, and a hint for a write that did
// not commit must be impossible, not merely unlikely.

// transitionCounterProjection is the one tenant-wide counter every committed
// promotion transition draws its position from. It is the Journeys region's
// wire projection name, as promotioninvalidation requires; the position is
// an item revision shared by every region's hint for that transition.
var transitionCounterProjection = func() string {
	name, _ := promotion.RegionJourneys.Projection()
	return name
}()

// invalidationPublishTimeout bounds the post-commit allocation. The
// operation it follows has already succeeded; a slow counter must not hold
// the caller's response.
const invalidationPublishTimeout = 5 * time.Second

// InvalidationPublisher receives committed transitions. The cell's
// *journeyinvalidation.Hub implements it.
type InvalidationPublisher interface {
	Publish(journeyinvalidation.Committed) bool
}

// publishCommitted records that intentIDs changed durably. err is the
// operation's own result; anything but nil publishes nothing.
func (e *journeyEngine) publishCommitted(ctx context.Context, err error, intentIDs ...string) {
	if err != nil || e == nil {
		return
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return
	}
	e.publishTenantCommitted(ctx, principal.Tenant(), intentIDs...)
}

// publishTenantCommitted is publishCommitted for a caller with no principal
// (the scheduler's timer and signal resumes), which names its tenant.
func (e *journeyEngine) publishTenantCommitted(ctx context.Context, tenant values.TenantId, intentIDs ...string) {
	if e == nil || e.invalidations == nil || e.db == nil || e.svc == nil || e.svc.tenantUUID == nil {
		return
	}
	tenantID := e.svc.tenantUUID(tenant)
	if tenantID == uuid.Nil {
		return
	}
	// The caller's request may be cancelled the moment its response is
	// written; the transition it made is durable regardless.
	allocCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), invalidationPublishTimeout)
	defer cancel()
	seen := make(map[string]bool, len(intentIDs))
	for _, intentID := range intentIDs {
		if intentID == "" || seen[intentID] {
			continue
		}
		seen[intentID] = true
		revision, allocErr := e.allocateTransition(allocCtx, tenantID)
		if allocErr != nil {
			// Losing a hint is recoverable: every client re-reads
			// authoritatively when it reconnects or navigates. Failing the
			// already-committed business operation is not.
			continue
		}
		e.invalidations.Publish(journeyinvalidation.Committed{Tenant: tenant, IntentID: intentID, Revision: revision})
	}
}

func (e *journeyEngine) allocateTransition(ctx context.Context, tenantID uuid.UUID) (uint64, error) {
	tx, err := e.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return 0, err
	}
	revision, err := promotioninvalidation.NextSequence(ctx, tx, tenantID, transitionCounterProjection)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return revision, nil
}

// interventionChanged reports whether an intervention outcome recorded a
// durable change. A denial or a too-late refusal wrote nothing.
func interventionChanged(outcome workspace.JourneyInterventionOutcome) bool {
	return outcome != workspace.InterventionDenied && outcome != workspace.InterventionTooLate
}
