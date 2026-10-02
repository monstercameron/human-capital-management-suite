package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestAgentUXGeneral_RuntimeProvisionerRejectsCrossTenant(t *testing.T) {
	p := &LocalPersonaRuntimeProvisioner{Config: LocalAgentDemoConfig{Tenant: localAgentDemoTenant}}
	err := p.ProvisionPersonaRuntime(nil, PersonaAdminCommandActor{}, agentpersonastore.PersonaVersion{}, agentpersona.PersonaProfile{}, agentpersonastore.PublicationEvidence{})
	if !errors.Is(err, ErrPersonaAdminRuntimeUnavailable) {
		t.Fatalf("invalid provisioner error = %v", err)
	}
}

func TestAgentUXGeneral_EvaluationAuthorityAcceptsBothSuites(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := localAgentDemoEvaluationAuthority(privateKey, localAgentDemoTenant, func(tenant values.TenantId) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceURL, []byte(tenant)) }, time.Now)
	if err != nil || authority == nil {
		t.Fatalf("two-suite evaluation authority = %v, %v", authority, err)
	}
}

func TestAgentUXGeneral_DeploymentAddsAssistantWithoutChangingPolicyProfile(t *testing.T) {
	material := localOpenAIMaterialFixture()
	policy := agentmodel.ModelProfile{ID: "policy", Identity: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: LocalPersonaOpenAIModelID, Version: LocalPersonaOpenAIModelVersion}, Regions: []string{LocalPersonaOpenAIRegion}, DataClasses: []string{"INTERNAL", "PUBLIC"}, TaskProfileIDs: []string{"policy.reply"}, MaxLatency: time.Minute, MaxCostMicros: 100, ExpectedCostMicros: 100, SemanticsDigest: "sha256:" + strings.Repeat("a", 64), OutputSchemaDigest: "sha256:" + strings.Repeat("b", 64), ToolSchemaDigest: "sha256:" + strings.Repeat("c", 64), Evaluation: agentmodel.ModelEvaluation{AgentVersionDigest: "sha256:" + strings.Repeat("d", 64), SuiteDigest: agenteval.PersonaSuiteDigest(agenteval.PolicyHelperSuite("search")), Passed: true}}
	policy.ProfileDigest = agentmodel.ModelProfileDigest(policy)
	assistant := policy
	assistant.ID, assistant.TaskProfileIDs = "assistant", []string{"assistant.reply"}
	assistant.Evaluation.AgentVersionDigest = "sha256:" + strings.Repeat("e", 64)
	assistant.Evaluation.SuiteDigest = agenteval.PersonaSuiteDigest(agenteval.AssistantSuite("search", "reply"))
	assistant.ProfileDigest = agentmodel.ModelProfileDigest(assistant)
	path := filepath.Join(t.TempDir(), "deployment.json")
	if changed, err := ensureLocalAgentDemoDeploymentProfile(path, localAgentDemoTenant, material, policy); err != nil || !changed {
		t.Fatalf("policy deployment changed=%t err=%v", changed, err)
	}
	if changed, err := ensureLocalAgentDemoDeploymentProfile(path, localAgentDemoTenant, material, assistant); err != nil || !changed {
		t.Fatalf("Assistant deployment changed=%t err=%v", changed, err)
	}
	deployment, err := LoadPersonaModelDeployment(path)
	if err != nil || len(deployment.Profiles) != 2 || localAgentDemoDeploymentProfileJSON(deployment.Profiles[1]) != localAgentDemoDeploymentProfileJSON(policy) {
		t.Fatalf("merged deployment=%+v err=%v", deployment.Profiles, err)
	}
}

