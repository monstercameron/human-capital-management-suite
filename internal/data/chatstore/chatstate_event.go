package chatstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// chatstateEvent keeps the command's reason in the immutable audit event as
// well as in the status/system-line payload. All writes share the status tx.
func chatstateEvent(ctx context.Context, tx dbport.Tx, status chat.ChannelStatus, actorHome string) error {
	var sequence int64
	if err := tx.QueryRow(ctx, `UPDATE chat_conversation SET event_sequence=event_sequence+1 WHERE tenant_id=$1 AND id=$2 RETURNING event_sequence`, status.TenantID, status.ConversationID).Scan(&sequence); err != nil {
		return err
	}
	value, err := json.Marshal(status)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(OutboxEnvelope{ConversationID: status.ConversationID, ActorID: status.ChangedBy, ActorHomeTenantID: actorHome, TargetID: status.ConversationID, Revision: status.Revision, PolicyRevision: int64(status.Revision), EventSequence: sequence, SchemaVersion: OutboxSchemaVersion, CorrelationID: uuid.NewString(), Value: value})
	if err != nil {
		return err
	}
	var id int64
	var at time.Time
	if err = tx.QueryRow(ctx, `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,'conversation.status_changed',$3) RETURNING id,created_at`, status.TenantID, status.ConversationID, payload).Scan(&id, &at); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "chat-audit:"+status.TenantID); err != nil {
		return err
	}
	var auditSequence uint64
	if err = tx.QueryRow(ctx, `SELECT coalesce(max(sequence),0)+1 FROM chat_audit_event WHERE tenant_id=$1`, status.TenantID).Scan(&auditSequence); err != nil {
		return err
	}
	event := chatrecords.AuditEvent{TenantID: status.TenantID, EventID: fmt.Sprintf("chat-outbox:%d", id), Sequence: auditSequence, ActorID: status.ChangedBy, Action: "conversation.status_changed", TargetType: "chat", TargetID: status.ConversationID, PriorRevision: status.Revision - 1, Reason: status.Reason, PolicyEvidence: fmt.Sprintf("chat:%s:%d:actor-home:%s", status.ConversationID, status.Revision, actorHome), At: at}
	if _, err = tx.Exec(ctx, `INSERT INTO chat_audit_event(tenant_id,event_id,sequence,actor_id,action,target_type,target_id,prior_revision,reason,policy_evidence,at_time,digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, event.TenantID, event.EventID, event.Sequence, event.ActorID, event.Action, event.TargetType, event.TargetID, event.PriorRevision, event.Reason, event.PolicyEvidence, event.At, chatrecords.DigestEvent(event)); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_record_inventory(tenant_id,record_id,conversation_id,kind,source_id,revision,created_at) VALUES($1,$2,$3,'CONVERSATION',$3,$4,$5) ON CONFLICT(tenant_id,record_id) DO UPDATE SET revision=greatest(chat_record_inventory.revision,EXCLUDED.revision)`, status.TenantID, "conversation:"+status.ConversationID, status.ConversationID, status.Revision, at)
	return err
}
