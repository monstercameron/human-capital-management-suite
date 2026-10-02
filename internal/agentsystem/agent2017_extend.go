package agentsystem

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
)

// ExtendBudgetByAllowance is the one-click form of ExtendBudget: it extends a
// paused task by the most the configured extension policy allows, against the
// task's current budget revision. It checks the same things ExtendBudget does
// (owner, tenant, paused task, current control authority, the revision fence)
// and leaves the task paused; Resume stays a separate action.
func (r *Runner) ExtendBudgetByAllowance(ctx context.Context, taskID, userID, requestID string, now time.Time) (agentbudget.ExtensionResult, error) {
	if r == nil || r.p == nil || r.p.cfg.Budget == nil {
		return agentbudget.ExtensionResult{}, fmt.Errorf("%w: budget ledger is required", ErrInvalid)
	}
	allowance := r.p.cfg.Budget.ExtensionAllowance(taskID)
	if allowance == (agentbudget.Limits{}) {
		return agentbudget.ExtensionResult{}, fmt.Errorf("%w: budget extensions are not permitted by policy", ErrDenied)
	}
	var revision uint64
	for _, budget := range r.p.cfg.Budget.Snapshot().Tasks {
		if budget.ID == taskID {
			revision = budget.Revision
		}
	}
	if revision == 0 {
		return agentbudget.ExtensionResult{}, fmt.Errorf("%w: task has no budget to extend", ErrDenied)
	}
	return r.ExtendBudget(ctx, taskID, userID, requestID, revision, allowance, now)
}
