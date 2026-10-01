package designeredit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
)

func (s *memoryDraftStore) Delete(_ context.Context, _ values.TenantId, request designeredit.DeleteRequest) error {
	if s.draft.DraftID != request.DraftID || s.draft.AuthorRef != request.AuthorRef {
		return designeredit.ErrNotFound
	}
	s.draft = designeredit.Draft{}
	return nil
}

func TestTodo_UXBLIND_081(t *testing.T) {
	store := &memoryDraftStore{}
	service := designeredit.Service{
		Store: store, Catalog: fixedCatalog{},
		NewID: func() (string, error) { return "draft-empty", nil },
	}
	created, err := service.Create(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(context.Background(), values.TenantId("tenant-a"), "author-b", created.Draft.DraftID); !errors.Is(err, designeredit.ErrNotFound) {
		t.Fatalf("wrong-author delete = %v, want ErrNotFound", err)
	}
	if err := service.Delete(context.Background(), values.TenantId("tenant-a"), "author-a", created.Draft.DraftID); err != nil {
		t.Fatalf("empty draft delete = %v", err)
	}
	if _, err := service.Get(context.Background(), values.TenantId("tenant-a"), "author-a", created.Draft.DraftID); !errors.Is(err, designeredit.ErrNotFound) {
		t.Fatalf("deleted draft Get() = %v, want ErrNotFound", err)
	}
}

func TestTodo_UXBLIND_081_SubmittedOrPublishedDraftCannotDelete(t *testing.T) {
	store := &memoryDraftStore{}
	service := designeredit.Service{
		Store:   store,
		Catalog: fixedCatalog{{ID: "task", Version: 1, Name: "Task", Kind: designerpalette.KindBlock, StepType: workflow.StepTask}},
		NewID:   func() (string, error) { return "draft-authored", nil },
	}
	created, err := service.Create(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Insert(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.InsertRequest{DraftID: created.Draft.DraftID, ExpectedRevision: 1, EntryID: "task", EntryVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(context.Background(), values.TenantId("tenant-a"), "author-a", created.Draft.DraftID); !errors.Is(err, designeredit.ErrNotDeletable) {
		t.Fatalf("authored draft delete = %v, want ErrNotDeletable", err)
	}
	if _, err := service.Get(context.Background(), values.TenantId("tenant-a"), "author-a", created.Draft.DraftID); err != nil {
		t.Fatalf("authored draft disappeared after refused delete: %v", err)
	}
}
