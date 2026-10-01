package application

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentportable"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type portableDraftRecorder struct {
	drafts  []agentportable.Draft
	failure error
}

func (f *portableDraftRecorder) SavePortableDraft(_ context.Context, tenant, actor string, d agentportable.Draft) error {
	if tenant != d.TenantID || actor != d.Manifest.OwnerID {
		return ErrAgentPortableDenied
	}
	if f.failure != nil {
		return f.failure
	}
	f.drafts = append(f.drafts, d)
	return nil
}

func (f *portableDraftRecorder) GetPortableDraft(_ context.Context, tenant, id string) (agentportable.Draft, error) {
	for _, draft := range f.drafts {
		if draft.TenantID == tenant && draft.Manifest.ID == id {
			return draft, nil
		}
	}
	return agentportable.Draft{}, ErrAgentPortableInvalid
}

func portableMappedFixture(t *testing.T) (context.Context, *AgentPortableService, AgentPortableImportRequest, *portableDraftRecorder) {
	t.Helper()
	ctx := trust.WithPrincipal(context.Background(), portableApplicationPrincipal(t))
	source := portableManifestForApplication("Exact reusable instruction body.")
	data, err := agentportable.ExportWithInstructions(source, "Exact reusable instruction body.")
	if err != nil {
		t.Fatal(err)
	}
	destination := source
	destination.ID = "destination"
	destination.ModelPolicy.ID = "destination-model"
	destination.OutputSchema.ID = "destination-output"
	destination.EvaluationRefs = []agentmanifest.Reference{source.EvaluationRefs[0]}
	destination.EvaluationRefs[0].ID = "destination-eval"
	recorder := &portableDraftRecorder{}
	service := &AgentPortableService{Manifests: portableManifestSourceFake{destination}, Instructions: portableInstructionSourceFake{"Exact reusable instruction body."}, Authorizer: portableAuthorizerFake{}, Drafts: recorder}
	request := AgentPortableImportRequest{Definition: data, DestinationManifestID: destination.ID, DestinationManifestVersion: destination.Version, Mappings: []AgentPortableReferenceMapping{{Kind: agentportable.ModelPolicyReference, SourceID: source.ModelPolicy.ID, DestinationID: destination.ModelPolicy.ID}, {Kind: agentportable.OutputSchemaReference, SourceID: source.OutputSchema.ID, DestinationID: destination.OutputSchema.ID}, {Kind: agentportable.EvaluationReference, SourceID: source.EvaluationRefs[0].ID, DestinationID: destination.EvaluationRefs[0].ID}}}
	return ctx, service, request, recorder
}

