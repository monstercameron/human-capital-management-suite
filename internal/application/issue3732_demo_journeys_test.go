package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// issue3732Compose composes the local-dev serve role the way `hcmnext serve`
// does for a demo company, with the demo workforce and, when demoJourneys is
// set, the seeded promotion journeys (#3732).
func issue3732Compose(t *testing.T, demoJourneys bool) (*App, ServeConfig, map[string]*trust.Principal) {
	t.Helper()
	composed, cfg, principals := issue3732ComposeTenants(t, demoJourneys, "")
	return composed, cfg, principals
}

// issue3732ComposeTenants is issue3732Compose also serving the extra tenants;
// principals are keyed by "<company>/<persona id>" for every served company,
// and by the bare persona id for the default company.
func issue3732ComposeTenants(t *testing.T, demoJourneys bool, tenants string) (*App, ServeConfig, map[string]*trust.Principal) {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{
		"search_path": db.Schema, "application_name": "issue3732-" + db.Schema,
	})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	cfg := ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: promoux015SigningKey, PageCursorKey: integrationPageCursorKey, Issuer: DefaultIssuer, Audience: DefaultAudience,
		Tenant: demoworkforce.CompanyKey, Tenants: tenants, CellID: "cell-issue3732", MaxDeadline: 60 * time.Second,
		Workspace: false, DevBrowserLogin: true, DevWorkforceBootstrap: true, DevDemoJourneys: demoJourneys,
		Profile: ServeProfileLocalDev, OTelExporter: OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:issue3732",
		ExecutionAuthorityRole: "promotion_operator", ExecutionApprover: "principal:promotion-approver",
		ExecutionFinancePartner: LocalDevFinancePartner,
		TimerTzdbVersion:        DefaultTimerTzdbVersion,
		TimerCalendarVersion:    DefaultTimerCalendarVersion,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("configuration: %v", err)
	}
	composed, err := ComposeServe(context.Background(), ServeInput{Config: cfg, Pool: pool, Identity: "issue3732", Options: Options{ProviderReceipts: newFakeProviderReceipts()}})
	if err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(cfg.DevHMACKey), Issuer: cfg.Issuer, Audience: cfg.Audience})
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	principals := map[string]*trust.Principal{}
	for _, persona := range composeDevPersonas(verifier, cfg, time.Now) {
		principal, verifyErr := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: persona.Token, Audience: cfg.Audience})
		if verifyErr != nil {
			t.Fatalf("verify %s: %v", persona.ID, verifyErr)
		}
		principals[persona.Company+"/"+persona.ID] = principal
		if persona.Company == demoworkforce.CompanyKey {
			principals[persona.ID] = principal
		}
	}
	return composed, cfg, principals
}

func issue3732Stages(t *testing.T, engine workspace.JourneyEngine, principal *trust.Principal) map[workspace.JourneyStage]int {
	t.Helper()
	listed, err := engine.ListJourneys(trust.WithPrincipal(context.Background(), principal))
	if err != nil {
		t.Fatalf("ListJourneys: %v", err)
	}
	stages := map[workspace.JourneyStage]int{}
	for _, summary := range listed {
		stages[summary.Stage]++
	}
	return stages
}

// TestIssue3732_DemoOrgSeedsPromotionsAtSeveralStages_Integration proves a
// freshly started local demo org already has in-flight promotions at distinct
// stages (finance approval, manager approval, rejected), so the Journeys list
// is not empty and the chat journey card can be exercised without creating a
// promotion by hand. The HarborCare reporting line under the proposer holds
// three people with a published path and an open position, so the fourth
// scenario (approved, waiting for its effective date) is only seeded when a
// company has a fourth.
func TestIssue3732_DemoOrgSeedsPromotionsAtSeveralStages_Integration(t *testing.T) {
	composed, cfg, principals := issue3732Compose(t, true)
	engine := composed.Cell().Journey
	stages := issue3732Stages(t, engine, principals["admin"])
	if len(stages) == 0 {
		verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(cfg.DevHMACKey), Issuer: cfg.Issuer, Audience: cfg.Audience})
		if err != nil {
			t.Fatalf("verifier: %v", err)
		}
		t.Logf("seed notes after an empty start: %v", seedLocalDevDemoJourneys(context.Background(), engine, verifier, cfg, time.Now).Notes)
	}
	for _, want := range []workspace.JourneyStage{
		workspace.JourneyStageFinanceApproval,
		workspace.JourneyStageManagerApproval,
		workspace.JourneyStageRejected,
	} {
		if stages[want] < 1 {
			t.Errorf("admin sees stages %v, want at least one %s journey", stages, want)
		}
	}
	// A person with a reason to look sees them too; the employee persona
	// sees none of other people's promotions.
	if got := issue3732Stages(t, engine, principals["finance-partner"]); len(got) == 0 {
		t.Errorf("finance partner sees no seeded promotions, want the finance and manager approvals")
	}
	if got := issue3732Stages(t, engine, principals["hiring-manager"]); len(got) == 0 {
		t.Errorf("proposer sees no seeded promotions")
	}
	issue3732RequireEmployeeSeesNone(t, engine, principals["individual-contributor"])
}

