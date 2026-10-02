package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agentMatrixCell is a local cell that serves two tenants, over real
// PostgreSQL: the core database, the agent database, one persona store that
// holds the review and evaluation verifiers, the local evaluation runner and
// the runtime provisioner the publish step calls. No model is called: the
// evaluation is the deterministic local one.
type agentMatrixCell struct {
	ctx      context.Context
	core     *pgxadapter.Pool
	owner    *pgxadapter.Pool
	store    *pgstore.Store
	agentDB  *pgtest.DB
	agents   *agentstore.Store
	personas *agentpersonastore.Store
	reviews  agentpersonastore.ReviewEvidenceSource
	issuer   *PersonaReviewIssuanceService
	runtime  *LocalPersonaRuntimeProvisioner
	runner   *localPersonaAdminEvaluationRunner
	mapper   func(values.TenantId) uuid.UUID
	now      time.Time
}

// agentMatrixTenant is one tenant of the cell: its administrator (the agent's
// owner), an independent reviewer, the command executor those two use and the
// Policy Helper profile they take through the lifecycle.
type agentMatrixTenant struct {
	id                        string
	owner, steward, reviewer  string
	adminCtx, reviewerCtx     context.Context
	actor, reviewerActor      PersonaAdminCommandActor
	executor                  *PersonaAdminLifecycleExecutor
	profile                   agentpersona.PersonaProfile
	sealed                    agentpersona.PersonaVersion
	businessOwner, technician string
}

func newAgentMatrixCell(t *testing.T, tenants ...string) *agentMatrixCell {
	t.Helper()
	ctx := context.Background()
	coreDB, agentDB := pgtest.New(t), pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, agentDB.SQL); err != nil {
		t.Fatal(err)
	}
	mapper := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	core, err := pgxadapter.NewPool(ctx, personaChatSchemaDSN(t, coreDB.URL, coreDB.Schema), map[string]string{"role": "hcmnext_app"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.Close)
	owner, err := pgxadapter.NewPool(ctx, personaChatSchemaDSN(t, coreDB.URL, coreDB.Schema), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Close)
	coreStore, err := pgstore.New(owner, pgstore.WithCellID("agent-matrix"))
	if err != nil {
		t.Fatal(err)
	}
	agents := commonAgentOpenIntegrationStore(t, agentDB)
	now := time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)
	clock := func() time.Time { return now }
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	evaluations, err := localAgentDemoEvaluationAuthority(key, localAgentDemoTenant, mapper, clock)
	if err != nil {
		t.Fatal(err)
	}
	reviews, err := agentpersonastore.NewDurableReviewAuthority(mapper)
	if err != nil {
		t.Fatal(err)
	}
	personas, err := agentpersonastore.NewWithPublicationAuthorities(agents, mapper, reviews, evaluations)
	if err != nil {
		t.Fatal(err)
	}
	reviewConn := agentDB.NewConn(t)
	if _, err := reviewConn.Exec(ctx, "SET ROLE hcmnext_persona_review_authority"); err != nil {
		t.Fatal(err)
	}
	writer, err := agentpersonastore.NewDurablePersonaReviewIssuer(reviewConn, mapper)
	if err != nil {
		t.Fatal(err)
	}
	reviewAuthority, err := NewCurrentPersonaReviewGrantAuthority(agents, mapper)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewPersonaReviewIssuanceService(reviewAuthority, writer)
	if err != nil {
		t.Fatal(err)
	}
	agentDSN := personaChatSchemaDSN(t, agentDB.URL, agentDB.Schema)
	signing := localOpenAIMaterialFixture()
	_, policyKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signing.PolicySeed = base64.StdEncoding.EncodeToString(policyKey.Seed())
	runtime := &LocalPersonaRuntimeProvisioner{Config: LocalAgentDemoConfig{Tenant: localAgentDemoTenant, AgentDatabaseURL: agentDSN, RouteAuthorityDatabaseURL: agentDSN}, Tenants: append([]string(nil), tenants...), Core: core, Agents: agents, Personas: personas, Evaluations: evaluations, Signing: signing, DeploymentPath: filepath.Join(t.TempDir(), "deployment.json"), Now: clock}
	runner := &localPersonaAdminEvaluationRunner{store: personas, agents: agents, tenant: localAgentDemoTenant, tenants: append([]string(nil), tenants...), tenantUUID: mapper, privateKey: key, now: clock, newRunID: uuid.NewString}
	return &agentMatrixCell{ctx: ctx, core: core, owner: owner, store: coreStore, agentDB: agentDB, agents: agents, personas: personas, reviews: reviews, issuer: issuer, runtime: runtime, runner: runner, mapper: mapper, now: now}
}

