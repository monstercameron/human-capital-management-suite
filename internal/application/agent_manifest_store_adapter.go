package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
)

var (
	// ErrAgentManifestUnavailable identifies a resolver that cannot safely read
	// the isolated agent store or was constructed without a tenant.
	ErrAgentManifestUnavailable = errors.New("application: agent manifest unavailable")
	// ErrAgentManifestNotPublished identifies a manifest version that is not the
	// tenant's current published pointer.
	ErrAgentManifestNotPublished = errors.New("application: agent manifest is not published")
	// ErrAgentManifestDigestMismatch identifies a reference whose digest does
	// not match the immutable bytes stored for its exact version.
	ErrAgentManifestDigestMismatch = errors.New("application: agent manifest digest mismatch")
)

// AgentManifestVersionStore is the narrow isolated-store contract needed by
// AgentManifestStoreAdapter. agentstore.Store implements this contract.
type AgentManifestVersionStore interface {
	CurrentManifest(context.Context, uuid.UUID, string) (agentmanifest.Manifest, uint64, error)
	ManifestVersion(context.Context, uuid.UUID, string, uint64) (agentmanifest.Manifest, error)
}

// AgentManifestStoreAdapter resolves persona manifest references against one
// tenant's immutable, published agent manifest in the isolated agent store.
// The tenant is fixed when the adapter is composed and never supplied by a
// persona or other caller.
type AgentManifestStoreAdapter struct {
	Store    AgentManifestVersionStore
	TenantID uuid.UUID
}

var _ agentpersona.AgentManifestResolver = AgentManifestStoreAdapter{}

// ResolveAgentManifest resolves the exact published manifest expected by a
// persona. It exists for agentpersona.AgentManifestResolver; callers with a
// request context should prefer ResolveAgentManifestContext.
func (a AgentManifestStoreAdapter) ResolveAgentManifest(ref agentpersona.AgentManifestRef) (agentmanifest.Manifest, error) {
	return a.ResolveAgentManifestContext(context.Background(), ref)
}

// ResolveAgentManifestContext resolves a tenant-scoped immutable manifest and
// verifies identity, schema version, digest, and publication. A historical
// version is refused even when it remains retained in the append-only store.
func (a AgentManifestStoreAdapter) ResolveAgentManifestContext(ctx context.Context, ref agentpersona.AgentManifestRef) (agentmanifest.Manifest, error) {
	if a.Store == nil || a.TenantID == uuid.Nil || ref.ID == "" || ref.Version == 0 || ref.SchemaVersion == 0 || ctx == nil {
		return agentmanifest.Manifest{}, ErrAgentManifestUnavailable
	}
	current, _, err := a.Store.CurrentManifest(ctx, a.TenantID, ref.ID)
	if err != nil {
		return agentmanifest.Manifest{}, errors.Join(ErrAgentManifestNotPublished, fmt.Errorf("read publication: %w", err))
	}
	if current.Version != uint64(ref.Version) || current.SchemaVersion != ref.SchemaVersion {
		return agentmanifest.Manifest{}, ErrAgentManifestNotPublished
	}
	currentDigest, err := current.Digest()
	if err != nil {
		return agentmanifest.Manifest{}, fmt.Errorf("%w: canonicalize publication: %v", ErrAgentManifestUnavailable, err)
	}
	if currentDigest != ref.Digest {
		return agentmanifest.Manifest{}, ErrAgentManifestDigestMismatch
	}
	manifest, err := a.Store.ManifestVersion(ctx, a.TenantID, ref.ID, uint64(ref.Version))
	if err != nil {
		return agentmanifest.Manifest{}, errors.Join(ErrAgentManifestUnavailable, fmt.Errorf("read immutable version: %w", err))
	}
	if err := agentmanifest.Compatible(agentmanifest.ManifestRef{ID: ref.ID, Version: uint64(ref.Version), SchemaVersion: ref.SchemaVersion, Digest: ref.Digest}, manifest); err != nil {
		if currentDigest == ref.Digest {
			return agentmanifest.Manifest{}, errors.Join(ErrAgentManifestDigestMismatch, err)
		}
		return agentmanifest.Manifest{}, ErrAgentManifestUnavailable
	}
	return manifest, nil
}
