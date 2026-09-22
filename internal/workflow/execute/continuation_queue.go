package execute

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// continueAfterAdvance is shared by every durable resume boundary. A wait is
// local to its branch: drain READY work before returning control to the caller.
func (d *Driver) continueAfterAdvance(ctx context.Context, run runContext, result Result, advanced runtime.AdvanceReceipt) (Result, error) {
	if advanced.Complete {
		result.Status = StatusComplete
		return result, nil
	}
	ready, parked := readyAndParked(advanced.Continuations, result.Timers...)
	if len(ready) > 0 {
		return d.drainReady(ctx, run, result, ready)
	}
	if parked {
		result.Status = StatusParked
		return result, nil
	}
	return d.parkWaitingFrontier(ctx, run, result)
}

// A sibling may have parked in an earlier advancement, or before recovery
// began. Inspect durable state once at the drained boundary, not on every
// step. An empty or unexpectedly runnable frontier is not a successful wait.
func (d *Driver) parkWaitingFrontier(ctx context.Context, run runContext, result Result) (Result, error) {
	err := d.inTenantTx(ctx, run.start.TenantID, func(tx runtime.Executor) error {
		store := runtime.Store{}
		inst, err := store.LoadInstance(ctx, tx, run.start.TenantID, run.instanceID)
		if err != nil {
			return err
		}
		if inst.InstanceVersion != result.InstanceVersion || inst.CompiledPlanHash != run.selection.Plan.Digest() {
			return fmt.Errorf("%w: instance %s changed while draining READY work", ErrNoProgress, run.instanceID)
		}
		if inst.RuntimeStatus.Terminal() || len(inst.CurrentNodeIDs) == 0 {
			return fmt.Errorf("%w: instance %s has no waiting frontier", ErrNoProgress, run.instanceID)
		}
		rows, err := store.LoadNodeExecutions(ctx, tx, run.start.TenantID, run.instanceID)
		if err != nil {
			return err
		}
		latest := make(map[string]runtime.NodeExecution, len(rows))
		for _, row := range rows {
			if prior, ok := latest[row.NodeID]; !ok || row.Attempt > prior.Attempt {
				latest[row.NodeID] = row
			}
		}
		for _, id := range inst.CurrentNodeIDs {
			row, ok := latest[id]
			if !ok || (row.Status != runtime.NodeWaiting && !(row.Status == runtime.NodeRetrying && retryParked(result.Timers, id))) {
				return fmt.Errorf("%w: frontier node %s is not waiting", ErrNoProgress, id)
			}
		}
		result.Frontier = append([]string(nil), inst.CurrentNodeIDs...)
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	result.Status = StatusParked
	return result, nil
}
