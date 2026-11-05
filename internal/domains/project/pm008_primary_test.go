package project

import (
	"errors"
	"testing"
)

func pm008Project() Project {
	return Project{ID: "project-1", TenantID: "tenant-a", OwnerID: "owner-1", Name: "ordinary tasks", Timezone: "UTC", State: LifecycleActive, Revision: 1}
}

// TestTodo_PM_008 proves the GREEN lifecycle: project-owned task commands
// use stable IDs, expected revisions, and retained history across create,
// revise, archive, and restore.
func TestTodo_PM_008(t *testing.T) {
	project := pm008Project()
	task, err := NewTask("task-1", project, "Write the plan", "todo")
	if err != nil || task.Revision != 1 || task.Archived {
		t.Fatalf("create = %+v, err=%v", task, err)
	}
	revised, err := task.ReviseTask(project, TaskUpdate{Title: "Write the launch plan", Description: "Cover rollout", AssigneeID: "person-1", DueDate: "2026-10-02"}, 1)
	if err != nil || revised.Revision != 2 || revised.Title != "Write the launch plan" || revised.AssigneeID != "person-1" {
		t.Fatalf("revise = %+v, err=%v", revised, err)
	}
	if _, err := revised.ReviseTask(project, TaskUpdate{Title: "Stale edit"}, 1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revise error = %v", err)
	}
	archived, err := revised.Archive(project, 2)
	if err != nil || archived.Revision != 3 || !archived.Archived {
		t.Fatalf("archive = %+v, err=%v", archived, err)
	}
	if _, err := archived.ReviseTask(project, TaskUpdate{Title: "Edit archived"}, 3); !errors.Is(err, ErrWriteUnavailable) {
		t.Fatalf("archived revise error = %v", err)
	}
	restored, err := archived.Restore(project, 3)
	if err != nil || restored.Revision != 4 || restored.Archived {
		t.Fatalf("restore = %+v, err=%v", restored, err)
	}
	if restored.Title != "Write the launch plan" || restored.Description != "Cover rollout" || restored.ID != "task-1" || restored.ProjectID != project.ID {
		t.Fatalf("restore lost task history: %+v", restored)
	}
}
