package chatstore

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3733: chat notifications are a read-time projection over posts the
// caller can already read; chat_notification_read only remembers what was read.
// Parameters shared by the queries:
//
//	$1 host company  $2 caller's home company  $3 row limit  $4 caller
//	$5 oldest a post may be
//
// A post is a notification for the caller when the caller did not write it, may
// read it (membership, history and post audience, as in the counts), has not
// muted the conversation, and it is a mention of the caller, a message in a
// direct conversation, or a reply in a thread the caller follows. A
// mentions-only choice keeps only the first. A post is read when the caller
// chose so, or the caller's read position in the conversation is past it.
const issue3733Mention = issue3787MentionsMe

var issue3733From = ` FROM chat_membership m
 JOIN chat_conversation c ON c.tenant_id=m.tenant_id AND c.id=m.conversation_id AND c.lifecycle<>'ARCHIVED'
 JOIN chat_post p ON p.tenant_id=m.tenant_id AND p.conversation_id=m.conversation_id
 LEFT JOIN chat_post root ON root.tenant_id=p.tenant_id AND root.id=p.parent_id AND p.parent_id<>''
 LEFT JOIN chat_thread_follow f ON f.tenant_id=p.tenant_id AND f.home_tenant_id=$2 AND f.member_id=$4 AND f.conversation_id=p.conversation_id AND f.root_post_id=p.parent_id AND f.followed AND p.parent_id<>''
 LEFT JOIN chat_preference pref ON pref.tenant_id=m.tenant_id AND pref.home_tenant_id=$2 AND pref.member_id=$4 AND pref.conversation_id=p.conversation_id AND pref.marker='notification'
 LEFT JOIN chat_cursor cur ON cur.tenant_id=m.tenant_id AND cur.home_tenant_id=$2 AND cur.member_id=$4 AND cur.conversation_id=p.conversation_id
 LEFT JOIN chat_notification_read nr ON nr.tenant_id=p.tenant_id AND nr.home_tenant_id=$2 AND nr.member_id=$4 AND nr.post_id=p.id
 WHERE m.tenant_id=$1 AND m.home_tenant_id=$2 AND m.member_id=$4 AND m.state='active'
 AND NOT p.tombstoned AND p.created_at>=$5
 AND NOT (p.author_id=$4 AND p.author_home_tenant_id IN ('',$2))
 AND p.body NOT LIKE chr(8291)||'member-added:%'
 AND ` + issue3787Vis("p") + issue3916PostAudience("p") + `
 AND NOT COALESCE((pref.value->>'Muted')::boolean,false)
 AND (p.references_json @> ` + issue3733Mention + ` OR c.kind='DIRECT' OR f.root_post_id IS NOT NULL)
 AND (NOT COALESCE((pref.value->>'MentionsOnly')::boolean,false) OR p.references_json @> ` + issue3733Mention + `)`

var issue3733ListQuery = `SELECT p.id,p.conversation_id,c.name,c.kind,p.sequence,p.parent_id,COALESCE(root.sequence,0),p.author_id,p.body,p.created_at,
 CASE WHEN p.references_json @> ` + issue3733Mention + ` THEN 'mention' WHEN c.kind='DIRECT' THEN 'direct' ELSE 'reply' END,
 (nr.post_id IS NOT NULL OR COALESCE(cur.last_sequence,0)>=p.sequence)` + issue3733From + `
 ORDER BY p.created_at DESC,p.id DESC LIMIT $3`

// issue3733MarkQuery adds the caller's read rows for the eligible posts the
// statement names ($6, or every one when $7). Anything else is skipped, so the
// call cannot be used to ask whether a message exists.
var issue3733MarkQuery = `INSERT INTO chat_notification_read(tenant_id,home_tenant_id,member_id,post_id)
 SELECT $1,$2,$4,p.id` + issue3733From + ` AND ($7::boolean OR p.id=ANY($6::text[]))
 AND nr.post_id IS NULL AND p.sequence>COALESCE(cur.last_sequence,0)
 ORDER BY p.created_at DESC,p.id DESC LIMIT $3
 ON CONFLICT DO NOTHING`

var _ chatrecipient.NotificationStore = (*RecipientStateStore)(nil)

// ListNotifications returns the caller's notifications, newest first. It is a
// read: it writes no read position and no read row.
func (r *RecipientStateStore) ListNotifications(ctx context.Context, host, home, subject string, since time.Time, limit int) ([]chatrecipient.Notification, error) {
	if limit <= 0 || limit > chatrecipient.NotificationsLimit {
		limit = chatrecipient.NotificationsLimit
	}
	out := []chatrecipient.Notification{}
	err := r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, issue3733ListQuery, host, home, limit, subject, since)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				n        chatrecipient.Notification
				sequence int64
				rootSeq  int64
			)
			if err := rows.Scan(&n.PostID, &n.ConversationID, &n.ConversationName, &n.ConversationKind, &sequence, &n.ThreadRootID, &rootSeq, &n.AuthorID, &n.Body, &n.At, &n.Kind, &n.Read); err != nil {
				return err
			}
			n.ID, n.Sequence, n.ThreadRootSequence = n.PostID, uint64(sequence), uint64(rootSeq)
			n.At = n.At.UTC()
			out = append(out, n)
		}
		return rows.Err()
	})
	return out, err
}

// MarkNotificationsRead stores the caller's read choice for notifications.
func (r *RecipientStateStore) MarkNotificationsRead(ctx context.Context, host, home, subject string, since time.Time, postIDs []string, all bool) (int64, error) {
	if postIDs == nil {
		postIDs = []string{}
	}
	for i := range postIDs {
		postIDs[i] = strings.TrimSpace(postIDs[i])
	}
	var marked int64
	err := r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, issue3733MarkQuery, host, home, chatrecipient.NotificationsLimit*4, subject, since, postIDs, all)
		marked = n
		return err
	})
	return marked, err
}
