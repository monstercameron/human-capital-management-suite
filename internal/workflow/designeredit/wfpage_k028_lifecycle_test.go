package designeredit

import (
	"context"
	"errors"
	"testing"

	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func pageDraftFixture() PageOverrideDraft {
	return PageOverrideDraft{DraftID: "draft-1", WorkflowID: "workflow.change", WorkflowVersion: 4, PageID: "workflow.change.input", BaseWorkflowDigest: "sha256:wf4", GeneratedDefaultDigest: "sha256:default", OverrideDigest: "sha256:override", Bindings: []workflowversion.PageBinding{{Path: "person.name", Type: "STRING"}}}
}

func TestTodo_WFPAGE_028(t *testing.T) {
	store := NewPageDraftStore()
	draft, err := store.Save(context.Background(), pageDraftFixture(), 0)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := ValidatePageOverride(draft, []workflowversion.PageBinding{{Path: "person.name", Type: "STRING"}})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := ApprovePageOverride(validated, "reviewer-1")
	if err != nil {
		t.Fatal(err)
	}
	publication, err := store.Publish(context.Background(), approved, 1, "publisher-1", []workflowversion.PageBinding{{Path: "person.name", Type: "STRING"}}, false)
	if err != nil || publication.PageVersion != 1 || publication.WorkflowVersion != 4 {
		t.Fatalf("publication = %+v, err = %v", publication, err)
	}
}

func TestTodo_WFPAGE_028_Security(t *testing.T) {
	store := NewPageDraftStore()
	draft, _ := store.Save(context.Background(), pageDraftFixture(), 0)
	if _, err := store.Save(context.Background(), draft, 0); !errors.Is(err, ErrPageDraftConflict) {
		t.Fatalf("stale/duplicate draft save = %v, want ErrPageDraftConflict", err)
	}
	if _, err := ValidatePageOverride(draft, []workflowversion.PageBinding{{Path: "person.name", Type: "MONEY"}}); !errors.Is(err, workflowversion.ErrPageBindingUnresolved) {
		t.Fatalf("retyped page binding = %v, want unresolved binding", err)
	}
	rebased := RebasePageOverride(draft, draft.Bindings, append(append([]workflowversion.PageBinding(nil), draft.Bindings...), workflowversion.PageBinding{Path: "person.start", Type: "DATE"}))
	if _, err := ValidatePageOverride(rebased, append(append([]workflowversion.PageBinding(nil), draft.Bindings...), workflowversion.PageBinding{Path: "person.start", Type: "DATE"})); !errors.Is(err, workflowversion.ErrPageBindingUnresolved) {
		t.Fatalf("added input validation = %v, want unresolved binding", err)
	}
}

func TestTodo_WFPAGE_028_Fault(t *testing.T) {
	store := NewPageDraftStore()
	draft, _ := store.Save(context.Background(), pageDraftFixture(), 0)
	validated, _ := ValidatePageOverride(draft, draft.Bindings)
	approved, _ := ApprovePageOverride(validated, "reviewer-1")
	first, err := store.Publish(context.Background(), approved, 1, "publisher-1", draft.Bindings, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(context.Background(), approved, 2, "publisher-1", draft.Bindings, true); err == nil {
		t.Fatal("interrupted publish unexpectedly succeeded")
	}
	active, ok := store.Active(draft)
	if !ok || active != first {
		t.Fatalf("active after interrupted publish = %+v, want previous %+v", active, first)
	}
}
