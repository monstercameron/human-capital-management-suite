package threatregister

import (
	"strings"
	"testing"
	"time"
)

// TestTodo_THREAT_001_Fault is THREAT-001's named FAULT test. It proves
// [Register.ReleaseDecision]'s two required behaviors: an unmitigated
// CRITICAL threat blocks release, and an expired residual-risk acceptance
// does not waive that block even when every other field (justification,
// accepted_by, accepted_date) looks complete. Both are proved twice: once
// against the real checked-in register (which can only exercise the "no
// acceptance at all" branch, since its one residual risk was never
// accepted), and once against a fixture built specifically to exercise the
// "accepted but expired" branch the real file cannot reach - see the second
// test's comment for why that fixture is necessary rather than decorative.
func TestTodo_THREAT_001_Fault(t *testing.T) {
	r := mustLoadRegister(t)

	t.Run("the real register blocks release for its one unmitigated CRITICAL threat", func(t *testing.T) {
		blocked, blockers := r.ReleaseDecision(time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC))
		if !blocked {
			t.Fatal("expected ReleaseDecision to block release")
		}
		if len(blockers) == 0 {
			t.Fatal("ReleaseDecision reported blocked=true but named no blockers")
		}
		found := false
		for _, b := range blockers {
			if strings.Contains(b.Field, "THR-07") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected a blocker naming THR-07, got %v", blockers)
		}
	})

	t.Run("a mitigated CRITICAL threat never blocks release on its own", func(t *testing.T) {
		// THR-02 (CONFUSED_DEPUTY) and THR-03 (TENANT_CROSSOVER) are both
		// CRITICAL and both carry a mitigation; removing THR-07 (the one
		// unmitigated CRITICAL threat) proves the other two, mitigated,
		// CRITICAL threats do not themselves trigger a block.
		fixture := deepCopy(r)
		s := &fixture.Slices[0]
		var kept []Threat
		for _, th := range s.Threats {
			if th.ID != "THR-07" {
				kept = append(kept, th)
			}
		}
		s.Threats = kept
		s.ResidualRisks = nil

		blocked, blockers := fixture.ReleaseDecision(time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC))
		if blocked {
			t.Errorf("expected no block once the one unmitigated CRITICAL threat is removed, got blockers: %v", blockers)
		}
	})
}

// TestTodo_THREAT_001_Fault_ExpiredResidualRiskAcceptanceDoesNotWaiveRelease
// proves the branch the real checked-in register cannot exercise: a
// residual-risk acceptance that names an accountable owner (accepted_by
// populated) but whose expiry_date has already passed must not waive a
// release block for the CRITICAL threat it claims to cover - only an
// accepted AND unexpired acceptance does. This is exercised against a
// fixture built from the real register with a synthetic, fully-populated-
// but-expired acceptance grafted onto THR-07, because the real file's own
// acceptance is honestly empty (see doc.go) and can never move through the
// "accepted" state within this test alone.
func TestTodo_THREAT_001_Fault_ExpiredResidualRiskAcceptanceDoesNotWaiveRelease(t *testing.T) {
	base := mustLoadRegister(t)
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)

	expired := deepCopy(base)
	expired.Slices[0].ResidualRisks[0].AcceptedBy = "Jane Security Officer"
	expired.Slices[0].ResidualRisks[0].AcceptedDate = "2026-01-01"
	expired.Slices[0].ResidualRisks[0].ExpiryDate = "2026-06-01" // before `now`

	blocked, blockers := expired.ReleaseDecision(now)
	if !blocked {
		t.Fatal("an expired residual-risk acceptance must not waive the release block")
	}
	found := false
	for _, b := range blockers {
		if strings.Contains(b.Field, "THR-07") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the blocker to still name THR-07 despite the (expired) acceptance, got %v", blockers)
	}

	// The companion positive case: the same acceptance, with an expiry that
	// has not yet passed, does waive the block - proving the expiry check
	// itself (not something else) is what distinguishes the two cases.
	unexpired := deepCopy(base)
	unexpired.Slices[0].ResidualRisks[0].AcceptedBy = "Jane Security Officer"
	unexpired.Slices[0].ResidualRisks[0].AcceptedDate = "2026-01-01"
	unexpired.Slices[0].ResidualRisks[0].ExpiryDate = "2026-12-31" // after `now`

	blocked, blockers = unexpired.ReleaseDecision(now)
	if blocked {
		t.Errorf("an accountable, unexpired residual-risk acceptance should waive the block, got blockers: %v", blockers)
	}
}
