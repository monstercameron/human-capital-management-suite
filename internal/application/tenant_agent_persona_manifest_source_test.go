package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type tenantAgentManifestStoreFake struct {
	manifest agentmanifest.Manifest
	tenant   uuid.UUID
	err      error
}

func (f tenantAgentManifestStoreFake) CurrentManifest(_ context.Context, tenant uuid.UUID, _ string) (agentmanifest.Manifest, uint64, error) {
	if tenant != f.tenant {
		return agentmanifest.Manifest{}, 0, errors.New("tenant mismatch")
	}
	return f.manifest, f.manifest.Version, f.err
}

func (f tenantAgentManifestStoreFake) ManifestVersion(_ context.Context, tenant uuid.UUID, _ string, version uint64) (agentmanifest.Manifest, error) {
	if tenant != f.tenant || version != f.manifest.Version {
		return agentmanifest.Manifest{}, errors.New("version mismatch")
	}
	return f.manifest, f.err
}

func TestTenantAgentPersonaManifestSource_BindsCurrentManifestAndCompatibilityToTenant(t *testing.T) {
	manifest := personaStarterManifest("Answer approved questions.")
	id := uuid.New()
	source, err := NewTenantAgentPersonaManifestSource(tenantAgentManifestStoreFake{manifest: manifest, tenant: id}, func(tenant values.TenantId) uuid.UUID {
		if tenant == "harborcare" {
			return id
		}
		return uuid.Nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := personaTenantContext(t, "harborcare")
	resolver, err := source.ForTenant(ctx, "harborcare")
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolver.ResolveCurrentPersonaManifest(ctx, manifest.ID)
	if err != nil || got.ID != manifest.ID || got.Version != manifest.Version {
		t.Fatalf("current manifest = %+v, %v", got, err)
	}
	compatibility, err := source.CompatibilityForTenant(ctx, "harborcare")
	if err != nil || compatibility.Compatible(agentpersona.AgentManifestRef{ID: manifest.ID, Version: uint32(manifest.Version), Digest: manifestDigest(manifest), SchemaVersion: manifest.SchemaVersion}) != nil {
		t.Fatalf("manifest compatibility = %v, %v", compatibility, err)
	}
	if _, err := source.ForTenant(ctx, values.TenantId("ironridge")); !errors.Is(err, ErrAgentManifestUnavailable) {
		t.Fatalf("cross-tenant resolver error = %v", err)
	}
}

func TestTenantAgentPersonaManifestSource_RejectsInvalidCurrentManifest(t *testing.T) {
	manifest := personaStarterManifest("Answer approved questions.")
	manifest.Version = 0
	id := uuid.New()
	source, err := NewTenantAgentPersonaManifestSource(tenantAgentManifestStoreFake{manifest: manifest, tenant: id}, func(values.TenantId) uuid.UUID { return id })
	if err != nil {
		t.Fatal(err)
	}
	ctx := personaTenantContext(t, "ironridge")
	resolver, err := source.ForTenant(ctx, "ironridge")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.ResolveCurrentPersonaManifest(ctx, manifest.ID); !errors.Is(err, ErrAgentManifestNotPublished) {
		t.Fatalf("invalid current manifest error = %v", err)
	}
	if _, err := source.ForTenant(context.Background(), "ironridge"); !errors.Is(err, ErrAgentManifestUnavailable) {
		t.Fatalf("missing trust error = %v", err)
	}
}
