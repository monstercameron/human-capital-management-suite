package project

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

type allowTransition struct {
	called   bool
	revision uint64
	edits    []TaskFieldEdit
}

func (p *allowTransition) ValidateTaskTransition(_ ProjectID, from, to string, configRevision uint64, edits []TaskFieldEdit) error {
	p.called = true
	p.edits = append([]TaskFieldEdit(nil), edits...)
	if from != "status.todo" || to != "status.doing" || configRevision != p.revision {
		return ErrInvalidState
	}
	return nil
}

func fixture(t *testing.T) Project {
	t.Helper()
	p, err := NewProject("project-1", "tenant-1", "owner-1", "Launch", "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTodo_PM_008_ProjectSettingsRevision(t *testing.T) {
	p := fixture(t)
	updated, err := p.UpdateSettings("Launch 2", "Europe/Paris", 1)
	if err != nil || updated.Name != "Launch 2" || updated.Timezone != "Europe/Paris" || updated.Revision != 2 {
		t.Fatalf("settings update = %+v, err=%v", updated, err)
	}
	if _, err := updated.UpdateSettings("Stale", "UTC", 1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale settings revision error = %v", err)
	}
	if _, err := p.UpdateSettings("Launch", "Invalid/Timezone", 1); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("invalid timezone error = %v", err)
	}
}

func TestTodo_PM_002(t *testing.T) {
	p := fixture(t)
	task, err := NewTask("task-1", p, "Prepare rollout", "status.todo")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID == TaskID(p.ID) || task.ProjectID != p.ID || task.TenantID != p.TenantID || task.Revision != 1 {
		t.Fatalf("task identity/ownership/revision = %+v", task)
	}
	updated, err := task.ReviseTask(p, TaskUpdate{Title: "Prepare launch", Description: "Check readiness", AssigneeID: "person-1"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != task.ID || updated.ProjectID != task.ProjectID || updated.Revision != 2 || updated.Title != "Prepare launch" {
		t.Fatalf("revised task = %+v", updated)
	}
	if _, err := updated.ReviseTask(p, TaskUpdate{Title: "Stale write"}, 1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision error = %v", err)
	}
}

func TestTodo_PM_002_Conformance(t *testing.T) {
	p := fixture(t)
	operatorSuspended, err := p.TransitionProject(LifecycleSuspended, 1, "incident-operator", RoleOperator, "security incident")
	if err != nil {
		t.Fatal(err)
	}
	if operatorSuspended.CanWrite() || operatorSuspended.Revision != 2 || len(operatorSuspended.History) != 1 {
		t.Fatalf("suspended project = %+v", operatorSuspended)
	}
	if _, err := NewTask("task-blocked", operatorSuspended, "Blocked", "status.todo"); !errors.Is(err, ErrWriteUnavailable) {
		t.Fatalf("create in suspended project error = %v", err)
	}
	active, err := operatorSuspended.TransitionProject(LifecycleActive, 2, "owner-1", RoleOwner, "")
	if err != nil {
		t.Fatal(err)
	}
	archived, err := active.TransitionProject(LifecycleArchived, 3, "owner-1", RoleOwner, "")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := archived.TransitionProject(LifecycleActive, 4, "owner-1", RoleOwner, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.History) != 4 || restored.Revision != 5 || restored.State != LifecycleActive {
		t.Fatalf("restored project did not retain lifecycle history: %+v", restored)
	}
	task, err := NewTask("task-2", restored, "Prepare rollout", "status.todo")
	if err != nil {
		t.Fatal(err)
	}
	archivedTask, err := task.Archive(restored, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !archivedTask.Archived || archivedTask.ID != task.ID || archivedTask.Revision != 2 {
		t.Fatalf("archived task = %+v", archivedTask)
	}
	restoredTask, err := archivedTask.Restore(restored, 2)
	if err != nil {
		t.Fatal(err)
	}
	if restoredTask.Archived || restoredTask.Revision != 3 {
		t.Fatalf("restored task = %+v", restoredTask)
	}
}

func TestTodo_PM_002_Security(t *testing.T) {
	p := fixture(t)
	if _, err := p.TransitionProject(LifecycleSuspended, 1, "ordinary-member", RoleOwner, "incident"); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("non-operator suspension error = %v", err)
	}
	if _, err := p.TransitionProject(LifecycleArchived, 1, "ordinary-member", RoleOwner, ""); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("non-owner archive error = %v", err)
	}
	task, err := NewTask("project-card-7", p, "Coordinate approval", "status.todo")
	if err != nil {
		t.Fatal(err)
	}
	workItem := WorkItemProjection{ID: "hcm-work-88", ObservedVersion: 8, SafeStatus: "PENDING", Freshness: "CURRENT"}
	linked, err := task.WithWorkItemProjection(workItem)
	if err != nil {
		t.Fatal(err)
	}
	workItem.SafeStatus = "COMPLETED"
	if linked.WorkItem.SafeStatus != "PENDING" {
		t.Fatalf("projection aliases caller data: %+v", linked.WorkItem)
	}
	policy := &allowTransition{revision: 12}
	fieldEdit := TaskFieldEdit{FieldID: "field.region", Type: "TEXT", CanonicalValue: "west"}
	moved, err := linked.TransitionTask(p, policy, "status.doing", 1, 12, []TaskFieldEdit{fieldEdit})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.called || len(policy.edits) != 1 || policy.edits[0] != fieldEdit || moved.Status != "status.doing" || moved.Revision != 2 || moved.WorkItem.SafeStatus != "PENDING" || moved.Fields[fieldEdit.FieldID] != fieldEdit {
		t.Fatalf("project transition altered authority boundary: task=%+v policy=%+v", moved, policy)
	}
	denied := &allowTransition{revision: 11}
	if _, err := moved.TransitionTask(p, denied, "status.done", 2, 12, nil); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("invalid configured transition error = %v", err)
	}
	suspended, err := p.TransitionProject(LifecycleSuspended, 1, "operator", RoleOperator, "incident")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := moved.TransitionTask(suspended, policy, "status.done", 2, 12, nil); !errors.Is(err, ErrWriteUnavailable) {
		t.Fatalf("move in suspended project error = %v", err)
	}
	if moved.Status != "status.doing" || moved.Revision != 2 {
		t.Fatal("failed transition mutated source task")
	}
	if _, err := task.WithWorkItemProjection(WorkItemProjection{ID: "hcm-work-88"}); !errors.Is(err, ErrInvalidWorkItemRef) {
		t.Fatalf("incomplete WorkItem projection error = %v", err)
	}
}

