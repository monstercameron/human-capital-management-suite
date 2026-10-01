package application

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PersonaModelPolicySourceConfig is operator-pinned production public trust.
// The signed policy document cannot install its own verification key. The
// deployment digest is independently supplied and excludes signature envelopes.
type PersonaModelPolicySourceConfig struct {
	KeyID            string   `json:"key_id"`
	PublicKey        string   `json:"public_key"`
	DeploymentDigest string   `json:"deployment_digest"`
	Tenants          []string `json:"tenants"`
}

// PersonaModelPolicyRegistrationConfig contains a reviewed immutable contract
// and its existing authority envelope. Loading does not publish authority.
type PersonaModelPolicyRegistrationConfig struct {
	Record    agentmodelpolicystore.Record    `json:"record"`
	Authority agentmodelpolicystore.Authority `json:"authority"`
}

func configuredPersonaModelPolicyPins(cfg PersonaModelDeployment) (map[string]AgentPolicySourcePin, error) {
	pins := make(map[string]AgentPolicySourcePin, len(cfg.PolicySources))
	pricingKey, _ := base64.StdEncoding.Strict().DecodeString(cfg.PricingPublicKey)
	for _, source := range cfg.PolicySources {
		pub, err := base64.StdEncoding.Strict().DecodeString(source.PublicKey)
		if !canonicalOpenAIValue(source.KeyID) || err != nil || len(pub) != ed25519.PublicKeySize || bytes.Equal(pub, pricingKey) || len(source.Tenants) == 0 || len(source.DeploymentDigest) != 71 || !strings.HasPrefix(source.DeploymentDigest, "sha256:") {
			return nil, modelConfigurationError("policy source requires an independent Ed25519 public key, deployment digest and explicit tenant scope")
		}
		if _, err := hex.DecodeString(source.DeploymentDigest[7:]); err != nil {
			return nil, modelConfigurationError("policy source deployment digest is invalid")
		}
		if _, duplicate := pins[source.KeyID]; duplicate {
			return nil, modelConfigurationError("policy source key IDs must be unique")
		}
		pin := AgentPolicySourcePin{PublicKey: append(ed25519.PublicKey(nil), pub...), DeploymentDigest: source.DeploymentDigest}
		for _, tenant := range source.Tenants {
			id := values.TenantId(tenant)
			if id.Validate() != nil || slices.Contains(pin.Tenants, id) {
				return nil, modelConfigurationError("policy source tenant scope is invalid or duplicated")
			}
			pin.Tenants = append(pin.Tenants, id)
		}
		pins[source.KeyID] = pin
	}
	return pins, nil
}

func validateConfiguredPersonaModelPolicies(cfg PersonaModelDeployment) error {
	pins, err := configuredPersonaModelPolicyPins(cfg)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, registration := range cfg.PolicyRegistrations {
		r, a := registration.Record, registration.Authority
		if agentmodelpolicystore.ValidateRecord(r) != nil {
			return modelConfigurationError("registered policy contract is invalid")
		}
		pin, ok := pins[a.KeyID]
		if !ok || !ed25519.Verify(pin.PublicKey, append([]byte(agentPolicyAuthorityDomain), a.Document...), a.Signature) {
			return modelConfigurationError("policy authority signature does not verify against the configured public key")
		}
		var doc AgentPolicyAuthorityDocument
		if decodeAgentPolicyDocument(a.Document, &doc) != nil {
			return modelConfigurationError("policy authority document is invalid")
		}
		canonical, err := json.Marshal(doc)
		tenant, tenantErr := uuid.Parse(doc.TenantUUID)
		if err != nil || !bytes.Equal(canonical, a.Document) || tenantErr != nil || tenant == uuid.Nil || !slices.Contains(pin.Tenants, values.TenantId(doc.TenantID)) || doc.Kind != r.Kind || doc.Reference != r.Reference || doc.ContentDigest != agentmodelpolicystore.ContentDigest(r.Content) || doc.DeploymentDigest != pin.DeploymentDigest || !canonicalOpenAIValue(doc.SourceID) || doc.SourceID != a.SourceID || doc.SourceRevision == 0 || doc.SourceRevision != a.SourceRevision || doc.KeyID != a.KeyID || doc.Basis != "reviewed-deployment" || !canonicalOpenAIValue(doc.ReviewRef) || !doc.EffectiveFrom.Equal(a.EffectiveFrom) || !doc.EffectiveUntil.Equal(a.EffectiveUntil) || doc.EffectiveFrom.IsZero() || !doc.EffectiveUntil.After(doc.EffectiveFrom) || doc.Revoked != a.Revoked || doc.Revoked {
			return modelConfigurationError("policy authority must bind the exact reviewed contract, deployment, source revision, tenant and validity window")
		}
		key := doc.TenantUUID + "/" + r.Kind + "/" + r.Reference.ID + "/" + r.Reference.Digest
		if seen[key] {
			return modelConfigurationError("policy registration is duplicated")
		}
		seen[key] = true
	}
	return nil
}

// NewConfiguredPersonaModelPolicySources freezes operator public trust while
// requiring a real current revision/revocation owner for every runtime check.
func NewConfiguredPersonaModelPolicySources(cfg PersonaModelDeployment, tenantUUID func(values.TenantId) uuid.UUID, state AgentPolicySourceState) (*AgentModelPolicySources, error) {
	if tenantUUID == nil || isNilPersonaOutputPort(state) || len(cfg.PolicySources) == 0 {
		return nil, modelConfigurationError("configured policy trust pins and current source state are required")
	}
	if err := validateConfiguredPersonaModelPolicies(cfg); err != nil {
		return nil, err
	}
	pins, err := configuredPersonaModelPolicyPins(cfg)
	if err != nil {
		return nil, err
	}
	for _, registration := range cfg.PolicyRegistrations {
		var doc AgentPolicyAuthorityDocument
		if decodeAgentPolicyDocument(registration.Authority.Document, &doc) != nil || tenantUUID(values.TenantId(doc.TenantID)).String() != doc.TenantUUID {
			return nil, modelConfigurationError("configured policy tenant identity does not match the served tenant mapping")
		}
	}
	return &AgentModelPolicySources{Pins: pins, State: state, TenantUUID: tenantUUID}, nil
}
