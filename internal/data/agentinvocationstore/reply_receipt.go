package agentinvocationstore

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ReplyReceipt retains server-issued delivery identities alongside canonical
// actor attribution. It carries no answer content or source titles.
type ReplyReceipt struct {
	TenantID              string
	InvocationID          string
	RunID                 string
	OutputID              string
	OutputDigest          string
	InvokerID             string
	ConversationID        string
	ThreadID              string
	InvokingPostID        string
	PersonaID             string
	PersonaVersion        string
	AgentID               string
	Display               string
	InvokerHandle         string
	PublicPostID          string
	EphemeralPostID       string
	PrivatePostID         string
	PrivateConversationID string
}

// RecordReplyReceipt binds delivery metadata to an already persisted sealed
// output with identical owner, source and persona identity. Replays must match.
func (s *Store) RecordReplyReceipt(ctx context.Context, receipt ReplyReceipt) error {
	tenant, err := s.resolveTenant(receipt.TenantID)
	if ctx == nil || err != nil {
		return ErrInvalid
	}
	for _, field := range []string{receipt.InvocationID, receipt.RunID, receipt.OutputID, receipt.OutputDigest, receipt.InvokerID, receipt.ConversationID, receipt.ThreadID, receipt.InvokingPostID, receipt.PersonaID, receipt.PersonaVersion, receipt.AgentID, receipt.Display, receipt.InvokerHandle} {
		if field == "" || strings.TrimSpace(field) != field {
			return ErrInvalid
		}
	}
	if (receipt.PublicPostID == "") == (receipt.EphemeralPostID == "") || (receipt.PrivatePostID == "") != (receipt.PrivateConversationID == "") {
		return ErrInvalid
	}
	payload, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	return s.db.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `INSERT INTO persona_reply_receipt (tenant_id,invocation_id,output_id,invoker_id,conversation_id,public_post_id,private_conversation_id,receipt)
		 SELECT $1,$2,$3,$4,$5,$6,$7,$8::jsonb WHERE EXISTS (SELECT 1 FROM persona_final_outputs WHERE tenant_id=$1 AND output_id=$3 AND invocation_id=$2 AND invoker_id=$4 AND conversation_id=$5 AND thread_id=$9 AND parent_post_id=$10 AND persona_id=$11 AND persona_version=$12 AND persistence_digest=$13 AND run_id=$14)
		 ON CONFLICT (tenant_id,invocation_id) DO NOTHING`, tenant, receipt.InvocationID, receipt.OutputID, receipt.InvokerID, receipt.ConversationID, receipt.PublicPostID, receipt.PrivateConversationID, string(payload), receipt.ThreadID, receipt.InvokingPostID, receipt.PersonaID, receipt.PersonaVersion, receipt.OutputDigest, receipt.RunID)
		if err != nil {
			return err
		}
		if n == 1 {
			return nil
		}
		var same bool
		if err := tx.QueryRow(ctx, `SELECT receipt=$3::jsonb FROM persona_reply_receipt WHERE tenant_id=$1 AND invocation_id=$2`, tenant, receipt.InvocationID, string(payload)).Scan(&same); err != nil {
			return ErrConflict
		}
		if !same {
			return ErrConflict
		}
		return nil
	})
}

// ListReplyReceipts returns public delivery identities in a conversation plus
// private deliveries owned by the supplied invoker. Chat still filters posts.
func (s *Store) ListReplyReceipts(ctx context.Context, tenantID, invokerID, conversationID string) ([]ReplyReceipt, error) {
	tenant, err := s.resolveTenant(tenantID)
	if ctx == nil || err != nil || invokerID == "" || conversationID == "" {
		return nil, ErrInvalid
	}
	out := make([]ReplyReceipt, 0)
	err = s.db.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT receipt FROM persona_reply_receipt WHERE tenant_id=$1 AND ((conversation_id=$2 AND public_post_id<>'') OR (invoker_id=$3 AND (conversation_id=$2 OR private_conversation_id=$2))) ORDER BY occurred_at DESC,invocation_id LIMIT 200`, tenant, conversationID, invokerID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			var receipt ReplyReceipt
			if err := json.Unmarshal(raw, &receipt); err != nil {
				return err
			}
			if receipt.TenantID != tenantID {
				return ErrConflict
			}
			out = append(out, receipt)
		}
		return rows.Err()
	})
	return out, err
}
