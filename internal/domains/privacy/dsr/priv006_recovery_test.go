package dsr

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_PRIV_006_Recovery is the RECOVERY matrix test for PRIV-006.
// Resolution is a pure, replayable decision: re-resolving the same inputs
// reproduces the identical certificate (so a retry or a post-restore replay
// can never double-apply or diverge), a certificate round-trips through
// evidence storage byte-identically, and a copy whose deletion was already
// fulfilled but which reappears via restore re-resolves to re-delete --
// restore never resurrects erased data by default, while a hold placed
// after the restore still wins.
func TestTodo_PRIV_006_Recovery(t *testing.T) {
	t.Run("replay is idempotent: the same inputs reproduce the identical certificate", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-rec-replay", KindErasure, trust.AssuranceHigh)
		first, err := Resolve(fixtureResolutionSpec(t, req, allRedClasses()))
		if err != nil {
			t.Fatalf("Resolve (first): %v", err)
		}
		second, err := Resolve(fixtureResolutionSpec(t, req, allRedClasses()))
		if err != nil {
			t.Fatalf("Resolve (replay): %v", err)
		}
		if first.EvidenceID != second.EvidenceID {
			t.Errorf("replay EvidenceID = %q, want the first call's %q", second.EvidenceID, first.EvidenceID)
		}
	})

	t.Run("a certificate round-trips through evidence storage with its digest intact", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-rec-roundtrip", KindErasure, trust.AssuranceHigh)
		res, err := Resolve(fixtureResolutionSpec(t, req, allRedClasses()))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		raw, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var restored Resolution
		if err := json.Unmarshal(raw, &restored); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if restored.Digest() != res.Digest() {
			t.Errorf("restored Digest = %q, want %q", restored.Digest(), res.Digest())
		}
		if err := restored.Validate(); err != nil {
			t.Errorf("restored certificate does not validate: %v", err)
		}
	})

	t.Run("a restored copy with a prior fulfilled erasure re-resolves to re-delete", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-rec-restore", KindErasure, trust.AssuranceHigh)
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyBackup})
		spec.Copies[0].ErasureFulfilled = true
		res, err := Resolve(spec)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if res.Items[0].Outcome != OutcomeFulfill {
			t.Errorf("restored copy outcome = %s, want %s (re-delete, never by-default retain)", res.Items[0].Outcome, OutcomeFulfill)
		}
		if res.Items[0].Detail != "RE_DELETE_AFTER_RESTORE" {
			t.Errorf("restored copy detail = %q, want the re-delete marker", res.Items[0].Detail)
		}
	})

	t.Run("a hold placed after restore still grips the reappeared copy", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-rec-hold", KindErasure, trust.AssuranceHigh)
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyBackup})
		spec.Copies[0].ErasureFulfilled = true
		spec.Copies[0].Held = true
		spec.Copies[0].HoldAuthority = "post-restore-hold-9"
		res, err := Resolve(spec)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if res.Items[0].Outcome != OutcomeRetain {
			t.Errorf("held restored copy outcome = %s, want %s", res.Items[0].Outcome, OutcomeRetain)
		}
	})

	t.Run("an immutable copy claiming a prior fulfilled erasure is contradictory and blocks", func(t *testing.T) {
		req := fixtureVerifiedRequest(t, "dsr-rec-contra", KindErasure, trust.AssuranceHigh)
		spec := fixtureResolutionSpec(t, req, []CopyClass{CopyRetained})
		spec.Copies[0].Capability = DeletionNone
		spec.Copies[0].RetentionAuthority = "schedule-7y"
		spec.Copies[0].ErasureFulfilled = true
		if _, err := Resolve(spec); !errors.Is(err, ErrResolutionBlocked) {
			t.Fatalf("Resolve(contradictory re-delete) = %v, want %v", err, ErrResolutionBlocked)
		}
	})
}
