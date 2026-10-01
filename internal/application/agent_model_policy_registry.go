package application

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentportable"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrAgentModelPolicyUnavailable = errors.New("application: current model policy authority unavailable")

// AgentPolicySourceState is the deployment-owned current source revision. A
// source revocation is checked on every resolution, including after restart.
type AgentPolicySourceState interface {
	CurrentAgentPolicySource(context.Context, values.TenantId, string) (uint64, bool, error)
}

// AgentPolicySourcePin comes from server trust configuration. Its public key,
// exact deployment digest and tenant scope cannot be chosen by a request.
type AgentPolicySourcePin struct {
	PublicKey            ed25519.PublicKey
	DeploymentDigest     string
	Tenants              []values.TenantId
	LocalUserInstruction bool
}

type AgentPolicyAuthorityDocument struct {
	TenantID         string                  `json:"tenant_id"`
	TenantUUID       string                  `json:"tenant_uuid"`
	Kind             string                  `json:"kind"`
	Reference        agentmanifest.Reference `json:"reference"`
	ContentDigest    string                  `json:"content_digest"`
	DeploymentDigest string                  `json:"deployment_digest"`
	SourceID         string                  `json:"source_id"`
	SourceRevision   uint64                  `json:"source_revision"`
	KeyID            string                  `json:"key_id"`
	Basis            string                  `json:"basis"`
	ReviewRef        string                  `json:"review_ref,omitempty"`
	EffectiveFrom    time.Time               `json:"effective_from"`
	EffectiveUntil   time.Time               `json:"effective_until"`
	Revoked          bool                    `json:"revoked"`
}

const agentPolicyAuthorityDomain = "hcm-next-agent-policy-authority/v1\x00"

// AgentModelPolicySources verifies configured reviewed sources. Local sources
// have an explicit user-instruction basis and the exact synthetic demo scope;
// they never claim a production reviewer or commercial processing agreement.
type AgentModelPolicySources struct {
	Pins       map[string]AgentPolicySourcePin
	State      AgentPolicySourceState
	TenantUUID func(values.TenantId) uuid.UUID
}

func (s *AgentModelPolicySources) VerifyPolicyPublication(ctx context.Context, _ dbport.Tx, tenant uuid.UUID, r agentmodelpolicystore.Record, a agentmodelpolicystore.Authority) error {
	return s.verify(ctx, tenant, r, a)
}

func (s *AgentModelPolicySources) verify(ctx context.Context, tenant uuid.UUID, r agentmodelpolicystore.Record, a agentmodelpolicystore.Authority) error {
	if s == nil || s.State == nil || s.TenantUUID == nil || ctx == nil {
		return ErrAgentModelPolicyUnavailable
	}
	if err := agentmodelpolicystore.ValidateRecord(r); err != nil {
		return err
	}
	pin, ok := s.Pins[a.KeyID]
	if !ok || len(pin.PublicKey) != ed25519.PublicKeySize || pin.DeploymentDigest == "" || !ed25519.Verify(pin.PublicKey, append([]byte(agentPolicyAuthorityDomain), a.Document...), a.Signature) {
		return ErrAgentModelPolicyUnavailable
	}
	var doc AgentPolicyAuthorityDocument
	if err := decodeAgentPolicyDocument(a.Document, &doc); err != nil {
		return err
	}
	canonical, err := json.Marshal(doc)
	if err != nil || string(canonical) != string(a.Document) {
		return ErrAgentModelPolicyUnavailable
	}
	tenantName := values.TenantId(doc.TenantID)
	if tenantName.Validate() != nil || !slices.Contains(pin.Tenants, tenantName) || s.TenantUUID(tenantName) != tenant || doc.TenantUUID != tenant.String() || doc.Kind != r.Kind || doc.Reference != r.Reference || doc.ContentDigest != agentmodelpolicystore.ContentDigest(r.Content) || doc.DeploymentDigest != pin.DeploymentDigest || doc.SourceID != a.SourceID || doc.SourceRevision != a.SourceRevision || doc.KeyID != a.KeyID || !doc.EffectiveFrom.Equal(a.EffectiveFrom) || !doc.EffectiveUntil.Equal(a.EffectiveUntil) || doc.Revoked != a.Revoked {
		return ErrAgentModelPolicyUnavailable
	}
	if pin.LocalUserInstruction {
		if !localPersonaOpenAIDemoTenant(doc.TenantID) || doc.Basis != "local-dev:user-authorized-openai:synthetic-demo" || doc.ReviewRef != "" || !localAgentPolicyRecord(r) {
			return ErrAgentModelPolicyUnavailable
		}
	} else if doc.Basis != "reviewed-deployment" || doc.ReviewRef == "" {
		return ErrAgentModelPolicyUnavailable
	}
	revision, revoked, err := s.State.CurrentAgentPolicySource(ctx, tenantName, a.SourceID)
	if err != nil || revoked || revision != a.SourceRevision {
		return ErrAgentModelPolicyUnavailable
	}
	return nil
}

