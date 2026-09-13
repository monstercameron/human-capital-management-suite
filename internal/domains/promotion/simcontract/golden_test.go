package simcontract_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcontract"
)

// TestTodo_PROMO_004_Golden is PROMO-004's GOLDEN matrix test. It pins the
// exact digest of the Promotion reference fixture and, per the REFACTOR line,
// a second golden over the Manager Change fixture reusing the identical
// contract shape. A change to either digest is a deliberate encoding change,
// never an accident: update the constant only when the fixture or the
// canonical encoding changed on purpose.
func TestTodo_PROMO_004_Golden(t *testing.T) {
	t.Run("promotion", func(t *testing.T) {
		// Re-pinned for PROMOUX-009: canonicalBody now digests each
		// finding's Owner and CorroboratedBy alongside Code/Severity/
		// Field/Message, so adding those two fields to the encoding moved
		// this digest even though the fixture's one finding is not a
		// duplicate and dedup is a no-op on it. Verified by dumping and
		// reading the actual computed value (`go test -run
		// TestTodo_PROMO_004_Golden/promotion -v`) before pinning it here,
		// per this repository's golden-repin discipline.
		const wantDigest = "sha256:a5c04d903e0c5e2524e5b3b9a64eeff056a3c7d641a6421225dd0391738da64e"
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
		const wantDigest = "sha256:71bca62030d236db6801abb7ee81057439e0e5b779889d58aeef1e4be7156211"
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
