package chatstore

import (
	"context"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// ListReactionsForPosts reads the reactions of several posts of one
// conversation in one query (CHATBUG-014: opening a conversation asked for them
// post by post). It applies what ListReactions applies to each post: the post
// is in the conversation and not tombstoned, the principal is an active member,
// and the post is inside the principal's history window. Each post contributes
// at most perPost reactions, in ListReactions's order.
func (s *Adapter) ListReactionsForPosts(ctx context.Context, principal chat.Principal, t, cid string, postIDs []string, perPost uint32) ([]chat.Reaction, error) {
	if len(postIDs) > chat.ReactionBatchLimit || perPost == 0 || perPost > 200 {
		return nil, chat.ErrInvalidArgument
	}
	if len(postIDs) == 0 {
		return nil, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, t); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT post_id,home_tenant_id,member_id,emoji,created_at FROM (
 SELECT r.post_id,r.home_tenant_id,r.member_id,r.emoji,r.created_at,
 row_number() OVER (PARTITION BY r.post_id ORDER BY r.emoji,r.home_tenant_id,r.member_id) AS place
 FROM chat_reaction r
 JOIN chat_post p ON p.tenant_id=r.tenant_id AND p.id=r.post_id AND p.conversation_id=$2 AND p.tombstoned=false
 JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$4 AND m.member_id=$5 AND m.state='active'
 WHERE r.tenant_id=$1 AND r.post_id=ANY($3::text[])
 AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at))
 ) bounded WHERE place<=$6 ORDER BY post_id,emoji,home_tenant_id,member_id`, t, cid, postIDs, principal.TenantID, principal.SubjectID, int(perPost))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []chat.Reaction
	for rows.Next() {
		x := chat.Reaction{TenantID: t, ConversationID: cid}
		if err = rows.Scan(&x.PostID, &x.HomeTenantID, &x.SubjectID, &x.Emoji, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, tx.Commit(ctx)
}

var _ chat.ReactionBatchStore = (*Adapter)(nil)
