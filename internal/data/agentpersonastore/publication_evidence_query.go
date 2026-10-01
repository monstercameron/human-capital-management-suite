package agentpersonastore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ResolvePublicationEvidence selects and verifies current evidence for the
// exact immutable persona profile. Publish repeats verification at commit.
func (s *TenantStore) ResolvePublicationEvidence(ctx context.Context, personaID string, version int64) (PublicationEvidence, error) {
	if s == nil || ctx == nil || strings.TrimSpace(personaID) == "" || version <= 0 || s.reviews == nil || s.evaluations == nil {
		return PublicationEvidence{}, ErrPublicationEvidenceRequired
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return PublicationEvidence{}, err
	}
	defer tx.Rollback(ctx)
	var digest, profileOwner, owner string
	if err := tx.QueryRow(ctx, `SELECT v.content_digest,COALESCE(v.profile->>'owner',''),o.principal_id
		FROM persona_versions v JOIN persona_owners o ON o.tenant_id=v.tenant_id AND o.persona_id=v.persona_id AND o.owner_role='BUSINESS_OWNER'
		WHERE v.tenant_id=$1 AND v.persona_id=$2 AND v.version=$3`, s.tenantID, personaID, version).Scan(&digest, &profileOwner, &owner); err != nil || profileOwner == "" || profileOwner != owner {
		return PublicationEvidence{}, fmt.Errorf("%w: exact persona profile is unavailable", ErrPublicationEvidenceRequired)
	}
	var evidence PublicationEvidence
	var decision string
	err = tx.QueryRow(ctx, `SELECT review_id,decision FROM persona_review_decision
		WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3 AND profile_digest=$4
		ORDER BY reviewed_at DESC,review_id COLLATE "C" DESC LIMIT 1`, s.tenantID, personaID, version, digest).Scan(&evidence.ReviewID, &decision)
	if errors.Is(err, dbport.ErrNoRows) || decision != "APPROVE" {
		return PublicationEvidence{}, ErrPublicationEvidenceRequired
	}
	if err != nil {
		return PublicationEvidence{}, err
	}
	err = tx.QueryRow(ctx, `SELECT run_id FROM persona_evaluation_evidence
		WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3 AND profile_digest=$4
		ORDER BY issued_at DESC,run_id COLLATE "C" DESC LIMIT 1`, s.tenantID, personaID, version, digest).Scan(&evidence.EvaluationRunID)
	if err != nil {
		return PublicationEvidence{}, fmt.Errorf("%w: evaluation is unavailable", ErrPublicationEvidenceRequired)
	}
	review, err := s.reviews.ResolvePersonaReview(ctx, tx, s.tenant, personaID, version, digest, evidence.ReviewID)
	if err != nil {
		return PublicationEvidence{}, err
	}
	evaluation, err := s.evaluations.ResolvePersonaEvaluation(ctx, tx, s.tenant, evidence.EvaluationRunID, personaID, version, digest)
	if err != nil {
		return PublicationEvidence{}, err
	}
	if review.ReviewID != evidence.ReviewID || review.TenantID != string(s.tenant) || review.PersonaID != personaID || review.PersonaVersion != version || review.ProfileDigest != digest || review.Decision != "APPROVE" || review.Permission != "persona:review" || !review.GrantCurrent || review.ReviewDigest == "" || review.ReviewerID == "" || review.ReviewerID == owner ||
		evaluation.RunID != evidence.EvaluationRunID || evaluation.TenantID != string(s.tenant) || evaluation.PersonaID != personaID || evaluation.PersonaVersion != version || evaluation.ProfileDigest != digest || !evaluation.Passed || !evaluation.Fresh || evaluation.RunDigest == "" || evaluation.SuiteDigest == "" {
		return PublicationEvidence{}, ErrPublicationEvidenceRequired
	}
	if err := commit(ctx, tx); err != nil {
		return PublicationEvidence{}, err
	}
	return evidence, nil
}
