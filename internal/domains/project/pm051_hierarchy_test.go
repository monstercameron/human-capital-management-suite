package project

import (
	"errors"
	"testing"
)

func pm051Tasks(projectID ProjectID, ids ...TaskID) []HierarchyTask {
	tasks := make([]HierarchyTask, 0, len(ids))
	for _, id := range ids {
		tasks = append(tasks, HierarchyTask{ID: id, ProjectID: projectID})
	}
	return tasks
}

func TestTodo_PM_051(t *testing.T) {
	hierarchy, err := NewTaskHierarchy("project-1")
	if err != nil {
		t.Fatal(err)
	}
	tasks := pm051Tasks("project-1", "parent", "child-a", "child-b")
	hierarchy, err = hierarchy.SetParent(hierarchy.Revision, "child-a", "parent", tasks)
	if err != nil {
		t.Fatal(err)
	}
	hierarchy, err = hierarchy.SetParent(hierarchy.Revision, "child-b", "parent", tasks)
	if err != nil {
		t.Fatal(err)
	}
	rollup, err := hierarchy.Rollup("parent", map[TaskID]string{"child-a": FlowDone, "child-b": FlowActive}, map[string]bool{FlowDone: true}, RollupPercentComplete)
	if err != nil || rollup.DirectChildCount != 2 || rollup.CompletedChildCount != 1 || rollup.PercentComplete != 50 || rollup.MutatesParent {
		t.Fatalf("bounded rollup = %+v err=%v", rollup, err)
	}
}

func TestTodo_PM_051_Property(t *testing.T) {
	hierarchy, _ := NewTaskHierarchy("p")
	tasks := pm051Tasks("p", "a", "b", "c", "d")
	links := [][2]TaskID{{"b", "a"}, {"c", "b"}, {"d", "c"}}
	for _, link := range links {
		var err error
		hierarchy, err = hierarchy.SetParent(hierarchy.Revision, link[0], link[1], tasks)
		if err != nil {
			t.Fatalf("valid link rejected: %v", err)
		}
	}
	if _, err := hierarchy.SetParent(hierarchy.Revision, "a", "d", tasks); !errors.Is(err, ErrHierarchyCycle) {
		t.Fatalf("cycle error = %v", err)
	}
}

func BenchmarkTodo_PM_051(t *testing.B) {
	hierarchy, _ := NewTaskHierarchy("p")
	tasks := make([]HierarchyTask, MaxTaskHierarchyChildren+1)
	tasks[0] = HierarchyTask{ID: "parent", ProjectID: "p"}
	for i := 1; i < len(tasks); i++ {
		tasks[i] = HierarchyTask{ID: TaskID("child" + string(rune(i))), ProjectID: "p"}
	}
	t.ResetTimer()
	for i := 1; i < len(tasks); i++ {
		var err error
		hierarchy, err = hierarchy.SetParent(hierarchy.Revision, tasks[i].ID, "parent", tasks)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_PM_051_Security(t *testing.T) {
	hierarchy, _ := NewTaskHierarchy("project-1")
	_, err := hierarchy.SetParent(hierarchy.Revision, "child", "parent", []HierarchyTask{{ID: "child", ProjectID: "project-2"}, {ID: "parent", ProjectID: "project-1"}})
	if !errors.Is(err, ErrHierarchyScope) {
		t.Fatalf("cross-project parent accepted: %v", err)
	}
}
