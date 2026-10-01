package project

import (
	"errors"
	"sort"
	"strings"
	"time"
)

const (
	ScrumbanBacklog    = "BACKLOG"
	ScrumbanReady      = "READY"
	ScrumbanInProgress = "IN_PROGRESS"
	ScrumbanDone       = "DONE"
	ScrumbanCancelled  = "CANCELLED"
)

var (
	ErrInvalidScrumban = errors.New("project: invalid scrumban board")
	ErrWIPLimit        = errors.New("project: scrumban WIP limit exceeded")
	ErrTaskRevision    = errors.New("project: scrumban task revision conflict")
)

type ScrumbanTask struct {
	ID       TaskID
	Status   string
	Revision uint64
}

type ScrumbanMove struct {
	Sequence uint64
	TaskID   TaskID
	From     string
	To       string
	Revision uint64
}

type ScrumbanCloseResult struct {
	Cycle       CycleCloseRecord
	WIPAtClose  int
	MovesBefore uint64
}

// ScrumbanBoard composes the dated CycleBook with a continuous-flow policy.
// Cycle close is still a historical snapshot; moving a task never closes a
// cycle or changes a prior close record.
type ScrumbanBoard struct {
	ProjectID ProjectID
	WIPLimit  int
	Cycles    CycleBook
	Tasks     []ScrumbanTask
	Moves     []ScrumbanMove
}

func NewScrumbanBoard(projectID ProjectID, wipLimit int) (ScrumbanBoard, error) {
	cycles, err := NewCycleBook(projectID)
	if err != nil || wipLimit < 1 {
		return ScrumbanBoard{}, ErrInvalidScrumban
	}
	return ScrumbanBoard{ProjectID: projectID, WIPLimit: wipLimit, Cycles: cycles}, nil
}

func (b ScrumbanBoard) AddTask(taskID TaskID, status string) (ScrumbanBoard, error) {
	if strings.TrimSpace(string(taskID)) == "" || !validScrumbanStatus(status) || b.task(taskID) != nil {
		return ScrumbanBoard{}, ErrInvalidScrumban
	}
	if status == ScrumbanInProgress && b.wipCount() >= b.WIPLimit {
		return ScrumbanBoard{}, ErrWIPLimit
	}
	b.Tasks = append(append([]ScrumbanTask(nil), b.Tasks...), ScrumbanTask{ID: taskID, Status: status, Revision: 1})
	sort.Slice(b.Tasks, func(i, j int) bool { return b.Tasks[i].ID < b.Tasks[j].ID })
	return b, nil
}

func (b ScrumbanBoard) MoveTask(taskID TaskID, target string, expectedRevision uint64) (ScrumbanBoard, ScrumbanMove, error) {
	if !validScrumbanStatus(target) || expectedRevision == 0 {
		return ScrumbanBoard{}, ScrumbanMove{}, ErrInvalidScrumban
	}
	index := -1
	for i := range b.Tasks {
		if b.Tasks[i].ID == taskID {
			index = i
			break
		}
	}
	if index < 0 {
		return ScrumbanBoard{}, ScrumbanMove{}, ErrInvalidScrumban
	}
	current := b.Tasks[index]
	if current.Revision != expectedRevision {
		return ScrumbanBoard{}, ScrumbanMove{}, ErrTaskRevision
	}
	if target == ScrumbanInProgress && current.Status != ScrumbanInProgress && b.wipCount() >= b.WIPLimit {
		return ScrumbanBoard{}, ScrumbanMove{}, ErrWIPLimit
	}
	if target == current.Status {
		return ScrumbanBoard{}, ScrumbanMove{}, ErrInvalidScrumban
	}
	nextRevision := current.Revision + 1
	b.Tasks = append([]ScrumbanTask(nil), b.Tasks...)
	b.Tasks[index] = ScrumbanTask{ID: current.ID, Status: target, Revision: nextRevision}
	move := ScrumbanMove{Sequence: uint64(len(b.Moves) + 1), TaskID: taskID, From: current.Status, To: target, Revision: nextRevision}
	b.Moves = append(append([]ScrumbanMove(nil), b.Moves...), move)
	return b, move, nil
}

func (b ScrumbanBoard) PlanCycle(cycle Cycle) (ScrumbanBoard, Cycle, error) {
	if cycle.ProjectID != b.ProjectID {
		return ScrumbanBoard{}, Cycle{}, ErrInvalidScrumban
	}
	cycles, err := b.Cycles.Add(cycle)
	if err != nil {
		return ScrumbanBoard{}, Cycle{}, err
	}
	b.Cycles = cycles
	return b, cycle, nil
}

func (b ScrumbanBoard) StartCycle(id string, expected uint64) (ScrumbanBoard, Cycle, error) {
	cycles, cycle, err := b.Cycles.Start(id, expected)
	if err != nil {
		return ScrumbanBoard{}, Cycle{}, err
	}
	b.Cycles = cycles
	return b, cycle, nil
}

func (b ScrumbanBoard) AssignCycleTask(id string, expected uint64, taskID TaskID) (ScrumbanBoard, Cycle, error) {
	if b.task(taskID) == nil {
		return ScrumbanBoard{}, Cycle{}, ErrInvalidScrumban
	}
	cycles, cycle, err := b.Cycles.AssignTask(id, expected, taskID)
	if err != nil {
		return ScrumbanBoard{}, Cycle{}, err
	}
	b.Cycles = cycles
	return b, cycle, nil
}

func (b ScrumbanBoard) CloseCycle(id string, expected uint64, closedAt time.Time, statuses map[TaskID]string, carryover map[TaskID]CarryoverChoice, reportInputs []string) (ScrumbanBoard, ScrumbanCloseResult, error) {
	cycles, cycle, err := b.Cycles.Close(id, expected, closedAt, statuses, carryover, reportInputs)
	if err != nil {
		return ScrumbanBoard{}, ScrumbanCloseResult{}, err
	}
	b.Cycles = cycles
	return b, ScrumbanCloseResult{Cycle: *cycle.CloseRecord, WIPAtClose: b.wipCount(), MovesBefore: uint64(len(b.Moves))}, nil
}

func (b ScrumbanBoard) task(taskID TaskID) *ScrumbanTask {
	for i := range b.Tasks {
		if b.Tasks[i].ID == taskID {
			return &b.Tasks[i]
		}
	}
	return nil
}

func (b ScrumbanBoard) wipCount() int {
	count := 0
	for _, task := range b.Tasks {
		if task.Status == ScrumbanInProgress {
			count++
		}
	}
	return count
}

func validScrumbanStatus(status string) bool {
	switch status {
	case ScrumbanBacklog, ScrumbanReady, ScrumbanInProgress, ScrumbanDone, ScrumbanCancelled:
		return true
	default:
		return false
	}
}
