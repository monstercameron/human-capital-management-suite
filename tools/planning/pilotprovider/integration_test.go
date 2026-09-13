package pilotprovider

import (
	"testing"
)

// TestTodo_SELECT_002_Integration is SELECT-002's named INTEGRATION test.
// Following this repository's tools/planning convention for a package with
// no database (tools/planning/operations's TestTodo_OPS_007_Integration
// composes two real, independently-loaded registries and runs the real
// validation logic across both), this test loads the real checked-in
// topology from disk and drives it through every public entry point this
// package exposes end to end - parse, structural validation, signature
// verification, canonical digest, and the real-selection gate - proving
// they compose into one coherent pipeline over the actual file, not just
// individually against synthetic fixtures.
func TestTodo_SELECT_002_Integration(t *testing.T) {
	topology, err := LoadTopology(topologyPath)
	if err != nil {
		t.Fatalf("LoadTopology(%s): %v", topologyPath, err)
	}

	violations := topology.Validate()
	if len(violations) != 0 {
		t.Fatalf("the checked-in topology must validate as a structurally complete placeholder, got: %v", violations)
	}

	verified, err := VerifyTopologySignature(*topology)
	if err != nil {
		t.Fatalf("VerifyTopologySignature: %v", err)
	}
	if !verified {
		t.Fatal("the checked-in topology must verify against its own signature")
	}

	digest, err := topology.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	if digest == "" {
		t.Fatal("CanonicalDigest returned an empty digest")
	}

	// The pipeline's whole point: a topology can be internally valid AND
	// signed AND still correctly refused by any downstream gate that
	// requires a real provider selection, because it is a placeholder.
	satisfiesRealGate, gateViolations := topology.SatisfiesRealProviderSelectionGate()
	if satisfiesRealGate {
		t.Fatal("a valid, signed PLACEHOLDER_UNVERIFIED topology must still fail the real provider selection gate")
	}
	if len(gateViolations) == 0 {
		t.Fatal("SatisfiesRealProviderSelectionGate returned false but named no violations")
	}

	// Re-loading the same file twice must be deterministic: two independent
	// LoadTopology calls over the same bytes must agree on every derived
	// fact, which is what lets a CI gate and a developer's local run trust
	// the same digest.
	second, err := LoadTopology(topologyPath)
	if err != nil {
		t.Fatalf("second LoadTopology: %v", err)
	}
	secondDigest, err := second.CanonicalDigest()
	if err != nil {
		t.Fatalf("second CanonicalDigest: %v", err)
	}
	if secondDigest != digest {
		t.Fatalf("loading the same file twice produced different digests: %s vs %s", digest, secondDigest)
	}
}
