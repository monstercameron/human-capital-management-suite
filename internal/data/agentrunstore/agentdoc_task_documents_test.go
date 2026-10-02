package agentrunstore

import (
	"context"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

func TestTodo_AGENTDOC_004_IntegrationStore(t *testing.T) {
	e := newEnv(t)
	store := e.tenantStore(t, "run-one")
	refs := []agentdocref.Reference{{DocumentID: "doc-12345678-1234-4123-8123-123456789abc", VersionMode: agentdocref.ModePinned, PinnedVersion: 3, SectionAnchor: "leave", Label: "Leave policy"}}
	ctx, err := agentrun.WithDocumentReferences(context.Background(), refs)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := agentrun.NewRuntime(store)
	if err != nil {
		t.Fatal(err)
	}
	task, err := runtime.CreateTask(ctx, agentrun.CreateRequest{ID: "task-doc", TenantID: "run-one", UserID: "user-1", Goal: "summarize", Plan: testPlan(t), Now: t0, ExpiresAt: t0.AddDate(0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	omissions := []agentdocref.Omission{{Label: "Leave policy", Reason: agentdocref.NotPublished}}
	if err := runtime.RecordDocumentOmissions(context.Background(), task.ID, refs, omissions); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Get(context.Background(), task.ID)
	if err != nil || stored.Version != task.Version || !reflect.DeepEqual(stored.Plan.DocumentReferences, refs) || !reflect.DeepEqual(stored.Plan.DocumentOmissions, omissions) {
		t.Fatalf("stored documents = refs %+v omissions %+v, %v", stored.Plan.DocumentReferences, stored.Plan.DocumentOmissions, err)
	}
	stored.Version++
	stored.UpdatedAt = stored.UpdatedAt.Add(1)
	stored.Plan.DocumentOmissions = nil
	if err := store.Save(context.Background(), stored, stored.Version-1); err != nil {
		t.Fatal(err)
	}
	after, err := store.Get(context.Background(), task.ID)
	if err != nil || !reflect.DeepEqual(after.Plan.DocumentOmissions, omissions) {
		t.Fatalf("ordinary save overwrote omissions = %+v, %v", after.Plan.DocumentOmissions, err)
	}
}
