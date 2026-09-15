package execute

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Live-instance dispositions a governed version quarantine declares
// (WF-RUN-009).
const (
	QuarantinePause    = "PAUSE"
	QuarantineContinue = "CONTINUE"
	QuarantineBlock    = "BLOCK"
)

// VersionQuarantine reports, inside the advance transaction, whether the
// compiled version an instance is pinned to stands under a governed
// quarantine and which disposition its live instances take.
// internal/data/workflowversionstore.Store implements it.
type VersionQuarantine interface {
	LiveInstancePolicy(ctx context.Context, ex dbport.Conn, compiledPlanDigest string) (policy string, quarantined bool, err error)
}

// ErrVersionQuarantined reports an advancement refused because the instance's
// version is quarantined with a BLOCK disposition (or an unknown one, which
// fails closed).
var ErrVersionQuarantined = errors.New("workflow execute: workflow version is quarantined")

// errQuarantinePaused marks a quarantine pause that took effect at a safe
// point; the advance transaction commits the pause and the run reports
// [StatusPaused].
var errQuarantinePaused = errors.New("workflow execute: paused by version quarantine")

// quarantineActor is recorded on a pause the driver requests for a
// quarantined version.
const quarantineActor = "workflow:version-quarantine"

// applyQuarantine enforces the quarantine disposition before any step input
// or advancement is produced. CONTINUE advances normally. BLOCK refuses. PAUSE
// requests the pause in this transaction: at a safe point the instance is
// PAUSED and the run stops; inside an atomic region it is PAUSE_REQUESTED and
// this advancement proceeds (against the version the request produced) until
// the region reaches a safe point, where the runtime's pause gate stops it.
func (d *Driver) applyQuarantine(ctx context.Context, tx dbport.Tx, run runContext, expectedVersion int64, at time.Time) (int64, error) {
	if d.opts.Quarantine == nil {
		return expectedVersion, nil
	}
	policy, quarantined, err := d.opts.Quarantine.LiveInstancePolicy(ctx, tx, run.selection.Plan.Digest())
	if err != nil {
		return expectedVersion, fmt.Errorf("workflow execute: read version quarantine: %w", err)
	}
	if !quarantined || policy == QuarantineContinue {
		return expectedVersion, nil
	}
	if policy != QuarantinePause {
		return expectedVersion, fmt.Errorf("%w: instance %s is held by a %s disposition", ErrVersionQuarantined, run.instanceID, policy)
	}
	receipt, err := runtime.RequestPause(ctx, tx, runtime.PauseRequest{
		TenantID: run.start.TenantID, InstanceID: run.instanceID, ExpectedInstanceVersion: expectedVersion,
		Plan: run.selection.Plan, Reason: "workflow version quarantined", RequestedBy: quarantineActor, RequestedAt: at,
	})
	if err != nil {
		return expectedVersion, fmt.Errorf("workflow execute: request quarantine pause: %w", err)
	}
	if receipt.Status == runtime.InstancePaused {
		return receipt.InstanceVersion, errors.Join(errQuarantinePaused, &pausedAtSafePoint{receipt: receipt})
	}
	return receipt.InstanceVersion, nil
}
