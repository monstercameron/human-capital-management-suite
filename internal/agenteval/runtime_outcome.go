package agenteval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrIncompleteRuntimeEvidence means the durable runtime history cannot
// support one or more AGENT2-025 measurements without guessing.
var ErrIncompleteRuntimeEvidence = errors.New("agenteval: incomplete runtime evidence")

// ObservedTaskOutcome is the evaluator-neutral projection of measured task
// facts. tools/agenteval may adapt it to its release-evidence schema.
type ObservedTaskOutcome struct {
	Completed          bool
	VerifyPassed       int
	VerifyTotal        int
	PlanRevisions      int
	ApprovalsRequested map[string]int
	Steps              int
	WallClock          time.Duration
	CostMicros         int64
}

// RuntimeEvidenceReader is implemented by agentrun.Runtime. Both reads must
// come from the tenant-bound runtime that executed the task.
type RuntimeEvidenceReader interface {
	GetTask(context.Context, string) (agentrun.AgentTask, error)
	TaskEvents(context.Context, string) ([]agentrun.TaskEvent, error)
}

type runtimeTaskReader struct{ runtime RuntimeEvidenceReader }

func (r runtimeTaskReader) Get(ctx context.Context, id string) (agentrun.AgentTask, error) {
	return r.runtime.GetTask(ctx, id)
}

// ObserveRuntimeOutcome derives evaluation metrics from terminal task state,
// append-only runtime events and settled tenant-scoped usage. It never accepts
// outcome counters from a model or scenario fixture.
func ObserveRuntimeOutcome(ctx context.Context, tenant values.TenantId, taskID string, runtime RuntimeEvidenceReader, usage SettledUsageReader) (ObservedTaskOutcome, error) {
	if ctx == nil || runtime == nil || usage == nil || strings.TrimSpace(taskID) == "" {
		return ObservedTaskOutcome{}, fmt.Errorf("%w: context, task id and runtime readers are required", ErrInvalidObservation)
	}
	observation, err := ObserveTask(ctx, tenant, taskID, runtimeTaskReader{runtime: runtime}, usage)
	if err != nil {
		return ObservedTaskOutcome{}, err
	}
	task, err := runtime.GetTask(ctx, taskID)
	if err != nil {
		return ObservedTaskOutcome{}, err
	}
	if task.ID != observation.TaskID || task.TenantID != observation.TenantID {
		return ObservedTaskOutcome{}, ErrIdentityMismatch
	}
	if task.State != observation.State || !task.UpdatedAt.Equal(observation.TerminalAt) {
		return ObservedTaskOutcome{}, fmt.Errorf("%w: terminal task changed between evidence reads", ErrIncompleteRuntimeEvidence)
	}
	if task.CreatedAt.IsZero() || observation.TerminalAt.Before(task.CreatedAt) {
		return ObservedTaskOutcome{}, fmt.Errorf("%w: task lifecycle timestamps are invalid", ErrInvalidObservation)
	}
	events, err := runtime.TaskEvents(ctx, taskID)
	if err != nil {
		return ObservedTaskOutcome{}, err
	}
	outcome, err := outcomeFromRuntime(task, events, observation)
	if err != nil {
		return ObservedTaskOutcome{}, err
	}
	return outcome, nil
}

