package agentinvocationstore

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// OutcomeInvocation is the part of one invocation that a conversation's
// outcome receipts need: who asked, under which post, and which agent. It holds
// no grant, skill scope or actor chain.
type OutcomeInvocation struct {
	ID             string
	TenantID       string
	ConversationID string
	ThreadID       string
	PostID         string
	InvokerID      string
	PersonaID      string
	PersonaVersion string
}

// OutcomeRefusal is a question the agent could not take, kept without the
// reason: a refusal's reason is about the asker's own access.
type OutcomeRefusal struct {
	TenantID       string
	InvokerID      string
	ConversationID string
	ThreadID       string
	PostID         string
}

// outcomeListLimit bounds a conversation's receipts to its newest runs.
const outcomeListLimit = 50

// ListConversationOutcomeInvocations returns the newest invocations of every
// invoker in one conversation (#3803). It is a tenant-scoped read for a caller
// that has already established the reader's membership of the conversation; it
// shows no more than the identities a receipt names.
func (s *Store) ListConversationOutcomeInvocations(ctx context.Context, tenant, conversation string) ([]OutcomeInvocation, error) {
	tid, err := s.resolveTenant(tenant)
	if ctx == nil || err != nil || strings.TrimSpace(conversation) == "" || conversation != strings.TrimSpace(conversation) {
		return nil, ErrInvalid
	}
	out := make([]OutcomeInvocation, 0)
	err = s.db.RunTenantTx(ctx, tid, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT invocation_id,conversation_id,thread_id,post_id,invoker_id,persona_id,persona_version
			FROM persona_invocations WHERE tenant_id=$1 AND conversation_id=$2
			ORDER BY created_at DESC,invocation_id COLLATE "C" LIMIT $3`, tid, conversation, outcomeListLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item := OutcomeInvocation{TenantID: tenant}
			if err := rows.Scan(&item.ID, &item.ConversationID, &item.ThreadID, &item.PostID, &item.InvokerID, &item.PersonaID, &item.PersonaVersion); err != nil {
				return err
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}

// ListConversationRefusals returns the questions of one conversation that an
// agent refused at admission, for any asker. No other failure code is listed:
// the rest are one person's own operating state.
func (s *Store) ListConversationRefusals(ctx context.Context, tenant, conversation string) ([]OutcomeRefusal, error) {
	tid, err := s.resolveTenant(tenant)
	if ctx == nil || err != nil || strings.TrimSpace(conversation) == "" || conversation != strings.TrimSpace(conversation) {
		return nil, ErrInvalid
	}
	out := make([]OutcomeRefusal, 0)
	err = s.db.RunTenantTx(ctx, tid, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT invoker_id,conversation_id,thread_id,post_id FROM persona_invocation_failure
			WHERE tenant_id=$1 AND conversation_id=$2 AND failure_code='ADMISSION_REFUSED'
			ORDER BY occurred_at DESC,post_id LIMIT $3`, tid, conversation, outcomeListLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item := OutcomeRefusal{TenantID: tenant}
			if err := rows.Scan(&item.InvokerID, &item.ConversationID, &item.ThreadID, &item.PostID); err != nil {
				return err
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}
