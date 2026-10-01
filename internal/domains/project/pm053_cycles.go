package project

import (
	"errors"
	"sort"
	"strings"
	"time"
)

type CycleState string

const (
	CyclePlanned CycleState = "PLANNED"
	CycleActive  CycleState = "ACTIVE"
	CycleClosed  CycleState = "CLOSED"
)

type CarryoverChoice string

const (
	CarryoverNone    CarryoverChoice = "NONE"
	CarryoverToNext  CarryoverChoice = "TO_NEXT_CYCLE"
	CarryoverBacklog CarryoverChoice = "TO_BACKLOG"
	CarryoverDrop    CarryoverChoice = "DROP"
)

var (
	ErrInvalidCycle      = errors.New("project: invalid cycle")
	ErrCycleClosed       = errors.New("project: cycle is closed")
	ErrCycleOverlap      = errors.New("project: active cycles overlap")
	ErrCycleTaskConflict = errors.New("project: task belongs to another active cycle")
	ErrCycleRevision     = errors.New("project: cycle revision conflict")
	ErrCycleScopeMissing = errors.New("project: cycle scope is incomplete")
	ErrInvalidCarryover  = errors.New("project: invalid carryover choice")
)

// CycleTask is the committed scope input for close and report generation.
// Status is captured at close time; later task changes cannot rewrite it.
type CycleTask struct {
	TaskID TaskID
	Status string
}

type CycleCarryover struct {
	TaskID TaskID
	Choice CarryoverChoice
}

type CycleCloseRecord struct {
	CycleID        string
	Revision       uint64
	ClosedAt       time.Time
	CommittedScope []CycleTask
	Carryover      []CycleCarryover
	ReportInputs   []string
}

type Cycle struct {
	ID          string
	ProjectID   ProjectID
	Name        string
	StartDate   string
	EndDate     string
	State       CycleState
	Revision    uint64
	TaskIDs     []TaskID
	CloseRecord *CycleCloseRecord
}

func NewCycle(id string, projectID ProjectID, name, startDate, endDate string) (Cycle, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(string(projectID)) == "" || strings.TrimSpace(name) == "" || !validCycleDate(startDate) || !validCycleDate(endDate) || startDate >= endDate {
		return Cycle{}, ErrInvalidCycle
	}
	return Cycle{ID: id, ProjectID: projectID, Name: name, StartDate: startDate, EndDate: endDate, State: CyclePlanned, Revision: 1}, nil
}

func (c Cycle) AddTask(expected uint64, taskID TaskID) (Cycle, error) {
	if err := c.checkRevision(expected); err != nil {
		return Cycle{}, err
	}
	if c.State == CycleClosed {
		return Cycle{}, ErrCycleClosed
	}
	if strings.TrimSpace(string(taskID)) == "" || containsTask(c.TaskIDs, taskID) {
		return Cycle{}, ErrInvalidCycle
	}
	c.TaskIDs = append(append([]TaskID(nil), c.TaskIDs...), taskID)
	sort.Slice(c.TaskIDs, func(i, j int) bool { return c.TaskIDs[i] < c.TaskIDs[j] })
	c.Revision++
	return c, nil
}

func (c Cycle) RemoveTask(expected uint64, taskID TaskID) (Cycle, error) {
	if err := c.checkRevision(expected); err != nil {
		return Cycle{}, err
	}
	if c.State == CycleClosed {
		return Cycle{}, ErrCycleClosed
	}
	result := make([]TaskID, 0, len(c.TaskIDs))
	found := false
	for _, current := range c.TaskIDs {
		if current == taskID {
			found = true
			continue
		}
		result = append(result, current)
	}
	if !found {
		return Cycle{}, ErrInvalidCycle
	}
	c.TaskIDs, c.Revision = result, c.Revision+1
	return c, nil
}

func (c Cycle) Start(expected uint64) (Cycle, error) {
	if err := c.checkRevision(expected); err != nil {
		return Cycle{}, err
	}
	if c.State != CyclePlanned {
		return Cycle{}, ErrInvalidCycle
	}
	c.State, c.Revision = CycleActive, c.Revision+1
	return c, nil
}