func outcomeFromRuntime(task agentrun.AgentTask, events []agentrun.TaskEvent, observation TaskObservation) (ObservedTaskOutcome, error) {
	plans := make(map[uint64]agentrun.HistoricalPlanSnapshot)
	for i, event := range events {
		if event.TaskID != task.ID || event.Sequence != uint64(i+1) {
			return ObservedTaskOutcome{}, fmt.Errorf("%w: task event identity or sequence is invalid", ErrIncompleteRuntimeEvidence)
		}
		if err := event.Validate(); err != nil {
			return ObservedTaskOutcome{}, fmt.Errorf("%w: invalid task event: %v", ErrIncompleteRuntimeEvidence, err)
		}
		if event.OccurredAt.Before(task.CreatedAt) || event.OccurredAt.After(observation.TerminalAt) ||
			(i > 0 && event.OccurredAt.Before(events[i-1].OccurredAt)) {
			return ObservedTaskOutcome{}, fmt.Errorf("%w: event timestamp is outside the task lifecycle or regresses", ErrIncompleteRuntimeEvidence)
		}
		if event.PlanSnapshot == nil {
			continue
		}
		if prior, ok := plans[event.PlanRevision]; ok && prior.Digest != event.PlanDigest {
			return ObservedTaskOutcome{}, fmt.Errorf("%w: conflicting plan snapshot", ErrIncompleteRuntimeEvidence)
		}
		plans[event.PlanRevision] = *event.PlanSnapshot
	}
	if len(plans) == 0 {
		return ObservedTaskOutcome{}, fmt.Errorf("%w: plan snapshots are missing", ErrIncompleteRuntimeEvidence)
	}
	if snapshot, ok := plans[task.Plan.Revision]; !ok || snapshot.Digest != task.Plan.Digest {
		return ObservedTaskOutcome{}, fmt.Errorf("%w: terminal plan has no matching history", ErrIncompleteRuntimeEvidence)
	}
	for revision := uint64(1); revision <= task.Plan.Revision; revision++ {
		if _, ok := plans[revision]; !ok {
			return ObservedTaskOutcome{}, fmt.Errorf("%w: plan revision %d is missing", ErrIncompleteRuntimeEvidence, revision)
		}
	}

	var executions, verifyPassed, verifyTotal int
	approvals := make(map[string]int)
	verified := make(map[runtimeStepKey]int)
	executed := make(map[runtimeStepKey]int)
	effectSteps := make(map[runtimeStepKey]int)
	effectDigests := make(map[runtimeStepKey]string)
	requestedApproval := make(map[runtimeStepKey]string)
	approved := make(map[runtimeStepKey]string)
	confirmed := make(map[uint64]string)
	for _, event := range events {
		if event.StepID != "" && !stepInSnapshot(plans, event) {
			return ObservedTaskOutcome{}, fmt.Errorf("%w: step event does not match its plan", ErrIncompleteRuntimeEvidence)
		}
		if event.Type == agentrun.TaskEventPlanConfirmation && event.Outcome == "CONFIRMED" {
			confirmed[event.PlanRevision] = event.PlanDigest
		}
		if event.StepID != "" && confirmed[event.PlanRevision] != event.PlanDigest {
			return ObservedTaskOutcome{}, fmt.Errorf("%w: step occurred before plan confirmation", ErrIncompleteRuntimeEvidence)
		}
		if event.Type == agentrun.TaskEventApprovalRequest {
			key := runtimeStepKey{revision: event.PlanRevision, digest: event.PlanDigest, stepID: event.StepID}
			if requestedApproval[key] != "" {
				return ObservedTaskOutcome{}, fmt.Errorf("%w: duplicate approval request for %q", ErrIncompleteRuntimeEvidence, key.stepID)
			}
			requestedApproval[key] = event.ApprovalDigest
			approvals[fmt.Sprintf("T%d", event.Tier)]++
		}
		if event.Type == agentrun.TaskEventApprovalOutcome && event.Outcome == "APPROVED" {
			key := runtimeStepKey{revision: event.PlanRevision, digest: event.PlanDigest, stepID: event.StepID}
			if approved[key] != "" {
				return ObservedTaskOutcome{}, fmt.Errorf("%w: duplicate approval outcome for %q", ErrIncompleteRuntimeEvidence, key.stepID)
			}
			if requestedApproval[key] != event.ApprovalDigest {
				return ObservedTaskOutcome{}, fmt.Errorf("%w: approval preceded or mismatched its request", ErrIncompleteRuntimeEvidence)
			}
			approved[key] = event.ApprovalDigest
		}
		if event.Type == agentrun.TaskEventStepVerification {
			key := runtimeStepKey{revision: event.PlanRevision, digest: event.PlanDigest, stepID: event.StepID}
			if executed[key] != 1 {
				return ObservedTaskOutcome{}, fmt.Errorf("%w: verification preceded execution", ErrIncompleteRuntimeEvidence)
			}
			verified[key]++
			verifyTotal++
			if event.Outcome == "VERIFIED" {
				verifyPassed++
			}
		}
		if event.Type == agentrun.TaskEventStepExecution {
			key := runtimeStepKey{revision: event.PlanRevision, digest: event.PlanDigest, stepID: event.StepID}
			if event.Tier >= agentrun.TierSubmitGoverned && (approved[key] == "" || approved[key] != requestedApproval[key]) {
				return ObservedTaskOutcome{}, fmt.Errorf("%w: high-tier execution preceded approval", ErrIncompleteRuntimeEvidence)
			}
			if task.State == agentrun.StateCompleted && event.Outcome != "SUCCEEDED" {
				return ObservedTaskOutcome{}, fmt.Errorf("%w: completed task contains failed execution", ErrIncompleteRuntimeEvidence)
			}
			executed[key]++
			executions++
		}
		if event.Type == agentrun.TaskEventStepEffect {
			key := runtimeStepKey{revision: event.PlanRevision, digest: event.PlanDigest, stepID: event.StepID}
			if executed[key] != 1 || approved[key] != event.ApprovalDigest {
				return ObservedTaskOutcome{}, fmt.Errorf("%w: effect preceded its approved execution", ErrIncompleteRuntimeEvidence)
			}
			effectSteps[key]++
			effectDigests[key] = event.ApprovalDigest
		}
	}
	if executions == 0 || verifyTotal == 0 {
		return ObservedTaskOutcome{}, fmt.Errorf("%w: step execution and VERIFY outcomes are required", ErrIncompleteRuntimeEvidence)
	}
	for key, count := range verified {
		if count != 1 || executed[key] != 1 {
			return ObservedTaskOutcome{}, fmt.Errorf("%w: VERIFY outcome is missing, duplicated, or detached for %q", ErrIncompleteRuntimeEvidence, key.stepID)
		}
	}
	for key, count := range executed {
		if count != 1 {
			return ObservedTaskOutcome{}, fmt.Errorf("%w: duplicate execution outcome for %q", ErrIncompleteRuntimeEvidence, key.stepID)
		}
		if stepType(plans, key) == agentrun.StepVerify && verified[key] != 1 {
			return ObservedTaskOutcome{}, fmt.Errorf("%w: VERIFY execution has no owner-verifier outcome", ErrIncompleteRuntimeEvidence)
		}
		if tier(plans, key) >= agentrun.TierSubmitGoverned && effectSteps[key] != 1 {
			return ObservedTaskOutcome{}, fmt.Errorf("%w: high-tier execution has no effect outcome", ErrIncompleteRuntimeEvidence)
		}
		if tier(plans, key) >= agentrun.TierSubmitGoverned && (requestedApproval[key] == "" || requestedApproval[key] != approved[key]) {
			return ObservedTaskOutcome{}, fmt.Errorf("%w: high-tier execution has no matching approval", ErrIncompleteRuntimeEvidence)
		}
	}
	if task.State == agentrun.StateCompleted {
		if task.CurrentStep != len(task.Plan.Steps) {
			return ObservedTaskOutcome{}, fmt.Errorf("%w: completed task cursor does not match its plan", ErrIncompleteRuntimeEvidence)
		}
		for _, step := range task.Plan.Steps {
			key := runtimeStepKey{revision: task.Plan.Revision, digest: task.Plan.Digest, stepID: step.ID}
			if executed[key] != 1 {
				return ObservedTaskOutcome{}, fmt.Errorf("%w: completed task step %q has no unique execution event", ErrIncompleteRuntimeEvidence, step.ID)
			}
		}
	}
	for key, count := range effectSteps {
		if count != 1 || executed[key] != 1 || effectDigests[key] == "" || effectDigests[key] != requestedApproval[key] || effectDigests[key] != approved[key] {
			return ObservedTaskOutcome{}, fmt.Errorf("%w: effect outcome is missing, duplicated, or detached for %q", ErrIncompleteRuntimeEvidence, key.stepID)
		}
	}
	for key := range approved {
		if requestedApproval[key] != approved[key] {
			return ObservedTaskOutcome{}, fmt.Errorf("%w: approval outcome is detached from its request for %q", ErrIncompleteRuntimeEvidence, key.stepID)
		}
	}
	revisions := len(plans) - 1
	return ObservedTaskOutcome{
		Completed: task.State == agentrun.StateCompleted, VerifyPassed: verifyPassed, VerifyTotal: verifyTotal,
		PlanRevisions: revisions, ApprovalsRequested: approvals, Steps: executions,
		WallClock: observation.TerminalAt.Sub(task.CreatedAt), CostMicros: observation.SettledUsage.SpendMicros,
	}, nil
}

