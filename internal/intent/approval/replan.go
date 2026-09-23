package approval

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/fielddiff"
	replanengine "github.com/monstercameron/human-capital-management-suite/internal/engines/replan"
	driftgraph "github.com/monstercameron/human-capital-management-suite/internal/replan"
)

// ReplanRequest binds the immutable drift evidence the proposal
// revalidation path needs to turn detected input drift into a routed
// successor proposal. Snapshots, lineage nodes and the dependency
// declaration are loaded from their authoritative stores by the caller;
// Components carries the recomputed component digests the successor seals.
type ReplanRequest struct {
	PriorProposalDigest string
	NewSnapshotDigest   string
	OldSnapshot         driftgraph.Snapshot
	NewSnapshot         driftgraph.Snapshot
	Nodes               []driftgraph.Node
	Declaration         replanengine.Declaration
	Policy              ReusePolicy
	Prior               []ApprovalDecision
	Components          map[string]string
}

// ReplanOutcome is the deterministic evidence for one drift check. When
// no snapshot input changed there is nothing to replan: DriftDetected is
// false and no successor is produced.
type ReplanOutcome struct {
	DriftDetected bool
	Analysis      driftgraph.Result
	Invalidation  replanengine.Result
	Retained      []ReuseDecision
	Successor     SuccessorProposal
}

// HasSuccessor reports whether the drift check produced a routed
// successor proposal.
func (o ReplanOutcome) HasSuccessor() bool {
	return o.DriftDetected && o.Successor.SuccessorDigest != ""
}

// ReplanOnDrift wires the replan packages into the proposal revalidation
// path. It runs the material-subgraph analysis over the snapshot drift,
// feeds the changed inputs to the replan engine's Compute, evaluates reuse
// of the prior decisions against the invalidated components, and routes a
// CreateSuccessor proposal to the node the retained verdicts require.
// Undeclared drift never invalidates by inference: a changed input with no
// declared component is kept as unbound evidence and the prior decisions
// are retained for review. Unknown components fail closed to revalidation;
// material drift forces reapproval.
func ReplanOnDrift(req ReplanRequest) (ReplanOutcome, error) {
	if strings.TrimSpace(req.PriorProposalDigest) == "" || strings.TrimSpace(req.NewSnapshotDigest) == "" {
		return ReplanOutcome{}, fmt.Errorf("approval: replan binds the superseded and new snapshot digests")
	}
	if req.PriorProposalDigest == req.NewSnapshotDigest {
		return ReplanOutcome{}, fmt.Errorf("approval: replan must advance past the superseded digest")
	}
	analysis := driftgraph.AnalyzeMaterialSubgraph(req.OldSnapshot, req.NewSnapshot, req.Nodes)
	outcome := ReplanOutcome{Analysis: analysis}
	if len(analysis.Changed) == 0 {
		return outcome, nil
	}
	outcome.DriftDetected = true
	// The analysis already proved these inputs differ; at this layer no
	// update ordering is claimed, so the drift is an unordered conflict.
	diffs := make([]replanengine.FieldDiff, 0, len(analysis.Changed))
	for _, input := range analysis.Changed {
		diffs = append(diffs, replanengine.FieldDiff{
			Input:   input,
			Outcome: fielddiff.Outcome{Relation: fielddiff.RelationConflict, Reason: fielddiff.ReasonUnordered},
		})
	}
	invalidation, err := replanengine.Compute(req.Declaration, diffs)
	if err != nil {
		return ReplanOutcome{}, fmt.Errorf("approval: replan compute: %w", err)
	}
	outcome.Invalidation = invalidation
	retained, err := EvaluateReuse(req.Policy, req.Prior, SuccessorLink{
		PriorProposalDigest: req.PriorProposalDigest,
		NextProposalDigest:  req.NewSnapshotDigest,
		Changed:             invalidation.Invalidated,
	})
	if err != nil {
		return ReplanOutcome{}, fmt.Errorf("approval: replan reuse: %w", err)
	}
	outcome.Retained = retained
	components := make(map[string]string, len(req.Components))
	for name, digest := range req.Components {
		components[name] = digest
	}
	input := ReplanInput{
		PriorProposalDigest: req.PriorProposalDigest,
		NewSnapshotDigest:   req.NewSnapshotDigest,
		Components:          components,
		Retained:            retained,
		Route:               RequiredRoute(retained),
	}
	successor, err := CreateSuccessor(input)
	if err != nil {
		return ReplanOutcome{}, fmt.Errorf("approval: replan successor: %w", err)
	}
	if err := successor.Verify(input); err != nil {
		return ReplanOutcome{}, fmt.Errorf("approval: replan successor seal: %w", err)
	}
	outcome.Successor = successor
	return outcome, nil
}
