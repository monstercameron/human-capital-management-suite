package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentportable"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type portableCatalogFixture struct {
	portableManifestSourceFake
	reads int
}

type portableCatalogActionGrant string

type portableCatalogSubjectGrant string

func (a portableCatalogSubjectGrant) AuthorizePortable(ctx context.Context, _ string) error {
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.Subject() != string(a) {
		return ErrAgentPortableDenied
	}
	return nil
}

type portableDraftCatalogFake struct {
	draft productui.AgentPortableReviewDraft
	reads int
}

func (f *portableDraftCatalogFake) SavePortableDraft(context.Context, string, string, agentportable.Draft) error {
	return nil
}

func (f *portableDraftCatalogFake) ReadPortableDraftProjection(context.Context, string, string) (productui.AgentPortableReviewDraft, error) {
	f.reads++
	return f.draft, nil
}

func (a portableCatalogActionGrant) AuthorizePortable(_ context.Context, action string) error {
	if action != string(a) {
		return ErrAgentPortableDenied
	}
	return nil
}

func TestTodo_AGENTUX_015_PortableDraftProjection(t *testing.T) {
	ctx, service, _, _ := portableMappedFixture(t)
	manifest := portableManifestForApplication("Exact imported instructions.")
	service.Manifests = &portableCatalogFixture{portableManifestSourceFake: portableManifestSourceFake{manifest: manifest}}
	drafts := &portableDraftCatalogFake{draft: productui.AgentPortableReviewDraft{ID: manifest.ID, Name: "Policy Helper", Purpose: manifest.Purpose, ImportedAt: time.Date(2026, 9, 30, 18, 0, 0, 0, time.UTC).Format(time.RFC3339), State: "DRAFT", Version: 1}}
	service.Drafts = drafts
	service.Authorizer = portableCatalogSubjectGrant("owner-a")
	snapshot, err := service.Catalog(ctx)
	if err != nil || len(snapshot.Drafts) != 1 || snapshot.Drafts[0].Name != "Policy Helper" || snapshot.Drafts[0].State != "DRAFT" || drafts.reads != 1 {
		t.Fatalf("portable draft catalog = %+v, reads=%d, err=%v", snapshot.Drafts, drafts.reads, err)
	}
	principal, _ := trust.FromContext(ctx)
	admission, ownerBearer := agentRolloutPortableAdmission(t, principal)
	handler := OverlayAgentPortableHTTP(nil, service, admission)
	response := agentDesignHTTP(t, handler, ownerBearer, AgentPortableCatalogPath, struct{}{})
	var served productui.AgentPortableSnapshot
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &served) != nil || len(served.Drafts) != 1 || served.Drafts[0].Name != "Policy Helper" {
		t.Fatalf("served portable drafts status=%d body=%s", response.Code, response.Body.String())
	}
	nonOwner := agentTestPrincipal(t, "tenant-a", "viewer-a")
	_, nonOwnerBearer := agentRolloutPortableAdmission(t, nonOwner)
	response = agentDesignHTTP(t, handler, nonOwnerBearer, AgentPortableCatalogPath, struct{}{})
	if response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "Policy Helper") || drafts.reads != 2 {
		t.Fatalf("non-owner portable response status=%d reads=%d body=%s", response.Code, drafts.reads, response.Body.String())
	}
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

func TestAgentUXR5Ops_PortableCatalogUsesVisibleReviewedAgentName(t *testing.T) {
	manifest := portableManifestForApplication("Agent.starter.policy Helper")
	manifest.ID = "agent.starter.policy_helper"
	if name := portableCatalogDisplayName(manifest, manifest.Purpose); name != "Policy Helper" {
		t.Fatalf("portable catalog name = %q, want persona-style display name", name)
	}
}
