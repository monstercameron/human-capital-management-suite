package chatrecordstore

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ListAudit reads the tenant's audit log newest first (#3780). Every filter is
// a bound parameter; the action filter matches an action or a whole family and
// never uses LIKE, so a "%" typed by a reader is only a character. A
// conversation matches an event that targets it, targets a record the
// inventory places in it, or was announced for it on the outbox.
func (s *Store) ListAudit(ctx context.Context, t string, q chatrecords.AuditQuery) ([]chatrecords.AuditEvent, error) {
	if t == "" {
		return nil, chatrecords.ErrInvalid
	}
	limit := clampPageLimit(q.Limit)
	var from, to any
	if !q.From.IsZero() {
		from = q.From.UTC()
	}
	if !q.To.IsZero() {
		to = q.To.UTC()
	}
	var out []chatrecords.AuditEvent
	err := s.txTenant(ctx, t, func(tx dbport.Tx) error {
		rows, e := tx.Query(ctx, `SELECT e.event_id,e.sequence,e.actor_id,e.action,e.target_type,e.target_id,e.prior_revision,e.reason,e.policy_evidence,e.at_time,e.digest
FROM chat_audit_event e
WHERE e.tenant_id=$1
 AND ($2::text='' OR e.actor_id=$2)
 AND ($3::text='' OR e.action=$3 OR left(e.action,length($3)+1)=$3||'.')
 AND ($4::timestamptz IS NULL OR e.at_time>=$4)
 AND ($5::timestamptz IS NULL OR e.at_time<$5)
 AND ($6::bigint=0 OR e.sequence<$6)
 AND ($7::text='' OR e.target_id=$7
  OR EXISTS(SELECT 1 FROM chat_record_inventory i WHERE i.tenant_id=e.tenant_id AND i.conversation_id=$7 AND (i.record_id=e.target_id OR i.source_id=e.target_id))
  OR EXISTS(SELECT 1 FROM chat_outbox o WHERE o.tenant_id=e.tenant_id AND o.aggregate_id=e.event_id AND o.event_type='records.audit' AND o.payload->>'conversation_id'=$7))
ORDER BY e.sequence DESC LIMIT $8`, t, q.ActorID, q.Action, from, to, int64(q.Before), q.ConversationID, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var x chatrecords.AuditEvent
			var at time.Time
			if e = rows.Scan(&x.EventID, &x.Sequence, &x.ActorID, &x.Action, &x.TargetType, &x.TargetID, &x.PriorRevision, &x.Reason, &x.PolicyEvidence, &at, &x.Digest); e != nil {
				return e
			}
			x.TenantID, x.At = t, at
			out = append(out, x)
		}
		return rows.Err()
	})
	return out, err
}

var _ chatrecords.AuditReadRepository = (*Store)(nil)
