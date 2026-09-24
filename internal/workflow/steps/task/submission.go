package task

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/candidate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
)

// NewSubmission validates and mints an immutable typed submission artifact.
func NewSubmission(in SubmissionSpec) (Submission, error) {
	s := Submission{
		WorkflowInstanceID:       in.WorkflowInstanceID,
		NodeID:                   in.NodeID,
		WorkItemID:               in.WorkItemID,
		ItemVersion:              in.ItemVersion,
		CompletedBy:              in.CompletedBy,
		CandidateVia:             in.CandidateVia,
		DelegationID:             in.DelegationID,
		ClaimID:                  in.ClaimID,
		ClaimExpiresAt:           in.ClaimExpiresAt,
		SubmittedAt:              in.SubmittedAt,
		OutputSchema:             in.OutputSchema,
		CanonicalPayloadDigest:   in.CanonicalPayloadDigest,
		FormDefinition:           in.FormDefinition,
		RenderContextDigest:      in.RenderContextDigest,
		ValidationEvidenceRef:    in.ValidationEvidenceRef,
		AccessibilityEvidenceRef: in.AccessibilityEvidenceRef,
		AccommodationEvidenceRef: in.AccommodationEvidenceRef,
	}
	if err := validateSubmission(s); err != nil {
		return Submission{}, err
	}
	s.digest = computeSubmissionDigest(s)
	if s.digest == "" {
		return Submission{}, ErrInvalidSubmission
	}
	return s, nil
}

// Verify reports whether a Submission still matches its minted digest.
func (s Submission) Verify() error {
	if s.digest == "" || computeSubmissionDigest(s) != s.digest {
		return ErrInvalidSubmission
	}
	return validateSubmission(s)
}

func validateSubmission(s Submission) error {
	switch {
	case s.WorkflowInstanceID == uuid.Nil || s.WorkItemID == uuid.Nil || s.NodeID == "":
		return fmt.Errorf("%w: workflow, node, and work-item identity are required", ErrInvalidSubmission)
	case s.ItemVersion < 1:
		return fmt.Errorf("%w: item version must be positive", ErrInvalidSubmission)
	case s.CompletedBy == "" || s.ClaimID == uuid.Nil:
		return fmt.Errorf("%w: completer and claim identity are required", ErrInvalidSubmission)
	case !validCandidateSource(s.CandidateVia):
		return fmt.Errorf("%w: candidate route %q is not declared", ErrInvalidSubmission, s.CandidateVia)
	case !s.ClaimExpiresAt.IsSet() || !s.SubmittedAt.IsSet():
		return fmt.Errorf("%w: claim expiry and submission time are required", ErrInvalidSubmission)
	case s.SubmittedAt.After(s.ClaimExpiresAt):
		return ErrClaimExpired
	case !s.OutputSchema.Valid():
		return fmt.Errorf("%w: output schema is incomplete", ErrInvalidSubmission)
	case !workitem.ValidDigest(s.CanonicalPayloadDigest):
		return fmt.Errorf("%w: canonical payload digest is malformed", ErrInvalidSubmission)
	case s.FormDefinition.Ref == "" || s.FormDefinition.Version == 0:
		return fmt.Errorf("%w: exact form definition is required", ErrInvalidSubmission)
	case !workitem.ValidDigest(s.RenderContextDigest):
		return fmt.Errorf("%w: render-context digest is malformed", ErrInvalidSubmission)
	case s.ValidationEvidenceRef == "" || s.AccessibilityEvidenceRef == "" || s.AccommodationEvidenceRef == "":
		return fmt.Errorf("%w: validation, accessibility, and accommodation evidence are required", ErrInvalidSubmission)
	}
	return nil
}

func validCandidateSource(s candidate.Source) bool {
	switch s {
	case candidate.SourceDirect, candidate.SourceDelegated, candidate.SourceFallback:
		return true
	default:
		return false
	}
}
