package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var errSurfacePersonaDirectory = errors.New("application: surface persona directory unavailable")

// surfacePublishedPersona is the handle-bearing identity of one published
// persona version. Handles resolve to persona IDs; version currency is
// admission's job, not the directory's.
type surfacePublishedPersona struct {
	PersonaID string
	Version   int64
	Handle    string
}

// surfacePersonaCatalog lists the currently published personas of one tenant.
// The production implementation reads the tenant-scoped persona store, so a
// handle resolves only while its persona stays published.
type surfacePersonaCatalog interface {
	ListSurfacePublishedPersonas(ctx context.Context, tenant string) ([]surfacePublishedPersona, error)
}

// surfacePersonaStoreCatalog is the production surfacePersonaCatalog over the
// durable persona store.
type surfacePersonaStoreCatalog struct{ store *agentpersonastore.Store }

func newSurfacePersonaStoreCatalog(store *agentpersonastore.Store) (*surfacePersonaStoreCatalog, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: persona store is required", errSurfacePersonaDirectory)
	}
	return &surfacePersonaStoreCatalog{store: store}, nil
}

func (c *surfacePersonaStoreCatalog) ListSurfacePublishedPersonas(ctx context.Context, tenant string) ([]surfacePublishedPersona, error) {
	if c == nil || c.store == nil || ctx == nil {
		return nil, errSurfacePersonaDirectory
	}
	id := values.TenantId(tenant)
	if err := id.Validate(); err != nil {
		return nil, fmt.Errorf("%w: invalid tenant: %v", errSurfacePersonaDirectory, err)
	}
	scoped, err := c.store.ForTenant(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%w: scope persona store: %v", errSurfacePersonaDirectory, err)
	}
	versions, err := scoped.ListPublished(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: list published personas: %v", errSurfacePersonaDirectory, err)
	}
	out := make([]surfacePublishedPersona, 0, len(versions))
	for _, version := range versions {
		if version.TenantID != id || strings.TrimSpace(version.PersonaID) == "" || version.Version <= 0 || strings.TrimSpace(version.Handle) == "" {
			return nil, fmt.Errorf("%w: published persona row is incomplete", errSurfacePersonaDirectory)
		}
		out = append(out, surfacePublishedPersona{PersonaID: version.PersonaID, Version: version.Version, Handle: version.Handle})
	}
	return out, nil
}

// storeSurfacePersonaDirectory resolves raw @handles to persona identities
// against the currently published catalog. Workflow pages and hub documents
// share one catalog: a handle names the same persona on both surfaces.
// Matching is exact — handles are identifiers, never display text — and an
// ambiguous handle names no one.
type storeSurfacePersonaDirectory struct{ catalog surfacePersonaCatalog }

func newStoreSurfacePersonaDirectory(catalog surfacePersonaCatalog) (*storeSurfacePersonaDirectory, error) {
	if catalog == nil {
		return nil, fmt.Errorf("%w: published catalog is required", errSurfacePersonaDirectory)
	}
	return &storeSurfacePersonaDirectory{catalog: catalog}, nil
}

func (d *storeSurfacePersonaDirectory) ResolveSurfacePersona(ctx context.Context, tenant string, kind chatrecipient.SurfaceKind, handle string) (string, bool, error) {
	if d == nil || d.catalog == nil || ctx == nil {
		return "", false, errSurfacePersonaDirectory
	}
	if !kind.Valid() {
		return "", false, fmt.Errorf("%w: unknown surface kind", errSurfacePersonaDirectory)
	}
	if strings.TrimSpace(tenant) == "" {
		return "", false, fmt.Errorf("%w: tenant is required", errSurfacePersonaDirectory)
	}
	if strings.TrimSpace(handle) == "" {
		return "", false, nil
	}
	rows, err := d.catalog.ListSurfacePublishedPersonas(ctx, tenant)
	if err != nil {
		return "", false, fmt.Errorf("%w: list published personas: %v", errSurfacePersonaDirectory, err)
	}
	id := ""
	for _, row := range rows {
		if row.Handle != handle {
			continue
		}
		if strings.TrimSpace(row.PersonaID) == "" {
			return "", false, fmt.Errorf("%w: published persona row is incomplete", errSurfacePersonaDirectory)
		}
		if id == "" {
			id = row.PersonaID
			continue
		}
		if id != row.PersonaID {
			return "", false, fmt.Errorf("%w: handle %q is ambiguous", errSurfacePersonaDirectory, handle)
		}
	}
	return id, id != "", nil
}

var _ surfacePersonaDirectory = (*storeSurfacePersonaDirectory)(nil)
