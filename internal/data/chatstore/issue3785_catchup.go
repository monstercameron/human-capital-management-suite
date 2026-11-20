package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3785: the range a "Catch me up" brief reads. It is a read-only
// projection over posts the reader may read right now, in one transaction, so
// the head, the count and the posts agree with one another. Parameters shared
// by every query below:
//
//	$1 host company  $2 reader's home company  $3 conversation  $4 reader
//	$5 thread root ('' for the whole conversation)  $6 range starts after this sequence
//
// The reader's read position is only read, never written: producing a brief
// is not reading the conversation.

var issue3785ScopeSQL = issue3785ScopeFor()

func issue3785ScopeFor() string {
	return ` FROM chat_membership m
 JOIN chat_post p ON p.tenant_id=m.tenant_id AND p.conversation_id=m.conversation_id
 WHERE m.tenant_id=$1 AND m.home_tenant_id=$2 AND m.conversation_id=$3 AND m.member_id=$4 AND m.state='active'
 AND NOT p.tombstoned AND p.body NOT LIKE chr(8291)||'member-added:%'
 AND ` + issue3787Vis("p") + issue3916PostAudience("p") + `
 AND ($5='' OR p.id=$5 OR p.parent_id=$5)`
}

var (
	issue3785Member = `SELECT c.name FROM chat_membership m JOIN chat_conversation c ON c.tenant_id=m.tenant_id AND c.id=m.conversation_id
 WHERE m.tenant_id=$1 AND m.home_tenant_id=$2 AND m.conversation_id=$3 AND m.member_id=$4 AND m.state='active'`
	issue3785Cursor = `SELECT last_sequence FROM chat_cursor WHERE tenant_id=$1 AND home_tenant_id=$2 AND conversation_id=$3 AND member_id=$4`
	// The anchor and the thread root are looked up under the same visibility as
	// the range itself, so an unreadable post is "not found", never "forbidden".
	issue3785Anchor = `SELECT p.sequence` + issue3785ScopeSQL + ` AND p.id=$6`
	issue3785Root   = `SELECT p.sequence` + issue3785ScopeSQL + ` AND p.id=$5 AND p.parent_id=''`
	issue3785Count  = `SELECT count(*) FILTER (WHERE p.sequence>$6),COALESCE(max(p.sequence),0)` + issue3785ScopeSQL
	issue3785Posts  = `SELECT p.id,p.sequence,p.author_id,p.body,p.parent_id,p.created_at,p.references_json,(p.references_json @> ` + issue3787MentionsMe + `)` +
		issue3785ScopeSQL + ` AND p.sequence>$6 ORDER BY p.sequence DESC LIMIT $7`
)

var _ chatrecipient.CatchUpStore = (*RecipientStateStore)(nil)

// CatchUpRange reads what the reader missed. The membership is checked first
// and a reader who is not an active member gets a refusal that says nothing
// about the conversation.
func (r *RecipientStateStore) CatchUpRange(ctx context.Context, host, home, subject, conversation, threadRootID, anchorPostID string, limit int) (chatrecipient.CatchUpRange, error) {
	var out chatrecipient.CatchUpRange
	if host == "" || home == "" || subject == "" || conversation == "" {
		return out, chat.ErrInvalidArgument
	}
	if limit <= 0 || limit > chatrecipient.CatchUpLimit {
		limit = chatrecipient.CatchUpLimit
	}
	err := r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		out = chatrecipient.CatchUpRange{}
		if err := tx.QueryRow(ctx, issue3785Member, host, home, conversation, subject).Scan(&out.ConversationName); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return chat.ErrPermissionDenied
			}
			return err
		}
		var cursor int64
		if err := tx.QueryRow(ctx, issue3785Cursor, host, home, conversation, subject).Scan(&cursor); err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		out.Cursor = uint64(max(cursor, 0))
		var sequence int64
		if threadRootID != "" {
			if err := tx.QueryRow(ctx, issue3785Root, host, home, conversation, subject, threadRootID).Scan(&sequence); err != nil {
				if errors.Is(err, dbport.ErrNoRows) {
					return chat.ErrNotFound
				}
				return err
			}
		}
		after := int64(0)
		switch {
		case anchorPostID != "":
			if err := tx.QueryRow(ctx, issue3785Anchor, host, home, conversation, subject, threadRootID, anchorPostID).Scan(&sequence); err != nil {
				if errors.Is(err, dbport.ErrNoRows) {
					return chat.ErrNotFound
				}
				return err
			}
			after = sequence - 1
		case threadRootID == "":
			// A conversation has a read position; a thread has none, so a thread
			// without an anchor is read from its root.
			after = cursor
		}
		out.After = uint64(max(after, 0))
		var total, head int64
		if err := tx.QueryRow(ctx, issue3785Count, host, home, conversation, subject, threadRootID, after).Scan(&total, &head); err != nil {
			return err
		}
		out.Total, out.Head = int(total), uint64(max(head, 0))
		rows, err := tx.Query(ctx, issue3785Posts, host, home, conversation, subject, threadRootID, after, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				post     chatrecipient.CatchUpPost
				seq      int64
				at       time.Time
				refs     []byte
				mentions bool
			)
			if err := rows.Scan(&post.ID, &seq, &post.AuthorID, &post.Body, &post.ThreadRootID, &at, &refs, &mentions); err != nil {
				return err
			}
			post.Sequence, post.At, post.Mentioned = uint64(max(seq, 0)), at.UTC(), mentions
			if len(refs) > 0 {
				if err := json.Unmarshal(refs, &post.Refs); err != nil {
					return err
				}
			}
			out.Posts = append(out.Posts, post)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		// Newest were read; the brief is told oldest first.
		for i, j := 0, len(out.Posts)-1; i < j; i, j = i+1, j-1 {
			out.Posts[i], out.Posts[j] = out.Posts[j], out.Posts[i]
		}
		return nil
	})
	if err != nil {
		return chatrecipient.CatchUpRange{}, err
	}
	return out, nil
}
