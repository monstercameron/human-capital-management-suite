package agentrun

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TaskEventType identifies a durable task planning or approval transition.
type TaskEventType string

const (
	TaskEventPlanRevision     TaskEventType = "PLAN_REVISION"
	TaskEventPlanConfirmation TaskEventType = "PLAN_CONFIRMATION"
	TaskEventApprovalRequest  TaskEventType = "APPROVAL_REQUESTED"
	TaskEventApprovalOutcome  TaskEventType = "APPROVAL_OUTCOME"
	TaskEventStepExecution    TaskEventType = "STEP_EXECUTION"
	TaskEventStepVerification TaskEventType = "STEP_VERIFICATION"
	TaskEventStepEffect       TaskEventType = "STEP_EFFECT"
)

// TaskEvent records one observed plan or approval transition. Outcome describes
// the runtime transition, never an evaluator expectation.
type TaskEvent struct {
	Sequence       uint64
	TaskID         string
	Type           TaskEventType
	PlanRevision   uint64
	PlanDigest     string
	PlanSnapshot   *HistoricalPlanSnapshot
	StepID         string
	StepType       StepType
	Tier           Tier
	ApprovalDigest string
	EvidenceRef    string
	EvidenceDigest string
	Outcome        string
	ActorID        string
	OccurredAt     time.Time
}

const maxPersistedTaskRevision = uint64(1<<63 - 1)

// Validate checks the fields required by each event kind.
func (e TaskEvent) Validate() error {
	if strings.TrimSpace(e.TaskID) == "" || e.OccurredAt.IsZero() {
		return fmt.Errorf("%w: task event needs task id and time", ErrInvalid)
	}
	switch e.Type {
	case TaskEventPlanRevision, TaskEventPlanConfirmation:
		if e.PlanRevision == 0 || e.PlanRevision > maxPersistedTaskRevision || strings.TrimSpace(e.PlanDigest) == "" || strings.TrimSpace(e.Outcome) == "" ||
			e.PlanSnapshot == nil || e.PlanSnapshot.Revision != e.PlanRevision || e.PlanSnapshot.Digest != e.PlanDigest || e.PlanSnapshot.Verify() != nil {
			return fmt.Errorf("%w: plan event needs a matching verified historical snapshot", ErrInvalid)
		}
	case TaskEventApprovalRequest, TaskEventApprovalOutcome:
		if !validEventPlanBinding(e) || strings.TrimSpace(e.StepID) == "" || !e.StepType.valid() || e.Tier < TierSubmitGoverned || !e.Tier.valid() || strings.TrimSpace(e.ApprovalDigest) == "" || strings.TrimSpace(e.Outcome) == "" {
			return fmt.Errorf("%w: approval event needs plan, step, tier, digest and outcome", ErrInvalid)
		}
	case TaskEventStepVerification:
		if !validEventPlanBinding(e) || strings.TrimSpace(e.StepID) == "" || e.StepType != StepVerify || !e.Tier.valid() || (e.Outcome != "VERIFIED" && e.Outcome != "FAILED") {
			return fmt.Errorf("%w: verification event needs a bound VERIFY step and observed outcome", ErrInvalid)
		}
	case TaskEventStepExecution:
		if !validEventPlanBinding(e) || strings.TrimSpace(e.StepID) == "" || !e.StepType.valid() || !e.Tier.valid() || (e.Outcome != "SUCCEEDED" && e.Outcome != "FAILED" && e.Outcome != "PAUSED") {
			return fmt.Errorf("%w: execution event needs a bound step and observed outcome", ErrInvalid)
		}
	case TaskEventStepEffect:
		if !validEventPlanBinding(e) || strings.TrimSpace(e.StepID) == "" || !e.StepType.valid() || e.Tier < TierSubmitGoverned || !e.Tier.valid() || strings.TrimSpace(e.ApprovalDigest) == "" || (e.Outcome != "SUCCEEDED" && e.Outcome != "FAILED") {
			return fmt.Errorf("%w: effect event needs an approved high-tier step and observed outcome", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown task event type", ErrInvalid)
	}
	return nil
}

func validEventPlanBinding(e TaskEvent) bool {
	return e.PlanRevision > 0 && e.PlanRevision <= maxPersistedTaskRevision && strings.TrimSpace(e.PlanDigest) != ""
}

func cloneTaskEvent(event TaskEvent) TaskEvent {
	if event.PlanSnapshot != nil {
		snapshot := *event.PlanSnapshot
		snapshot.Steps = append([]PlanStepIdentity(nil), snapshot.Steps...)
		for i := range snapshot.Steps {
			snapshot.Steps[i].Inputs = append([]InputRef(nil), snapshot.Steps[i].Inputs...)
			for j := range snapshot.Steps[i].Inputs {
				snapshot.Steps[i].Inputs[j].Taint = append([]string(nil), snapshot.Steps[i].Inputs[j].Taint...)
			}
			snapshot.Steps[i].Wait = cloneWake(snapshot.Steps[i].Wait)
		}
		event.PlanSnapshot = &snapshot
	}
	return event
}

// TaskEventStore atomically stores task transitions with their events and
// reads the task-scoped append-only event history.
type TaskEventStore interface {
	CreateWithEvents(context.Context, AgentTask, ...TaskEvent) error
	SaveWithEvents(context.Context, AgentTask, uint64, ...TaskEvent) error
	ListEvents(context.Context, string) ([]TaskEvent, error)
}

func createTaskWithEvents(ctx context.Context, store TaskStore, task AgentTask, events ...TaskEvent) error {
	return store.CreateWithEvents(ctx, task, events...)
}

func saveTaskWithEvents(ctx context.Context, store TaskStore, task AgentTask, expected uint64, events ...TaskEvent) error {
	return store.SaveWithEvents(ctx, task, expected, events...)
}

// TaskEvents returns the durable observed event history for one task.
func (r *Runtime) TaskEvents(ctx context.Context, id string) ([]TaskEvent, error) {
	if r == nil || r.store == nil || strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("%w: runtime and task id are required", ErrInvalid)
	}
	store, ok := r.store.(TaskEventStore)
	if !ok {
		return nil, fmt.Errorf("%w: task store does not retain events", ErrInvalid)
	}
	return store.ListEvents(ctx, id)
}
