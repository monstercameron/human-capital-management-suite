package chatstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// HasOpenReport says whether this person already has a report about this
// message that no moderator has decided. The report path asks it first, so the
// same person pressing Report again does not add another item to the queue:
// the queue is cut at a fixed length, and a person who could add items without
// limit could push everybody else's out of it.
func (s *ModeratedAdapter) HasOpenReport(ctx context.Context, p chat.Principal, t, cid, id string) (bool, error) {
	var found bool
	err := s.removalTx(ctx, t, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_moderation_report WHERE tenant_id=$1 AND conversation_id=$2 AND target_id=$3 AND reporter_id=$4 AND COALESCE(NULLIF(reporter_home_tenant_id,''),tenant_id)=$5 AND state='OPEN' AND reason<>'appeal')`, t, cid, id, p.SubjectID, p.TenantID).Scan(&found)
	})
	return found, err
}
