package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	ErrPhotoDispositionHeld     = errors.New("photo disposition is protected by a live hold or open exception")
	ErrPhotoDispositionNotDue   = errors.New("photo disposition is not due")
	ErrPhotoDispositionConflict = errors.New("photo disposition revision conflict")
)

// PhotoDisposition is the durable lease/tombstone state for one photo.
type PhotoDisposition struct {
	TenantID, PhotoID, ArtifactRef, State, IdempotencyKey string
	Revision                                              int64
	ClaimedAt, TombstonedAt, FinalizedAt                  *time.Time
}

func (s *Store) ClaimPhotoDisposition(ctx context.Context, tenant, photoID string, expectedRevision int64, idempotencyKey string, asOf time.Time) (PhotoDisposition, error) {
	if tenant == "" || photoID == "" || expectedRevision <= 0 || strings.TrimSpace(idempotencyKey) == "" || asOf.IsZero() {
		return PhotoDisposition{}, ErrInvalid
	}
	var out PhotoDisposition
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if err := photoDispositionLock(ctx, tx, tenant, photoID); err != nil {
			return err
		}
		if err := loadIdempotentDisposition(ctx, tx, tenant, photoID, idempotencyKey, &out); err == nil {
			return nil
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		var deadline time.Time
		var current int64
		var artifact string
		if err := tx.QueryRow(ctx, `SELECT artifact_ref,review_deadline,disposition_revision FROM time_punch_photo WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, tenant, photoID).Scan(&artifact, &deadline, &current); errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if current != expectedRevision {
			return ErrPhotoDispositionConflict
		}
		if asOf.Before(deadline) {
			return ErrPhotoDispositionNotDue
		}
		held, err := photoGovernanceHeld(ctx, tx, tenant, photoID)
		if err != nil {
			return err
		}
		if held {
			return ErrPhotoDispositionHeld
		}
		return writePhotoDisposition(ctx, tx, tenant, photoID, artifact, "CLAIMED", current+1, idempotencyKey, "CLAIM", &out)
	})
	return out, err
}

// TombstonePhotoDisposition durably fences the artifact before an external delete.
func (s *Store) TombstonePhotoDisposition(ctx context.Context, tenant, photoID string, expectedRevision int64, idempotencyKey string) (PhotoDisposition, error) {
	return s.transitionPhotoDisposition(ctx, tenant, photoID, expectedRevision, idempotencyKey, "TOMBSTONE")
}

// FinalizePhotoDisposition clears the artifact reference after the external
// store confirms deletion. It is safe to retry with the same idempotency key.
func (s *Store) FinalizePhotoDisposition(ctx context.Context, tenant, photoID string, expectedRevision int64, idempotencyKey string) (PhotoDisposition, error) {
	return s.transitionPhotoDisposition(ctx, tenant, photoID, expectedRevision, idempotencyKey, "FINALIZE")
}

func (s *Store) transitionPhotoDisposition(ctx context.Context, tenant, photoID string, expectedRevision int64, idempotencyKey, action string) (PhotoDisposition, error) {
	if tenant == "" || photoID == "" || expectedRevision <= 0 || strings.TrimSpace(idempotencyKey) == "" {
		return PhotoDisposition{}, ErrInvalid
	}
	var out PhotoDisposition
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if err := photoDispositionLock(ctx, tx, tenant, photoID); err != nil {
			return err
		}
		if err := loadIdempotentDisposition(ctx, tx, tenant, photoID, idempotencyKey, &out); err == nil {
			return nil
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		var d PhotoDisposition
		if err := tx.QueryRow(ctx, `SELECT tenant_id,photo_id,artifact_ref,state,revision,last_idempotency_key,claimed_at,tombstoned_at,finalized_at FROM time_punch_photo_disposition WHERE tenant_id=$1 AND photo_id=$2 FOR UPDATE`, tenant, photoID).Scan(&d.TenantID, &d.PhotoID, &d.ArtifactRef, &d.State, &d.Revision, &d.IdempotencyKey, &d.ClaimedAt, &d.TombstonedAt, &d.FinalizedAt); errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if d.Revision != expectedRevision {
			return ErrPhotoDispositionConflict
		}
		if action == "TOMBSTONE" {
			held, err := photoGovernanceHeld(ctx, tx, tenant, photoID)
			if err != nil {
				return err
			}
			if d.State != "CLAIMED" || held {
				return ErrPhotoDispositionHeld
			}
			return writePhotoDisposition(ctx, tx, tenant, photoID, d.ArtifactRef, "TOMBSTONED", d.Revision+1, idempotencyKey, action, &out)
		}
		if d.State != "TOMBSTONED" {
			return ErrPhotoDispositionConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE time_punch_photo SET artifact_ref='',deleted_at=COALESCE(deleted_at,now()),disposition_revision=$1 WHERE tenant_id=$2 AND id=$3`, d.Revision+1, tenant, photoID); err != nil {
			return err
		}
		return writePhotoDisposition(ctx, tx, tenant, photoID, d.ArtifactRef, "DELETED", d.Revision+1, idempotencyKey, action, &out)
	})
	return out, err
}

