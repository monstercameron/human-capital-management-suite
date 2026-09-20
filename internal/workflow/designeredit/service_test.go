package designeredit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

type memoryDraftStore struct{ draft designeredit.Draft }

func (s *memoryDraftStore) Load(_ context.Context, _ values.TenantId, id string) (designeredit.Draft, error) {
	if s.draft.DraftID != id {
		return designeredit.Draft{}, designeredit.ErrNotFound
	}
	return s.draft, nil
}

func (s *memoryDraftStore) Save(_ context.Context, _ values.TenantId, request designeredit.SaveRequest) (designeredit.Draft, error) {
	if request.ExpectedRevision == 0 {
		if s.draft.DraftID != "" {
			return designeredit.Draft{}, designeredit.ErrConflict
		}
		s.draft = designeredit.Draft{DraftID: request.DraftID, WorkflowID: request.WorkflowID, AuthorRef: request.AuthorRef, SemanticVersion: request.SemanticVersion, BaseVersionDigest: request.BaseVersionDigest, Revision: 1, Document: append(json.RawMessage(nil), request.Document...), ExpiresAt: request.ExpiresAt}
		return s.draft, nil
	}
	if s.draft.DraftID != request.DraftID || s.draft.AuthorRef != request.AuthorRef || s.draft.Revision != request.ExpectedRevision {
		return designeredit.Draft{}, designeredit.ErrConflict
	}
	s.draft.WorkflowID, s.draft.SemanticVersion, s.draft.Document, s.draft.Revision = request.WorkflowID, request.SemanticVersion, append(json.RawMessage(nil), request.Document...), s.draft.Revision+1
	return s.draft, nil
}

type fixedCatalog []designerpalette.Entry

func (c fixedCatalog) List(context.Context, values.TenantId) []designerpalette.Entry { return c }

func TestTodo_WF_UI_005_DurableFragmentInsertion(t *testing.T) {
	store := &memoryDraftStore{}
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	service := designeredit.Service{
		Store: store, NewID: func() (string, error) { return "01999f37-9f42-7000-8000-000000000005", nil }, Now: func() time.Time { return now },
		Catalog: fixedCatalog{{ID: "fragment.review", Version: 1, Name: "Review", Kind: designerpalette.KindFragment, Expansion: designerpalette.Expansion{Nodes: []workflow.Node{{ID: "manager", Type: workflow.StepApproval}}}}},
	}
	created, err := service.Create(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.CreateRequest{Name: "Employee change"})
	if err != nil || created.Draft.Revision != 1 || created.Draft.Name != "Employee change" {
		t.Fatalf("Create() = %+v, %v", created, err)
	}
	if created.Draft.SemanticVersion != "0.1.0" {
		t.Fatalf("default semantic version = %q, want 0.1.0", created.Draft.SemanticVersion)
	}
	changed, err := service.Insert(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.InsertRequest{DraftID: created.Draft.DraftID, ExpectedRevision: 1, EntryID: "fragment.review", EntryVersion: 1})
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if changed.Draft.Revision != 2 || len(changed.Draft.Groups) != 1 || !changed.Draft.Groups[0].Collapsed || !reflect.DeepEqual(changed.Draft.Groups[0].NodeIDs, changed.InsertedNodeIDs) {
		t.Fatalf("changed draft = %+v", changed)
	}
	recovered, err := service.Get(context.Background(), values.TenantId("tenant-a"), "author-a", created.Draft.DraftID)
	if err != nil || !reflect.DeepEqual(recovered, changed.Draft) {
		t.Fatalf("Get() = %+v, %v; want %+v", recovered, err, changed.Draft)
	}
}