func TestAgentUXGeneral_CurrentIdentity_Security(t *testing.T) {
	ctx := context.Background()
	coreDB, agentDB := pgtest.New(t), pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, agentDB.SQL); err != nil {
		t.Fatal(err)
	}
	mapper := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	tenant := values.TenantId(localAgentDemoTenant)
	coreDB.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,$2,'general-test','General','ACTIVE',now())`, mapper(tenant), tenant.String())
	agentDB.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, mapper(tenant))
	core, err := pgxadapter.NewPool(ctx, personaChatSchemaDSN(t, coreDB.URL, coreDB.Schema), map[string]string{"role": "hcmnext_app"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.Close)
	agents := commonAgentOpenIntegrationStore(t, agentDB)
	personas, err := agentpersonastore.New(agents, mapper)
	if err != nil {
		t.Fatal(err)
	}
	scoped, _ := personas.Scoped(tenant)
	now := time.Now().UTC()
	sealed := validPersonaProfileForLifecycleTest(t, localAgentDemoAdmin)
	raw, _ := json.Marshal(sealed.Profile)
	row := agentpersonastore.PersonaVersion{TenantID: tenant, PersonaID: "assistant", Version: 1, AgentVersion: "agent@1", Handle: "assistant", DisplayName: "Assistant", Profile: raw, ContentDigest: sealed.Digest, CreatedAt: now}
	if err := scoped.PutVersion(ctx, row); err != nil {
		t.Fatal(err)
	}
	if err := ensureLocalAgentDemoServicePrincipal(ctx, core, personas, mapper, row, "assistant", now); err != nil {
		t.Fatal(err)
	}
	binding, err := scoped.ResolvePersonaAgentPrincipal(ctx, row.PersonaID, 1)
	if err != nil {
		t.Fatal(err)
	}
	row.Version = 2
	if err := scoped.PutVersion(ctx, row); err != nil {
		t.Fatal(err)
	}
	if err := scoped.RegisterPersonaAgentPrincipal(ctx, row.PersonaID, 2, binding.PrincipalID, now); err != nil {
		t.Fatal(err)
	}
	if err := ensureLocalAgentDemoServicePrincipal(ctx, core, personas, mapper, row, "assistant", now); err == nil {
		t.Fatal("another version's identity accepted")
	}
	row.Version = 1
	coreDB.Exec(t, `UPDATE principal SET expires_at=$3 WHERE tenant_id=$1 AND principal_id=$2`, mapper(tenant), binding.PrincipalID, now.Add(time.Second))
	if err := ensureLocalAgentDemoServicePrincipal(ctx, core, personas, mapper, row, "assistant", now.Add(2*time.Second)); err == nil {
		t.Fatal("expired existing identity accepted")
	}
}

func TestAgentUXGeneral_AssistantDraft_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	mapper := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	tenant := values.TenantId(localAgentDemoTenant)
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, mapper(tenant))
	agents := commonAgentOpenIntegrationStore(t, db)
	personas, err := agentpersonastore.New(agents, mapper)
	if err != nil {
		t.Fatal(err)
	}
	scoped, _ := personas.Scoped(tenant)
	starter, _ := agenttemplate.PersonaStarterFor(localAgentDemoAssistantStarterID, 1)
	manifest, err := ensureLocalAgentDemoAssistantManifest(ctx, agents, mapper(tenant), starter)
	if err != nil {
		t.Fatal(err)
	}
	policy := validPersonaProfileForLifecycleTest(t, localAgentDemoAdmin).Profile
	now := time.Now().UTC().Truncate(time.Microsecond)
	first, state, err := ensureLocalAgentDemoAssistantVersion(ctx, scoped, starter, manifest, policy, now)
	if err != nil || state != agentpersonastore.StateDraft {
		t.Fatalf("Assistant draft=%+v %s %v", first, state, err)
	}
	second, again, err := ensureLocalAgentDemoAssistantVersion(ctx, scoped, starter, manifest, policy, now.Add(time.Hour))
	if err != nil || again != state || second.ContentDigest != first.ContentDigest || !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("draft replay=%+v %s %v", second, again, err)
	}
	var profile agentpersona.PersonaProfile
	if json.Unmarshal(first.Profile, &profile) != nil || profile.Handle != "assistant" || profile.Instructions != localAgentDemoAssistantInstructions || profile.Template == nil || profile.Template.ID != starter.ID || profile.EvalLimits != policy.EvalLimits {
		t.Fatalf("Assistant template projection=%+v", profile)
	}
	if sealed, err := agentpersona.Seal(profile); err != nil || sealed.Digest != first.ContentDigest {
		t.Fatalf("Assistant draft seal=%v", err)
	}
}

func TestAgentUXGeneral_VerifierKeys(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := localAgentDemoEvaluationVerificationKeys(key, "ironridge-demo", "harborcare-demo", "production")
	if err != nil || len(keys) != 4 {
		t.Fatalf("local verifier keys=%d err=%v", len(keys), err)
	}
	for _, tenant := range []string{"ironridge-demo", "harborcare-demo"} {
		for _, suite := range []agenteval.PersonaSuite{agenteval.PolicyHelperSuite(personaPolicyHelperSkillID), agenteval.AssistantSuite(personaPolicyHelperSkillID, personaChatReplySkillID)} {
			id, err := PersonaEvaluationVerificationKeyID(tenant, suite.ID, localAgentDemoEvaluationKeyID)
			if err != nil || !keys[id].Equal(key.Public()) {
				t.Fatalf("missing exact verifier %s %s: %v", tenant, suite.ID, err)
			}
		}
	}
	if _, err := localAgentDemoEvaluationVerificationKeys(nil, "ironridge-demo"); err == nil {
		t.Fatal("invalid key accepted")
	}
}

func TestAgentUXGeneral_LocalRuntimeComposition(t *testing.T) {
	executor := &PersonaAdminLifecycleExecutor{}
	catalog := &PersonaAdminCatalogService{}
	wiring := &personaServeWiring{adminFactory: &PersonaAdminCommandFactory{executor: executor}, adminCatalog: readOnlyPersonaAdminCatalog{service: catalog}}
	runtime := &LocalPersonaRuntimeProvisioner{Config: LocalAgentDemoConfig{Tenant: localAgentDemoTenant}, Now: time.Now}
	if err := wiring.bindAdminRuntimeProvisioner(runtime); err != nil || executor.Runtime != runtime {
		t.Fatalf("runtime publication wiring err=%v executor=%+v", err, executor)
	}
	status, ok := catalog.Runtime.(PersonaCatalogRuntimeStatus)
	if !ok || status.Store != runtime.Personas || status.Principal.Now == nil || status.Principal.Principals == nil || status.Principal.Bindings == nil {
		t.Fatalf("stopped-placement readiness wiring=%+v", catalog.Runtime)
	}
	if status.PersonaRuntimeReady(context.Background(), localAgentDemoTenant, "assistant", 1) {
		t.Fatal("unprovisioned composition advertised a restart")
	}
	var missing *personaServeWiring
	if !errors.Is(missing.bindAdminRuntimeProvisioner(runtime), ErrPersonaAdminRuntimeUnavailable) {
		t.Fatal("missing composition accepted runtime wiring")
	}
}
