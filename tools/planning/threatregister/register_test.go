package threatregister

import (
	"strings"
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
// every slice graph edge is mapped to a reviewed threat; the register is
// honestly incomplete in exactly one documented place
// (residual_risks[0].accepted_by); and release is correctly blocked for the
// one unmitigated CRITICAL threat that gap leaves open.
func TestPhaseOneThreatModelCoversEveryTrustBoundaryAssetActorAndAbusePath(t *testing.T) {
	r := mustLoadRegister(t)

	violations := r.Validate()
	if len(violations) != 1 {
		t.Fatalf("expected exactly one documented violation (the empty residual-risk owner), got %d: %v", len(violations), violations)
	}
	if !strings.Contains(violations[0].String(), "residual_risks[0].accepted_by") {
		t.Errorf("expected the one violation to name residual_risks[0].accepted_by, got %v", violations[0])
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

	// --- GREEN: release is blocked for the one unmitigated CRITICAL threat
	// (THR-07), because its residual risk has no accountable owner ---
	blocked, blockers := r.ReleaseDecision(time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC))
	if !blocked {
		t.Fatal("expected release to be blocked by the unmitigated CRITICAL threat THR-07")
	}
	found := false
	for _, b := range blockers {
		if strings.Contains(b.Field, "THR-07") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a release blocker naming THR-07, got %v", blockers)
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
