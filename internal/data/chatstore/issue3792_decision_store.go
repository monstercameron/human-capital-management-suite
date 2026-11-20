package chatstore

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var _ chat.DecisionStore = (*Store)(nil)

// Issue 3792. Recorded decisions. A decision is written once and is never
// edited; a newer one replaces it by pointing back at it, in one transaction,
// and only inside the same conversation of the same tenant.

const decisionColumns = `id,tenant_id,conversation_id,decision,rationale,owner_home_tenant_id,owner_id,decided_at,source_post_id,source_sequence,supersedes_id,superseded_by_id,informal`

func decisionScan(row interface{ Scan(...any) error }) (chat.Decision, error) {
	var d chat.Decision
	err := row.Scan(&d.ID, &d.TenantID, &d.ConversationID, &d.Text, &d.Rationale, &d.OwnerHomeTenantID, &d.OwnerID, &d.DecidedAt, &d.SourcePostID, &d.SourceSequence, &d.SupersedesID, &d.SupersededByID, &d.Informal)
	if errors.Is(err, dbport.ErrNoRows) {
		return chat.Decision{}, chat.ErrNotFound
	}
	return d, err
}

// InsertDecision stores a decision once. Recording an ID that exists in the
// conversation returns what is stored, so a retry changes nothing.
func (s *Store) InsertDecision(ctx context.Context, d chat.Decision) (chat.Decision, error) {
	if d.ID == "" || d.TenantID == "" || d.ConversationID == "" || d.OwnerID == "" || d.OwnerHomeTenantID == "" {
		return chat.Decision{}, chat.ErrInvalidArgument
	}
	if d.DecidedAt.IsZero() {
		d.DecidedAt = time.Now().UTC()
	}
	var stored chat.Decision
	err := s.RunTenantTx(ctx, d.TenantID, func(tx dbport.Tx) error {
		existing, err := decisionScan(tx.QueryRow(ctx, `SELECT `+decisionColumns+` FROM chat_decision WHERE tenant_id=$1 AND id=$2 AND conversation_id=$3`, d.TenantID, d.ID, d.ConversationID))
		if err == nil {
			stored = existing
			return nil
		}
		if !errors.Is(err, chat.ErrNotFound) {
			return err
		}
		if d.SupersedesID != "" {
			var by string
			if err := tx.QueryRow(ctx, `SELECT superseded_by_id FROM chat_decision WHERE tenant_id=$1 AND id=$2 AND conversation_id=$3 FOR UPDATE`, d.TenantID, d.SupersedesID, d.ConversationID).Scan(&by); errors.Is(err, dbport.ErrNoRows) {
				return chat.ErrNotFound
			} else if err != nil {
				return err
			}
			if by != "" {
				return chat.ErrConflict
			}
		}
		stored, err = decisionScan(tx.QueryRow(ctx, `INSERT INTO chat_decision(tenant_id,id,conversation_id,decision,rationale,owner_home_tenant_id,owner_id,decided_at,source_post_id,source_sequence,supersedes_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+decisionColumns,
			d.TenantID, d.ID, d.ConversationID, d.Text, d.Rationale, d.OwnerHomeTenantID, d.OwnerID, d.DecidedAt, d.SourcePostID, d.SourceSequence, d.SupersedesID))
		if err != nil {
			return err
		}
		if d.SupersedesID != "" {
			if _, err = tx.Exec(ctx, `UPDATE chat_decision SET superseded_by_id=$4 WHERE tenant_id=$1 AND id=$2 AND conversation_id=$3 AND superseded_by_id=''`, d.TenantID, d.SupersedesID, d.ConversationID, d.ID); err != nil {
				return err
			}
		}
		return nil
	})
	return stored, err
}

// GetDecision reads one decision of a conversation.
func (s *Store) GetDecision(ctx context.Context, tenant, conversation, id string) (chat.Decision, error) {
	var d chat.Decision
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var err error
		d, err = decisionScan(tx.QueryRow(ctx, `SELECT `+decisionColumns+` FROM chat_decision WHERE tenant_id=$1 AND id=$2 AND conversation_id=$3`, tenant, id, conversation))
		return err
	})
	return d, err
}

// ListDecisions returns a conversation's decisions, newest first.
func (s *Store) ListDecisions(ctx context.Context, tenant, conversation string, since *time.Time, limit int) ([]chat.Decision, error) {
	if limit <= 0 || limit > chat.DecisionListLimit {
		limit = chat.DecisionListLimit
	}
	out := []chat.Decision{}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+decisionColumns+` FROM chat_decision WHERE tenant_id=$1 AND conversation_id=$2 AND ($3::timestamptz IS NULL OR decided_at>=$3) ORDER BY decided_at DESC, id DESC LIMIT $4`, tenant, conversation, since, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := decisionScan(rows)
			if err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}