func TestTodo_AGENT_045_Served(t *testing.T) {
	ctx, s, r, store := portableMappedFixture(t)
	first, err := s.ImportMapped(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ImportMapped(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if first.State != "DRAFT" || first.Manifest.ID == second.Manifest.ID || len(first.Manifest.ContextGrants) != 0 || first.Manifest.ModelPolicy.ID != "destination-model" || first.Manifest.OutputSchema.ID != "destination-output" || first.Instructions != "Exact reusable instruction body." || len(store.drafts) != 2 {
		t.Fatalf("drafts=%+v %+v", first, second)
	}
	if first.Manifest.OwnerID != "owner-a" || first.TenantID != "tenant-a" {
		t.Fatal("caller supplied destination identity")
	}
	read, err := s.ReadDraft(ctx, first.Manifest.ID)
	if err != nil || read.Manifest.ID != first.Manifest.ID || read.Instructions != first.Instructions || read.State != "DRAFT" {
		t.Fatalf("reviewable draft %+v %v", read, err)
	}
}

func TestTodo_AGENT_045_Security_ReviewableDraft(t *testing.T) {
	ctx, s, r, store := portableMappedFixture(t)
	draft, err := s.ImportMapped(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadDraft(context.Background(), draft.Manifest.ID); !errors.Is(err, ErrAgentPortableDenied) {
		t.Fatal("draft read unauthenticated")
	}
	s.Authorizer = portableAuthorizerFake{ErrAgentPortableDenied}
	if _, err = s.ReadDraft(ctx, draft.Manifest.ID); !errors.Is(err, ErrAgentPortableDenied) {
		t.Fatal("draft read without authoring grant")
	}
	s.Authorizer = portableAuthorizerFake{}
	store.drafts[0].Manifest.ContextGrants = []agentmanifest.Reference{store.drafts[0].Manifest.ModelPolicy}
	if _, err = s.ReadDraft(ctx, draft.Manifest.ID); !errors.Is(err, ErrAgentPortableInvalid) {
		t.Fatal("review draft contained execution grants")
	}
}

func TestTodo_AGENT_045_Security_Served(t *testing.T) {
	for _, mutate := range []func(*AgentPortableImportRequest){func(r *AgentPortableImportRequest) { r.Mappings = r.Mappings[:2] }, func(r *AgentPortableImportRequest) { r.Mappings[0].DestinationID = "unavailable" }, func(r *AgentPortableImportRequest) { r.Mappings[0].Kind = agentportable.CapabilityReference }, func(r *AgentPortableImportRequest) { r.Mappings[1] = r.Mappings[0] }, func(r *AgentPortableImportRequest) {
		r.Definition = []byte(strings.Replace(string(r.Definition), "Exact reusable instruction body.", "tampered instructions", 1))
	}, func(r *AgentPortableImportRequest) { r.DestinationManifestVersion++ }} {
		ctx, s, r, store := portableMappedFixture(t)
		mutate(&r)
		if _, err := s.ImportMapped(ctx, r); err == nil || len(store.drafts) != 0 {
			t.Fatalf("forged mapping or body accepted: %v", err)
		}
	}
	ctx, s, r, store := portableMappedFixture(t)
	s.Authorizer = portableAuthorizerFake{ErrAgentPortableDenied}
	if _, err := s.ImportMapped(ctx, r); !errors.Is(err, ErrAgentPortableDenied) || len(store.drafts) != 0 {
		t.Fatal("unauthorized draft persisted")
	}
}

func TestTodo_AGENT_045_Golden_Served(t *testing.T) {
	ctx, s, _, _ := portableMappedFixture(t)
	manifest := portableManifestForApplication("Exact reusable instruction body.")
	s.Manifests = portableManifestSourceFake{manifest}
	data, err := s.Export(ctx, AgentPortableExportRequest{ManifestID: manifest.ID, ManifestVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	d, err := agentportable.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := d.CanonicalJSON()
	if err != nil || string(data) != string(again) {
		t.Fatal("portable bytes not canonical")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	for _, name := range []string{"tenant_id", "owner_id", "context_grants", "credentials", "memory", "installations", "run_history"} {
		if _, present := fields[name]; present {
			t.Fatalf("export authority field %s", name)
		}
	}
	if d.Instructions != "Exact reusable instruction body." {
		t.Fatal("executable instruction body omitted")
	}
}

func TestTodo_AGENT_045_Conformance_Served(t *testing.T) {
	ctx, s, r, store := portableMappedFixture(t)
	old := append([]AgentPortableReferenceMapping(nil), r.Mappings...)
	if _, err := s.ImportMapped(ctx, r); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old, r.Mappings) {
		t.Fatal("mapping mutated caller data")
	}
	if err := store.drafts[0].Manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if decodePortableRequest(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"manifest_id":"x","unknown":true}`)), &AgentPortableExportRequest{}, 64<<10) == nil {
		t.Fatal("unknown request accepted")
	}
	if decodePortableRequest(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"manifest_id":"x"} {}`)), &AgentPortableExportRequest{}, 64<<10) == nil {
		t.Fatal("trailing JSON accepted")
	}
}

func TestTodo_AGENT_045_HTTP(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	h := OverlayAgentPortableHTTP(next, nil, transport.Config{})
	for _, tc := range []struct {
		path   string
		status int
	}{{"/other", 204}, {AgentPortableImportPath, 503}, {AgentPortableExportPath, 503}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(`{}`)))
		if w.Code != tc.status {
			t.Fatalf("path=%s status=%d", tc.path, w.Code)
		}
	}
}

func TestTodo_AGENT_045_Conformance_Composition(t *testing.T) {
	mapper := func(values.TenantId) uuid.UUID { return uuid.Nil }
	auth := &personaAdminCommandAuthFake{}
	if _, err := NewDatabaseAgentPortableService(nil, auth, mapper); !errors.Is(err, ErrAgentPortableUnavailable) {
		t.Fatal("nil store composed")
	}
	store := &agentstore.Store{}
	if _, err := NewDatabaseAgentPortableService(store, nil, mapper); !errors.Is(err, ErrAgentPortableUnavailable) {
		t.Fatal("nil authorizer composed")
	}
	s, err := NewDatabaseAgentPortableService(store, auth, mapper)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Manifests.ResolveAgentManifest(context.Background(), "manifest", 1); !errors.Is(err, ErrAgentPortableDenied) {
		t.Fatal("source read admitted without identity")
	}
	if _, err = s.Instructions.ResolveAgentInstructions(context.Background(), "manifest", 1, "digest"); !errors.Is(err, ErrAgentPortableDenied) {
		t.Fatal("instruction read admitted without identity")
	}
	if err = s.Authorizer.AuthorizePortable(context.Background(), "import"); !errors.Is(err, ErrAgentPortableDenied) {
		t.Fatal("unauthenticated import authorized")
	}
	ctx := trust.WithPrincipal(context.Background(), portableApplicationPrincipal(t))
	if err = s.Authorizer.AuthorizePortable(ctx, "import"); err != nil || auth.actor.Tenant != "tenant-a" || auth.action != PersonaAdminCreateDraft {
		t.Fatalf("portable authorization %+v %v", auth, err)
	}
	if err = (AgentStorePortableDraftStore{}).SavePortableDraft(ctx, "tenant-a", "owner-a", agentportable.Draft{State: "DRAFT"}); !errors.Is(err, ErrAgentPortableUnavailable) {
		t.Fatal("missing database writer accepted")
	}
}
