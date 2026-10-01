package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// PersonaInvocationTask is an owning task reader's persisted invocation link.
// The execution run's identity alone cannot establish that a task exists.
type PersonaInvocationTask struct {
	InvocationID string
	RunID        string
	Task         agentrun.AgentTask
}

// PersonaInvocationTaskReader reads the real task through its owner after the
// surface has authenticated the exact invoker and current room membership.
type PersonaInvocationTaskReader interface {
	ReadPersonaInvocationTask(context.Context, agentinvoke.Invocation, runstate.Run) (PersonaInvocationTask, bool, error)
}

func (s *PersonaChatSurface) projectPersonaInvocationTask(ctx context.Context, invocation agentinvoke.Invocation, run runstate.Run, out *personachat.Invocation) error {
	if s.Tasks == nil {
		return nil
	}
	linked, found, err := s.Tasks.ReadPersonaInvocationTask(ctx, invocation, run)
	if err != nil {
		return personachat.ErrUnavailable
	}
	if !found {
		return nil
	}
	task := linked.Task
	if linked.InvocationID != invocation.ID || linked.RunID != run.ID || task.TenantID != invocation.TenantID || task.UserID != invocation.InvokerID || strings.TrimSpace(task.ID) == "" || task.Version == 0 || strings.TrimSpace(task.Goal) == "" || !personaSurfaceTaskState(task.State) {
		return personachat.ErrUnavailable
	}
	out.TaskID, out.TaskTitle, out.TaskState, out.TaskRevision = task.ID, task.Goal, string(task.State), task.Version
	return nil
}

func personaSurfaceTaskState(state agentrun.TaskState) bool {
	switch state {
	case agentrun.StateDrafting, agentrun.StateAwaitingPlanConfirmation, agentrun.StateRunning, agentrun.StateWaiting, agentrun.StateAwaitingApproval, agentrun.StatePaused, agentrun.StateCompleted, agentrun.StateFailed, agentrun.StateCancelled, agentrun.StateExpired:
		return true
	default:
		return false
	}
}
