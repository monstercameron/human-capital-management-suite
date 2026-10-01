package application

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

func modelDeploymentFixture(t *testing.T) PersonaModelDeployment {
	t.Helper()
	identity := agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "test-model", Version: "test-version"}
	profile := agentmodel.ModelProfile{ID: "openai.profile", Identity: identity, Regions: []string{"us"}, DataClasses: []string{string(trustdlp.ClassPublic)}, TaskProfileIDs: []string{"reply"}, MaxLatency: time.Second, MaxCostMicros: 1000, ExpectedCostMicros: 100, SemanticsDigest: "test-semantics", OutputSchemaDigest: PersonaChatReplySchemaDigest, ToolSchemaDigest: "test-tools", Evaluation: agentmodel.ModelEvaluation{AgentVersionDigest: "test-agent-digest", SuiteDigest: "test-suite", Passed: true}}
	profile.ProfileDigest = agentmodel.ModelProfileDigest(profile)
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{3}, ed25519.SeedSize))
	pricing := agentmodel.PricingSchedule{Version: "test-v1", Authority: "test-admin", Entries: []agentmodel.PricingEntry{{Identity: identity, InputMicrosPerToken: 2, OutputMicrosPerToken: 8}}}
	pricing.Digest = agentmodel.PricingScheduleDigest(pricing)
	pricing.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte(pricing.Digest)))
	return PersonaModelDeployment{Version: 1, Profiles: []agentmodel.ModelProfile{profile}, Terms: []agentegress.ProviderTerms{{ModelProfile: profile.ID, ProviderID: "openai", ModelID: identity.ModelID, ModelVersion: identity.Version, ContractRef: "test-contract", EgressGrantRef: "test-grant", Approved: true, Encryption: true, AllowedRegions: []string{"us"}, AllowedClasses: []trustdlp.DataClass{trustdlp.ClassPublic}, Retention: agentegress.RetentionPolicy{Mode: agentegress.RetentionNone}, TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied, SourceRules: []agentegress.ProviderSourceRule{{Class: "persona-invoking-post", Classes: []trustdlp.DataClass{trustdlp.ClassPublic}}}}}, Pricing: pricing, PricingPublicKey: base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey)),
		Destinations: []outbound.Destination{{Name: profile.ID, TrustBundleRef: "test-bundle", Purposes: []string{"persona.reply"}, DataClasses: profile.DataClasses}}, Clearances: []trustdlp.Clearance{{Destination: profile.ID, Classes: []trustdlp.DataClass{trustdlp.ClassPublic}, Decision: trustdlp.Allow}},
		Credential: PersonaModelCredentialDeployment{ID: "test-credential", Version: "v1", LeaseTTLSeconds: 60, Scopes: []OpenAIModelCredentialScope{{TenantID: "tenant-a", Region: "us", Purpose: "persona.reply", Destination: profile.ID}}},
		Worker:     PersonaModelWorkerDeployment{ID: "test-worker", Workload: "test-workload", Issuer: "test-issuer", KeyID: "test-worker-key", Cell: "test-cell", IdentityTTLSeconds: 60, RunLeaseTTLSeconds: 60}, OutputKeyID: "test-output-key"}
}

func TestTodo_AGENT_021_ModelDeploymentConfiguration(t *testing.T) {
	cfg := modelDeploymentFixture(t)
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "deployment.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPersonaModelDeployment(path)
	if err != nil || loaded.OutputKeyID != cfg.OutputKeyID {
		t.Fatalf("load = %+v, %v", loaded, err)
	}
	for _, raw := range []string{"{}", string(raw) + " {}", strings.TrimSuffix(string(raw), "}") + `,"api_key":"secret"`, strings.Repeat(" ", (1<<20)+1)} {
		if _, err := ParsePersonaModelDeployment([]byte(raw)); !errors.Is(err, ErrPersonaModelConfiguration) {
			t.Fatalf("invalid configuration accepted: %v", err)
		}
	}
}

func TestTodo_AGENT_021_ModelDeploymentConfiguration_Security(t *testing.T) {
	for _, mutation := range []struct {
		name  string
		apply func(*PersonaModelDeployment)
	}{
		{"negative public retention", func(c *PersonaModelDeployment) { c.PublicTaskRetentionSeconds = -1 }},
		{"excess public retention", func(c *PersonaModelDeployment) { c.PublicTaskRetentionSeconds = 30*24*60*60 + 1 }},
		{"unsigned pricing", func(c *PersonaModelDeployment) { c.Pricing.Signature = "unverified" }},
		{"changed pricing", func(c *PersonaModelDeployment) { c.Pricing.Entries[0].InputMicrosPerToken++ }},
		{"unreviewed model", func(c *PersonaModelDeployment) {
			c.Profiles[0].Evaluation.Passed = false
			c.Profiles[0].ProfileDigest = agentmodel.ModelProfileDigest(c.Profiles[0])
		}},
		{"missing contract", func(c *PersonaModelDeployment) { c.Terms[0].ContractRef = "" }},
		{"wildcard scope", func(c *PersonaModelDeployment) { c.Credential.Scopes[0].TenantID = "*" }},
		{"unconfigured scope", func(c *PersonaModelDeployment) { c.Credential.Scopes[0].Destination = "other-provider" }},
		{"duplicate scope", func(c *PersonaModelDeployment) {
			c.Credential.Scopes = append(c.Credential.Scopes, c.Credential.Scopes[0])
		}},
		{"identity lifetime", func(c *PersonaModelDeployment) { c.Worker.IdentityTTLSeconds = 901 }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			cfg := modelDeploymentFixture(t)
			mutation.apply(&cfg)
			raw, _ := json.Marshal(cfg)
			if _, err := ParsePersonaModelDeployment(raw); !errors.Is(err, ErrPersonaModelConfiguration) {
				t.Fatalf("invalid config accepted: %v", err)
			}
		})
	}
}

