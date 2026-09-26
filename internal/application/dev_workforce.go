package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/brandassetstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/jobarchstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/performancestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/preferencestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/brandasset"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
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
//
// The company is resolved from the tenant key (demoworkforce.PackFor): a
// tenant that is no shipped demo company is left untouched.
func bootstrapLocalDevWorkforce(ctx context.Context, pool *pgxadapter.Pool, tenant string) (demoworkforce.Summary, demoworkforce.OrganizationSummary, error) {
	pack, isDemo := demoworkforce.PackFor(tenant)
	if pool == nil || !isDemo {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, nil
	}
	tenantID := pgstore.TenantID(tenant)
	// The job architecture is recorded before the workforce it classifies.
	// internal/data/jobarchstore owns its own transaction (it establishes
	// tenant context itself before touching a row), so it cannot join the one
	// below; doing it first means a committed workforce is never left
	// classified against a graph that was never written.
	if _, err := pack.SeedJobArchitecture(ctx, jobarchstore.New(pool), tenantID.String()); err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed the demo job architecture: %w", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("begin local development workforce seed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	organization, err := pack.SeedOrganization(ctx, tx, tenantID)
	if err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed local development organization: %w", err)
	}
	seeded, err := pack.Seed(ctx, tx, tenantID)
	if err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed local development workforce: %w", err)
	}
	rows, err := (workforce.Store{}).List(ctx, tx, tenantID)
	if err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("list the local development workforce: %w", err)
	}
	for _, row := range rows {
		if _, err := demoworkforce.ProjectWorker(ctx, tx, row, pack.Company.LegalEntity); err != nil {
			return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("project the local development workforce: %w", err)
		}
	}
	// The published salary ranges and the published career ladder are the
	// tenant's own rows, not a compiled-in map: the served band catalog
	// (internal/data/bandfacts) and the served promotion-path catalog
	// (internal/data/promotionladder) both read them back. Seeding them in
	// this transaction is what makes the workers, their bands and the ladder
	// they can move along exist together or not at all.
	if _, err := pack.SeedPayBands(ctx, tx, tenantID, demoSeedRecordedAt()); err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed the demo pay bands: %w", err)
	}
	if _, err := pack.SeedPromotionLadder(ctx, tx, tenantID, demoSeedRecordedAt()); err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed the demo promotion ladder: %w", err)
	}
	// The catalog is stamped with the same fixed knowledge instant the
	// HarborCare organization seed uses, so every proposal produced after the
	// seed reads it as known. It is derived from the same authored ladder the
	// rows above record, so every published target has a job, OPEN vacancies
	// and a pool to reserve against.
	catalog, err := app.PromotionAggregateCatalogForPack(pack, demoSeedRecordedAt())
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
	if _, err := pack.SeedPerformance(ctx, tx, tenantID); err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed the local development performance record: %w", err)
	}
	if _, err := pack.SeedPayroll(ctx, tx, tenantID); err != nil {
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
	if _, isDemo := demoworkforce.PackFor(cfg.Tenant); !cfg.DevBrowserLogin || !isDemo {
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
	for _, tenant := range cfg.ServedTenants() {
		if tenant != demoworkforce.IronridgeKey {
			continue
		}
		pilot, err := platformexecution.PublishIronridgeWorkOrder(registry, at)
		if err != nil {
			return nil, fmt.Errorf("publish the Ironridge work order workflow: %w", err)
		}
		released = append(released, pilot)
		break
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
	pack, isDemo := demoworkforce.PackFor(tenant)
	if pool == nil || !isDemo {
		return demoworkforce.RoleAssignmentSummary{}, nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return demoworkforce.RoleAssignmentSummary{}, fmt.Errorf("begin local development role assignment seed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	summary, err := pack.SeedRoleAssignments(ctx, tx, pgstore.TenantID(tenant))
	if err != nil {
		return demoworkforce.RoleAssignmentSummary{}, fmt.Errorf("seed the local development role assignments: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return demoworkforce.RoleAssignmentSummary{}, fmt.Errorf("commit local development role assignment seed: %w", err)
	}
	return summary, nil
}

// bootstrapLocalDevBranding gives a demo company that declares its own look
// (demoworkforce.Pack.Theme and Pack.Logo) that look on its tenant: the logo
// becomes the tenant's current brand asset revision, and every organization
// scope the company's development credentials carry is given the theme that
// points at it. Appearance is stored per organization scope, and each seeded
// employee signs in scoped to their own unit, so the theme is written once
// per scope; otherwise switching companies on the sign-in page would switch
// the branding only for the quick picks.
//
// It adds and never overwrites: a scope whose appearance somebody already
// saved, and a tenant whose library already holds this logo, are left alone.
func bootstrapLocalDevBranding(ctx context.Context, pool *pgxadapter.Pool, tenant string) (int, error) {
	pack, isDemo := demoworkforce.PackFor(tenant)
	if pool == nil || !isDemo || pack.Theme == nil {
		return 0, nil
	}
	logoURL := ""
	if pack.Logo != nil {
		url, err := seedPackLogo(ctx, pool, tenant, pack.Logo)
		if err != nil {
			return 0, err
		}
		logoURL = url
	}
	store := preferencestore.New(pool, tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID))
	theme := preferences.Theme{
		BrandName: pack.Theme.BrandName, BrandMark: pack.Theme.BrandMark, BrandLogoURL: logoURL,
		ColorMode: pack.Theme.ColorMode, Palette: pack.Theme.Palette, Shape: pack.Theme.Shape,
		Density: pack.Theme.Density, Glyphs: pack.Theme.Glyphs, Typeface: pack.Theme.Typeface,
		Navigation: pack.Theme.Navigation, Motion: pack.Theme.Motion,
		TokenOverrides: pack.Theme.TokenOverrides, DarkTokenOverrides: pack.Theme.DarkTokenOverrides,
	}
	written := 0
	for _, scope := range packOrganizationScopes(pack) {
		current, err := store.Load(ctx, kernelvalues.TenantId(tenant), scope, "system:demo-branding")
		if err != nil {
			return written, fmt.Errorf("read the %s appearance for %s: %w", pack.DisplayName, scope, err)
		}
		if current.Theme.Version != 0 {
			continue
		}
		if _, err := store.SaveTheme(ctx, kernelvalues.TenantId(tenant), scope, "system:demo-branding", preferences.TenantTheme{Theme: theme}); err != nil {
			return written, fmt.Errorf("seed the %s appearance for %s: %w", pack.DisplayName, scope, err)
		}
		written++
	}
	return written, nil
}

// packOrganizationScopes is every organization scope a demo company's
// development credentials are issued under: the quick picks' scope and one
// per organization unit.
func packOrganizationScopes(pack *demoworkforce.Pack) []string {
	scopes := []string{pack.OrgScope()}
	seen := map[string]bool{pack.OrgScope(): true}
	for _, unit := range pack.Company.Units {
		scope := "org:" + pack.Key + ":" + unit.Code
		if !seen[scope] {
			seen[scope] = true
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

// seedPackLogo stores a pack's logo as the tenant's current brand asset
// revision, unless the current revision already is that logo, and returns
// the URL the theme points at.
func seedPackLogo(ctx context.Context, pool *pgxadapter.Pool, tenant string, logo *demoworkforce.PackLogo) (string, error) {
	assets, err := brandassetstore.New(pool, tenantKeyMapper[kernelvalues.TenantId](pgstore.TenantID))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(logo.PNG)
	digest := hex.EncodeToString(sum[:])
	current, found, err := assets.Current(ctx, tenant)
	if err != nil {
		return "", fmt.Errorf("read the brand asset library: %w", err)
	}
	if found && current.Digest == digest {
		return workspace.PathBrandAssetPrefix + digest, nil
	}
	expected := 0
	if found {
		expected = current.Revision
	}
	if _, err := assets.Save(ctx, tenant, expected, "system:demo-branding", brandasset.Asset{
		Name: logo.Name, MediaType: "image/png", Width: logo.Width, Height: logo.Height,
		Digest: digest, Original: logo.PNG, Proxy: logo.Proxy, ProxyType: "image/jpeg",
	}); err != nil {
		return "", fmt.Errorf("store the brand logo: %w", err)
	}
	return workspace.PathBrandAssetPrefix + digest, nil
}
