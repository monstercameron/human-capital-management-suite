// Ownership transfer after departure (HUB-040). Custody of a document must
// be able to move to a successor custodian when an owner or team leaves,
// without leaving official documentation unreviewable and without
// silently widening a private document's audience to whoever picks it up.
// TransferOwnership records the successor as the new document.owner_id,
// bootstraps them with the owner action set only where they lack it, never
// touches any other subject's existing grants, and appends an immutable
// evidence row so custody history is never lost.
package documenthubstore

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	// ErrTransferInput is returned when a transfer omits its successor or
	// its reason.
	ErrTransferInput = errors.New("document ownership: successor and reason are required")
	// ErrTransferSameOwner is returned when the named successor already
	// owns the document.
	ErrTransferSameOwner = errors.New("document ownership: successor already owns the document")
)

// TransferInput names one ownership transfer. The actor must be the
// current owner or hold MANAGE.
type TransferInput struct {
	DocumentID, SuccessorOwnerID, ActorID, Reason string
}

// OwnershipTransfer is one immutable custody-change record.
type OwnershipTransfer struct {
	ID, DocumentID, PriorOwnerID, SuccessorOwnerID, Reason, TransferredBy string
	CreatedAt                                                             time.Time
}

// TransferOwnership moves custody of a document to a successor, preserving
// every existing grant and appending an audit record; it refuses a
// disposed document and a successor who already owns it.
func (s *Store) TransferOwnership(ctx context.Context, tenantID string, in TransferInput) (OwnershipTransfer, error) {
	if in.SuccessorOwnerID == "" || in.Reason == "" {
		return OwnershipTransfer{}, ErrTransferInput
	}
	var out OwnershipTransfer
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var owner, lifecycle string
		if err := tx.QueryRow(ctx, `SELECT owner_id,lifecycle FROM document WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, in.DocumentID).Scan(&owner, &lifecycle); err != nil {
			return ErrDenied
		}
		if lifecycle == "DISPOSED" {
			return ErrDenied
		}
		if owner != in.ActorID {
			if err := authorizeTx(ctx, tx, tenantID, in.DocumentID, "person", in.ActorID, ActionManage); err != nil {
				return err
			}
		}
		if owner == in.SuccessorOwnerID {
			return ErrTransferSameOwner
		}
		if _, err := tx.Exec(ctx, `UPDATE document SET owner_id=$1 WHERE tenant_id=$2 AND id=$3`, in.SuccessorOwnerID, tenantID, in.DocumentID); err != nil {
			return err
		}
		// Bootstrap the successor with any owner action they lack, without
		// touching anyone else's grants: a transfer installs a working
		// custodian, it never widens who else can already see the document.
		for _, action := range ownerActions {
			var already int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM document_grant WHERE tenant_id=$1 AND document_id=$2 AND subject_kind='person' AND subject_id=$3 AND action=$4 AND effect='allow' AND revoked=false AND (expires_at IS NULL OR expires_at>now())`,
				tenantID, in.DocumentID, in.SuccessorOwnerID, action).Scan(&already); err != nil {
				return err
			}
			if already > 0 {
				continue
			}
			if _, err := grantActionTx(ctx, tx, tenantID, GrantInput{DocumentID: in.DocumentID, SubjectKind: "person", SubjectID: in.SuccessorOwnerID, Action: action, Effect: EffectAllow, Issuer: in.ActorID, Purpose: "ownership_transfer"}); err != nil {
				return err
			}
		}
		out = OwnershipTransfer{
			ID: "docxo-" + uuid.NewString(), DocumentID: in.DocumentID, PriorOwnerID: owner,
			SuccessorOwnerID: in.SuccessorOwnerID, Reason: in.Reason, TransferredBy: in.ActorID,
		}
		_, err := tx.Exec(ctx, `INSERT INTO document_ownership_transfer(id,tenant_id,document_id,prior_owner_id,successor_owner_id,reason,transferred_by) VALUES($1,$2,$3,$4,$5,$6,$7)`,
			out.ID, tenantID, out.DocumentID, out.PriorOwnerID, out.SuccessorOwnerID, out.Reason, out.TransferredBy)
		return err
	})
	if err != nil {
		return OwnershipTransfer{}, err
	}
	return out, nil
}

// CurrentCustodian returns the live owner of one document.
func (s *Store) CurrentCustodian(ctx context.Context, tenantID, docID string) (string, error) {
	var owner string
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT owner_id FROM document WHERE tenant_id=$1 AND id=$2`, tenantID, docID).Scan(&owner)
	})
	if err != nil {
		return "", ErrDenied
	}
	return owner, nil
}

// TransferHistory lists ownership transfers for one document, newest
// first; history is append-only and never rewritten by a later transfer.
func (s *Store) TransferHistory(ctx context.Context, tenantID, docID string) ([]OwnershipTransfer, error) {
	var out []OwnershipTransfer
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,document_id,prior_owner_id,successor_owner_id,reason,transferred_by,created_at FROM document_ownership_transfer WHERE tenant_id=$1 AND document_id=$2 ORDER BY created_at DESC,id DESC`, tenantID, docID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t OwnershipTransfer
			if err := rows.Scan(&t.ID, &t.DocumentID, &t.PriorOwnerID, &t.SuccessorOwnerID, &t.Reason, &t.TransferredBy, &t.CreatedAt); err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
