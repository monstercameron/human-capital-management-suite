package agenteval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrInvalidObservation marks a request without the required identity or readers.
	ErrInvalidObservation = errors.New("agenteval: invalid observation request")
	// ErrTaskNotTerminal means the task does not have durable terminal evidence.
	ErrTaskNotTerminal = errors.New("agenteval: task has no terminal timestamp")
	// ErrIdentityMismatch means a store returned a different tenant or task.
	ErrIdentityMismatch = errors.New("agenteval: observation identity mismatch")
)

// TaskReader must be backed by a tenant-scoped durable agentrun store.
type TaskReader interface {
	Get(context.Context, string) (agentrun.AgentTask, error)
}

// SettledUsageReader returns persisted task usage without in-flight
// reservations. agentbudgetstore.Store implements this interface.
type SettledUsageReader interface {
	SettledTaskUsage(context.Context, values.TenantId, string) (agentbudget.SettledTaskUsage, error)
}

// TaskObservation contains only durable evidence. TerminalAt is the persisted
// task UpdatedAt value, which is set by the terminal transition and remains
// stable because terminal tasks reject further transitions.
type TaskObservation struct {
	TenantID     string
	TaskID       string
	State        agentrun.TaskState
	TerminalAt   time.Time
	SettledUsage agentbudget.Usage
}

// ObserveTask reads one terminal task and its settled usage from tenant-bound
// stores. It fails closed when either source is missing or identity differs.
func ObserveTask(ctx context.Context, tenant values.TenantId, taskID string, tasks TaskReader, usage SettledUsageReader) (TaskObservation, error) {
	if ctx == nil || strings.TrimSpace(string(tenant)) == "" || strings.TrimSpace(taskID) == "" || tasks == nil || usage == nil {
		return TaskObservation{}, fmt.Errorf("%w: tenant, task id and readers are required", ErrInvalidObservation)
	}
	task, err := tasks.Get(ctx, taskID)
	if err != nil {
		return TaskObservation{}, err
	}
	if task.ID != taskID || task.TenantID != string(tenant) {
		return TaskObservation{}, fmt.Errorf("%w: task store returned %q/%q", ErrIdentityMismatch, task.TenantID, task.ID)
	}
	if !terminal(task.State) || task.UpdatedAt.IsZero() {
		return TaskObservation{}, ErrTaskNotTerminal
	}
	settled, err := usage.SettledTaskUsage(ctx, tenant, taskID)
	if err != nil {
		return TaskObservation{}, err
	}
	if settled.TenantID != string(tenant) || settled.TaskID != taskID {
		return TaskObservation{}, fmt.Errorf("%w: usage store returned %q/%q", ErrIdentityMismatch, settled.TenantID, settled.TaskID)
	}
	if settled.Usage.Steps < 0 || settled.Usage.Tokens < 0 || settled.Usage.WallClock < 0 || settled.Usage.SpendMicros < 0 {
		return TaskObservation{}, fmt.Errorf("%w: settled usage is invalid", ErrInvalidObservation)
	}
	return TaskObservation{TenantID: string(tenant), TaskID: taskID, State: task.State,
		TerminalAt: task.UpdatedAt.UTC(), SettledUsage: settled.Usage}, nil
}

func terminal(state agentrun.TaskState) bool {
	switch state {
	case agentrun.StateCompleted, agentrun.StateFailed, agentrun.StateCancelled, agentrun.StateExpired:
		return true
	default:
		return false
	}
}
