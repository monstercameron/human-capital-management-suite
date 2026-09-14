package devclock

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/devprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// TestTodo_PROMOUX_014_Security is PROMOUX-014's SECURITY matrix test. It
// proves, by value rather than by error string, that this package's
// timer-firing capability is absent from the ordinary build -- the one
// `go build ./...`, cmd/hcmnext and CI all produce -- not merely refused by
// a runtime check that a differently configured caller could get past.
//
// This file carries no build tag, so it runs under the default `go test`
// this todo's own gate list requires. When that default build also happens
// to be a `-tags devtools` one (a developer deliberately testing the driver
// half), the absence assertions do not apply and are skipped rather than
// asserting something false of that build; see builtWithDevtools.
func TestTodo_PROMOUX_014_Security(t *testing.T) {
	t.Run("New fails closed for every profile string that is not exactly devprofile.Name", func(t *testing.T) {
		for _, profile := range []string{
			"", "standard", "production", "Local-Dev", "local-dev ", " local-dev", "LOCAL-DEV", "local_dev",
		} {
			if _, err := New(profile); !errors.Is(err, ErrProductionProfile) {
				t.Errorf("New(%q) err = %v, want ErrProductionProfile", profile, err)
			}
		}
	})

	if builtWithDevtools {
		t.Skip("this test binary was built with -tags devtools; PROMOUX-014's absence proof applies only to the default build (run `go test ./internal/platform/devclock/...` with no -tags to verify it)")
	}

	t.Run("even the exact local-dev profile gets no working driver in this build", func(t *testing.T) {
		if _, err := New(devprofile.Name); !errors.Is(err, ErrNotBuilt) {
			t.Fatalf("New(%q) err = %v, want ErrNotBuilt: the default build must not construct a working driver even for the right profile",
				devprofile.Name, err)
		}
	})

	t.Run("FireNow's refusal is byte-identical regardless of the timer, tenant or instant presented", func(t *testing.T) {
		// d is whatever New(devprofile.Name) actually returned above: in
		// this build that is always the zero Driver, but the point of this
		// sub-test is that FireNow itself never differentiates by argument,
		// so it is exercised directly rather than re-deriving d.
		var d Driver
		ctx := context.Background()
		sched := timer.Scheduler{}

		resultA, errA := d.FireNow(ctx, nil, sched,
			uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			uuid.MustParse("00000000-0000-0000-0000-0000000000aa"),
			time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), lease.Fence{Token: 1})
		resultB, errB := d.FireNow(ctx, nil, sched,
			uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff"),
			uuid.MustParse("00000000-0000-0000-0000-0000000000bb"),
			time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC), lease.Fence{Token: 999})

		if resultA != resultB {
			t.Fatalf("FireNow settled value differs across differing underlying state: %+v vs %+v", resultA, resultB)
		}
		if !errors.Is(errA, ErrNotBuilt) || !errors.Is(errB, ErrNotBuilt) {
			t.Fatalf("FireNow errors = %v / %v, want ErrNotBuilt on both regardless of arguments", errA, errB)
		}
		if errA.Error() != errB.Error() {
			t.Fatalf("FireNow error text differs across differing underlying state: %q vs %q", errA.Error(), errB.Error())
		}
	})

	t.Run("a Driver{} literal built without New is inert too", func(t *testing.T) {
		var d Driver
		if _, err := d.FireNow(context.Background(), nil, timer.Scheduler{},
			uuid.New(), uuid.New(), time.Now(), lease.Fence{}); !errors.Is(err, ErrNotBuilt) {
			t.Fatalf("zero-value Driver.FireNow err = %v, want ErrNotBuilt", err)
		}
	})

	t.Run("Clock is refused identically regardless of how far ahead it is asked to run", func(t *testing.T) {
		var d Driver
		clockA, errA := d.Clock(time.Minute)
		clockB, errB := d.Clock(24 * 365 * time.Hour)
		if clockA != nil || clockB != nil {
			t.Fatalf("Clock returned a working function in this build (non-nil: %t / %t)", clockA != nil, clockB != nil)
		}
		if !errors.Is(errA, ErrNotBuilt) || !errors.Is(errB, ErrNotBuilt) {
			t.Fatalf("Clock errors = %v / %v, want ErrNotBuilt on both", errA, errB)
		}
		if errA.Error() != errB.Error() {
			t.Fatalf("Clock error text differs by how far ahead was requested: %q vs %q", errA.Error(), errB.Error())
		}
	})
}
