package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	workflowcore "github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func TestTodo_WF_UI_005_TransportCreatesInsertsAndRestoresDraft(t *testing.T) {
	store := &wfui005AuthoringStore{}
	authoring := &designeredit.Service{
		Store: store, Catalog: wfui005AuthoringCatalog{{
			ID: "fragment.review", Version: 2, Name: "Review", Kind: designerpalette.KindFragment,
			Expansion: designerpalette.Expansion{Nodes: []workflowcore.Node{{ID: "manager", Type: workflowcore.StepApproval}}},
		}},
		NewID: func() (string, error) { return "01999f37-9f42-7000-8000-000000000008", nil },
		Now:   func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) },
	}
	srv := &server{deps: Dependencies{DraftAuthoring: authoring, Authorize: allowWorkflowCalls}}
	ctx := workflowTestContext(t, CreateWorkflowDraftProcedure)
	created, err := srv.CreateWorkflowDraft(ctx, &workflowv1.CreateWorkflowDraftRequest{Name: "Worker change"})
	if err != nil {
		t.Fatalf("CreateWorkflowDraft: %v", err)
	}
	if created.GetDraft().GetRevision() != 1 || created.GetDraft().GetName() != "Worker change" {
		t.Fatalf("created = %+v", created)
	}
	inserted, err := srv.InsertWorkflowPaletteEntry(ctx, &workflowv1.InsertWorkflowPaletteEntryRequest{
		DraftId: created.GetDraft().GetDraftId(), ExpectedRevision: 1, EntryId: "fragment.review", EntryVersion: 2,
	})
	if err != nil {
		t.Fatalf("InsertWorkflowPaletteEntry: %v", err)
	}
	if inserted.GetDraft().GetRevision() != 2 || len(inserted.GetDraft().GetGroups()) != 1 || !inserted.GetDraft().GetGroups()[0].GetCollapsed() || inserted.GetGroupId() == "" {
		t.Fatalf("inserted = %+v", inserted)
	}
	restored, err := srv.GetWorkflowDraft(ctx, &workflowv1.GetWorkflowDraftRequest{DraftId: created.GetDraft().GetDraftId()})
	if err != nil || restored.GetDraft().GetRevision() != 2 || len(restored.GetDraft().GetNodes()) != 1 {
		t.Fatalf("GetWorkflowDraft = %+v, %v", restored, err)
	}
}

func TestTodo_WF_UI_005_TransportDraftAuthoringSecurity(t *testing.T) {
	store := &wfui005AuthoringStore{}
	authoring := &designeredit.Service{Store: store, Catalog: wfui005AuthoringCatalog{}, NewID: func() (string, error) { return "01999f37-9f42-7000-8000-000000000009", nil }}
	srv := &server{deps: Dependencies{DraftAuthoring: authoring, Authorize: func(context.Context, *trust.Principal, string) bool { return false }}}
	_, err := srv.CreateWorkflowDraft(workflowTestContext(t, CreateWorkflowDraftProcedure), &workflowv1.CreateWorkflowDraftRequest{})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodePermissionDenied {
		t.Fatalf("CreateWorkflowDraft denial = %v", err)
	}
	if store.saves != 0 {
		t.Fatalf("denied create saved %d drafts", store.saves)
	}
}

func TestWorkflowDraftVersioningStartsOnlyFromANewerSemanticVersion(t *testing.T) {
	registry, active := wfui002Publication(t)
	store := &wfui005AuthoringStore{}
	authoring := &designeredit.Service{
		Store: store, Catalog: wfui005AuthoringCatalog{},
		NewID: func() (string, error) { return "01999f37-9f42-7000-8000-000000000011", nil },
	}
	srv := &server{deps: Dependencies{Definitions: registry, DraftAuthoring: authoring, Authorize: allowWorkflowCalls}}
	ctx := workflowTestContext(t, CreateWorkflowDraftProcedure)

	created, err := srv.CreateWorkflowDraft(ctx, &workflowv1.CreateWorkflowDraftRequest{WorkflowId: active.WorkflowID})
	if err != nil {
		t.Fatalf("CreateWorkflowDraft successor: %v", err)
	}
	if got := created.GetDraft(); got.GetSemanticVersion() != "1.0.1" || got.GetBaseVersionDigest() != active.CompiledPlanDigest {
		t.Fatalf("successor identity = version %q base %q, want 1.0.1 and %q", got.GetSemanticVersion(), got.GetBaseVersionDigest(), active.CompiledPlanDigest)
	}

	store.draft = designeredit.Draft{}
	_, err = srv.CreateWorkflowDraft(ctx, &workflowv1.CreateWorkflowDraftRequest{
		WorkflowId: active.WorkflowID, SemanticVersion: "1.0.0", BaseVersionDigest: active.CompiledPlanDigest,
	})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodeInvalidArgument {
		t.Fatalf("equal-version replacement = %v, want invalid argument", err)
	}
}

