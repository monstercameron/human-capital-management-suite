package threatregister

import (
	"testing"
	"time"
)

// TestPhaseOneThreatModelCoversEveryTrustBoundaryAssetActorAndAbusePath is
// THREAT-001's PRIMARY test. It loads the real, checked-in
// definitions/planning/gates/threat-001-register.yaml and proves: every RED
// element is structurally present for the promotion slice (actor, asset,
// trust boundary, data class, entry point, threat, mitigation, detection,
// recovery, owner, test); the nine named attack classes are covered
// totally, driven off [AllAttackClasses] rather than a hardcoded list;
// every slice graph edge is mapped to a reviewed threat; and the verified
// THR-07 EDGE-07 control does not block release.
func TestPhaseOneThreatModelCoversEveryTrustBoundaryAssetActorAndAbusePath(t *testing.T) {
	r := mustLoadRegister(t)

	violations := r.Validate()
	if len(violations) != 0 {
		t.Fatalf("expected the signed register to validate cleanly, got %d: %v", len(violations), violations)
	}

	if len(r.Slices) != 1 {
		t.Fatalf("expected exactly one Phase 1 vertical slice today (promotion), got %d", len(r.Slices))
	}
	s := r.Slices[0]
	if s.SliceID != "promotion" {
		t.Errorf("slice_id = %q, want promotion", s.SliceID)
	}

	// --- RED: actor/asset/trust boundary/data class/entry point present ---
	if len(s.Actors) == 0 || len(s.Assets) == 0 || len(s.TrustBoundaries) == 0 || len(s.EntryPoints) == 0 {
		t.Fatalf("slice is missing a required RED element: actors=%d assets=%d trust_boundaries=%d entry_points=%d",
			len(s.Actors), len(s.Assets), len(s.TrustBoundaries), len(s.EntryPoints))
	}
	for _, a := range s.Assets {
		if !validDataClasses[a.DataClass] {
			t.Errorf("asset %s has invalid data_class %q", a.ID, a.DataClass)
		}
	}

	// --- GREEN: every slice graph edge is mapped to a reviewed threat ---
	consumed := map[string]bool{}
	for _, th := range s.Threats {
		for _, e := range th.ConsumingEdges {
			consumed[e] = true
		}
	}
	for _, e := range s.Edges {
		if !consumed[e.ID] {
			t.Errorf("edge %s is not mapped to any reviewed threat", e.ID)
		}
	}

	// --- RED: the nine named attack classes are covered totally, driven off
	// the taxonomy rather than a hardcoded list ---
	seenAttack := map[string]bool{}
	for _, th := range s.Threats {
		seenAttack[th.AttackClass] = true
		if th.Detection == "" || th.Recovery == "" || th.Owner == "" {
			t.Errorf("threat %s is missing detection, recovery or owner", th.ID)
		}
		if len(th.Tests) == 0 {
			t.Errorf("threat %s names no test", th.ID)
		}
	}
	for _, class := range AllAttackClasses() {
		if !seenAttack[string(class)] {
			t.Errorf("register ignores attack class %s", class)
		}
	}

	// --- REFACTOR: the shared mitigation retains every consuming edge it
	// is actually named by ---
	var shared *Mitigation
	for i := range s.Mitigations {
		if s.Mitigations[i].ID == "MIT-REVALIDATE-BEFORE-COMMIT" {
			shared = &s.Mitigations[i]
		}
	}
	if shared == nil {
		t.Fatal("expected the shared mitigation MIT-REVALIDATE-BEFORE-COMMIT to be present")
	}
	if len(shared.ConsumingEdges) != 2 {
		t.Errorf("shared mitigation should retain exactly 2 consuming edges (EDGE-01 from THR-01, EDGE-06 from THR-06), got %v", shared.ConsumingEdges)
	}

	// --- GREEN: the exact-path EDGE-07 mitigation resolves THR-07 ---
	blocked, blockers := r.ReleaseDecision(time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC))
	if blocked {
		t.Fatalf("verified EDGE-07 mitigation must not block release, got %v", blockers)
	}

	// Signature verifies.
	ok, err := VerifyRegisterSignature(r)
	if err != nil {
		t.Fatalf("VerifyRegisterSignature: %v", err)
	}
	if !ok {
		t.Fatal("the checked-in register must verify against its own signature")
	}
}