// Close commits a complete snapshot of scope, task status and carryover. A
// closed cycle has no mutating path; a later planning decision must create a
// new cycle or a new report rather than rewriting this record.
func (c Cycle) Close(expected uint64, closedAt time.Time, statuses map[TaskID]string, carryover map[TaskID]CarryoverChoice, reportInputs []string) (Cycle, error) {
	if err := c.checkRevision(expected); err != nil {
		return Cycle{}, err
	}
	if c.State == CycleClosed {
		return Cycle{}, ErrCycleClosed
	}
	if c.State != CycleActive || closedAt.IsZero() {
		return Cycle{}, ErrInvalidCycle
	}
	committed := make([]CycleTask, 0, len(c.TaskIDs))
	choices := make([]CycleCarryover, 0, len(c.TaskIDs))
	for _, taskID := range c.TaskIDs {
		status := strings.TrimSpace(statuses[taskID])
		choice := carryover[taskID]
		if status == "" {
			return Cycle{}, ErrCycleScopeMissing
		}
		if choice == "" {
			choice = CarryoverNone
		}
		if !validCarryover(choice) || (choice != CarryoverNone && (status == "DONE" || status == "CANCELLED")) {
			return Cycle{}, ErrInvalidCarryover
		}
		if status != "DONE" && status != "CANCELLED" && choice == CarryoverNone {
			return Cycle{}, ErrInvalidCarryover
		}
		committed = append(committed, CycleTask{TaskID: taskID, Status: status})
		choices = append(choices, CycleCarryover{TaskID: taskID, Choice: choice})
	}
	report := append([]string(nil), reportInputs...)
	c.CloseRecord = &CycleCloseRecord{CycleID: c.ID, Revision: c.Revision + 1, ClosedAt: closedAt.UTC(), CommittedScope: committed, Carryover: choices, ReportInputs: report}
	c.State, c.Revision = CycleClosed, c.Revision+1
	return c, nil
}

func (c Cycle) checkRevision(expected uint64) error {
	if expected == 0 || c.Revision != expected {
		return ErrCycleRevision
	}
	return nil
}

type CycleBook struct {
	ProjectID ProjectID
	Cycles    []Cycle
}

func NewCycleBook(projectID ProjectID) (CycleBook, error) {
	if strings.TrimSpace(string(projectID)) == "" {
		return CycleBook{}, ErrInvalidCycle
	}
	return CycleBook{ProjectID: projectID}, nil
}

func (b CycleBook) Add(cycle Cycle) (CycleBook, error) {
	if cycle.ProjectID != b.ProjectID || cycle.State != CyclePlanned || cycle.Revision == 0 {
		return CycleBook{}, ErrInvalidCycle
	}
	for _, current := range b.Cycles {
		if current.ID == cycle.ID {
			return CycleBook{}, ErrInvalidCycle
		}
	}
	b.Cycles = append(append([]Cycle(nil), b.Cycles...), cycle)
	return b, nil
}

func (b CycleBook) Start(cycleID string, expected uint64) (CycleBook, Cycle, error) {
	index, err := b.index(cycleID)
	if err != nil {
		return CycleBook{}, Cycle{}, err
	}
	cycle := b.Cycles[index]
	if cycle.Revision != expected {
		return CycleBook{}, Cycle{}, ErrCycleRevision
	}
	for _, current := range b.Cycles {
		if current.State == CycleActive && rangesOverlap(current, cycle) {
			return CycleBook{}, Cycle{}, ErrCycleOverlap
		}
	}
	updated, err := cycle.Start(expected)
	if err != nil {
		return CycleBook{}, Cycle{}, err
	}
	b.Cycles = append([]Cycle(nil), b.Cycles...)
	b.Cycles[index] = updated
	return b, updated, nil
}

func (b CycleBook) AssignTask(cycleID string, expected uint64, taskID TaskID) (CycleBook, Cycle, error) {
	index, err := b.index(cycleID)
	if err != nil {
		return CycleBook{}, Cycle{}, err
	}
	cycle := b.Cycles[index]
	if cycle.State == CycleActive {
		for _, current := range b.Cycles {
			if current.ID != cycleID && current.State == CycleActive && containsTask(current.TaskIDs, taskID) {
				return CycleBook{}, Cycle{}, ErrCycleTaskConflict
			}
		}
	}
	updated, err := cycle.AddTask(expected, taskID)
	if err != nil {
		return CycleBook{}, Cycle{}, err
	}
	b.Cycles = append([]Cycle(nil), b.Cycles...)
	b.Cycles[index] = updated
	return b, updated, nil
}

func (b CycleBook) Close(cycleID string, expected uint64, closedAt time.Time, statuses map[TaskID]string, carryover map[TaskID]CarryoverChoice, reportInputs []string) (CycleBook, Cycle, error) {
	index, err := b.index(cycleID)
	if err != nil {
		return CycleBook{}, Cycle{}, err
	}
	updated, err := b.Cycles[index].Close(expected, closedAt, statuses, carryover, reportInputs)
	if err != nil {
		return CycleBook{}, Cycle{}, err
	}
	b.Cycles = append([]Cycle(nil), b.Cycles...)
	b.Cycles[index] = updated
	return b, updated, nil
}

func (b CycleBook) index(cycleID string) (int, error) {
	for i, cycle := range b.Cycles {
		if cycle.ID == cycleID {
			return i, nil
		}
	}
	return -1, ErrInvalidCycle
}

func validCycleDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func rangesOverlap(a, b Cycle) bool {
	return a.StartDate < b.EndDate && b.StartDate < a.EndDate
}

func containsTask(tasks []TaskID, wanted TaskID) bool {
	for _, task := range tasks {
		if task == wanted {
			return true
		}
	}
	return false
}

func validCarryover(choice CarryoverChoice) bool {
	switch choice {
	case CarryoverNone, CarryoverToNext, CarryoverBacklog, CarryoverDrop:
		return true
	default:
		return false
	}
}