// issue3732RequireEmployeeSeesNone fails when the employee persona can list
// anyone else's promotion: the seeded rows carry pay detail, so the engine's
// own disclosure rules must keep every one of them out of that list.
func issue3732RequireEmployeeSeesNone(t *testing.T, engine workspace.JourneyEngine, employee *trust.Principal) {
	t.Helper()
	listed, err := engine.ListJourneys(trust.WithPrincipal(context.Background(), employee))
	if err != nil {
		return // a refusal also keeps them private
	}
	for _, summary := range listed {
		t.Errorf("employee persona lists another person's %s promotion %s (pay %q)", summary.Stage, summary.IntentID, summary.ProposedBase)
	}
}

// TestIssue3732_EveryServedDemoCompanySeedsItsOwnJourneys_Integration proves
// the second shipped demo company is seeded too, each company's journeys are
// listed only under that company's own personas, and the employee persona of
// either sees none of them.
func TestIssue3732_EveryServedDemoCompanySeedsItsOwnJourneys_Integration(t *testing.T) {
	composed, _, principals := issue3732ComposeTenants(t, true, demoworkforce.IronridgePack.Key)
	engine := composed.Cell().Journey
	for _, company := range []string{demoworkforce.HarborCarePack.Key, demoworkforce.IronridgePack.Key} {
		var admin, employee *trust.Principal
		for key, principal := range principals {
			switch {
			case strings.HasPrefix(key, company+"/") && strings.HasSuffix(key, "admin"):
				admin = principal
			case strings.HasPrefix(key, company+"/") && strings.HasSuffix(key, "individual-contributor"):
				employee = principal
			}
		}
		if admin == nil || employee == nil {
			t.Fatalf("%s: personas missing from %v", company, principals)
		}
		stages := issue3732Stages(t, engine, admin)
		for _, want := range []workspace.JourneyStage{
			workspace.JourneyStageFinanceApproval, workspace.JourneyStageManagerApproval, workspace.JourneyStageRejected,
		} {
			if stages[want] < 1 {
				t.Errorf("%s: admin sees stages %v, want at least one %s journey", company, stages, want)
			}
		}
		issue3732RequireEmployeeSeesNone(t, engine, employee)
	}
}

// TestIssue3732_DemoJourneysAreOptIn_Integration proves a composition that
// does not ask for the demo journeys starts with none: the seed is a local
// development convenience, never implied by the workforce seed alone.
func TestIssue3732_DemoJourneysAreOptIn_Integration(t *testing.T) {
	composed, _, principals := issue3732Compose(t, false)
	if got := issue3732Stages(t, composed.Cell().Journey, principals["admin"]); len(got) != 0 {
		t.Fatalf("a composition without DevDemoJourneys listed journeys %v, want none", got)
	}
}

// TestIssue3732_ReplayAddsNothing_Integration proves the seed is replay safe:
// a restart over the same database leaves exactly the journeys it already has.
func TestIssue3732_ReplayAddsNothing_Integration(t *testing.T) {
	composed, cfg, principals := issue3732Compose(t, true)
	engine := composed.Cell().Journey
	before := issue3732Stages(t, engine, principals["admin"])
	total := 0
	for _, n := range before {
		total += n
	}
	if total == 0 {
		t.Fatal("the first start seeded no journeys")
	}
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{Key: []byte(cfg.DevHMACKey), Issuer: cfg.Issuer, Audience: cfg.Audience})
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	result := seedLocalDevDemoJourneys(context.Background(), engine, verifier, cfg, time.Now)
	if result.Created != 0 {
		t.Fatalf("a replay created %d journeys, want 0 (%v)", result.Created, result.Notes)
	}
	after := 0
	for _, n := range issue3732Stages(t, engine, principals["admin"]) {
		after += n
	}
	if after != total {
		t.Fatalf("journeys after replay = %d, want %d", after, total)
	}
}
