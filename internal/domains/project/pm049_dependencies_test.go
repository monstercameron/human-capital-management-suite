package project

import (
	"errors"
	"testing"
)

type pm049Access map[string]bool

func (a pm049Access) CanAccessTask(projectID ProjectID, taskID TaskID) bool {
	return a[string(projectID)+":"+string(taskID)]
}

func TestTodo_PM_049(t *testing.T) {
	access := pm049Access{"p1:a": true, "p1:b": true, "p2:c": true}
	graph := DependencyGraph{TenantID: "tenant-1"}
	var err error
	graph, err = graph.Add(DependencyEdge{ID: "ab", TenantID: "tenant-1", FromProjectID: "p1", FromTaskID: "a", ToProjectID: "p1", ToTaskID: "b", Kind: DependencyBlocks, Revision: 1}, access)
	if err != nil {
		t.Fatal(err)
	}
	graph, err = graph.Add(DependencyEdge{ID: "bc", TenantID: "tenant-1", FromProjectID: "p1", FromTaskID: "b", ToProjectID: "p2", ToTaskID: "c", Kind: DependencyBlocks, Revision: 1}, access)
	if err != nil || len(graph.Edges) != 2 {
		t.Fatalf("authorized cross-project edge = %+v err=%v", graph, err)
	}
	_, err = graph.Add(DependencyEdge{ID: "ca", TenantID: "tenant-1", FromProjectID: "p2", FromTaskID: "c", ToProjectID: "p1", ToTaskID: "a", Kind: DependencyBlocks, Revision: 1}, access)
	if !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestTodo_PM_049_Property(t *testing.T) {
	access := pm049Access{"p:a": true, "p:b": true, "p:c": true, "p:d": true}
	graph := DependencyGraph{TenantID: "t"}
	for i, edge := range []DependencyEdge{
		{ID: "ab", TenantID: "t", FromProjectID: "p", FromTaskID: "a", ToProjectID: "p", ToTaskID: "b", Kind: DependencyBlocks, Revision: 1},
		{ID: "bc", TenantID: "t", FromProjectID: "p", FromTaskID: "b", ToProjectID: "p", ToTaskID: "c", Kind: DependencyBlocks, Revision: 1},
		{ID: "cd", TenantID: "t", FromProjectID: "p", FromTaskID: "c", ToProjectID: "p", ToTaskID: "d", Kind: DependencyBlocks, Revision: 1},
	} {
		var err error
		graph, err = graph.Add(edge, access)
		if err != nil {
			t.Fatalf("chain edge %d rejected: %v", i, err)
		}
	}
	if _, err := graph.Add(DependencyEdge{ID: "da", TenantID: "t", FromProjectID: "p", FromTaskID: "d", ToProjectID: "p", ToTaskID: "a", Kind: DependencyBlocks, Revision: 1}, access); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("property cycle error = %v", err)
	}
}

func TestTodo_PM_049_Security(t *testing.T) {
	graph := DependencyGraph{TenantID: "tenant"}
	edge := DependencyEdge{ID: "secret", TenantID: "tenant", FromProjectID: "p1", FromTaskID: "visible", ToProjectID: "p2", ToTaskID: "hidden", Kind: DependencyBlocks, Revision: 1}
	_, err := graph.Add(edge, pm049Access{"p1:visible": true})
	if !errors.Is(err, ErrDependencyUnauthorized) {
		t.Fatalf("one-sided access error = %v", err)
	}
	if _, err := graph.Add(edge, pm049Access{"p2:hidden": true}); !errors.Is(err, ErrDependencyUnauthorized) {
		t.Fatalf("other-sided access error = %v", err)
	}
}
