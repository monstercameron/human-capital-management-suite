package application

// The portable agent boundary is deliberately small.  It owns request
// authentication and tenant scoping while the agentportable package owns the
// authority-free wire format and its canonical validation.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentportable"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrAgentPortableUnavailable = errors.New("application: portable agent service unavailable")
	ErrAgentPortableDenied      = errors.New("application: portable agent request denied")
	ErrAgentPortableInvalid     = errors.New("application: invalid portable agent request")
)

// AgentPortableManifestSource reads one exact, tenant-scoped manifest.
type AgentPortableManifestSource interface {
	ResolveAgentManifest(context.Context, string, uint64) (agentmanifest.Manifest, error)
}

// AgentPortableInstructionSource returns the exact executable instruction
// bytes bound to one manifest version and digest.
type AgentPortableInstructionSource interface {
	ResolveAgentInstructions(context.Context, string, uint64, string) (string, error)
}

// AgentPortableDestinationRegistry is the only authority allowed to map
// references during import. Implementations must resolve against the supplied
// destination tenant and return an immutable destination reference.
type AgentPortableDestinationRegistry interface {
	MapPortableReference(context.Context, string, agentportable.ReferenceKind, agentmanifest.Reference) (agentmanifest.Reference, error)
}

// AgentPortableAuthorizer is composed from persona-admin role policy. The
// action is evaluated only after transport authentication and tenant binding.
type AgentPortableAuthorizer interface {
	AuthorizePortable(context.Context, string) error
}

// AgentPortableDraftStore durably records an inert draft. Implementations must
// preserve DRAFT state and must not publish, install, or grant it.
type AgentPortableDraftStore interface {
	SavePortableDraft(context.Context, string, string, agentportable.Draft) error
}

type AgentPortableDraftReader interface {
	GetPortableDraft(context.Context, string, string) (agentportable.Draft, error)
}

// AgentStorePortableDraftStore is the production adapter for the isolated
// agent manifest store. Imported instruction bytes must match the definition's
// digest, and the complete draft is recorded atomically without execution grants.
type AgentStorePortableDraftStore struct {
	Store      *agentstore.Store
	TenantUUID func(string) uuid.UUID
}

func (s AgentStorePortableDraftStore) SavePortableDraft(ctx context.Context, tenant, actor string, draft agentportable.Draft) error {
	if s.Store == nil || s.TenantUUID == nil || draft.State != "DRAFT" || draft.TenantID != tenant || len(draft.Manifest.ContextGrants) != 0 {
		return ErrAgentPortableUnavailable
	}
	tenantID := s.TenantUUID(tenant)
	if tenantID == uuid.Nil {
		return ErrAgentPortableDenied
	}
	p, verifiedTenant, err := portablePrincipal(ctx)
	if err != nil || verifiedTenant != tenant || p.Subject() != actor || draft.Manifest.OwnerID != actor {
		return ErrAgentPortableDenied
	}
	content := draft.Instructions
	if !portableInstructionDigestMatches(content, draft.Manifest.InstructionsDigest) {
		return fmt.Errorf("%w: destination instruction content is unavailable", ErrAgentPortableInvalid)
	}
	return s.Store.SavePortableDefinitionDraft(ctx, tenantID, actor, draft.Manifest, content)
}

func (s AgentStorePortableDraftStore) GetPortableDraft(ctx context.Context, tenant, id string) (agentportable.Draft, error) {
	if s.Store == nil || s.TenantUUID == nil {
		return agentportable.Draft{}, ErrAgentPortableUnavailable
	}
	_, verifiedTenant, err := portablePrincipal(ctx)
	if err != nil || verifiedTenant != tenant {
		return agentportable.Draft{}, ErrAgentPortableDenied
	}
	stored, err := s.Store.GetPortableDefinitionDraft(ctx, s.TenantUUID(tenant), id)
	if err != nil {
		return agentportable.Draft{}, err
	}
	return agentportable.Draft{TenantID: tenant, State: stored.State, Manifest: stored.Manifest, Instructions: stored.Instructions}, nil
}

