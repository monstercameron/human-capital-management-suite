package agentpersonastore

import (
	"context"
	"fmt"
	"strings"
)

// ResolveCurrentReview verifies the latest decision for the exact immutable
// profile using the configured independent approval authority. A rejection
// refuses the projection; an earlier approval never replaces a later decision.
func (s *TenantStore) ResolveCurrentReview(ctx context.Context, personaID string, version int64) (VerifiedReview, error) {
	if s == nil || ctx == nil || strings.TrimSpace(personaID) == "" || strings.TrimSpace(personaID) != personaID || version <= 0 || s.reviews == nil {
		return VerifiedReview{}, ErrPublicationEvidenceRequired
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return VerifiedReview{}, err
	}
	defer tx.Rollback(ctx)
	var digest, profileOwner, owner string
	if err := tx.QueryRow(ctx, `SELECT v.content_digest,COALESCE(v.profile->>'owner',''),o.principal_id
		FROM persona_versions v JOIN persona_owners o ON o.tenant_id=v.tenant_id AND o.persona_id=v.persona_id AND o.owner_role='BUSINESS_OWNER'
		WHERE v.tenant_id=$1 AND v.persona_id=$2 AND v.version=$3`, s.tenantID, personaID, version).Scan(&digest, &profileOwner, &owner); err != nil || profileOwner == "" || profileOwner != owner {
		return VerifiedReview{}, fmt.Errorf("%w: exact persona profile is unavailable", ErrPublicationEvidenceRequired)
	}
	var reviewID, decision string
	if err := tx.QueryRow(ctx, `SELECT review_id,decision FROM persona_review_decision
		WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3 AND profile_digest=$4
		ORDER BY reviewed_at DESC,review_id COLLATE "C" DESC LIMIT 1`, s.tenantID, personaID, version, digest).Scan(&reviewID, &decision); err != nil {
		return VerifiedReview{}, fmt.Errorf("%w: latest review is unavailable: %v", ErrPublicationEvidenceRequired, err)
	}
	if decision != "APPROVE" {
		return VerifiedReview{}, ErrPublicationEvidenceRequired
	}
	review, err := s.reviews.ResolvePersonaReview(ctx, tx, s.tenant, personaID, version, digest, reviewID)
	if err != nil {
		return VerifiedReview{}, err
	}
	if review.ReviewID != reviewID || review.TenantID != string(s.tenant) || review.PersonaID != personaID || review.PersonaVersion != version || review.ProfileDigest != digest || review.Decision != decision || review.Permission != "persona:review" || !review.GrantCurrent || review.ReviewDigest == "" || review.ReviewerID == "" || review.ReviewerID == owner {
		return VerifiedReview{}, ErrPublicationEvidenceRequired
	}
	if err := commit(ctx, tx); err != nil {
		return VerifiedReview{}, err
	}
	return review, nil
}