// tenant seeds one demo tenant and returns its people and their executor. The
// persona is not created yet.
func (c *agentMatrixCell) tenant(t *testing.T, id string) *agentMatrixTenant {
	t.Helper()
	if err := c.store.Bootstrap(c.ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bootstrapLocalDevWorkforce(c.ctx, c.owner, id); err != nil {
		t.Fatal(err)
	}
	tenantUUID := c.mapper(values.TenantId(id))
	c.agentDB.Exec(t, "INSERT INTO tenant(tenant_id) VALUES($1)", tenantUUID)
	pack, ok := demoworkforce.PackFor(id)
	if !ok {
		t.Fatalf("%s is not a demo tenant", id)
	}
	owner, steward, err := localDevPolicyHelperOwners(pack, tenantUUID)
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := localDevPolicyHelperReviewer(pack, tenantUUID)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := localAgentDemoPrincipal(c.now, id, owner, pack.OrgScope())
	if err != nil {
		t.Fatal(err)
	}
	reviewerPrincipal, err := localAgentDemoPrincipal(c.now, id, reviewer, pack.OrgScope())
	if err != nil {
		t.Fatal(err)
	}
	c.agentDB.Exec(t, `INSERT INTO persona_review_grant(tenant_id,grant_id,principal_id,permission,granted_at,expires_at) VALUES($1,$2,$3,'persona:review',now()-interval '1 hour',now()+interval '1 day')`, tenantUUID, uuid.NewString(), reviewer)
	starter, _ := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	manifest, err := ensureLocalAgentDemoAssistantManifest(c.ctx, c.agents, tenantUUID, starter)
	if err != nil {
		t.Fatal(err)
	}
	profile := personaStarterProfile(starter, PersonaStarterDraftRequest{PersonaID: localAgentDemoPersonaID, AvatarRef: "avatar", OrganizationScopes: []string{pack.OrgScope()}, BusinessOwnerID: owner, TechnicalStewardID: steward}, manifest, personaStarterInstructions(starter))
	profile.DataClassesRead = []string{"PUBLIC", "INTERNAL"}
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time { return c.now }
	drafts := &PersonaAdminDraftService{Store: personaAdminDraftStoreAdapter{store: c.personas}, Authorizer: &personaCreateAuthorizerFake{allowedTenant: principal.Tenant()}, Profiles: agentDemoProfileBuilder{manifest: manifest}, Clock: personaAdminDraftClock{now: clock}}
	executor := NewPersonaAdminLifecycleExecutor(personaAdminLifecycleStoreAdapter{store: c.personas}, personaAdminLifecycleAuthorizerFake{}, drafts, nil, c.issuer, personaAdminStoredEvidence{store: c.personas}, agentMatrixPlacement(), nil, clock, uuid.NewString, c.runtime)
	executor.Evaluations = c.runner
	return &agentMatrixTenant{
		id: id, owner: owner, steward: steward, reviewer: reviewer,
		adminCtx: trust.WithPrincipal(c.ctx, principal), reviewerCtx: trust.WithPrincipal(c.ctx, reviewerPrincipal),
		actor:         PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: owner},
		reviewerActor: PersonaAdminCommandActor{Principal: reviewerPrincipal, Tenant: principal.Tenant(), Subject: reviewer},
		executor:      executor, profile: profile, sealed: sealed, businessOwner: owner, technician: steward,
	}
}

// agentMatrixPlacement admits a placement in a public channel with the
// read-only policy the demo conversations carry.
func agentMatrixPlacement() agentux037Placement {
	return agentux037Placement{class: agentpersonastore.ConversationPublic, policy: agentpersonastore.ChannelPolicy{
		MaxTier: "T0", AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPublic},
	}}
}

func (tenant *agentMatrixTenant) run(t *testing.T, ctx context.Context, actor PersonaAdminCommandActor, command PersonaAdminCommand) error {
	t.Helper()
	command.PersonaID = tenant.profile.PersonaID
	return tenant.executor.ExecutePersonaAdminCommand(ctx, actor, command)
}

// card reloads Agent setup's catalog the way a page load does: a new catalog
// client over the given persona store, read as the tenant's administrator.
func (tenant *agentMatrixTenant) card(t *testing.T, store *agentpersonastore.Store, runtime PersonaCatalogRuntimeStatusReader, conversations ...string) productui.PersonaAdminPersona {
	t.Helper()
	targets := catalogTargets{users: []productui.PersonaAdminTarget{{ID: tenant.owner, Label: "Owner"}}}
	for _, conversation := range conversations {
		targets.conversations = append(targets.conversations, productui.PersonaAdminTarget{ID: conversation, Label: conversation, Kind: "CHANNEL"})
	}
	client, err := NewPersonaAdminCatalogClient(PersonaAdminCatalogComposition{Store: personaAdminCatalogStoreAdapter{store: store}, Authorizer: &catalogAuth{}, Targets: targets})
	if err != nil {
		t.Fatal(err)
	}
	client.(readOnlyPersonaAdminCatalog).service.Runtime = runtime
	snapshot, err := client.Snapshot(tenant.adminCtx, productui.PersonaAdminSnapshotRequest{TenantID: tenant.id, Principal: tenant.owner})
	if err != nil {
		t.Fatalf("reload the catalog: %v", err)
	}
	for _, persona := range snapshot.Personas {
		if persona.ID == tenant.profile.PersonaID {
			return persona
		}
	}
	t.Fatalf("the catalog does not list %s: %+v", tenant.profile.PersonaID, snapshot.Personas)
	return productui.PersonaAdminPersona{}
}

