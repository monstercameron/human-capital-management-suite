package application

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	executionscheduler "github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
)

// agentWakeRole is the scheduler's agent seam (AGENT2-011): on every tick it
// asks the composed agent runtime to deliver due wakes to the tenant's parked
// tasks, settle stale ones and drive runnable ones. It is not a second
// scheduler; it rides the workflow scheduler's existing recovery role.
//
// Like the workflow recovery role it runs on every tick whether or not this
// replica holds the queue lease. That is safe because the agent runtime
// dedupes: wake events carry deterministic ids that the task store's inbox
// collapses and every task transition is version-checked, so two replicas
// ticking the same tenant move a task once.
//
// The waker is read at tick time, so a composition that installs
// Cell.AgentWaker after the scheduler workload is built still takes effect,
// and a cell with no agent runtime ticks nothing.
type agentWakeRole struct {
	tenant string
	waker  func() app.AgentWaker
}

// RunRecoveryRole ticks the tenant's agent runtime and reports how many tasks
// it moved.
func (r agentWakeRole) RunRecoveryRole(ctx context.Context, _ lease.AcquireRequest, now time.Time) (int, error) {
	if r.waker == nil {
		return 0, nil
	}
	waker := r.waker()
	if waker == nil {
		return 0, nil
	}
	return waker.TickTenant(ctx, r.tenant, now)
}

// recoveryRoleFanOut runs several recovery roles for one claim. Every role
// runs even when an earlier one fails, counts are summed and errors joined, so
// one role's failure never hides another's work or error.
type recoveryRoleFanOut []executionscheduler.RecoveryRole

// RunRecoveryRole runs each role in order.
func (f recoveryRoleFanOut) RunRecoveryRole(ctx context.Context, claim lease.AcquireRequest, now time.Time) (int, error) {
	total := 0
	var errs []error
	for _, role := range f {
		if role == nil {
			continue
		}
		n, err := role.RunRecoveryRole(ctx, claim, now)
		total += n
		if err != nil {
			errs = append(errs, err)
		}
	}
	return total, errors.Join(errs...)
}

// newSchedulerRecoveryRole is the scheduler's recovery role for one served
// tenant: the workflow recovery sweep first, then the agent wake role.
func newSchedulerRecoveryRole(workflowRecovery executionscheduler.RecoveryRole, tenant string, cell *app.Cell) executionscheduler.RecoveryRole {
	return recoveryRoleFanOut{
		workflowRecovery,
		agentWakeRole{tenant: tenant, waker: func() app.AgentWaker {
			if cell == nil {
				return nil
			}
			return cell.AgentWaker
		}},
	}
}
