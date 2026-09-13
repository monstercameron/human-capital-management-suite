// Package devclock is PROMOUX-014's fenced local-dev completion path: a way
// to settle one durable effective-date timer immediately, so a developer can
// drive a same-day promotion fixture through terminal recording without
// waiting for the wall clock to actually reach the effective instant.
//
// It manufactures no new capability. Firing a due timer is exactly
// [timer.Scheduler.Fire] -- the identical call
// internal/platform/execution/scheduler.Scheduler.Tick makes on every real
// production tick -- and resuming the instance the fire wakes remains
// exactly internal/intent/app.Cell.ResumeFiredTimer, which this package does
// not touch at all; the caller runs that itself with the (instance, node,
// attempt) the fire reports. The only thing this package supplies is
// permission to call Fire with an `at` earlier than the timer's own
// scheduled instant would otherwise require waiting for.
//
// GREEN requires two things at once: production exposes no manual timer
// bypass, and this driver is impossible to enable under a production
// profile -- not merely refused at runtime. Both are structural rather than
// a runtime "if":
//
//  1. The code that can actually call Fire ahead of schedule lives in
//     driver_devtools.go, built only with `-tags devtools`. Neither
//     cmd/hcmnext's own build nor this repository's documented gates
//     (`go build ./...`, the cross-compile check, `go run ./tools/quality`)
//     ever pass that tag, so the ordinary and CI builds link
//     driver_stub.go instead: a function with the identical signature that
//     reads none of its arguments and always refuses [ErrNotBuilt]. A
//     production binary does not carry a flag that could turn this on
//     after the fact -- the capability is absent from the compiled binary,
//     not declined by it.
//  2. [New] additionally requires the caller to present
//     internal/trust/devprofile's own profile name, fail closed
//     (PROMOUX-008's pattern: an exact match is required, not "anything
//     that isn't the production name"), so a devtools-tagged binary
//     mistakenly pointed at a non-local-dev deployment still refuses. This
//     reuses devprofile rather than inventing a second profile mechanism,
//     per this todo's own instruction.
package devclock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/devprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// ErrProductionProfile reports a [New] call whose profile was not exactly
// [devprofile.Name].
var ErrProductionProfile = errors.New("devclock: refused outside the local-dev profile")

// ErrNotBuilt reports that this binary was not built with `-tags devtools`.
// It is what every method on [Driver] returns in that build, regardless of
// its arguments: see driver_stub.go.
var ErrNotBuilt = errors.New("devclock: this binary was not built with -tags devtools; the driver does not exist in it")

// Driver is the fenced local-dev completion path. Its zero value is safe to
// hold but every method on it refuses: ready is unexported precisely so
// that constructing a Driver{} literal directly -- skipping [New] -- cannot
// forge a working one from outside this package, in either build.
type Driver struct{ ready bool }

// New returns a Driver that can advance one durable timer immediately
// instead of waiting for its FiresAt instant to pass. profile must equal
// [devprofile.Name] exactly; anything else -- including the empty string, a
// production profile name, or a near-miss spelling -- is refused with
// [ErrProductionProfile] before this package's build-tagged half is ever
// reached. See the package doc for why that check is defence in depth
// rather than the fence itself.
func New(profile string) (Driver, error) {
	if profile != devprofile.Name {
		return Driver{}, fmt.Errorf("%w: got %q, want %q", ErrProductionProfile, profile, devprofile.Name)
	}
	return newDriver()
}

// FireNow settles the one durable timer named by timerID as though at were
// the current instant, under the caller's lease fence. It is
// [timer.Scheduler.Fire] restricted to that one timer with Only, so a
// caller advancing a fixture cannot accidentally settle every other due
// promise in the tenant at the same time -- "a same-day fixture", not "the
// tenant's clock".
//
// The caller is responsible for at being at or after the timer's own
// FiresAt (an earlier at simply reports the timer as not yet due, exactly
// as an ordinary Fire call would) and for resuming the instance the
// settlement wakes, via internal/intent/app.Cell.ResumeFiredTimer with the
// (InstanceID, NodeID, Attempt) [timer.Settled] names.
func (d Driver) FireNow(ctx context.Context, ex timer.Executor, sched timer.Scheduler, tenantID, timerID uuid.UUID, at time.Time, fence lease.Fence) (timer.Settled, error) {
	return d.fireNow(ctx, ex, sched, tenantID, timerID, at, fence)
}

// Clock returns a reading `ahead` later than the real host wall clock, in
// exactly the shape internal/platform/execution/scheduler.Config.Clock
// wants -- the seam internal/application/scheduler_workload.go's
// production composition leaves nil to get the real clock. Handing this to
// that same Scheduler is how a fixture is driven through END with the
// production Tick/Fire/dispatch/ResumeFiredTimer path unchanged: nothing
// about how a fired timer is resumed is this package's concern or this
// method's business, only how far ahead the clock a caller's own Scheduler
// reads may run. ahead must be positive: a fixture is advanced, never
// reversed.
func (d Driver) Clock(ahead time.Duration) (func() time.Time, error) {
	return d.clock(ahead)
}
