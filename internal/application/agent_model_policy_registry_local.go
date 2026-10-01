package application

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const LocalPersonaOpenAIPolicySourceID = "local-dev:user-authorized-openai:synthetic-demo"
const LocalPersonaOpenAIPolicyKeyID = "local-persona-policy-v1"

type AgentPolicySourceStateFunc func(context.Context, values.TenantId, string) (uint64, bool, error)

func (f AgentPolicySourceStateFunc) CurrentAgentPolicySource(ctx context.Context, tenant values.TenantId, source string) (uint64, bool, error) {
	return f(ctx, tenant, source)
}

func NewLocalPersonaOpenAIPolicySources(key ed25519.PublicKey, tenantUUID func(values.TenantId) uuid.UUID, state AgentPolicySourceState) (*AgentModelPolicySources, error) {
	if len(key) != ed25519.PublicKeySize || tenantUUID == nil || state == nil {
		return nil, ErrAgentModelPolicyUnavailable
	}
	ref, _ := LocalPersonaOpenAIModelPolicyReference()
	return &AgentModelPolicySources{Pins: map[string]AgentPolicySourcePin{LocalPersonaOpenAIPolicyKeyID: {PublicKey: append(ed25519.PublicKey(nil), key...), DeploymentDigest: ref.Digest, Tenants: []values.TenantId{"harborcare-demo", "ironridge-demo"}, LocalUserInstruction: true}}, State: state, TenantUUID: tenantUUID}, nil
}

// EnsureLocalPersonaOpenAIPolicyRecords is called by the selected local
// bootstrap authority before manifest provisioning. Existing exact active
// registrations survive restarts; a revoked registration is never replaced.
func EnsureLocalPersonaOpenAIPolicyRecords(ctx context.Context, registry *AgentModelPolicyRegistry, publisher *agentmodelpolicystore.Publisher, tenant values.TenantId, key ed25519.PrivateKey, sourceRevision uint64, from, until time.Time) error {
	if registry == nil || publisher == nil || !localPersonaOpenAIDemoTenant(string(tenant)) {
		return ErrAgentModelPolicyUnavailable
	}
	for _, record := range LocalPersonaOpenAIPolicyRecords() {
		_, resolutionErr := registry.resolve(ctx, tenant, record.Kind, record.Reference)
		if resolutionErr == nil {
			continue
		}
		doc, err := LocalPersonaOpenAIPolicyAuthorityDocument(tenant, registry.cfg.TenantUUID(tenant), record, LocalPersonaOpenAIPolicySourceID, LocalPersonaOpenAIPolicyKeyID, sourceRevision, from, until)
		if err != nil {
			return err
		}
		authority, err := SignAgentPolicyAuthority(doc, key)
		if err != nil {
			return err
		}
		if err := publisher.Publish(ctx, registry.cfg.TenantUUID(tenant), record, authority); err != nil {
			return fmt.Errorf("local contract %s %s v%d: current resolution: %v; publication: %w", record.Kind, record.Reference.ID, record.Reference.Version, resolutionErr, err)
		}
		if _, err := registry.resolve(ctx, tenant, record.Kind, record.Reference); err != nil {
			return err
		}
	}
	return nil
}

// LocalPersonaOpenAIPolicyRecords are the exact shipped contracts. Generating
// these bytes neither publishes them nor grants production provider access.
func LocalPersonaOpenAIPolicyRecords() []agentmodelpolicystore.Record {
	model, raw := LocalPersonaOpenAIModelPolicyReference()
	suite := agenteval.PolicyHelperSuite(personaPolicyHelperSkillID)
	suiteRaw, _ := json.Marshal(suite)
	return []agentmodelpolicystore.Record{
		{Kind: agentmodelpolicystore.ModelPolicy, Reference: model, Content: raw},
		{Kind: agentmodelpolicystore.OutputSchema, Reference: agentmanifest.Reference{ID: PersonaChatReplySchema, Version: 1, SchemaVersion: 1, Digest: PersonaChatReplySchemaDigest}, Content: []byte(PersonaChatReplyJSONSchema)},
		{Kind: agentmodelpolicystore.EvaluationSuite, Reference: agentmanifest.Reference{ID: suite.ID, Version: 1, SchemaVersion: 1, Digest: agenteval.PersonaSuiteDigest(suite)}, Content: suiteRaw},
	}
}

func LocalPersonaOpenAIPolicySelection() AgentPolicyReferenceSelection {
	records := LocalPersonaOpenAIPolicyRecords()
	return AgentPolicyReferenceSelection{ModelPolicy: records[0].Reference, OutputSchema: records[1].Reference, EvaluationSuites: map[string]agentmanifest.Reference{records[2].Reference.ID: records[2].Reference}}
}

// SignAgentPolicyAuthority signs a deployment-owned exact publication source.
// The configured verifier still checks reviewer basis, source revision and
// current revocation state before the restricted publisher can persist it.
func SignAgentPolicyAuthority(doc AgentPolicyAuthorityDocument, key ed25519.PrivateKey) (agentmodelpolicystore.Authority, error) {
	if len(key) != ed25519.PrivateKeySize {
		return agentmodelpolicystore.Authority{}, ErrAgentModelPolicyUnavailable
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return agentmodelpolicystore.Authority{}, err
	}
	return agentmodelpolicystore.Authority{SourceID: doc.SourceID, SourceRevision: doc.SourceRevision, KeyID: doc.KeyID, Document: raw, Signature: ed25519.Sign(key, append([]byte(agentPolicyAuthorityDomain), raw...)), EffectiveFrom: doc.EffectiveFrom, EffectiveUntil: doc.EffectiveUntil, Revoked: doc.Revoked}, nil
}

// LocalPersonaOpenAIPolicyAuthorityDocument records the actual local user
// instruction for synthetic demo data. It cannot claim a review identity.
func LocalPersonaOpenAIPolicyAuthorityDocument(tenant values.TenantId, tenantID uuid.UUID, r agentmodelpolicystore.Record, sourceID, keyID string, revision uint64, from, until time.Time) (AgentPolicyAuthorityDocument, error) {
	if !localPersonaOpenAIDemoTenant(string(tenant)) || tenantID == uuid.Nil || !localAgentPolicyRecord(r) {
		return AgentPolicyAuthorityDocument{}, ErrAgentModelPolicyUnavailable
	}
	policy, _ := LocalPersonaOpenAIModelPolicyReference()
	return AgentPolicyAuthorityDocument{TenantID: string(tenant), TenantUUID: tenantID.String(), Kind: r.Kind, Reference: r.Reference, ContentDigest: agentmodelpolicystore.ContentDigest(r.Content), DeploymentDigest: policy.Digest, SourceID: sourceID, SourceRevision: revision, KeyID: keyID, Basis: "local-dev:user-authorized-openai:synthetic-demo", EffectiveFrom: from.UTC(), EffectiveUntil: until.UTC()}, nil
}
