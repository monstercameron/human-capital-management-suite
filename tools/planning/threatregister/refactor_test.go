package threatregister

import (
	"strings"
	"testing"
)

// TestTodo_THREAT_001_Refactor_DuplicateThreatIdentityIsDetected proves
// REFACTOR's dedup rule with a fixture that genuinely contains a duplicate:
// two Threat entries with different free-text IDs and different scenario
// prose, but the same (asset, trust_boundary, attack_class) triple - the
// exact identity [ThreatIdentity] computes. The real checked-in register
// has no duplicates (that is the point of REFACTOR passing), so this
// fixture is deliberately constructed rather than lifted from live data -
// a dedup test whose fixture has no duplicate would pass trivially and
// prove nothing.
func TestTodo_THREAT_001_Refactor_DuplicateThreatIdentityIsDetected(t *testing.T) {
	r := deepCopy(validFixture())
	s := &r.Slices[0]

	// Clone the first threat under a different ID and different scenario
	// prose, but keep the same asset, trust boundary and attack class - the
	// same identity under REFACTOR's rule, recorded twice.
	dup := s.Threats[0]
	dup.ID = "THR-DUPLICATE-DIFFERENT-ID"
	dup.Scenario = "a completely different-sounding scenario describing the same underlying threat"
	dup.ConsumingEdges = append([]string(nil), s.Threats[0].ConsumingEdges...)
	dup.Mitigations = append([]string(nil), s.Threats[0].Mitigations...)
	dup.Tests = append([]ThreatTest(nil), s.Threats[0].Tests...)
	s.Threats = append(s.Threats, dup)

	if s.Threats[0].Asset != dup.Asset || s.Threats[0].TrustBoundary != dup.TrustBoundary || s.Threats[0].AttackClass != dup.AttackClass {
		t.Fatal("test setup bug: the cloned threat must share asset, trust_boundary and attack_class with the original")
	}
	if s.Threats[0].ID == dup.ID || s.Threats[0].Scenario == dup.Scenario {
		t.Fatal("test setup bug: the cloned threat must differ by ID and scenario, or this fixture would not be a genuine duplicate-identity case")
	}

	violations := r.Validate()
	found := false
	for _, v := range violations {
		if strings.Contains(v.String(), "duplicate threat identity") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a 'duplicate threat identity' violation for two threats sharing (asset, trust_boundary, attack_class), got %v", violations)
	}
}

// TestTodo_THREAT_001_Refactor_SharedMitigationRetainsEveryConsumingEdge
// proves REFACTOR's other rule with a fixture where one mitigation is
// genuinely shared across at least two distinct slice graph edges: it
// starts from a mitigation correctly retaining both edges (clean), then
// breaks it by dropping one edge from the mitigation's declared
// consuming_edges while the second threat still names the mitigation - the
// narrower regression a real edit could introduce - and proves Validate
// catches exactly that.
func TestTodo_THREAT_001_Refactor_SharedMitigationRetainsEveryConsumingEdge(t *testing.T) {
	r := deepCopy(validFixture())
	s := &r.Slices[0]

	// validFixture already shares MIT-SHARED across every threat/edge; take
	// the two-edge slice of that relationship the todo asks for.
	shared := &s.Mitigations[0]
	if shared.ID != "MIT-SHARED" || len(shared.ConsumingEdges) < 2 {
		t.Fatal("test setup bug: expected validFixture's MIT-SHARED to already retain 2+ consuming edges")
	}
	edgeA, edgeB := shared.ConsumingEdges[0], shared.ConsumingEdges[1]

	// Restrict to exactly two threats naming MIT-SHARED, consuming edgeA and
	// edgeB respectively, so the shared-mitigation relationship under test
	// is unambiguous.
	var kept []Threat
	for _, th := range s.Threats {
		for _, e := range th.ConsumingEdges {
			if e == edgeA || e == edgeB {
				kept = append(kept, th)
				break
			}
		}
	}
	if len(kept) < 2 {
		t.Fatal("test setup bug: expected at least two threats consuming edgeA/edgeB")
	}
	s.Threats = kept
	shared.ConsumingEdges = []string{edgeA, edgeB}

	// Trimming to two threats deliberately breaks this fixture's unrelated
	// attack-class/edge-coverage totality (that is not what this test is
	// about), so only the mitigation-retention violations matter here: none
	// should fire while the declared edges correctly match what the kept
	// threats actually consume.
	for _, v := range r.Validate() {
		if strings.Contains(v.String(), "consuming edge") {
			t.Errorf("shared mitigation correctly retaining both consuming edges must not report a mitigation-edge violation, got: %v", v)
		}
	}

	broken := deepCopy(r)
	broken.Slices[0].Mitigations[0].ConsumingEdges = []string{edgeA} // silently drops edgeB

	violations := broken.Validate()
	found := false
	for _, v := range violations {
		if strings.Contains(v.String(), "does not retain consuming edge") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a 'does not retain consuming edge' violation once the shared mitigation's declared edges were narrowed, got %v", violations)
	}
}
