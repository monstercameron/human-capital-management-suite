package project

import (
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
)

var (
	ErrInvalidDueDate        = errors.New("project: due date must be an ISO calendar date")
	ErrInvalidProjectZone    = errors.New("project: invalid project timezone")
	ErrInvalidStatusCategory = errors.New("project: invalid status category")
)

type TaskDueState struct {
	DueDate  string
	Overdue  bool
	AsOfDate string
}

// CalculateDueState compares an ISO due date with the current calendar date
// in the project's timezone. The caller supplies the published workflow
// category for the task's current stable status ID.
func CalculateDueState(task Task, project Project, category projectworkflow.StatusCategory, now time.Time) (TaskDueState, error) {
	if task.ProjectID != project.ID || task.TenantID != project.TenantID {
		return TaskDueState{}, ErrInvalidIdentity
	}
	if !validStatusCategory(category) {
		return TaskDueState{}, ErrInvalidStatusCategory
	}
	if task.DueDate == "" {
		return TaskDueState{}, nil
	}
	due, err := time.Parse("2006-01-02", task.DueDate)
	if err != nil || due.Format("2006-01-02") != task.DueDate {
		return TaskDueState{}, fmt.Errorf("%w: %q", ErrInvalidDueDate, task.DueDate)
	}
	location, err := time.LoadLocation(project.Timezone)
	if err != nil {
		return TaskDueState{}, fmt.Errorf("%w: %q", ErrInvalidProjectZone, project.Timezone)
	}
	asOfDate := now.In(location).Format("2006-01-02")
	completed := category == projectworkflow.CategoryDone || category == projectworkflow.CategoryCancelled
	return TaskDueState{DueDate: task.DueDate, AsOfDate: asOfDate, Overdue: !completed && asOfDate > task.DueDate}, nil
}

func validStatusCategory(category projectworkflow.StatusCategory) bool {
	switch category {
	case projectworkflow.CategoryNotStarted, projectworkflow.CategoryActive, projectworkflow.CategoryBlocked, projectworkflow.CategoryDone, projectworkflow.CategoryCancelled:
		return true
	default:
		return false
	}
}
