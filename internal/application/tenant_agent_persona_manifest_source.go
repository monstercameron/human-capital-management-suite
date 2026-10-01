package application

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TenantAgentPersonaManifestSource binds the shared agent manifest store to
// the authenticated tenant for starter and profile validation.
type TenantAgentPersonaManifestSource struct {
	Store      AgentManifestVersionStore
	TenantUUID func(values.TenantId) uuid.UUID
}

// NewTenantAgentPersonaManifestSource creates a fail-closed tenant manifest
// source from the existing isolated agent store and canonical tenant mapper.
func NewTenantAgentPersonaManifestSource(store AgentManifestVersionStore, tenantUUID func(values.TenantId) uuid.UUID) (*TenantAgentPersonaManifestSource, error) {
	if store == nil || tenantUUID == nil {
		return nil, ErrAgentManifestUnavailable
	}
	return &TenantAgentPersonaManifestSource{Store: store, TenantUUID: tenantUUID}, nil
}

// ForTenant returns a manifest resolver fixed to the verified tenant.
func (s *TenantAgentPersonaManifestSource) ForTenant(ctx context.Context, tenant values.TenantId) (PersonaStarterManifestResolver, error) {
	principal, ok := trust.FromContext(ctx)
	if s == nil || s.Store == nil || s.TenantUUID == nil || !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || tenant.Validate() != nil || principal.Tenant() != tenant {
		return nil, ErrAgentManifestUnavailable
	}
	id := s.TenantUUID(tenant)
	if id == uuid.Nil {
		return nil, ErrAgentManifestUnavailable
	}
	return tenantAgentPersonaManifestAdapter{store: s.Store, tenantID: id}, nil
}

// CompatibilityForTenant returns the exact manifest compatibility validator
// suitable for agentpersona.Validator construction in a tenant builder source.
func (s *TenantAgentPersonaManifestSource) CompatibilityForTenant(ctx context.Context, tenant values.TenantId) (agentpersona.ManifestCompatibility, error) {
	resolver, err := s.ForTenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	adapter, ok := resolver.(tenantAgentPersonaManifestAdapter)
	if !ok {
		return nil, ErrAgentManifestUnavailable
	}
	return agentpersona.ManifestCompatibilityAdapter{Resolver: adapter}, nil
}

type tenantAgentPersonaManifestAdapter struct {
	store    AgentManifestVersionStore
	tenantID uuid.UUID
}

func (a tenantAgentPersonaManifestAdapter) ResolveCurrentPersonaManifest(ctx context.Context, manifestID string) (agentmanifest.Manifest, error) {
	if a.store == nil || a.tenantID == uuid.Nil || ctx == nil || manifestID == "" {
		return agentmanifest.Manifest{}, ErrAgentManifestUnavailable
	}
	manifest, currentVersion, err := a.store.CurrentManifest(ctx, a.tenantID, manifestID)
	if err != nil {
		return agentmanifest.Manifest{}, fmt.Errorf("%w: current manifest read failed", ErrAgentManifestUnavailable)
	}
	if manifest.ID != manifestID || manifest.Version == 0 || manifest.Version != currentVersion || manifest.Validate() != nil {
		return agentmanifest.Manifest{}, ErrAgentManifestNotPublished
	}
	return manifest, nil
}

func (a tenantAgentPersonaManifestAdapter) ResolveAgentManifest(ref agentpersona.AgentManifestRef) (agentmanifest.Manifest, error) {
	return (AgentManifestStoreAdapter{Store: a.store, TenantID: a.tenantID}).ResolveAgentManifest(ref)
}

var _ PersonaStarterManifestResolverSource = (*TenantAgentPersonaManifestSource)(nil)
var _ PersonaStarterManifestResolver = tenantAgentPersonaManifestAdapter{}
var _ agentpersona.AgentManifestResolver = tenantAgentPersonaManifestAdapter{}
