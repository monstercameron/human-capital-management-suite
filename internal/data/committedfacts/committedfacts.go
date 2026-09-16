// Package committedfacts reads the placement and pay a worker actually holds
// after a promotion has committed (owner: data plane; phase: P1B).
//
// The Journey surface records employees in journey_worker, which is
// append-only and never revised; a committed promotion writes its successor
// facts to the DB-008/010 aggregates instead (internal/data/promotioncommit).
// Without this package every surface kept showing the pre-promotion job,
// grade and pay for a worker whose promotion had already committed.
//
// [Placement] is the aggregate answer at one business instant, and
// [Overlay] is the people.WorkerFacts wrapper that projects it over a
// delegate's fact set: the committed assignment fields replace the recorded
// ones, and every other fact, the revision watermark and the provenance stay
// the delegate's. The watermark deliberately stays the delegate's own: it is
// what the proposal path binds a created worker's revision to, and this
// package changes what is disclosed, not which row a proposal is bound to.
//
// Nothing here writes, and an absent aggregate is absence, never an error: a
// worker with no projected aggregates reads exactly as it did before.
package committedfacts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Placement is what the aggregates record for one worker at one instant.
type Placement struct {
	JobCode, Grade, OrgUnit, PositionCode string
	Location, PayZone, FTE                string
	ManagerRef                            string
	BasePay, Currency                     string
}

// CurrentPlacement reads the worker's live assignment and base pay at
// businessAt inside the caller's tenant-scoped transaction. It reports false
// when the worker has no projected assignment.
func CurrentPlacement(ctx context.Context, ex dbport.Tx, tenant, workerID uuid.UUID, businessAt time.Time) (Placement, bool, error) {
	people, org, comp := aggregates.PeopleStore{}, aggregates.OrganizationStore{}, aggregates.CompensationStore{}
	employment, err := people.ActiveEmploymentForWorker(ctx, ex, tenant, workerID, businessAt)
	if errors.Is(err, aggregates.ErrNotFound) {
		return Placement{}, false, nil
	}
	if err != nil {
		return Placement{}, false, fmt.Errorf("committedfacts: read employment: %w", err)
	}
	assignment, err := people.PrimaryAssignmentForEmployment(ctx, ex, tenant, employment.EntityID, businessAt)
	if errors.Is(err, aggregates.ErrNotFound) {
		return Placement{}, false, nil
	}
	if err != nil {
		return Placement{}, false, fmt.Errorf("committedfacts: read assignment: %w", err)
	}
	out := Placement{
		JobCode: assignment.JobCode, Grade: assignment.Grade, Location: assignment.Location,
		PayZone: assignment.PayZone, FTE: assignment.FTE, ManagerRef: assignment.ManagerRelationshipRef,
	}
	if assignment.OrganizationRef != nil {
		if unit, unitErr := org.CurrentOrganizationUnit(ctx, ex, tenant, *assignment.OrganizationRef, businessAt); unitErr == nil {
			out.OrgUnit = unit.Code
		} else if !errors.Is(unitErr, aggregates.ErrNotFound) {
			return Placement{}, false, fmt.Errorf("committedfacts: read organization unit: %w", unitErr)
		}
	}
	if assignment.PositionRef != nil {
		if position, posErr := org.CurrentJobPosition(ctx, ex, tenant, *assignment.PositionRef, businessAt); posErr == nil {
			out.PositionCode = position.PositionCode
		} else if !errors.Is(posErr, aggregates.ErrNotFound) {
			return Placement{}, false, fmt.Errorf("committedfacts: read job position: %w", posErr)
		}
	}
	pkg, err := comp.ActivePackageForWorker(ctx, ex, tenant, workerID, businessAt)
	switch {
	case errors.Is(err, aggregates.ErrNotFound):
		return out, true, nil
	case err != nil:
		return Placement{}, false, fmt.Errorf("committedfacts: read compensation package: %w", err)
	}
	base, err := comp.BasePayComponentForPackage(ctx, ex, tenant, pkg.EntityID, businessAt)
	switch {
	case errors.Is(err, aggregates.ErrNotFound):
		return out, true, nil
	case err != nil:
		return Placement{}, false, fmt.Errorf("committedfacts: read base pay: %w", err)
	}
	out.BasePay, out.Currency = centsText(base.Amount), base.Currency
	return out, true, nil
}

