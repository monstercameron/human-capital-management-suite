package chatstore

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PostReferenceDocument marks a message that names a document in its text.
const PostReferenceDocument = "DOCUMENT"

// PostReferenceListLimit bounds one read of the messages that name a target.
const PostReferenceListLimit = 200

// PostReference says that one message names one target. It carries no text:
// the caller must still check that the reader may open the conversation and
// the message before saying that either exists.
type PostReference struct {
	TenantID, PostID, ConversationID, Kind, TargetID string
	PostedAt                                         time.Time
}

// ListPostReferencesByTarget returns the messages that name a target, newest
// first. The index is kept current by the database as messages are posted,
// edited, removed and restored, so a removed message is never returned.
func (s *Store) ListPostReferencesByTarget(ctx context.Context, tenant, kind, targetID string, limit int) ([]PostReference, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(targetID) == "" || kind != PostReferenceDocument {
		return nil, chat.ErrInvalidArgument
	}
	if limit <= 0 || limit > PostReferenceListLimit {
		limit = PostReferenceListLimit
	}
	out := []PostReference{}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id,post_id,conversation_id,target_kind,target_id,posted_at FROM chat_post_reference WHERE tenant_id=$1 AND target_kind=$2 AND target_id=$3 ORDER BY posted_at DESC, post_id DESC LIMIT $4`, tenant, kind, targetID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r PostReference
			if err := rows.Scan(&r.TenantID, &r.PostID, &r.ConversationID, &r.Kind, &r.TargetID, &r.PostedAt); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}