func TestTodo_AGENT_021_ModelDeploymentComposition(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	cfg := modelDeploymentFixture(t)
	seed := func(n byte) string {
		return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{n}, ed25519.SeedSize))
	}
	deps := PersonaModelDeploymentDependencies{Budget: &openAITestBudget{}, Routes: &openAITestRouteRecorder{}, Sources: openAITestSource{}, Now: func() time.Time { return now }}
	model, verifier, err := ComposePersonaRuntimeModel(cfg, "test-provider-secret", seed(1), seed(2), deps)
	if err != nil || model.Gateway == nil || model.Leases == nil || model.Recovery == nil || verifier == nil || model.RecoveryVerifier != verifier || model.Gateway.leaseBindings != model.Leases {
		t.Fatalf("composition = %+v, %v", model, err)
	}
	first, err := model.WorkerIdentity.ResolvePersonaChatWorker(context.Background())
	if err != nil || first.Subject() != cfg.Worker.ID || first.Cell() != cfg.Worker.Cell || !first.ValidAt(now) {
		t.Fatalf("identity = %v, %v", first, err)
	}
	now = now.Add(2 * time.Minute)
	second, err := model.WorkerIdentity.ResolvePersonaChatWorker(context.Background())
	if err != nil || !second.ValidAt(now) || second.Fingerprint() == first.Fingerprint() {
		t.Fatalf("identity did not renew: %v, %v", second, err)
	}
	if _, err := model.WorkerIdentity.ResolvePersonaChatWorker(nil); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, _, err := ComposePersonaRuntimeModel(cfg, "", seed(1), seed(2), deps); !errors.Is(err, ErrPersonaModelConfiguration) || !strings.Contains(err.Error(), "MODEL_API_KEY") {
		t.Fatalf("missing key = %v", err)
	}
	if _, _, err := ComposePersonaRuntimeModel(cfg, "test-provider-secret", seed(1), seed(1), deps); !errors.Is(err, ErrPersonaModelConfiguration) {
		t.Fatalf("shared signing key accepted: %v", err)
	}
	typed, typedVerifier, err := ComposePersonaRuntimeTypedModel(cfg, "test-provider-secret", seed(1), seed(2), deps)
	if err != nil || typed.Gateway == nil || typed.RecoveryVerifier != typedVerifier || typed.Gateway.leaseBindings != typed.Leases {
		t.Fatalf("typed composition authority not bound: %v", err)
	}
	for _, adapter := range typed.Gateway.adapters {
		if !adapter.Capabilities().Supports(agentmodel.FeatureStructuredJSON) {
			t.Fatal("typed gateway cannot carry the outer generator's structured JSON contract")
		}
	}
}

func TestTodo_AGENT_021_ModelDeploymentServeConfig(t *testing.T) {
	seed := func(n byte) string {
		return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{n}, ed25519.SeedSize))
	}
	config := ServeConfig{AgentModelConfigFile: "approved.json", ChatEnabled: true, AgentDatabaseURL: "agents-db", PersonaOutputSigningSeed: seed(1), PersonaWorkloadSigningSeed: seed(2)}
	if err := config.validatePersonaModelConfiguration(); err != nil {
		t.Fatal(err)
	}
	config.ChatEnabled = false
	if err := config.validatePersonaModelConfiguration(); err != nil {
		t.Fatalf("ordinary typed model configuration requires an unrelated chat listener: %v", err)
	}
	config.PersonaOutputSigningSeed = ""
	if err := config.validatePersonaModelConfiguration(); err == nil || !strings.Contains(err.Error(), EnvPersonaOutputSigningSeed) {
		t.Fatalf("missing seed = %v", err)
	}
	for _, field := range ServeConfigFields() {
		if (field.Name == FieldPersonaOutputSigningSeed || field.Name == FieldPersonaWorkloadSigningSeed) && (!field.Secret || field.Default != "") {
			t.Fatalf("unsafe seed field: %s", field.Name)
		}
	}
}

func TestTodo_AGENT_021_ModelDeploymentConfigurationValues(t *testing.T) {
	output := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, ed25519.SeedSize))
	worker := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, ed25519.SeedSize))
	env := map[string]string{EnvAgentModelConfigFile: "approved.json", EnvPersonaOutputSigningSeed: output, EnvPersonaWorkloadSigningSeed: worker}
	values, err := bootstrap.ParseConfig(nil, func(name string) (string, bool) { value, found := env[name]; return value, found }, ServeConfigFields())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ServeConfigFromValues(values)
	if err != nil || cfg.AgentModelConfigFile != "approved.json" || cfg.PersonaOutputSigningSeed != output || cfg.PersonaWorkloadSigningSeed != worker {
		t.Fatalf("deployment fields did not resolve: %v", err)
	}
	if strings.Contains(values.Effective(), output) || strings.Contains(values.Effective(), worker) {
		t.Fatal("signing seed reached effective configuration")
	}
}
