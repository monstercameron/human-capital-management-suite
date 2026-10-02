package agentsystem

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

// PreviousPlan returns the plan revision that was in force before the task's
// current one: the newest earlier revision the event history holds. The task
// view uses it to show the owner what a new revision adds and drops before
// they confirm it. Only the task's owner may read it. A task still on its
// first plan, or a store that keeps no events, has no previous plan.
func (r *Runner) PreviousPlan(ctx context.Context, taskID, userID string) (agentrun.HistoricalPlanSnapshot, bool, error) {
	if r == nil || r.Runtime == nil {
		return agentrun.HistoricalPlanSnapshot{}, false, ErrNotConfigured
	}
	task, err := r.Runtime.GetTask(ctx, taskID)
	if err != nil {
		return agentrun.HistoricalPlanSnapshot{}, false, err
	}
	if strings.TrimSpace(userID) == "" || task.UserID != userID {
		return agentrun.HistoricalPlanSnapshot{}, false, fmt.Errorf("%w: only the task owner may read its plan history", ErrDenied)
	}
	if task.Plan.Revision <= 1 {
		return agentrun.HistoricalPlanSnapshot{}, false, nil
	}
	events, err := r.Runtime.TaskEvents(ctx, taskID)
	if err != nil {
		return agentrun.HistoricalPlanSnapshot{}, false, err
	}
	var previous agentrun.HistoricalPlanSnapshot
	found := false
	for _, event := range events {
		snapshot := event.PlanSnapshot
		if snapshot == nil || snapshot.Revision >= task.Plan.Revision || snapshot.Verify() != nil {
			continue
		}
		if !found || snapshot.Revision >= previous.Revision {
			previous, found = *snapshot, true
		}
	}
	return previous, found, nil
}
