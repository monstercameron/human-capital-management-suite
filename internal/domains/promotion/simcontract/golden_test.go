package simcontract_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcontract"
)

// TestTodo_PROMO_004_Golden is PROMO-004's GOLDEN matrix test. It pins the
// exact digest of the Promotion reference fixture and, per the REFACTOR line,
// a second golden over the Manager Change fixture reusing the identical
// contract shape. PROMOUX-009 intentionally changed the finding portion of
// the canonical encoding from rendered legacy fields to the typed identity,
// owner, field, effect, explanation and corroboration source. The legacy
// Findings API remains, but this digest must move because the contract now
// commits to the typed canonical projection. A change to either digest is a
// deliberate encoding change, never an accident: update the constant only
// when the fixture or canonical encoding changed on purpose.
func TestTodo_PROMO_004_Golden(t *testing.T) {
	t.Run("promotion", func(t *testing.T) {
		// The reconciled encoding commits to both the deduplicated legacy
		// owner/corroboration and typed finding effect/source projections.
		// The digest below was observed from this fixture after that merge.
		const wantDigest = "sha256:318bc843e5fa287bbf247c384234bb6b43a270dcfd506c3ab459482262efd623"
		result, err := simcontract.Assemble(promotionFixtureInput(t))
		if err != nil {
			t.Fatalf("Assemble: %v", err)
		}
		if result.Digest != wantDigest {
			t.Fatalf("digest = %s, want %s", result.Digest, wantDigest)
		}
		if result.Status != simcontract.ResultExecutableAsSimulated {
			t.Fatalf("Status = %s, want %s", result.Status, simcontract.ResultExecutableAsSimulated)
		}
		if len(result.Writes) != 3 || len(result.SideEffects) != 3 || len(result.Approvals) != 1 {
			t.Fatalf("shape drifted: writes=%d side_effects=%d approvals=%d, want 3/3/1",
				len(result.Writes), len(result.SideEffects), len(result.Approvals))
		}
	})

	t.Run("manager_change", func(t *testing.T) {
		const wantDigest = "sha256:01bd20eed6aab4569f82ba7d7e8eca9fbf454c5f5f2b6a839b1b31b75c9891e8"
		result, err := simcontract.Assemble(managerChangeFixtureInput(t))
		if err != nil {
			t.Fatalf("Assemble: %v", err)
		}
		if result.Digest != wantDigest {
			t.Fatalf("digest = %s, want %s", result.Digest, wantDigest)
		}
		if result.Status != simcontract.ResultExecutableAsSimulated {
			t.Fatalf("Status = %s, want %s", result.Status, simcontract.ResultExecutableAsSimulated)
		}
		if len(result.Writes) != 1 || len(result.SideEffects) != 1 || len(result.Approvals) != 0 {
			t.Fatalf("shape drifted: writes=%d side_effects=%d approvals=%d, want 1/1/0",
				len(result.Writes), len(result.SideEffects), len(result.Approvals))
		}
	})
}