// reviewed takes the tenant's Policy Helper to "reviewed, not yet evaluated".
func (tenant *agentMatrixTenant) reviewed(t *testing.T) {
	t.Helper()
	if err := tenant.run(t, tenant.adminCtx, tenant.actor, PersonaAdminCommand{Action: PersonaAdminCreateDraft, Version: tenant.sealed, BusinessOwnerID: tenant.businessOwner, TechnicalStewardID: tenant.technician}); err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if err := tenant.run(t, tenant.adminCtx, tenant.actor, PersonaAdminCommand{Action: PersonaAdminRequestReview}); err != nil {
		t.Fatalf("request review: %v", err)
	}
	if err := tenant.run(t, tenant.reviewerCtx, tenant.reviewerActor, PersonaAdminCommand{Action: PersonaAdminReview, Decision: "APPROVE"}); err != nil {
		t.Fatalf("review: %v", err)
	}
}

// TestTodo_AGENTUX_045 covers the two causes of the defect without a database:
// who is offered the evaluation on a cell that serves several tenants, and
// what the page is told when the server has none for the caller's tenant.
func TestTodo_AGENTUX_045(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	principalFor := func(tenant string) (context.Context, PersonaAdminCommandActor) {
		principal, err := localAgentDemoPrincipal(now, tenant, "admin", "org")
		if err != nil {
			t.Fatal(err)
		}
		return trust.WithPrincipal(context.Background(), principal), PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: "admin"}
	}
	// The process's default tenant is not the demo tenant; the demo tenant is
	// one of the two it serves. That is the cell the defect was found on.
	runner := &localPersonaAdminEvaluationRunner{tenant: localAgentDemoTenant, tenants: []string{"harborcare-demo", localAgentDemoTenant}}
	offered := map[string]bool{localAgentDemoTenant: true, "harborcare-demo": true, "northwind": false}
	for tenant, want := range offered {
		ctx, actor := principalFor(tenant)
		executor := &PersonaAdminLifecycleExecutor{Evaluations: runner}
		if got := executor.PersonaAdminCommandAvailable(ctx, PersonaAdminRunEvaluation); got != want {
			t.Errorf("%s administrator offered evaluation = %v, want %v", tenant, got, want)
		}
		// What Agent setup receives: the command in the allowed list, or the
		// statement that this server has no evaluation for the tenant.
		client := personaAdminCommandClient{catalog: agentMatrixEmptyCatalog{}, ctx: ctx, actor: actor, authorizer: personaAdminLifecycleAuthorizerFake{}, executor: executor}
		snapshot, err := client.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{})
		if err != nil {
			t.Fatal(err)
		}
		allowed := false
		for _, command := range snapshot.AllowedCommands {
			allowed = allowed || command == string(PersonaAdminRunEvaluation)
		}
		if allowed != want || snapshot.EvaluationRuntimeUnavailable == want {
			t.Errorf("%s snapshot: RUN_EVALUATION allowed=%v, runtime unavailable=%v", tenant, allowed, snapshot.EvaluationRuntimeUnavailable)
		}
	}
	// A single-tenant cell keeps the old rule: only its own tenant.
	single := &localPersonaAdminEvaluationRunner{tenant: localAgentDemoTenant}
	for tenant, want := range map[string]bool{localAgentDemoTenant: true, "harborcare-demo": false} {
		ctx, _ := principalFor(tenant)
		if got := single.PersonaEvaluationAvailable(ctx); got != want {
			t.Errorf("single-tenant cell offered %s evaluation = %v, want %v", tenant, got, want)
		}
	}
	// The local evaluator exists only in the local profile, for the demo tenant.
	wiring := &personaServeWiring{}
	for name, attempt := range map[string]struct{ profile, tenant string }{
		"standard profile": {ServeProfileStandard, localAgentDemoTenant},
		"another tenant":   {ServeProfileLocalDev, "harborcare-demo"},
	} {
		if _, err := wiring.prepareLocalAdminEvaluation(&agentstore.Store{}, attempt.profile, attempt.tenant, func() time.Time { return now }, "harborcare-demo", localAgentDemoTenant); !errors.Is(err, ErrPersonaAdminEvaluationUnavailable) {
			t.Errorf("%s was given the local evaluator: %v", name, err)
		}
		if wiring.store != nil {
			t.Errorf("%s replaced the persona store", name)
		}
	}
}

type agentMatrixEmptyCatalog struct{ productui.PersonaAdminClient }

func (agentMatrixEmptyCatalog) Snapshot(context.Context, productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminSnapshot, error) {
	return productui.PersonaAdminSnapshot{Available: true}, nil
}

