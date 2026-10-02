package agentinvocationstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// UnfinishedInvocation is one claimed invocation that has no final outcome
// recorded anywhere: either no run and no failure row, or a run that is still
// READY, RUNNING, WAITING or RECONCILING. A failure row does not finish an
// invocation that has a run, because the person's card follows the run: a post
// failure recorded while its run is left running changes nothing they see. It
// carries only the facts recovery decides on; completed and failed runs never
// appear here.
type UnfinishedInvocation struct {
	InvocationID   string
	PostID         string
	ConversationID string
	ThreadID       string
	InvokerID      string
	PersonaID      string
	CreatedAt      time.Time

	// RequestID is empty when the claim never reached admission.
	RequestID string
	// Decision is the admission decision; empty without an admission.
	Decision string
	// Deadline is the admission deadline; zero without an admission.
	Deadline time.Time

	// RunState is empty when admission produced no run.
	RunState    string
	RunRevision uint64
	LeaseUntil  time.Time
	// ModelCallStarted is true when a model checkpoint exists or a model step
	// was ever begun under the run's security lease. Either means a model call
	// may have been paid for.
	ModelCallStarted bool
}

// ListUnfinished returns up to limit unfinished invocations of one tenant
// created at or after since, oldest first. The run is joined by the same source
// key digest the admission store uses, so each invocation sees its own run even
// when one post mentioned several agents.
func (s *Store) ListUnfinished(ctx context.Context, tenant string, since time.Time, limit int) ([]UnfinishedInvocation, error) {
	tid, err := s.resolveTenant(tenant)
	if ctx == nil || err != nil || since.IsZero() || limit < 1 || limit > 1000 {
		return nil, ErrInvalid
	}
	out := make([]UnfinishedInvocation, 0)
	err = s.db.RunTenantTx(ctx, tid, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT i.invocation_id,i.post_id,i.conversation_id,i.thread_id,i.invoker_id,i.persona_id,i.created_at,
				r.request_id,r.decision,r.deadline,e.state,e.revision,e.lease_until,
				(e.run_id IS NOT NULL AND (
				   EXISTS (SELECT 1 FROM agent_run_checkpoint c WHERE c.tenant_id=e.tenant_id AND c.run_id=e.run_id AND c.phase='MODEL_CALL')
				   OR EXISTS (SELECT 1 FROM persona_security_lease l JOIN persona_security_step st ON st.tenant_id=l.tenant_id AND st.lease_id=l.lease_id
				              WHERE l.tenant_id=e.tenant_id AND l.run_id=e.run_id AND st.step_id LIKE 'persona-model-%')))
			FROM persona_invocations i
			LEFT JOIN agent_run_request r ON r.tenant_id=i.tenant_id AND r.source_kind='PERSONA_MENTION'
				AND r.source_key_digest=encode(sha256(convert_to(i.invocation_id,'UTF8')),'hex')
			LEFT JOIN agent_run_execution e ON e.tenant_id=r.tenant_id AND e.admission_id=r.request_id
			WHERE i.tenant_id=$1 AND i.created_at>=$2
			  AND ((e.state IS NULL AND NOT EXISTS (SELECT 1 FROM persona_invocation_failure f WHERE f.tenant_id=i.tenant_id AND f.invoker_id=i.invoker_id AND f.post_id=i.post_id))
			       OR e.state IN ('READY','RUNNING','WAITING','RECONCILING'))
			ORDER BY i.created_at,i.invocation_id COLLATE "C" LIMIT $3`, tid, since.UTC(), limit)
		if err != nil {
			return fmt.Errorf("agentinvocationstore: list unfinished: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var item UnfinishedInvocation
			var requestID, decision, state *string
			var deadline, lease *time.Time
			var revision *int64
			if err := rows.Scan(&item.InvocationID, &item.PostID, &item.ConversationID, &item.ThreadID, &item.InvokerID, &item.PersonaID, &item.CreatedAt,
				&requestID, &decision, &deadline, &state, &revision, &lease, &item.ModelCallStarted); err != nil {
				return fmt.Errorf("agentinvocationstore: scan unfinished: %w", err)
			}
			item.CreatedAt = item.CreatedAt.UTC()
			if requestID != nil {
				item.RequestID = *requestID
			}
			if decision != nil {
				item.Decision = *decision
			}
			if deadline != nil {
				item.Deadline = deadline.UTC()
			}
			if state != nil {
				item.RunState = *state
			}
			if revision != nil {
				item.RunRevision = uint64(*revision)
			}
			if lease != nil {
				item.LeaseUntil = lease.UTC()
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}

// FinishUnfinished records the one final outcome of an invocation that has no
// run to carry it. The row is append-only and keyed by the post, so every
// process that finishes the same invocation writes the same single row.
func (s *Store) FinishUnfinished(ctx context.Context, tenant string, item UnfinishedInvocation, code string, retryable bool) error {
	if strings.TrimSpace(item.InvocationID) == "" {
		return ErrInvalid
	}
	return s.RecordPostFailure(ctx, PostFailure{TenantID: tenant, InvokerID: item.InvokerID, ConversationID: item.ConversationID, ThreadID: item.ThreadID, PostID: item.PostID, Code: code, Retryable: retryable})
}
