package application

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type AgentPortableManifestCatalog interface {
	ListAgentPortableManifests(context.Context) ([]agentmanifest.Manifest, error)
}

// Catalog projects authoring controls independently of publication or
// installation. Each action still reauthorizes against its current owner.
func (s *AgentPortableService) Catalog(ctx context.Context) (productui.AgentPortableSnapshot, error) {
	if _, _, err := portablePrincipal(ctx); err != nil {
		return productui.AgentPortableSnapshot{}, err
	}
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
	snapshot := productui.AgentPortableSnapshot{Available: true, CanExport: export && len(manifests) > 0, CanImport: importAllowed && len(manifests) > 0, Manifests: []productui.AgentPortableManifest{}}
	for _, manifest := range manifests {
		digest, err := manifest.Digest()
		if err != nil {
			return productui.AgentPortableSnapshot{}, ErrAgentPortableInvalid
		}
		snapshot.Manifests = append(snapshot.Manifests, productui.AgentPortableManifest{ID: manifest.ID, Version: fmt.Sprint(manifest.Version), Name: manifest.ID, Digest: digest})
	}
	return snapshot, nil
}
