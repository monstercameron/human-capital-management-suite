package chatstore

import (
	"context"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func moderationAudit(ctx context.Context, tx dbport.Tx, p chat.Principal, t, cid, id, action, reason string, revision uint64, at time.Time) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "chat-audit:"+t); err != nil {
		return err
	}
	var sequence uint64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM chat_audit_event WHERE tenant_id=$1`, t).Scan(&sequence); err != nil {
		return err
	}
	e := chatrecords.AuditEvent{TenantID: t, EventID: fmt.Sprintf("moderation:%s:%d", id, sequence), Sequence: sequence, ActorID: p.SubjectID, Action: action, TargetType: "chat_post", TargetID: id, PriorRevision: revision, Reason: reason, PolicyEvidence: "moderation:" + cid + ":" + p.TenantID, At: at}
	_, err := tx.Exec(ctx, `INSERT INTO chat_audit_event(tenant_id,event_id,sequence,actor_id,action,target_type,target_id,prior_revision,reason,policy_evidence,at_time,digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, e.TenantID, e.EventID, e.Sequence, e.ActorID, e.Action, e.TargetType, e.TargetID, e.PriorRevision, e.Reason, e.PolicyEvidence, e.At, chatrecords.DigestEvent(e))
	return err
}
