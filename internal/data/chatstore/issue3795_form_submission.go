package chatstore

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3795. The answer to a form card, kept once. The row holds the values a
// person gave, so it is read only through a caller that has already decided
// the reader is the person who answered or the agent that asked; the message
// list reads only whether a card was answered.

// FormSubmissionBatch bounds one status read.
const FormSubmissionBatch = 100

// FormSubmission is one answered form card.
type FormSubmission struct {
	TenantID, ConversationID, PostID string
	SubmittedByID, RequestedByID     string
	Answers                          map[string]string
	SubmittedAt                      time.Time
}

// RecordFormSubmission writes the answer to a card. It reports false, and
// changes nothing, when the card was already answered: a card takes one answer.
func (s *Store) RecordFormSubmission(ctx context.Context, f FormSubmission) (bool, error) {
	if f.TenantID == "" || f.ConversationID == "" || f.PostID == "" || strings.TrimSpace(f.SubmittedByID) == "" || strings.TrimSpace(f.RequestedByID) == "" || f.Answers == nil {
		return false, chat.ErrInvalidArgument
	}
	if f.SubmittedAt.IsZero() {
		f.SubmittedAt = time.Now().UTC()
	}
	raw, err := json.Marshal(f.Answers)
	if err != nil {
		return false, chat.ErrInvalidArgument
	}
	created := false
	err = s.RunTenantTx(ctx, f.TenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `INSERT INTO chat_form_submission(tenant_id,conversation_id,post_id,submitted_by_id,requested_by_id,answers,submitted_at) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7) ON CONFLICT (tenant_id,conversation_id,post_id) DO NOTHING`,
			f.TenantID, f.ConversationID, f.PostID, f.SubmittedByID, f.RequestedByID, string(raw), f.SubmittedAt)
		if err != nil {
			return err
		}
		created = n == 1
		return nil
	})
	return created, err
}

// GetFormSubmission returns the answer to a card, or false when it has none.
func (s *Store) GetFormSubmission(ctx context.Context, tenant, conversation, post string) (FormSubmission, bool, error) {
	var out FormSubmission
	found := false
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id,conversation_id,post_id,submitted_by_id,requested_by_id,answers::text,submitted_at FROM chat_form_submission WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3`, tenant, conversation, post)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return rows.Err()
		}
		var raw string
		if err := rows.Scan(&out.TenantID, &out.ConversationID, &out.PostID, &out.SubmittedByID, &out.RequestedByID, &raw, &out.SubmittedAt); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(raw), &out.Answers); err != nil {
			return err
		}
		found = true
		return nil
	})
	return out, found, err
}

// FormSubmittedAt says which of the given cards were answered, and when. It
// reads no answer.
func (s *Store) FormSubmittedAt(ctx context.Context, tenant, conversation string, posts []string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	if len(posts) == 0 {
		return out, nil
	}
	if len(posts) > FormSubmissionBatch {
		return nil, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT post_id,submitted_at FROM chat_form_submission WHERE tenant_id=$1 AND conversation_id=$2 AND post_id = ANY($3)`, tenant, conversation, posts)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var at time.Time
			if err := rows.Scan(&id, &at); err != nil {
				return err
			}
			out[id] = at
		}
		return rows.Err()
	})
	return out, err
}
