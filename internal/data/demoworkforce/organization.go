package demoworkforce

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

type OrganizationSummary struct {
	LegalEntityInserted bool
	UnitsPlanned        int
	UnitsInserted       int
	UnitsSkipped        int
}

// SeedOrganization records HarborCare's legal entity and hierarchy in the
// existing bitemporal organization aggregate. It checks for the deterministic
// entity IDs before inserting so a replay never manufactures new history.
func SeedOrganization(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) (OrganizationSummary, error) {
	return HarborCarePack.SeedOrganization(ctx, tx, tenant)
}

// SeedOrganization records this company's legal entity and hierarchy.
func (p *Pack) SeedOrganization(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) (OrganizationSummary, error) {
	company := p.Company
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return OrganizationSummary{}, err
	}
	store := aggregates.OrganizationStore{}
	effective := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	recorded := time.Date(2026, time.September, 1, 13, 45, 0, 0, time.UTC)
	legalID := deterministicID("legal-entity", company.LegalEntity)
	summary := OrganizationSummary{UnitsPlanned: len(company.Units)}

	legal, err := store.CurrentLegalEntity(ctx, tx, tenant, legalID, effective)
	switch {
	case err == nil:
		if legal.RegisteredName != company.LegalEntity || legal.LifecycleState != "ACTIVE" {
			return OrganizationSummary{}, fmt.Errorf("demoworkforce: existing %s legal entity conflicts with the deterministic seed", p.DisplayName)
		}
	case errors.Is(err, aggregates.ErrNotFound):
		candidate, buildErr := aggregates.NewLegalEntity(tenant, legalID, effective, nil, recorded, company.LegalEntity, "ACTIVE")
		if buildErr != nil {
			return OrganizationSummary{}, buildErr
		}
		if _, putErr := store.PutLegalEntity(ctx, tx, candidate); putErr != nil {
			return OrganizationSummary{}, fmt.Errorf("demoworkforce: seed legal entity: %w", putErr)
		}
		summary.LegalEntityInserted = true
	default:
		return OrganizationSummary{}, fmt.Errorf("demoworkforce: read legal entity: %w", err)
	}

	unitIDs := make(map[string]uuid.UUID, len(company.Units))
	for _, unit := range company.Units {
		unitIDs[unit.Code] = deterministicID("organization-unit", unit.Code)
	}
	for _, unit := range company.Units {
		var parent *uuid.UUID
		if unit.ParentCode != "" {
			parentID, ok := unitIDs[unit.ParentCode]
			if !ok {
				return OrganizationSummary{}, fmt.Errorf("demoworkforce: organization unit %s has unknown parent %s", unit.Code, unit.ParentCode)
			}
			parent = &parentID
		}
		unitID := unitIDs[unit.Code]
		existing, readErr := store.CurrentOrganizationUnit(ctx, tx, tenant, unitID, effective)
		switch {
		case readErr == nil:
			if !sameOrganizationUnit(existing, unit, legalID, parent) {
				return OrganizationSummary{}, fmt.Errorf("demoworkforce: existing organization unit %s conflicts with the deterministic seed", unit.Code)
			}
			summary.UnitsSkipped++
		case errors.Is(readErr, aggregates.ErrNotFound):
			candidate, buildErr := aggregates.NewOrganizationUnit(
				tenant, unitID, effective, nil, recorded, unit.Type, unit.Code, unit.Name, &legalID, parent, "ACTIVE",
			)
			if buildErr != nil {
				return OrganizationSummary{}, fmt.Errorf("demoworkforce: build organization unit %s: %w", unit.Code, buildErr)
			}
			if _, putErr := store.PutOrganizationUnit(ctx, tx, candidate); putErr != nil {
				return OrganizationSummary{}, fmt.Errorf("demoworkforce: seed organization unit %s: %w", unit.Code, putErr)
			}
			summary.UnitsInserted++
		default:
			return OrganizationSummary{}, fmt.Errorf("demoworkforce: read organization unit %s: %w", unit.Code, readErr)
		}
	}
	return summary, nil
}

func sameOrganizationUnit(have aggregates.OrganizationUnit, want OrganizationUnit, legalID uuid.UUID, parent *uuid.UUID) bool {
	if have.OrgType != want.Type || have.Code != want.Code || have.Name != want.Name || have.LifecycleState != "ACTIVE" || have.LegalEntityRef == nil || *have.LegalEntityRef != legalID {
		return false
	}
	if parent == nil {
		return have.ParentOrganizationRef == nil
	}
	return have.ParentOrganizationRef != nil && *have.ParentOrganizationRef == *parent
}
