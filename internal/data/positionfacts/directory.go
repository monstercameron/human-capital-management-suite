package positionfacts

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DirectoryRow is one position this cell can see, with the display labels a
// picker needs and the occupancy the Position domain needs to compute what
// is left of it.
//
// It is deliberately not a positionpicker.DirectoryEntry: this package
// reads rows, and which of those rows a particular viewer may be offered is
// a Position-domain decision (CheckCompatibility's Authorize hook), taken
// one layer up. Nothing here is authorization -- the caller must not treat
// a row's presence as permission to disclose it.
type DirectoryRow struct {
	Position values.EntityRef
	// Title is the job's title, Organization the organization unit's name,
	// and Location the position's own recorded location. All three are
	// presentation labels; none of them is used to decide anything.
	Title        string
	Organization string
	Location     string
	// JobCode and OrgUnit are the same two codes PositionRevisionAt reports
	// on the revision for this row (job.code and organization_unit.code), so
	// a caller that narrows a list by them narrows it by exactly what
	// position.CheckCompatibility would compare against.
	JobCode string
	OrgUnit string
	// Occupants is this position's own occupancy at the coordinate, in the
	// shape position.CalculateCapacity takes. It belongs to the row rather
	// than to the request because an Occupant carries no position of its
	// own, so a caller that pooled them across positions would be telling
	// the domain every position is as full as the busiest one.
	Occupants []position.Occupant
}

