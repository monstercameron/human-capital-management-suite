package project

import (
	"errors"
	"strings"
)

type DependencyKind string

const DependencyBlocks DependencyKind = "BLOCKS"

var (
	ErrInvalidDependency       = errors.New("project: invalid dependency")
	ErrDependencyUnauthorized  = errors.New("project: dependency access denied")
	ErrDependencyCycle         = errors.New("project: dependency cycle")
	ErrDependencyAlreadyExists = errors.New("project: dependency already exists")
)

// DependencyEdge is directed: FromTaskID blocks ToTaskID. It never carries a
// completion command, so adding/removing an edge cannot mutate task status.
type DependencyEdge struct {
	ID            string
	TenantID      string
	FromProjectID ProjectID
	FromTaskID    TaskID
	ToProjectID   ProjectID
	ToTaskID      TaskID
	Kind          DependencyKind
	Revision      uint64
}

// DependencyAccess is checked for both endpoints before a cross-project edge
// is accepted. A false result is intentionally indistinguishable from a
// missing endpoint to callers without access.
type DependencyAccess interface {
	CanAccessTask(projectID ProjectID, taskID TaskID) bool
}

type DependencyGraph struct {
	TenantID string
	Edges    []DependencyEdge
}

func (g DependencyGraph) Add(edge DependencyEdge, access DependencyAccess) (DependencyGraph, error) {
	if strings.TrimSpace(g.TenantID) == "" || strings.TrimSpace(edge.ID) == "" || strings.TrimSpace(edge.TenantID) == "" || edge.TenantID != g.TenantID || edge.Kind != DependencyBlocks || strings.TrimSpace(string(edge.FromProjectID)) == "" || strings.TrimSpace(string(edge.ToProjectID)) == "" || strings.TrimSpace(string(edge.FromTaskID)) == "" || strings.TrimSpace(string(edge.ToTaskID)) == "" || edge.FromTaskID == edge.ToTaskID && edge.FromProjectID == edge.ToProjectID || edge.Revision == 0 || access == nil {
		return DependencyGraph{}, ErrInvalidDependency
	}
	if !access.CanAccessTask(edge.FromProjectID, edge.FromTaskID) || !access.CanAccessTask(edge.ToProjectID, edge.ToTaskID) {
		return DependencyGraph{}, ErrDependencyUnauthorized
	}
	for _, existing := range g.Edges {
		if existing.ID == edge.ID || existing.FromProjectID == edge.FromProjectID && existing.FromTaskID == edge.FromTaskID && existing.ToProjectID == edge.ToProjectID && existing.ToTaskID == edge.ToTaskID && existing.Kind == edge.Kind {
			return DependencyGraph{}, ErrDependencyAlreadyExists
		}
	}
	if g.reaches(edge.ToProjectID, edge.ToTaskID, edge.FromProjectID, edge.FromTaskID) {
		return DependencyGraph{}, ErrDependencyCycle
	}
	result := DependencyGraph{TenantID: g.TenantID, Edges: append([]DependencyEdge(nil), g.Edges...)}
	result.Edges = append(result.Edges, edge)
	return result, nil
}

func (g DependencyGraph) reaches(projectID ProjectID, taskID TaskID, wantProject ProjectID, wantTask TaskID) bool {
	type node struct {
		project ProjectID
		task    TaskID
	}
	seen := map[node]bool{}
	stack := []node{{project: projectID, task: taskID}}
	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		if current.project == wantProject && current.task == wantTask {
			return true
		}
		if seen[current] {
			continue
		}
		seen[current] = true
		for _, edge := range g.Edges {
			if edge.FromProjectID == current.project && edge.FromTaskID == current.task {
				stack = append(stack, node{project: edge.ToProjectID, task: edge.ToTaskID})
			}
		}
	}
	return false
}

func (g DependencyGraph) Remove(edgeID string, access DependencyAccess) (DependencyGraph, error) {
	if strings.TrimSpace(edgeID) == "" || access == nil {
		return DependencyGraph{}, ErrInvalidDependency
	}
	result := DependencyGraph{TenantID: g.TenantID, Edges: make([]DependencyEdge, 0, len(g.Edges))}
	found := false
	for _, edge := range g.Edges {
		if edge.ID != edgeID {
			result.Edges = append(result.Edges, edge)
			continue
		}
		found = true
		if !access.CanAccessTask(edge.FromProjectID, edge.FromTaskID) || !access.CanAccessTask(edge.ToProjectID, edge.ToTaskID) {
			return DependencyGraph{}, ErrDependencyUnauthorized
		}
	}
	if !found {
		return DependencyGraph{}, ErrInvalidDependency
	}
	return result, nil
}