func photoDispositionLock(ctx context.Context, tx dbport.Tx, tenant, photoID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2, 0))`, tenant, photoID)
	return err
}

func photoGovernanceHeld(ctx context.Context, tx dbport.Tx, tenant, photoID string) (bool, error) {
	var held bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM time_punch_photo_hold WHERE tenant_id=$1 AND photo_id=$2 AND released_at IS NULL) OR EXISTS(SELECT 1 FROM time_punch_photo_exception WHERE tenant_id=$1 AND photo_id=$2 AND resolved_at IS NULL)`, tenant, photoID).Scan(&held)
	return held, err
}

func loadIdempotentDisposition(ctx context.Context, tx dbport.Tx, tenant, photoID, key string, out *PhotoDisposition) error {
	err := tx.QueryRow(ctx, `SELECT tenant_id,photo_id,artifact_ref,state,revision,last_idempotency_key,claimed_at,tombstoned_at,finalized_at FROM time_punch_photo_disposition WHERE tenant_id=$1 AND photo_id=$2 AND last_idempotency_key=$3`, tenant, photoID, key).Scan(&out.TenantID, &out.PhotoID, &out.ArtifactRef, &out.State, &out.Revision, &out.IdempotencyKey, &out.ClaimedAt, &out.TombstonedAt, &out.FinalizedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func writePhotoDisposition(ctx context.Context, tx dbport.Tx, tenant, photoID, artifact, state string, revision int64, key, action string, out *PhotoDisposition) error {
	if _, err := tx.Exec(ctx, `INSERT INTO time_punch_photo_disposition(tenant_id,photo_id,artifact_ref,state,revision,last_idempotency_key,tombstoned_at,finalized_at) VALUES($1,$2,$3,$4,$5,$6,CASE WHEN $4='TOMBSTONED' THEN now() END,CASE WHEN $4='DELETED' THEN now() END) ON CONFLICT (tenant_id,photo_id) DO UPDATE SET artifact_ref=EXCLUDED.artifact_ref,state=EXCLUDED.state,revision=EXCLUDED.revision,last_idempotency_key=EXCLUDED.last_idempotency_key,tombstoned_at=EXCLUDED.tombstoned_at,finalized_at=EXCLUDED.finalized_at`, tenant, photoID, artifact, state, revision, key); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO time_punch_photo_disposition_audit(tenant_id,id,photo_id,action,idempotency_key,revision,artifact_ref) VALUES($1,$2,$3,$4,$5,$6,$7)`, tenant, uuid.NewString(), photoID, action, key, revision, artifact); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"photo_id": photoID, "action": action, "revision": revision, "artifact_ref": artifact})
	if err := appendOutbox(ctx, tx, tenant, "time.photo.disposition."+strings.ToLower(action), 1, payload); err != nil {
		return err
	}
	return tx.QueryRow(ctx, `SELECT tenant_id,photo_id,artifact_ref,state,revision,last_idempotency_key,claimed_at,tombstoned_at,finalized_at FROM time_punch_photo_disposition WHERE tenant_id=$1 AND photo_id=$2`, tenant, photoID).Scan(&out.TenantID, &out.PhotoID, &out.ArtifactRef, &out.State, &out.Revision, &out.IdempotencyKey, &out.ClaimedAt, &out.TombstonedAt, &out.FinalizedAt)
}
