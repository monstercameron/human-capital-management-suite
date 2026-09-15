package execute

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// StatusPaused reports a run that stopped because a requested pause took
// effect at the safe point the run reached (WF-RUN-008).
const StatusPaused Status = "PAUSED"

// pauseActor is the actor recorded when the driver, rather than an operator
// call, is what reached the safe point a pending pause was waiting for.
const pauseActor = "workflow:execute-driver"

// pausedAtSafePoint carries the receipt of a pause the driver applied.
type pausedAtSafePoint struct {
	receipt runtime.PauseReceipt
}

func (p *pausedAtSafePoint) Error() string {
	return fmt.Sprintf("workflow execute: instance %s paused at a safe point (version %d)", p.receipt.InstanceID, p.receipt.InstanceVersion)
}

// settlePause turns an advancement refused because a requested pause stands
// at a safe point into the pause itself. Before WF-RUN-008 nothing in
// production called runtime.ApplyPause, so an instance whose pause was
// requested mid-region reached its safe point, was refused, and stayed
// PAUSE_REQUESTED forever. The pause is applied in its own tenant transaction;
// any other error is returned unchanged.
func (d *Driver) settlePause(ctx context.Context, run runContext, at time.Time, cause error) error {
	if runtime.CodeOf(cause) != runtime.CodeInstancePaused {
		return cause
	}
	var receipt runtime.PauseReceipt
	err := d.inTenantTx(ctx, run.start.TenantID, func(tx runtime.Executor) error {
		var aerr error
		receipt, aerr = runtime.ApplyPause(ctx, tx, runtime.PauseRequest{
			TenantID: run.start.TenantID, InstanceID: run.instanceID, Plan: run.selection.Plan,
			Reason: "safe point reached with a pause pending", RequestedBy: pauseActor, RequestedAt: at,
		})
		return aerr
	})
	if err != nil {
		return errors.Join(cause, fmt.Errorf("workflow execute: apply pending pause: %w", err))
	}
	// An instance that was already PAUSED (a replayed receipt) or is still
	// not at a safe point keeps the runtime's typed refusal unchanged.
	if receipt.Replay || receipt.Status != runtime.InstancePaused {
		return cause
	}
	return &pausedAtSafePoint{receipt: receipt}
}

// pausedResult converts a settled pause into the run's result.
func pausedResult(err error, result Result) (Result, bool) {
	var paused *pausedAtSafePoint
	if !errors.As(err, &paused) {
		return Result{}, false
	}
	result.Status = StatusPaused
	result.InstanceVersion = paused.receipt.InstanceVersion
	result.Frontier = append([]string(nil), paused.receipt.Frontier...)
	return result, true
}
