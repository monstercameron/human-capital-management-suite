package scheduler

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// RecoveryRole redelivers work a dead driver lost mid-drain (WF-RUN-003).
//
// The scheduler's own recover step returns abandoned workflow_ready_work rows
// to the pool, but a driver that died during a synchronous drain -- an intent
// execution, an approval completion, a timer resume past its WAIT node --
// leaves READY node executions behind with no ready-work row at all. The role
// is the seam for the sweep that finds and redelivers those
// (internal/workflow/recover.Sweeper); like the signal role it is a port, so
// this package keeps no workflow semantics. It runs on every tick whether or
// not this replica holds the queue lease: each orphan is claimed under its own
// WORKFLOW_INSTANCE lease takeover, which is what keeps two replicas from
// redelivering the same instance.
type RecoveryRole interface {
	RunRecoveryRole(ctx context.Context, claim lease.AcquireRequest, now time.Time) (int, error)
}

// runRecoveryRole runs the configured role for one claim, instrumented, and
// reports how many instances it redelivered. A nil role redelivers nothing.
func runRecoveryRole(ctx context.Context, role RecoveryRole, logger Logger, claim lease.AcquireRequest, now time.Time) (count int, err error) {
	if role == nil {
		return 0, nil
	}
	ctx, op := observe.Begin(ctx, "workflow.scheduler.recovery_role", claim)
	defer func() { observe.DoneWith(op, err, count) }()
	count, err = role.RunRecoveryRole(ctx, claim, now)
	if count > 0 {
		logger.Info("scheduler.recovery_role_tick",
			"tenant", claim.TenantID.String(), "queue", claim.Resource.ID, "redelivered", count)
	}
	if err != nil {
		logger.Error("scheduler.recovery_role_failed",
			"tenant", claim.TenantID.String(), "queue", claim.Resource.ID,
			"redelivered", count, "error", err.Error())
	}
	return count, err
}
