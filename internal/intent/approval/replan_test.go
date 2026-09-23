package approval_test

import (
	"errors"
	"testing"

	replanengine "github.com/monstercameron/human-capital-management-suite/internal/engines/replan"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	driftgraph "github.com/monstercameron/human-capital-management-suite/internal/replan"
)

// replanBoundDriftRequest binds a material budget input under an approved
// proposal. The declaration component carries the drift-vocabulary name so
// the invalidation set feeds reuse without translation.
func replanBoundDriftRequest(prior []approval.ApprovalDecision, priorDigest string) approval.ReplanRequest {
	return approval.ReplanRequest{
		PriorProposalDigest: priorDigest,
		NewSnapshotDigest:   "digest:snapshot-2",
		OldSnapshot:         driftgraph.Snapshot{"amount": "100", "currency": "USD"},
		NewSnapshot:         driftgraph.Snapshot{"amount": "250", "currency": "USD"},
		Nodes: []driftgraph.Node{
			{ID: "amount", Kind: driftgraph.Fact},
			{ID: "currency", Kind: driftgraph.Fact},
		},
		Declaration: replanengine.Declaration{
			ProposalRevisionID: "rev-1",
			Components: []replanengine.Component{
				{ID: "amount", Dependencies: []replanengine.Dependency{{Input: "amount", Classification: replanengine.ClassificationMaterial}}},
				{ID: "currency", Dependencies: []replanengine.Dependency{{Input: "currency", Classification: replanengine.ClassificationMaterial}}},
			},
		},
		Policy:     approval.ReusePolicy{Version: "reuse-2026.1", RetainPresentationOnly: true},
		Prior:      prior,
		Components: map[string]string{"amount": "digest:amt-2", "currency": "digest:cur-2"},
	}
}

func TestTodo_REV_007_01(t *testing.T) {
	prior, priorDigest := reuseDecisions(t)
	receipt := prior[0].Digest()

	// No snapshot drift means no successor, even with a full declaration.
	quiet := replanBoundDriftRequest(prior, priorDigest)
	quiet.OldSnapshot = quiet.NewSnapshot
	still, err := approval.ReplanOnDrift(quiet)
	if err != nil {
		t.Fatalf("ReplanOnDrift quiet: %v", err)
	}
	if still.DriftDetected || still.HasSuccessor() {
		t.Fatalf("quiet=%+v", still)
	}
	if len(still.Analysis.Changed) != 0 {
		t.Fatalf("analysis=%+v", still.Analysis)
	}

	// A changed bound material input routes a successor to reapproval.
	req := replanBoundDriftRequest(prior, priorDigest)
	outcome, err := approval.ReplanOnDrift(req)
	if err != nil {
		t.Fatalf("ReplanOnDrift: %v", err)
	}
	if !outcome.DriftDetected || !outcome.HasSuccessor() {
		t.Fatalf("outcome=%+v", outcome)
	}
	if len(outcome.Invalidation.Invalidated) != 1 || outcome.Invalidation.Invalidated[0] != "amount" {
		t.Fatalf("invalidation=%+v", outcome.Invalidation)
	}
	if len(outcome.Retained) != len(prior) || outcome.Retained[0].Verdict != approval.ReuseReapprovalRequired {
		t.Fatalf("retained=%+v", outcome.Retained)
	}
	successor := outcome.Successor
	if successor.Route != approval.RouteReapproval {
		t.Fatalf("successor=%+v", successor)
	}
	if successor.SuccessorDigest == "" || successor.SuccessorDigest == priorDigest {
		t.Fatalf("successor=%+v", successor)
	}
	if successor.Supersedes != priorDigest || successor.SnapshotDigest != "digest:snapshot-2" {
		t.Fatalf("successor=%+v", successor)
	}
	if outcome.Retained[0].Decision.Digest() != receipt {
		t.Fatal("replan rebound the original immutable receipt")
	}

	// Undeclared drift is unbound evidence, never an inferred
	// invalidation: decisions are retained for review.
	unbound := replanBoundDriftRequest(prior, priorDigest)
	unbound.OldSnapshot = driftgraph.Snapshot{"amount": "100", "currency": "USD"}
	unbound.NewSnapshot = driftgraph.Snapshot{"amount": "100", "currency": "USD", "note": "late memo"}
	drifted, err := approval.ReplanOnDrift(unbound)
	if err != nil {
		t.Fatalf("ReplanOnDrift unbound: %v", err)
	}
	if !drifted.HasSuccessor() || drifted.Successor.Route != approval.RouteReview {
		t.Fatalf("drifted=%+v", drifted.Successor)
	}
	if len(drifted.Invalidation.UnboundDrift) != 1 || drifted.Invalidation.UnboundDrift[0] != "note" {
		t.Fatalf("invalidation=%+v", drifted.Invalidation)
	}

	// A component outside the drift vocabulary fails closed to
	// revalidation, never silent retention.
	weird := replanBoundDriftRequest(prior, priorDigest)
	weird.OldSnapshot = driftgraph.Snapshot{"mood": "calm"}
	weird.NewSnapshot = driftgraph.Snapshot{"mood": "tense"}
	weird.Nodes = []driftgraph.Node{{ID: "mood", Kind: driftgraph.Fact}}
	weird.Declaration = replanengine.Declaration{
		ProposalRevisionID: "rev-1",
		Components: []replanengine.Component{
			{ID: "vibes", Dependencies: []replanengine.Dependency{{Input: "mood", Classification: replanengine.ClassificationMaterial}}},
		},
	}
	blinded, err := approval.ReplanOnDrift(weird)
	if err != nil {
		t.Fatalf("ReplanOnDrift unknown: %v", err)
	}
	if !blinded.HasSuccessor() || blinded.Successor.Route != approval.RouteRevalidation {
		t.Fatalf("blinded=%+v", blinded.Successor)
	}

	// Hollow successors, hollow links and broken declarations refuse.
	hollow := replanBoundDriftRequest(prior, priorDigest)
	hollow.Components = nil
	if _, err := approval.ReplanOnDrift(hollow); err == nil {
		t.Fatal("component-free drift produced a successor")
	}
	stale := replanBoundDriftRequest(prior, priorDigest)
	stale.NewSnapshotDigest = stale.PriorProposalDigest
	if _, err := approval.ReplanOnDrift(stale); err == nil {
		t.Fatal("non-advancing drift produced a successor")
	}
	broken := replanBoundDriftRequest(prior, priorDigest)
	broken.Declaration.Components = append(broken.Declaration.Components, broken.Declaration.Components[0])
	if _, err := approval.ReplanOnDrift(broken); !errors.Is(err, replanengine.ErrInvalidDeclaration) {
		t.Fatalf("broken declaration err=%v", err)
	}
	nopolicy := replanBoundDriftRequest(prior, priorDigest)
	nopolicy.Policy = approval.ReusePolicy{}
	if _, err := approval.ReplanOnDrift(nopolicy); err == nil {
		t.Fatal("versionless reuse policy replanned")
	}
}