func decodeAgentPolicyDocument(raw []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return ErrAgentModelPolicyUnavailable
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrAgentModelPolicyUnavailable
	}
	return nil
}

func localAgentPolicyRecord(r agentmodelpolicystore.Record) bool {
	for _, expected := range LocalPersonaOpenAIPolicyRecords() {
		if expected.Kind == r.Kind && expected.Reference == r.Reference && string(expected.Content) == string(r.Content) {
			return true
		}
	}
	return false
}

// AgentModelPolicyDeploymentAuthority resolves current configured provider
// eligibility for the admitted agent, installation, source and processing
// ceiling. A stored immutable reference alone never grants provider access.
type AgentModelPolicyDeploymentAuthority interface {
	CheckAgentPolicyDeployment(context.Context, agentrun.Request, agentmodelpolicystore.Current) error
}

type AgentModelPolicyRegistryConfig struct {
	Store             *agentmodelpolicystore.Store
	Sources           *AgentModelPolicySources
	TenantUUID        func(values.TenantId) uuid.UUID
	Now               func() time.Time
	CurrentDeployment AgentModelPolicyDeploymentAuthority
	Manifests         AgentManifestVersionStore
}

type AgentModelPolicyRegistry struct {
	cfg AgentModelPolicyRegistryConfig
}

func NewAgentModelPolicyRegistry(cfg AgentModelPolicyRegistryConfig) (*AgentModelPolicyRegistry, error) {
	if cfg.Store == nil || cfg.Sources == nil || cfg.TenantUUID == nil || cfg.Now == nil {
		return nil, ErrAgentModelPolicyUnavailable
	}
	copySources := *cfg.Sources
	copySources.Pins = make(map[string]AgentPolicySourcePin, len(cfg.Sources.Pins))
	for id, pin := range cfg.Sources.Pins {
		pin.PublicKey = slices.Clone(pin.PublicKey)
		pin.Tenants = slices.Clone(pin.Tenants)
		copySources.Pins[id] = pin
	}
	cfg.Sources = &copySources
	return &AgentModelPolicyRegistry{cfg: cfg}, nil
}

func (r *AgentModelPolicyRegistry) resolve(ctx context.Context, tenant values.TenantId, kind string, ref agentmanifest.Reference) (agentmodelpolicystore.Current, error) {
	if r == nil || ctx == nil || tenant.Validate() != nil {
		return agentmodelpolicystore.Current{}, ErrAgentModelPolicyUnavailable
	}
	tenantID := r.cfg.TenantUUID(tenant)
	current, err := r.cfg.Store.Resolve(ctx, tenantID, kind, ref, r.cfg.Now().UTC())
	if err != nil {
		return agentmodelpolicystore.Current{}, err
	}
	if err := r.cfg.Sources.verify(ctx, tenantID, current.Record, current.Authority); err != nil {
		return agentmodelpolicystore.Current{}, err
	}
	return current, nil
}

// ResolveTypedReference is the tenant-bound immutable reference read used by
// manifest validation and portable destination composition.
func (r *AgentModelPolicyRegistry) ResolveTypedReference(ctx context.Context, tenant values.TenantId, kind agentportable.ReferenceKind, ref agentmanifest.Reference) ([]byte, error) {
	current, err := r.resolve(ctx, tenant, string(kind), ref)
	if err != nil {
		return nil, err
	}
	return slices.Clone(current.Record.Content), nil
}

