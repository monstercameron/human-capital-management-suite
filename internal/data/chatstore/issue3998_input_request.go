package chatstore

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3998. The record that a reviewer asked for informal input about one
// proposal revision of a workflow case, in one conversation. Only a digest of
// the question is kept, never its words. Rows are written once and kept, and a
// reviewer reads only their own. Nothing in the workflow reads these rows: they
// can never stand in for an approval.

// InputRequestListLimit bounds one journey's list.
const InputRequestListLimit = 50

// InputRequest is one recorded request for informal input.
type InputRequest struct {
	TenantID, ID, JourneyID, ProposalRevision string
	ConversationID, PostID, AskedBy           string
	AudienceKind                              string
	AudienceSize                              int
	SharedCaseName                            bool
	QuestionDigest, IdempotencyKey            string
	AskedAt                                   time.Time
}

const inputRequestColumns = `id,journey_id,proposal_revision,conversation_id,post_id,asked_by,audience_kind,audience_size,shared_case_name,question_digest,idempotency_key,asked_at`

func scanInputRequest(tenant string, row interface{ Scan(...any) error }) (InputRequest, error) {
	r := InputRequest{TenantID: tenant}
	err := row.Scan(&r.ID, &r.JourneyID, &r.ProposalRevision, &r.ConversationID, &r.PostID, &r.AskedBy, &r.AudienceKind, &r.AudienceSize, &r.SharedCaseName, &r.QuestionDigest, &r.IdempotencyKey, &r.AskedAt)
	return r, err
}

// RecordInputRequest keeps a request once. Recording the same asker and key
// again returns the first record and reports false.
func (s *Store) RecordInputRequest(ctx context.Context, r InputRequest) (InputRequest, bool, error) {
	switch r.AudienceKind {
	case "direct", "group", "private_channel", "public_channel":
	default:
		return InputRequest{}, false, chat.ErrInvalidArgument
	}
	if r.TenantID == "" || strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.JourneyID) == "" || strings.TrimSpace(r.ProposalRevision) == "" ||
		strings.TrimSpace(r.ConversationID) == "" || strings.TrimSpace(r.AskedBy) == "" || r.AudienceSize < 0 ||
		strings.TrimSpace(r.QuestionDigest) == "" || strings.TrimSpace(r.IdempotencyKey) == "" {
		return InputRequest{}, false, chat.ErrInvalidArgument
	}
	if r.AskedAt.IsZero() {
		r.AskedAt = time.Now().UTC()
	}
	var out InputRequest
	created := false
	err := s.RunTenantTx(ctx, r.TenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `INSERT INTO chat_input_request(tenant_id,`+inputRequestColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT (tenant_id,asked_by,idempotency_key) DO NOTHING`,
			r.TenantID, r.ID, r.JourneyID, r.ProposalRevision, r.ConversationID, r.PostID, r.AskedBy, r.AudienceKind, r.AudienceSize, r.SharedCaseName, r.QuestionDigest, r.IdempotencyKey, r.AskedAt)
		if err != nil {
			return err
		}
		created = n == 1
		out, err = scanInputRequest(r.TenantID, tx.QueryRow(ctx, `SELECT `+inputRequestColumns+` FROM chat_input_request WHERE tenant_id=$1 AND asked_by=$2 AND idempotency_key=$3`, r.TenantID, r.AskedBy, r.IdempotencyKey))
		return err
	})
	return out, created, err
}

// ListInputRequests returns the requests one person made about one journey,
// newest first. Anyone else's requests are never returned.
func (s *Store) ListInputRequests(ctx context.Context, tenant, journeyID, asker string, limit int) ([]InputRequest, error) {
	if tenant == "" || strings.TrimSpace(journeyID) == "" || strings.TrimSpace(asker) == "" {
		return nil, chat.ErrInvalidArgument
	}
	if limit <= 0 || limit > InputRequestListLimit {
		limit = InputRequestListLimit
	}
	var out []InputRequest
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+inputRequestColumns+` FROM chat_input_request WHERE tenant_id=$1 AND journey_id=$2 AND asked_by=$3 ORDER BY asked_at DESC, id LIMIT $4`, tenant, journeyID, asker, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanInputRequest(tenant, rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}
