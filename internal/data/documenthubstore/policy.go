// Policy epoch for HUB-016: every grant mutation advances the document's
// epoch and publishes the invalidation event in the same commit. Derived
// consumers compare epochs to drop stale reads, caches and index entries;
// no revoke is ever lost between the policy row and its event.
package documenthubstore

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PolicyEpoch returns the document's current policy epoch.
func (s *Store) PolicyEpoch(ctx context.Context, tenantID, docID string) (uint64, error) {
	var epoch uint64
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT policy_epoch FROM document WHERE tenant_id=$1 AND id=$2`, tenantID, docID).Scan(&epoch)
	})
	if err != nil {
		return 0, err
	}
	return epoch, nil
}

// bumpPolicyEpochTx advances the epoch and publishes the grant.revised
// invalidation event carrying the grant's new state.
func bumpPolicyEpochTx(ctx context.Context, tx dbport.Tx, tenantID, docID, grantID, subjectKind, subjectID, action, effect string, revoked bool) error {
	var epoch uint64
	if err := tx.QueryRow(ctx, `UPDATE document SET policy_epoch=policy_epoch+1 WHERE tenant_id=$1 AND id=$2 RETURNING policy_epoch`, tenantID, docID).Scan(&epoch); err != nil {
		return err
	}
	raw, err := json.Marshal(map[string]string{
		"action": action, "document_id": docID, "effect": effect,
		"epoch": strconv.FormatUint(epoch, 10), "grant_id": grantID, "revoked": strconv.FormatBool(revoked),
		"subject_id": subjectID, "subject_kind": subjectKind, "tenant_id": tenantID,
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO document_outbox(tenant_id,aggregate_id,event_type,payload,created_at) VALUES($1,$2,'grant.revised',$3,clock_timestamp())`, tenantID, docID, string(raw))
	return err
}
