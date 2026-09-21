package positionfacts

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The batched half of this reader (PROMOUX-015 follow-up).
//
// [Reader.PositionRevisionAt] opens one transaction per position and reads
// four single rows inside it, which is right for the one-position question a
// proposal's preflight asks. A vacancy list asks the same question about
// every position the tenant has, twice each (position.CheckCompatibility and
// position.CalculateCapacity each resolve the revision), so a demo tenant
// publishing a hundred and sixty positions opened three hundred and twenty
// transactions to render one form. [Reader.RevisionsAt] answers the whole set
// in one transaction and four statements, and hands back a
// [position.PositionFacts] that replays those answers -- including the
// refusals -- so nothing downstream has to know which read it was served by.

// PreloadedRevisions is a [position.PositionFacts] that answers from an
// already-resolved set of revisions.
//
// It replays whatever [Reader.PositionRevisionAt] would have answered for
// each preloaded position, verdict for verdict: a resolved revision, a plain
// absence, or the same error. A query it was not preloaded for -- another
// tenant, another effective date, or a position outside the requested set --
// is delegated to Fallback rather than answered as absent, because silently
// reporting "no such position" for a question this value never asked would be
// a disclosure decision taken by a cache.
type PreloadedRevisions struct {
	// Fallback answers every query outside the preloaded set. It is the
	// Reader the set was built from.
	Fallback position.PositionFacts

	tenant  values.TenantId
	asOf    position.AsOf
	answers map[string]preloadedAnswer
}

// preloadedAnswer is one position's resolved verdict.
type preloadedAnswer struct {
	revision position.PositionRevision
	exists   bool
	err      error
}

var _ position.PositionFacts = PreloadedRevisions{}

// PositionRevisionAt implements [position.PositionFacts] from the preloaded
// set.
func (p PreloadedRevisions) PositionRevisionAt(ctx context.Context, q position.PositionQuery) (position.PositionRevision, bool, error) {
	// KnownAt is deliberately not compared: PositionRevisionAt reads the
	// live row at the business coordinate and never narrows by knownAt, so a
	// query that differs only there has the same answer.
	if q.Position.Tenant == p.tenant && q.AsOf.EffectiveOn == p.asOf.EffectiveOn {
		if answer, preloaded := p.answers[q.Position.Id]; preloaded {
			return answer.revision, answer.exists, answer.err
		}
	}
	if p.Fallback == nil {
		return position.PositionRevision{}, false, fmt.Errorf(
			"positionfacts: %s was not preloaded and this reader has no fallback", q.Position.Id)
	}
	return p.Fallback.PositionRevisionAt(ctx, q)
}

