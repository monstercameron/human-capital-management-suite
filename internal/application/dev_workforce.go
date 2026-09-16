package application

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionterminal"
)

// bootstrapLocalDevWorkforce makes the local browser identities genuine
// members of the same durable tenant population the Journey service reads.
// Both seeders are replay-safe, and one transaction prevents a login page
// from advertising an account before its worker and organization exist.
//
// WF-RUN-034: the same transaction projects every journey_worker row into the
// bitemporal aggregates a promotion commits against and records the
// promotion catalog (jobs, OPEN vacancies, compensation pools), so a served
// promotion resolves real rows instead of none.
func bootstrapLocalDevWorkforce(ctx context.Context, pool *pgxadapter.Pool, tenant string) (demoworkforce.Summary, demoworkforce.OrganizationSummary, error) {
	if pool == nil || tenant != demoworkforce.CompanyKey {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("begin local development workforce seed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tenantID := pgstore.TenantID(tenant)
	organization, err := demoworkforce.SeedOrganization(ctx, tx, tenantID)
	if err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed local development organization: %w", err)
	}
	seeded, err := demoworkforce.Seed(ctx, tx, tenantID)
	if err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed local development workforce: %w", err)
	}
	rows, err := (workforce.Store{}).List(ctx, tx, tenantID)
	if err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("list the local development workforce: %w", err)
	}
	for _, row := range rows {
		if _, err := demoworkforce.ProjectWorker(ctx, tx, row, demoworkforce.HarborCare.LegalEntity); err != nil {
			return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("project the local development workforce: %w", err)
		}
	}
	// The catalog is stamped with the same fixed knowledge instant the
	// HarborCare organization seed uses, so every proposal produced after the
	// seed reads it as known.
	catalog, err := app.PromotionAggregateCatalog(time.Date(2026, time.September, 1, 13, 45, 0, 0, time.UTC))
	if err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, err
	}
	if _, err := demoworkforce.SeedAggregateCatalog(ctx, tx, tenantID, catalog); err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed the promotion catalog: %w", err)
	}
	// A committed promotion enqueues its payroll and IAM sync legs, and the
	// outbox foreign-keys each leg's payload schema. Registering exactly the
	// schemas the terminal resolver renders is a composition act: an effect
	// under any other schema stays refused.
	if err := promotioncommit.RegisterEffectSchemas(ctx, tx, tenantID, promotionterminal.EffectSchemaRefs()...); err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("commit local development workforce seed: %w", err)
	}
	return seeded, organization, nil
}
