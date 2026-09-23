package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Successor routes: the exact node replanning must visit before effects.
const (
	RouteReview       = "review"
	RouteReapproval   = "reapproval"
	RouteRevalidation = "revalidation"
)

// ReplanInput carries the immutable artifacts partial replanning binds:
// the superseded digest, the new snapshot, recomputed component digests
// and the retained decisions from EvaluateReuse.
type ReplanInput struct {
	PriorProposalDigest string
	NewSnapshotDigest   string
	Components          map[string]string
	Retained            []ReuseDecision
	Route               string
}

// SuccessorProposal is the routed successor. The approved proposal is
// never mutated: the successor references it, binds the recomputation and
// receives its own digest.
type SuccessorProposal struct {
	SuccessorDigest    string
	Supersedes         string
	SnapshotDigest     string
	Components         map[string]string
	RetainedReceipts   []string
	ApplicabilityLinks []string
	Route              string
	PolicyVersion      string
}

func successorDigest(input ReplanInput) string {
	names := make([]string, 0, len(input.Components))
	for name := range input.Components {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := []string{"approval-successor", input.PriorProposalDigest, input.NewSnapshotDigest, input.Route}
	for _, name := range names {
		parts = append(parts, name+"="+input.Components[name])
	}
	receipts := make([]string, 0, len(input.Retained))
	for _, retained := range input.Retained {
		receipts = append(receipts, retained.Decision.Digest()+">"+retained.Verdict)
	}
	sort.Strings(receipts)
	parts = append(parts, receipts...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// RequiredRoute reports the exact route the retained verdicts require:
// any unknown drift forces revalidation, any material drift forces
// reapproval, and clean retention reviews.
func RequiredRoute(retained []ReuseDecision) string {
	return requiredRoute(retained)
}

// requiredRoute derives the exact route from the retained verdicts: any
// unknown drift forces revalidation, any material drift forces
// reapproval, and clean retention reviews.
func requiredRoute(retained []ReuseDecision) string {
	route := RouteReview
	for _, decision := range retained {
		if decision.Verdict == ReuseUnknown {
			return RouteRevalidation
		}
		if decision.Verdict == ReuseReapprovalRequired {
			route = RouteReapproval
		}
	}
	return route
}

// CreateSuccessor binds the replanning artifacts into a routed successor.
// Skipped acknowledgements never pass: the route must equal the exact
// node the retained verdicts require, and execution against the old
// digest is impossible because the successor carries its own.
func CreateSuccessor(input ReplanInput) (SuccessorProposal, error) {
	if strings.TrimSpace(input.PriorProposalDigest) == "" || strings.TrimSpace(input.NewSnapshotDigest) == "" {
		return SuccessorProposal{}, fmt.Errorf("approval: successor binds the superseded and new snapshot digests")
	}
	if input.PriorProposalDigest == input.NewSnapshotDigest {
		return SuccessorProposal{}, fmt.Errorf("approval: successor must advance past the superseded digest")
	}
	if len(input.Components) == 0 {
		return SuccessorProposal{}, fmt.Errorf("approval: successor binds recomputed components")
	}
	for name, digest := range input.Components {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(digest) == "" {
			return SuccessorProposal{}, fmt.Errorf("approval: recomputed components need names and digests")
		}
	}
	if want := requiredRoute(input.Retained); input.Route != want {
		return SuccessorProposal{}, fmt.Errorf("approval: retained verdicts require the %s node, not %s", want, input.Route)
	}
	components := make(map[string]string, len(input.Components))
	for name, digest := range input.Components {
		components[name] = digest
	}
	successor := SuccessorProposal{
		Supersedes: input.PriorProposalDigest, SnapshotDigest: input.NewSnapshotDigest,
		Components: components, Route: input.Route,
	}
	for _, retained := range input.Retained {
		successor.RetainedReceipts = append(successor.RetainedReceipts, retained.Decision.Digest())
		successor.ApplicabilityLinks = append(successor.ApplicabilityLinks, retained.ApplicabilityLink)
		successor.PolicyVersion = retained.PolicyVersion
	}
	sort.Strings(successor.RetainedReceipts)
	sort.Strings(successor.ApplicabilityLinks)
	successor.SuccessorDigest = successorDigest(input)
	return successor, nil
}

// Verify recomputes the successor seal from its bound input.
func (successor SuccessorProposal) Verify(input ReplanInput) error {
	if successor.SuccessorDigest == "" || successorDigest(input) != successor.SuccessorDigest {
		return fmt.Errorf("approval: successor seal is broken")
	}
	if successor.Route != input.Route || successor.Route != requiredRoute(input.Retained) {
		return fmt.Errorf("approval: successor route does not match the retained verdicts")
	}
	if successor.Supersedes != input.PriorProposalDigest || successor.SnapshotDigest != input.NewSnapshotDigest {
		return fmt.Errorf("approval: successor bindings do not match its input")
	}
	return nil
}
