package projectworkflow

import (
	"errors"
	"testing"
)

func pm16Config() Config {
	return Config{
		TaskTypes:   []TaskType{{ID: "task_default", Name: "Task", InitialStatus: "todo"}},
		Statuses:    []Status{{ID: "todo", Name: "To do", Category: CategoryNotStarted, AllowedNextStatusIDs: []string{"done"}}, {ID: "done", Name: "Done", Category: CategoryDone}},
		Transitions: []Transition{{From: "todo", To: "done"}},
		Columns:     []Column{{ID: "todo", Name: "To do", StatusIDs: []string{"todo"}}, {ID: "done", Name: "Done", StatusIDs: []string{"done"}}},
	}
}

func TestTodo_PM_060(t *testing.T) {
	config := pm16Config()
	migration, err := NewResumableMigration("tenant-1", "project-1", 3, 4, config, config, []TaskSnapshot{{ID: "task-2", TypeID: "task_default", StatusID: "todo", Revision: 2}, {ID: "task-1", TypeID: "task_default", StatusID: "todo", Revision: 1}}, MigrationMappings{Statuses: map[string]string{"todo": "done"}})
	if err != nil {
		t.Fatal(err)
	}
	if migration.Phase != MigrationPhaseMigrating || migration.AllowsWrite(3) || migration.AllowsWrite(4) {
		t.Fatalf("migration did not fence both epochs: %+v", migration)
	}
	migration, err = migration.ApplyBatch(migration.Epoch, 1, "batch-1")
	if err != nil || len(migration.AppliedTaskIDs) != 1 || migration.NextTask != 1 {
		t.Fatalf("first checkpoint = %+v, err=%v", migration, err)
	}
	replayed, err := migration.ApplyBatch(migration.Epoch-1, 1, "batch-1")
	if err != nil || replayed.NextTask != migration.NextTask {
		t.Fatalf("replaying checkpoint was not idempotent: %+v, err=%v", replayed, err)
	}
	migration, err = migration.ApplyBatch(migration.Epoch, 1, "batch-2")
	if err != nil {
		t.Fatal(err)
	}
	migration, err = migration.Activate(migration.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	if migration.Phase != MigrationPhaseActivated || !migration.AllowsWrite(4) || migration.AllowsWrite(3) {
		t.Fatalf("activation did not atomically open the pending epoch: %+v", migration)
	}
}

func TestTodo_PM_060_Fault(t *testing.T) {
	config := pm16Config()
	migration, err := NewResumableMigration("tenant-1", "project-1", 1, 2, config, config, []TaskSnapshot{{ID: "task-1", TypeID: "task_default", StatusID: "todo", Revision: 1}}, MigrationMappings{Statuses: map[string]string{"todo": "done"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migration.ApplyBatch(migration.Epoch+1, 1, "batch"); !errors.Is(err, ErrMigrationCheckpoint) {
		t.Fatalf("stale epoch err=%v, want checkpoint error", err)
	}
	if _, err := migration.Activate(migration.Epoch); !errors.Is(err, ErrMigrationNotComplete) {
		t.Fatalf("partial activation err=%v, want incomplete error", err)
	}
}

func TestTodo_PM_060_Recovery(t *testing.T) {
	config := pm16Config()
	migration, err := NewResumableMigration("tenant-1", "project-1", 1, 2, config, config, []TaskSnapshot{{ID: "task-1", TypeID: "task_default", StatusID: "todo", Revision: 1}, {ID: "task-2", TypeID: "task_default", StatusID: "todo", Revision: 1}}, MigrationMappings{Statuses: map[string]string{"todo": "done"}})
	if err != nil {
		t.Fatal(err)
	}
	migration, err = migration.ApplyBatch(migration.Epoch, 1, "checkpoint-before-crash")
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := migration.ApplyBatch(migration.Epoch, 1, "checkpoint-after-restart")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.NextTask != 2 || len(resumed.AppliedTaskIDs) != 2 {
		t.Fatalf("resume lost checkpoint progress: %+v", resumed)
	}
}
