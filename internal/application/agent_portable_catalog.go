package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type AgentPortableManifestCatalog interface {
	ListAgentPortableManifests(context.Context) ([]agentmanifest.Manifest, error)
}

// Catalog projects authoring controls independently of publication or
// installation. Each action still reauthorizes against its current owner.
func (s *AgentPortableService) Catalog(ctx context.Context) (productui.AgentPortableSnapshot, error) {
	_, tenant, err := portablePrincipal(ctx)
	if err != nil {
		return productui.AgentPortableSnapshot{}, err
	}
	return s.catalog(ctx, tenant)
}

func (s *AgentPortableService) catalog(ctx context.Context, tenant string) (productui.AgentPortableSnapshot, error) {
	if s == nil || s.Authorizer == nil {
		return productui.AgentPortableSnapshot{}, ErrAgentPortableUnavailable
	}
	export := s.Authorizer.AuthorizePortable(ctx, "export") == nil
	importAllowed := s.Authorizer.AuthorizePortable(ctx, "import") == nil
	if !export && !importAllowed {
		return productui.AgentPortableSnapshot{}, ErrAgentPortableDenied
	}
	source, ok := s.Manifests.(AgentPortableManifestCatalog)
	if !ok {
		return productui.AgentPortableSnapshot{}, ErrAgentPortableUnavailable
	}
	manifests, err := source.ListAgentPortableManifests(ctx)
	if err != nil {
		return productui.AgentPortableSnapshot{}, err
	}
	snapshot := productui.AgentPortableSnapshot{Available: true, CanExport: export && len(manifests) > 0, CanImport: importAllowed && len(manifests) > 0, Manifests: []productui.AgentPortableManifest{}, Drafts: []productui.AgentPortableReviewDraft{}}
	for _, manifest := range manifests {
		digest, err := manifest.Digest()
		if err != nil {
			return productui.AgentPortableSnapshot{}, ErrAgentPortableInvalid
		}
		name := strings.TrimSpace(manifest.Purpose)
		if name == "" {
			name = manifest.ID
		}
		snapshot.Manifests = append(snapshot.Manifests, productui.AgentPortableManifest{ID: manifest.ID, Version: fmt.Sprint(manifest.Version), Name: portableCatalogDisplayName(manifest, name), Digest: digest})
	}
	sort.SliceStable(snapshot.Manifests, func(i, j int) bool {
		if snapshot.Manifests[i].ID == snapshot.Manifests[j].ID {
			left, _ := strconv.ParseUint(snapshot.Manifests[i].Version, 10, 64)
			right, _ := strconv.ParseUint(snapshot.Manifests[j].Version, 10, 64)
			return left > right
		}
		return snapshot.Manifests[i].Name < snapshot.Manifests[j].Name
	})
	for index := range snapshot.Manifests {
		if index == 0 || snapshot.Manifests[index-1].ID != snapshot.Manifests[index].ID {
			snapshot.Manifests[index].Live = true
		}
	}
	if importAllowed {
		if drafts, ok := s.Drafts.(AgentPortableDraftCatalogReader); ok {
			for _, manifest := range manifests {
				draft, readErr := drafts.ReadPortableDraftProjection(ctx, tenant, manifest.ID)
				if errors.Is(readErr, dbport.ErrNoRows) {
					continue
				}
				if readErr != nil {
					return productui.AgentPortableSnapshot{}, readErr
				}
				if draft.ID != manifest.ID || strings.TrimSpace(draft.Name) == "" || draft.State != "DRAFT" || draft.Version == 0 {
					return productui.AgentPortableSnapshot{}, ErrAgentPortableInvalid
				}
				if importedAt, parseErr := time.Parse(time.RFC3339, draft.ImportedAt); parseErr != nil || importedAt.IsZero() {
					return productui.AgentPortableSnapshot{}, ErrAgentPortableInvalid
				}
				snapshot.Drafts = append(snapshot.Drafts, draft)
			}
		}
	}
	return snapshot, nil
}

// portableCatalogDisplayName converts only the catalog's reviewed manifest
// identity to a visible agent name. A manifest purpose remains explanatory
// text and must never become the name in an export selector.
func portableCatalogDisplayName(manifest agentmanifest.Manifest, fallback string) string {
	name := strings.TrimSpace(manifest.ID)
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		name = name[dot+1:]
	}
	name = productui.DisplayLabel(name)
	if name != "" {
		return name
	}
	return strings.TrimSpace(fallback)
}
