//go:build devtools

package devclock

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// builtWithDevtools lets a build-tag-agnostic test (one compiled either
// way, such as TestTodo_PROMOUX_014_Security) tell which half of this
// package it linked, since a build tag itself is invisible at runtime.
const builtWithDevtools = true

// newDriver is reached only once New's profile check has already passed. It
// stamps ready so that a Driver{} literal built directly by a caller --
// bypassing New -- stays inert even in this build.
func newDriver() (Driver, error) {
	return Driver{ready: true}, nil
}

// fireNow is the one piece of this package that a production build never
// links: see devclock.go's package doc. Its own ready guard is the
// defence-in-depth half (a Driver{} literal built without New refuses here
// too); the misfire policy is a one-catch-up allowance so a fixture whose
// FiresAt has already passed by the time a developer gets to it still
// settles ON_TIME/LATE rather than refusing for want of a declared policy.
func (d Driver) fireNow(ctx context.Context, ex timer.Executor, sched timer.Scheduler, tenantID, timerID uuid.UUID, at time.Time, fence lease.Fence) (timer.Settled, error) {
	if !d.ready {
		return timer.Settled{}, ErrProductionProfile
	}
	result, err := sched.Fire(ctx, ex, timer.FireRequest{
		TenantID: tenantID,
		Now:      at,
		Fence:    fence,
		Misfire:  schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: 7 * 24 * time.Hour, MaxCatchUp: 1},
		Only:     []uuid.UUID{timerID},
	})
	if err != nil {
		return timer.Settled{}, err
	}
	for _, group := range [][]timer.Settled{result.Fired, result.Skipped, result.Deferred} {
		for _, settled := range group {
			if settled.Timer.TimerID == timerID {
				return settled, nil
			}
		}
	}
	return timer.Settled{}, fmt.Errorf("devclock: timer %s was not due at %s", timerID, at)
}

// clock is fireNow's sibling: the same ready guard, the same reason it
// exists only in this build.
func (d Driver) clock(ahead time.Duration) (func() time.Time, error) {
	if !d.ready {
		return nil, ErrProductionProfile
	}
	if ahead <= 0 {
		return nil, fmt.Errorf("devclock: ahead must be positive; got %s", ahead)
	}
	return func() time.Time { return time.Now().UTC().Add(ahead) }, nil
}