// TestTodo_AGENTUX_045_Integration composes a two-tenant local cell and takes
// one version through review, evaluation and publication, reloading the
// catalog after every step. The step must still hold after the reload.
func TestTodo_AGENTUX_045_Integration(t *testing.T) {
	cell := newAgentMatrixCell(t, "harborcare-demo", localAgentDemoTenant)
	demo := cell.tenant(t, localAgentDemoTenant)
	reload := func(t *testing.T) productui.PersonaAdminPersona { return demo.card(t, cell.personas, nil) }

	if err := demo.run(t, demo.adminCtx, demo.actor, PersonaAdminCommand{Action: PersonaAdminCreateDraft, Version: demo.sealed, BusinessOwnerID: demo.businessOwner, TechnicalStewardID: demo.technician}); err != nil {
		t.Fatal(err)
	}
	if card := reload(t); card.Lifecycle != productui.PersonaDraft || card.ReviewApproved || card.EvaluationRef != "" {
		t.Fatalf("after create, reloaded card = %s approved=%v evaluation=%q", card.Lifecycle, card.ReviewApproved, card.EvaluationRef)
	}
	if err := demo.run(t, demo.adminCtx, demo.actor, PersonaAdminCommand{Action: PersonaAdminRequestReview}); err != nil {
		t.Fatal(err)
	}
	if card := reload(t); card.Lifecycle != productui.PersonaInReview || card.ReviewApproved {
		t.Fatalf("after the review request, reloaded card = %s approved=%v", card.Lifecycle, card.ReviewApproved)
	}
	// The owner cannot approve their own version; the reviewer can.
	if err := demo.run(t, demo.adminCtx, demo.actor, PersonaAdminCommand{Action: PersonaAdminReview, Decision: "APPROVE"}); err == nil {
		t.Fatal("the owner approved their own version")
	}
	if err := demo.run(t, demo.reviewerCtx, demo.reviewerActor, PersonaAdminCommand{Action: PersonaAdminReview, Decision: "APPROVE"}); err != nil {
		t.Fatal(err)
	}
	if card := reload(t); card.Lifecycle != productui.PersonaInReview || !card.ReviewApproved || card.Reviewer != demo.reviewer || card.EvaluationRef != "" {
		t.Fatalf("after the review, reloaded card = %s approved=%v reviewer=%q evaluation=%q", card.Lifecycle, card.ReviewApproved, card.Reviewer, card.EvaluationRef)
	}
	// Publishing before the evaluation is refused and changes nothing.
	if err := demo.run(t, demo.adminCtx, demo.actor, PersonaAdminCommand{Action: PersonaAdminPublish}); err == nil {
		t.Fatal("a version with no evaluation was published")
	}
	result, err := demo.executor.ExecutePersonaAdminCommandWithResult(demo.adminCtx, demo.actor, PersonaAdminCommand{Action: PersonaAdminRunEvaluation, PersonaID: demo.profile.PersonaID})
	if err != nil || result.Status != "PASSED" || result.Failed != 0 || result.Passed == 0 {
		t.Fatalf("evaluation = %+v err=%v", result, err)
	}
	evaluated := reload(t)
	if evaluated.Lifecycle != productui.PersonaInReview || !evaluated.ReviewApproved || evaluated.EvaluationRef == "" {
		t.Fatalf("after the evaluation, reloaded card = %s approved=%v evaluation=%q; the page would offer the evaluation again", evaluated.Lifecycle, evaluated.ReviewApproved, evaluated.EvaluationRef)
	}
	// Cause two of the defect: a catalog still holding a persona store composed
	// before the evaluator cannot verify the local seal and reads the evidence
	// back as absent. One store, composed once with every verifier, is what the
	// reload above reads.
	stale, err := agentpersonastore.NewWithReviewAuthority(cell.agents, cell.mapper, cell.reviews)
	if err != nil {
		t.Fatal(err)
	}
	if card := demo.card(t, stale, nil); card.EvaluationRef != "" {
		t.Fatalf("a store without the evaluation verifier read the evidence as verified: %q", card.EvaluationRef)
	}
	if err := demo.run(t, demo.adminCtx, demo.actor, PersonaAdminCommand{Action: PersonaAdminPublish}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	published := reload(t)
	if published.Lifecycle != productui.PersonaPublished || !published.ReviewApproved || published.EvaluationRef != evaluated.EvaluationRef || published.ReviewApprovedAt == "" || published.EvaluationPassedAt == "" {
		t.Fatalf("after publication, reloaded card = %s approved=%v evaluation=%q (was %q) reviewed at %q evaluated at %q", published.Lifecycle, published.ReviewApproved, published.EvaluationRef, evaluated.EvaluationRef, published.ReviewApprovedAt, published.EvaluationPassedAt)
	}
	// The other tenant of the same cell has its own, untouched catalog.
	other := cell.tenant(t, "harborcare-demo")
	snapshot, err := func() (productui.PersonaAdminSnapshot, error) {
		client, err := NewPersonaAdminCatalogClient(PersonaAdminCatalogComposition{Store: personaAdminCatalogStoreAdapter{store: cell.personas}, Authorizer: &catalogAuth{}})
		if err != nil {
			return productui.PersonaAdminSnapshot{}, err
		}
		return client.Snapshot(other.adminCtx, productui.PersonaAdminSnapshotRequest{TenantID: other.id, Principal: other.owner})
	}()
	if err != nil || len(snapshot.Personas) != 0 {
		t.Fatalf("the other tenant's catalog = %+v err=%v; want no agents", snapshot.Personas, err)
	}
}

// TestTodo_AGENTUX_045_Security: on a cell whose evaluator serves one tenant,
// the other tenant's administrator is not offered the evaluation, and sending
// the command anyway is refused and records nothing.
func TestTodo_AGENTUX_045_Security(t *testing.T) {
	cell := newAgentMatrixCell(t, "harborcare-demo", localAgentDemoTenant)
	// The evaluator of this cell serves the demo tenant only.
	cell.runner.tenants = []string{localAgentDemoTenant}
	other := cell.tenant(t, "harborcare-demo")
	other.reviewed(t)

	if other.executor.PersonaAdminCommandAvailable(other.adminCtx, PersonaAdminRunEvaluation) {
		t.Fatal("the other tenant's administrator is offered the evaluation")
	}
	_, err := other.executor.ExecutePersonaAdminCommandWithResult(other.adminCtx, other.actor, PersonaAdminCommand{Action: PersonaAdminRunEvaluation, PersonaID: other.profile.PersonaID})
	if !errors.Is(err, ErrPersonaAdminEvaluationUnavailable) {
		t.Fatalf("the other tenant's evaluation command = %v, want a refusal", err)
	}
	var coded *PersonaAdminCommandError
	if !errors.As(classifyPersonaAdminCommandError(err), &coded) || coded.PersonaAdminCommandCode() != personaAdminCodeEvaluationUnavailable {
		t.Fatalf("the refusal reaches the page as %v", classifyPersonaAdminCommandError(err))
	}
	var evidence int
	if err := cell.agentDB.QueryRow(cell.ctx, `SELECT count(*) FROM persona_evaluation_evidence WHERE tenant_id=$1`, cell.mapper(values.TenantId(other.id))).Scan(&evidence); err != nil || evidence != 0 {
		t.Fatalf("a refused evaluation recorded %d evidence rows (err=%v)", evidence, err)
	}
	if card := other.card(t, cell.personas, nil); card.EvaluationRef != "" || card.Lifecycle != productui.PersonaInReview {
		t.Fatalf("the refused version = %s evaluation=%q", card.Lifecycle, card.EvaluationRef)
	}
	if err := other.run(t, other.adminCtx, other.actor, PersonaAdminCommand{Action: PersonaAdminPublish}); err == nil {
		t.Fatal("an unevaluated version was published")
	}
	// Naming the demo tenant in the command while signed in to the other one
	// is refused before any evaluation runs.
	forged := other.actor
	forged.Tenant = values.TenantId(localAgentDemoTenant)
	if _, err := other.executor.ExecutePersonaAdminCommandWithResult(other.adminCtx, forged, PersonaAdminCommand{Action: PersonaAdminRunEvaluation, PersonaID: other.profile.PersonaID}); !errors.Is(err, ErrPersonaAdminCommandUnavailable) {
		t.Fatalf("an actor naming another tenant = %v, want a refusal", err)
	}
	if err := cell.agentDB.QueryRow(cell.ctx, `SELECT count(*) FROM persona_evaluation_evidence`).Scan(&evidence); err != nil || evidence != 0 {
		t.Fatalf("evidence rows after the forged command = %d (err=%v)", evidence, err)
	}
}

// TestTodo_AGENTUX_047 covers what publishing owes the page without a
// database: a version that cannot be made runnable is refused with its own
// code, and a stopped placement reaches the page as a reason it has words for.
func TestTodo_AGENTUX_047(t *testing.T) {
	// The refusal has its own code, so the page shows the sentence about
	// running and not the general failure sentence.
	var coded *PersonaAdminCommandError
	if !errors.As(classifyPersonaAdminCommandError(ErrPersonaAdminRuntimeUnavailable), &coded) || coded.PersonaAdminCommandCode() != "runtime_unavailable" {
		t.Fatalf("a publish refused for want of a runtime is classified %v", coded)
	}
	english := productui.ResolveProductLocale("en-US")
	if sentence := productui.PersonaAdminCommandStatusText(english, coded.PersonaAdminCommandCode()); sentence != "This version cannot be published yet: it is not set up to run in this workspace. Nothing was changed. Ask the person who manages this installation." {
		t.Fatalf("the page sentence for the refusal = %q", sentence)
	}
	// An executor with no provisioning step does not offer Publish at all.
	if (&PersonaAdminLifecycleExecutor{}).PersonaAdminCommandAvailable(context.Background(), PersonaAdminPublish) {
		t.Fatal("Publish is offered with no provisioning step composed")
	}

	// Every stored suspension reason becomes one of the page's reasons; free
	// text never leaves the server.
	for stored, want := range map[string]string{
		"AGENT_PRINCIPAL_MISSING_AFTER_RESTORE":      productui.PersonaPlacementStoppedNoIdentity,
		"AGENT_PRINCIPAL_RETIRED_AFTER_RESTORE":      productui.PersonaPlacementStoppedIdentityInactive,
		"PERSONA_PUBLICATION_MISSING_AFTER_RESTORE":  productui.PersonaPlacementStoppedNotPublished,
		agentpersonastore.MissingVersionAfterRestore: productui.PersonaPlacementStoppedVersionMissing,
		"paused by ir-001 while the vendor is down":  productui.PersonaPlacementStoppedOther,
		"": productui.PersonaPlacementStoppedOther,
	} {
		if got := personaCatalogStoppedReason(stored); got != want {
			t.Errorf("stored reason %q reaches the page as %q, want %q", stored, got, want)
		}
	}

	// The catalog lists a stopped placement with its reason and whether it can
	// start again, lists a running one as running, and lists one conversation
	// once even when a stopped duplicate of it is stored.
	service, ctx := agentUXSetup3CatalogService(t)
	running := service.Installations.(catalogInstalls)[1]
	stopped := service.Installations.(catalogInstalls)[0]
	stopped.Active, stopped.State, stopped.SuspensionReason = false, string(agentpersonastore.InstallationSuspended), "AGENT_PRINCIPAL_MISSING_AFTER_RESTORE"
	duplicate := running
	duplicate.ID, duplicate.Active, duplicate.State, duplicate.SuspensionReason = "install-direct-old", false, string(agentpersonastore.InstallationSuspended), "AGENT_PRINCIPAL_RETIRED_AFTER_RESTORE"
	running.State = string(agentpersonastore.InstallationActive)
	service.Installations = agentMatrixInstallStates{duplicate, running, stopped}
	for _, ready := range []bool{false, true} {
		service.Runtime = agentMatrixRuntimeReady(ready)
		snapshot, err := service.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
		if err != nil || len(snapshot.Personas) != 1 || len(snapshot.Personas[0].Installations) != 2 {
			t.Fatalf("snapshot = %+v err=%v; want one agent with one row per conversation", snapshot.Personas, err)
		}
		for _, row := range snapshot.Personas[0].Installations {
			switch row.ConversationID {
			case "general":
				if !row.Stopped || row.StoppedReason != productui.PersonaPlacementStoppedNoIdentity || row.StartAgain != ready {
					t.Errorf("stopped placement (cause cured=%v) = %+v", ready, row)
				}
			case "direct-policy":
				if row.InstallationID != "install-direct" || row.Stopped || row.StoppedReason != "" || row.StartAgain {
					t.Errorf("running placement with a stopped duplicate = %+v", row)
				}
			default:
				t.Errorf("unexpected placement %+v", row)
			}
		}
	}
}

// agentMatrixInstallStates is a placement reader that, like the real one,
// reports stopped placements as well as running ones.
type agentMatrixInstallStates []PersonaCatalogInstallation

func (r agentMatrixInstallStates) ListPersonaCatalogInstallations(context.Context, values.TenantId) ([]PersonaCatalogInstallation, error) {
	var active []PersonaCatalogInstallation
	for _, row := range r {
		if row.Active {
			active = append(active, row)
		}
	}
	return active, nil
}

func (r agentMatrixInstallStates) ListPersonaCatalogInstallationStates(context.Context, values.TenantId) ([]PersonaCatalogInstallation, error) {
	return r, nil
}

type agentMatrixRuntimeReady bool

func (r agentMatrixRuntimeReady) PersonaRuntimeReady(context.Context, values.TenantId, string, int64) bool {
	return bool(r)
}

// TestTodo_AGENTUX_047_Integration publishes through the executor on the
// two-tenant cell, restarts the reconciler and finds nothing stopped. It then
// stops the placement the way the reconciler does and follows it across the
// page: listed as stopped with its reason, startable once the cause is cured,
// started again through the command, and listed once throughout. The answered
// mention on this same composition is TestAgentUXGeneral_PublishRunnable_Integration.
func TestTodo_AGENTUX_047_Integration(t *testing.T) {
	cell := newAgentMatrixCell(t, localAgentDemoTenant, "harborcare-demo")
	demo := cell.tenant(t, localAgentDemoTenant)
	demo.reviewed(t)
	if err := demo.run(t, demo.adminCtx, demo.actor, PersonaAdminCommand{Action: PersonaAdminRunEvaluation}); err != nil {
		t.Fatalf("evaluation: %v", err)
	}
	if err := demo.run(t, demo.adminCtx, demo.actor, PersonaAdminCommand{Action: PersonaAdminPublish}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	tenant := values.TenantId(demo.id)
	scoped, err := cell.personas.Scoped(tenant)
	if err != nil {
		t.Fatal(err)
	}
	// Publication left a version that can run: an identity bound to exactly
	// this version, which the run path resolves.
	clock := func() time.Time { return cell.now }
	resolver := PersonaRunAgentPrincipalResolver{Bindings: AgentPersonaRunPrincipalBindingReader{Store: cell.personas}, Principals: GovernancePersonaPrincipalAuthority{DB: cell.core, TenantUUID: cell.mapper}, Now: clock}
	binding, err := scoped.ResolvePersonaAgentPrincipal(cell.ctx, demo.profile.PersonaID, 1)
	if err != nil {
		t.Fatalf("the published version has no runtime identity: %v", err)
	}
	if resolved, err := resolver.Resolve(cell.ctx, tenant, demo.profile.PersonaID, 1); err != nil || resolved != binding.PrincipalID.String() {
		t.Fatalf("run-path identity = %q err=%v, want %s", resolved, err, binding.PrincipalID)
	}
	runtime := PersonaCatalogRuntimeStatus{Store: cell.personas, Principal: resolver}

	place := PersonaAdminCommand{Action: PersonaAdminInstall, Installation: agentpersonastore.PersonaInstallation{PersonaID: demo.profile.PersonaID, ConversationID: "general"}}
	if err := demo.run(t, demo.adminCtx, demo.actor, place); err != nil {
		t.Fatalf("add to a conversation: %v", err)
	}
	// A server start runs the reconciler. Twice, as two starts would.
	for start := 1; start <= 2; start++ {
		if changed, err := ReconcileRestoredAgentInstallations(cell.ctx, cell.agents, cell.personas, cell.core, tenant, cell.mapper(tenant), cell.now); err != nil || changed != 0 {
			t.Fatalf("start %d: the reconciler stopped %d placements (err=%v)", start, changed, err)
		}
	}
	card := demo.card(t, cell.personas, runtime, "general")
	if len(card.Installations) != 1 || card.Installations[0].Stopped || card.Installations[0].Version != "1" {
		t.Fatalf("after two starts the placement = %+v; want one running placement", card.Installations)
	}
	first := card.Installations[0].InstallationID
	if _, installation, err := scoped.ReadCurrentPersonaAuthority(cell.ctx, "general", demo.profile.PersonaID); err != nil || installation.InstallationID != first {
		t.Fatalf("run authority in the conversation = %+v err=%v", installation, err)
	}

	// The reconciler's suspension, as it writes it. The identity itself is
	// intact, which is the "cause cured" state.
	cell.agentDB.Exec(t, `UPDATE persona_installations SET state='SUSPENDED',suspension_reason='AGENT_PRINCIPAL_MISSING_AFTER_RESTORE',revision=revision+1,revocation_epoch=revocation_epoch+1,updated_at=now() WHERE tenant_id=$1 AND installation_id=$2`, cell.mapper(tenant), first)
	if _, _, err := scoped.ReadCurrentPersonaAuthority(cell.ctx, "general", demo.profile.PersonaID); err == nil {
		t.Fatal("a stopped placement still has run authority")
	}
	// Without a runtime check the page cannot promise a restart.
	card = demo.card(t, cell.personas, nil, "general")
	if len(card.Installations) != 1 || !card.Installations[0].Stopped || card.Installations[0].StoppedReason != productui.PersonaPlacementStoppedNoIdentity || card.Installations[0].StartAgain {
		t.Fatalf("stopped placement with no runtime check = %+v", card.Installations)
	}
	card = demo.card(t, cell.personas, runtime, "general")
	if len(card.Installations) != 1 || card.Installations[0].InstallationID != first || !card.Installations[0].Stopped || !card.Installations[0].StartAgain {
		t.Fatalf("stopped placement whose cause is cured = %+v; want Start again", card.Installations)
	}
	// "Start again" is the REINSTALL command.
	again := place
	again.Action = PersonaAdminReinstall
	if err := demo.run(t, demo.adminCtx, demo.actor, again); err != nil {
		t.Fatalf("start again: %v", err)
	}
	card = demo.card(t, cell.personas, runtime, "general")
	if len(card.Installations) != 1 || card.Installations[0].Stopped || card.Installations[0].InstallationID == first {
		t.Fatalf("after Start again the conversation lists %+v; want one new running placement", card.Installations)
	}
	var active, suspended int
	if err := cell.agentDB.QueryRow(cell.ctx, `SELECT count(*) FILTER (WHERE state='ACTIVE'), count(*) FILTER (WHERE state='SUSPENDED') FROM persona_installations WHERE tenant_id=$1 AND conversation_id='general'`, cell.mapper(tenant)).Scan(&active, &suspended); err != nil || active != 1 || suspended != 0 {
		t.Fatalf("stored placements in the conversation: %d active, %d stopped (err=%v); want 1 and 0", active, suspended, err)
	}
	if changed, err := ReconcileRestoredAgentInstallations(cell.ctx, cell.agents, cell.personas, cell.core, tenant, cell.mapper(tenant), cell.now); err != nil || changed != 0 {
		t.Fatalf("the next start stopped %d placements (err=%v)", changed, err)
	}
	if _, installation, err := scoped.ReadCurrentPersonaAuthority(cell.ctx, "general", demo.profile.PersonaID); err != nil || installation.InstallationID != card.Installations[0].InstallationID {
		t.Fatalf("run authority after Start again = %+v err=%v", installation, err)
	}
}

// TestTodo_AGENTUX_047_Security: the provisioning step publish depends on
// cannot bind an identity that belongs to another tenant, and never gives two
// versions, or two tenants, the same identity.
func TestTodo_AGENTUX_047_Security(t *testing.T) {
	cell := newAgentMatrixCell(t, localAgentDemoTenant, "harborcare-demo")
	identities := map[string]uuid.UUID{}
	var demo *agentMatrixTenant
	for _, id := range []string{localAgentDemoTenant, "harborcare-demo"} {
		tenant := cell.tenant(t, id)
		tenant.reviewed(t)
		if err := tenant.run(t, tenant.adminCtx, tenant.actor, PersonaAdminCommand{Action: PersonaAdminRunEvaluation}); err != nil {
			t.Fatalf("%s evaluation: %v", id, err)
		}
		if err := tenant.run(t, tenant.adminCtx, tenant.actor, PersonaAdminCommand{Action: PersonaAdminPublish}); err != nil {
			t.Fatalf("%s publish: %v", id, err)
		}
		scoped, err := cell.personas.Scoped(values.TenantId(id))
		if err != nil {
			t.Fatal(err)
		}
		binding, err := scoped.ResolvePersonaAgentPrincipal(cell.ctx, tenant.profile.PersonaID, 1)
		if err != nil {
			t.Fatalf("%s: published version has no identity: %v", id, err)
		}
		identities[id] = binding.PrincipalID
		if id == localAgentDemoTenant {
			demo = tenant
		}
	}
	// The same agent, the same version number, two tenants: two identities.
	if identities[localAgentDemoTenant] == identities["harborcare-demo"] || identities[localAgentDemoTenant] == uuid.Nil {
		t.Fatalf("the two tenants' agents share an identity: %v", identities)
	}
	tenant := values.TenantId(demo.id)
	scoped, _ := cell.personas.Scoped(tenant)
	row, err := scoped.GetVersion(cell.ctx, demo.profile.PersonaID, 1)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := scoped.ResolvePublicationEvidence(cell.ctx, demo.profile.PersonaID, 1)
	if err != nil {
		t.Fatal(err)
	}
	// An actor naming the other tenant cannot provision this tenant's version.
	forged := demo.actor
	forged.Tenant = "harborcare-demo"
	if err := cell.runtime.ProvisionPersonaRuntime(demo.adminCtx, forged, row, demo.profile, evidence); !errors.Is(err, ErrPersonaAdminRuntimeUnavailable) {
		t.Fatalf("provisioning for an actor of another tenant = %v, want a refusal", err)
	}
	// Evidence that does not belong to this version provisions nothing.
	wrong := evidence
	wrong.EvaluationRunID = "evaluation-of-another-version"
	if err := cell.runtime.ProvisionPersonaRuntime(demo.adminCtx, demo.actor, row, demo.profile, wrong); !errors.Is(err, ErrPersonaAdminRuntimeUnavailable) {
		t.Fatalf("provisioning with another version's evidence = %v, want a refusal", err)
	}
	// The other tenant's identity cannot be bound to this tenant's next version,
	// and this version's own identity cannot be replaced.
	other, _ := cell.personas.Scoped("harborcare-demo")
	if err := scoped.RegisterPersonaAgentPrincipal(cell.ctx, demo.profile.PersonaID, 1, identities["harborcare-demo"], cell.now); err == nil {
		if binding, _ := scoped.ResolvePersonaAgentPrincipal(cell.ctx, demo.profile.PersonaID, 1); binding.PrincipalID != identities[localAgentDemoTenant] {
			t.Fatalf("the published version's identity was replaced by another tenant's: %s", binding.PrincipalID)
		}
	}
	if binding, err := other.ResolvePersonaAgentPrincipal(cell.ctx, demo.profile.PersonaID, 1); err != nil || binding.PrincipalID != identities["harborcare-demo"] {
		t.Fatalf("the other tenant's identity changed: %+v err=%v", binding, err)
	}
	// A second version gets its own identity, never the first one's.
	next := demo.profile
	next.Version = 2
	sealed, err := agentpersona.Seal(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := demo.run(t, demo.adminCtx, demo.actor, PersonaAdminCommand{Action: PersonaAdminCreateVersion, Version: sealed, BusinessOwnerID: demo.businessOwner, TechnicalStewardID: demo.technician}); err != nil {
		t.Fatalf("create version 2: %v", err)
	}
	if err := demo.run(t, demo.adminCtx, demo.actor, PersonaAdminCommand{Action: PersonaAdminRequestReview}); err != nil {
		t.Fatal(err)
	}
	if err := demo.run(t, demo.reviewerCtx, demo.reviewerActor, PersonaAdminCommand{Action: PersonaAdminReview, Decision: "APPROVE"}); err != nil {
		t.Fatal(err)
	}
	if err := demo.run(t, demo.adminCtx, demo.actor, PersonaAdminCommand{Action: PersonaAdminRunEvaluation}); err != nil {
		t.Fatal(err)
	}
	// Version 2 is reviewed and evaluated but not published: it has no identity
	// yet, and version 1's is not offered in its place.
	if binding, err := scoped.ResolvePersonaAgentPrincipal(cell.ctx, demo.profile.PersonaID, 2); err == nil {
		t.Fatalf("an unpublished version already has an identity: %+v", binding)
	}
	if err := demo.run(t, demo.adminCtx, demo.actor, PersonaAdminCommand{Action: PersonaAdminPublish}); err != nil {
		t.Fatalf("publish version 2: %v", err)
	}
	second, err := scoped.ResolvePersonaAgentPrincipal(cell.ctx, demo.profile.PersonaID, 2)
	if err != nil || second.PrincipalID == identities[localAgentDemoTenant] || second.PrincipalID == identities["harborcare-demo"] || second.PrincipalID == uuid.Nil {
		t.Fatalf("version 2 identity = %+v err=%v; want one of its own", second, err)
	}
	if first, err := scoped.ResolvePersonaAgentPrincipal(cell.ctx, demo.profile.PersonaID, 1); err != nil || first.PrincipalID != identities[localAgentDemoTenant] {
		t.Fatalf("publishing version 2 changed version 1's identity: %+v err=%v", first, err)
	}
}
