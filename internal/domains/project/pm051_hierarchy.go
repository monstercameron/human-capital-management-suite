package project

import (
	"errors"
	"strings"
)

const (
	MaxTaskHierarchyDepth    = 32
	MaxTaskHierarchyChildren = 1000
)

var (
	ErrInvalidHierarchy  = errors.New("project: invalid task hierarchy")
	ErrHierarchyConflict = errors.New("project: hierarchy revision conflict")
	ErrHierarchyCycle    = errors.New("project: task hierarchy cycle")
	ErrHierarchyDepth    = errors.New("project: task hierarchy depth exceeded")
	ErrHierarchyScope    = errors.New("project: task hierarchy scope denied")
)

type HierarchyTask struct {
	ID        TaskID
	ProjectID ProjectID
}

type RollupRule string

const (
	RollupNone            RollupRule = "NONE"
	RollupAllChildrenDone RollupRule = "ALL_CHILDREN_DONE"
	RollupPercentComplete RollupRule = "PERCENT_COMPLETE"
)

type HierarchyRollup struct {
	ParentTaskID        TaskID
	Rule                RollupRule
	DirectChildCount    int
	CompletedChildCount int
	PercentComplete     int
	ShouldMarkDone      bool
	MutatesParent       bool
}

type TaskHierarchy struct {
	ProjectID ProjectID
	Revision  uint64
	Parents   map[TaskID]TaskID
}

func NewTaskHierarchy(projectID ProjectID) (TaskHierarchy, error) {
	if strings.TrimSpace(string(projectID)) == "" {
		return TaskHierarchy{}, ErrInvalidHierarchy
	}
	return TaskHierarchy{ProjectID: projectID, Revision: 1, Parents: map[TaskID]TaskID{}}, nil
}

// SetParent requires both tasks to be in the hierarchy's project. It copies
// the map and checks cycles/depth before changing the revision.
func (h TaskHierarchy) SetParent(expected uint64, child, parent TaskID, tasks []HierarchyTask) (TaskHierarchy, error) {
	if expected == 0 || expected != h.Revision {
		return TaskHierarchy{}, ErrHierarchyConflict
	}
	if strings.TrimSpace(string(child)) == "" || strings.TrimSpace(string(parent)) == "" || child == parent {
		return TaskHierarchy{}, ErrInvalidHierarchy
	}
	scope := make(map[TaskID]ProjectID, len(tasks))
	for _, task := range tasks {
		if strings.TrimSpace(string(task.ID)) == "" || task.ProjectID != h.ProjectID {
			return TaskHierarchy{}, ErrHierarchyScope
		}
		if prior, exists := scope[task.ID]; exists && prior != task.ProjectID {
			return TaskHierarchy{}, ErrHierarchyScope
		}
		scope[task.ID] = task.ProjectID
	}
	if scope[child] != h.ProjectID || scope[parent] != h.ProjectID {
		return TaskHierarchy{}, ErrHierarchyScope
	}
	result := h.clone()
	result.Parents[child] = parent
	if len(result.children(parent)) > MaxTaskHierarchyChildren {
		return TaskHierarchy{}, ErrInvalidHierarchy
	}
	if result.hasCycle() {
		return TaskHierarchy{}, ErrHierarchyCycle
	}
	if result.depth(child) > MaxTaskHierarchyDepth {
		return TaskHierarchy{}, ErrHierarchyDepth
	}
	result.Revision++
	return result, nil
}

func (h TaskHierarchy) RemoveParent(expected uint64, child TaskID) (TaskHierarchy, error) {
	if expected == 0 || expected != h.Revision {
		return TaskHierarchy{}, ErrHierarchyConflict
	}
	if _, ok := h.Parents[child]; !ok {
		return TaskHierarchy{}, ErrInvalidHierarchy
	}
	result := h.clone()
	delete(result.Parents, child)
	result.Revision++
	return result, nil
}

// Rollup computes only direct-child counts. It reports the desired outcome;
// it never changes a parent task, so a rollup cannot inflate completion.
func (h TaskHierarchy) Rollup(parent TaskID, statuses map[TaskID]string, doneCategories map[string]bool, rule RollupRule) (HierarchyRollup, error) {
	if strings.TrimSpace(string(parent)) == "" || (rule != RollupNone && rule != RollupAllChildrenDone && rule != RollupPercentComplete) {
		return HierarchyRollup{}, ErrInvalidHierarchy
	}
	children := h.children(parent)
	result := HierarchyRollup{ParentTaskID: parent, Rule: rule, DirectChildCount: len(children), MutatesParent: false}
	for _, child := range children {
		if doneCategories[statuses[child]] {
			result.CompletedChildCount++
		}
	}
	if result.DirectChildCount > 0 {
		result.PercentComplete = result.CompletedChildCount * 100 / result.DirectChildCount
	}
	result.ShouldMarkDone = rule == RollupAllChildrenDone && result.DirectChildCount > 0 && result.CompletedChildCount == result.DirectChildCount
	return result, nil
}

func (h TaskHierarchy) children(parent TaskID) []TaskID {
	children := make([]TaskID, 0)
	for child, currentParent := range h.Parents {
		if currentParent == parent {
			children = append(children, child)
		}
	}
	return children
}

func (h TaskHierarchy) depth(task TaskID) int {
	depth := 1
	seen := map[TaskID]bool{}
	for parent, ok := h.Parents[task]; ok; parent, ok = h.Parents[parent] {
		if seen[parent] {
			return MaxTaskHierarchyDepth + 1
		}
		seen[parent] = true
		depth++
	}
	return depth
}

func (h TaskHierarchy) hasCycle() bool {
	for child := range h.Parents {
		seen := map[TaskID]bool{}
		for current := child; ; {
			parent, ok := h.Parents[current]
			if !ok {
				break
			}
			if seen[parent] {
				return true
			}
			seen[parent] = true
			current = parent
		}
	}
	return false
}

func (h TaskHierarchy) clone() TaskHierarchy {
	result := h
	result.Parents = make(map[TaskID]TaskID, len(h.Parents)+1)
	for child, parent := range h.Parents {
		result.Parents[child] = parent
	}
	return result
}
