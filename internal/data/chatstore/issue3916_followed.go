package chatstore

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// issue3916PostAudience hides a post addressed to other people only ($4 is the
// caller), the same rule the follow lookup applies to the root.
func issue3916PostAudience(alias string) string {
	return ` AND NOT EXISTS (SELECT 1 FROM chat_post_audience au WHERE au.tenant_id=` + alias + `.tenant_id AND au.conversation_id=` + alias + `.conversation_id AND au.post_id=` + alias + `.id AND au.restricted AND au.author_id<>$4 AND NOT ($4=ANY(au.member_ids)))`
}

// ListFollowed returns the caller's followed threads in the host company, most
// recently active first. A thread is listed only while the caller is an active
// member who may still read its root message; reply counts and the activity
// time use only replies the caller may read. This is a read: it writes no read
// cursor and no follow row.
func (r *RecipientStateStore) ListFollowed(ctx context.Context, host, home, subject string, limit int) ([]chatrecipient.FollowedThread, error) {
	if limit <= 0 || limit > chatrecipient.FollowedThreadsLimit {
		limit = chatrecipient.FollowedThreadsLimit
	}
	out := []chatrecipient.FollowedThread{}
	err := r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT f.conversation_id,f.root_post_id,f.revision,p.sequence,p.author_id,p.body,p.tombstoned,p.created_at,COALESCE(r.replies,0),COALESCE(r.last_reply,p.created_at)
        FROM chat_thread_follow f
        JOIN chat_membership m ON m.tenant_id=f.tenant_id AND m.conversation_id=f.conversation_id AND m.home_tenant_id=f.home_tenant_id AND m.member_id=f.member_id AND m.state='active'
        JOIN chat_post p ON p.tenant_id=f.tenant_id AND p.conversation_id=f.conversation_id AND p.id=f.root_post_id AND p.parent_id=''
        LEFT JOIN LATERAL (SELECT count(*) AS replies,max(c.created_at) AS last_reply FROM chat_post c
            WHERE c.tenant_id=p.tenant_id AND c.conversation_id=p.conversation_id AND c.parent_id=p.id AND NOT c.tombstoned
            AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND c.created_at>=m.joined_at))`+issue3916PostAudience("c")+`) r ON true
        WHERE f.tenant_id=$1 AND f.home_tenant_id=$2 AND f.member_id=$4 AND f.followed
        AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at))`+issue3916PostAudience("p")+`
        ORDER BY COALESCE(r.last_reply,p.created_at) DESC,f.root_post_id DESC LIMIT $3`, host, home, limit, subject)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				row       chatrecipient.FollowedThread
				revision  int64
				sequence  int64
				replies   int64
				removed   bool
				body      string
				createdAt time.Time
			)
			if err := rows.Scan(&row.ConversationID, &row.RootPostID, &revision, &sequence, &row.RootAuthorID, &body, &removed, &createdAt, &replies, &row.LastActivity); err != nil {
				return err
			}
			row.Revision, row.RootSequence, row.ReplyCount, row.RootRemoved = uint64(revision), uint64(sequence), uint64(replies), removed
			if !removed {
				row.RootBody = body
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	return out, err
}
