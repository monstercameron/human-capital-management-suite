package application

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentportable"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// NewDatabaseAgentPortableService composes the served portable surface over
// the isolated agent store. Every source and destination lookup is tenant
// scoped, and persona-admin authorization is mandatory for both actions.
func NewDatabaseAgentPortableService(store *agentstore.Store, role PersonaAdminCommandAuthorizer, tenantUUID func(values.TenantId) uuid.UUID) (*AgentPortableService, error) {
	if store == nil || role == nil || tenantUUID == nil {
		return nil, ErrAgentPortableUnavailable
	}
	return &AgentPortableService{
		Manifests:    databasePortableManifests{store: store, tenantUUID: tenantUUID},
		Instructions: databasePortableInstructions{store: store, tenantUUID: tenantUUID},
		Drafts:       AgentStorePortableDraftStore{Store: store, TenantUUID: func(t string) uuid.UUID { return tenantUUID(values.TenantId(t)) }},
		Authorizer:   portablePersonaAdminAuthorizer{role: role},
	}, nil
}

type databasePortableManifests struct {
	store      *agentstore.Store
	tenantUUID func(values.TenantId) uuid.UUID
}

func (s databasePortableManifests) ListAgentPortableManifests(ctx context.Context) ([]agentmanifest.Manifest, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil {
		return nil, ErrAgentPortableDenied
	}
	return s.store.ListCurrentManifests(ctx, s.tenantUUID(p.Tenant()))
}

func (s databasePortableManifests) ResolveAgentManifest(ctx context.Context, id string, version uint64) (agentmanifest.Manifest, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil {
		return agentmanifest.Manifest{}, ErrAgentPortableDenied
	}
	return s.store.ManifestVersion(ctx, s.tenantUUID(p.Tenant()), id, version)
}

type databasePortableInstructions struct {
	store      *agentstore.Store
	tenantUUID func(values.TenantId) uuid.UUID
}

func (s databasePortableInstructions) ResolveAgentInstructions(ctx context.Context, id string, version uint64, digest string) (string, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil {
		return "", ErrAgentPortableDenied
	}
	return s.store.ManifestInstructions(ctx, s.tenantUUID(p.Tenant()), id, version, digest)
}

// ExplicitPortableDestinationRegistry is populated by a server-owned
// destination manifest selector. It never guesses a mapping from source IDs.
type ExplicitPortableDestinationRegistry struct {
	References map[agentportable.ReferenceKind]map[string]agentmanifest.Reference
}

func (r ExplicitPortableDestinationRegistry) MapPortableReference(_ context.Context, _ string, kind agentportable.ReferenceKind, source agentmanifest.Reference) (agentmanifest.Reference, error) {
	refs := r.References[kind]
	destination, ok := refs[source.ID]
	if !ok || destination.ID == "" || destination.Version == 0 || destination.SchemaVersion == 0 || destination.Digest == "" {
		return agentmanifest.Reference{}, fmt.Errorf("no explicit destination mapping for %s %q", kind, source.ID)
	}
	return destination, nil
}

type portablePersonaAdminAuthorizer struct{ role PersonaAdminCommandAuthorizer }

func (a portablePersonaAdminAuthorizer) AuthorizePortable(ctx context.Context, action string) error {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || !validPersonaAdminActor(p) || a.role == nil || (action != "export" && action != "import") {
		return ErrAgentPortableDenied
	}
	cmd := PersonaAdminCreateDraft
	return a.role.AuthorizePersonaAdminCommand(ctx, PersonaAdminCommandActor{Principal: p, Tenant: p.Tenant(), Subject: p.Subject()}, cmd, "portable")
}

var _ AgentPortableManifestSource = databasePortableManifests{}
var _ AgentPortableInstructionSource = databasePortableInstructions{}
