package threatregister

import (
	"strings"
	"testing"
	"time"
)

// TestTodo_THREAT_001_Fault proves [Register.ReleaseDecision]'s critical
// threat handling: the exact-path mitigation closes THR-07, while removing it
// restores the release block and a synthetic acceptance only waives it when
// accountable and unexpired.
func TestTodo_THREAT_001_Fault(t *testing.T) {
	r := mustLoadRegister(t)

	t.Run("the real exact-path mitigation resolves THR-07", func(t *testing.T) {
		blocked, blockers := r.ReleaseDecision(time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC))
		if blocked {
			t.Fatalf("verified EDGE-07 mitigation should not block release: %v", blockers)
		}
	})

	t.Run("removing the EDGE-07 mitigation restores the block", func(t *testing.T) {
		fixture := deepCopy(r)
		for i := range fixture.Slices[0].Threats {
			if fixture.Slices[0].Threats[i].ID == "THR-07" {
				fixture.Slices[0].Threats[i].Mitigations = nil
			}
		}
		blocked, blockers := fixture.ReleaseDecision(time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC))
		if !blocked || !strings.Contains(strings.Join(blockerFields(blockers), " "), "THR-07") {
			t.Fatalf("THR-07 without its EDGE-07 mitigation must block: %v", blockers)
		}
	})

	t.Run("a mitigated CRITICAL threat never blocks release on its own", func(t *testing.T) {
		// THR-02 (CONFUSED_DEPUTY) and THR-03 (TENANT_CROSSOVER) are both
		// CRITICAL and carry mitigations; deleting THR-07 proves they do not
		// themselves trigger a block.
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

func blockerFields(blockers []Violation) []string {
	fields := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		fields = append(fields, blocker.Field)
	}
	return fields
}

// TestTodo_THREAT_001_Fault_ExpiredResidualRiskAcceptanceDoesNotWaiveRelease
// proves the branch the real checked-in register cannot exercise: a
// residual-risk acceptance that names an accountable owner (accepted_by
// populated) but whose expiry_date has already passed must not waive a
// release block for the CRITICAL threat it claims to cover - only an
// accepted AND unexpired acceptance does. This is exercised against a
// fixture built from the real register with THR-07's mitigation removed
// and a synthetic, fully-populated-but-expired acceptance added.
func TestTodo_THREAT_001_Fault_ExpiredResidualRiskAcceptanceDoesNotWaiveRelease(t *testing.T) {
	base := mustLoadRegister(t)
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)

	expired := deepCopy(base)
	for i := range expired.Slices[0].Threats {
		if expired.Slices[0].Threats[i].ID == "THR-07" {
			expired.Slices[0].Threats[i].Mitigations = nil
		}
	}
	expired.Slices[0].ResidualRisks = []ResidualRisk{{ThreatID: "THR-07", Justification: "fixture only", AcceptedBy: "Jane Security Officer", AcceptedDate: "2026-01-01", ExpiryDate: "2026-06-01"}}

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
	for i := range unexpired.Slices[0].Threats {
		if unexpired.Slices[0].Threats[i].ID == "THR-07" {
			unexpired.Slices[0].Threats[i].Mitigations = nil
		}
	}
	unexpired.Slices[0].ResidualRisks = []ResidualRisk{{ThreatID: "THR-07", Justification: "fixture only", AcceptedBy: "Jane Security Officer", AcceptedDate: "2026-01-01", ExpiryDate: "2026-12-31"}}

	blocked, blockers = unexpired.ReleaseDecision(now)
	if blocked {
		t.Errorf("an accountable, unexpired residual-risk acceptance should waive the block, got blockers: %v", blockers)
	}
}
