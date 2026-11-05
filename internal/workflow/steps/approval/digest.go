package approval

import (
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
)

const (
	continuationSchema = "hcmnext.workflow.steps.approval.Continuation"
	resolutionSchema   = "hcmnext.workflow.steps.approval.Resolution"
)

func writeReference(w *canonicalbytes.Writer, ref digest.Reference) *canonicalbytes.Writer {
	return w.
		String("proposal.profile_id", ref.ProfileID).
		Int("proposal.profile_version", int64(ref.ProfileVersion)).
		String("proposal.schema_id", ref.SchemaID).
		Int("proposal.schema_version", int64(ref.SchemaVersion)).
		String("proposal.algorithm_id", ref.AlgorithmID).
		Int("proposal.canonical_length", int64(ref.CanonicalLength)).
		String("proposal.digest", ref.Digest).
		String("proposal.scope_binding", ref.ScopeBindingDigest)
}

func computeContinuationDigest(c Continuation) string {
	w := canonicalbytes.New(continuationSchema, 1).
		String("workflow_instance_id", c.WorkflowInstanceID.String()).
		String("node_id", c.NodeID).
		String("proposal_revision_id", c.ProposalRevisionID)
	writeReference(w, c.ProposalDigest).Count("requirements", len(c.Requirements))
	// The default encodes only when declared, so a continuation without
	// one keeps its historical digest byte for byte.
	if c.DeadlineDefault.Schema != "" {
		w.
			String("deadline_default.schema", c.DeadlineDefault.Schema).
			String("deadline_default.payload", c.DeadlineDefault.CanonicalJSON)
	}
	for _, req := range c.Requirements {
		w.String("requirement.id", req.RequirementID).
			Int("requirement.revision", int64(req.RequirementRevision)).
			String("requirement.digest", req.RequirementDigest).
			Int("requirement.quorum", int64(req.Quorum)).
			Bool("requirement.distinct", req.Distinct).
			Value("requirement.decide_by", req.DecideBy).
			Value("requirement.expiry", req.Expiry).
			Int("requirement.work_item_count", int64(len(req.WorkItemIDs)))
		for _, id := range req.WorkItemIDs {
			w.String("requirement.work_item_id", id.String())
		}
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

func computeResolutionDigest(r Resolution) string {
	decisions := append([]string(nil), r.DecisionRefs...)
	items := append([]string(nil), r.WorkItemRefs...)
	sort.Strings(decisions)
	sort.Strings(items)
	w := canonicalbytes.New(resolutionSchema, 1).
		String("continuation_digest", r.ContinuationDigest).
		String("outcome", string(r.Outcome)).
		SortedStrings("decision_refs", decisions).
		SortedStrings("work_item_refs", items).
		Value("resolved_at", r.ResolvedAt).
		String("reason", r.Reason)
	if r.DefaultOutputDigest != "" {
		w.String("default_output_digest", r.DefaultOutputDigest)
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

func sameReference(a, b digest.Reference) bool {
	return a.ProfileID == b.ProfileID &&
		a.ProfileVersion == b.ProfileVersion &&
		a.SchemaID == b.SchemaID &&
		a.SchemaVersion == b.SchemaVersion &&
		a.AlgorithmID == b.AlgorithmID &&
		a.CanonicalLength == b.CanonicalLength &&
		a.Digest == b.Digest &&
		a.ScopeBindingDigest == b.ScopeBindingDigest &&
		sameOptional(a.CanonicalBytesArtifactRef, b.CanonicalBytesArtifactRef) &&
		sameOptional(a.IntentID, b.IntentID) &&
		sameOptional(a.ProposalRevisionID, b.ProposalRevisionID) &&
		sameOptional(a.MaterialProfileRef, b.MaterialProfileRef)
}

func sameOptional(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
