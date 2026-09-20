// Package positionfacts is PROMOUX-004's production, database-backed
// internal/domains/position.PositionFacts adapter: the read half of "resolve
// to a real, authorized, vacant position," over the DB-009
// job_position/job/organization_unit/legal_entity tables read through
// internal/data/aggregates.
//
// There is no judgment here. A position id that is not a canonical UUID, a
// tenant this reader has no physical mapping for, or a coordinate that
// matches no row is reported as not found, never guessed at or defaulted.
// internal/domains/position.CheckCompatibility and CalculateCapacity are
// what turn "found" into a business answer; this package only turns rows
// into the typed revision they describe.
package positionfacts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// businessCalendar is the calendar every effective interval this reader
// builds is expressed against. It names no holiday schedule of its own
// (DB-009's stored intervals are already resolved dates), so one stable
// reference is enough.
var businessCalendar = values.CalendarRef{Ref: "hcmnext.position.job_position", Version: "1"}

// Reader is the position.PositionFacts adapter.
type Reader struct {
	// DB opens the tenant-scoped, read-only transaction each read runs
	// inside. A read-only preview page and a real proposal's preflight both
	// only ever call PositionRevisionAt, which never writes.
	DB dbport.Beginner
	// TenantUUID resolves the domain's logical tenant key to the physical
	// tenant UUID DB-009's tables key on.
	TenantUUID func(values.TenantId) uuid.UUID
}

var _ position.PositionFacts = Reader{}

// PositionRevisionAt implements position.PositionFacts over job_position,
// job, organization_unit and legal_entity.
//
// The physical job_position row carries capacity_fte but no head count, so
// -- exactly like internal/domains/promotion/snapshot's own aggregate
// integration fixture documents for the identical schema -- a position with
// any positive capacity at all is reported as exactly one head. That is a
// property of the current schema, stated here rather than hidden: a schema
// that grows a head-count column replaces this one line, it does not
// invalidate the revision this reader discloses today.
func (r Reader) PositionRevisionAt(ctx context.Context, q position.PositionQuery) (position.PositionRevision, bool, error) {
	if r.DB == nil || r.TenantUUID == nil {
		return position.PositionRevision{}, false, fmt.Errorf("positionfacts: reader has no database and tenant resolver configured")
	}
	entityID, err := uuid.Parse(q.Position.Id)
	if err != nil {
		// Not a canonical job_position identifier at all -- reported as
		// absent, never as an error. A guessed or malformed id is exactly
		// what this branch exists to refuse without disclosing anything
		// about why.
		return position.PositionRevision{}, false, nil
	}
	tenantID := r.TenantUUID(q.Position.Tenant)
	if tenantID == uuid.Nil {
		return position.PositionRevision{}, false, nil
	}
	businessAt := localDateToTime(q.AsOf.EffectiveOn)

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return position.PositionRevision{}, false, fmt.Errorf("positionfacts: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return position.PositionRevision{}, false, fmt.Errorf("positionfacts: scope tenant: %w", err)
	}

	store := aggregates.OrganizationStore{}
	jobPosition, err := store.CurrentJobPosition(ctx, tx, tenantID, entityID, businessAt)
	if err != nil {
		if errors.Is(err, aggregates.ErrNotFound) {
			return position.PositionRevision{}, false, nil
		}
		return position.PositionRevision{}, false, fmt.Errorf("positionfacts: current job position: %w", err)
	}

	orgUnit, err := store.CurrentOrganizationUnit(ctx, tx, tenantID, jobPosition.OrganizationRef, businessAt)
	if err != nil {
		if errors.Is(err, aggregates.ErrNotFound) {
			// A position whose declared organization unit does not resolve
			// at this coordinate is a data-integrity gap, not a caller
			// error; it is reported as absent rather than disclosed
			// half-built.
			return position.PositionRevision{}, false, nil
		}
		return position.PositionRevision{}, false, fmt.Errorf("positionfacts: current organization unit: %w", err)
	}

	job, err := store.CurrentJob(ctx, tx, tenantID, jobPosition.JobRef, businessAt)
	if err != nil {
		if errors.Is(err, aggregates.ErrNotFound) {
			return position.PositionRevision{}, false, nil
		}
		return position.PositionRevision{}, false, fmt.Errorf("positionfacts: current job: %w", err)
	}

	legalEntityRef := jobPosition.LegalEntityRef
	if legalEntityRef == nil {
		legalEntityRef = orgUnit.LegalEntityRef
	}
	if legalEntityRef == nil {
		return position.PositionRevision{}, false, fmt.Errorf(
			"positionfacts: job_position %s and its organization unit both carry no legal entity", entityID)
	}
	legalEntity, err := store.CurrentLegalEntity(ctx, tx, tenantID, *legalEntityRef, businessAt)
	if err != nil {
		if errors.Is(err, aggregates.ErrNotFound) {
			return position.PositionRevision{}, false, nil
		}
		return position.PositionRevision{}, false, fmt.Errorf("positionfacts: current legal entity: %w", err)
	}

	return buildRevision(q.Position, entityID, jobPosition, orgUnit, job, legalEntity)
}

