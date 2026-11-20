package chatstore

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3995. The record that an approver asked a requester for clarification
// about one proposal revision of a workflow case, in one private conversation.
// The question's words are not stored here, only a digest, so the record keeps
// what was asked even if the message is later edited or removed, and never
// holds more than the conversation would show its members. Rows are written
// once and kept. The caller decides who may read a row; the store scopes by
// tenant only.

// ApprovalClarificationListLimit bounds one journey's list.
const ApprovalClarificationListLimit = 50

// ApprovalClarification is one recorded request.
type ApprovalClarification struct {
	TenantID, ID, JourneyID, ProposalRevision string
	ConversationID, PostID                    string
	AskedBy, RecipientID                      string
	QuestionDigest, IdempotencyKey            string
	AskedAt                                   time.Time
}

const approvalClarificationColumns = `id,journey_id,proposal_revision,conversation_id,post_id,asked_by,recipient_id,question_digest,idempotency_key,asked_at`

func scanApprovalClarification(tenant string, row interface{ Scan(...any) error }) (ApprovalClarification, error) {
	c := ApprovalClarification{TenantID: tenant}
	err := row.Scan(&c.ID, &c.JourneyID, &c.ProposalRevision, &c.ConversationID, &c.PostID, &c.AskedBy, &c.RecipientID, &c.QuestionDigest, &c.IdempotencyKey, &c.AskedAt)
	return c, err
}

// RecordApprovalClarification keeps a request once. Recording the same
// asker and key again returns the first record and reports false, so a retry
// rewrites nothing and never creates a second request.
func (s *Store) RecordApprovalClarification(ctx context.Context, c ApprovalClarification) (ApprovalClarification, bool, error) {
	if c.TenantID == "" || strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.JourneyID) == "" || strings.TrimSpace(c.ProposalRevision) == "" ||
		strings.TrimSpace(c.ConversationID) == "" || strings.TrimSpace(c.AskedBy) == "" || strings.TrimSpace(c.RecipientID) == "" ||
		c.AskedBy == c.RecipientID || strings.TrimSpace(c.QuestionDigest) == "" || strings.TrimSpace(c.IdempotencyKey) == "" {
		return ApprovalClarification{}, false, chat.ErrInvalidArgument
	}
	if c.AskedAt.IsZero() {
		c.AskedAt = time.Now().UTC()
	}
	var out ApprovalClarification
	created := false
	err := s.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `INSERT INTO chat_approval_clarification(tenant_id,`+approvalClarificationColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (tenant_id,asked_by,idempotency_key) DO NOTHING`,
			c.TenantID, c.ID, c.JourneyID, c.ProposalRevision, c.ConversationID, c.PostID, c.AskedBy, c.RecipientID, c.QuestionDigest, c.IdempotencyKey, c.AskedAt)
		if err != nil {
			return err
		}
		created = n == 1
		out, err = scanApprovalClarification(c.TenantID, tx.QueryRow(ctx, `SELECT `+approvalClarificationColumns+` FROM chat_approval_clarification WHERE tenant_id=$1 AND asked_by=$2 AND idempotency_key=$3`, c.TenantID, c.AskedBy, c.IdempotencyKey))
		return err
	})
	return out, created, err
}

// ListApprovalClarifications returns the requests recorded for one journey in
// which the person is the asker or the recipient, newest first. Anyone else's
// requests are never returned.
func (s *Store) ListApprovalClarifications(ctx context.Context, tenant, journeyID, person string, limit int) ([]ApprovalClarification, error) {
	if tenant == "" || strings.TrimSpace(journeyID) == "" || strings.TrimSpace(person) == "" {
		return nil, chat.ErrInvalidArgument
	}
	if limit <= 0 || limit > ApprovalClarificationListLimit {
		limit = ApprovalClarificationListLimit
	}
	var out []ApprovalClarification
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+approvalClarificationColumns+` FROM chat_approval_clarification WHERE tenant_id=$1 AND journey_id=$2 AND (asked_by=$3 OR recipient_id=$3) ORDER BY asked_at DESC, id LIMIT $4`, tenant, journeyID, person, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanApprovalClarification(tenant, rows)
			if err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}
