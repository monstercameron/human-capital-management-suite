// Private-by-default sharing for HUB-012: a new personal document is
// visible only to its owner, who receives every action at creation.
// Sharing is an explicit, audited act by the owner or a manager, and the
// audience preview shows exactly whom a share reaches. A copied link or a
// known version ID grants nothing: every read still passes Authorize.
package documenthubstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ownerActions is the full action set bootstrapped to the owner at
// creation, so private defaults never lock the owner out.
var ownerActions = []string{ActionRead, ActionHistory, ActionPropose, ActionReview, ActionDeploy, ActionManage, ActionComment, ActionExport, ActionRetire}

// bootstrapOwner grants the owner every action with the owner as issuer.
// The id carries the document so two documents by one owner never share
// grant rows.
func bootstrapOwnerTx(ctx context.Context, tx dbport.Tx, tenantID, docID, ownerID string) error {
	for _, action := range ownerActions {
		if _, err := tx.Exec(ctx, `INSERT INTO document_grant(id,tenant_id,document_id,subject_kind,subject_id,action,effect,issuer_id,purpose) VALUES('docg-owner-'||$3||'-'||$4,$1,$3,'person',$2,$4,'allow',$2,'owner')`,
			tenantID, ownerID, docID, action); err != nil {
			return err
		}
	}
	return nil
}

// ShareDocument records one share when the actor is the owner or holds the
// manage action; the issuer on the row is always the actor.
func (s *Store) ShareDocument(ctx context.Context, tenantID, docID, actorID string, in GrantInput) (Grant, error) {
	in.DocumentID = docID
	in.Issuer = actorID
	if in.SubjectKind == "" {
		in.SubjectKind = "person"
	}
	var grant Grant
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var owner string
		if err := tx.QueryRow(ctx, `SELECT owner_id FROM document WHERE tenant_id=$1 AND id=$2`, tenantID, docID).Scan(&owner); err != nil {
			return ErrDenied
		}
		if err := authorizeTx(ctx, tx, tenantID, docID, "person", actorID, ActionManage); err != nil {
			return err
		}
		var err error
		grant, err = grantActionTx(ctx, tx, tenantID, in)
		return err
	})
	if err != nil {
		return Grant{}, err
	}
	return grant, nil
}

// SharePersonalDocument grants one person access to the current published
// version. The first owner share publishes the latest personal candidate in
// the same transaction; later drafts stay private until separately published.
func (s *Store) SharePersonalDocument(ctx context.Context, tenantID, docID, ownerID, recipientID string) error {
	recipientID = strings.TrimSpace(recipientID)
	if recipientID == "" || recipientID == ownerID {
		return errors.New("document share: a distinct recipient is required")
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var owner, home, lifecycle string
		if err := tx.QueryRow(ctx, `SELECT owner_id,home,lifecycle FROM document WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, docID).Scan(&owner, &home, &lifecycle); err != nil {
			return ErrDenied
		}
		if owner != ownerID || home != "PERSONAL" || lifecycle == "DISPOSED" {
			return ErrDenied
		}
		for _, action := range []string{ActionRead, ActionManage, ActionDeploy} {
			if err := authorizeTx(ctx, tx, tenantID, docID, "person", ownerID, action); err != nil {
				return err
			}
		}
		var recipientDenies int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_grant WHERE tenant_id=$1 AND document_id=$2 AND subject_kind='person' AND subject_id=$3 AND action='read' AND effect='deny' AND revoked=false AND (expires_at IS NULL OR expires_at>now())`, tenantID, docID, recipientID).Scan(&recipientDenies); err != nil {
			return err
		}
		if recipientDenies > 0 {
			return ErrDenied
		}
		var liveVersion string
		err := tx.QueryRow(ctx, `SELECT version_id FROM document_active_pointer WHERE tenant_id=$1 AND document_id=$2 AND scope_kind='default' AND scope_id=''`, tenantID, docID).Scan(&liveVersion)
		if errors.Is(err, dbport.ErrNoRows) {
			var versionID string
			if err := tx.QueryRow(ctx, `SELECT id FROM document_version WHERE tenant_id=$1 AND document_id=$2 AND status='candidate' ORDER BY created_at DESC,id DESC LIMIT 1`, tenantID, docID).Scan(&versionID); err != nil {
				return ErrDenied
			}
			deploymentID := "docd-" + uuid.NewString()
			if _, err := tx.Exec(ctx, `INSERT INTO document_deployment(id,tenant_id,document_id,version_id,scope_kind,scope_id,deployer_id) VALUES($1,$2,$3,$4,'default','',$5)`, deploymentID, tenantID, docID, versionID, ownerID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO document_active_pointer(tenant_id,document_id,scope_kind,scope_id,deployment_id,version_id) VALUES($1,$2,'default','',$3,$4)`, tenantID, docID, deploymentID, versionID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE document_version SET status='deployed' WHERE tenant_id=$1 AND document_id=$2 AND id=$3`, tenantID, docID, versionID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO document_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,'deployment.published',jsonb_build_object('document_id',$2::text,'version_id',$3::text,'deployment_id',$4::text,'deployer_id',$5::text,'scope_kind','default','scope_id',''))`, tenantID, docID, versionID, deploymentID, ownerID); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		for _, action := range []string{ActionRead, ActionComment} {
			if _, err := grantActionTx(ctx, tx, tenantID, GrantInput{DocumentID: docID, SubjectKind: "person", SubjectID: recipientID, Action: action, Effect: EffectAllow, Issuer: ownerID, Purpose: "owner_share"}); err != nil {
				return err
			}
		}
		return nil
	})
}

// Audience returns the live (unrevoked) grant rows for a document to its
// owner or a manager; anyone else is denied without learning the audience.
func (s *Store) Audience(ctx context.Context, tenantID, docID, actorID string) ([]Grant, error) {
	var audience []Grant
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var owner string
		if err := tx.QueryRow(ctx, `SELECT owner_id FROM document WHERE tenant_id=$1 AND id=$2`, tenantID, docID).Scan(&owner); err != nil {
			return ErrDenied
		}
		if owner != actorID {
			if err := authorizeTx(ctx, tx, tenantID, docID, "person", actorID, ActionManage); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `SELECT id,document_id,subject_kind,subject_id,action,effect,issuer_id,purpose,expires_at,revision,revoked FROM document_grant WHERE tenant_id=$1 AND document_id=$2 AND revoked=false ORDER BY subject_kind,subject_id,action`, tenantID, docID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var g Grant
			var expires sql.NullTime
			if err := rows.Scan(&g.ID, &g.DocumentID, &g.SubjectKind, &g.SubjectID, &g.Action, &g.Effect, &g.Issuer, &g.Purpose, &expires, &g.Revision, &g.Revoked); err != nil {
				return err
			}
			if expires.Valid {
				g.ExpiresAt = expires.Time
			}
			audience = append(audience, g)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return audience, nil
}
