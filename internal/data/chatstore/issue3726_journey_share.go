package chatstore

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// WorkLinkJourneyShare says a journey (a promotion workflow) was shared into a
// conversation. Like every work link it holds no content: whether a reader may
// open the conversation, or the journey, is decided when the link is read.
const WorkLinkJourneyShare = "JOURNEY_SHARE"

// ListWorkLinksBySource finds the conversations one source (a journey id) is
// linked from, newest first. It is the reverse of ListWorkLinks; the caller
// must still check that the reader can open each conversation before saying
// that it exists.
func (s *Store) ListWorkLinksBySource(ctx context.Context, tenant, kind, sourceID string, limit int) ([]WorkLink, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(sourceID) == "" || (kind != WorkLinkJourneyShare && kind != WorkLinkIntentProposal) {
		return nil, chat.ErrInvalidArgument
	}
	if limit <= 0 || limit > WorkLinkListLimit {
		limit = WorkLinkListLimit
	}
	out := []WorkLink{}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id,conversation_id,kind,source_id,proposed_by_id,agent_installation_id,intent_type,linked_at FROM chat_work_link WHERE tenant_id=$1 AND kind=$2 AND source_id=$3 ORDER BY linked_at DESC, conversation_id DESC LIMIT $4`, tenant, kind, sourceID, limit)
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
