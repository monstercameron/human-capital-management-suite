package app

import (
	"context"
	"time"
)

// AgentWaker is the scheduler's view of the composed agent runtime: one tick
// for one tenant delivers due wakes to that tenant's parked tasks, settles
// stale ones and drives tasks that became runnable. It returns how many tasks
// the tick moved. The scheduler calls it from its existing recovery seam, so
// there is no second scheduler.
type AgentWaker interface {
	TickTenant(ctx context.Context, tenant string, now time.Time) (int, error)
}
