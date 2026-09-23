package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// devVersionServeConfig is a serve configuration with the execution authority
// composed. devBrowserLogin decides whether it is the local-development
// profile the workflow-version bootstrap is gated on.
func devVersionServeConfig(url string, devBrowserLogin bool, tenant string) ServeConfig {
	return ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: url,
		DevHMACKey: "dev-workflow-version-bootstrap-signing-key", Issuer: DefaultIssuer, Audience: DefaultAudience,
		PageCursorKey: testPageCursorKey,
		Tenant:        tenant, CellID: "cell-dev-versions", MaxDeadline: 60 * time.Second,
		Workspace: true, DevBrowserLogin: devBrowserLogin, OTelExporter: OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:dev-workflow-versions",
		ExecutionAuthorityRole: "promotion_operator", ExecutionApprover: "principal:promotion-approver",
		ExecutionFinancePartner: LocalDevFinancePartner,
		WorkflowPlan:            WorkflowPlanExecute, TimerTzdbVersion: DefaultTimerTzdbVersion,
		TimerCalendarVersion: DefaultTimerCalendarVersion,
	}
}

func composeForVersions(t *testing.T, cfg ServeConfig, pool *pgxadapter.Pool) {
	t.Helper()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("configuration: %v", err)
	}
	if _, err := ComposeServe(context.Background(), ServeInput{Config: cfg, Pool: pool, Identity: "dev-versions"}); err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
}

func versionPool(t *testing.T) (*pgxadapter.Pool, string) {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, db.URL
}

// TestLocalDevServeReleasesTheShippedWorkflowVersions proves a
// local-development serve leaves the demo tenant able to run the reference
// promotion end to end.
//
// Before this, seeding produced a tenant a promotion could be proposed for
// but never started: the shipped versions stay DRAFT until a release, so the
// engine refused VERSION_NOT_ACTIVE until somebody ran
// `hcmnext workflow-version bootstrap-dev` by hand. The release is performed,
// not bypassed -- the approver is the development release approver, never the
// publisher -- and it is idempotent across a restart.
func TestLocalDevServeReleasesTheShippedWorkflowVersions(t *testing.T) {
	pool, url := versionPool(t)
	registry := workflowversionstore.Store{DB: pool}
	cfg := devVersionServeConfig(url, true, demoworkforce.CompanyKey)
	composeForVersions(t, cfg, pool)

	for _, workflowID := range []string{promotionexec.WorkflowID, prototype.ApprovalWorkflowID} {
		active, found, err := registry.GetActiveForWorkflow(workflowID)
		if err != nil || !found {
			t.Fatalf("%s has no ACTIVE version after a local-development serve (%v)", workflowID, err)
		}
		if active.Status != workflowversion.StatusActive {
			t.Fatalf("%s active version status = %s", workflowID, active.Status)
		}
		// The release is a real approval by somebody who is not the publisher.
		if len(active.Approvals) == 0 {
			t.Fatalf("%s was activated with no recorded approval", workflowID)
		}
		for _, approval := range active.Approvals {
			if approval.ApprovedBy == active.PublishedBy {
				t.Fatalf("%s was approved by its own publisher %s", workflowID, approval.ApprovedBy)
			}
			if approval.ApprovedBy != platformexecution.DevReleaseApprover {
				t.Fatalf("%s approver = %q, want the development release approver", workflowID, approval.ApprovedBy)
			}
		}
	}
	// The current executable promotion is the one new starts resolve.
	active, _, err := registry.GetActiveForWorkflow(promotionexec.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if active.SemanticVersion != promotionexec.SemanticVersion {
		t.Fatalf("active promotion execute version = %s, want %s", active.SemanticVersion, promotionexec.SemanticVersion)
	}

	// A restart re-releases nothing: an ACTIVE version is left exactly as it
	// stands, approvals and all.
	before := active
	composeForVersions(t, cfg, pool)
	after, found, err := registry.GetActiveForWorkflow(promotionexec.WorkflowID)
	if err != nil || !found {
		t.Fatalf("the active version did not survive a restart (%v)", err)
	}
	if after.CompiledPlanDigest != before.CompiledPlanDigest || len(after.Approvals) != len(before.Approvals) {
		t.Fatalf("a restart changed the release: %d approvals of %s, was %d of %s",
			len(after.Approvals), after.CompiledPlanDigest, len(before.Approvals), before.CompiledPlanDigest)
	}
}

// TestNonDevServeLeavesTheShippedWorkflowVersionsDraft proves the bootstrap
// is impossible outside the local-development profile: without the dev
// browser login, and for any tenant other than the demo one, composition
// still publishes DRAFTs and activates nothing, so the governed CLI release
// remains the only way a version reaches service.
func TestNonDevServeLeavesTheShippedWorkflowVersionsDraft(t *testing.T) {
	for _, profile := range []struct {
		name            string
		devBrowserLogin bool
		tenant          string
	}{
		{"no dev browser login", false, demoworkforce.CompanyKey},
		{"another tenant", true, "acme-production"},
	} {
		t.Run(profile.name, func(t *testing.T) {
			pool, url := versionPool(t)
			registry := workflowversionstore.Store{DB: pool}
			composeForVersions(t, devVersionServeConfig(url, profile.devBrowserLogin, profile.tenant), pool)
			for _, workflowID := range []string{promotionexec.WorkflowID, prototype.ApprovalWorkflowID} {
				if _, found, err := registry.GetActiveForWorkflow(workflowID); err != nil || found {
					t.Fatalf("%s has an ACTIVE version outside local development (found=%t, %v)", workflowID, found, err)
				}
				versions, err := registry.List(workflowID)
				if err != nil || len(versions) == 0 {
					t.Fatalf("%s published nothing (%v)", workflowID, err)
				}
				for _, v := range versions {
					if v.Status != workflowversion.StatusDraft || len(v.Approvals) != 0 {
						t.Fatalf("%s %s is %s with %d approvals, want an unapproved DRAFT",
							workflowID, v.SemanticVersion, v.Status, len(v.Approvals))
					}
				}
			}
		})
	}
}

// TestBootstrapLocalDevWorkflowVersionsIsGatedAndDurableOnly proves the two
// gates directly, including the one a composed serve cannot reach: a cell
// whose registry is the private in-memory one has no durable release to
// perform and is left alone.
func TestBootstrapLocalDevWorkflowVersionsIsGatedAndDurableOnly(t *testing.T) {
	ctx := context.Background()
	at := func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) }
	demo := devVersionServeConfig("postgres://unused", true, demoworkforce.CompanyKey)
	inMemory := workflowversion.NewRegistry()
	if _, err := platformexecution.PublishShippedVersions(inMemory, at()); err != nil {
		t.Fatal(err)
	}

	released, err := bootstrapLocalDevWorkflowVersions(ctx, demo, inMemory, at)
	if err != nil || released != nil {
		t.Fatalf("an in-memory registry was bootstrapped: %+v, %v", released, err)
	}
	notDev := demo
	notDev.DevBrowserLogin = false
	if released, err := bootstrapLocalDevWorkflowVersions(ctx, notDev, nil, at); err != nil || released != nil {
		t.Fatalf("a non-dev profile was bootstrapped: %+v, %v", released, err)
	}
	otherTenant := demo
	otherTenant.Tenant = "acme-production"
	if released, err := bootstrapLocalDevWorkflowVersions(ctx, otherTenant, nil, at); err != nil || released != nil {
		t.Fatalf("a non-demo tenant was bootstrapped: %+v, %v", released, err)
	}
}
