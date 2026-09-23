package approval_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
)

// TestTodo_REV_007_01_Integration changes a bound input under an approved
// proposal and observes a successor proposal appear. It reaches the replan
// chain only through the approval seam: no replan package is called here
// directly.
func TestTodo_REV_007_01_Integration(t *testing.T) {
	prior, priorDigest := reuseDecisions(t)
	receipt := prior[0].Digest()

	outcome, err := approval.ReplanOnDrift(replanBoundDriftRequest(prior, priorDigest))
	if err != nil {
		t.Fatalf("ReplanOnDrift: %v", err)
	}
	if !outcome.DriftDetected {
		t.Fatal("bound input change detected no drift")
	}
	if !outcome.HasSuccessor() {
		t.Fatal("bound input change produced no successor proposal")
	}
	successor := outcome.Successor
	if successor.Route != approval.RouteReapproval {
		t.Fatalf("material drift routed to %q, not reapproval", successor.Route)
	}
	if successor.Supersedes != priorDigest {
		t.Fatalf("successor does not supersede the approved proposal: %+v", successor)
	}
	if len(outcome.Retained) != len(prior) {
		t.Fatalf("retained=%d prior=%d", len(outcome.Retained), len(prior))
	}
	if outcome.Retained[0].Decision.Digest() != receipt {
		t.Fatal("successor lost the original immutable receipt")
	}

	// The quiet control: identical snapshots under the same approved
	// proposal produce no successor.
	quiet := replanBoundDriftRequest(prior, priorDigest)
	quiet.OldSnapshot = quiet.NewSnapshot
	unchanged, err := approval.ReplanOnDrift(quiet)
	if err != nil {
		t.Fatalf("ReplanOnDrift quiet: %v", err)
	}
	if unchanged.DriftDetected || unchanged.HasSuccessor() {
		t.Fatalf("unchanged inputs replanned: %+v", unchanged)
	}
}
