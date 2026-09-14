//go:build !devtools

package devclock

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// builtWithDevtools lets a build-tag-agnostic test (one compiled either
// way, such as TestTodo_PROMOUX_014_Security) tell which half of this
// package it linked, since a build tag itself is invisible at runtime.
const builtWithDevtools = false

// newDriver is what every build without `-tags devtools` links: it never
// constructs a working Driver, regardless of how New's profile check went.
func newDriver() (Driver, error) {
	return Driver{}, ErrNotBuilt
}

// fireNow reads none of its arguments. That is the point: this is the
// function TestTodo_PROMOUX_014_Security calls with two different tenants,
// timers and instants and proves returns byte-identical output either way
// -- not a runtime check that happens to refuse both, but a body with
// nothing in it capable of differentiating them because it never reads
// them, and never imports internal/workflow/timer's Scheduler.Fire at all.
func (d Driver) fireNow(context.Context, timer.Executor, timer.Scheduler, uuid.UUID, uuid.UUID, time.Time, lease.Fence) (timer.Settled, error) {
	// d.ready is intentionally read and discarded: this build must still
	// refuse even a forged Driver{ready: true}, so the field is not left
	// meaningful only in the devtools half of this package.
	_ = d.ready
	return timer.Settled{}, ErrNotBuilt
}

// clock reads none of its arguments either, for the same reason fireNow
// does not.
func (d Driver) clock(time.Duration) (func() time.Time, error) {
	return nil, ErrNotBuilt
}
