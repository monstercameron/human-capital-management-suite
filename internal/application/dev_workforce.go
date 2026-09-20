package application

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/jobarchstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/performancestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionterminal"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// demoSeedRecordedAt is the fixed knowledge instant every catalog row this
// bootstrap writes is stamped with. It is declared rather than read off the
// wall clock so a reseeded database is byte-identical to the one before it,
// and it is a function rather than a package-level var because the
// composition root holds no package state of its own (ARCH-GO-020, proved by
// TestCompositionRootRejectsGlobalRegistrationAndHiddenDependencies).
func demoSeedRecordedAt() time.Time {
	return time.Date(2026, time.September, 1, 13, 45, 0, 0, time.UTC)
}

// bootstrapLocalDevWorkforce makes the local browser identities genuine
// members of the same durable tenant population the Journey service reads.
// Both seeders are replay-safe, and one transaction prevents a login page
// from advertising an account before its worker and organization exist.
//
// WF-RUN-034: the same transaction projects every journey_worker row into the
// bitemporal aggregates a promotion commits against and records the
// promotion catalog (jobs, OPEN vacancies, compensation pools), so a served
// promotion resolves real rows instead of none. The published pay bands and
// career ladder are recorded here too, so the served catalogs read the
// tenant's own rows.
func bootstrapLocalDevWorkforce(ctx context.Context, pool *pgxadapter.Pool, tenant string) (demoworkforce.Summary, demoworkforce.OrganizationSummary, error) {
	if pool == nil || tenant != demoworkforce.CompanyKey {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, nil
	}
	tenantID := pgstore.TenantID(tenant)
	// The job architecture is recorded before the workforce it classifies.
	// internal/data/jobarchstore owns its own transaction (it establishes
	// tenant context itself before touching a row), so it cannot join the one
	// below; doing it first means a committed workforce is never left
	// classified against a graph that was never written.
	if _, err := demoworkforce.SeedJobArchitecture(ctx, jobarchstore.New(pool), tenantID.String()); err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed the demo job architecture: %w", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("begin local development workforce seed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
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
	// The published salary ranges and the published career ladder are the
	// tenant's own rows, not a compiled-in map: the served band catalog
	// (internal/data/bandfacts) and the served promotion-path catalog
	// (internal/data/promotionladder) both read them back. Seeding them in
	// this transaction is what makes the workers, their bands and the ladder
	// they can move along exist together or not at all.
	if _, err := demoworkforce.SeedPayBands(ctx, tx, tenantID, demoSeedRecordedAt()); err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed the demo pay bands: %w", err)
	}
	if _, err := demoworkforce.SeedPromotionLadder(ctx, tx, tenantID, demoSeedRecordedAt()); err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed the demo promotion ladder: %w", err)
	}
	// The catalog is stamped with the same fixed knowledge instant the
	// HarborCare organization seed uses, so every proposal produced after the
	// seed reads it as known. It is derived from the same authored ladder the
	// rows above record, so every published target has a job, OPEN vacancies
	// and a pool to reserve against.
	catalog, err := app.PromotionAggregateCatalog(demoSeedRecordedAt())
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
	// The same transaction records the workforce's performance history and its
	// recent payroll runs. Both are derived from the workers just seeded, so
	// neither can describe a worker this tenant does not have; and the
	// calibrated ratings are what the served promotion's high-performer
	// routing reads through cellConfig.PerformanceRatings.
	if _, err := demoworkforce.SeedPerformance(ctx, tx, tenantID); err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed the local development performance record: %w", err)
	}
	if _, err := demoworkforce.SeedPayroll(ctx, tx, tenantID); err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed the local development payroll history: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("commit local development workforce seed: %w", err)
	}
	return seeded, organization, nil
}

// bootstrapLocalDevWorkflowVersions leaves a local-development cell able to
// run the reference promotion end to end.
//
// Composition publishes the shipped workflows as DRAFT and never approves or
// activates them (WF-COMP-006), which is right: a release is a governed
// operator action, not configuration. The consequence on a freshly created
// development database was that seeding produced a tenant a promotion could be
// proposed for but never started -- the engine refused VERSION_NOT_ACTIVE
// until somebody ran `hcmnext workflow-version bootstrap-dev` by hand.
//
// This closes that gap without weakening the gate. It calls the same
// [platformexecution.BootstrapDevVersions] the subcommand calls, so the
// approval still re-runs every declared conformance fixture, still refuses the
// publisher approving itself, and activation still re-verifies the stored
// report; the approver remains [platformexecution.DevReleaseApprover] under
// the development authority, so a registry shows at a glance that a version
// was only ever bootstrapped for local development. It is idempotent (an
// ACTIVE, QUARANTINED or RETIRED version is left alone) and gated exactly like
// the workforce seed: the dev browser login AND the demo tenant. Any other
// profile, and any cell whose registry is the private in-memory one, is left
// on the governed CLI path untouched.
func bootstrapLocalDevWorkflowVersions(ctx context.Context, cfg ServeConfig, versions workflowversion.Store, now func() time.Time) ([]workflowversion.CompiledVersion, error) {
	if !cfg.DevBrowserLogin || cfg.Tenant != demoworkforce.CompanyKey {
		return nil, nil
	}
	// Only a durable registry has approvals and activations to record. A
	// composition without one already activates its private in-memory registry
	// from its own in-process fixture run, and has nothing to bootstrap.
	registry, durable := versions.(platformexecution.VersionRegistry)
	if !durable {
		return nil, nil
	}
	at := time.Now().UTC()
	if now != nil {
		at = now().UTC()
	}
	released, err := platformexecution.BootstrapDevVersions(ctx, registry, at)
	if err != nil {
		return nil, fmt.Errorf("bootstrap the local development workflow versions: %w", err)
	}
	return released, nil
}

// composeCalibratedRatings binds the production CalibratedRatingLookup
// (REV-096-01). Without it the intent service's high-performer routing takes
// its "no lookup" branch for every subject, so the variant is unreachable in
// any served cell. A composition with no database pool has no performance
// rows to read and binds nothing, which keeps every start on the plan's own
// digest exactly as before.
func composeCalibratedRatings(pool *pgxadapter.Pool) app.CalibratedRatingLookup {
	if pool == nil {
		return nil
	}
	return performancestore.NewCalibratedRatings(pool, tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID))
}

// bootstrapLocalDevRoleAssignments gives every seeded worker the durable
// access-role set migrations/00238 provides for. It is deliberately not part
// of the transaction above: worker_access_role_assignment references
// access_role, and the tenant's role catalog is written later in the
// composition by roleaccessstore.Bootstrap, so this runs after it.
//
// It adds rows and nothing else. No page grant, visibility policy or gating
// rule is touched; each worker is assigned the same bundle the local-dev
// directory already issues them a credential for, and a set somebody has
// already edited is left alone.
func bootstrapLocalDevRoleAssignments(ctx context.Context, pool *pgxadapter.Pool, tenant string) (demoworkforce.RoleAssignmentSummary, error) {
	if pool == nil || tenant != demoworkforce.CompanyKey {
		return demoworkforce.RoleAssignmentSummary{}, nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return demoworkforce.RoleAssignmentSummary{}, fmt.Errorf("begin local development role assignment seed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	summary, err := demoworkforce.SeedRoleAssignments(ctx, tx, pgstore.TenantID(tenant))
	if err != nil {
		return demoworkforce.RoleAssignmentSummary{}, fmt.Errorf("seed the local development role assignments: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return demoworkforce.RoleAssignmentSummary{}, fmt.Errorf("commit local development role assignment seed: %w", err)
	}
	return summary, nil
}
