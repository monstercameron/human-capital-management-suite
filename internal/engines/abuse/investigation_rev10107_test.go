package abuse

import (
	"testing"
	"time"
)

// fixedClock returns a clock pinned at t.
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func openFixedInvestigation(t *testing.T, now time.Time) Investigation {
	t.Helper()
	inv, err := OpenInvestigation(OpenRequest{
		ID: "inv-clock", Tenant: "acme", Compartment: "compartment:trust-safety",
		Finding:      abuseFinding(),
		Investigator: "investigator:case-9",
		EvidenceRefs: []string{"evidence:events-1"},
		OpenedAt:     time.Date(2026, 6, 15, 13, 0, 0, 0, time.UTC),
		Clock:        fixedClock(now),
	})
	if err != nil {
		t.Fatalf("OpenInvestigation: %v", err)
	}
	return inv
}

// TestTodo_REV_101_07_Property proves the injected clock governs every
// timestamp the lifecycle records: two lifecycles under the same fixed
// clock stamp identical instants (replayable), and moving the clock moves
// the stamps (no hidden wall read).
func TestTodo_REV_101_07_Property(t *testing.T) {
	pinned := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	run := func(t *testing.T) (transferAt, dispositionAt, correctionAt time.Time) {
		t.Helper()
		inv := openFixedInvestigation(t, pinned)
		var err error
		if inv, err = inv.Begin("investigator:case-9"); err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if inv, err = inv.Transfer("compartment:legal", "privilege", "investigator:case-9"); err != nil {
			t.Fatalf("Transfer: %v", err)
		}
		transferAt = inv.Trail[len(inv.Trail)-1].At
		// No DecidedAt: the fallback must come from the injected clock.
		if inv, err = inv.Disposition(DispositionRequest{
			Outcome: OutcomeSubstantiated, Reason: "confirmed", DecidedBy: "reviewer:lead-2",
		}); err != nil {
			t.Fatalf("Disposition: %v", err)
		}
		if inv.Result == nil {
			t.Fatal("disposition must record a result")
		}
		dispositionAt = inv.Result.DecidedAt
		if inv, err = inv.Correct("amended", "reviewer:lead-2"); err != nil {
			t.Fatalf("Correct: %v", err)
		}
		correctionAt = inv.Corrections[len(inv.Corrections)-1].At
		return transferAt, dispositionAt, correctionAt
	}

	firstTransfer, firstDisposition, firstCorrection := run(t)
	for name, got := range map[string]time.Time{
		"transfer":    firstTransfer,
		"disposition": firstDisposition,
		"correction":  firstCorrection,
	} {
		if !got.Equal(pinned) {
			t.Fatalf("%s stamped %v, want pinned %v", name, got, pinned)
		}
	}

	// Replay: the same fixed clock stamps the same instants again.
	secondTransfer, secondDisposition, secondCorrection := run(t)
	if !secondTransfer.Equal(firstTransfer) || !secondDisposition.Equal(firstDisposition) || !secondCorrection.Equal(firstCorrection) {
		t.Fatal("identical clock produced different stamps; the lifecycle still reads the wall clock")
	}

	// A moved clock moves the stamps: the clock is load bearing, not dead.
	moved := pinned.Add(90 * time.Minute)
	inv := openFixedInvestigation(t, moved)
	var err error
	if inv, err = inv.Begin("investigator:case-9"); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if inv, err = inv.Transfer("compartment:legal", "privilege", "investigator:case-9"); err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	if got := inv.Trail[len(inv.Trail)-1].At; !got.Equal(moved) {
		t.Fatalf("moved clock stamped %v, want %v", got, moved)
	}
}
