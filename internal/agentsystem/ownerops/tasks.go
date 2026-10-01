package ownerops

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

// TaskRecord combines durable task state with owner-issued identity and
// measured counters. Free text stays in Task and never enters operator views.
type TaskRecord struct {
	Task                                      agentrun.AgentTask
	OwnerID, AgentID, Version, InstallationID string
	SpendMicros                               int64
	Steps                                     []StepMetric
	Stalled, Looping, OverBudget              bool
}
type StepMetric struct {
	StepID      string
	Latency     time.Duration
	Retries     uint32
	DenialCodes []string
	WakeLag     time.Duration
	SpendMicros int64
}
type TaskView struct {
	Revision                                                     uint64
	CanPause                                                     bool
	TaskID, AgentID, Version, InstallationID, State, FailureCode string
	SpendMicros                                                  int64
	Stalled, Looping, OverBudget                                 bool
	Steps                                                        []StepMetric
	// Full is populated only for the task's authenticated user.
	Full *agentrun.AgentTask `json:"full,omitempty"`
}

// ProjectTasks requires one explicit tenant even for an operator. A user gets
// only their own full trace; owners and operators receive content-free facts.
func ProjectTasks(scope Scope, records []TaskRecord) ([]TaskView, error) {
	if scope.TenantID == "" || scope.SubjectID == "" || !has(scope.Capabilities, CapabilityRead) {
		return nil, ErrDenied
	}
	if scope.Audience == AudienceOperator {
		if scope.Purpose != PurposeOperatorOps {
			return nil, ErrDenied
		}
	} else if scope.Purpose != PurposeOwnerDashboard {
		return nil, ErrDenied
	}
	if scope.Audience != AudienceMember && scope.Audience != AudienceOwner && scope.Audience != AudienceOperator {
		return nil, ErrDenied
	}
	result := []TaskView{}
	for _, record := range records {
		task := record.Task
		if task.TenantID != scope.TenantID || (scope.Audience == AudienceMember && task.UserID != scope.SubjectID) || (scope.Audience == AudienceOwner && record.OwnerID != scope.SubjectID) {
			continue
		}
		view := TaskView{TaskID: task.ID, AgentID: record.AgentID, Version: record.Version, InstallationID: record.InstallationID, State: safeCode(string(task.State)), FailureCode: safeCode(task.FailureCode), SpendMicros: nonnegativeInt(record.SpendMicros), Stalled: record.Stalled, Looping: record.Looping, OverBudget: record.OverBudget, Steps: []StepMetric{}}
		view.Revision = task.Version
		view.CanPause = has(scope.Capabilities, CapabilityPause) && task.State != agentrun.StatePaused && task.State != agentrun.StateCompleted && task.State != agentrun.StateFailed && task.State != agentrun.StateCancelled && task.State != agentrun.StateExpired
		for _, step := range record.Steps {
			view.Steps = append(view.Steps, StepMetric{StepID: step.StepID, Latency: nonnegative(step.Latency), Retries: step.Retries, DenialCodes: safeCodes(step.DenialCodes), WakeLag: nonnegative(step.WakeLag), SpendMicros: nonnegativeInt(step.SpendMicros)})
		}
		if scope.Audience == AudienceMember {
			copy := task
			view.Full = &copy
		}
		result = append(result, view)
	}
	return result, nil
}
