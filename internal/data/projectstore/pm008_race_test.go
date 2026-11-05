package projectstore

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// TestTodo_PM_008_Race proves concurrent archive commands cannot overwrite
// one another: exactly one wins and the loser observes a typed conflict.
func TestTodo_PM_008_Race(t *testing.T) {
	s, _ := projectFixture(t)
	ctx := context.Background()
	project := ProjectRecord{ID: "race-archive-project", TenantID: "tenant-race-archive", OwnerID: "owner", Name: "Race", Timezone: "UTC", Lifecycle: "ACTIVE", Revision: 1}
	if err := s.CreateProject(ctx, project, "owner", "HUMAN", "create-race-archive-project"); err != nil {
		t.Fatal(err)
	}
	seedCurrentWorkflow(t, s, project.TenantID, project.ID, "owner", 1)
	task := TaskRecord{ID: "race-archive-task", TenantID: project.TenantID, ProjectID: project.ID, Title: "Original", StatusID: "todo", TypeID: "task_default", Priority: "NORMAL", Revision: 1}
	if err := s.CreateTaskWithConfig(ctx, task, 1, "owner", "HUMAN", "create-race-archive-task"); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, key := range []string{"archive-first", "archive-second"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			<-start
			_, err := s.SetTaskArchived(ctx, project.TenantID, project.ID, task.ID, 1, 1, true, "owner", "HUMAN", key)
			results <- err
		}(key)
	}
	close(start)
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrRevisionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent archive error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent archives: successes=%d conflicts=%d", successes, conflicts)
	}
	got, err := s.GetTask(ctx, project.TenantID, project.ID, task.ID)
	if err != nil || got.Revision != 2 || !got.Archived {
		t.Fatalf("concurrent archive result=%+v err=%v", got, err)
	}
}
