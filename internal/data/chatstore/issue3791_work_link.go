package chatstore

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3791. The work a conversation proposed. A link only says that a
// proposal came from a conversation; it carries no status and no content, so
// whatever a reader sees of the proposal is read from its owner under that
// reader's own authority.

// WorkLinkIntentProposal is an intent an installed agent proposed from a
// conversation.
const WorkLinkIntentProposal = "INTENT_PROPOSAL"

// WorkLinkListLimit bounds one read of a conversation's links.
const WorkLinkListLimit = 100

// WorkLink ties one proposal to the conversation it came from.
type WorkLink struct {
	TenantID, ConversationID, Kind, SourceID string
	ProposedByID, AgentInstallationID        string
	IntentType                               string
	LinkedAt                                 time.Time
}

// LinkWork records a link once. Recording the same proposal again keeps the
// first record, so a retried proposal is not an error and rewrites nothing.
func (s *Store) LinkWork(ctx context.Context, l WorkLink) error {
	if l.TenantID == "" || l.ConversationID == "" || (l.Kind != WorkLinkIntentProposal && l.Kind != WorkLinkJourneyShare) || strings.TrimSpace(l.SourceID) == "" || l.ProposedByID == "" {
		return chat.ErrInvalidArgument
	}
	if l.LinkedAt.IsZero() {
		l.LinkedAt = time.Now().UTC()
	}
	return s.RunTenantTx(ctx, l.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_work_link(tenant_id,conversation_id,kind,source_id,proposed_by_id,agent_installation_id,intent_type,linked_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (tenant_id,conversation_id,kind,source_id) DO NOTHING`,
			l.TenantID, l.ConversationID, l.Kind, l.SourceID, l.ProposedByID, l.AgentInstallationID, l.IntentType, l.LinkedAt)
		return err
	})
}

// ListWorkLinks returns a conversation's links, newest first.
func (s *Store) ListWorkLinks(ctx context.Context, tenant, conversation string, limit int) ([]WorkLink, error) {
	if limit <= 0 || limit > WorkLinkListLimit {
		limit = WorkLinkListLimit
	}
	out := []WorkLink{}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id,conversation_id,kind,source_id,proposed_by_id,agent_installation_id,intent_type,linked_at FROM chat_work_link WHERE tenant_id=$1 AND conversation_id=$2 ORDER BY linked_at DESC, source_id DESC LIMIT $3`, tenant, conversation, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var l WorkLink
			if err := rows.Scan(&l.TenantID, &l.ConversationID, &l.Kind, &l.SourceID, &l.ProposedByID, &l.AgentInstallationID, &l.IntentType, &l.LinkedAt); err != nil {
				return err
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	return out, err
}
