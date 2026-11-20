package chatrecordstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3779: a hold on a conversation is placed or released in one tenant
// transaction together with its immutable audit event and outbox row. The
// transaction takes the same per-tenant audit lock as every other audit
// writer, so the event sequence stays gap free.

// holdAuditEvent assigns the next tenant sequence, event id and digest, then
// writes the audit and outbox rows for a hold change.
func holdAuditEvent(ctx context.Context, tx dbport.Tx, tenant, conversation string, event chatrecords.AuditEvent) (chatrecords.AuditEvent, error) {
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM chat_audit_event WHERE tenant_id=$1`, tenant).Scan(&event.Sequence); err != nil {
		return event, err
	}
	event.EventID = fmt.Sprintf("hold:%s:%d", event.TargetID, event.Sequence)
	event.Digest = chatrecords.DigestEvent(event)
	if _, err := tx.Exec(ctx, `INSERT INTO chat_audit_event(tenant_id,event_id,sequence,actor_id,action,target_type,target_id,prior_revision,reason,policy_evidence,at_time,digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, event.TenantID, event.EventID, event.Sequence, event.ActorID, event.Action, event.TargetType, event.TargetID, event.PriorRevision, event.Reason, event.PolicyEvidence, event.At, event.Digest); err != nil {
		return event, err
	}
	payload, err := json.Marshal(map[string]any{"audit_digest": event.Digest, "conversation_id": conversation, "ConversationID": conversation, "event_sequence": event.Sequence, "schema_version": 1, "event_type": "records.audit"})
	if err != nil {
		return event, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,'records.audit',$3)`, tenant, event.EventID, payload)
	return event, err
}

// PutHoldAudited places a hold. A hold id already placed is ErrConflict.
func (s *Store) PutHoldAudited(ctx context.Context, h chatrecords.Hold, event chatrecords.AuditEvent) (chatrecords.AuditEvent, error) {
	if h.TenantID == "" || h.HoldID == "" || h.Reason == "" || h.PlacedBy == "" || h.ConversationID == "" || event.TenantID != h.TenantID || event.Action != chatrecords.ActionHoldPlace || event.ActorID != h.PlacedBy || event.TargetID != h.HoldID || event.PolicyEvidence == "" {
		return chatrecords.AuditEvent{}, chatrecords.ErrInvalid
	}
	err := s.txTenant(ctx, h.TenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "chat-audit:"+h.TenantID); err != nil {
			return err
		}
		n, err := tx.Exec(ctx, `INSERT INTO chat_record_hold(tenant_id,hold_id,matter_ref,reason,placed_by,placed_at,conversation_id) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (tenant_id,hold_id) DO NOTHING`, h.TenantID, h.HoldID, h.MatterRef, h.Reason, h.PlacedBy, h.PlacedAt, h.ConversationID)
		if err != nil {
			return err
		}
		if n == 0 {
			return chatrecords.ErrConflict
		}
		event, err = holdAuditEvent(ctx, tx, h.TenantID, h.ConversationID, event)
		return err
	})
	if err != nil {
		return chatrecords.AuditEvent{}, err
	}
	return event, nil
}

// ReleaseHoldAudited lifts an active hold. An unknown hold is ErrHoldNotFound
// and one already released is ErrConflict, so a release is never silently
// repeated.
func (s *Store) ReleaseHoldAudited(ctx context.Context, tenant, holdID, by, reason string, at time.Time, event chatrecords.AuditEvent) (chatrecords.Hold, chatrecords.AuditEvent, error) {
	if tenant == "" || holdID == "" || by == "" || reason == "" || event.TenantID != tenant || event.Action != chatrecords.ActionHoldRelease || event.ActorID != by || event.TargetID != holdID || event.PolicyEvidence == "" {
		return chatrecords.Hold{}, chatrecords.AuditEvent{}, chatrecords.ErrInvalid
	}
	var hold chatrecords.Hold
	err := s.txTenant(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "chat-audit:"+tenant); err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `SELECT hold_id,matter_ref,reason,placed_by,placed_at,released_at,conversation_id FROM chat_record_hold WHERE tenant_id=$1 AND hold_id=$2 FOR UPDATE`, tenant, holdID)
		if err := row.Scan(&hold.HoldID, &hold.MatterRef, &hold.Reason, &hold.PlacedBy, &hold.PlacedAt, &hold.ReleasedAt, &hold.ConversationID); err != nil {
			if err == dbport.ErrNoRows {
				return chatrecords.ErrHoldNotFound
			}
			return err
		}
		if hold.ReleasedAt != nil {
			return chatrecords.ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE chat_record_hold SET released_at=$3,released_by=$4,release_reason=$5 WHERE tenant_id=$1 AND hold_id=$2 AND released_at IS NULL`, tenant, holdID, at, by, reason); err != nil {
			return err
		}
		hold.TenantID, hold.ReleasedAt, hold.ReleasedBy, hold.ReleaseReason = tenant, &at, by, reason
		var err error
		event, err = holdAuditEvent(ctx, tx, tenant, hold.ConversationID, event)
		return err
	})
	if err != nil {
		return chatrecords.Hold{}, chatrecords.AuditEvent{}, err
	}
	return hold, event, nil
}

var _ chatrecords.HoldRepository = (*Store)(nil)