func TestTodo_PM_010(t *testing.T) {
	p := fixture(t)
	task, err := NewTaskWithDetails("task-due", p, "Prepare launch", "status.active", "type.delivery", PriorityHigh)
	if err != nil {
		t.Fatal(err)
	}
	task.DueDate = "2026-03-08"
	cases := []struct {
		name        string
		dueDate     string
		now         time.Time
		wantDate    string
		wantOverdue bool
	}{
		{name: "last second of spring-forward due date", dueDate: "2026-03-08", now: time.Date(2026, 3, 9, 3, 59, 59, 0, time.UTC), wantDate: "2026-03-08"},
		{name: "first instant after spring-forward due date", dueDate: "2026-03-08", now: time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC), wantDate: "2026-03-09", wantOverdue: true},
		{name: "last second of fall-back due date", dueDate: "2026-11-01", now: time.Date(2026, 11, 2, 4, 59, 59, 0, time.UTC), wantDate: "2026-11-01"},
		{name: "first instant after fall-back due date", dueDate: "2026-11-01", now: time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC), wantDate: "2026-11-02", wantOverdue: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task.DueDate = tc.dueDate
			state, err := CalculateDueState(task, p, projectworkflow.CategoryActive, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			if state.AsOfDate != tc.wantDate || state.DueDate != task.DueDate || state.Overdue != tc.wantOverdue {
				t.Fatalf("CalculateDueState = %+v, want local date %q overdue=%v", state, tc.wantDate, tc.wantOverdue)
			}
		})
	}
	completed, err := CalculateDueState(task, p, projectworkflow.CategoryDone, cases[1].now)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Overdue {
		t.Fatal("DONE task reported overdue")
	}
	cancelled, err := CalculateDueState(task, p, projectworkflow.CategoryCancelled, cases[1].now)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Overdue {
		t.Fatal("CANCELLED task reported overdue")
	}
}

func TestTodo_PM_010_Golden(t *testing.T) {
	p := fixture(t)
	task, err := NewTask("task-golden", p, "Review", "status.active")
	if err != nil {
		t.Fatal(err)
	}
	task.DueDate = "2026-09-24"
	cases := []struct {
		at   string
		want string
		late bool
	}{
		{"2026-09-24T03:59:59Z", "2026-09-23", false},
		{"2026-09-24T04:00:00Z", "2026-09-24", false},
		{"2026-09-25T03:59:59Z", "2026-09-24", false},
		{"2026-09-25T04:00:00Z", "2026-09-25", true},
	}
	for _, tc := range cases {
		now, err := time.Parse(time.RFC3339, tc.at)
		if err != nil {
			t.Fatal(err)
		}
		state, err := CalculateDueState(task, p, projectworkflow.CategoryNotStarted, now)
		if err != nil {
			t.Fatal(err)
		}
		if state.AsOfDate != tc.want || state.Overdue != tc.late {
			t.Errorf("at %s: got %+v, want local date %s overdue=%v", tc.at, state, tc.want, tc.late)
		}
	}
}