// buildRevision projects the four resolved aggregate rows onto the disclosed
// revision. It is shared with the batched reader (batch.go) so one set of
// rows can only ever produce one revision, whether they were read one
// position at a time or a whole directory at once.
func buildRevision(
	ref values.EntityRef, entityID uuid.UUID,
	jobPosition aggregates.JobPosition, orgUnit aggregates.OrganizationUnit,
	job aggregates.Job, legalEntity aggregates.LegalEntity,
) (position.PositionRevision, bool, error) {
	lifecycle := position.Lifecycle(jobPosition.LifecycleState)
	if !lifecycle.Valid() {
		return position.PositionRevision{}, false, fmt.Errorf(
			"positionfacts: job_position %s lifecycle_state %q is not a recognized position.Lifecycle", entityID, jobPosition.LifecycleState)
	}

	effective, err := effectiveInterval(jobPosition.EffectiveFrom, jobPosition.EffectiveTo)
	if err != nil {
		return position.PositionRevision{}, false, fmt.Errorf("positionfacts: job_position %s effective interval: %w", entityID, err)
	}

	capacityFTE, err := values.NewDecimal(jobPosition.CapacityFTE, capacityScale(jobPosition.CapacityFTE), values.RoundingExactRequired)
	if err != nil {
		return position.PositionRevision{}, false, fmt.Errorf("positionfacts: job_position %s capacity_fte: %w", entityID, err)
	}
	heads := int64(0)
	if capacityFTE.Sign() > 0 {
		heads = 1
	}

	revision, err := values.NewOpaqueRevision("aggregate.job_position."+entityID.String(), []byte(jobPosition.Digest))
	if err != nil {
		return position.PositionRevision{}, false, fmt.Errorf("positionfacts: job_position %s revision: %w", entityID, err)
	}

	recordedAt, err := values.NewRecordedAt(values.NewInstant(jobPosition.RecordedAt.UTC()))
	if err != nil {
		return position.PositionRevision{}, false, fmt.Errorf("positionfacts: job_position %s recorded_at: %w", entityID, err)
	}

	return position.PositionRevision{
		Position:    ref,
		Revision:    revision,
		Effective:   effective,
		Lifecycle:   lifecycle,
		JobCode:     job.Code,
		OrgUnit:     orgUnit.Code,
		LegalEntity: legalEntity.RegisteredName,
		Capacity:    position.CapacityPolicy{CapacityFTE: capacityFTE, CapacityHeads: heads},
		Authority: evidence.SourceAuthority{
			Kind: evidence.AuthorityLocal, System: "hcmnext.position", PolicyRef: "position.source_authority/2026.1",
		},
		Provenance: evidence.Provenance{
			Source: "hcmnext.position", EvidenceRef: jobPosition.Digest, RecordedAt: recordedAt,
		},
	}, true, nil
}

// effectiveInterval builds the position's own effective-dated interval:
// half-open [from, to) when the row has a determinate end, open-ended
// otherwise.
func effectiveInterval(from time.Time, to *time.Time) (values.EffectiveInterval, error) {
	start := localDateFromTime(from)
	if to == nil {
		return values.NewOpenLocalDateInterval(start, businessCalendar)
	}
	// DB-009's effective_to is an exclusive timestamptz boundary (the row
	// stops being live at that instant), but values.NewLocalDateInterval's
	// end is the last covered calendar day, inclusive. The stored boundary
	// is always midnight UTC (every Put in this schema writes date-aligned
	// bounds), so the last covered day is exactly one day before it.
	end := localDateFromTime(*to).AddDays(-1)
	return values.NewLocalDateInterval(start, end, businessCalendar)
}

func localDateFromTime(t time.Time) values.LocalDate {
	t = t.UTC()
	d, err := values.NewLocalDate(t.Year(), t.Month(), t.Day())
	if err != nil {
		// t is always a value this reader itself read back from a stored
		// timestamptz column, never caller input; a calendar date built from
		// a real time.Time's own year/month/day cannot fail this
		// construction, so there is no caller-facing error path for it.
		return values.LocalDate{}
	}
	return d
}

func localDateToTime(d values.LocalDate) time.Time {
	if !d.IsSet() {
		return time.Time{}
	}
	return time.Date(int(d.Year()), d.Month(), int(d.Day()), 0, 0, 0, 0, time.UTC)
}

// capacityScale reports the number of digits after the decimal point in a
// stored exact-decimal text value, so the parsed Decimal's scale matches
// exactly what the row declared rather than a hardcoded assumption.
func capacityScale(text string) int32 {
	for i, r := range text {
		if r == '.' {
			return int32(len(text) - i - 1)
		}
	}
	return 0
}
