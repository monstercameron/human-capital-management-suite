package task

import (
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	continuationSchema = "hcmnext.workflow.steps.task.Continuation"
	submissionSchema   = "hcmnext.workflow.steps.task.Submission"
	resolutionSchema   = "hcmnext.workflow.steps.task.Resolution"
)

func writeNode(w *canonicalbytes.Writer, n CompiledTaskNode) *canonicalbytes.Writer {
	w.
		String("workflow_id", n.WorkflowID).
		Int("workflow_version", int64(n.WorkflowVersion)).
		String("node_id", n.NodeID).
		String("work_type", n.WorkType).
		String("output_schema.id", n.OutputSchema.SchemaID).
		Int("output_schema.version", int64(n.OutputSchema.Version)).
		String("output_schema.protobuf", n.OutputSchema.ProtobufFullName).
		String("form.ref", n.FormDefinition.Ref).
		Int("form.version", int64(n.FormDefinition.Version)).
		String("accessibility_policy.ref", n.AccessibilityPolicy.Ref).
		Int("accessibility_policy.version", int64(n.AccessibilityPolicy.Version)).
		String("accommodation_policy.ref", n.AccommodationPolicy.Ref).
		Int("accommodation_policy.version", int64(n.AccommodationPolicy.Version))
	// Deadline fields encode only when the node declares them, so a node
	// without a deadline keeps its historical digest byte for byte.
	if n.Deadline.AnchorPath != "" {
		w.
			String("deadline.anchor_path", n.Deadline.AnchorPath).
			String("deadline.offset_kind", string(n.Deadline.OffsetKind)).
			Int("deadline.offset_days", int64(n.Deadline.OffsetDays)).
			Int("deadline.offset_seconds", n.Deadline.OffsetSeconds).
			String("deadline.calendar", n.Deadline.Calendar.String()).
			String("deadline.zone", n.Deadline.Zone.String()).
			String("deadline.local_time", n.Deadline.WakeLocalTime.String()).
			String("deadline.disambiguation", n.Deadline.Disambiguation.String()).
			String("deadline.statute", statuteString(n.Deadline.Statute))
	}
	if n.DeadlineDefault.Schema != "" {
		w.
			String("deadline_default.schema", n.DeadlineDefault.Schema).
			String("deadline_default.payload", n.DeadlineDefault.CanonicalJSON)
	}
	return w
}

func statuteString(s *values.StatuteRef) string {
	if s == nil {
		return ""
	}
	return s.String()
}

func computeContinuationDigest(c Continuation) string {
	w := canonicalbytes.New(continuationSchema, 1).
		String("workflow_instance_id", c.WorkflowInstanceID.String()).
		String("work_item_id", c.WorkItemID.String())
	writeNode(w, c.Node)
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

func computeSubmissionDigest(s Submission) string {
	w := canonicalbytes.New(submissionSchema, 1).
		String("workflow_instance_id", s.WorkflowInstanceID.String()).
		String("node_id", s.NodeID).
		String("work_item_id", s.WorkItemID.String()).
		Int("item_version", s.ItemVersion).
		String("completed_by", s.CompletedBy).
		String("candidate_via", string(s.CandidateVia)).
		String("delegation_id", s.DelegationID).
		String("claim_id", s.ClaimID.String()).
		Value("claim_expires_at", s.ClaimExpiresAt).
		Value("submitted_at", s.SubmittedAt).
		String("output_schema.id", s.OutputSchema.SchemaID).
		Int("output_schema.version", int64(s.OutputSchema.Version)).
		String("output_schema.protobuf", s.OutputSchema.ProtobufFullName).
		String("payload_digest", s.CanonicalPayloadDigest).
		String("form.ref", s.FormDefinition.Ref).
		Int("form.version", int64(s.FormDefinition.Version)).
		String("render_context_digest", s.RenderContextDigest).
		String("validation_evidence_ref", s.ValidationEvidenceRef).
		String("accessibility_evidence_ref", s.AccessibilityEvidenceRef).
		String("accommodation_evidence_ref", s.AccommodationEvidenceRef)
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

func computeResolutionDigest(r Resolution) string {
	w := canonicalbytes.New(resolutionSchema, 1).
		String("continuation_digest", r.ContinuationDigest).
		String("outcome", string(r.Outcome)).
		String("work_item_ref", r.WorkItemRef).
		String("submission_digest", r.SubmissionDigest).
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
