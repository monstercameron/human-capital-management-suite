package projectstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_PM_035(t *testing.T) {
	store, _ := projectFixture(t)
	ctx := context.Background()
	project := ProjectRecord{ID: "pm035-project", TenantID: "tenant-pm035", OwnerID: "owner", Name: "Restore proof", Timezone: "UTC", Lifecycle: "ACTIVE", Revision: 1}
	if err := store.CreateProject(ctx, project, "owner", "HUMAN", "pm035-project"); err != nil {
		t.Fatal(err)
	}
	seedCurrentWorkflow(t, store, project.TenantID, project.ID, "owner", 1)
	task := TaskRecord{ID: "pm035-task", TenantID: project.TenantID, ProjectID: project.ID, Title: "Rebuild search", StatusID: "todo", TypeID: "task_default", Priority: "HIGH", Revision: 1}
	if err := store.CreateTaskWithConfig(ctx, task, 1, "owner", "HUMAN", "pm035-task"); err != nil {
		t.Fatal(err)
	}
	moved, err := store.MoveTask(ctx, project.TenantID, project.ID, task.ID, "doing", 1, 1, nil, "owner", "HUMAN", "pm035-move")
	if err != nil || moved.Revision != 2 || moved.StatusID != "doing" {
		t.Fatalf("committed task state = %+v, err=%v", moved, err)
	}
	archived, err := store.SetTaskArchived(ctx, project.TenantID, project.ID, task.ID, 2, 1, true, "owner", "HUMAN", "pm035-archive")
	if err != nil || !archived.Archived || archived.Revision != 3 {
		t.Fatalf("archive = %+v, err=%v", archived, err)
	}
	restored, err := store.SetTaskArchived(ctx, project.TenantID, project.ID, task.ID, 3, 1, false, "owner", "HUMAN", "pm035-restore")
	if err != nil || restored.Archived || restored.Revision != 4 {
		t.Fatalf("restore = %+v, err=%v", restored, err)
	}
	listed, err := store.ListTasksFiltered(ctx, project.TenantID, project.ID, "", 100, TaskQuery{StatusIDs: []string{"doing"}, RequireStatusScope: true, ExcludeArchived: true})
	if err != nil || len(listed) != 1 || listed[0].ID != task.ID {
		t.Fatalf("rebuilt authorized task view = %+v, err=%v", listed, err)
	}
	snapshots, err := store.ActiveTaskSnapshots(ctx, project.TenantID, project.ID)
	if err != nil || len(snapshots) != 1 || snapshots[0].ID != task.ID || snapshots[0].StatusID != "doing" {
		t.Fatalf("rebuilt migration snapshot = %+v, err=%v", snapshots, err)
	}
	var outbox int
	if err := store.RunTenantTx(ctx, project.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM project_outbox WHERE tenant_id=$1 AND project_id=$2`, project.TenantID, project.ID).Scan(&outbox)
	}); err != nil {
		t.Fatal(err)
	}
	if outbox != 5 {
		t.Fatalf("recovery mutations emitted %d outbox events, want 5", outbox)
	}
}

func TestTodo_PM_035_Recovery(t *testing.T) {
	store, _ := projectFixture(t)
	ctx := context.Background()
	project := ProjectRecord{ID: "pm035-recovery", TenantID: "tenant-pm035-recovery", OwnerID: "owner", Name: "Recovery", Timezone: "UTC", Lifecycle: "ACTIVE", Revision: 1}
	if err := store.CreateProject(ctx, project, "owner", "HUMAN", "pm035-recovery-project"); err != nil {
		t.Fatal(err)
	}
	seedCurrentWorkflow(t, store, project.TenantID, project.ID, "owner", 1)
	task := TaskRecord{ID: "pm035-recovery-task", TenantID: project.TenantID, ProjectID: project.ID, Title: "Retained task", StatusID: "todo", TypeID: "task_default", Revision: 1}
	if err := store.CreateTaskWithConfig(ctx, task, 1, "owner", "HUMAN", "pm035-recovery-task"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetTaskArchived(ctx, project.TenantID, project.ID, task.ID, 1, 1, true, "owner", "HUMAN", "pm035-recovery-archive"); err != nil {
		t.Fatal(err)
	}
	archived, err := store.GetTask(ctx, project.TenantID, project.ID, task.ID)
	if err != nil || !archived.Archived || archived.ID != task.ID {
		t.Fatalf("archived committed task was not recoverable: %+v, err=%v", archived, err)
	}
	if _, err := store.SetTaskArchived(ctx, project.TenantID, project.ID, task.ID, 1, 1, false, "owner", "HUMAN", "pm035-recovery-stale"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale restore error = %v", err)
	}
}

func TestTodo_PM_035_Fault(t *testing.T) {
	store, _ := projectFixture(t)
	ctx := context.Background()
	project := ProjectRecord{ID: "pm035-fault", TenantID: "tenant-pm035-fault", OwnerID: "owner", Name: "Fault", Timezone: "UTC", Lifecycle: "ACTIVE", Revision: 1}
	if err := store.CreateProject(ctx, project, "owner", "HUMAN", "pm035-fault-project"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListTasksFiltered(ctx, project.TenantID, project.ID, "", 102, TaskQuery{}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("oversized derived-view page error = %v", err)
	}
}
