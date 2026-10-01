package project

import (
	"errors"
	"sort"
	"strings"
	"time"
)

var ErrScrumTaskNotInBacklog = errors.New("project: scrum task is not in backlog")

// ScrumSprintReport is derived only from the immutable close record. It is a
// truthful report even if the tasks change after the sprint closes.
type ScrumSprintReport struct {
	CycleID   string
	Planned   int
	Completed int
	Carried   int
	Dropped   int
}

type ScrumBoard struct {
	ProjectID ProjectID
	Backlog   []TaskID
	Cycles    CycleBook
	Reports   []ScrumSprintReport
}

func NewScrumBoard(projectID ProjectID) (ScrumBoard, error) {
	cycles, err := NewCycleBook(projectID)
	if err != nil {
		return ScrumBoard{}, err
	}
	return ScrumBoard{ProjectID: projectID, Cycles: cycles}, nil
}

func (b ScrumBoard) AddBacklogTask(taskID TaskID) (ScrumBoard, error) {
	if strings.TrimSpace(string(taskID)) == "" || containsTask(b.Backlog, taskID) {
		return ScrumBoard{}, ErrInvalidCycle
	}
	b.Backlog = append(append([]TaskID(nil), b.Backlog...), taskID)
	sort.Slice(b.Backlog, func(i, j int) bool { return b.Backlog[i] < b.Backlog[j] })
	return b, nil
}

// PlanSprint creates a dated cycle and commits its initial scope before the
// sprint starts. Scope can still change while active through ChangeScope.
func (b ScrumBoard) PlanSprint(id string, name, startDate, endDate string, taskIDs []TaskID) (ScrumBoard, Cycle, error) {
	cycle, err := NewCycle(id, b.ProjectID, name, startDate, endDate)
	if err != nil {
		return ScrumBoard{}, Cycle{}, err
	}
	updated, err := b.Cycles.Add(cycle)
	if err != nil {
		return ScrumBoard{}, Cycle{}, err
	}
	b.Cycles = updated
	for _, taskID := range taskIDs {
		if !containsTask(b.Backlog, taskID) {
			return ScrumBoard{}, Cycle{}, ErrScrumTaskNotInBacklog
		}
		b.Cycles, cycle, err = b.Cycles.AssignTask(id, cycle.Revision, taskID)
		if err != nil {
			return ScrumBoard{}, Cycle{}, err
		}
		b.Backlog = removeTask(b.Backlog, taskID)
	}
	return b, cycle, nil
}

func (b ScrumBoard) StartSprint(id string, expected uint64) (ScrumBoard, Cycle, error) {
	var cycle Cycle
	var err error
	b.Cycles, cycle, err = b.Cycles.Start(id, expected)
	if err != nil {
		return ScrumBoard{}, Cycle{}, err
	}
	return b, cycle, nil
}

func (b ScrumBoard) ChangeScope(id string, expected uint64, add []TaskID, remove []TaskID) (ScrumBoard, Cycle, error) {
	cycle, err := b.findCycle(id)
	if err != nil {
		return ScrumBoard{}, Cycle{}, err
	}
	if cycle.State != CycleActive {
		return ScrumBoard{}, Cycle{}, ErrInvalidCycle
	}
	for _, taskID := range add {
		if !containsTask(b.Backlog, taskID) {
			return ScrumBoard{}, Cycle{}, ErrScrumTaskNotInBacklog
		}
		b.Cycles, cycle, err = b.Cycles.AssignTask(id, expected, taskID)
		if err != nil {
			return ScrumBoard{}, Cycle{}, err
		}
		expected = cycle.Revision
		b.Backlog = removeTask(b.Backlog, taskID)
	}
	for _, taskID := range remove {
		b.Cycles, cycle, err = b.removeScopedTask(id, expected, taskID)
		if err != nil {
			return ScrumBoard{}, Cycle{}, err
		}
		expected = cycle.Revision
		if !containsTask(b.Backlog, taskID) {
			b.Backlog = append(b.Backlog, taskID)
		}
	}
	sort.Slice(b.Backlog, func(i, j int) bool { return b.Backlog[i] < b.Backlog[j] })
	return b, cycle, nil
}

func (b ScrumBoard) CloseSprint(id string, expected uint64, closedAt time.Time, statuses map[TaskID]string, carryover map[TaskID]CarryoverChoice, reportInputs []string) (ScrumBoard, ScrumSprintReport, error) {
	var cycle Cycle
	var err error
	b.Cycles, cycle, err = b.Cycles.Close(id, expected, closedAt, statuses, carryover, reportInputs)
	if err != nil {
		return ScrumBoard{}, ScrumSprintReport{}, err
	}
	report := ScrumSprintReport{CycleID: cycle.ID, Planned: len(cycle.CloseRecord.CommittedScope)}
	for _, task := range cycle.CloseRecord.CommittedScope {
		choice := carryoverChoice(cycle.CloseRecord.Carryover, task.TaskID)
		switch {
		case task.Status == "DONE":
			report.Completed++
		case choice == CarryoverDrop:
			report.Dropped++
		case choice == CarryoverToNext || choice == CarryoverBacklog:
			report.Carried++
		}
	}
	b.Reports = append(append([]ScrumSprintReport(nil), b.Reports...), report)
	for _, entry := range cycle.CloseRecord.Carryover {
		if entry.Choice == CarryoverToNext || entry.Choice == CarryoverBacklog {
			if !containsTask(b.Backlog, entry.TaskID) {
				b.Backlog = append(b.Backlog, entry.TaskID)
			}
		}
	}
	sort.Slice(b.Backlog, func(i, j int) bool { return b.Backlog[i] < b.Backlog[j] })
	return b, report, nil
}

func (b ScrumBoard) Report(cycleID string) (ScrumSprintReport, error) {
	for _, report := range b.Reports {
		if report.CycleID == cycleID {
			return report, nil
		}
	}
	return ScrumSprintReport{}, ErrInvalidCycle
}

func (b ScrumBoard) findCycle(id string) (Cycle, error) {
	for _, cycle := range b.Cycles.Cycles {
		if cycle.ID == id {
			return cycle, nil
		}
	}
	return Cycle{}, ErrInvalidCycle
}

func (b ScrumBoard) removeScopedTask(id string, expected uint64, taskID TaskID) (CycleBook, Cycle, error) {
	cycle, err := b.findCycle(id)
	if err != nil {
		return CycleBook{}, Cycle{}, err
	}
	updated, err := cycle.RemoveTask(expected, taskID)
	if err != nil {
		return CycleBook{}, Cycle{}, err
	}
	index, _ := b.Cycles.index(id)
	b.Cycles.Cycles = append([]Cycle(nil), b.Cycles.Cycles...)
	b.Cycles.Cycles[index] = updated
	return b.Cycles, updated, nil
}

func carryoverChoice(choices []CycleCarryover, taskID TaskID) CarryoverChoice {
	for _, choice := range choices {
		if choice.TaskID == taskID {
			return choice.Choice
		}
	}
	return CarryoverNone
}

func removeTask(tasks []TaskID, wanted TaskID) []TaskID {
	result := make([]TaskID, 0, len(tasks))
	for _, taskID := range tasks {
		if taskID != wanted {
			result = append(result, taskID)
		}
	}
	return result
}