func TestTodo_WF_UI_005_SecurityCatalogAndOwnershipFailClosed(t *testing.T) {
	store := &memoryDraftStore{}
	service := designeredit.Service{Store: store, Catalog: fixedCatalog{}, NewID: func() (string, error) { return "01999f37-9f42-7000-8000-000000000006", nil }}
	created, err := service.Create(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(context.Background(), values.TenantId("tenant-a"), "author-b", created.Draft.DraftID); !errors.Is(err, designeredit.ErrNotFound) {
		t.Fatalf("cross-author Get() error = %v, want ErrNotFound", err)
	}
	if _, err := service.Insert(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.InsertRequest{DraftID: created.Draft.DraftID, ExpectedRevision: 1, EntryID: "fragment.denied", EntryVersion: 1}); !errors.Is(err, designeredit.ErrNotFound) {
		t.Fatalf("denied entry Insert() error = %v, want ErrNotFound", err)
	}
	if store.draft.Revision != 1 {
		t.Fatalf("denied insert advanced revision to %d", store.draft.Revision)
	}
}

func TestTodo_WF_UI_005_OptimisticRevisionPreventsLostEdit(t *testing.T) {
	store := &memoryDraftStore{}
	service := designeredit.Service{Store: store, Catalog: fixedCatalog{{ID: "kernel.task", Version: 1, Name: "Task", Kind: designerpalette.KindBlock, StepType: workflow.StepTask}}, NewID: func() (string, error) { return "01999f37-9f42-7000-8000-000000000007", nil }}
	created, err := service.Create(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	request := designeredit.InsertRequest{DraftID: created.Draft.DraftID, ExpectedRevision: 1, EntryID: "kernel.task", EntryVersion: 1}
	if _, err := service.Insert(context.Background(), values.TenantId("tenant-a"), "author-a", request); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Insert(context.Background(), values.TenantId("tenant-a"), "author-a", request); !errors.Is(err, designeredit.ErrConflict) {
		t.Fatalf("stale Insert() error = %v, want ErrConflict", err)
	}
}

func TestCreateCarriesSemanticReleaseIdentity(t *testing.T) {
	store := &memoryDraftStore{}
	service := designeredit.Service{Store: store, Catalog: fixedCatalog{}, NewID: func() (string, error) { return "01999f37-9f42-7000-8000-000000000012", nil }}
	created, err := service.Create(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.CreateRequest{
		WorkflowID: "workflow.people.change", SemanticVersion: "2.3.0-beta.1", BaseVersionDigest: "sha256:base",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Draft.SemanticVersion != "2.3.0-beta.1" || created.Draft.BaseVersionDigest != "sha256:base" {
		t.Fatalf("draft identity = %+v", created.Draft)
	}
	if _, err := service.Create(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.CreateRequest{SemanticVersion: "v2"}); !errors.Is(err, designeredit.ErrInvalid) {
		t.Fatalf("invalid semantic version error = %v, want ErrInvalid", err)
	}
}

func TestTodo_WF_UI_005_PromotionEditorDraftMatchesExecutableByteForByte(t *testing.T) {
	want := promotionexec.Definition()
	store := &memoryDraftStore{}
	service := designeredit.Service{
		Store: store,
		Catalog: fixedCatalog{{
			ID: "hcmnext.templates.promotion", Version: 1, Name: "Promotion", Kind: designerpalette.KindTemplate,
			Expansion: designerpalette.Expansion{Template: &want},
		}},
		NewID: func() (string, error) { return "01999f37-9f42-7000-8000-000000000013", nil },
	}
	created, err := service.Create(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.CreateRequest{
		WorkflowID: want.WorkflowID, SemanticVersion: "1.1.1", BaseVersionDigest: "sha256:published-plan",
		TemplateID: "hcmnext.templates.promotion", TemplateVersion: 1,
	})
	if err != nil {
		t.Fatalf("Create promotion template: %v", err)
	}
	wantDocument, err := workflow.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(store.draft.Document, wantDocument) {
		t.Fatal("stored promotion draft is not the canonical executable definition")
	}
	if created.Draft.StartNodeID != want.StartNodeID || created.Draft.DefinitionDigest != workflowversion.DefinitionDigest(want) ||
		len(created.Draft.Nodes) != len(want.Nodes) || len(created.Draft.Edges) != len(want.Edges) {
		t.Fatalf("promotion draft projection drifted from its canonical definition: %+v", created.Draft)
	}
}
