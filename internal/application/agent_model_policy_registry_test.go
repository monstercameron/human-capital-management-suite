package application

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENT_021_ImmutablePolicySignedSource(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	tenantID := uuid.New()
	mapper := func(tenant values.TenantId) uuid.UUID {
		if tenant == "harborcare-demo" {
			return tenantID
		}
		return uuid.Nil
	}
	state := &agentPolicySourceStateFixture{revision: 1}
	sources, err := NewLocalPersonaOpenAIPolicySources(key.Public().(ed25519.PublicKey), mapper, state)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	for _, record := range LocalPersonaOpenAIPolicyRecords() {
		if err := agentmodelpolicystore.ValidateRecord(record); err != nil {
			t.Fatalf("shipped contract %s: %v", record.Kind, err)
		}
		doc, err := LocalPersonaOpenAIPolicyAuthorityDocument("harborcare-demo", tenantID, record, LocalPersonaOpenAIPolicySourceID, LocalPersonaOpenAIPolicyKeyID, 1, now, now.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		authority, err := SignAgentPolicyAuthority(doc, key)
		if err != nil {
			t.Fatal(err)
		}
		if err := sources.VerifyPolicyPublication(context.Background(), nil, tenantID, record, authority); err != nil {
			t.Fatalf("signed shipped contract %s: %v", record.Kind, err)
		}
		for _, tc := range []struct {
			name   string
			mutate func(*AgentPolicyAuthorityDocument)
		}{{"forged review", func(d *AgentPolicyAuthorityDocument) { d.ReviewRef = "made-up-review" }}, {"production basis", func(d *AgentPolicyAuthorityDocument) { d.Basis = "reviewed-deployment" }}, {"outside demo", func(d *AgentPolicyAuthorityDocument) { d.TenantID = "production" }}, {"substituted reference", func(d *AgentPolicyAuthorityDocument) { d.Reference.Version++ }}, {"different deployment", func(d *AgentPolicyAuthorityDocument) { d.DeploymentDigest = "other" }}, {"different content", func(d *AgentPolicyAuthorityDocument) { d.ContentDigest = "other" }}} {
			changed := doc
			tc.mutate(&changed)
			signed, _ := SignAgentPolicyAuthority(changed, key)
			if err := sources.VerifyPolicyPublication(context.Background(), nil, tenantID, record, signed); err == nil {
				t.Fatalf("%s source accepted", tc.name)
			}
		}
		forged := authority
		forged.Signature = append([]byte(nil), authority.Signature...)
		forged.Signature[0] ^= 1
		if err := sources.VerifyPolicyPublication(context.Background(), nil, tenantID, record, forged); err == nil {
			t.Fatal("forged signature accepted")
		}
		state.revoked = true
		if err := sources.verify(context.Background(), tenantID, record, authority); err == nil {
			t.Fatal("current source revocation ignored")
		}
		state.revoked = false
		state.revision = 2
		if err := sources.verify(context.Background(), tenantID, record, authority); err == nil {
			t.Fatal("stale reviewed source reused")
		}
		state.revision = 1
	}
	selection := LocalPersonaOpenAIPolicySelection()
	if selection.ModelPolicy != LocalPersonaOpenAIPolicyRecords()[0].Reference || selection.OutputSchema.Digest != PersonaChatReplySchemaDigest || len(selection.EvaluationSuites) != 1 {
		t.Fatalf("reference selection differs=%+v", selection)
	}
	if _, err := NewAgentModelPolicyRegistry(AgentModelPolicyRegistryConfig{}); !errors.Is(err, ErrAgentModelPolicyUnavailable) {
		t.Fatalf("missing dependencies=%v", err)
	}
	if _, err := LocalPersonaOpenAIPolicyAuthorityDocument("production", tenantID, LocalPersonaOpenAIPolicyRecords()[0], "source", "key", 1, now, now.Add(time.Hour)); !errors.Is(err, ErrAgentModelPolicyUnavailable) {
		t.Fatalf("production local document=%v", err)
	}
	if _, err := SignAgentPolicyAuthority(AgentPolicyAuthorityDocument{}, nil); !errors.Is(err, ErrAgentModelPolicyUnavailable) {
		t.Fatalf("missing signing key=%v", err)
	}
}

func TestTodo_AGENT_021_ImmutablePolicyReviewedSource(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	tenantID := uuid.New()
	record := LocalPersonaOpenAIPolicyRecords()[0]
	sources := &AgentModelPolicySources{Pins: map[string]AgentPolicySourcePin{"reviewed-key": {PublicKey: key.Public().(ed25519.PublicKey), DeploymentDigest: "signed-config-digest", Tenants: []values.TenantId{"tenant-prod"}}}, State: &agentPolicySourceStateFixture{revision: 7}, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }}
	now := time.Now().UTC()
	doc := AgentPolicyAuthorityDocument{TenantID: "tenant-prod", TenantUUID: tenantID.String(), Kind: record.Kind, Reference: record.Reference, ContentDigest: agentmodelpolicystore.ContentDigest(record.Content), DeploymentDigest: "signed-config-digest", SourceID: "configured-governance-source", SourceRevision: 7, KeyID: "reviewed-key", Basis: "reviewed-deployment", ReviewRef: "review:deployment-7", EffectiveFrom: now, EffectiveUntil: now.Add(time.Hour)}
	authority, _ := SignAgentPolicyAuthority(doc, key)
	if err := sources.verify(context.Background(), tenantID, record, authority); err != nil {
		t.Fatal(err)
	}
	doc.ReviewRef = ""
	unsignedReview, _ := SignAgentPolicyAuthority(doc, key)
	if err := sources.verify(context.Background(), tenantID, record, unsignedReview); err == nil {
		t.Fatal("production publication without review accepted")
	}
	var document map[string]any
	_ = json.Unmarshal(authority.Document, &document)
	document["unreviewed_authority"] = "all"
	authority.Document, _ = json.Marshal(document)
	authority.Signature = ed25519.Sign(key, append([]byte(agentPolicyAuthorityDomain), authority.Document...))
	if err := sources.verify(context.Background(), tenantID, record, authority); err == nil {
		t.Fatal("unknown signed authority field accepted")
	}
}

type agentPolicySourceStateFixture struct {
	revision uint64
	revoked  bool
}

func (s *agentPolicySourceStateFixture) CurrentAgentPolicySource(context.Context, values.TenantId, string) (uint64, bool, error) {
	return s.revision, s.revoked, nil
}
