package documenthubstore

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ReadPersonalDocument returns the latest candidate to its owner or the
// active default deployment to a directly granted reader. Unknown, denied,
// retired, and not yet deployed documents have the same public result.
func (s *Store) ReadPersonalDocument(ctx context.Context, tenantID, actorID, docID string) (DocumentSummary, Version, error) {
	if actorID == "" || docID == "" {
		return DocumentSummary{}, Version{}, ErrDenied
	}
	var summary DocumentSummary
	var version Version
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var owner, home, lifecycle string
		if err := tx.QueryRow(ctx, `SELECT owner_id,home,lifecycle FROM document WHERE tenant_id=$1 AND id=$2`, tenantID, docID).Scan(&owner, &home, &lifecycle); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrDenied
			}
			return err
		}
		if home != "PERSONAL" || lifecycle == "DISPOSED" {
			return ErrDenied
		}
		if err := authorizeTx(ctx, tx, tenantID, docID, "person", actorID, ActionRead); err != nil {
			return err
		}
		var versionID string
		if owner == actorID {
			if err := tx.QueryRow(ctx, `SELECT id FROM document_version WHERE tenant_id=$1 AND document_id=$2 AND status<>'retired' ORDER BY created_at DESC,id DESC LIMIT 1`, tenantID, docID).Scan(&versionID); err != nil {
				if errors.Is(err, dbport.ErrNoRows) {
					return ErrDenied
				}
				return err
			}
		} else {
			if err := tx.QueryRow(ctx, `SELECT p.version_id FROM document_active_pointer p
				JOIN document_version v ON v.tenant_id=p.tenant_id AND v.document_id=p.document_id AND v.id=p.version_id AND v.status<>'retired'
				WHERE p.tenant_id=$1 AND p.document_id=$2 AND p.scope_kind='default' AND p.scope_id=''`, tenantID, docID).Scan(&versionID); err != nil {
				if errors.Is(err, dbport.ErrNoRows) {
					return ErrDenied
				}
				return err
			}
		}
		var err error
		version, err = loadVersion(ctx, tx, tenantID, docID, versionID)
		if err != nil {
			return ErrDenied
		}
		var shared bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM document_grant g WHERE g.tenant_id=$1 AND g.document_id=$2 AND g.subject_kind='person' AND g.subject_id<>$3 AND g.action='read' AND g.effect='allow' AND g.revoked=false AND (g.expires_at IS NULL OR g.expires_at>now()) AND NOT EXISTS (SELECT 1 FROM document_grant deny WHERE deny.tenant_id=g.tenant_id AND deny.document_id=g.document_id AND deny.subject_kind='person' AND deny.subject_id=g.subject_id AND deny.action='read' AND deny.effect='deny' AND deny.revoked=false AND (deny.expires_at IS NULL OR deny.expires_at>now())))`, tenantID, docID, owner).Scan(&shared); err != nil {
			return err
		}
		status := "shared"
		if owner == actorID {
			status = "private"
		}
		canManage := false
		canEdit := false
		if owner == actorID {
			manageErr := authorizeTx(ctx, tx, tenantID, docID, "person", actorID, ActionManage)
			if manageErr != nil && !errors.Is(manageErr, ErrDenied) {
				return manageErr
			}
			canManage = manageErr == nil
			editErr := authorizeTx(ctx, tx, tenantID, docID, "person", actorID, ActionPropose)
			if editErr != nil && !errors.Is(editErr, ErrDenied) {
				return editErr
			}
			canEdit = editErr == nil
		}
		summary = DocumentSummary{ID: docID, Title: version.Title, OwnerID: owner, VersionID: version.ID, Status: status, UpdatedAt: version.CreatedAt, Shared: shared, CanManage: canManage, CanEdit: canEdit}
		return nil
	})
	if err != nil {
		return DocumentSummary{}, Version{}, err
	}
	return summary, version, nil
}