// centsText renders a stored numeric(_, 4) amount at the money scale every
// surface and rule in the product reads pay at. An amount that genuinely
// carries a fraction of a cent is reported unchanged rather than rounded into
// a different number.
func centsText(amount string) string {
	stored, err := values.NewDecimal(amount, 4, values.RoundingExactRequired)
	if err != nil {
		return amount
	}
	cents, err := stored.Quantize(2, values.RoundingExactRequired)
	if err != nil {
		return amount
	}
	return cents.String()
}

// Reader reads placements in their own short, rolled-back transaction, for
// callers that hold none of their own.
type Reader struct {
	DB         dbport.Beginner
	TenantUUID func(values.TenantId) uuid.UUID
}

// PlacementAt resolves one worker's committed placement at businessAt.
func (r Reader) PlacementAt(ctx context.Context, tenant values.TenantId, workerID string, businessAt time.Time) (Placement, bool, error) {
	if r.DB == nil || r.TenantUUID == nil {
		return Placement{}, false, nil
	}
	tenantID := r.TenantUUID(tenant)
	id, err := uuid.Parse(workerID)
	if tenantID == uuid.Nil || err != nil {
		return Placement{}, false, nil
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return Placement{}, false, fmt.Errorf("committedfacts: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return Placement{}, false, fmt.Errorf("committedfacts: scope tenant: %w", err)
	}
	return CurrentPlacement(ctx, tx, tenantID, id, businessAt)
}

// Overlay is a people.WorkerFacts that answers from Base and then replaces
// the placement fields the aggregates record for the same worker.
type Overlay struct {
	Base   people.WorkerFacts
	Reader Reader
}

var _ people.WorkerFacts = Overlay{}

// WorkerFactsAt implements [people.WorkerFacts].
func (o Overlay) WorkerFactsAt(ctx context.Context, q people.FactQuery) (people.FactSet, error) {
	if o.Base == nil {
		return people.FactSet{}, fmt.Errorf("committedfacts: overlay has no base reader")
	}
	set, err := o.Base.WorkerFactsAt(ctx, q)
	if err != nil || !set.Exists {
		return set, err
	}
	at := businessInstant(q)
	placement, found, err := o.Reader.PlacementAt(ctx, q.Tenant, q.Worker.Id, at)
	if err != nil || !found {
		return set, err
	}
	replacements := map[people.FieldID]string{
		people.FieldJobCode: placement.JobCode, people.FieldGrade: placement.Grade,
		people.FieldOrgUnit: placement.OrgUnit, people.FieldLocation: placement.Location,
		people.FieldPayZone: placement.PayZone, people.FieldFTE: placement.FTE,
		people.FieldManagerRelation: placement.ManagerRef, people.FieldPositionID: placement.PositionCode,
	}
	for i := range set.Facts {
		replacement, governed := replacements[set.Facts[i].Field]
		if !governed || replacement == "" {
			continue
		}
		if _, disclosed := set.Facts[i].Value.Get(); !disclosed {
			// A withheld or absent field stays withheld: this overlay reports
			// what is recorded, it does not widen a disclosure decision.
			continue
		}
		set.Facts[i].Value = values.Value(replacement)
	}
	return set, nil
}

// businessInstant is the business coordinate a fact query asks about.
func businessInstant(q people.FactQuery) time.Time {
	if date := q.AsOf.EffectiveOn; date.IsSet() {
		return time.Date(int(date.Year()), date.Month(), int(date.Day()), 0, 0, 0, 0, time.UTC)
	}
	return q.AsOf.KnownAt.Instant().Time().UTC()
}
