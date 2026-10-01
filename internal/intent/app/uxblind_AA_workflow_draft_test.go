package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
)

type uxblindAADraftStore struct {
	draft   designeredit.Draft
	deleted bool
}

func (s *uxblindAADraftStore) Load(context.Context, values.TenantId, string) (designeredit.Draft, error) {
	if s.deleted {
		return designeredit.Draft{}, designeredit.ErrNotFound
	}
	return s.draft, nil
}

func (*uxblindAADraftStore) Save(context.Context, values.TenantId, designeredit.SaveRequest) (designeredit.Draft, error) {
	return designeredit.Draft{}, designeredit.ErrInvalid
}

func (s *uxblindAADraftStore) Delete(context.Context, values.TenantId, designeredit.DeleteRequest) error {
	s.deleted = true
	return nil
}

func TestTodo_UXBLIND_081_Server(t *testing.T) {
	document, err := workflow.Marshal(workflow.Definition{WorkflowID: "workflow.empty", Version: 1, Name: "Untitled workflow"})
	if err != nil {
		t.Fatal(err)
	}
	store := &uxblindAADraftStore{draft: designeredit.Draft{DraftID: "draft-1", AuthorRef: "author-1", Document: document}}
	cell := &Cell{WorkflowDraftAuthoring: &designeredit.Service{Store: store}}
	if err := cell.DeleteWorkflowDraft(context.Background(), values.TenantId("tenant-a"), "author-1", "draft-1"); err != nil {
		t.Fatalf("DeleteWorkflowDraft = %v", err)
	}
	if !store.deleted {
		t.Fatal("application delete did not reach the owned draft store")
	}

	nodesDocument, err := workflow.Marshal(workflow.Definition{WorkflowID: "workflow.authored", Version: 1, Name: "Authored", Nodes: []workflow.Node{{ID: "step-1", Type: workflow.StepTask}}})
	if err != nil {
		t.Fatal(err)
	}
	store = &uxblindAADraftStore{draft: designeredit.Draft{DraftID: "draft-2", AuthorRef: "author-1", Document: nodesDocument}}
	cell.WorkflowDraftAuthoring = &designeredit.Service{Store: store}
	if err := cell.DeleteWorkflowDraft(context.Background(), values.TenantId("tenant-a"), "author-1", "draft-2"); !errors.Is(err, designeredit.ErrNotDeletable) {
		t.Fatalf("authored DeleteWorkflowDraft = %v, want ErrNotDeletable", err)
	}
	if store.deleted {
		t.Fatal("application delete removed an authored draft")
	}
}
