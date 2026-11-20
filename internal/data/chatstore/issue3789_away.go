package chatstore

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3789: the away setting lives in chat_away, one row per person and
// company, written with a revision check like quiet hours. The row grants
// nothing: it is read for display and for optional cover notices only.

var _ chatrecipient.AwayStore = (*RecipientStateStore)(nil)

// Away returns the person's setting. A person with none is not away, at revision 1.
func (r *RecipientStateStore) Away(ctx context.Context, home, subject string) (chatrecipient.Away, error) {
	out := chatrecipient.Away{Revision: 1}
	err := r.store.RunTenantTx(ctx, home, func(tx dbport.Tx) error {
		var since, until *time.Time
		var rev int64
		err := tx.QueryRow(ctx, `SELECT away_since,away_until,cover_member_id,hidden,notify_cover,revision FROM chat_away WHERE tenant_id=$1 AND member_id=$2`, home, subject).
			Scan(&since, &until, &out.CoverID, &out.Hidden, &out.NotifyCover, &rev)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if since != nil {
			out.Since = since.UTC()
		}
		if until != nil {
			out.Until = until.UTC()
		}
		out.Revision = uint64(rev)
		return nil
	})
	return out, err
}

// PutAway stores the setting when expected is the revision the caller read; any
// other revision is a conflict. A cover must be a person with a place in this
// company's chat, so the setting cannot be used to probe other identities.
func (r *RecipientStateStore) PutAway(ctx context.Context, home, subject string, a chatrecipient.Away, expected uint64) (chatrecipient.Away, error) {
	var since, until any
	if !a.Until.IsZero() {
		since, until = a.Since.UTC(), a.Until.UTC()
	}
	out := a
	err := r.store.RunTenantTx(ctx, home, func(tx dbport.Tx) error {
		if a.CoverID != "" {
			var one int
			err := tx.QueryRow(ctx, `SELECT 1 FROM chat_membership WHERE tenant_id=$1 AND home_tenant_id=$1 AND member_id=$2 AND state='active' LIMIT 1`, home, a.CoverID).Scan(&one)
			if errors.Is(err, dbport.ErrNoRows) {
				return chat.ErrInvalidArgument
			}
			if err != nil {
				return err
			}
		}
		var rev int64
		err := tx.QueryRow(ctx, `WITH updated AS (
            UPDATE chat_away SET away_since=$3,away_until=$4,cover_member_id=$5,hidden=$6,notify_cover=$7,revision=revision+1,updated_at=now()
            WHERE tenant_id=$1 AND member_id=$2 AND revision=$8 RETURNING revision
        ), inserted AS (
            INSERT INTO chat_away(tenant_id,member_id,away_since,away_until,cover_member_id,hidden,notify_cover,revision)
            SELECT $1,$2,$3,$4,$5,$6,$7,2 WHERE $8=1 AND NOT EXISTS (SELECT 1 FROM updated)
            ON CONFLICT DO NOTHING RETURNING revision
        ) SELECT revision FROM updated UNION ALL SELECT revision FROM inserted`,
			home, subject, since, until, a.CoverID, a.Hidden, a.NotifyCover, expected).Scan(&rev)
		if errors.Is(err, dbport.ErrNoRows) {
			return chat.ErrConflict
		}
		if err != nil {
			return err
		}
		out.Revision = uint64(rev)
		return nil
	})
	return out, err
}

// AwayOf returns the running, unhidden absences among the named people.
func (r *RecipientStateStore) AwayOf(ctx context.Context, home string, subjects []string, now time.Time) ([]chatrecipient.AwayPublic, error) {
	out := []chatrecipient.AwayPublic{}
	err := r.store.RunTenantTx(ctx, home, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT member_id,away_until,cover_member_id FROM chat_away
 WHERE tenant_id=$1 AND member_id=ANY($2::text[]) AND NOT hidden AND away_until>$3 ORDER BY member_id`, home, subjects, now.UTC())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a chatrecipient.AwayPublic
			if err := rows.Scan(&a.MemberID, &a.Until, &a.CoverID); err != nil {
				return err
			}
			a.Until = a.Until.UTC()
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

// coverCharges reads the people who named cover as their cover with notices on
// and an absence still running, as parallel lists of person and the moment their
// absence began.
func (r *RecipientStateStore) coverCharges(ctx context.Context, home, cover string, now time.Time) (ids, sinces []string, err error) {
	ids, sinces = []string{}, []string{}
	err = r.store.RunTenantTx(ctx, home, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT member_id,away_since FROM chat_away
 WHERE tenant_id=$1 AND cover_member_id=$2 AND notify_cover AND away_until>$3 ORDER BY member_id`, home, cover, now.UTC())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var since time.Time
			if err := rows.Scan(&id, &since); err != nil {
				return err
			}
			ids, sinces = append(ids, id), append(sinces, since.UTC().Format(time.RFC3339Nano))
		}
		return rows.Err()
	})
	return ids, sinces, err
}

