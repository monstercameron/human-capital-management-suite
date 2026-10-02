package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/roleaccessstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func TestTodo_AGENTUX_005(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	target := agenteval.PersonaEvaluationTarget{TenantID: localAgentDemoTenant, SyntheticTenantID: localAgentDemoSynthetic, InvokerID: localAgentDemoAdmin, PersonaID: localAgentDemoPersonaID, PersonaVersion: 4, ProfileDigest: digest, ModelDigest: digest}
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	report, suite, err := localAgentDemoEvaluationReport(context.Background(), target, at)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := report.Evidence()
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.Passed || len(evidence.Cases) != len(suite.Cases) || len(evidence.Cases) < 8 || evidence.Target != target {
		t.Fatalf("deterministic evaluation evidence=%#v cases=%d", evidence, len(suite.Cases))
	}
	for _, result := range evidence.Cases {
		if !result.Passed || !strings.HasPrefix(result.EvidenceDigest, "sha256:") {
			t.Fatalf("case %q did not produce sealed passing evidence", result.CaseID)
		}
	}

	reason := personaServedProviderUnavailableReason(ServeConfig{Profile: ServeProfileLocalDev, AgentModelConfigFile: filepath.Join(t.TempDir(), "missing.json")}, func(string) string { return "configured" })
	if !strings.Contains(reason, localPersonaModelPreparationCommand) {
		t.Fatalf("unavailable reason %q does not name the preparation command", reason)
	}
}

func TestTodo_AGENTUX_005_Security(t *testing.T) {
	loopback := "postgres://local@127.0.0.1:5432/hcm?sslmode=disable"
	valid := LocalAgentDemoConfig{Profile: ServeProfileLocalDev, Tenant: localAgentDemoTenant, DatabaseURL: loopback, AgentDatabaseURL: loopback, ChatDatabaseURL: loopback, DocumentDatabaseURL: loopback, ReviewAuthorityDatabaseURL: loopback, PolicyAuthorityDatabaseURL: loopback, RouteAuthorityDatabaseURL: loopback, EvaluationProvisionerDatabaseURL: loopback}
	if err := ValidateLocalAgentDemoConfig(valid); err != nil {
		t.Fatalf("local config refused: %v", err)
	}
	for name, mutate := range map[string]func(*LocalAgentDemoConfig){
		"production profile": func(c *LocalAgentDemoConfig) { c.Profile = ServeProfileStandard },
		"other tenant":       func(c *LocalAgentDemoConfig) { c.Tenant = "harborcare-demo" },
		"remote database":    func(c *LocalAgentDemoConfig) { c.AgentDatabaseURL = "postgres://agent@db.internal:5432/hcm" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := valid
			mutate(&changed)
			if err := ValidateLocalAgentDemoConfig(changed); err == nil {
				t.Fatal("unsafe agent-demo configuration accepted")
			}
		})
	}

	_, localPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	productionPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	digest := "sha256:" + strings.Repeat("b", 64)
	keyID, err := PersonaEvaluationVerificationKeyID(localAgentDemoTenant, "hcmnext.eval.policy-helper.v1", localAgentDemoEvaluationKeyID)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := agentpersonastore.SignPersonaEvaluationClaim(localPrivate, agentpersonastore.PersonaEvaluationClaim{TenantID: localAgentDemoTenant, RunID: "local-run", PersonaID: localAgentDemoPersonaID, PersonaVersion: 4, ProfileDigest: digest, SuiteDigest: digest, RunDigest: digest, ModelDigest: digest, Passed: true, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), KeyID: keyID})
	if err != nil {
		t.Fatal(err)
	}
	production, err := agentpersonastore.NewEvaluationSealAuthority(map[string]ed25519.PublicKey{"production-evaluator": productionPublic}, func(values.TenantId) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceURL, []byte("tenant")) }, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	tx := &agentUXRejectedTx{}
	if err := production.RecordPersonaEvaluation(context.Background(), tx, signed); err == nil || tx.execs != 0 {
		t.Fatalf("production verifier accepted local-only evaluator: err=%v database_writes=%d", err, tx.execs)
	}
}

