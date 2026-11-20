package chatstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatbarrier"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3783: the workspace's information barriers. Every statement runs in the
// tenant transaction (row-level security backs the tenant predicate), and a
// change is written to the audit log in the same transaction, so a rule never
// changes without a record of who changed it and when.

// MaxBarrierRules bounds the rules one workspace keeps, so a barrier check
// stays a small in-memory scan.
const MaxBarrierRules = 200

// ListBarrierRules returns the rules in force for the tenant, oldest first.
func (s *Store) ListBarrierRules(ctx context.Context, tenantID string) (chatbarrier.Rules, error) {
	if tenantID == "" {
		return nil, chat.ErrInvalidArgument
	}
	out := chatbarrier.Rules{}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,group_a,group_b,label_a,label_b,created_by,created_at FROM chat_information_barrier WHERE tenant_id=$1 AND revoked_at IS NULL ORDER BY created_at,id LIMIT $2`, tenantID, MaxBarrierRules)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r chatbarrier.Rule
			if err := rows.Scan(&r.ID, &r.GroupA, &r.GroupB, &r.LabelA, &r.LabelB, &r.CreatedBy, &r.CreatedAt); err != nil {
				return err
			}
			r.CreatedAt = r.CreatedAt.UTC()
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// AddBarrierRule stores a new rule. A pair that already has a rule in force is
// chat.ErrAlreadyExists; a workspace at its limit is chat.ErrConflict.
func (s *Store) AddBarrierRule(ctx context.Context, tenantID string, rule chatbarrier.Rule) (chatbarrier.Rule, error) {
	if tenantID == "" || rule.ID == "" || rule.GroupA == "" || rule.GroupB == "" || rule.GroupA >= rule.GroupB || rule.CreatedBy == "" {
		return chatbarrier.Rule{}, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var held, same int
		if err := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE group_a=$2 AND group_b=$3) FROM chat_information_barrier WHERE tenant_id=$1 AND revoked_at IS NULL`, tenantID, rule.GroupA, rule.GroupB).Scan(&held, &same); err != nil {
			return err
		}
		if same > 0 {
			return chat.ErrAlreadyExists
		}
		if held >= MaxBarrierRules {
			return chat.ErrConflict
		}
		if err := tx.QueryRow(ctx, `INSERT INTO chat_information_barrier(tenant_id,id,group_a,group_b,label_a,label_b,created_by) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING created_at`,
			tenantID, rule.ID, rule.GroupA, rule.GroupB, rule.LabelA, rule.LabelB, rule.CreatedBy).Scan(&rule.CreatedAt); err != nil {
			return err
		}
		return barrierAudit(ctx, tx, tenantID, rule.CreatedBy, rule.ID, "chat.barrier.add", rule.LabelA+" | "+rule.LabelB)
	})
	if err != nil {
		return chatbarrier.Rule{}, err
	}
	rule.CreatedAt = rule.CreatedAt.UTC()
	return rule, nil
}

// RemoveBarrierRule revokes a rule in force. A rule that is not in force, or
// belongs to another workspace, is chat.ErrNotFound.
func (s *Store) RemoveBarrierRule(ctx context.Context, tenantID, id, actor string) error {
	if tenantID == "" || id == "" || actor == "" {
		return chat.ErrInvalidArgument
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var a, b string
		err := tx.QueryRow(ctx, `UPDATE chat_information_barrier SET revoked_at=now(), revoked_by=$3 WHERE tenant_id=$1 AND id=$2 AND revoked_at IS NULL RETURNING label_a,label_b`, tenantID, id, actor).Scan(&a, &b)
		if errors.Is(err, dbport.ErrNoRows) {
			return chat.ErrNotFound
		}
		if err != nil {
			return err
		}
		return barrierAudit(ctx, tx, tenantID, actor, id, "chat.barrier.remove", a+" | "+b)
	})
}

// barrierAudit appends the change to the workspace's audit log, serialised with
// the other audit writers of the tenant so the sequence has no gaps.
func barrierAudit(ctx context.Context, tx dbport.Tx, tenantID, actor, id, action, reason string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "chat-audit:"+tenantID); err != nil {
		return err
	}
	var sequence uint64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM chat_audit_event WHERE tenant_id=$1`, tenantID).Scan(&sequence); err != nil {
		return err
	}
	e := chatrecords.AuditEvent{TenantID: tenantID, EventID: fmt.Sprintf("barrier:%s:%d", id, sequence), Sequence: sequence, ActorID: actor, Action: action, TargetType: "chat_barrier", TargetID: id, Reason: reason, PolicyEvidence: "barrier:" + tenantID, At: time.Now().UTC()}
	_, err := tx.Exec(ctx, `INSERT INTO chat_audit_event(tenant_id,event_id,sequence,actor_id,action,target_type,target_id,prior_revision,reason,policy_evidence,at_time,digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, e.TenantID, e.EventID, e.Sequence, e.ActorID, e.Action, e.TargetType, e.TargetID, e.PriorRevision, e.Reason, e.PolicyEvidence, e.At, chatrecords.DigestEvent(e))
	return err
}