// Directory lists the tenant's positions effective at asOf, with their
// occupancy. There is no filtering here beyond the bitemporal coordinate:
// "which of these may this viewer see" and "which of these still have room"
// are both Position-domain questions, and answering either one here would
// put the same decision in two places.
//
// It exists because internal/domains/position has no list-all port and
// deliberately will not grow one -- a per-reference compatibility port
// cannot certify what the set of all positions is. Somebody has to read the
// table, and the adapter that already reads it for one reference is the
// honest place.
func (r Reader) Directory(ctx context.Context, tenant values.TenantId, asOf position.AsOf) ([]DirectoryRow, error) {
	if r.DB == nil || r.TenantUUID == nil {
		return nil, fmt.Errorf("positionfacts: reader has no database and tenant resolver configured")
	}
	if err := tenant.Validate(); err != nil {
		return nil, fmt.Errorf("positionfacts: directory tenant: %w", err)
	}
	if err := asOf.Validate(); err != nil {
		return nil, fmt.Errorf("positionfacts: directory coordinate: %w", err)
	}
	tenantID := r.TenantUUID(tenant)
	if tenantID == uuid.Nil {
		// No physical mapping is the same answer as no rows, never an
		// error: a tenant this cell does not host has no positions here.
		return nil, nil
	}
	businessAt := localDateToTime(asOf.EffectiveOn)

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("positionfacts: directory begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return nil, fmt.Errorf("positionfacts: directory scope tenant: %w", err)
	}

	rows, err := tx.Query(ctx, directoryQuery, tenantID, businessAt)
	if err != nil {
		return nil, fmt.Errorf("positionfacts: directory query: %w", err)
	}
	defer rows.Close()

	out := []DirectoryRow{}
	index := map[uuid.UUID]int{}
	// unnamed collects positions whose occupancy does not name a worker.
	// position.Occupant has no shape for an anonymous occupant, and reading
	// such a row as "nobody is here" would offer an occupied position as
	// vacant, so the position is withheld entirely instead.
	unnamed := map[uuid.UUID]bool{}
	for rows.Next() {
		var (
			entityID     uuid.UUID
			title        *string
			organization *string
			location     *string
			jobCode      *string
			orgUnitCode  *string
		)
		if err := rows.Scan(&entityID, &title, &organization, &location, &jobCode, &orgUnitCode); err != nil {
			return nil, fmt.Errorf("positionfacts: directory scan: %w", err)
		}
		index[entityID] = len(out)
		out = append(out, DirectoryRow{
			Position:     values.EntityRef{Tenant: tenant, Kind: position.KindPosition, Id: entityID.String()},
			Title:        derefString(title),
			Organization: derefString(organization),
			Location:     derefString(location),
			JobCode:      derefString(jobCode),
			OrgUnit:      derefString(orgUnitCode),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("positionfacts: directory rows: %w", err)
	}
	rows.Close()

	occupancy, err := tx.Query(ctx, directoryOccupancyQuery, tenantID, businessAt)
	if err != nil {
		return nil, fmt.Errorf("positionfacts: directory occupancy query: %w", err)
	}
	defer occupancy.Close()
	for occupancy.Next() {
		var (
			positionRef uuid.UUID
			workerRef   *uuid.UUID
			allocation  *string
			primaryFlag *bool
			from        time.Time
			to          *time.Time
		)
		if err := occupancy.Scan(&positionRef, &workerRef, &allocation, &primaryFlag, &from, &to); err != nil {
			return nil, fmt.Errorf("positionfacts: directory occupancy scan: %w", err)
		}
		if workerRef == nil {
			unnamed[positionRef] = true
			continue
		}
		at, known := index[positionRef]
		if !known {
			// An occupancy row for a position this coordinate does not show
			// is not this reader's to reconcile. Dropping it is the safe
			// direction only because the position it names is not offered
			// either.
			continue
		}
		fte, err := occupancyFTE(allocation)
		if err != nil {
			return nil, fmt.Errorf("positionfacts: position_occupancy %s allocation_fte: %w", positionRef, err)
		}
		interval, err := effectiveInterval(from, to)
		if err != nil {
			return nil, fmt.Errorf("positionfacts: position_occupancy %s effective interval: %w", positionRef, err)
		}
		out[at].Occupants = append(out[at].Occupants, position.Occupant{
			Worker:    values.EntityRef{Tenant: tenant, Kind: position.KindWorker, Id: workerRef.String()},
			FTE:       fte,
			Effective: interval,
			Exclusive: primaryFlag != nil && *primaryFlag,
		})
	}
	if err := occupancy.Err(); err != nil {
		return nil, fmt.Errorf("positionfacts: directory occupancy rows: %w", err)
	}
	if len(unnamed) == 0 {
		return out, nil
	}
	kept := out[:0]
	for _, row := range out {
		id, err := uuid.Parse(row.Position.Id)
		if err != nil || !unnamed[id] {
			kept = append(kept, row)
		}
	}
	return kept, nil
}

// occupancyFTE reads the stored allocation. A row with no recorded
// allocation is read as one whole head rather than as zero: an occupancy
// row exists because somebody is in the position, and reading a missing
// number as "takes up nothing" would offer an occupied position as vacant.
func occupancyFTE(stored *string) (values.Decimal, error) {
	text := "1"
	if stored != nil && *stored != "" {
		text = *stored
	}
	return values.NewDecimal(text, capacityScale(text), values.RoundingExactRequired)
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// directoryQuery selects the row current at the coordinate for each
// position: not superseded, effective at businessAt, with its job title and
// organization unit name resolved at the same coordinate. A position whose
// job or organization unit does not resolve there is left out for the same
// reason PositionRevisionAt reports it absent -- a half-built position is
// not a choice.
const directoryQuery = `
SELECT p.entity_id, j.title, o.name, p.location, j.code, o.code
FROM job_position p
JOIN job j
  ON j.tenant_id = p.tenant_id
 AND j.entity_id = p.job_ref
 AND j.superseded_at IS NULL
 AND j.effective_from <= $2
 AND (j.effective_to IS NULL OR j.effective_to > $2)
JOIN organization_unit o
  ON o.tenant_id = p.tenant_id
 AND o.entity_id = p.organization_ref
 AND o.superseded_at IS NULL
 AND o.effective_from <= $2
 AND (o.effective_to IS NULL OR o.effective_to > $2)
WHERE p.tenant_id = $1
  AND p.superseded_at IS NULL
  AND p.effective_from <= $2
  AND (p.effective_to IS NULL OR p.effective_to > $2)
ORDER BY j.title, o.name, p.entity_id
`

// directoryOccupancyQuery selects the occupancy current at the same
// coordinate. Effective-to is read as exclusive, matching the position
// query above, so a placement that ends today does not also start today.
const directoryOccupancyQuery = `
SELECT c.position_ref, c.worker_ref, c.allocation_fte::text, c.primary_flag, c.effective_from, c.effective_to
FROM position_occupancy c
WHERE c.tenant_id = $1
  AND c.superseded_at IS NULL
  AND c.effective_from <= $2
  AND (c.effective_to IS NULL OR c.effective_to > $2)
`
