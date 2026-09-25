package projectstore

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// Planning fields (labels, start date, story points) persist through
// PatchTask, the reporter is recorded at creation, and invalid values are
// refused without a write.
func TestTaskPlanningFieldsPersist(t *testing.T) {
	s, _ := projectFixture(t)
	ctx := context.Background()
	project := ProjectRecord{ID: "plan-project", TenantID: "tenant-plan", OwnerID: "owner", Name: "Plan", Timezone: "UTC", Lifecycle: "ACTIVE", Revision: 1}
	if err := s.CreateProject(ctx, project, "owner", "HUMAN", "create-plan-project"); err != nil {
		t.Fatal(err)
	}
	seedCurrentWorkflow(t, s, project.TenantID, project.ID, "owner", 1)
	task := TaskRecord{ID: "plan-task", TenantID: project.TenantID, ProjectID: project.ID, Title: "Plan the rollout", StatusID: "todo", TypeID: "task_default", Priority: "NORMAL", Revision: 1}
	if err := s.CreateTaskWithConfig(ctx, task, 1, "reporter-1", "HUMAN", "create-plan-task"); err != nil {
		t.Fatal(err)
	}
	created, err := s.GetTask(ctx, project.TenantID, project.ID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if created.CreatedBy != "reporter-1" || created.Labels == nil || len(created.Labels) != 0 || created.StartDate != nil || created.StoryPoints != 0 || created.CreatedAt.IsZero() {
		t.Fatalf("new task planning defaults = by %q labels %v start %v points %d created %v", created.CreatedBy, created.Labels, created.StartDate, created.StoryPoints, created.CreatedAt)
	}

	start, points, labels := "2026-10-01", int32(5), []string{"payroll", "Q4"}
	patched, err := s.PatchTask(ctx, project.TenantID, project.ID, task.ID, 1, 1, TaskPatch{StartDate: &start, StoryPoints: &points, Labels: &labels}, "owner", "HUMAN", "plan-patch-1")
	if err != nil {
		t.Fatal(err)
	}
	if patched.StartDate == nil || patched.StartDate.Format("2006-01-02") != start || patched.StoryPoints != 5 || !slices.Equal(patched.Labels, labels) || patched.Revision != 2 {
		t.Fatalf("patched planning fields = start %v points %d labels %v rev %d", patched.StartDate, patched.StoryPoints, patched.Labels, patched.Revision)
	}
	if patched.CreatedBy != "reporter-1" {
		t.Fatalf("patch changed the reporter to %q", patched.CreatedBy)
	}

	empty, clear := "", []string{}
	cleared, err := s.PatchTask(ctx, project.TenantID, project.ID, task.ID, 2, 1, TaskPatch{StartDate: &empty, Labels: &clear}, "owner", "HUMAN", "plan-patch-2")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.StartDate != nil || len(cleared.Labels) != 0 || cleared.StoryPoints != 5 {
		t.Fatalf("clear kept start %v labels %v, points %d", cleared.StartDate, cleared.Labels, cleared.StoryPoints)
	}

	tooMany := int32(1001)
	dupes := []string{"a", "A"}
	badDate := "2026-13-01"
	for name, patch := range map[string]TaskPatch{
		"points":     {StoryPoints: &tooMany},
		"duplicates": {Labels: &dupes},
		"date":       {StartDate: &badDate},
	} {
		if _, err := s.PatchTask(ctx, project.TenantID, project.ID, task.ID, 3, 1, patch, "owner", "HUMAN", "plan-bad-"+name); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("%s: err = %v, want ErrInvalidRecord", name, err)
		}
	}
}
