package projectservice

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
)

type lifecycleFakeStore struct {
	*fakeStore
	calls    int
	receipts map[string]TaskRecord
}

func (f *lifecycleFakeStore) PatchTask(_ context.Context, tenantID, projectID, taskID string, expected, workflow uint64, patch project.TaskPatch, actor, key string) (TaskRecord, error) {
	if result, ok := f.receipts[key]; ok {
		return result, nil
	}
	t := f.tasks[taskKey(projectID, taskID)]
	if t.Revision != expected {
		return TaskRecord{}, project.ErrRevisionConflict
	}
	f.calls++
	if patch.Title != nil {
		t.Title = *patch.Title
	}
	t.Revision = expected + 1
	f.tasks[taskKey(projectID, taskID)] = t
	f.receipts[key] = t
	return t, nil
}
func (f *lifecycleFakeStore) SetTaskArchived(_ context.Context, tenantID, projectID, taskID string, expected, workflow uint64, archived bool, actor, key string) (TaskRecord, error) {
	if result, ok := f.receipts[key]; ok {
		return result, nil
	}
	t := f.tasks[taskKey(projectID, taskID)]
	if t.Revision != expected {
		return TaskRecord{}, project.ErrRevisionConflict
	}
	f.calls++
	t.Archived, t.Revision = archived, expected+1
	f.tasks[taskKey(projectID, taskID)] = t
	f.receipts[key] = t
	return t, nil
}

func TestTodo_PM_008_TaskLifecycleAuthorizationAndRevisions(t *testing.T) {
	ctx := context.Background()
	principal := testPrincipal(t)
	store := &lifecycleFakeStore{fakeStore: newFakeStore(), receipts: map[string]TaskRecord{}}
	store.projects["p1"] = ProjectRecord{ID: "p1", TenantID: "tenant-a", State: project.LifecycleActive}
	store.tasks[taskKey("p1", "t1")] = TaskRecord{ID: "t1", TenantID: "tenant-a", ProjectID: "p1", Title: "Original", StatusID: "todo", TypeID: "task_default", Priority: "NORMAL", Revision: 2}
	auth := &fakeAuth{}
	service := Service{Auth: auth, Commands: store, Reads: store, Workflows: &fakeWorkflowPort{version: 7}}
	title := "Revised"
	patch := PatchTaskRequest{ProjectID: "p1", TaskID: "t1", IdempotencyKey: "patch", ExpectedTaskRevision: 1, ExpectedWorkflowRevision: 7, Patch: project.TaskPatch{Title: &title}}
	if _, err := service.PatchTask(ctx, principal, patch); err == nil || store.calls != 0 {
		t.Fatalf("stale edit reached store: err=%v calls=%d", err, store.calls)
	}
	patch.ExpectedTaskRevision = 2
	auth.denied = true
	if _, err := service.PatchTask(ctx, principal, patch); err == nil || store.calls != 0 {
		t.Fatalf("denied edit reached store: err=%v calls=%d", err, store.calls)
	}
	auth.denied = false
	updated, err := service.PatchTask(ctx, principal, patch)
	if err != nil || updated.Title != title || updated.Revision != 3 || store.calls != 1 {
		t.Fatalf("patch=%+v err=%v calls=%d", updated, err, store.calls)
	}
	if replay, err := service.PatchTask(ctx, principal, patch); err != nil || replay.Revision != 3 || store.calls != 1 {
		t.Fatalf("patch retry=%+v err=%v calls=%d", replay, err, store.calls)
	}
	archive := SetTaskArchivedRequest{ProjectID: "p1", TaskID: "t1", IdempotencyKey: "archive", ExpectedTaskRevision: 3, ExpectedWorkflowRevision: 7}
	archived, err := service.ArchiveTask(ctx, principal, archive)
	if err != nil || !archived.Archived || archived.Revision != 4 || store.calls != 2 {
		t.Fatalf("archive=%+v err=%v calls=%d", archived, err, store.calls)
	}
	if replay, err := service.ArchiveTask(ctx, principal, archive); err != nil || !replay.Archived || store.calls != 2 {
		t.Fatalf("archive retry=%+v err=%v calls=%d", replay, err, store.calls)
	}
	archive.ExpectedTaskRevision = 4
	archive.IdempotencyKey = "restore"
	restored, err := service.RestoreTask(ctx, principal, archive)
	if err != nil || restored.Archived || restored.Revision != 5 || store.calls != 3 {
		t.Fatalf("restore=%+v err=%v calls=%d", restored, err, store.calls)
	}
	if _, err := service.RestoreTask(ctx, principal, SetTaskArchivedRequest{ProjectID: "p1", TaskID: "t1", IdempotencyKey: "stale-new", ExpectedTaskRevision: 3, ExpectedWorkflowRevision: 7}); !errors.Is(err, project.ErrRevisionConflict) || store.calls != 3 {
		t.Fatalf("stale new restore=%v calls=%d", err, store.calls)
	}
}

func TestArchivedTaskDoesNotEnterBoard(t *testing.T) {
	if boardTaskMatches(projectboard.AuthorizedTaskQuery{}, TaskRecord{ID: "archived", Archived: true, StatusID: "todo"}) {
		t.Fatal("archived task appeared on board")
	}
}
