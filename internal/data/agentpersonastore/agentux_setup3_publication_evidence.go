package agentpersonastore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PublicationEvidenceDetails is the human-readable identity and timing of the
// exact review and evaluation pins retained by a publication event.
type PublicationEvidenceDetails struct {
	ReviewerID         string
	ReviewApprovedAt   time.Time
	EvaluationPassedAt time.Time
}

// ReadPublicationEvidenceDetails reads retained evidence for an exact
// immutable version. It never substitutes a newer review or evaluation that
// was not pinned by the publication event.
func (s *TenantStore) ReadPublicationEvidenceDetails(ctx context.Context, personaID string, version int64) (PublicationEvidenceDetails, error) {
	if s == nil || ctx == nil || strings.TrimSpace(personaID) == "" || strings.TrimSpace(personaID) != personaID || version <= 0 {
		return PublicationEvidenceDetails{}, ErrPublicationEvidenceRequired
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return PublicationEvidenceDetails{}, err
	}
	defer tx.Rollback(ctx)
	var details PublicationEvidenceDetails
	err = tx.QueryRow(ctx, `SELECT lifecycle.reviewer_id,review.reviewed_at,evaluation.issued_at
		FROM persona_lifecycle_events lifecycle
		JOIN persona_versions version ON version.tenant_id=lifecycle.tenant_id
		 AND version.persona_id=lifecycle.persona_id AND version.version=lifecycle.persona_version
		JOIN persona_review_decision review ON review.tenant_id=lifecycle.tenant_id
		 AND review.persona_id=lifecycle.persona_id AND review.persona_version=lifecycle.persona_version
		 AND review.profile_digest=lifecycle.profile_digest AND review.review_digest=lifecycle.review_digest
		 AND review.reviewer_id=lifecycle.reviewer_id AND review.decision='APPROVE'
		JOIN persona_evaluation_evidence evaluation ON evaluation.tenant_id=lifecycle.tenant_id
		 AND evaluation.persona_id=lifecycle.persona_id AND evaluation.persona_version=lifecycle.persona_version
		 AND evaluation.profile_digest=lifecycle.evaluation_profile_digest
		 AND evaluation.run_digest=lifecycle.evaluation_digest
		 AND evaluation.suite_digest=lifecycle.evaluation_suite_digest AND evaluation.passed
		WHERE lifecycle.tenant_id=$1 AND lifecycle.persona_id=$2 AND lifecycle.persona_version=$3
		 AND lifecycle.to_state='PUBLISHED' AND lifecycle.profile_digest=version.content_digest
		ORDER BY lifecycle.event_sequence DESC,review.reviewed_at DESC,evaluation.issued_at DESC LIMIT 1`, s.tenantID, personaID, version).Scan(
		&details.ReviewerID, &details.ReviewApprovedAt, &details.EvaluationPassedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return PublicationEvidenceDetails{}, ErrPublicationEvidenceRequired
	}
	if err != nil {
		return PublicationEvidenceDetails{}, fmt.Errorf("agentpersonastore: read publication evidence details: %w", err)
	}
	if strings.TrimSpace(details.ReviewerID) == "" || details.ReviewApprovedAt.IsZero() || details.EvaluationPassedAt.IsZero() {
		return PublicationEvidenceDetails{}, ErrPublicationEvidenceRequired
	}
	details.ReviewApprovedAt = details.ReviewApprovedAt.UTC()
	details.EvaluationPassedAt = details.EvaluationPassedAt.UTC()
	if err := commit(ctx, tx); err != nil {
		return PublicationEvidenceDetails{}, err
	}
	return details, nil
}