func (s *AgentPortableService) ReadDraft(ctx context.Context, id string) (agentportable.Draft, error) {
	_, tenant, err := portablePrincipal(ctx)
	if err != nil {
		return agentportable.Draft{}, err
	}
	if s == nil || s.Authorizer == nil || s.Drafts == nil {
		return agentportable.Draft{}, ErrAgentPortableUnavailable
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(id) != id {
		return agentportable.Draft{}, ErrAgentPortableInvalid
	}
	if s.Authorizer.AuthorizePortable(ctx, "export") != nil {
		return agentportable.Draft{}, ErrAgentPortableDenied
	}
	reader, ok := s.Drafts.(AgentPortableDraftReader)
	if !ok {
		return agentportable.Draft{}, ErrAgentPortableUnavailable
	}
	draft, err := reader.GetPortableDraft(ctx, tenant, id)
	if err != nil {
		return agentportable.Draft{}, err
	}
	if draft.TenantID != tenant || draft.State != "DRAFT" || draft.Manifest.ID != id || len(draft.Manifest.ContextGrants) != 0 || draft.Manifest.Validate() != nil || !portableInstructionDigestMatches(draft.Instructions, draft.Manifest.InstructionsDigest) {
		return agentportable.Draft{}, ErrAgentPortableInvalid
	}
	return draft, nil
}

type AgentPortableService struct {
	Manifests    AgentPortableManifestSource
	Instructions AgentPortableInstructionSource
	Destinations AgentPortableDestinationRegistry
	Drafts       AgentPortableDraftStore
	Authorizer   AgentPortableAuthorizer
}

type AgentPortableExportRequest struct {
	ManifestID      string `json:"manifest_id"`
	ManifestVersion uint64 `json:"manifest_version"`
}

type AgentPortableImportRequest struct {
	Definition                 json.RawMessage                 `json:"definition"`
	DestinationManifestID      string                          `json:"destination_manifest_id"`
	DestinationManifestVersion uint64                          `json:"destination_manifest_version"`
	Mappings                   []AgentPortableReferenceMapping `json:"mappings"`
}

type AgentPortableReferenceMapping struct {
	Kind          agentportable.ReferenceKind `json:"kind"`
	SourceID      string                      `json:"source_id"`
	DestinationID string                      `json:"destination_id"`
}

// ImportMapped resolves each explicitly selected reference from an existing
// immutable destination definition in the authenticated tenant.
func (s *AgentPortableService) ImportMapped(ctx context.Context, request AgentPortableImportRequest) (agentportable.Draft, error) {
	if s == nil || s.Authorizer == nil || s.Manifests == nil || s.Drafts == nil {
		return agentportable.Draft{}, ErrAgentPortableUnavailable
	}
	if _, _, err := portablePrincipal(ctx); err != nil {
		return agentportable.Draft{}, err
	}
	if err := s.Authorizer.AuthorizePortable(ctx, "import"); err != nil {
		return agentportable.Draft{}, ErrAgentPortableDenied
	}
	definition, err := agentportable.Parse(request.Definition)
	if err != nil || definition.Instructions == "" || !portableInstructionDigestMatches(definition.Instructions, definition.InstructionsDigest) {
		return agentportable.Draft{}, ErrAgentPortableInvalid
	}
	if request.DestinationManifestID == "" || request.DestinationManifestVersion == 0 {
		return agentportable.Draft{}, ErrAgentPortableInvalid
	}
	destination, err := s.Manifests.ResolveAgentManifest(ctx, request.DestinationManifestID, request.DestinationManifestVersion)
	if err != nil || destination.ID != request.DestinationManifestID || destination.Version != request.DestinationManifestVersion || destination.Validate() != nil || destination.SchemaVersion != definition.ManifestSchema {
		return agentportable.Draft{}, ErrAgentPortableInvalid
	}
	sourceRefs := portableReferences(definition.SourceCeiling, definition.ToolCeiling, definition.ModelPolicy, definition.OutputSchema, definition.EvaluationRefs)
	destRefs := portableReferences(destination.SourceCeiling, destination.ToolCeiling, destination.ModelPolicy, destination.OutputSchema, destination.EvaluationRefs)
	mapped := make(map[agentportable.ReferenceKind]map[string]agentmanifest.Reference)
	count := 0
	for _, refs := range sourceRefs {
		count += len(refs)
	}
	if len(request.Mappings) != count {
		return agentportable.Draft{}, ErrAgentPortableInvalid
	}
	for _, selection := range request.Mappings {
		if _, ok := sourceRefs[selection.Kind][selection.SourceID]; !ok {
			return agentportable.Draft{}, ErrAgentPortableInvalid
		}
		ref, ok := destRefs[selection.Kind][selection.DestinationID]
		if !ok {
			return agentportable.Draft{}, ErrAgentPortableInvalid
		}
		if mapped[selection.Kind] == nil {
			mapped[selection.Kind] = map[string]agentmanifest.Reference{}
		}
		if _, duplicate := mapped[selection.Kind][selection.SourceID]; duplicate {
			return agentportable.Draft{}, ErrAgentPortableInvalid
		}
		mapped[selection.Kind][selection.SourceID] = ref
	}
	copyService := *s
	copyService.Destinations = ExplicitPortableDestinationRegistry{References: mapped}
	return copyService.Import(ctx, request.Definition)
}

func portableReferences(sources, tools []agentmanifest.Reference, model, output agentmanifest.Reference, evaluations []agentmanifest.Reference) map[agentportable.ReferenceKind]map[string]agentmanifest.Reference {
	groups := map[agentportable.ReferenceKind][]agentmanifest.Reference{agentportable.SourceReference: sources, agentportable.CapabilityReference: tools, agentportable.ModelPolicyReference: {model}, agentportable.OutputSchemaReference: {output}, agentportable.EvaluationReference: evaluations}
	out := map[agentportable.ReferenceKind]map[string]agentmanifest.Reference{}
	for kind, refs := range groups {
		out[kind] = map[string]agentmanifest.Reference{}
		for _, ref := range refs {
			out[kind][ref.ID] = ref
		}
	}
	return out
}

func (s *AgentPortableService) Export(ctx context.Context, req AgentPortableExportRequest) ([]byte, error) {
	p, tenant, err := portablePrincipal(ctx)
	if err != nil || p == nil || p.SubjectKind() != trust.SubjectKindHuman {
		return nil, ErrAgentPortableDenied
	}
	if s == nil || s.Authorizer == nil {
		return nil, ErrAgentPortableInvalid
	}
	if err := s.Authorizer.AuthorizePortable(ctx, "export"); err != nil {
		return nil, ErrAgentPortableDenied
	}
	if s == nil || s.Manifests == nil || s.Instructions == nil || strings.TrimSpace(req.ManifestID) == "" || req.ManifestVersion == 0 {
		return nil, ErrAgentPortableInvalid
	}
	manifest, err := s.Manifests.ResolveAgentManifest(ctx, req.ManifestID, req.ManifestVersion)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve manifest: %v", ErrAgentPortableInvalid, err)
	}
	if manifest.ID != req.ManifestID || manifest.Version != req.ManifestVersion {
		return nil, fmt.Errorf("%w: manifest identity mismatch", ErrAgentPortableInvalid)
	}
	instructions, err := s.Instructions.ResolveAgentInstructions(ctx, manifest.ID, manifest.Version, manifest.InstructionsDigest)
	if err != nil || !portableInstructionDigestMatches(instructions, manifest.InstructionsDigest) {
		return nil, fmt.Errorf("%w: instruction content does not match manifest digest", ErrAgentPortableInvalid)
	}
	_ = tenant // tenant is intentionally obtained from verified context above.
	return agentportable.ExportWithInstructions(manifest, instructions)
}