func TestTodo_PM_010_Property(t *testing.T) {
	p := fixture(t)
	task, err := NewTask("task-property", p, "Follow up", "status.active")
	if err != nil {
		t.Fatal(err)
	}
	task.DueDate = "2026-05-15"
	for _, category := range []projectworkflow.StatusCategory{
		projectworkflow.CategoryNotStarted, projectworkflow.CategoryActive, projectworkflow.CategoryBlocked,
		projectworkflow.CategoryDone, projectworkflow.CategoryCancelled,
	} {
		for day := 13; day <= 17; day++ {
			now := time.Date(2026, 5, day, 12, 0, 0, 0, time.UTC)
			state, err := CalculateDueState(task, p, category, now)
			if err != nil {
				t.Fatal(err)
			}
			want := day > 15 && category != projectworkflow.CategoryDone && category != projectworkflow.CategoryCancelled
			if state.Overdue != want {
				t.Errorf("category=%s day=%d overdue=%v want %v", category, day, state.Overdue, want)
			}
		}
	}
	for _, due := range []string{"2026-2-03", "2026-02-30", "2026-02-03T00:00:00Z"} {
		task.DueDate = due
		if _, err := CalculateDueState(task, p, projectworkflow.CategoryActive, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)); !errors.Is(err, ErrInvalidDueDate) {
			t.Errorf("invalid due date %q error = %v", due, err)
		}
	}
	if _, err := CalculateDueState(task, p, projectworkflow.StatusCategory("UNKNOWN"), time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)); !errors.Is(err, ErrInvalidStatusCategory) {
		t.Fatalf("unknown category error = %v", err)
	}
	task.DueDate = ""
	unscheduled, err := CalculateDueState(task, p, projectworkflow.CategoryActive, time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC))
	if err != nil || unscheduled != (TaskDueState{}) {
		t.Fatalf("unscheduled due state = %+v, %v", unscheduled, err)
	}
	task.DueDate = "2026-05-15"
	badZone := p
	badZone.Timezone = "Mars/Olympus_Mons"
	if _, err := CalculateDueState(task, badZone, projectworkflow.CategoryActive, time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)); !errors.Is(err, ErrInvalidProjectZone) {
		t.Fatalf("invalid project timezone error = %v", err)
	}
	if _, err := NewProject("project-invalid-zone", "tenant-1", "owner-1", "Bad timezone", badZone.Timezone); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("invalid timezone project error = %v", err)
	}
}

func TestTaskPatchPreservesOmittedFields(t *testing.T) {
	p := fixture(t)
	task, err := NewTaskWithDetails("task-patch", p, "Design", "status.todo", "type.design", PriorityHigh)
	if err != nil {
		t.Fatal(err)
	}
	task.Description, task.AssigneeID, task.DueDate = "Existing description", "person-9", "2026-10-01"
	newTitle := "Updated title"
	patched, err := task.PatchTask(p, TaskPatch{Title: &newTitle}, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if patched.Title != newTitle || patched.Description != task.Description || patched.AssigneeID != task.AssigneeID || patched.DueDate != task.DueDate || patched.TypeID != task.TypeID || patched.Priority != task.Priority || patched.Revision != 2 {
		t.Fatalf("sparse patch cleared omitted fields: before=%+v after=%+v", task, patched)
	}
	clearedAssignee := ""
	cleared, err := patched.PatchTask(p, TaskPatch{AssigneeID: &clearedAssignee}, patched.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.AssigneeID != "" || cleared.Description != task.Description || cleared.Priority != PriorityHigh {
		t.Fatalf("explicit optional clear = %+v", cleared)
	}
	badPriority := Priority("SUPER")
	if _, err := cleared.PatchTask(p, TaskPatch{Priority: &badPriority}, cleared.Revision); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("invalid priority error = %v", err)
	}
	if _, err := NewTaskWithDetails("task-bad", p, "Invalid", "status.todo", "type.invalid", badPriority); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("invalid creation priority error = %v", err)
	}
	if _, err := NewTaskWithDetails("task-bad-type", p, "Invalid", "status.todo", "", PriorityNormal); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("missing creation type error = %v", err)
	}
}
