package agentpersonastore

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DurableReviewAuthority resolves approval evidence and its current grant from
// the caller-owned agent-store transaction. It has no caller-controlled cache.
type DurableReviewAuthority struct {
	tenantUUID func(values.TenantId) uuid.UUID
}

// NewDurableReviewAuthority binds logical tenant identities to the agent
// database UUIDs used by forced row-level security.
func NewDurableReviewAuthority(tenantUUID func(values.TenantId) uuid.UUID) (DurableReviewAuthority, error) {
	if tenantUUID == nil {
		return DurableReviewAuthority{}, fmt.Errorf("%w: tenant mapper is required", ErrInvalid)
	}
	return DurableReviewAuthority{tenantUUID: tenantUUID}, nil
}

// ResolvePersonaReview verifies an immutable decision for the exact tenant,
// persona version and profile digest, then holds transaction advisory locks
// shared with owner and grant revocation writers through commit. The serving
// role remains SELECT-only while revocation and publication serialize.
func (a DurableReviewAuthority) ResolvePersonaReview(ctx context.Context, tx dbport.Tx, tenant values.TenantId, personaID string, version int64, profileDigest, reviewID string) (VerifiedReview, error) {
	if ctx == nil || tx == nil || a.tenantUUID == nil || strings.TrimSpace(string(tenant)) == "" || strings.TrimSpace(personaID) == "" || version <= 0 || strings.TrimSpace(profileDigest) == "" || strings.TrimSpace(reviewID) == "" {
		return VerifiedReview{}, fmt.Errorf("%w: incomplete durable review lookup", ErrInvalid)
	}
	tenantID := a.tenantUUID(tenant)
	if tenantID == uuid.Nil {
		return VerifiedReview{}, fmt.Errorf("%w: tenant is unknown", ErrInvalid)
	}
	var grantID string
	err := tx.QueryRow(ctx, `SELECT grant_id FROM persona_review_decision
		WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3 AND profile_digest=$4 AND review_id=$5`,
		tenantID, personaID, version, profileDigest, reviewID).Scan(&grantID)
	if err != nil {
		return VerifiedReview{}, fmt.Errorf("%w: durable review decision unavailable: %v", ErrPublicationEvidenceRequired, err)
	}
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2 || ':' || $3, 0)) IS NULL`, string(tenant), personaID, fmt.Sprintf("%d", version)).Scan(&locked); err != nil {
		return VerifiedReview{}, fmt.Errorf("%w: review target lock unavailable: %v", ErrPublicationEvidenceRequired, err)
	}
	if err := tx.QueryRow(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('persona-review-owner:' || $1 || ':' || $2, 0)) IS NULL`, tenantID.String(), personaID).Scan(&locked); err != nil {
		return VerifiedReview{}, fmt.Errorf("%w: review owner lock unavailable: %v", ErrPublicationEvidenceRequired, err)
	}
	if err := tx.QueryRow(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('persona-review-grant:' || $1 || ':' || $2, 0)) IS NULL`, tenantID.String(), grantID).Scan(&locked); err != nil {
		return VerifiedReview{}, fmt.Errorf("%w: review grant lock unavailable: %v", ErrPublicationEvidenceRequired, err)
	}
	var out VerifiedReview
	var resolvedGrantID, authorID string
	var reviewRevoked bool
	err = tx.QueryRow(ctx, `SELECT review_id,tenant_id::text,persona_id,persona_version,profile_digest,
		reviewer_id,permission,decision,review_digest,grant_id,
		author_id,(revoked_at IS NOT NULL OR expires_at <= clock_timestamp())
		FROM persona_review_decision WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3 AND profile_digest=$4 AND review_id=$5`,
		tenantID, personaID, version, profileDigest, reviewID).Scan(
		&out.ReviewID, &out.TenantID, &out.PersonaID, &out.PersonaVersion, &out.ProfileDigest,
		&out.ReviewerID, &out.Permission, &out.Decision, &out.ReviewDigest, &resolvedGrantID, &authorID, &reviewRevoked)
	if err != nil {
		return VerifiedReview{}, fmt.Errorf("%w: durable review decision unavailable: %v", ErrPublicationEvidenceRequired, err)
	}
	if reviewRevoked || resolvedGrantID != grantID || out.Decision != "APPROVE" {
		return VerifiedReview{}, fmt.Errorf("%w: review decision revoked or stale", ErrPublicationEvidenceRequired)
	}
	var currentAuthor string
	if err := tx.QueryRow(ctx, `SELECT principal_id FROM persona_owners WHERE tenant_id=$1 AND persona_id=$2 AND owner_role='BUSINESS_OWNER'`, tenantID, personaID).Scan(&currentAuthor); err != nil || currentAuthor != authorID {
		return VerifiedReview{}, fmt.Errorf("%w: review author does not match the current durable persona author", ErrPublicationEvidenceRequired)
	}
	// SQL tenant_id is the agent database UUID; return the corresponding logical
	// identity only after the exact tenant-bound record has been resolved.
	out.TenantID = string(tenant)
	var grantPrincipal, grantPermission string
	var grantCurrent bool
	err = tx.QueryRow(ctx, `SELECT principal_id,permission,
		(revoked_at IS NULL AND granted_at <= clock_timestamp() AND expires_at > clock_timestamp())
		FROM persona_review_grant WHERE tenant_id=$1 AND grant_id=$2`, tenantID, grantID).Scan(&grantPrincipal, &grantPermission, &grantCurrent)
	if err != nil {
		return VerifiedReview{}, fmt.Errorf("%w: review grant unavailable: %v", ErrPublicationEvidenceRequired, err)
	}
	out.GrantCurrent = grantCurrent && grantPrincipal == out.ReviewerID && grantPermission == out.Permission
	if !out.GrantCurrent {
		return VerifiedReview{}, fmt.Errorf("%w: reviewer permission is not current", ErrPublicationEvidenceRequired)
	}
	return out, nil
}