func TestTodo_WF_UI_005_PublishedPromotionSuccessorMatchesExecutableByteForByte(t *testing.T) {
	definition := promotionexec.Definition()
	definitionBytes, err := workflowcore.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := promotionexec.Compile(definition)
	if err != nil {
		t.Fatal(err)
	}
	base := workflowversion.CompiledVersion{
		WorkflowID: definition.WorkflowID, DefinitionVersion: definition.Version, SemanticVersion: promotionexec.SemanticVersion,
		DefinitionDigest: "sha256:published-execute-projection", CompiledPlanDigest: plan.Digest(), Status: workflowversion.StatusActive,
	}
	definitions := wfui005DefinitionReader{version: base}
	catalog := wfui005AuthoringCatalog{{
		ID: "hcmnext.templates.promotion", Version: 1, Name: "Promotion", Kind: designerpalette.KindTemplate,
		PublishedPlanDigest: plan.Digest(), Expansion: designerpalette.Expansion{Template: &definition},
	}}
	store := &wfui005AuthoringStore{}
	authoring := &designeredit.Service{
		Store: store, Catalog: catalog,
		NewID: func() (string, error) { return "01999f37-9f42-7000-8000-000000000014", nil },
	}
	srv := &server{deps: Dependencies{Definitions: definitions, Palette: catalog, DraftAuthoring: authoring, Authorize: allowWorkflowCalls}}

	created, err := srv.CreateWorkflowDraft(workflowTestContext(t, CreateWorkflowDraftProcedure), &workflowv1.CreateWorkflowDraftRequest{
		WorkflowId: definition.WorkflowID,
	})
	if err != nil {
		t.Fatalf("CreateWorkflowDraft successor: %v", err)
	}
	if !bytes.Equal(store.draft.Document, definitionBytes) {
		t.Fatal("published Promotion successor is not the canonical executable definition")
	}
	storedDefinition, err := workflowcore.Load(store.draft.Document)
	if err != nil {
		t.Fatalf("load stored Promotion successor: %v", err)
	}
	storedPlan, err := promotionexec.Compile(storedDefinition)
	if err != nil {
		t.Fatalf("compile stored Promotion successor: %v", err)
	}
	if storedPlan.Digest() != base.CompiledPlanDigest {
		t.Fatalf("stored Promotion successor plan digest = %q, want published executable %q", storedPlan.Digest(), base.CompiledPlanDigest)
	}
	got := created.GetDraft()
	if got.GetSemanticVersion() != "1.1.1" || got.GetBaseVersionDigest() != base.CompiledPlanDigest ||
		got.GetDefinitionDigest() != workflowversion.DefinitionDigest(definition) || got.GetMatchesBaseDefinition() || !got.GetMatchesTemplateDefinition() {
		t.Fatalf("published Promotion successor parity = %+v", got)
	}
	if len(got.GetNodes()) != len(definition.Nodes) || len(got.GetEdges()) != len(definition.Edges) {
		t.Fatalf("published Promotion successor = %d nodes/%d edges, want %d/%d", len(got.GetNodes()), len(got.GetEdges()), len(definition.Nodes), len(definition.Edges))
	}
}

func TestWorkflowDraftProjectionProvesExactBaseDefinitionParity(t *testing.T) {
	registry, active := wfui002Publication(t)
	srv := &server{deps: Dependencies{Definitions: registry}}

	projected := srv.projectDraftView(context.Background(), "tenant-a", designeredit.View{
		DraftID: "draft-parity", WorkflowID: active.WorkflowID, Name: "Promotion", SemanticVersion: "1.0.1",
		BaseVersionDigest: active.CompiledPlanDigest, DefinitionDigest: active.DefinitionDigest,
		StartNodeID: "start", Revision: 1,
	})
	if projected.GetDefinitionDigest() != active.DefinitionDigest || projected.GetBaseDefinitionDigest() != active.DefinitionDigest || !projected.GetMatchesBaseDefinition() {
		t.Fatalf("exact base parity projection = %+v", projected)
	}

	changed := srv.projectDraftView(context.Background(), "tenant-a", designeredit.View{
		DraftID: "draft-changed", WorkflowID: active.WorkflowID, Name: "Promotion", SemanticVersion: "1.0.1",
		BaseVersionDigest: active.CompiledPlanDigest, DefinitionDigest: "different-definition",
		StartNodeID: "start", Revision: 2,
	})
	if changed.GetMatchesBaseDefinition() {
		t.Fatalf("changed draft was reported as an exact base match: %+v", changed)
	}
}

