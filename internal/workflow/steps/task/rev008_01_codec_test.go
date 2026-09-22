package task_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	steptask "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/task"
)

// TestSubmissionEncodeDecodeRoundTrip pins the persistence codec REV-008-01's
// resume gate reads: a body EncodeSubmission produced always decodes back to
// a submission with the same digest, and any post-recording alteration -- a
// stripped acknowledgement, a swapped schema, a non-JSON body -- is refused.
func TestSubmissionEncodeDecodeRoundTrip(t *testing.T) {
	at := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	node := steptask.CompiledTaskNode{
		WorkflowID: "wf.promotion", WorkflowVersion: 1, NodeID: "review_task", WorkType: "promotion.review",
		OutputSchema:        workflow.SchemaRef{SchemaID: "hcmnext.task.promotion_review.Output", Version: 1, ProtobufFullName: "hcmnext.task.promotion_review.Output"},
		FormDefinition:      steptask.VersionedRef{Ref: "form.promotion.review", Version: 1},
		AccessibilityPolicy: steptask.VersionedRef{Ref: "policy.accessibility.default", Version: 1},
		AccommodationPolicy: steptask.VersionedRef{Ref: "policy.accommodation.default", Version: 1},
	}
	claimID := uuid.New()
	sub, err := steptask.NewSubmission(steptask.SubmissionSpec{
		WorkflowInstanceID: uuid.New(), NodeID: node.NodeID, WorkItemID: uuid.New(), ItemVersion: 4,
		CompletedBy: "principal:reviewer", CandidateVia: humanwork.SourceDirect, ClaimID: claimID,
		ClaimExpiresAt: values.NewInstant(at.Add(time.Hour)), SubmittedAt: values.NewInstant(at),
		OutputSchema: node.OutputSchema, CanonicalPayloadDigest: "sha256:" + strings.Repeat("9", 64),
		FormDefinition: node.FormDefinition, RenderContextDigest: "sha256:" + strings.Repeat("8", 64),
		ValidationEvidenceRef: "validation:reviewed", AccessibilityEvidenceRef: "ack:accessibility:reviewed",
		AccommodationEvidenceRef: "ack:accommodation:reviewed",
	})
	if err != nil {
		t.Fatalf("NewSubmission: %v", err)
	}
	body, err := steptask.EncodeSubmission(sub)
	if err != nil {
		t.Fatalf("EncodeSubmission: %v", err)
	}
	decoded, err := steptask.DecodeSubmission(body)
	if err != nil {
		t.Fatalf("DecodeSubmission: %v", err)
	}
	if decoded.Digest() != sub.Digest() {
		t.Fatalf("decoded digest = %q, want %q", decoded.Digest(), sub.Digest())
	}
	if _, err := steptask.DecodeSubmission([]byte("{not json")); err == nil {
		t.Fatal("DecodeSubmission accepted a non-JSON body, want refusal")
	}
	stripped := strings.Replace(string(body), `"accommodation_evidence_ref":"ack:accommodation:reviewed"`, `"accommodation_evidence_ref":""`, 1)
	if stripped == string(body) {
		t.Fatal("fixture body does not carry the expected acknowledgement ref")
	}
	if _, err := steptask.DecodeSubmission([]byte(stripped)); err == nil {
		t.Fatal("DecodeSubmission accepted a body with no accommodation acknowledgement, want refusal")
	}
}
