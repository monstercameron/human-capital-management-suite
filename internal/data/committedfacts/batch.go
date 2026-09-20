package committedfacts

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// CurrentPlacements is [CurrentPlacement] for a whole listed population.
//
// It exists because the served worker directory resolved one placement per
// worker, each through its own five single-row reads: a sixty-worker tenant
// spent three hundred sequential round trips answering one page. The answer
// here is the identical one -- the same tables, the same coordinate, the same
// "an absent aggregate is absence, never an error" rule -- computed in a
// bounded six statements no matter how many workers are asked about.
//
// A worker with no active employment, or an employment with no primary
// assignment, is absent from the returned map, which is exactly the false
// [CurrentPlacement] reports for the same worker. A worker present in the map
// always has at least an assignment; its organization unit, position code and
// base pay are filled in only where the aggregates record them, one field at
// a time, as [CurrentPlacement] fills them.
func CurrentPlacements(
	ctx context.Context, ex dbport.Tx, tenant uuid.UUID, workerIDs []uuid.UUID, businessAt time.Time,
) (map[uuid.UUID]Placement, error) {
	out := make(map[uuid.UUID]Placement, len(workerIDs))
	if len(workerIDs) == 0 {
		return out, nil
	}
	people, org, comp := aggregates.PeopleStore{}, aggregates.OrganizationStore{}, aggregates.CompensationStore{}

	employments, err := people.ActiveEmploymentsForWorkers(ctx, ex, tenant, workerIDs, businessAt)
	if err != nil {
		return nil, fmt.Errorf("committedfacts: read employments: %w", err)
	}
	if len(employments) == 0 {
		return out, nil
	}
	employmentIDs := make([]uuid.UUID, 0, len(employments))
	for _, employment := range employments {
		employmentIDs = append(employmentIDs, employment.EntityID)
	}
	assignments, err := people.PrimaryAssignmentsForEmployments(ctx, ex, tenant, employmentIDs, businessAt)
	if err != nil {
		return nil, fmt.Errorf("committedfacts: read assignments: %w", err)
	}

	// placed is the workers that got as far as an assignment, in the caller's
	// own order, so every later lookup asks about exactly the workers
	// CurrentPlacement would have gone on to ask about.
	placed := make([]uuid.UUID, 0, len(workerIDs))
	orgRefs := make([]uuid.UUID, 0, len(workerIDs))
	positionRefs := make([]uuid.UUID, 0, len(workerIDs))
	for _, workerID := range workerIDs {
		employment, hasEmployment := employments[workerID]
		if !hasEmployment {
			continue
		}
		assignment, hasAssignment := assignments[employment.EntityID]
		if !hasAssignment {
			continue
		}
		if _, already := out[workerID]; already {
			continue
		}
		placed = append(placed, workerID)
		out[workerID] = Placement{
			JobCode: assignment.JobCode, Grade: assignment.Grade, Location: assignment.Location,
			PayZone: assignment.PayZone, FTE: assignment.FTE, ManagerRef: assignment.ManagerRelationshipRef,
		}
		if assignment.OrganizationRef != nil {
			orgRefs = append(orgRefs, *assignment.OrganizationRef)
		}
		if assignment.PositionRef != nil {
			positionRefs = append(positionRefs, *assignment.PositionRef)
		}
	}
	if len(placed) == 0 {
		return out, nil
	}

	units, err := org.CurrentOrganizationUnits(ctx, ex, tenant, orgRefs, businessAt)
	if err != nil {
		return nil, fmt.Errorf("committedfacts: read organization units: %w", err)
	}
	positions, err := org.CurrentJobPositions(ctx, ex, tenant, positionRefs, businessAt)
	if err != nil {
		return nil, fmt.Errorf("committedfacts: read job positions: %w", err)
	}
	for _, workerID := range placed {
		assignment := assignments[employments[workerID].EntityID]
		placement := out[workerID]
		if assignment.OrganizationRef != nil {
			if unit, found := units[*assignment.OrganizationRef]; found {
				placement.OrgUnit = unit.Code
			}
		}
		if assignment.PositionRef != nil {
			if jobPosition, found := positions[*assignment.PositionRef]; found {
				placement.PositionCode = jobPosition.PositionCode
			}
		}
		out[workerID] = placement
	}

	packages, err := comp.ActivePackagesForWorkers(ctx, ex, tenant, placed, businessAt)
	if err != nil {
		return nil, fmt.Errorf("committedfacts: read compensation packages: %w", err)
	}
	if len(packages) == 0 {
		return out, nil
	}
	packageIDs := make([]uuid.UUID, 0, len(packages))
	for _, workerID := range placed {
		if pkg, found := packages[workerID]; found {
			packageIDs = append(packageIDs, pkg.EntityID)
		}
	}
	components, err := comp.BasePayComponentsForPackages(ctx, ex, tenant, packageIDs, businessAt)
	if err != nil {
		return nil, fmt.Errorf("committedfacts: read base pay: %w", err)
	}
	for _, workerID := range placed {
		pkg, hasPackage := packages[workerID]
		if !hasPackage {
			continue
		}
		base, hasBase := components[pkg.EntityID]
		if !hasBase {
			continue
		}
		placement := out[workerID]
		placement.BasePay, placement.Currency = centsText(base.Amount), base.Currency
		out[workerID] = placement
	}
	return out, nil
}
