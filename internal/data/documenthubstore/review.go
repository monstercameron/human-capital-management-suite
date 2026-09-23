// Independent review for HUB-008: a decision binds the exact version hash
// the reviewer saw, plus scope, reviewer and authority. The hash is
// server-resolved from the immutable version row, never caller-supplied,
// and the author cannot review their own candidate, so approving version A
// can never authorize version B.
package documenthubstore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Review decisions inside the lifecycle vocabulary.
const (
	ReviewApproved         = "approved"
	ReviewRejected         = "rejected"
	ReviewChangesRequested = "changes_requested"
)

// ErrSelfApproval is returned when the author reviews their own candidate;
// ErrNoReview when a scope holds no decision for a version.
var (
	ErrSelfApproval = errors.New("document review: author cannot review own candidate")
	ErrNoReview     = errors.New("document review: no decision for version and scope")
)

// ReviewInput carries a reviewer's decision; the version hash is resolved
// by the store, not supplied by the caller.
type ReviewInput struct {
	DocumentID, VersionID, ScopeKind, ScopeID string
	ReviewerID, Authority, Decision, Note     string
}

// Review is one recorded decision with its hash binding.
type Review struct {
	ID, DocumentID, VersionID, VersionHash    string
	ScopeKind, ScopeID, ReviewerID, Authority string
	Decision, Note                            string
	DecidedAt                                 time.Time
}

// RecordReview appends a decision pinning the version's current hash.
func (s *Store) RecordReview(ctx context.Context, tenantID string, in ReviewInput) (Review, error) {
	switch in.Decision {
	case ReviewApproved, ReviewRejected, ReviewChangesRequested:
	default:
		return Review{}, errors.New("document review: unknown decision")
	}
	if in.ReviewerID == "" || in.Authority == "" {
		return Review{}, errors.New("document review: reviewer and authority are required")
	}
	var review Review
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		version, err := loadVersion(ctx, tx, tenantID, in.DocumentID, in.VersionID)
		if err != nil {
			return err
		}
		if version.CreatorID == in.ReviewerID {
			return ErrSelfApproval
		}
		review = Review{
			ID:          "docr-" + uuid.NewString(),
			DocumentID:  in.DocumentID,
			VersionID:   in.VersionID,
			VersionHash: version.Hash,
			ScopeKind:   in.ScopeKind,
			ScopeID:     in.ScopeID,
			ReviewerID:  in.ReviewerID,
			Authority:   in.Authority,
			Decision:    in.Decision,
			Note:        in.Note,
		}
		_, err = tx.Exec(ctx, `INSERT INTO document_review(id,tenant_id,document_id,version_id,version_hash,scope_kind,scope_id,reviewer_id,authority,decision,note) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			review.ID, tenantID, review.DocumentID, review.VersionID, review.VersionHash, review.ScopeKind, review.ScopeID, review.ReviewerID, review.Authority, review.Decision, review.Note)
		return err
	})
	if err != nil {
		return Review{}, err
	}
	return review, nil
}

// LatestReview returns the most recent decision for one version and scope.
func (s *Store) LatestReview(ctx context.Context, tenantID, docID, versionID, scopeKind, scopeID string) (Review, error) {
	var review Review
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var err error
		review, err = latestReviewTx(ctx, tx, tenantID, docID, versionID, scopeKind, scopeID)
		return err
	})
	if err != nil {
		return Review{}, ErrNoReview
	}
	return review, nil
}

// latestReviewTx is the transaction-scoped review lookup shared by
// LatestReview and Deploy.
func latestReviewTx(ctx context.Context, tx dbport.Tx, tenantID, docID, versionID, scopeKind, scopeID string) (Review, error) {
	var review Review
	err := tx.QueryRow(ctx, `SELECT id,document_id,version_id,version_hash,scope_kind,scope_id,reviewer_id,authority,decision,note,decided_at FROM document_review WHERE tenant_id=$1 AND document_id=$2 AND version_id=$3 AND scope_kind=$4 AND scope_id=$5 ORDER BY decided_at DESC LIMIT 1`,
		tenantID, docID, versionID, scopeKind, scopeID).Scan(&review.ID, &review.DocumentID, &review.VersionID, &review.VersionHash, &review.ScopeKind, &review.ScopeID, &review.ReviewerID, &review.Authority, &review.Decision, &review.Note, &review.DecidedAt)
	if err != nil {
		return Review{}, ErrNoReview
	}
	return review, nil
}