// RevisionsAt resolves every named position at one coordinate in a single
// tenant-scoped transaction.
//
// The returned reader answers exactly what a per-position
// [Reader.PositionRevisionAt] would have: the same four aggregate rows, the
// same absence for a position, job, organization unit or legal entity that
// does not resolve at this coordinate, the same error for a row this reader
// cannot project, and the same delegation to the underlying Reader for
// anything not asked about here.
//
// A transport failure is returned as an error, because that is a failure of
// the read rather than an answer about a position.
func (r Reader) RevisionsAt(
	ctx context.Context, tenant values.TenantId, asOf position.AsOf, positions []values.EntityRef,
) (position.PositionFacts, error) {
	preloaded := PreloadedRevisions{
		Fallback: r, tenant: tenant, asOf: asOf, answers: make(map[string]preloadedAnswer, len(positions)),
	}
	if r.DB == nil || r.TenantUUID == nil {
		return nil, fmt.Errorf("positionfacts: reader has no database and tenant resolver configured")
	}
	if err := tenant.Validate(); err != nil {
		return nil, fmt.Errorf("positionfacts: revisions tenant: %w", err)
	}
	if err := asOf.Validate(); err != nil {
		return nil, fmt.Errorf("positionfacts: revisions coordinate: %w", err)
	}
	tenantID := r.TenantUUID(tenant)

	// entityIDs keeps the caller's own order so the answers are built in it;
	// wanted maps each requested reference back to the id it decoded to.
	entityIDs := make([]uuid.UUID, 0, len(positions))
	wanted := make(map[string]uuid.UUID, len(positions))
	for _, ref := range positions {
		if ref.Tenant != tenant {
			// Another tenant's reference is not this batch's to answer; it
			// goes to the fallback, which applies its own tenant mapping.
			continue
		}
		entityID, err := uuid.Parse(ref.Id)
		if err != nil || tenantID == uuid.Nil {
			// Exactly PositionRevisionAt's own two silent absences: an id
			// that is not a canonical job_position identifier, and a tenant
			// this reader has no physical mapping for.
			preloaded.answers[ref.Id] = preloadedAnswer{}
			continue
		}
		if _, already := wanted[ref.Id]; already {
			continue
		}
		wanted[ref.Id] = entityID
		entityIDs = append(entityIDs, entityID)
	}
	if len(entityIDs) == 0 {
		return preloaded, nil
	}
	businessAt := localDateToTime(asOf.EffectiveOn)

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("positionfacts: revisions begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return nil, fmt.Errorf("positionfacts: revisions scope tenant: %w", err)
	}

	store := aggregates.OrganizationStore{}
	jobPositions, err := store.CurrentJobPositions(ctx, tx, tenantID, entityIDs, businessAt)
	if err != nil {
		return nil, fmt.Errorf("positionfacts: current job positions: %w", err)
	}
	orgRefs := make([]uuid.UUID, 0, len(jobPositions))
	jobRefs := make([]uuid.UUID, 0, len(jobPositions))
	for _, jobPosition := range jobPositions {
		orgRefs = append(orgRefs, jobPosition.OrganizationRef)
		jobRefs = append(jobRefs, jobPosition.JobRef)
	}
	orgUnits, err := store.CurrentOrganizationUnits(ctx, tx, tenantID, orgRefs, businessAt)
	if err != nil {
		return nil, fmt.Errorf("positionfacts: current organization units: %w", err)
	}
	jobs, err := store.CurrentJobs(ctx, tx, tenantID, jobRefs, businessAt)
	if err != nil {
		return nil, fmt.Errorf("positionfacts: current jobs: %w", err)
	}
	legalEntityRefs := make([]uuid.UUID, 0, len(jobPositions))
	for _, jobPosition := range jobPositions {
		if ref := legalEntityRefFor(jobPosition, orgUnits); ref != nil {
			legalEntityRefs = append(legalEntityRefs, *ref)
		}
	}
	legalEntities, err := store.CurrentLegalEntities(ctx, tx, tenantID, legalEntityRefs, businessAt)
	if err != nil {
		return nil, fmt.Errorf("positionfacts: current legal entities: %w", err)
	}

	for _, ref := range positions {
		entityID, batched := wanted[ref.Id]
		if !batched {
			continue
		}
		preloaded.answers[ref.Id] = resolveOne(
			values.EntityRef{Tenant: tenant, Kind: ref.Kind, Id: ref.Id},
			entityID, jobPositions, orgUnits, jobs, legalEntities)
	}
	return preloaded, nil
}

// legalEntityRefFor is [Reader.PositionRevisionAt]'s own fallback: the
// position's declared legal entity, or its organization unit's when the
// position declares none.
func legalEntityRefFor(jobPosition aggregates.JobPosition, orgUnits map[uuid.UUID]aggregates.OrganizationUnit) *uuid.UUID {
	if jobPosition.LegalEntityRef != nil {
		return jobPosition.LegalEntityRef
	}
	if unit, found := orgUnits[jobPosition.OrganizationRef]; found {
		return unit.LegalEntityRef
	}
	return nil
}

// resolveOne reproduces [Reader.PositionRevisionAt]'s ground order over the
// batched rows: the position, then its organization unit, then its job, then
// its legal entity, each absence reported as absence and only a position that
// carries no legal entity at all reported as an error.
func resolveOne(
	ref values.EntityRef, entityID uuid.UUID,
	jobPositions map[uuid.UUID]aggregates.JobPosition,
	orgUnits map[uuid.UUID]aggregates.OrganizationUnit,
	jobs map[uuid.UUID]aggregates.Job,
	legalEntities map[uuid.UUID]aggregates.LegalEntity,
) preloadedAnswer {
	jobPosition, found := jobPositions[entityID]
	if !found {
		return preloadedAnswer{}
	}
	orgUnit, found := orgUnits[jobPosition.OrganizationRef]
	if !found {
		// A position whose declared organization unit does not resolve at
		// this coordinate is a data-integrity gap, not a caller error.
		return preloadedAnswer{}
	}
	job, found := jobs[jobPosition.JobRef]
	if !found {
		return preloadedAnswer{}
	}
	legalEntityRef := jobPosition.LegalEntityRef
	if legalEntityRef == nil {
		legalEntityRef = orgUnit.LegalEntityRef
	}
	if legalEntityRef == nil {
		return preloadedAnswer{err: fmt.Errorf(
			"positionfacts: job_position %s and its organization unit both carry no legal entity", entityID)}
	}
	legalEntity, found := legalEntities[*legalEntityRef]
	if !found {
		return preloadedAnswer{}
	}
	revision, exists, err := buildRevision(ref, entityID, jobPosition, orgUnit, job, legalEntity)
	return preloadedAnswer{revision: revision, exists: exists, err: err}
}