func (r *AgentModelPolicyRegistry) CheckCurrentModelPolicy(ctx context.Context, request agentrun.Request, ref agentmanifest.Reference) error {
	if r == nil || r.cfg.CurrentDeployment == nil || r.cfg.Manifests == nil {
		return ErrAgentModelPolicyUnavailable
	}
	current, err := r.resolve(ctx, values.TenantId(request.Source.TenantID), agentmodelpolicystore.ModelPolicy, ref)
	if err != nil {
		return err
	}
	if request.Deadline.After(current.Authority.EffectiveUntil) {
		return ErrAgentModelPolicyUnavailable
	}
	version, err := strconv.ParseUint(request.Agent.Version, 10, 64)
	if err != nil || version == 0 {
		return ErrAgentModelPolicyUnavailable
	}
	manifest, err := r.cfg.Manifests.ManifestVersion(ctx, r.cfg.TenantUUID(values.TenantId(request.Source.TenantID)), request.Agent.AgentID, version)
	if err != nil {
		return err
	}
	digest, err := manifest.Digest()
	if err != nil || digest != request.Agent.Digest || manifest.ModelPolicy != ref {
		return ErrAgentModelPolicyUnavailable
	}
	if _, err := r.resolve(ctx, values.TenantId(request.Source.TenantID), agentmodelpolicystore.OutputSchema, manifest.OutputSchema); err != nil {
		return err
	}
	for _, evaluation := range manifest.EvaluationRefs {
		if _, err := r.resolve(ctx, values.TenantId(request.Source.TenantID), agentmodelpolicystore.EvaluationSuite, evaluation); err != nil {
			return err
		}
	}
	if bundled, ok := r.cfg.CurrentDeployment.(interface {
		CheckAgentPolicyDeploymentForManifest(context.Context, agentrun.Request, agentmodelpolicystore.Current, agentmanifest.Manifest) error
	}); ok {
		return bundled.CheckAgentPolicyDeploymentForManifest(ctx, request, current, manifest)
	}
	return r.cfg.CurrentDeployment.CheckAgentPolicyDeployment(ctx, request, current)
}

type AgentPolicyReferenceSelection struct {
	ModelPolicy      agentmanifest.Reference
	OutputSchema     agentmanifest.Reference
	EvaluationSuites map[string]agentmanifest.Reference
}

// ForTenant binds a server-selected reference set to one tenant. Every getter
// reads current immutable authority rather than returning the selected pin.
func (r *AgentModelPolicyRegistry) ForTenant(tenant values.TenantId, selection AgentPolicyReferenceSelection) LocalDevPersonaStarterReferenceResolver {
	return agentPolicyStarterReferences{registry: r, tenant: tenant, selection: selection}
}

// ForPrincipal resolves the authenticated human's exact tenant on each call.
// It is used by a shared bootstrap surface that serves both seeded demos.
func (r *AgentModelPolicyRegistry) ForPrincipal(selection AgentPolicyReferenceSelection) LocalDevPersonaStarterReferenceResolver {
	return agentPolicyStarterReferences{registry: r, selection: selection}
}

type agentPolicyStarterReferences struct {
	registry  *AgentModelPolicyRegistry
	tenant    values.TenantId
	selection AgentPolicyReferenceSelection
}

func (s agentPolicyStarterReferences) checked(ctx context.Context, kind string, ref agentmanifest.Reference) (agentmanifest.Reference, error) {
	if ctx == nil {
		return agentmanifest.Reference{}, ErrAgentModelPolicyUnavailable
	}
	tenant := s.tenant
	if tenant == "" {
		p, ok := trust.FromContext(ctx)
		if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman {
			return agentmanifest.Reference{}, ErrAgentModelPolicyUnavailable
		}
		tenant = p.Tenant()
	}
	current, err := s.registry.resolve(ctx, tenant, kind, ref)
	if err != nil {
		return agentmanifest.Reference{}, err
	}
	return current.Record.Reference, nil
}
func (s agentPolicyStarterReferences) ResolveModelPolicy(ctx context.Context) (agentmanifest.Reference, error) {
	return s.checked(ctx, agentmodelpolicystore.ModelPolicy, s.selection.ModelPolicy)
}
func (s agentPolicyStarterReferences) ResolveOutputSchema(ctx context.Context) (agentmanifest.Reference, error) {
	return s.checked(ctx, agentmodelpolicystore.OutputSchema, s.selection.OutputSchema)
}
func (s agentPolicyStarterReferences) ResolveEvaluationSuite(ctx context.Context, id string) (agentmanifest.Reference, error) {
	ref, ok := s.selection.EvaluationSuites[id]
	if !ok {
		return agentmanifest.Reference{}, fmt.Errorf("%w: suite %s", ErrAgentModelPolicyUnavailable, id)
	}
	return s.checked(ctx, agentmodelpolicystore.EvaluationSuite, ref)
}
