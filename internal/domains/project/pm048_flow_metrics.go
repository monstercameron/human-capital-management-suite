package project

import (
	"errors"
	"sort"
	"strings"
	"time"
)

// FlowEventKind identifies an immutable task-history event used by flow
// metrics. The metric never reads the current task columns.
type FlowEventKind string

const (
	FlowTaskCreated   FlowEventKind = "TASK_CREATED"
	FlowStatusChanged FlowEventKind = "STATUS_CHANGED"
	FlowTaskArchived  FlowEventKind = "TASK_ARCHIVED"
)

const (
	FlowNotStarted = "NOT_STARTED"
	FlowActive     = "ACTIVE"
	FlowBlocked    = "BLOCKED"
	FlowDone       = "DONE"
	FlowCancelled  = "CANCELLED"
)

var (
	ErrInvalidFlowDefinition = errors.New("project: invalid flow metric definition")
	ErrInvalidFlowEvent      = errors.New("project: invalid flow metric event")
)

// FlowMetricDefinition is a versioned, reproducible metric contract. Window
// bounds are instants; Timezone declares the reporting zone and never changes
// the instant arithmetic.
type FlowMetricDefinition struct {
	Version            string
	ProjectID          ProjectID
	Timezone           string
	WindowStart        time.Time
	WindowEnd          time.Time
	ExcludedTaskIDs    map[TaskID]string
	ExcludedEventKinds map[FlowEventKind]string
}

// TaskFlowEvent is append-only history. Sequence disambiguates events at the
// same instant without relying on a process-local clock.
type TaskFlowEvent struct {
	Sequence       int64
	TaskID         TaskID
	ProjectID      ProjectID
	Kind           FlowEventKind
	StatusCategory string
	OccurredAt     time.Time
}

type TaskFlowMetric struct {
	TaskID    TaskID
	Age       time.Duration
	CycleTime time.Duration
	WIP       bool
}

type FlowMetrics struct {
	DefinitionVersion string
	ProjectID         ProjectID
	Timezone          string
	AsOf              time.Time
	Tasks             []TaskFlowMetric
	Throughput        int
	WIP               int
}

func (d FlowMetricDefinition) validate(asOf time.Time) error {
	if strings.TrimSpace(d.Version) == "" || strings.TrimSpace(string(d.ProjectID)) == "" || d.WindowStart.IsZero() || d.WindowEnd.IsZero() || !d.WindowStart.Before(d.WindowEnd) || asOf.IsZero() || asOf.Before(d.WindowEnd) {
		return ErrInvalidFlowDefinition
	}
	if _, err := time.LoadLocation(d.Timezone); err != nil {
		return ErrInvalidFlowDefinition
	}
	return nil
}

// ComputeFlowMetrics derives age, cycle time, throughput, and WIP from a
// stable event stream. Events from excluded tasks/kinds are omitted before
// sequencing, so exclusions are explicit and auditable.
func ComputeFlowMetrics(def FlowMetricDefinition, events []TaskFlowEvent, asOf time.Time) (FlowMetrics, error) {
	if err := def.validate(asOf); err != nil {
		return FlowMetrics{}, err
	}
	ordered := append([]TaskFlowEvent(nil), events...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].OccurredAt.Equal(ordered[j].OccurredAt) {
			return ordered[i].Sequence < ordered[j].Sequence
		}
		return ordered[i].OccurredAt.Before(ordered[j].OccurredAt)
	})
	byTask := make(map[TaskID][]TaskFlowEvent)
	for _, event := range ordered {
		if event.ProjectID != def.ProjectID || strings.TrimSpace(string(event.TaskID)) == "" || event.Sequence <= 0 || event.OccurredAt.IsZero() || event.OccurredAt.After(asOf) {
			return FlowMetrics{}, ErrInvalidFlowEvent
		}
		if _, excluded := def.ExcludedTaskIDs[event.TaskID]; excluded {
			continue
		}
		if _, excluded := def.ExcludedEventKinds[event.Kind]; excluded {
			continue
		}
		if event.Kind != FlowTaskCreated && event.Kind != FlowStatusChanged && event.Kind != FlowTaskArchived {
			return FlowMetrics{}, ErrInvalidFlowEvent
		}
		if event.Kind != FlowTaskArchived && strings.TrimSpace(event.StatusCategory) == "" {
			return FlowMetrics{}, ErrInvalidFlowEvent
		}
		byTask[event.TaskID] = append(byTask[event.TaskID], event)
	}

	tasks := make([]TaskID, 0, len(byTask))
	for taskID := range byTask {
		tasks = append(tasks, taskID)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i] < tasks[j] })
	result := FlowMetrics{DefinitionVersion: def.Version, ProjectID: def.ProjectID, Timezone: def.Timezone, AsOf: asOf, Tasks: make([]TaskFlowMetric, 0, len(tasks))}
	for _, taskID := range tasks {
		history := byTask[taskID]
		if len(history) == 0 || history[0].Kind != FlowTaskCreated {
			return FlowMetrics{}, ErrInvalidFlowEvent
		}
		createdAt := history[0].OccurredAt
		var startedAt, completedAt time.Time
		for _, event := range history {
			if event.OccurredAt.Before(createdAt) {
				return FlowMetrics{}, ErrInvalidFlowEvent
			}
			if startedAt.IsZero() && (event.StatusCategory == FlowActive || event.StatusCategory == FlowBlocked) {
				startedAt = event.OccurredAt
			}
			if completedAt.IsZero() && (event.StatusCategory == FlowDone || event.StatusCategory == FlowCancelled) {
				completedAt = event.OccurredAt
			}
		}
		if createdAt.After(asOf) {
			continue
		}
		wip := completedAt.IsZero()
		age := asOf.Sub(createdAt)
		cycle := time.Duration(0)
		if !startedAt.IsZero() {
			cycleEnd := asOf
			if !completedAt.IsZero() {
				cycleEnd = completedAt
			}
			cycle = cycleEnd.Sub(startedAt)
		}
		result.Tasks = append(result.Tasks, TaskFlowMetric{TaskID: taskID, Age: age, CycleTime: cycle, WIP: wip})
		if wip {
			result.WIP++
		}
		if !completedAt.IsZero() && !completedAt.Before(def.WindowStart) && completedAt.Before(def.WindowEnd) {
			result.Throughput++
		}
	}
	return result, nil
}