type runtimeStepKey struct {
	revision uint64
	digest   string
	stepID   string
}

func stepInSnapshot(plans map[uint64]agentrun.HistoricalPlanSnapshot, event agentrun.TaskEvent) bool {
	plan, ok := plans[event.PlanRevision]
	if !ok || plan.Digest != event.PlanDigest {
		return false
	}
	for _, step := range plan.Steps {
		if step.ID == event.StepID {
			return step.Type == event.StepType && step.Tier == event.Tier
		}
	}
	return false
}

func stepType(plans map[uint64]agentrun.HistoricalPlanSnapshot, key runtimeStepKey) agentrun.StepType {
	step, ok := findSnapshotStep(plans[key.revision], key.stepID)
	if !ok {
		return ""
	}
	return step.Type
}

func tier(plans map[uint64]agentrun.HistoricalPlanSnapshot, key runtimeStepKey) agentrun.Tier {
	step, ok := findSnapshotStep(plans[key.revision], key.stepID)
	if !ok {
		return agentrun.TierExternalWrite + 1
	}
	return step.Tier
}

func findSnapshotStep(plan agentrun.HistoricalPlanSnapshot, id string) (agentrun.PlanStepIdentity, bool) {
	for _, step := range plan.Steps {
		if step.ID == id {
			return step, true
		}
	}
	return agentrun.PlanStepIdentity{}, false
}