func TestWorkflowDraftProjectionProvesExecutableTemplateParitySeparatelyFromBase(t *testing.T) {
	template := workflowcore.Definition{WorkflowID: "workflow.promotion", Version: 2, Name: "Promotion", StartNodeID: "start", Nodes: []workflowcore.Node{{ID: "start", Type: workflowcore.StepTask}}}
	templateDigest := workflowversion.DefinitionDigest(template)
	srv := &server{deps: Dependencies{Palette: wfui005AuthoringCatalog{{
		ID: "template.promotion", Version: 7, Name: "Promotion", Kind: designerpalette.KindTemplate,
		Expansion: designerpalette.Expansion{Template: &template},
	}}}}
	projected := srv.projectDraftView(context.Background(), "tenant-a", designeredit.View{
		DraftID: "draft-template", WorkflowID: template.WorkflowID, Name: template.Name, SemanticVersion: "2.0.0",
		DefinitionDigest: templateDigest, StartNodeID: template.StartNodeID, Revision: 1,
	})
	if !projected.GetMatchesTemplateDefinition() || projected.GetTemplateDefinitionDigest() != templateDigest || projected.GetTemplateId() != "template.promotion" || projected.GetTemplateVersion() != 7 {
		t.Fatalf("executable-template parity projection = %+v", projected)
	}
}

func TestTodo_WF_UI_005_TransportStaleDraftEditIsAborted(t *testing.T) {
	store := &wfui005AuthoringStore{}
	authoring := &designeredit.Service{
		Store: store, Catalog: wfui005AuthoringCatalog{{ID: "kernel.task", Version: 1, Name: "Task", Kind: designerpalette.KindBlock, StepType: workflowcore.StepTask}},
		NewID: func() (string, error) { return "01999f37-9f42-7000-8000-000000000010", nil },
	}
	srv := &server{deps: Dependencies{DraftAuthoring: authoring, Authorize: allowWorkflowCalls}}
	ctx := workflowTestContext(t, CreateWorkflowDraftProcedure)
	created, err := srv.CreateWorkflowDraft(ctx, &workflowv1.CreateWorkflowDraftRequest{})
	if err != nil {
		t.Fatal(err)
	}
	request := &workflowv1.InsertWorkflowPaletteEntryRequest{DraftId: created.GetDraft().GetDraftId(), ExpectedRevision: 1, EntryId: "kernel.task", EntryVersion: 1}
	if _, err := srv.InsertWorkflowPaletteEntry(ctx, request); err != nil {
		t.Fatal(err)
	}
	_, err = srv.InsertWorkflowPaletteEntry(ctx, request)
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodeAborted {
		t.Fatalf("stale edit = %v", err)
	}
}

type wfui005AuthoringCatalog []designerpalette.Entry

func (c wfui005AuthoringCatalog) List(context.Context, values.TenantId) []designerpalette.Entry {
	return c
}

type wfui005DefinitionReader struct {
	version workflowversion.CompiledVersion
}

func (r wfui005DefinitionReader) ListAll() ([]workflowversion.CompiledVersion, error) {
	return []workflowversion.CompiledVersion{r.version}, nil
}

func (r wfui005DefinitionReader) GetByDigest(digest string) (workflowversion.CompiledVersion, bool, error) {
	return r.version, digest == r.version.CompiledPlanDigest, nil
}

func (r wfui005DefinitionReader) GetActiveForWorkflow(workflowID string) (workflowversion.CompiledVersion, bool, error) {
	return r.version, workflowID == r.version.WorkflowID, nil
}

func (r wfui005DefinitionReader) List(workflowID string) ([]workflowversion.CompiledVersion, error) {
	if workflowID != r.version.WorkflowID {
		return nil, nil
	}
	return []workflowversion.CompiledVersion{r.version}, nil
}

type wfui005AuthoringStore struct {
	draft designeredit.Draft
	saves int
}

func (s *wfui005AuthoringStore) Load(_ context.Context, _ values.TenantId, id string) (designeredit.Draft, error) {
	if s.draft.DraftID != id {
		return designeredit.Draft{}, designeredit.ErrNotFound
	}
	return s.draft, nil
}

func (s *wfui005AuthoringStore) Save(_ context.Context, _ values.TenantId, request designeredit.SaveRequest) (designeredit.Draft, error) {
	s.saves++
	if request.ExpectedRevision == 0 {
		if s.draft.DraftID != "" {
			return designeredit.Draft{}, designeredit.ErrConflict
		}
		s.draft = designeredit.Draft{DraftID: request.DraftID, WorkflowID: request.WorkflowID, AuthorRef: request.AuthorRef, SemanticVersion: request.SemanticVersion, BaseVersionDigest: request.BaseVersionDigest, Revision: 1, Document: append(json.RawMessage(nil), request.Document...), ExpiresAt: request.ExpiresAt}
		return s.draft, nil
	}
	if request.ExpectedRevision != s.draft.Revision || request.AuthorRef != s.draft.AuthorRef {
		return designeredit.Draft{}, designeredit.ErrConflict
	}
	s.draft.WorkflowID, s.draft.SemanticVersion, s.draft.Revision, s.draft.Document = request.WorkflowID, request.SemanticVersion, s.draft.Revision+1, append(json.RawMessage(nil), request.Document...)
	return s.draft, nil
}
