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
	allDone, err := hierarchy.Rollup("parent", map[TaskID]string{"child-a": FlowDone, "child-b": FlowDone}, map[string]bool{FlowDone: true}, RollupAllChildrenDone)
	if err != nil || !allDone.ShouldMarkDone || allDone.MutatesParent {
		t.Fatalf("all-done rollup = %+v err=%v", allDone, err)
	}
	partial, err := hierarchy.Rollup("parent", map[TaskID]string{"child-a": FlowDone, "child-b": FlowActive}, map[string]bool{FlowDone: true}, RollupAllChildrenDone)
	if err != nil || partial.ShouldMarkDone {
		t.Fatalf("partial rollup = %+v err=%v", partial, err)
	}
	none, err := hierarchy.Rollup("parent", map[TaskID]string{"child-a": FlowDone, "child-b": FlowDone}, map[string]bool{FlowDone: true}, RollupNone)
	if err != nil || none.DirectChildCount != 2 || none.CompletedChildCount != 2 || none.ShouldMarkDone {
		t.Fatalf("none rollup = %+v err=%v", none, err)
	}
	if _, err := hierarchy.Rollup("parent", map[TaskID]string{}, map[string]bool{}, RollupRule("BOGUS")); !errors.Is(err, ErrInvalidHierarchy) {
		t.Fatalf("bogus rollup rule error = %v", err)
	}
	if _, err := hierarchy.SetParent(hierarchy.Revision, "child-a", "child-a", tasks); !errors.Is(err, ErrInvalidHierarchy) {
		t.Fatalf("self parent error = %v", err)
	}
	if _, err := hierarchy.SetParent(hierarchy.Revision+1, "child-a", "parent", tasks); !errors.Is(err, ErrHierarchyConflict) {
		t.Fatalf("stale revision error = %v", err)
	}
	detached, err := hierarchy.RemoveParent(hierarchy.Revision, "child-b")
	if err != nil {
		t.Fatal(err)
	}
	after, err := detached.Rollup("parent", map[TaskID]string{"child-a": FlowDone, "child-b": FlowDone}, map[string]bool{FlowDone: true}, RollupPercentComplete)
	if err != nil || after.DirectChildCount != 1 || after.PercentComplete != 100 {
		t.Fatalf("rollup after detach = %+v err=%v", after, err)
	}
	if _, err := detached.RemoveParent(detached.Revision, "child-b"); !errors.Is(err, ErrInvalidHierarchy) {
		t.Fatalf("double detach error = %v", err)
	}

	deep, err := NewTaskHierarchy("deep")
	if err != nil {
		t.Fatal(err)
	}
	chain := pm051Tasks("deep", "t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9", "t10", "t11", "t12", "t13", "t14", "t15", "t16", "t17", "t18", "t19", "t20", "t21", "t22", "t23", "t24", "t25", "t26", "t27", "t28", "t29", "t30", "t31", "t32", "t33")
	for i := 1; i < 32; i++ {
		if deep, err = deep.SetParent(deep.Revision, chain[i].ID, chain[i-1].ID, chain); err != nil {
			t.Fatalf("chain link %d rejected: %v", i, err)
		}
	}
	if _, err := deep.SetParent(deep.Revision, "t33", "t32", chain); !errors.Is(err, ErrHierarchyDepth) {
		t.Fatalf("33-deep link error = %v", err)
	}

	wide, err := NewTaskHierarchy("wide")
	if err != nil {
		t.Fatal(err)
	}
	fan := make([]HierarchyTask, MaxTaskHierarchyChildren+2)
	fan[0] = HierarchyTask{ID: "hub", ProjectID: "wide"}
	for i := 1; i < len(fan); i++ {
		fan[i] = HierarchyTask{ID: TaskID("leaf" + string(rune(i))), ProjectID: "wide"}
	}
	for i := 1; i <= MaxTaskHierarchyChildren; i++ {
		if wide, err = wide.SetParent(wide.Revision, fan[i].ID, "hub", fan); err != nil {
			t.Fatalf("child %d rejected: %v", i, err)
		}
	}
	if _, err := wide.SetParent(wide.Revision, fan[MaxTaskHierarchyChildren+1].ID, "hub", fan); !errors.Is(err, ErrInvalidHierarchy) {
		t.Fatalf("1001st child error = %v", err)
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
