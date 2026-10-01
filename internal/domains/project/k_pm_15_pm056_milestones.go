package project

import (
	"errors"
	"strings"
	"time"
)

const MaxMilestoneTasks = 1000

var (
	ErrInvalidMilestone  = errors.New("project: invalid milestone")
	ErrMilestoneConflict = errors.New("project: milestone revision conflict")
	ErrMilestoneAccess   = errors.New("project: milestone access denied")
)

// Milestone is project-owned planning state. TargetDate is a civil date; it
// is never interpreted as completion evidence.
type Milestone struct {
	ID            string
	TenantID      string
	ProjectID     ProjectID
	Name          string
	OwnerID       string
	TargetDate    string
	LinkedTaskIDs []TaskID
	CompletedAt   *time.Time
	CompletedBy   string
	Revision      uint64
}

func NewMilestone(id, tenantID string, projectID ProjectID, name, ownerID, targetDate string, linkedTaskIDs []TaskID) (Milestone, error) {
	m := Milestone{ID: id, TenantID: tenantID, ProjectID: projectID, Name: name, OwnerID: ownerID, TargetDate: targetDate, LinkedTaskIDs: append([]TaskID(nil), linkedTaskIDs...), Revision: 1}
	if err := m.validate(); err != nil {
		return Milestone{}, err
	}
	return m, nil
}

// Complete records an explicit actor action. A target date passing does not
// call this method and therefore cannot make a milestone complete.
func (m Milestone) Complete(expected uint64, actor string, at time.Time) (Milestone, error) {
	if expected == 0 || expected != m.Revision {
		return Milestone{}, ErrMilestoneConflict
	}
	if strings.TrimSpace(actor) == "" || at.IsZero() || m.CompletedAt != nil {
		return Milestone{}, ErrInvalidMilestone
	}
	m.CompletedAt = timePtr(at.UTC())
	m.CompletedBy = actor
	m.Revision++
	return m, nil
}

func (m Milestone) Reopen(expected uint64, actor string) (Milestone, error) {
	if expected == 0 || expected != m.Revision {
		return Milestone{}, ErrMilestoneConflict
	}
	if strings.TrimSpace(actor) == "" || m.CompletedAt == nil || m.CompletedBy == "" {
		return Milestone{}, ErrInvalidMilestone
	}
	m.CompletedAt = nil
	m.CompletedBy = ""
	m.Revision++
	return m, nil
}

func (m Milestone) IsComplete() bool {
	return m.CompletedAt != nil && strings.TrimSpace(m.CompletedBy) != ""
}

func (m Milestone) validate() error {
	if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.TenantID) == "" || strings.TrimSpace(string(m.ProjectID)) == "" || strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.OwnerID) == "" || m.Revision == 0 || !validCivilDate(m.TargetDate) || len(m.LinkedTaskIDs) > MaxMilestoneTasks {
		return ErrInvalidMilestone
	}
	seen := make(map[TaskID]bool, len(m.LinkedTaskIDs))
	for _, taskID := range m.LinkedTaskIDs {
		if strings.TrimSpace(string(taskID)) == "" || seen[taskID] {
			return ErrInvalidMilestone
		}
		seen[taskID] = true
	}
	if (m.CompletedAt == nil) != (strings.TrimSpace(m.CompletedBy) == "") {
		return ErrInvalidMilestone
	}
	return nil
}

type MilestoneTask struct {
	ID        TaskID
	ProjectID ProjectID
	Title     string
	Status    string
	OwnerID   string
}

type MilestoneAccess interface {
	CanViewMilestone(string) bool
	CanViewTask(ProjectID, TaskID) bool
}

type MilestoneColumn struct {
	ID            string
	Name          string
	OwnerID       string
	TargetDate    string
	Complete      bool
	CompletedAt   *time.Time
	CompletedBy   string
	LinkedTaskIDs []TaskID
	Tasks         []MilestoneTask
}

type MilestoneBoard struct {
	TenantID  string
	ProjectID ProjectID
	Columns   []MilestoneColumn
}

// BuildMilestoneBoard filters before grouping or counting. Hidden tasks and
// hidden milestones therefore cannot affect the returned board.
func BuildMilestoneBoard(tenantID string, projectID ProjectID, milestones []Milestone, tasks []MilestoneTask, access MilestoneAccess) (MilestoneBoard, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(string(projectID)) == "" || access == nil {
		return MilestoneBoard{}, ErrMilestoneAccess
	}
	taskByID := make(map[TaskID]MilestoneTask, len(tasks))
	for _, task := range tasks {
		if task.ProjectID != projectID || strings.TrimSpace(string(task.ID)) == "" || !access.CanViewTask(projectID, task.ID) {
			continue
		}
		taskByID[task.ID] = task
	}
	board := MilestoneBoard{TenantID: tenantID, ProjectID: projectID, Columns: make([]MilestoneColumn, 0, len(milestones))}
	for _, milestone := range milestones {
		if milestone.TenantID != tenantID || milestone.ProjectID != projectID || !access.CanViewMilestone(milestone.ID) {
			continue
		}
		if err := milestone.validate(); err != nil {
			return MilestoneBoard{}, err
		}
		column := MilestoneColumn{ID: milestone.ID, Name: milestone.Name, OwnerID: milestone.OwnerID, TargetDate: milestone.TargetDate, Complete: milestone.IsComplete(), CompletedBy: milestone.CompletedBy, LinkedTaskIDs: make([]TaskID, 0)}
		if milestone.CompletedAt != nil {
			column.CompletedAt = timePtr(milestone.CompletedAt.UTC())
		}
		for _, taskID := range milestone.LinkedTaskIDs {
			task, ok := taskByID[taskID]
			if !ok {
				continue
			}
			column.LinkedTaskIDs = append(column.LinkedTaskIDs, taskID)
			column.Tasks = append(column.Tasks, task)
		}
		board.Columns = append(board.Columns, column)
	}
	return board, nil
}

func validCivilDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func timePtr(value time.Time) *time.Time { return &value }
