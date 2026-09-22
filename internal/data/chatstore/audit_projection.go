package chatstore

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// recordChatEvent persists body-free governance metadata in the mutation's
// transaction. A failed projection therefore rolls back the canonical write.
func recordChatEvent(ctx context.Context, tx dbport.Tx, tenantID, conversationID, aggregateID, eventType, actorHome, actorID, targetID, recordID, kind, sourceID string, revision uint64, policyRevision int64, payload []byte) error {
	var outboxID int64
	var at time.Time
	if err := tx.QueryRow(ctx, `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,$3,$4) RETURNING id,created_at`, tenantID, aggregateID, eventType, payload).Scan(&outboxID, &at); err != nil {
		return err
	}
	// All audit writers for a tenant serialize sequence assignment. The
	// outbox id makes replay identity stable without storing message content.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "chat-audit:"+tenantID); err != nil {
		return err
	}
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM chat_audit_event WHERE tenant_id=$1`, tenantID).Scan(&sequence); err != nil {
		return err
	}
	prior := int64(0)
	if revision > 0 {
		prior = int64(revision - 1)
	}
	eventID := fmt.Sprintf("chat-outbox:%d", outboxID)
	evidence := fmt.Sprintf("chat:%s:%d:actor-home:%s", conversationID, policyRevision, actorHome)
	e := chatrecords.AuditEvent{TenantID: tenantID, EventID: eventID, Sequence: uint64(sequence), ActorID: actorID, Action: eventType, TargetType: "chat", TargetID: targetID, PriorRevision: uint64(prior), Reason: eventType, PolicyEvidence: evidence, At: at}
	if _, err := tx.Exec(ctx, `INSERT INTO chat_audit_event(tenant_id,event_id,sequence,actor_id,action,target_type,target_id,prior_revision,reason,policy_evidence,at_time,digest) VALUES($1,$2,$3,$4,$5,'chat',$6,$7,$8,$9,$10,$11)`, tenantID, eventID, sequence, actorID, eventType, targetID, prior, eventType, evidence, at, chatrecords.DigestEvent(e)); err != nil {
		return err
	}
	if kind == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO chat_record_inventory(tenant_id,record_id,conversation_id,kind,source_id,revision,created_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,record_id) DO UPDATE SET revision=GREATEST(chat_record_inventory.revision,EXCLUDED.revision),kind=CASE WHEN EXCLUDED.revision >= chat_record_inventory.revision THEN EXCLUDED.kind ELSE chat_record_inventory.kind END`, tenantID, recordID, conversationID, kind, sourceID, revision, at)
	return err
}

// RecordAppEvent projects an authorized app mutation inside the caller's transaction.
func RecordAppEvent(ctx context.Context, tx dbport.Tx, tenant, conversation, appID, action, actorHome, actorID string, revision uint64, policyRevision int64) error {
	payload := []byte(`{}`) // App manifests and granted scopes do not belong in audit metadata.
	return recordChatEvent(ctx, tx, tenant, conversation, appID, action, actorHome, actorID, appID, "app:"+appID, string(chatrecords.KindAppChange), appID, revision, policyRevision, payload)
}
