package application

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func configuredPolicyDeploymentFixture(t *testing.T) (PersonaModelDeployment, uuid.UUID) {
	t.Helper()
	cfg := modelDeploymentFixture(t)
	tenant := uuid.New()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{4}, ed25519.SeedSize))
	content := []byte(`{"id":"test-policy","version":1,"schema_version":1}`)
	r := agentmodelpolicystore.Record{Kind: agentmodelpolicystore.ModelPolicy, Reference: agentmanifest.Reference{ID: "test-policy", Version: 1, SchemaVersion: 1, Digest: agentmodelpolicystore.ContentDigest(content)}, Content: content}
	deploymentDigest := agentmodelpolicystore.ContentDigest([]byte("test-independent-deployment"))
	doc := AgentPolicyAuthorityDocument{TenantID: "tenant-a", TenantUUID: tenant.String(), Kind: r.Kind, Reference: r.Reference, ContentDigest: agentmodelpolicystore.ContentDigest(content), DeploymentDigest: deploymentDigest, SourceID: "test-reviewed-source", SourceRevision: 1, KeyID: "test-policy-key", Basis: "reviewed-deployment", ReviewRef: "test-review", EffectiveFrom: time.Now().UTC(), EffectiveUntil: time.Now().UTC().Add(time.Hour)}
	authority, err := SignAgentPolicyAuthority(doc, private)
	if err != nil {
		t.Fatal(err)
	}
	cfg.PolicySources = []PersonaModelPolicySourceConfig{{KeyID: doc.KeyID, PublicKey: base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey)), DeploymentDigest: deploymentDigest, Tenants: []string{doc.TenantID}}}
	cfg.PolicyRegistrations = []PersonaModelPolicyRegistrationConfig{{Record: r, Authority: authority}}
	return cfg, tenant
}

func TestTodo_AGENT_021_ModelDeploymentConfiguredPolicyAuthority(t *testing.T) {
	cfg, tenant := configuredPolicyDeploymentFixture(t)
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParsePersonaModelDeployment(raw)
	if err != nil {
		t.Fatal(err)
	}
	revision := uint64(1)
	state := AgentPolicySourceStateFunc(func(context.Context, values.TenantId, string) (uint64, bool, error) { return revision, false, nil })
	sources, err := NewConfiguredPersonaModelPolicySources(parsed, func(values.TenantId) uuid.UUID { return tenant }, state)
	if err != nil {
		t.Fatal(err)
	}
	r, a := parsed.PolicyRegistrations[0].Record, parsed.PolicyRegistrations[0].Authority
	if err := sources.VerifyPolicyPublication(context.Background(), nil, tenant, r, a); err != nil {
		t.Fatalf("configured authority failed verification: %v", err)
	}
	revision = 2
	if err := sources.VerifyPolicyPublication(context.Background(), nil, tenant, r, a); !errors.Is(err, ErrAgentModelPolicyUnavailable) {
		t.Fatalf("source revision withdrawal was ignored: %v", err)
	}
	if _, err := NewConfiguredPersonaModelPolicySources(parsed, func(values.TenantId) uuid.UUID { return uuid.New() }, state); !errors.Is(err, ErrPersonaModelConfiguration) {
		t.Fatalf("tenant mapping mismatch accepted: %v", err)
	}
}

func TestTodo_AGENT_021_ModelDeploymentConfiguredPolicyAuthority_Security(t *testing.T) {
	for _, mutation := range []struct {
		name   string
		change func(*PersonaModelDeployment)
	}{
		{"self-supplied pricing key", func(c *PersonaModelDeployment) { c.PolicySources[0].PublicKey = c.PricingPublicKey }},
		{"source tenant mismatch", func(c *PersonaModelDeployment) { c.PolicySources[0].Tenants = []string{"tenant-b"} }},
		{"changed content", func(c *PersonaModelDeployment) { c.PolicyRegistrations[0].Record.Content = []byte(`{"id":"forged"}`) }},
		{"signature mismatch", func(c *PersonaModelDeployment) { c.PolicyRegistrations[0].Authority.Signature[0] ^= 1 }},
		{"duplicate contract", func(c *PersonaModelDeployment) {
			c.PolicyRegistrations = append(c.PolicyRegistrations, c.PolicyRegistrations[0])
		}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			cfg, _ := configuredPolicyDeploymentFixture(t)
			mutation.change(&cfg)
			if err := validateConfiguredPersonaModelPolicies(cfg); !errors.Is(err, ErrPersonaModelConfiguration) {
				t.Fatalf("malformed policy authority accepted: %v", err)
			}
		})
	}
}