func TestTodo_AGENTUX_005_Integration(t *testing.T) {
	if runtime.GOOS == "windows" && os.Getenv(pgtest.EnvDatabaseURL) == "" {
		t.Skip("HCMNEXT_TEST_DATABASE_URL is required for this multi-store PostgreSQL integration test")
	}
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, tenantID := values.TenantId(localAgentDemoTenant), uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenantID)
	mapper := func(value values.TenantId) uuid.UUID {
		if value == tenant {
			return tenantID
		}
		return uuid.Nil
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	starter, _ := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	suite := agenteval.PolicyHelperSuite(starter.SkillPins[0].ID)
	keyID, err := PersonaEvaluationVerificationKeyID(string(tenant), suite.ID, localAgentDemoEvaluationKeyID)
	if err != nil {
		t.Fatal(err)
	}
	reviews, err := agentpersonastore.NewDurableReviewAuthority(mapper)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)
	evaluations, err := agentpersonastore.NewEvaluationSealAuthority(map[string]ed25519.PublicKey{keyID: public}, mapper, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	root, err := agentpersonastore.NewWithPublicationAuthorities(conn, mapper, reviews, evaluations)
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := root.Scoped(tenant)
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	profile := agentpersona.PersonaProfile{Manifest: agentpersona.AgentManifestRef{ID: "hcmnext.agent.policy-helper", Version: 1, SchemaVersion: 1, Digest: digest}, PersonaID: localAgentDemoPersonaID, Version: 4, Handle: localAgentDemoAgentID, DisplayName: "Policy Helper", AvatarRef: "avatar:policy-helper", Purpose: "Answer approved policy questions", Audience: agentpersona.Audience{Roles: []string{"worker_self"}, Populations: []string{"employees"}, OrganizationScopes: []string{"ironridge"}}, SkillPins: starter.SkillPins, TierCeiling: agentskills.TierT0, ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect, agentpersona.ConversationChannel}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate, agentpersona.ChannelPublic}, Instructions: "Answer approved questions.", Owner: localAgentDemoAdmin, Steward: "ir-003-loretta-haynes", EvalSuiteRef: suite.ID, EvalLimits: agentpersona.EvaluationLimits{MaxCost: 10000, MaxSteps: 8, MaxLatencyMS: 120000}, DataClassesRead: []string{"PUBLIC", "INTERNAL"}}
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(sealed.Profile)
	row := agentpersonastore.PersonaVersion{TenantID: tenant, PersonaID: profile.PersonaID, Version: 4, AgentVersion: profile.Manifest.ID + "@1", Handle: profile.Handle, DisplayName: profile.DisplayName, Profile: raw, ContentDigest: sealed.Digest, CreatedAt: now}
	owner := agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: row.PersonaID, Role: agentpersonastore.BusinessOwner, PrincipalID: localAgentDemoAdmin}
	steward := agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: row.PersonaID, Role: agentpersonastore.TechnicalSteward, PrincipalID: profile.Steward}
	if err := scoped.CreateDraft(ctx, row, owner, steward, localAgentDemoAdmin, now); err != nil {
		t.Fatal(err)
	}
	if err := scoped.AppendLifecycle(ctx, agentpersonastore.LifecycleEvent{TenantID: tenant, EventID: "review-request", PersonaID: row.PersonaID, PersonaVersion: row.Version, From: agentpersonastore.StateDraft, To: agentpersonastore.StateInReview, Reason: "independent review", ActorID: localAgentDemoAdmin, OccurredAt: now}); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO persona_review_grant(tenant_id,grant_id,principal_id,permission,granted_at,expires_at) VALUES($1,'review-grant','ir-008-curtis-bell','persona:review',now()-interval '1 hour',now()+interval '1 day')`, tenantID)
	db.Exec(t, `INSERT INTO persona_review_decision(tenant_id,review_id,persona_id,persona_version,profile_digest,author_id,reviewer_id,grant_id,permission,decision,review_digest,reviewed_at,expires_at) VALUES($1,'review-1',$2,4,$3,$4,'ir-008-curtis-bell','review-grant','persona:review','APPROVE',$5,now()-interval '1 hour',now()+interval '1 day')`, tenantID, row.PersonaID, row.ContentDigest, localAgentDemoAdmin, "sha256:"+strings.Repeat("c", 64))

	policyRef, policyRaw := LocalPersonaOpenAIModelPolicyReference()
	candidate := agentmodel.ModelProfile{
		ID: "local-openai-policy-helper-integration", Identity: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: LocalPersonaOpenAIModelID, Version: LocalPersonaOpenAIModelVersion},
		Regions: []string{LocalPersonaOpenAIRegion}, DataClasses: []string{string(trustdlp.ClassPublic), string(trustdlp.ClassInternal)}, TaskProfileIDs: []string{"local.persona.policy-helper.reply"},
		MaxLatency: LocalPersonaOpenAIMaxLatency, MaxCostMicros: LocalPersonaOpenAIMaxCostMicros, ExpectedCostMicros: LocalPersonaOpenAIMaxCostMicros,
		SemanticsDigest: policyRef.Digest, OutputSchemaDigest: PersonaChatReplySchemaDigest, ToolSchemaDigest: LocalPersonaOpenAIToolSchemaDigest(),
	}
	candidate.ProfileDigest = agentmodel.ModelProfileDigest(candidate)
	modelDigest := "sha256:" + candidate.ProfileDigest
	target := agenteval.PersonaEvaluationTarget{TenantID: string(tenant), SyntheticTenantID: localAgentDemoSynthetic, InvokerID: localAgentDemoAdmin, PersonaID: row.PersonaID, PersonaVersion: row.Version, ProfileDigest: row.ContentDigest, ModelDigest: modelDigest}
	report, _, err := localAgentDemoEvaluationReport(ctx, target, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	issuer := PersonaEvaluationIssuer{Versions: personaAdminInstallationVersions{store: root}, Recorder: &PersonaEvaluationEvidenceService{store: root}, TenantID: tenant, SuiteID: suite.ID, SuiteDigest: agenteval.PersonaSuiteDigest(suite), KeyID: localAgentDemoEvaluationKeyID, PrivateKey: private, ModelDigest: modelDigest, Now: func() time.Time { return now }, FreshFor: time.Hour, NewRunID: func() string { return "local-evaluation-1" }}
	if _, err := issuer.Issue(ctx, report); err != nil {
		t.Fatal(err)
	}
	qualified := candidate
	qualified.Evaluation = agentmodel.ModelEvaluation{AgentVersionDigest: profile.Manifest.Digest, SuiteDigest: agenteval.PersonaSuiteDigest(suite), Passed: true}
	qualified.ProfileDigest = agentmodel.ModelProfileDigest(qualified)
	selection := agentmodel.ModelSelection{ProfileID: qualified.ID, ProfileDigest: qualified.ProfileDigest, Identity: qualified.Identity}
	term := LocalPersonaOpenAIProcessingTerms(qualified.ID)
	route := PersonaRunModelRoute{
		Route:   agentmodel.RouteRequest{TraceID: "local-dev-policy-helper-route", Pin: agentmodel.ModelPin{AgentVersionDigest: profile.Manifest.Digest, TaskProfileID: qualified.TaskProfileIDs[0], Primary: selection, SemanticsDigest: qualified.SemanticsDigest, OutputSchemaDigest: qualified.OutputSchemaDigest, ToolSchemaDigest: qualified.ToolSchemaDigest}, Task: agentmodel.TaskProfile{ID: qualified.TaskProfileIDs[0], AgentVersionDigest: profile.Manifest.Digest, Region: LocalPersonaOpenAIRegion, DataClasses: qualified.DataClasses, MaxLatency: qualified.MaxLatency, MaxCostMicros: qualified.MaxCostMicros, SemanticsDigest: qualified.SemanticsDigest, OutputSchemaDigest: qualified.OutputSchemaDigest, ToolSchemaDigest: qualified.ToolSchemaDigest}, BudgetRemainingMicros: qualified.MaxCostMicros},
		Purpose: LocalPersonaOpenAIPurpose, Processing: agentmodel.ProcessingPolicy{Residency: LocalPersonaOpenAIRegion, Retention: fmt.Sprintf("%s:%d", term.Retention.Mode, int64(term.Retention.MaxAge)), TrainingUse: term.TrainingUse, Logging: term.Logging},
		Egress: agentegress.Profile{ID: qualified.ID, Kind: agentegress.TargetModel, AllowedRegions: term.AllowedRegions, AllowedClasses: term.AllowedClasses, Retention: term.Retention}, ProfileClass: trustdlp.ClassInternal, InvokerClass: trustdlp.ClassInternal, ThreadClass: trustdlp.ClassInternal,
	}
	routeURL, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	query := routeURL.Query()
	query.Set("search_path", db.Schema)
	routeURL.RawQuery = query.Encode()
	agentDSN := routeURL.String()
	if created, err := publishLocalAgentDemoRoute(ctx, LocalAgentDemoConfig{Tenant: string(tenant), RouteAuthorityDatabaseURL: agentDSN}, evaluations, mapper, row, profile, qualified, route, modelDigest, policyRaw, policyRef, "local-evaluation-1", "ironridge", now); err != nil || !created {
		t.Fatalf("publish prepared model route: created=%t err=%v", created, err)
	}
	agents, err := agentstore.New(ctx, agentstore.Config{DSN: agentDSN, CoreDSN: "postgres://other@127.0.0.1:1/other"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(agents.Close)
	deployment, err := NewLocalPersonaOpenAIModelDeployment(string(tenant), []agentmodel.ModelProfile{qualified}, localOpenAIMaterialFixture())
	if err != nil {
		t.Fatal(err)
	}
	policyRecord := LocalPersonaOpenAIPolicyRecords()[0]
	runtimeAuthority := PersonaModelDeploymentPolicyAuthority{Routes: agents, Deployment: func(context.Context, values.TenantId) (PersonaModelDeployment, error) { return deployment, nil }, TenantUUID: mapper, Now: func() time.Time { return now }}
	runtimeRequest := agentrun.Request{Source: agentrun.SourceIdentity{TenantID: string(tenant)}, LegalEntity: "ironridge", Agent: agentrun.VersionRef{Digest: profile.Manifest.Digest}, Deadline: now.Add(time.Minute), Budget: agentrun.Budget{MaxCostMicros: 100, MaxInputTokens: 1000, MaxOutputTokens: 500}}
	if err := runtimeAuthority.CheckAgentPolicyDeployment(ctx, runtimeRequest, agentmodelpolicystore.Current{Record: policyRecord}); err != nil {
		t.Fatalf("runtime refused prepared model route: %v", err)
	}
	evidence, err := scoped.ResolvePublicationEvidence(ctx, row.PersonaID, row.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := scoped.Publish(ctx, agentpersonastore.LifecycleEvent{TenantID: tenant, EventID: "publication-1", PersonaID: row.PersonaID, PersonaVersion: row.Version, From: agentpersonastore.StateInReview, To: agentpersonastore.StatePublished, Reason: "reviewed persona published", ActorID: localAgentDemoAdmin, OccurredAt: now}, evidence); err != nil {
		t.Fatal(err)
	}
	installation := agentpersonastore.PersonaInstallation{TenantID: tenant, InstallationID: "install-general", PersonaID: row.PersonaID, PersonaVersion: row.Version, ConversationID: "general", ConversationClass: agentpersonastore.ConversationPublic, InstallerID: localAgentDemoAdmin, ChannelPolicy: agentpersonastore.ChannelPolicy{PlacementClass: "ANY_INTERNAL", MaxTier: "T0", AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPublic}, ConversationSearchAllowed: true}, State: agentpersonastore.InstallationActive, Revision: 1, RevocationEpoch: 1, CreatedAt: now, UpdatedAt: now}
	if err := scoped.Install(ctx, installation); err != nil {
		t.Fatal(err)
	}
	allowed := []agentpersonastore.AvailableInstallation{{PersonaID: row.PersonaID, PersonaVersion: row.Version, InstallationID: installation.InstallationID, ConversationID: installation.ConversationID}}
	principal, err := localAgentDemoPrincipal(now, string(tenant), localAgentDemoAdmin, "org:ironridge:test")
	if err != nil {
		t.Fatal(err)
	}
	discovered := make([]agentskills.SkillRecord, 0, len(starter.SkillPins))
	for _, pin := range starter.SkillPins {
		discovered = append(discovered, agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version}, Digest: pin.Digest, Status: agentskills.StatusActive})
	}
	availableReader := &TenantAvailablePersonaReader{Backend: AgentPersonaStoreBackend{Store: root}, Audience: agentUXAvailableAudience{installations: allowed}, Skills: agentUXAvailableSkills{records: discovered}}
	available, err := availableReader.ListAvailable(trust.WithPrincipal(ctx, principal), principal)
	if err != nil || len(available) != 1 || available[0].Profile.DisplayName != "Policy Helper" {
		t.Fatalf("available Policy Helper=%#v err=%v", available, err)
	}

	writer := &personaChatWriterFake{}
	streaming := &streamingChatService{ConversationService: servedPortChatWriter{writer: writer}}
	refs := &lazyPersonaReferenceSource{}
	refs.bind(servedPortReferenceSource{lookup: &personaReferenceLookupFake{}})
	personaWiring := &personaServeWiring{refs: refs, now: func() time.Time { return now }}
	invocationConfig := PersonaInvocationProductionConfig{
		Authority: personaAuthorityFake{admission: personaAdmission()}, Grants: &personaGrantFake{}, T0Skills: personaT0PolicyFake{allowed: true},
		Fence: &personaRunWorkerFenceFake{}, Leases: personaRunWorkerLeaseFake{id: "lease"},
		Run: PersonaRunStarterConfig{
			Builder: mustPersonaRunBuilder(t), Authority: personaChatAdmissionAuthorityFake{},
			Model: personaRunWorkerModelFake{}, Work: &personaRunWorkerWorkFake{}, Output: &personaRunWorkerOutputFake{}, Reply: personaRunWorkerReplyFake{},
			WorkerID: "local-dev-policy-helper", LeaseTTL: time.Minute, Now: func() time.Time { return now },
		},
	}
	servedRuntime, err := composeServedPersonaInvocation(streaming, personaWiring, composedAgentDatabase{store: &agentstore.Store{}, personas: root}, &invocationConfig, &recordingLogger{})
	if err != nil || servedRuntime == nil || servedRuntime.Worker == nil || streaming.personaInvocation != servedRuntime.Wiring {
		t.Fatalf("served Policy Helper runtime=%v binding=%v err=%v", servedRuntime != nil, streaming.personaInvocation != nil, err)
	}
	installed, err := localAgentDemoInstalled(ctx, scoped, installation.ConversationID, row.PersonaID, row.Version)
	if err != nil || !installed {
		t.Fatalf("second preparation did not recognize current installation: installed=%t err=%v", installed, err)
	}
	if resolved, err := scoped.ResolvePublicationEvidence(ctx, row.PersonaID, row.Version); err != nil || resolved != evidence {
		t.Fatalf("second preparation did not reuse durable evidence: %#v err=%v", resolved, err)
	}
	exerciseAgentUX005ServedSurfaces(t, root, scoped, row, profile, discovered, now)
}

func exerciseAgentUX005ServedSurfaces(t *testing.T, personas *agentpersonastore.Store, scoped *agentpersonastore.TenantStore, row agentpersonastore.PersonaVersion, profile agentpersona.PersonaProfile, skills []agentskills.SkillRecord, now time.Time) {
	t.Helper()
	ctx := context.Background()
	coreDB := pgtest.New(t)
	core, err := pgxadapter.NewPool(ctx, personaChatSchemaDSN(t, coreDB.URL, coreDB.Schema), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.Close)
	coreStore, err := pgstore.New(core, pgstore.WithCellID("agentux-005-live"))
	if err != nil {
		t.Fatal(err)
	}
	if err := coreStore.Bootstrap(ctx, localAgentDemoTenant); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bootstrapLocalDevWorkforce(ctx, core, localAgentDemoTenant); err != nil {
		t.Fatal(err)
	}
	mapper := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	roles := roleaccessstore.New(core, mapper, productFeatureCatalog()...)
	if err := roles.Bootstrap(ctx, localAgentDemoTenant, "system:bootstrap"); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrapLocalDevRoleAssignments(ctx, core, localAgentDemoTenant); err != nil {
		t.Fatal(err)
	}

	chatDB := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, chatDB)
	chatDSN := personaChatSchemaDSN(t, chatDB.URL, chatDB.Schema)
	raw, err := chatstore.New(ctx, chatstore.Config{DSN: chatDSN})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(raw.Close)
	directID, err := chatcore.DirectPairConversationID(localAgentDemoTenant, []chatcore.MemberRef{{TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAdmin}, {TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAgentID}})
	if err != nil {
		t.Fatal(err)
	}
	broken := chatcore.Conversation{ID: directID, TenantID: localAgentDemoTenant, Kind: chatcore.Direct, OwnerID: localAgentDemoAdmin}
	brokenMembers := []chatcore.Membership{
		{ConversationID: directID, TenantID: localAgentDemoTenant, HomeTenantID: localAgentDemoTenant, SubjectID: localAgentDemoAdmin, Role: chatcore.Manager, HistoryVisibility: chatcore.FullHistory},
		{ConversationID: directID, TenantID: localAgentDemoTenant, HomeTenantID: localAgentDemoTenant, SubjectID: localAgentDemoAgentID, Role: chatcore.Member, HistoryVisibility: chatcore.FullHistory},
	}
	if _, err := chatstore.NewAdapter(raw).CreateConversation(ctx, broken, brokenMembers, "agentux-005-broken-direct"); err != nil {
		t.Fatal(err)
	}
	// The malformed legacy row predates audience-policy provisioning. Expose
	// its original policy interface so the fixture preserves that exact shape.
	if _, err := ProvisionLocalDevPersonaDirectPolicy(ctx, proactiveLegacyPolicyStore{raw}, ServeProfileLocalDev, localAgentDemoTenant, directID, localAgentDemoAdmin); err != nil {
		t.Fatal(err)
	}

	runtime, err := composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: chatDSN, ChatCursorKey: "agentux-005-preparation-cursor"}, time.Now, newCurrentWorkerChatFacts(roles, core, mapper, nil), core, nil)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := localAgentDemoPrincipal(now, localAgentDemoTenant, localAgentDemoAdmin, profile.Audience.OrganizationScopes[0])
	if err != nil {
		t.Fatal(err)
	}
	adminCtx := trust.WithPrincipal(ctx, principal)
	chatPrincipal := chatcore.Principal{TenantID: localAgentDemoTenant, SubjectID: localAgentDemoAdmin}
	publicID := localDevPersonaDemoConversationID(localAgentDemoTenant, "general")
	if _, err := runtime.service.CreateConversation(adminCtx, chatcore.CreateConversationRequest{Principal: chatPrincipal, TenantID: localAgentDemoTenant, ConversationID: publicID, Kind: chatcore.PublicChannel, Name: "General", IdempotencyKey: "agentux-005-general"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvisionLocalDevPersonaChatPolicy(ctx, raw, ServeProfileLocalDev, localAgentDemoTenant, publicID, "general"); err != nil {
		t.Fatal(err)
	}
	gotDirect, repaired, err := ensureLocalAgentDemoDirectConversation(adminCtx, raw, runtime.service, localAgentDemoTenant, localAgentDemoAdmin, localAgentDemoAgentID)
	if err != nil || !repaired || gotDirect != directID {
		t.Fatalf("repair direct=%q repaired=%t err=%v", gotDirect, repaired, err)
	}
	if _, err := ProvisionLocalDevPersonaDirectPolicy(ctx, raw, ServeProfileLocalDev, localAgentDemoTenant, directID, localAgentDemoAdmin); err != nil {
		t.Fatal(err)
	}
	runtime.close()
	lazyReferences := &lazyPersonaReferenceSource{}
	runtime, err = composeChat(ctx, ServeConfig{Profile: ServeProfileLocalDev, ChatEnabled: true, ChatDatabaseURL: chatDSN, ChatCursorKey: "agentux-005-live-cursor"}, time.Now, newCurrentWorkerChatFacts(roles, core, mapper, nil), core, nil, ChatComposition{PersonaReferences: lazyReferences})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.close)

	for _, conversation := range []string{publicID, directID} {
		if _, err := runtime.service.ListPosts(adminCtx, chatcore.ListPostsRequest{Principal: chatPrincipal, TenantID: localAgentDemoTenant, ConversationID: conversation, Page: chatcore.Page{PageSize: 20}}); err != nil {
			t.Fatalf("ListPosts(%s): %v", conversation, err)
		}
		if _, err := runtime.service.ListMemberships(adminCtx, chatcore.ListMembershipsRequest{Principal: chatPrincipal, TenantID: localAgentDemoTenant, ConversationID: conversation, Page: chatcore.Page{PageSize: 20}}); err != nil {
			t.Fatalf("ListMemberships(%s): %v", conversation, err)
		}
		if _, err := runtime.service.GetPreferences(adminCtx, chatcore.GetPreferencesRequest{Principal: chatPrincipal, TenantID: localAgentDemoTenant, ConversationID: conversation}); err != nil {
			t.Fatalf("GetPreferences(%s): %v", conversation, err)
		}
		if _, err := runtime.extensions.Counts(adminCtx, chatPrincipal, localAgentDemoTenant, conversation); err != nil {
			t.Fatalf("GetCounts(%s): %v", conversation, err)
		}
	}

	if err := ensureLocalAgentDemoChatIdentity(ctx, scoped, localAgentDemoAgentID, row.PersonaID, now); err != nil {
		t.Fatal(err)
	}
	for _, installation := range []agentpersonastore.PersonaInstallation{
		{TenantID: localAgentDemoTenant, InstallationID: "agentux-005-live-general", PersonaID: row.PersonaID, PersonaVersion: row.Version, ConversationID: publicID, ConversationClass: agentpersonastore.ConversationPublic, InstallerID: localAgentDemoAdmin, ChannelPolicy: agentpersonastore.ChannelPolicy{PlacementClass: "ANY_INTERNAL", MaxTier: "T0", AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPublic}, ConversationSearchAllowed: true}, State: agentpersonastore.InstallationActive, Revision: 1, RevocationEpoch: 1, CreatedAt: now, UpdatedAt: now},
		{TenantID: localAgentDemoTenant, InstallationID: "agentux-005-live-direct", PersonaID: row.PersonaID, PersonaVersion: row.Version, ConversationID: directID, ConversationClass: agentpersonastore.ConversationPrivate, InstallerID: localAgentDemoAdmin, ChannelPolicy: agentpersonastore.ChannelPolicy{PlacementClass: "PRIVATE", MaxTier: "T0", AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPrivate}, AlwaysPrivate: true}, State: agentpersonastore.InstallationActive, Revision: 1, RevocationEpoch: 1, CreatedAt: now, UpdatedAt: now},
	} {
		if err := scoped.Install(ctx, installation); err != nil {
			t.Fatal(err)
		}
	}
	directory := agentUXLiveDirectory{admin: localAgentDemoAdmin, roles: profile.Audience.Roles, populations: profile.Audience.Populations, organization: profile.Audience.OrganizationScopes[0]}
	audienceSource := &DatabasePersonaAudienceSource{Chat: runtime.service, Installations: personaInstallationStore{store: personas}, Directory: directory}
	available := &TenantAvailablePersonaReader{Backend: AgentPersonaStoreBackend{Store: personas}, Audience: &CurrentPersonaAudience{Source: audienceSource}, Skills: agentUXAvailableSkills{records: skills}}
	identities, err := newProductionPersonaChatIdentityDirectory(personas)
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := newProductionPersonaReferenceLookup(identities, personas)
	if err != nil {
		t.Fatal(err)
	}
	references, err := newPersonaChatReferenceSource(available, identities, lookup, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	lazyReferences.bind(references)
	surface := &PersonaChatSurface{Chat: runtime.service, References: references, Personas: available, Skills: agentUXAvailableSkills{records: skills}, Invocations: agentUXEmptyInvocations{}, Executions: func(context.Context, string) (runstate.Store, error) { return runstate.NewMemoryStore(), nil }, Now: func() time.Time { return now }}
	for _, conversation := range []string{publicID, directID} {
		directory, err := surface.Directory(adminCtx, conversation)
		if err != nil || len(directory.Personas) != 1 || directory.Personas[0].Reference.ID != localAgentDemoAgentID {
			t.Fatalf("persona directory(%s)=%+v err=%v", conversation, directory, err)
		}
		progress, err := surface.Progress(adminCtx, conversation)
		if err != nil || len(progress.Invocations) != 0 {
			t.Fatalf("persona progress(%s)=%+v err=%v", conversation, progress, err)
		}
	}

	admin, err := NewPersonaAdminCatalogClient(PersonaAdminCatalogComposition{Store: personaAdminCatalogStoreAdapter{store: personas}, Targets: &ChatDirectoryPersonaCatalogTargets{Chat: runtime.service, Directory: directory}, Skills: agentUXAvailableSkills{records: skills}, Grants: agentUXCatalogGrants{}, Authorizer: agentUXCatalogAuth{}})
	if err != nil {
		t.Fatal(err)
	}
	adminSnapshot, err := admin.Snapshot(adminCtx, productui.PersonaAdminSnapshotRequest{TenantID: localAgentDemoTenant, Principal: localAgentDemoAdmin})
	if err != nil || !adminSnapshot.Available || len(adminSnapshot.Personas) == 0 || adminSnapshot.Personas[0].Name != "Policy Helper" {
		t.Fatalf("persona admin snapshot=%+v err=%v", adminSnapshot, err)
	}
	page, err := NewAgentPageCatalogBinding(&pageCatalogBindingClient{snapshot: productui.AgentSnapshot{Availability: productui.AgentsAvailable}}, available, agentUXAvailableSkills{records: skills})
	if err != nil {
		t.Fatal(err)
	}
	pageSnapshot, err := page.Snapshot(adminCtx, productui.AgentSnapshotRequest{TenantID: localAgentDemoTenant, Principal: localAgentDemoAdmin})
	if err != nil || len(pageSnapshot.Agents) != 1 || pageSnapshot.Agents[0].Name != "Policy Helper" {
		t.Fatalf("Agents page snapshot=%+v err=%v", pageSnapshot, err)
	}
	mentionResolver, err := newPersonaChatReferenceResolver(lookup, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	tasks := &agentUXLiveTasks{availability: productui.AgentsAvailable}
	runner := &agentUXDeterministicWorker{chat: runtime.service, principal: chatPrincipal, tasks: tasks}
	scopes := agentinvoke.SkillScopes{personaPolicyHelperSkillID: {"chat.current"}}
	invocation, err := newPersonaChatInvocation(personaChatInvocationConfig{Chat: runtime.service, References: mentionResolver, Authority: agentUXLiveAuthority{personaID: row.PersonaID, version: fmt.Sprint(row.Version), installationID: "agentux-005-live-general", skills: scopes}, Grants: agentUXLiveGrant{}, Runs: runner, T0Skills: agentUXLiveT0Policy{personaID: row.PersonaID}, Repository: agentinvoke.NewMemoryRepository()})
	if err != nil {
		t.Fatal(err)
	}
	humanPost, err := invocation.SendPost(adminCtx, chatcore.SendPostRequest{Principal: chatPrincipal, TenantID: localAgentDemoTenant, ConversationID: publicID, Body: "@policy-helper summarize the reviewed leave policy", IdempotencyKey: "agentux-005-live-mention", References: []chatcore.Reference{{Kind: chatcore.AgentMention, TenantID: localAgentDemoTenant, ID: localAgentDemoAgentID, Display: "Policy Helper"}}})
	if err != nil {
		t.Fatal(err)
	}
	posts, err := runtime.service.ListPosts(adminCtx, chatcore.ListPostsRequest{Principal: chatPrincipal, TenantID: localAgentDemoTenant, ConversationID: publicID, Page: chatcore.Page{PageSize: 20}})
	if err != nil || len(posts.Posts) != 2 || posts.Posts[0].ID != humanPost.ID || posts.Posts[1].ParentID != humanPost.ID || !strings.Contains(posts.Posts[1].Body, "Ironridge Leave Policy") {
		t.Fatalf("delivered mention reply=%+v err=%v", posts.Posts, err)
	}
	directInvocation, err := newPersonaChatInvocation(personaChatInvocationConfig{Chat: runtime.service, References: mentionResolver, Authority: agentUXLiveAuthority{personaID: row.PersonaID, version: fmt.Sprint(row.Version), installationID: "agentux-005-live-direct", skills: scopes}, Grants: agentUXLiveGrant{}, Runs: runner, T0Skills: agentUXLiveT0Policy{personaID: row.PersonaID}, Repository: agentinvoke.NewMemoryRepository()})
	if err != nil {
		t.Fatal(err)
	}
	directPost, err := directInvocation.SendPost(adminCtx, chatcore.SendPostRequest{Principal: chatPrincipal, TenantID: localAgentDemoTenant, ConversationID: directID, Body: "@policy-helper answer from our direct conversation", IdempotencyKey: "agentux-025-live-direct-mention", References: []chatcore.Reference{{Kind: chatcore.AgentMention, TenantID: localAgentDemoTenant, ID: localAgentDemoAgentID, Display: "Policy Helper"}}})
	if err != nil {
		t.Fatal(err)
	}
	directPosts, err := runtime.service.ListPosts(adminCtx, chatcore.ListPostsRequest{Principal: chatPrincipal, TenantID: localAgentDemoTenant, ConversationID: directID, Page: chatcore.Page{PageSize: 20}})
	if err != nil || len(directPosts.Posts) != 2 || directPosts.Posts[0].ID != directPost.ID || directPosts.Posts[1].ParentID != directPost.ID || !strings.Contains(directPosts.Posts[1].Body, "Ironridge Leave Policy") {
		t.Fatalf("delivered direct mention reply=%+v err=%v", directPosts.Posts, err)
	}
	page, err = NewAgentPageCatalogBinding(tasks, available, agentUXAvailableSkills{records: skills})
	if err != nil {
		t.Fatal(err)
	}
	pageSnapshot, err = page.Snapshot(adminCtx, productui.AgentSnapshotRequest{TenantID: localAgentDemoTenant, Principal: localAgentDemoAdmin})
	if err != nil || len(pageSnapshot.Tasks) != 2 {
		t.Fatalf("Agents page task snapshot=%+v err=%v", pageSnapshot, err)
	}
	if pageSnapshot.Tasks[0].ID == pageSnapshot.Tasks[1].ID {
		t.Fatal("public and direct mentions shared a task")
	}
	for _, task := range pageSnapshot.Tasks {
		if task.AgentID != row.PersonaID || task.State != productui.AgentTaskCompleted {
			t.Fatalf("mention task=%+v", task)
		}
	}
	if _, repaired, err := ensureLocalAgentDemoDirectConversation(adminCtx, raw, runtime.service, localAgentDemoTenant, localAgentDemoAdmin, localAgentDemoAgentID); err != nil || repaired {
		t.Fatalf("idempotent direct repair=%t err=%v", repaired, err)
	}
}

type agentUXAvailableAudience struct {
	installations []agentpersonastore.AvailableInstallation
}

func (a agentUXAvailableAudience) ResolveAvailablePersonaInstallations(context.Context, *trust.Principal) ([]agentpersonastore.AvailableInstallation, error) {
	return append([]agentpersonastore.AvailableInstallation(nil), a.installations...), nil
}

type agentUXAvailableSkills struct {
	records []agentskills.SkillRecord
}

func (s agentUXAvailableSkills) Discover(context.Context, *trust.Principal, string) ([]agentskills.SkillRecord, error) {
	return append([]agentskills.SkillRecord(nil), s.records...), nil
}

func (s agentUXAvailableSkills) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	for _, record := range s.records {
		if record.Definition.Key() == pin.Key() && record.Digest == pin.Digest {
			return record, nil
		}
	}
	return agentskills.SkillRecord{}, errors.New("skill pin is unavailable")
}

type agentUXLiveDirectory struct {
	admin, organization string
	roles, populations  []string
}

func (d agentUXLiveDirectory) ResolvePersonaAudienceMember(_ context.Context, tenant, subject string) (PersonaAudienceMember, error) {
	if tenant != localAgentDemoTenant || subject != d.admin {
		return PersonaAudienceMember{}, ErrPersonaAudienceDirectoryFactsMissing
	}
	return PersonaAudienceMember{SubjectID: subject, Roles: append([]string(nil), d.roles...), Populations: append([]string(nil), d.populations...), OrganizationScope: d.organization}, nil
}

func (d agentUXLiveDirectory) ResolvePersonaCatalogTarget(_ context.Context, tenant values.TenantId, subject string) (productui.PersonaAdminTarget, error) {
	if tenant != values.TenantId(localAgentDemoTenant) || subject != d.admin {
		return productui.PersonaAdminTarget{}, ErrPersonaAudienceDirectoryFactsMissing
	}
	return productui.PersonaAdminTarget{ID: subject, Label: "Walt Brennan", Kind: "PERSON"}, nil
}

type agentUXEmptyInvocations struct{}

func (agentUXEmptyInvocations) ListPersonaInvocations(context.Context, string, string, string) ([]agentinvoke.Invocation, error) {
	return nil, nil
}

func (agentUXEmptyInvocations) Lookup(context.Context, string, string, string) (agentinvoke.Invocation, error) {
	return agentinvoke.Invocation{}, errors.New("invocation not found")
}

type agentUXCatalogGrants struct{}

func (agentUXCatalogGrants) ResolvePersonaSkillGrant(context.Context, trust.Principal, values.TenantId, string, string, agentskills.SkillPin) (PersonaCatalogGrant, error) {
	return PersonaCatalogGrant{Allowed: true, Tier: agentskills.TierT0.String()}, nil
}

type agentUXCatalogAuth struct{}

func (agentUXCatalogAuth) AuthorizePersonaCatalog(context.Context, *trust.Principal, values.TenantId) error {
	return nil
}

type agentUXLiveAuthority struct {
	personaID, version, installationID string
	skills                             agentinvoke.SkillScopes
}

func (a agentUXLiveAuthority) Resolve(_ context.Context, request agentinvoke.AdmissionRequest) (agentinvoke.Admission, error) {
	if request.PersonaID != a.personaID || request.InvokerID != localAgentDemoAdmin || request.TenantID != localAgentDemoTenant {
		return agentinvoke.Admission{}, errors.New("unexpected live invocation")
	}
	return agentinvoke.Admission{Persona: agentinvoke.Persona{ID: a.personaID, Version: a.version, PinnedSkills: a.skills.Clone(), Current: true}, Installation: agentinvoke.Installation{ID: a.installationID, Current: true, SkillCeiling: a.skills.Clone()}, Channel: agentinvoke.ChannelPolicy{SkillCeiling: a.skills.Clone()}, Discoverable: a.skills.Clone(), HumanMember: true, AudienceMember: true, PersonaInstalled: true}, nil
}

type agentUXLiveGrant struct{}

func (agentUXLiveGrant) CreateOnBehalfOfGrant(_ context.Context, request agentinvoke.GrantRequest) (agentinvoke.DelegationGrant, error) {
	return agentinvoke.DelegationGrant{ID: "agentux-005-live-grant", UserID: request.UserID, TenantID: request.TenantID, TaskID: request.InvocationID, TargetAgentID: request.TargetAgentID, Skills: request.Skills.Clone(), ExpiresAt: request.ExpiresAt}, nil
}

type agentUXLiveT0Policy struct{ personaID string }

func (p agentUXLiveT0Policy) IsBoundT0Run(_ context.Context, request agentinvoke.RunRequest) (bool, error) {
	return request.PersonaID == p.personaID && request.TenantID == localAgentDemoTenant && request.InvokerID == localAgentDemoAdmin && len(request.Skills) == 1, nil
}

type agentUXLiveTasks struct {
	availability productui.AgentsAvailability
	tasks        []productui.AgentTask
}

func (c *agentUXLiveTasks) Snapshot(context.Context, productui.AgentSnapshotRequest) (productui.AgentSnapshot, error) {
	return productui.AgentSnapshot{Availability: c.availability, Tasks: append([]productui.AgentTask(nil), c.tasks...)}, nil
}

type agentUXDeterministicWorker struct {
	chat      chatcore.ConversationService
	principal chatcore.Principal
	tasks     *agentUXLiveTasks
}

func (w *agentUXDeterministicWorker) Start(ctx context.Context, request agentinvoke.RunRequest) error {
	if w == nil || w.chat == nil || w.tasks == nil || request.PersonaID != localAgentDemoPersonaID || request.ConversationID == "" || request.InvokingPostID == "" {
		return errors.New("deterministic Policy Helper worker is unavailable")
	}
	reply := "Ironridge employees receive the reviewed leave benefit described in the Ironridge Leave Policy."
	post, err := w.chat.SendPost(ctx, chatcore.SendPostRequest{Principal: w.principal, TenantID: request.TenantID, ConversationID: request.ConversationID, ParentID: request.ThreadID, Body: reply, IdempotencyKey: "agentux-005-reply:" + request.InvocationID})
	if err != nil {
		return err
	}
	w.tasks.tasks = append(w.tasks.tasks, productui.AgentTask{ID: request.InvocationID, AgentID: request.PersonaID, Title: "Policy question", Goal: "Summarize the reviewed leave policy", AnswerText: reply, ResultPreview: reply, State: productui.AgentTaskCompleted, CreatedAt: post.CreatedAt, UpdatedAt: post.CreatedAt})
	return nil
}

type agentUXRejectedTx struct{ execs int }

func (t *agentUXRejectedTx) Exec(context.Context, string, ...any) (int64, error) {
	t.execs++
	return 0, nil
}
func (*agentUXRejectedTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected query")
}
func (*agentUXRejectedTx) QueryRow(context.Context, string, ...any) dbport.Row {
	return agentUXRejectedRow{}
}
func (*agentUXRejectedTx) Commit(context.Context) error   { return nil }
func (*agentUXRejectedTx) Rollback(context.Context) error { return nil }

type agentUXRejectedRow struct{}

func (agentUXRejectedRow) Scan(...any) error { return errors.New("unexpected query") }
