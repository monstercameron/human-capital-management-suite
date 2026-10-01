package agentpersonastore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DurablePersonaReviewIssuer persists independent review evidence through a
// database handle configured with the dedicated review-authority role.
type DurablePersonaReviewIssuer struct {
	db         DB
	tenantUUID func(values.TenantId) uuid.UUID
}

// NewDurablePersonaReviewIssuer requires a separately credentialed writer and
// a canonical tenant-to-database identity mapping.
func NewDurablePersonaReviewIssuer(db DB, tenantUUID func(values.TenantId) uuid.UUID) (*DurablePersonaReviewIssuer, error) {
	if db == nil || tenantUUID == nil {
		return nil, fmt.Errorf("%w: review authority database and tenant mapper are required", ErrInvalid)
	}
	return &DurablePersonaReviewIssuer{db: db, tenantUUID: tenantUUID}, nil
}

// AuthorizePersonaReview checks the current tenant-scoped review grant before
// a caller performs an earlier lifecycle side effect. Evidence issuance still
// repeats and locks this check in its own transaction.
func (i *DurablePersonaReviewIssuer) AuthorizePersonaReview(ctx context.Context, tenant values.TenantId, reviewerID string) error {
	if i == nil || i.db == nil || i.tenantUUID == nil || ctx == nil || tenant.Validate() != nil || strings.TrimSpace(reviewerID) == "" || reviewerID != strings.TrimSpace(reviewerID) {
		return ErrInvalid
	}
	tenantID := i.tenantUUID(tenant)
	if tenantID == uuid.Nil {
		return ErrInvalid
	}
	tx, err := i.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("agentpersonastore: begin review grant check: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return fmt.Errorf("agentpersonastore: scope review grant check: %w", err)
	}
	var current bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM persona_review_grant
		WHERE tenant_id=$1 AND principal_id=$2 AND permission='persona:review'
		AND granted_at <= clock_timestamp() AND expires_at > clock_timestamp() AND revoked_at IS NULL)`, tenantID, reviewerID).Scan(&current)
	if err != nil {
		return fmt.Errorf("agentpersonastore: check current persona review grant: %w", err)
	}
	if !current {
		return fmt.Errorf("%w: reviewer has no current persona-review grant", ErrPublicationEvidenceRequired)
	}
	return nil
}

// IssuePersonaReview issues evidence only for the exact persisted profile and
// its current business owner, while locking a current grant through commit.
func (i *DurablePersonaReviewIssuer) IssuePersonaReview(ctx context.Context, tenant values.TenantId, personaID string, version int64, reviewerID, decision string) (VerifiedReview, error) {
	if i == nil || i.db == nil || i.tenantUUID == nil || ctx == nil || tenant.Validate() != nil || strings.TrimSpace(personaID) == "" || personaID != strings.TrimSpace(personaID) || version <= 0 || strings.TrimSpace(reviewerID) == "" || reviewerID != strings.TrimSpace(reviewerID) || (decision != "APPROVE" && decision != "REJECT") {
		return VerifiedReview{}, ErrInvalid
	}
	tenantID := i.tenantUUID(tenant)
	if tenantID == uuid.Nil {
		return VerifiedReview{}, ErrInvalid
	}
	tx, err := i.db.Begin(ctx)
	if err != nil {
		return VerifiedReview{}, fmt.Errorf("agentpersonastore: begin review issuance: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return VerifiedReview{}, fmt.Errorf("agentpersonastore: scope review issuance: %w", err)
	}
	evidence, err := issueReviewInTx(ctx, tx, tenant, tenantID, personaID, version, reviewerID, decision)
	if err != nil {
		return VerifiedReview{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VerifiedReview{}, fmt.Errorf("agentpersonastore: commit review evidence: %w", err)
	}
	return evidence, nil
}

func issueReviewInTx(ctx context.Context, tx dbport.Tx, tenant values.TenantId, tenantID uuid.UUID, personaID string, version int64, reviewerID, decision string) (VerifiedReview, error) {
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2 || ':' || $3, 0)) IS NULL`, string(tenant), personaID, fmt.Sprintf("%d", version)).Scan(&locked); err != nil {
		return VerifiedReview{}, fmt.Errorf("agentpersonastore: lock review target: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('persona-review-owner:' || $1 || ':' || $2, 0)) IS NULL`, tenantID.String(), personaID).Scan(&locked); err != nil {
		return VerifiedReview{}, fmt.Errorf("agentpersonastore: lock review owner: %w", err)
	}
	var profileDigest, profileOwner, businessOwner string
	err := tx.QueryRow(ctx, `SELECT v.content_digest, COALESCE(v.profile->>'owner',''), o.principal_id
		FROM persona_versions v JOIN persona_owners o ON o.tenant_id=v.tenant_id AND o.persona_id=v.persona_id
		WHERE v.tenant_id=$1 AND v.persona_id=$2 AND v.version=$3 AND o.owner_role='BUSINESS_OWNER'`, tenantID, personaID, version).Scan(&profileDigest, &profileOwner, &businessOwner)
	if errors.Is(err, dbport.ErrNoRows) {
		return VerifiedReview{}, fmt.Errorf("%w: exact persona version and business owner are required", ErrPublicationEvidenceRequired)
	}
	if err != nil {
		return VerifiedReview{}, fmt.Errorf("agentpersonastore: load review target: %w", err)
	}
	if strings.TrimSpace(profileDigest) == "" || strings.TrimSpace(profileOwner) == "" || profileOwner != businessOwner || reviewerID == businessOwner {
		return VerifiedReview{}, fmt.Errorf("%w: reviewer must differ from the exact stored business owner", ErrPublicationEvidenceRequired)
	}
	var currentState LifecycleState
	err = tx.QueryRow(ctx, `SELECT to_state FROM persona_lifecycle_events
		WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3 ORDER BY event_sequence DESC LIMIT 1`, tenantID, personaID, version).Scan(&currentState)
	if err != nil || currentState != StateInReview {
		return VerifiedReview{}, fmt.Errorf("%w: persona version is not awaiting review", ErrPublicationEvidenceRequired)
	}
	var grantID string
	var grantExpires, issuedAt time.Time
	err = tx.QueryRow(ctx, `SELECT grant_id, expires_at, clock_timestamp() FROM persona_review_grant
		WHERE tenant_id=$1 AND principal_id=$2 AND permission='persona:review'
		AND granted_at <= clock_timestamp() AND expires_at > clock_timestamp() AND revoked_at IS NULL
		ORDER BY granted_at DESC, grant_id LIMIT 1 FOR SHARE`, tenantID, reviewerID).Scan(&grantID, &grantExpires, &issuedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return VerifiedReview{}, fmt.Errorf("%w: reviewer has no current persona-review grant", ErrPublicationEvidenceRequired)
	}
	if err != nil {
		return VerifiedReview{}, fmt.Errorf("agentpersonastore: resolve current reviewer grant: %w", err)
	}
	expiresAt := issuedAt.Add(30 * 24 * time.Hour)
	if grantExpires.Before(expiresAt) {
		expiresAt = grantExpires
	}
	reviewID := uuid.NewString()
	permission := "persona:review"
	reviewDigest := personaReviewDigest(profileDigest, reviewerID, permission)
	_, err = tx.Exec(ctx, `INSERT INTO persona_review_decision
		(tenant_id,review_id,persona_id,persona_version,profile_digest,author_id,reviewer_id,grant_id,permission,decision,review_digest,reviewed_at,expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, tenantID, reviewID, personaID, version,
		profileDigest, businessOwner, reviewerID, grantID, permission, decision, reviewDigest, issuedAt, expiresAt)
	if err != nil {
		return VerifiedReview{}, fmt.Errorf("agentpersonastore: persist review evidence: %w", err)
	}
	return VerifiedReview{ReviewID: reviewID, TenantID: string(tenant), PersonaID: personaID, PersonaVersion: version,
		ProfileDigest: profileDigest, ReviewerID: reviewerID, Permission: permission, Decision: decision,
		ReviewDigest: reviewDigest, GrantCurrent: true}, nil
}

func personaReviewDigest(profileDigest, reviewerID, permission string) string {
	data, _ := json.Marshal(struct{ ProfileDigest, Reviewer, Permission string }{profileDigest, reviewerID, permission})
	sum := sha256.Sum256(append([]byte("hcm-next-agent-persona-review/v1\x00"), data...))
	return "sha256:" + hex.EncodeToString(sum[:])
}
