package timestore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PunchPhoto is a reference to a photo artifact captured for buddy-punch
// review, linked to the punch receipt it was taken for. The photo bytes
// live in the artifact service (ARTIFACT-007); only ArtifactRef is kept
// here.
type PunchPhoto struct {
	TenantID, ID, PunchReceiptID, ArtifactRef, SiteID string
	LegalHold                                         bool
	ReviewDeadline, CapturedAt                        time.Time
	DeletedAt                                         *time.Time
}

const punchPhotoColumns = `tenant_id,id,punch_receipt_id,artifact_ref,site_id,review_deadline,legal_hold,captured_at,deleted_at`

func scanPunchPhoto(row dbport.Row) (PunchPhoto, error) {
	var p PunchPhoto
	err := row.Scan(&p.TenantID, &p.ID, &p.PunchReceiptID, &p.ArtifactRef, &p.SiteID, &p.ReviewDeadline, &p.LegalHold, &p.CapturedAt, &p.DeletedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return PunchPhoto{}, ErrNotFound
	}
	return p, err
}

// RecordPunchPhoto links a captured photo artifact to its punch receipt
// under the site's declared review window.
func (s *Store) RecordPunchPhoto(ctx context.Context, tenant, punchReceiptID, artifactRef, siteID string, reviewDeadline time.Time) (PunchPhoto, error) {
	if tenant == "" || punchReceiptID == "" || artifactRef == "" || siteID == "" || reviewDeadline.IsZero() {
		return PunchPhoto{}, ErrInvalid
	}
	id := uuid.NewString()
	var out PunchPhoto
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO time_punch_photo(tenant_id,id,punch_receipt_id,artifact_ref,site_id,review_deadline) VALUES($1,$2,$3,$4,$5,$6)`,
			tenant, id, punchReceiptID, artifactRef, siteID, reviewDeadline); err != nil {
			return err
		}
		var err error
		out, err = scanPunchPhoto(tx.QueryRow(ctx, `SELECT `+punchPhotoColumns+` FROM time_punch_photo WHERE tenant_id=$1 AND id=$2`, tenant, id))
		return err
	})
	if err != nil {
		return PunchPhoto{}, err
	}
	return out, nil
}

// GetPunchPhoto returns a photo reference by ID. It does not record a view;
// call RecordPhotoView for that (they are kept separate so a caller that
// only needs metadata, e.g. deciding whether to render a thumbnail link,
// does not have to audit a view it did not actually show).
func (s *Store) GetPunchPhoto(ctx context.Context, tenant, id string) (PunchPhoto, error) {
	if tenant == "" || id == "" {
		return PunchPhoto{}, ErrInvalid
	}
	var out PunchPhoto
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var err error
		out, err = scanPunchPhoto(tx.QueryRow(ctx, `SELECT `+punchPhotoColumns+` FROM time_punch_photo WHERE tenant_id=$1 AND id=$2`, tenant, id))
		return err
	})
	return out, err
}

// PhotoView is one append-only audit record of a photo being opened.
type PhotoView struct {
	ID, TenantID, PhotoID, ViewerID, ViewerScope string
	ViewedAt                                     time.Time
}

// RecordPhotoView appends a view audit entry. Authorization (worker or a
// supervisor with time-review scope) is the caller's job; this only
// records who viewed what, and under which scope they were granted it,
// once the caller has already decided to allow the view.
func (s *Store) RecordPhotoView(ctx context.Context, tenant, photoID, viewerID, viewerScope string) (PhotoView, error) {
	if tenant == "" || photoID == "" || viewerID == "" || viewerScope == "" {
		return PhotoView{}, ErrInvalid
	}
	out := PhotoView{ID: uuid.NewString(), TenantID: tenant, PhotoID: photoID, ViewerID: viewerID, ViewerScope: viewerScope}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := scanPunchPhoto(tx.QueryRow(ctx, `SELECT `+punchPhotoColumns+` FROM time_punch_photo WHERE tenant_id=$1 AND id=$2`, tenant, photoID)); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO time_punch_photo_view(tenant_id,id,photo_id,viewer_id,viewer_scope) VALUES($1,$2,$3,$4,$5) RETURNING viewed_at`,
			tenant, out.ID, photoID, viewerID, viewerScope).Scan(&out.ViewedAt)
	})
	if err != nil {
		return PhotoView{}, err
	}
	return out, nil
}

// PhotoViews returns a photo's append-only view audit log, oldest first.
func (s *Store) PhotoViews(ctx context.Context, tenant, photoID string, limit int) ([]PhotoView, error) {
	if tenant == "" || photoID == "" || limit <= 0 || limit > 1000 {
		return nil, ErrInvalid
	}
	out := make([]PhotoView, 0, limit)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,photo_id,viewer_id,viewer_scope,viewed_at FROM time_punch_photo_view WHERE tenant_id=$1 AND photo_id=$2 ORDER BY viewed_at LIMIT $3`, tenant, photoID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v PhotoView
			if err := rows.Scan(&v.ID, &v.TenantID, &v.PhotoID, &v.ViewerID, &v.ViewerScope, &v.ViewedAt); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

// SetPhotoLegalHold toggles the legal-hold flag that exempts a photo from
// the retention sweep regardless of its review deadline.
func (s *Store) SetPhotoLegalHold(ctx context.Context, tenant, id string, hold bool) (PunchPhoto, error) {
	if tenant == "" || id == "" {
		return PunchPhoto{}, ErrInvalid
	}
	var out PunchPhoto
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE time_punch_photo SET legal_hold=$1 WHERE tenant_id=$2 AND id=$3 AND deleted_at IS NULL`, hold, tenant, id)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrNotFound
		}
		out, err = scanPunchPhoto(tx.QueryRow(ctx, `SELECT `+punchPhotoColumns+` FROM time_punch_photo WHERE tenant_id=$1 AND id=$2`, tenant, id))
		return err
	})
	if err != nil {
		return PunchPhoto{}, err
	}
	return out, nil
}

// DueForPhotoDeletion returns photos past their review deadline that are
// not under legal hold and not already deleted, for the retention sweep.
func (s *Store) DueForPhotoDeletion(ctx context.Context, tenant string, asOf time.Time, limit int) ([]PunchPhoto, error) {
	if tenant == "" || asOf.IsZero() || limit <= 0 || limit > 1000 {
		return nil, ErrInvalid
	}
	out := make([]PunchPhoto, 0, limit)
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+punchPhotoColumns+` FROM time_punch_photo WHERE tenant_id=$1 AND deleted_at IS NULL AND legal_hold=false AND review_deadline<=$2 ORDER BY review_deadline LIMIT $3`, tenant, asOf, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanPunchPhoto(rows)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// DeletePunchPhoto soft-deletes a photo past its review window, clearing
// the artifact reference so the row keeps only the fact that a photo once
// existed and was reviewed under audit, not a dangling pointer to bytes
// the artifact service has already destroyed. Deleting a photo under
// legal hold is refused.
func (s *Store) DeletePunchPhoto(ctx context.Context, tenant, id string) error {
	if tenant == "" || id == "" {
		return ErrInvalid
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE time_punch_photo SET deleted_at=now(),artifact_ref='' WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND legal_hold=false`, tenant, id)
		if err != nil {
			return err
		}
		if tag != 1 {
			return ErrInvalid
		}
		return nil
	})
}
