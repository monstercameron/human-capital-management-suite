// Separate action grants for HUB-011: one row grants or denies one action
// to one subject for one document. Actions never imply each other, absence
// denies, and an explicit deny dominates any allow. Grants mutate only
// through expiry, revocation and revision bumps; rows are never deleted.
package documenthubstore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Document actions, each granted separately.
const (
	ActionRead    = "read"
	ActionHistory = "history"
	ActionPropose = "propose"
	ActionReview  = "review"
	ActionDeploy  = "deploy"
	ActionManage  = "manage"
	ActionComment = "comment"
	ActionExport  = "export"
	ActionRetire  = "retire"
)

// Grant effects; deny dominates allow.
const (
	EffectAllow = "allow"
	EffectDeny  = "deny"
)

// ErrDenied is returned when no live allow covers the action, or a live
// deny does. It never distinguishes which, so denials leak no policy.
var ErrDenied = errors.New("document grant: denied")

// GrantInput carries one grant or deny. A zero ExpiresAt never expires.
type GrantInput struct {
	DocumentID, SubjectKind, SubjectID string
	Action, Effect, Issuer, Purpose    string
	ExpiresAt                          time.Time
}

// Grant is one stored policy row.
type Grant struct {
	ID, DocumentID, SubjectKind, SubjectID string
	Action, Effect, Issuer, Purpose        string
	ExpiresAt                              time.Time
	Revision                               int64
	Revoked                                bool
}

func validAction(action string) bool {
	switch action {
	case ActionRead, ActionHistory, ActionPropose, ActionReview, ActionDeploy, ActionManage, ActionComment, ActionExport, ActionRetire:
		return true
	}
	return false
}

// GrantAction records one allow or deny for a subject and action.
func (s *Store) GrantAction(ctx context.Context, tenantID string, in GrantInput) (Grant, error) {
	var grant Grant
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var err error
		grant, err = grantActionTx(ctx, tx, tenantID, in)
		return err
	})
	if err != nil {
		return Grant{}, err
	}
	return grant, nil
}

// grantActionTx is the transaction-scoped grant insert shared by
// GrantAction and ShareDocument.
func grantActionTx(ctx context.Context, tx dbport.Tx, tenantID string, in GrantInput) (Grant, error) {
	if !validAction(in.Action) {
		return Grant{}, errors.New("document grant: unknown action")
	}
	if in.Effect != EffectAllow && in.Effect != EffectDeny {
		return Grant{}, errors.New("document grant: unknown effect")
	}
	if in.SubjectID == "" || in.Issuer == "" {
		return Grant{}, errors.New("document grant: subject and issuer are required")
	}
	if in.SubjectKind == "" {
		in.SubjectKind = "person"
	}
	grant := Grant{
		ID: "docg-" + uuid.NewString(), DocumentID: in.DocumentID,
		SubjectKind: in.SubjectKind, SubjectID: in.SubjectID,
		Action: in.Action, Effect: in.Effect, Issuer: in.Issuer, Purpose: in.Purpose,
		ExpiresAt: in.ExpiresAt, Revision: 1,
	}
	var expires any
	if !in.ExpiresAt.IsZero() {
		expires = in.ExpiresAt
	}
	_, err := tx.Exec(ctx, `INSERT INTO document_grant(id,tenant_id,document_id,subject_kind,subject_id,action,effect,issuer_id,purpose,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		grant.ID, tenantID, grant.DocumentID, grant.SubjectKind, grant.SubjectID, grant.Action, grant.Effect, grant.Issuer, grant.Purpose, expires)
	if err != nil {
		return Grant{}, err
	}
	if err := bumpPolicyEpochTx(ctx, tx, tenantID, grant.DocumentID, grant.ID, grant.SubjectKind, grant.SubjectID, grant.Action, grant.Effect, false); err != nil {
		return Grant{}, err
	}
	return grant, nil
}

// RevokeGrant closes one grant row and bumps its revision; the row stays
// for policy history.
func (s *Store) RevokeGrant(ctx context.Context, tenantID, grantID, revoker string) error {
	if revoker == "" {
		return errors.New("document grant: revoker is required")
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var docID, subjectKind, subjectID, action, effect string
		if err := tx.QueryRow(ctx, `SELECT document_id,subject_kind,subject_id,action,effect FROM document_grant WHERE tenant_id=$1 AND id=$2 AND revoked=false`, tenantID, grantID).Scan(&docID, &subjectKind, &subjectID, &action, &effect); err != nil {
			return errors.New("document grant: unknown or already revoked grant")
		}
		n, err := tx.Exec(ctx, `UPDATE document_grant SET revoked=true, revoked_at=now(), revoked_by=$1, revision=revision+1 WHERE tenant_id=$2 AND id=$3 AND revoked=false`, revoker, tenantID, grantID)
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("document grant: unknown or already revoked grant")
		}
		return bumpPolicyEpochTx(ctx, tx, tenantID, docID, grantID, subjectKind, subjectID, action, effect, true)
	})
}

// Authorize reports whether a subject may take an action on a document. A
// live deny wins over any allow; an expired or revoked row authorizes
// nothing; anything else is denied without saying which.
func (s *Store) Authorize(ctx context.Context, tenantID, docID, subjectKind, subjectID, action string) error {
	verdict := ErrDenied
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if err := authorizeTx(ctx, tx, tenantID, docID, subjectKind, subjectID, action); err != nil {
			return err
		}
		verdict = nil
		return nil
	})
	if err != nil {
		return err
	}
	return verdict
}

// authorizeTx is the transaction-scoped grant check shared by Authorize and
// Deploy, so deploys evaluate access inside their own commit.
func authorizeTx(ctx context.Context, tx dbport.Tx, tenantID, docID, subjectKind, subjectID, action string) error {
	var denies int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_grant WHERE tenant_id=$1 AND document_id=$2 AND subject_kind=$3 AND subject_id=$4 AND action=$5 AND effect='deny' AND revoked=false AND (expires_at IS NULL OR expires_at>now())`,
		tenantID, docID, subjectKind, subjectID, action).Scan(&denies); err != nil {
		return err
	}
	if denies > 0 {
		return ErrDenied
	}
	var allows int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_grant WHERE tenant_id=$1 AND document_id=$2 AND subject_kind=$3 AND subject_id=$4 AND action=$5 AND effect='allow' AND revoked=false AND (expires_at IS NULL OR expires_at>now())`,
		tenantID, docID, subjectKind, subjectID, action).Scan(&allows); err != nil {
		return err
	}
	if allows == 0 {
		return ErrDenied
	}
	return nil
}
