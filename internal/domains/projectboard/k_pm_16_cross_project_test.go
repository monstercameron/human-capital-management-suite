package projectboard

import (
	"errors"
	"testing"
)

func TestTodo_PM_062(t *testing.T) {
	view := CrossProjectView{ID: "view-1", Version: 2, TenantID: "tenant-1", ProjectIDs: []string{"project-a", "project-b"}, OrderBy: OrderTaskID}
	rows := []CrossProjectTask{{TenantID: "tenant-1", ProjectID: "project-a", ProjectName: "A", OwnerID: "owner-a", Authorized: true, Task: Task{ID: "task-a", Title: "A"}}, {TenantID: "tenant-1", ProjectID: "project-b", ProjectName: "B", OwnerID: "owner-b", Authorized: false, Task: Task{ID: "secret", Title: "Private"}}, {TenantID: "tenant-1", ProjectID: "project-b", ProjectName: "B", OwnerID: "owner-b", Authorized: true, Task: Task{ID: "task-b", Title: "B"}}}
	page, err := BuildCrossProjectPage(view, "tenant-1", "viewer", []CrossProjectScope{{ProjectID: "project-a", Granted: true}, {ProjectID: "project-b", Granted: true}}, rows, 1, nil)
	if err != nil || len(page.Tasks) != 1 || page.Tasks[0].Task.ID != "task-a" || page.Next == nil {
		t.Fatalf("bounded cross-project page = %+v, err=%v", page, err)
	}
	next, err := BuildCrossProjectPage(view, "tenant-1", "viewer", []CrossProjectScope{{ProjectID: "project-a", Granted: true}, {ProjectID: "project-b", Granted: true}}, rows, 1, page.Next)
	if err != nil || len(next.Tasks) != 1 || next.Tasks[0].Task.ID != "task-b" {
		t.Fatalf("stable cursor page = %+v, err=%v", next, err)
	}
}

func TestTodo_PM_062_Security(t *testing.T) {
	view := CrossProjectView{ID: "view-1", Version: 1, TenantID: "tenant-1", ProjectIDs: []string{"private"}}
	page, err := BuildCrossProjectPage(view, "tenant-1", "viewer", []CrossProjectScope{{ProjectID: "private", Granted: false}}, []CrossProjectTask{{TenantID: "tenant-1", ProjectID: "private", Authorized: true, Task: Task{ID: "secret"}}}, 10, nil)
	if err != nil || len(page.Tasks) != 0 {
		t.Fatalf("private project affected cross-project page: %+v, err=%v", page, err)
	}
	page, err = BuildCrossProjectPage(view, "tenant-1", "viewer", []CrossProjectScope{{ProjectID: "private", Granted: true}}, []CrossProjectTask{{TenantID: "tenant-1", ProjectID: "private", Authorized: true, Task: Task{ID: "secret"}}}, 1, nil)
	if err != nil || page.Next != nil {
		t.Fatalf("single private row unexpectedly paged: %+v, err=%v", page, err)
	}
	page.Next = &CrossProjectCursor{TenantID: "tenant-1", ViewerID: "other", ViewID: view.ID, ViewVersion: view.Version}
	if _, err := BuildCrossProjectPage(view, "tenant-1", "viewer", []CrossProjectScope{{ProjectID: "private", Granted: true}}, nil, 1, page.Next); !errors.Is(err, ErrInvalidCrossProjectPage) {
		t.Fatalf("forged cursor err=%v", err)
	}
}

func BenchmarkTodo_PM_062(b *testing.B) {
	view := CrossProjectView{ID: "view-1", Version: 1, TenantID: "tenant-1", ProjectIDs: []string{"p1", "p2"}}
	rows := make([]CrossProjectTask, 0, 200)
	for i := 0; i < 200; i++ {
		rows = append(rows, CrossProjectTask{TenantID: "tenant-1", ProjectID: []string{"p1", "p2"}[i%2], Authorized: true, Task: Task{ID: string(rune('a'+i%26)) + string(rune('a'+i/26))}})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = BuildCrossProjectPage(view, "tenant-1", "viewer", []CrossProjectScope{{ProjectID: "p1", Granted: true}, {ProjectID: "p2", Granted: true}}, rows, 50, nil)
	}
}
