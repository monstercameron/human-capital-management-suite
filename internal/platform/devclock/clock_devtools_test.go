//go:build devtools

package devclock

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/devprofile"
)

// TestClockAdvancesAheadOfTheRealHostClock is a pure, DB-free unit test of
// the devtools half of Driver.Clock: PROMOUX-014's actual "drive the clock
// ahead" arithmetic, exercised directly rather than only incidentally
// through TestTodo_PROMOUX_014_Recovery's full pgtest harness (in
// internal/application, which is the natural place for that end-to-end
// proof, but leaves this package's own `go test -tags devtools` coverage
// thin without a test like this one).
func TestClockAdvancesAheadOfTheRealHostClock(t *testing.T) {
	driver, err := New(devprofile.Name)
	if err != nil {
		t.Fatalf("New(%q): %v", devprofile.Name, err)
	}

	if _, err := driver.Clock(0); err == nil {
		t.Fatal("Clock(0) succeeded, want an error: a fixture is advanced, never left in place")
	}
	if _, err := driver.Clock(-time.Hour); err == nil {
		t.Fatal("Clock(-1h) succeeded, want an error: a fixture is advanced, never reversed")
	}

	before := time.Now().UTC()
	clock, err := driver.Clock(2 * time.Hour)
	if err != nil {
		t.Fatalf("Clock(2h): %v", err)
	}
	got := clock()
	if got.Before(before.Add(90 * time.Minute)) {
		t.Fatalf("Clock(2h)() = %s, want at least ~2h ahead of %s", got, before)
	}
	if got.After(time.Now().UTC().Add(3 * time.Hour)) {
		t.Fatalf("Clock(2h)() = %s, want no more than ~2h ahead of now", got)
	}

	if _, err := New("standard"); err == nil {
		t.Fatal("New(\"standard\") succeeded in a devtools build; the profile check must still hold")
	}
}