func portableInstructionDigestMatches(content, expected string) bool {
	if strings.TrimSpace(content) == "" || len(expected) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(expected, "sha256:") {
		return false
	}
	sum := sha256.Sum256([]byte(content))
	return "sha256:"+hex.EncodeToString(sum[:]) == expected
}

func (s *AgentPortableService) Import(ctx context.Context, data []byte) (agentportable.Draft, error) {
	p, tenant, err := portablePrincipal(ctx)
	if err != nil || p == nil || p.SubjectKind() != trust.SubjectKindHuman {
		return agentportable.Draft{}, ErrAgentPortableDenied
	}
	if s == nil || s.Authorizer == nil {
		return agentportable.Draft{}, ErrAgentPortableInvalid
	}
	if err := s.Authorizer.AuthorizePortable(ctx, "import"); err != nil {
		return agentportable.Draft{}, ErrAgentPortableDenied
	}
	if s == nil || s.Destinations == nil || s.Drafts == nil || len(data) == 0 {
		return agentportable.Draft{}, ErrAgentPortableInvalid
	}
	// Destination identity is generated by the application boundary. It is
	// never accepted from the portable payload.
	agentID := "portable:" + uuid.NewString()
	request := agentportable.ImportRequest{TenantID: tenant, AgentID: agentID, OwnerID: p.Subject()}
	draft, err := agentportable.Import(data, request, func(kind agentportable.ReferenceKind, ref agentmanifest.Reference) (agentmanifest.Reference, error) {
		return s.Destinations.MapPortableReference(ctx, tenant, kind, ref)
	})
	if err != nil {
		return agentportable.Draft{}, err
	}
	if err := s.Drafts.SavePortableDraft(ctx, tenant, p.Subject(), draft); err != nil {
		return agentportable.Draft{}, fmt.Errorf("%w: persist draft: %v", ErrAgentPortableInvalid, err)
	}
	return draft, nil
}

func portablePrincipal(ctx context.Context) (*trust.Principal, string, error) {
	if ctx == nil {
		return nil, "", ErrAgentPortableDenied
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || p.Tenant().Validate() != nil || strings.TrimSpace(p.Subject()) == "" || strings.TrimSpace(p.Subject()) != p.Subject() {
		return nil, "", ErrAgentPortableDenied
	}
	return p, string(p.Tenant()), nil
}