// Parameters of the cover queries:
//
//	$1 host company  $2 cover's home company  $3 row limit  $4 cover
//	$5 oldest a post may be  $6 people who are away  $7 when each absence began
//
// A post is a cover notice when it mentions someone who is away after their
// absence began, the cover did not write it, and the cover may read it
// (membership, history and post audience) and has not muted the conversation.
var issue3789CoverFrom = ` FROM chat_membership m
 JOIN chat_conversation c ON c.tenant_id=m.tenant_id AND c.id=m.conversation_id AND c.lifecycle<>'ARCHIVED'
 JOIN chat_post p ON p.tenant_id=m.tenant_id AND p.conversation_id=m.conversation_id
 JOIN unnest($6::text[],$7::text[]) AS a(id,since) ON p.created_at>=a.since::timestamptz
   AND p.references_json @> jsonb_build_array(jsonb_build_object('Kind','PERSON_MENTION','TenantID',$2::text,'ID',a.id))
 LEFT JOIN chat_post root ON root.tenant_id=p.tenant_id AND root.id=p.parent_id AND p.parent_id<>''
 LEFT JOIN chat_preference pref ON pref.tenant_id=m.tenant_id AND pref.home_tenant_id=$2 AND pref.member_id=$4 AND pref.conversation_id=p.conversation_id AND pref.marker='notification'
 LEFT JOIN chat_cursor cur ON cur.tenant_id=m.tenant_id AND cur.home_tenant_id=$2 AND cur.member_id=$4 AND cur.conversation_id=p.conversation_id
 LEFT JOIN chat_notification_read nr ON nr.tenant_id=p.tenant_id AND nr.home_tenant_id=$2 AND nr.member_id=$4 AND nr.post_id=p.id
 WHERE m.tenant_id=$1 AND m.home_tenant_id=$2 AND m.member_id=$4 AND m.state='active'
 AND NOT p.tombstoned AND p.created_at>=$5
 AND NOT (p.author_id=$4 AND p.author_home_tenant_id IN ('',$2))
 AND p.body NOT LIKE chr(8291)||'member-added:%'
 AND ` + issue3787Vis("p") + issue3916PostAudience("p") + `
 AND NOT COALESCE((pref.value->>'Muted')::boolean,false)`

// The list never selects the message body: a cover learns where and who, not what.
var issue3789CoverListQuery = `SELECT p.id,p.conversation_id,c.name,c.kind,p.sequence,p.parent_id,COALESCE(root.sequence,0),p.author_id,a.id,p.created_at,
 (nr.post_id IS NOT NULL OR COALESCE(cur.last_sequence,0)>=p.sequence)` + issue3789CoverFrom + `
 ORDER BY p.created_at DESC,p.id DESC LIMIT $3`

var issue3789CoverMarkQuery = `INSERT INTO chat_notification_read(tenant_id,home_tenant_id,member_id,post_id)
 SELECT $1,$2,$4,p.id` + issue3789CoverFrom + ` AND ($9::boolean OR p.id=ANY($8::text[]))
 AND nr.post_id IS NULL AND p.sequence>COALESCE(cur.last_sequence,0)
 ORDER BY p.created_at DESC,p.id DESC LIMIT $3
 ON CONFLICT DO NOTHING`

// CoverNotices lists the mentions the cover may be told about. It is a read.
func (r *RecipientStateStore) CoverNotices(ctx context.Context, host, home, cover string, since, now time.Time, limit int) ([]chatrecipient.Notification, error) {
	if limit <= 0 || limit > chatrecipient.CoverLimit {
		limit = chatrecipient.CoverLimit
	}
	out := []chatrecipient.Notification{}
	ids, sinces, err := r.coverCharges(ctx, home, cover, now)
	if err != nil || len(ids) == 0 {
		return out, err
	}
	err = r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, issue3789CoverListQuery, host, home, limit*2, cover, since.UTC(), ids, sinces)
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := map[string]bool{}
		for rows.Next() {
			var n chatrecipient.Notification
			var sequence, rootSeq int64
			if err := rows.Scan(&n.PostID, &n.ConversationID, &n.ConversationName, &n.ConversationKind, &sequence, &n.ThreadRootID, &rootSeq, &n.AuthorID, &n.CoveringID, &n.At, &n.Read); err != nil {
				return err
			}
			if seen[n.PostID] {
				continue
			}
			seen[n.PostID] = true
			n.ID, n.Sequence, n.ThreadRootSequence, n.At = n.PostID, uint64(sequence), uint64(rootSeq), n.At.UTC()
			out = append(out, n)
		}
		return rows.Err()
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, err
}

// MarkCoverRead stores the cover's read choice for cover notices. A message that
// is not a cover notice for the caller is skipped without saying so.
func (r *RecipientStateStore) MarkCoverRead(ctx context.Context, host, home, cover string, since, now time.Time, postIDs []string, all bool) (int64, error) {
	ids, sinces, err := r.coverCharges(ctx, home, cover, now)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	if postIDs == nil {
		postIDs = []string{}
	}
	var marked int64
	err = r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, issue3789CoverMarkQuery, host, home, chatrecipient.NotificationsLimit*4, cover, since.UTC(), ids, sinces, postIDs, all)
		marked = n
		return err
	})
	return marked, err
}
