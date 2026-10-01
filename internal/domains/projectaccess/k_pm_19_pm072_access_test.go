package projectaccess

import (
	"errors"
	"testing"
)

func pm072Project() Project {
	return Project{Tenant: "tenant-a", ID: "project-a", Memberships: []Membership{
		{Tenant: "tenant-a", User: "owner", Role: RoleOwner, State: MembershipActive},
		{Tenant: "tenant-a", User: "viewer", Role: RoleViewer, State: MembershipActive},
	}}
}

func pm072Grant(fields []string, allowed bool) *TaskGrant {
	return &TaskGrant{TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-secret", UserID: "viewer", Allowed: allowed, FieldIDs: fields, Revision: 2}
}

func TestTodo_PM_072(t *testing.T) {
	policy := FieldPolicyFunc(func(fieldID string) bool { return fieldID != "salary" })
	decision, err := AuthorizeTask(pm072Project(), "viewer", "tenant-a", "task-secret", ReadTask, pm072Grant([]string{"title", "salary"}, true), []string{"title", "salary"}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed || !decision.Fields["title"].Allowed || decision.Fields["salary"].Allowed {
		t.Fatalf("composed decision = %+v", decision)
	}
	records := []TaskSurfaceRecord{{TaskID: "task-secret", Title: "Visible title", StatusID: "todo", Revision: 4, FieldValues: map[string]string{"title": "safe", "salary": "hidden"}}}
	decisions := map[string]TaskAccessDecision{"task-secret": decision}
	for _, surface := range []DerivedSurface{DerivedSurfaceBoard, DerivedSurfaceSearch, DerivedSurfaceActivity, DerivedSurfaceNotification, DerivedSurfaceExport, DerivedSurfaceAgentPrompt, DerivedSurfaceIntegration} {
		views, err := ProjectSurfaceBatch(surface, records, decisions)
		if err != nil {
			t.Fatalf("%s: %v", surface, err)
		}
		if len(views) != 1 || views[0].FieldValues["salary"] != "" || len(views[0].FieldValues) != 1 {
			t.Fatalf("%s leaked field projection: %+v", surface, views)
		}
		if (surface == DerivedSurfaceActivity || surface == DerivedSurfaceNotification) && views[0].Revision != 0 {
			t.Fatalf("%s leaked source revision: %+v", surface, views[0])
		}
	}
}

func TestTodo_PM_072_Security(t *testing.T) {
	policy := FieldPolicyFunc(func(string) bool { return true })
	decision, err := AuthorizeTask(pm072Project(), "viewer", "tenant-a", "task-secret", ReadTask, pm072Grant(nil, false), []string{"title"}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed || len(decision.Fields) != 0 {
		t.Fatalf("denied task retained metadata: %+v", decision)
	}
	records := []TaskSurfaceRecord{{TaskID: "task-secret", Title: "private", StatusID: "todo", Revision: 1, FieldValues: map[string]string{"title": "private"}}}
	views, err := ProjectSurfaceBatch(DerivedSurfaceBoard, records, map[string]TaskAccessDecision{"task-secret": decision})
	if err != nil || len(views) != 0 {
		t.Fatalf("hidden task affected board count: views=%+v err=%v", views, err)
	}
	views, err = ProjectSurfaceBatch(DerivedSurfaceBoard, records, nil)
	if err != nil || len(views) != 0 {
		t.Fatalf("missing decision affected board count: views=%+v err=%v", views, err)
	}
	if _, err := AuthorizeTask(pm072Project(), "viewer", "tenant-a", "task-secret", ReadTask, pm072Grant([]string{"title", "title"}, true), []string{"title"}, policy); !errors.Is(err, ErrInvalidTaskGrant) {
		t.Fatalf("duplicate grant fields error = %v", err)
	}
}

func TestTodo_PM_072_Conformance(t *testing.T) {
	policy := FieldPolicyFunc(func(fieldID string) bool { return fieldID == "title" })
	decision, err := AuthorizeTask(pm072Project(), "viewer", "tenant-a", "task-secret", ReadTask, pm072Grant(nil, true), []string{"title", "confidential"}, policy)
	if err != nil {
		t.Fatal(err)
	}
	records := []TaskSurfaceRecord{
		{TaskID: "task-secret", Title: "Secret task", StatusID: "todo", Revision: 1, FieldValues: map[string]string{"title": "safe", "confidential": "secret"}},
		{TaskID: "task-public", Title: "public task", StatusID: "todo", Revision: 1, FieldValues: map[string]string{"title": "safe"}},
	}
	decisions := map[string]TaskAccessDecision{"task-secret": decision, "task-public": {TaskID: "task-public", Allowed: false}}
	for _, surface := range []DerivedSurface{DerivedSurfaceBoard, DerivedSurfaceSearch, DerivedSurfaceActivity, DerivedSurfaceNotification, DerivedSurfaceExport, DerivedSurfaceAgentPrompt, DerivedSurfaceIntegration} {
		views, err := ProjectSurfaceBatch(surface, records, decisions)
		if err != nil {
			t.Fatalf("%s: %v", surface, err)
		}
		if len(views) != 1 || views[0].TaskID != "task-secret" || views[0].FieldValues["confidential"] != "" {
			t.Fatalf("%s conformance failed: %+v", surface, views)
		}
	}
}

func TestTodo_PM_072_Golden(t *testing.T) {
	policy := FieldPolicyFunc(func(string) bool { return true })
	decision, err := AuthorizeTask(pm072Project(), "viewer", "tenant-a", "task-secret", ReadTask, pm072Grant([]string{"title"}, true), []string{"title", "secret"}, policy)
	if err != nil {
		t.Fatal(err)
	}
	view, visible, err := ProjectSurface(DerivedSurfaceExport, TaskSurfaceRecord{TaskID: "task-secret", Title: "Task", StatusID: "todo", Revision: 7, FieldValues: map[string]string{"title": "safe", "secret": "do not export"}}, decision)
	if err != nil || !visible {
		t.Fatalf("export projection = %+v visible=%v err=%v", view, visible, err)
	}
	if view.TaskID != "task-secret" || view.Title != "Task" || view.StatusID != "todo" || view.Revision != 7 || len(view.FieldValues) != 1 || view.FieldValues["title"] != "safe" {
		t.Fatalf("export projection golden mismatch: %+v", view)
	}
}
