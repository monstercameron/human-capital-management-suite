package app

// WF-RUN-034: the served proposal's target position.
//
// A promotion commits into a target position (the commit model requires one
// and an occupancy row for it). A proposal that names no position is given
// one here, deterministically: the first OPEN catalog position for the target
// job in the worker's own organization unit with capacity left, disclosed as
// the same picker-issued revision reference PROMOUX-004 checks, so the
// Position domain still re-derives existence, compatibility and vacancy at
// preflight. In-place reclassification is out of scope.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/committedfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// committedPay reads the base pay the aggregates record for a created worker
// now. A worker with no committed compensation reports false, and the
// recorded journey_worker baseline stands.
func (e *journeyEngine) committedPay(ctx context.Context, principal *trust.Principal, workerID string) (committedfacts.Placement, bool, error) {
	if e.db == nil || e.svc == nil || e.svc.tenantUUID == nil {
		return committedfacts.Placement{}, false, nil
	}
	reader := committedfacts.Reader{DB: e.db, TenantUUID: e.svc.tenantUUID}
	placement, found, err := reader.PlacementAt(ctx, principal.Tenant(), workerID, e.now().UTC())
	if err != nil {
		return committedfacts.Placement{}, false, fmt.Errorf("app: journey: read the committed pay: %w", err)
	}
	if !found || strings.TrimSpace(placement.BasePay) == "" || strings.TrimSpace(placement.Currency) == "" {
		return committedfacts.Placement{}, false, nil
	}
	return placement, true, nil
}

// selectTargetPosition returns the revision reference of the OPEN position a
// created worker's promotion targets. It returns "" when this cell has no
// execution database or the tenant's catalog records no position for the
// target at all (the proposal then stays position-less and cannot commit), and
// a typed input refusal when every recorded position is already full.
func (e *journeyEngine) selectTargetPosition(ctx context.Context, principal *trust.Principal, orgUnit, jobCode, grade string, effective values.LocalDate) (string, error) {
	if e.db == nil || e.svc == nil || e.svc.tenantUUID == nil || strings.TrimSpace(orgUnit) == "" {
		return "", nil
	}
	tenantID := e.svc.tenantUUID(principal.Tenant())
	businessAt := time.Date(int(effective.Year()), effective.Month(), int(effective.Day()), 0, 0, 0, 0, time.UTC)
	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return "", err
	}
	vacancy, selectErr := demoworkforce.SelectVacancy(ctx, tx, tenantID, orgUnit, jobCode, grade, businessAt)
	_ = tx.Rollback(ctx)
	switch {
	case errors.Is(selectErr, demoworkforce.ErrNoCatalogVacancy):
		return "", nil
	case errors.Is(selectErr, demoworkforce.ErrNoVacancy):
		return "", journeyInputError("target_job_code", "no open position for this job has capacity in the worker's organization unit")
	case selectErr != nil:
		return "", fmt.Errorf("app: journey: select the target position: %w", selectErr)
	}
	ref := values.EntityRef{Tenant: principal.Tenant(), Kind: position.KindPosition, Id: vacancy.PositionID.String()}
	known, err := values.NewKnownAt(values.NewInstant(e.now().UTC()))
	if err != nil {
		return "", fmt.Errorf("app: journey: target position known-at: %w", err)
	}
	reader := positionfacts.Reader{DB: e.db, TenantUUID: e.svc.tenantUUID}
	revision, exists, err := reader.PositionRevisionAt(ctx, position.PositionQuery{
		Tenant: principal.Tenant(), Position: ref, AsOf: position.AsOf{EffectiveOn: effective, KnownAt: known},
	})
	if err != nil {
		return "", fmt.Errorf("app: journey: read the target position: %w", err)
	}
	if !exists {
		return "", fmt.Errorf("%w: the selected target position does not resolve", workspace.ErrJourneyUnavailable)
	}
	encoded, err := position.EncodeRevisionRef(ref, revision.Revision)
	if err != nil {
		return "", fmt.Errorf("app: journey: encode the target position: %w", err)
	}
	return encoded.String(), nil
}
