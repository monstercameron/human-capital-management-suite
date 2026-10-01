package application

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func localOpenAIProfileFixture(t *testing.T) agentmodel.ModelProfile {
	t.Helper()
	profile := modelDeploymentFixture(t).Profiles[0]
	profile.Identity = agentmodel.ModelIdentity{ProviderID: "openai", ModelID: LocalPersonaOpenAIModelID, Version: LocalPersonaOpenAIModelVersion}
	profile.Regions = []string{LocalPersonaOpenAIRegion}
	profile.DataClasses = []string{string(trustdlp.ClassPublic), string(trustdlp.ClassInternal)}
	profile.ProfileDigest = agentmodel.ModelProfileDigest(profile)
	return profile
}

func localOpenAIMaterialFixture() LocalPersonaModelSigningMaterial {
	seed := func(value byte) string {
		return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{value}, ed25519.SeedSize))
	}
	return LocalPersonaModelSigningMaterial{OutputSeed: seed(1), WorkloadSeed: seed(2), PricingSeed: seed(3)}
}

func TestTodo_AGENT_021_LocalOpenAIDeployment(t *testing.T) {
	profile := localOpenAIProfileFixture(t)
	cfg, err := NewLocalPersonaOpenAIModelDeployment("harborcare-demo", []agentmodel.ModelProfile{profile}, localOpenAIMaterialFixture())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := ParsePersonaModelDeployment(raw)
	if err != nil || loaded.Profiles[0].ProfileDigest != profile.ProfileDigest {
		t.Fatalf("signed configuration roundtrip: %v", err)
	}
	term := loaded.Terms[0]
	if term.Retention.Mode != agentegress.RetentionBounded || term.Retention.MaxAge != 30*24*time.Hour || term.TrainingUse != agentmodel.UseDenied || term.Logging != agentmodel.UseAllowed || term.HostedNetworkUse || term.HostedStateUse {
		t.Fatalf("normal API processing terms were misrepresented: %+v", term)
	}
	if len(loaded.Credential.Scopes) != 2 || loaded.Credential.Scopes[0].TenantID != "harborcare-demo" || loaded.Credential.Scopes[1].TenantID != "ironridge-demo" || loaded.Credential.Scopes[0].Region != "global" || loaded.Credential.Scopes[0].Purpose != LocalPersonaOpenAIPurpose {
		t.Fatalf("scope: %+v", loaded.Credential.Scopes)
	}
	pricing, _, err := loaded.validate()
	if err != nil {
		t.Fatal(err)
	}
	cost, err := pricing.Cost(agentmodel.ModelSelection{Identity: profile.Identity}, agentmodel.ModelUsage{InputTokens: 1000000, OutputTokens: 1000000, TotalTokens: 2000000})
	if err != nil || cost != 2250000 {
		t.Fatalf("standard rates: cost=%d error=%v", cost, err)
	}
	cost, err = pricing.Cost(agentmodel.ModelSelection{Identity: profile.Identity}, agentmodel.ModelUsage{InputTokens: 1000000, CachedInputTokens: 1000000, OutputTokens: 1000000, TotalTokens: 2000000})
	if err != nil || cost != 2025000 {
		t.Fatalf("cached input rates: cost=%d error=%v", cost, err)
	}
	profile.DataClasses[0] = string(trustdlp.ClassPHI)
	if cfg.Profiles[0].DataClasses[0] != string(trustdlp.ClassPublic) {
		t.Fatal("caller mutation changed configured profile")
	}
}

func TestTodo_AGENT_021_LocalOpenAIDeployment_Security(t *testing.T) {
	mutations := []struct {
		name  string
		apply func(*agentmodel.ModelProfile)
	}{
		{"failed evaluation", func(p *agentmodel.ModelProfile) { p.Evaluation.Passed = false }},
		{"unpinned model", func(p *agentmodel.ModelProfile) { p.Identity.Version = "gpt-5-mini" }},
		{"regional guarantee", func(p *agentmodel.ModelProfile) { p.Regions = []string{"us"} }},
		{"personal", func(p *agentmodel.ModelProfile) { p.DataClasses = []string{string(trustdlp.ClassPII)} }},
		{"health", func(p *agentmodel.ModelProfile) { p.DataClasses = []string{string(trustdlp.ClassPHI)} }},
		{"unbounded cost", func(p *agentmodel.ModelProfile) { p.MaxCostMicros = LocalPersonaOpenAIMaxCostMicros + 1 }},
		{"unbounded time", func(p *agentmodel.ModelProfile) { p.MaxLatency = LocalPersonaOpenAIMaxLatency + time.Second }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			profile := localOpenAIProfileFixture(t)
			mutation.apply(&profile)
			profile.ProfileDigest = agentmodel.ModelProfileDigest(profile)
			if _, err := NewLocalPersonaOpenAIModelDeployment("harborcare-demo", []agentmodel.ModelProfile{profile}, localOpenAIMaterialFixture()); !errors.Is(err, ErrPersonaModelConfiguration) {
				t.Fatalf("unsafe profile accepted: %v", err)
			}
		})
	}
	profile := localOpenAIProfileFixture(t)
	if _, err := NewLocalPersonaOpenAIModelDeployment("ironridge-demo", []agentmodel.ModelProfile{profile}, localOpenAIMaterialFixture()); err != nil {
		t.Fatalf("second seeded demo tenant refused: %v", err)
	}
	for _, tenant := range []string{"", "*", " tenant-a", "tenant-a", "harborcare-demo "} {
		if _, err := NewLocalPersonaOpenAIModelDeployment(tenant, []agentmodel.ModelProfile{profile}, localOpenAIMaterialFixture()); !errors.Is(err, ErrPersonaModelConfiguration) {
			t.Fatalf("unsafe scope accepted: %v", err)
		}
	}
	if _, err := NewLocalPersonaOpenAIModelDeployment("harborcare-demo", []agentmodel.ModelProfile{profile, profile}, localOpenAIMaterialFixture()); !errors.Is(err, ErrPersonaModelConfiguration) {
		t.Fatalf("duplicate destination accepted: %v", err)
	}
}
