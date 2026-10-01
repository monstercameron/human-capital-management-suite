package project

import (
	"errors"
	"fmt"
	"strings"
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

// DueDatePreviewTask supplies the published status category needed to
// calculate a task's due state without making the project domain depend on a
// workflow store.
type DueDatePreviewTask struct {
	Task     Task
	Category projectworkflow.StatusCategory
}

// DueDateEffect records the before/after result for one task. Both states are
// retained so a caller can explain a timezone change without re-reading the
// project or task rows after the preview.
type DueDateEffect struct {
	TaskID TaskID
	Before TaskDueState
	After  TaskDueState
}

type TimezoneChangePreview struct {
	ProjectID    ProjectID
	FromTimezone string
	ToTimezone   string
	Effects      []DueDateEffect
}

// PreviewTimezoneChange validates the same expected project revision used by
// UpdateSettings and calculates every supplied task in both project
// timezones. It is a read-only preview; publishing remains the revisioned
// settings mutation owned by the project store.
func (p Project) PreviewTimezoneChange(timezone string, expected uint64, tasks []DueDatePreviewTask, now time.Time) (TimezoneChangePreview, error) {
	if expected != p.Revision {
		return TimezoneChangePreview{}, ErrRevisionConflict
	}
	if p.State != LifecycleActive || strings.TrimSpace(timezone) == "" {
		return TimezoneChangePreview{}, ErrInvalidState
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return TimezoneChangePreview{}, fmt.Errorf("%w: invalid project timezone %q", ErrInvalidState, timezone)
	}
	preview := TimezoneChangePreview{ProjectID: p.ID, FromTimezone: p.Timezone, ToTimezone: timezone, Effects: make([]DueDateEffect, 0, len(tasks))}
	updated := p
	updated.Timezone = timezone
	for _, candidate := range tasks {
		before, err := CalculateDueState(candidate.Task, p, candidate.Category, now)
		if err != nil {
			return TimezoneChangePreview{}, err
		}
		after, err := CalculateDueState(candidate.Task, updated, candidate.Category, now)
		if err != nil {
			return TimezoneChangePreview{}, err
		}
		preview.Effects = append(preview.Effects, DueDateEffect{TaskID: candidate.Task.ID, Before: before, After: after})
	}
	return preview, nil
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
