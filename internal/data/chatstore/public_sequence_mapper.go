package chatstore

import (
	"context"
	"math"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// ResolvePublicSequence translates a conversation's public event sequence to
// the shared chat stream offset used by durable and ephemeral events.
//
// Public sequences are scoped to one tenant and conversation, while outbox
// offsets are global to the chat database. A cursor may refer to an outbox row
// that has since been retained away, so the greatest retained public sequence
// at or before the requested sequence is used. A cursor older than the
// retained history (or ahead of the current head) resolves to the nearest
// available boundary; zero means that the caller should read from the start.
func (s *Adapter) ResolvePublicSequence(ctx context.Context, tenantID, conversationID string, publicSequence uint64) (uint64, error) {
	if s == nil || s.Store == nil || s.pool == nil || tenantID == "" || conversationID == "" || publicSequence > math.MaxInt64 {
		return 0, chat.ErrInvalidArgument
	}
	if publicSequence == 0 {
		return 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, tenantID); err != nil {
		return 0, err
	}
	var offset int64
	err = tx.QueryRow(ctx, `SELECT COALESCE((
		SELECT o.id
		FROM chat_outbox o
		WHERE o.tenant_id=$1
		  AND o.payload->>'ConversationID'=$2
		  AND (o.payload->>'EventSequence') ~ '^[0-9]+$'
		  AND (o.payload->>'EventSequence')::bigint <= $3
		ORDER BY (o.payload->>'EventSequence')::bigint DESC, o.id DESC
		LIMIT 1
	), 0)`, tenantID, conversationID, int64(publicSequence)).Scan(&offset)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return uint64(offset), nil
}
