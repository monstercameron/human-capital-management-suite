package demoworkforce

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
)

type Summary struct {
	Tenant   uuid.UUID
	Company  string
	Planned  int
	Inserted int
	Skipped  int
	Photos   int
}

// Seed appends the deterministic HarborCare people to the durable workforce
// projection. A replay verifies every existing row rather than treating an
// arbitrary uniqueness collision as a successful seed.
func Seed(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) (Summary, error) {
	return HarborCarePack.Seed(ctx, tx, tenant)
}

// Seed appends this company's deterministic people to the workforce
// projection.
func (p *Pack) Seed(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) (Summary, error) {
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return Summary{}, err
	}
	employees, err := p.Plan(tenant)
	if err != nil {
		return Summary{}, err
	}
	summary := Summary{Tenant: tenant, Company: p.Company.Name, Planned: len(employees)}
	store := workforce.Store{}
	for _, employee := range employees {
		if employee.HasProfilePhoto {
			summary.Photos++
		}
		_, createErr := store.Create(ctx, tx, employee.Row)
		if createErr == nil {
			summary.Inserted++
			continue
		}
		if !errors.Is(createErr, workforce.ErrDuplicate) {
			return Summary{}, fmt.Errorf("demoworkforce: create %s: %w", employee.Row.WorkerKey, createErr)
		}
		existing, found, getErr := store.Get(ctx, tx, tenant, employee.Row.WorkerKey)
		if getErr != nil {
			return Summary{}, fmt.Errorf("demoworkforce: verify %s: %w", employee.Row.WorkerKey, getErr)
		}
		if !found || !sameSeedIdentity(existing, employee.Row) {
			return Summary{}, fmt.Errorf("demoworkforce: existing worker %s conflicts with the deterministic %s seed", employee.Row.WorkerKey, p.DisplayName)
		}
		summary.Skipped++
	}
	return summary, nil
}

func sameSeedIdentity(have, want workforce.WorkerRow) bool {
	return have.WorkerID == want.WorkerID &&
		have.LegalName == want.LegalName && have.PreferredName == want.PreferredName && have.WorkerNumber == want.WorkerNumber &&
		have.WorkerType == want.WorkerType && have.LifecycleStatus == want.LifecycleStatus && have.PayBasis == want.PayBasis &&
		have.JobCode == want.JobCode && have.JobTitle == want.JobTitle && have.Grade == want.Grade && have.OrgUnit == want.OrgUnit && have.PositionID == want.PositionID &&
		have.Location == want.Location && have.PayZone == want.PayZone && have.FTE == want.FTE && have.ManagerRelationshipRef == want.ManagerRelationshipRef &&
		have.EmploymentType == want.EmploymentType && have.TimeType == want.TimeType &&
		have.Company == want.Company && have.BusinessUnit == want.BusinessUnit &&
		have.CostCenter == want.CostCenter && have.WorkArrangement == want.WorkArrangement &&
		have.ProfilePhotoOriginalRef == want.ProfilePhotoOriginalRef && have.ProfilePhotoProxyRef == want.ProfilePhotoProxyRef
}
