package agentsystem

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

// TaskActionPolicy is the server-computed affordance projection for one task.
// It is advisory; ControlTask repeats the authority, owner and CAS checks.
type TaskActionPolicy struct {
	ConfirmPlan  bool
	Pause        bool
	Resume       bool
	Cancel       bool
	ExtendBudget bool
}

const (
	controlPurpose = "agent.self_service"
	controlRead    = "hcmnext.agent.read_own_worker_state"
)

// TaskPolicy resolves current durable authority and runtime legality before
// exposing an action. Any missing authority or unknown state fails closed.

func (r *Runner) TaskPolicy(ctx context.Context, task agentrun.AgentTask, userID string) TaskActionPolicy {
	if r != nil && r.p != nil && r.p.cfg.Clock != nil {
		now := r.p.cfg.Clock().UTC()
		return r.taskPolicyAt(ctx, task, userID, now)
	}
	return TaskActionPolicy{}
}

func (r *Runner) taskPolicyAt(ctx context.Context, task agentrun.AgentTask, userID string, now time.Time) TaskActionPolicy {
	if r == nil || r.p == nil || r.p.cfg.Authority == nil || strings.TrimSpace(userID) == "" || now.IsZero() || task.UserID != userID || task.TenantID != r.tenant.String() || task.State == agentrun.StateCompleted || task.State == agentrun.StateFailed || task.State == agentrun.StateCancelled || task.State == agentrun.StateExpired {
		return TaskActionPolicy{}
	}
	if !r.hasControlAuthority(userID, now) {
		return TaskActionPolicy{}
	}
	switch task.State {
	case agentrun.StateAwaitingPlanConfirmation, agentrun.StateDrafting:
		return TaskActionPolicy{ConfirmPlan: true, Cancel: true}
	case agentrun.StateRunning, agentrun.StateWaiting, agentrun.StateAwaitingApproval:
		return TaskActionPolicy{Pause: true, Cancel: true}
	case agentrun.StatePaused:
		policy := TaskActionPolicy{Resume: true, Cancel: true}
		for _, budget := range r.p.cfg.Budget.Snapshot().Tasks {
			if budget.ID == task.ID && r.p.cfg.Budget.ExtensionEnabled() && budget.Paused != "" && budget.Paused != agentbudget.PauseLoopDetected && budget.Paused != agentbudget.PauseRetryLimit {
				policy.ExtendBudget = true
			}
		}
		return policy
	default:
		return TaskActionPolicy{}
	}
}

func (r *Runner) hasControlAuthority(userID string, now time.Time) bool {
	if r == nil || r.p == nil || r.p.cfg.Authority == nil || strings.TrimSpace(userID) == "" || now.IsZero() {
		return false
	}
	current, err := r.p.cfg.Authority.Resolve(userID, r.tenant, controlPurpose, now)
	return err == nil && current.Active && current.UserID == userID && current.Authority.Tenant == r.tenant &&
		slices.Contains(current.Authority.Capabilities, controlRead) && slices.Contains(current.Authority.Purposes, controlPurpose)
}

// TaskAction is the server-side vocabulary exposed by the Agents transport.
type TaskAction string

const (
	ActionConfirmPlan  TaskAction = "CONFIRM_PLAN"
	ActionPause        TaskAction = "PAUSE"
	ActionResume       TaskAction = "RESUME"
	ActionCancel       TaskAction = "CANCEL"
	ActionExtendBudget TaskAction = "EXTEND_BUDGET"
)

// ExtendBudgetRequest carries the user-owned, replay-safe budget mutation.
// Additional is evaluated against the configured policy by agentbudget.
type ExtendBudgetRequest struct {
	TaskID           string
	UserID           string
	RequestID        string
	ExpectedRevision uint64
	Additional       agentbudget.Limits
	Now              time.Time
}

