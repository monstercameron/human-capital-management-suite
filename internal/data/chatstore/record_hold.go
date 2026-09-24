package chatstore

import (
	"context"
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PlaceRecordHold is the records-policy port CHAT-026 preserves deleted post
// content through. It is deliberately independent of the chatrecords Go
// package (chat keeps its own database and migrations per CHAT-003); it
// writes the same chat_record_hold / chat_record_inventory rows that
// recordChatEvent already maintains for every governance event, so a hold
// placed here is visible to the same tenant-scoped inventory a records
// operator inspects.
//
// Placing a hold does not touch ordinary chat state: it only marks the named
// records as held so a later delete preserves their pre-deletion body in the
// immutable chat_post_revision ledger instead of discarding it. Everyone
// without this hold still gets an unrecoverable delete.
func (s *Adapter) PlaceRecordHold(ctx context.Context, tenantID, holdID, matterRef, reason, placedBy string, recordIDs []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, tenantID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO chat_record_hold(tenant_id,hold_id,matter_ref,reason,placed_by,placed_at) VALUES($1,$2,$3,$4,$5,now()) ON CONFLICT (tenant_id,hold_id) DO NOTHING`, tenantID, holdID, matterRef, reason, placedBy); err != nil {
		return err
	}
	for _, recordID := range recordIDs {
		if err = attachHold(ctx, tx, tenantID, recordID, holdID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// attachHold appends holdID to the named record's hold_ids set, creating the
// inventory row first if the record has not yet produced a governance event.
// It is idempotent: attaching the same hold twice leaves the set unchanged.
func attachHold(ctx context.Context, tx dbport.Tx, tenantID, recordID, holdID string) error {
	var current []byte
	err := tx.QueryRow(ctx, `SELECT hold_ids FROM chat_record_inventory WHERE tenant_id=$1 AND record_id=$2 FOR UPDATE`, tenantID, recordID).Scan(&current)
	if err != nil {
		if err != dbport.ErrNoRows {
			return err
		}
		holds, merr := json.Marshal([]string{holdID})
		if merr != nil {
			return merr
		}
		_, err = tx.Exec(ctx, `INSERT INTO chat_record_inventory(tenant_id,record_id,conversation_id,kind,source_id,revision,created_at,hold_ids) VALUES($1,$2,'','','',0,now(),$3)`, tenantID, recordID, holds)
		return err
	}
	var ids []string
	if len(current) > 0 {
		if err = json.Unmarshal(current, &ids); err != nil {
			return err
		}
	}
	for _, id := range ids {
		if id == holdID {
			return nil
		}
	}
	ids = append(ids, holdID)
	holds, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE chat_record_inventory SET hold_ids=$3 WHERE tenant_id=$1 AND record_id=$2`, tenantID, recordID, holds)
	return err
}

// recordHeld reports whether recordID currently carries at least one
// unreleased (active) hold. It runs inside the caller's transaction so a
// delete and its hold check see the same snapshot.
func recordHeld(ctx context.Context, tx dbport.Tx, tenantID, recordID string) (bool, error) {
	var held bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM chat_record_inventory inv
		JOIN chat_record_hold h ON h.tenant_id = inv.tenant_id
			AND h.hold_id IN (SELECT jsonb_array_elements_text(inv.hold_ids))
		WHERE inv.tenant_id=$1 AND inv.record_id=$2 AND h.released_at IS NULL
	)`, tenantID, recordID).Scan(&held)
	return held, err
}
