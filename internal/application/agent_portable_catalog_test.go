package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type portableCatalogFixture struct {
	portableManifestSourceFake
	reads int
}

type portableCatalogActionGrant string

func (a portableCatalogActionGrant) AuthorizePortable(_ context.Context, action string) error {
	if action != string(a) {
		return ErrAgentPortableDenied
	}
	return nil
}

func (f *portableCatalogFixture) ListAgentPortableManifests(context.Context) ([]agentmanifest.Manifest, error) {
	f.reads++
	return []agentmanifest.Manifest{f.manifest}, nil
}

func TestTodo_AGENT_045_HTTP_UnpublishedDefinitionCatalog(t *testing.T) {
	ctx, service, _, _ := portableMappedFixture(t)
	source := &portableCatalogFixture{portableManifestSourceFake: portableManifestSourceFake{portableManifestForApplication("Exact unpublished instructions.")}}
	service.Manifests = source
	principal, _ := trust.FromContext(ctx)
	admission, bearer := agentRolloutPortableAdmission(t, principal)
	handler := OverlayAgentPortableHTTP(nil, service, admission)
	w := agentDesignHTTP(t, handler, bearer, AgentPortableCatalogPath, struct{}{})
	var snapshot productui.AgentPortableSnapshot
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &snapshot) != nil || !snapshot.Available || !snapshot.CanExport || !snapshot.CanImport || len(snapshot.Manifests) != 1 || snapshot.Manifests[0].ID != source.manifest.ID || snapshot.Manifests[0].Version != "1" {
		t.Fatalf("unpublished definition controls missing: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("catalog can be cached across sessions")
	}
	w = agentDesignHTTP(t, handler, "invalid", AgentPortableCatalogPath, struct{}{})
	if w.Code != http.StatusUnauthorized || source.reads != 1 {
		t.Fatal("catalog read before authentication")
	}
	service.Authorizer = portableAuthorizerFake{ErrAgentPortableDenied}
	w = agentDesignHTTP(t, handler, bearer, AgentPortableCatalogPath, struct{}{})
	if w.Code != http.StatusForbidden || source.reads != 1 {
		t.Fatal("catalog read after authoring grant denial")
	}
	if _, err := service.Catalog(context.Background()); !errors.Is(err, ErrAgentPortableDenied) {
		t.Fatalf("anonymous catalog: %v", err)
	}
	service.Authorizer = portableCatalogActionGrant("export")
	snapshot, err := service.Catalog(ctx)
	if err != nil || !snapshot.CanExport || snapshot.CanImport {
		t.Fatalf("export permission enabled import: %+v %v", snapshot, err)
	}
}