// ExtendBudget applies a budget extension only while the durable runtime task
// is paused, under the same fresh self-service authority used by task controls.
// It deliberately leaves the runtime task paused; Resume is a separate CAS
// action so an extension cannot smuggle in execution.
func (r *Runner) ExtendBudget(ctx context.Context, taskID, userID, requestID string, expectedBudgetRevision uint64, additional agentbudget.Limits, now time.Time) (agentbudget.ExtensionResult, error) {
	if r == nil || r.p == nil || strings.TrimSpace(taskID) == "" || strings.TrimSpace(userID) == "" || strings.TrimSpace(requestID) == "" || expectedBudgetRevision == 0 || now.IsZero() || !validExtensionLimits(additional) {
		return agentbudget.ExtensionResult{}, fmt.Errorf("%w: task, user, request, revision, additional and time are required", ErrInvalid)
	}
	if r.p.cfg.WakeGate != nil && !r.p.cfg.WakeGate(ctx, r.tenant.String()) {
		return agentbudget.ExtensionResult{}, fmt.Errorf("%w: tenant agents are disabled", ErrDenied)
	}
	task, err := r.ownedTask(ctx, taskID, userID)
	if err != nil {
		return agentbudget.ExtensionResult{}, err
	}
	if task.TenantID != r.tenant.String() || task.UserID != userID {
		return agentbudget.ExtensionResult{}, fmt.Errorf("%w: task owner or tenant mismatch", ErrDenied)
	}
	if task.State != agentrun.StatePaused {
		return agentbudget.ExtensionResult{}, fmt.Errorf("%w: budget extension requires a paused runtime task", ErrDenied)
	}
	if !r.hasControlAuthority(userID, now) {
		return agentbudget.ExtensionResult{}, fmt.Errorf("%w: current task control authority is required", ErrDenied)
	}
	result, err := r.p.cfg.Budget.AcceptExtensionCAS(agentbudget.ExtensionRequest{TaskID: taskID, RequestID: requestID, ExpectedRevision: expectedBudgetRevision, Additional: additional})
	if err != nil {
		return agentbudget.ExtensionResult{}, err
	}
	return result, nil
}

func validExtensionLimits(l agentbudget.Limits) bool {
	return l.Steps >= 0 && l.Tokens >= 0 && l.WallClock >= 0 && l.SpendMicros >= 0 && (l.Steps > 0 || l.Tokens > 0 || l.WallClock > 0 || l.SpendMicros > 0)
}

// ControlTask applies one owner-scoped, version-checked action. The caller
// must have already resolved current authority; this method still checks the
// durable task owner and tenant before any state transition.
func (r *Runner) ControlTask(ctx context.Context, taskID, userID string, action TaskAction, expectedVersion uint64, now time.Time) (agentrun.AgentTask, error) {
	if r == nil || strings.TrimSpace(taskID) == "" || strings.TrimSpace(userID) == "" || expectedVersion == 0 || now.IsZero() {
		return agentrun.AgentTask{}, fmt.Errorf("%w: task, user, version and time are required", ErrInvalid)
	}
	task, err := r.ownedTask(ctx, taskID, userID)
	if err != nil {
		return task, err
	}
	if task.TenantID != r.tenant.String() {
		return task, fmt.Errorf("%w: task tenant mismatch", ErrDenied)
	}
	if !r.hasControlAuthority(userID, now) {
		return task, fmt.Errorf("%w: current task control authority is required", ErrDenied)
	}
	switch action {
	case ActionConfirmPlan:
		return r.Runtime.ConfirmPlan(ctx, taskID, userID, expectedVersion, now)
	case ActionPause:
		return r.Runtime.Pause(ctx, taskID, expectedVersion, now)
	case ActionResume:
		return r.Runtime.Resume(ctx, taskID, expectedVersion, now)
	case ActionCancel:
		return r.Runtime.Cancel(ctx, taskID, expectedVersion, now)
	default:
		return task, fmt.Errorf("%w: unknown task action %q", ErrInvalid, action)
	}
}
