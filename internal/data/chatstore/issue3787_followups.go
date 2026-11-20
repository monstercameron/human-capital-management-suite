package chatstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3787: follow-ups are a read-time projection over posts the caller can
// already read. Parameters shared by both queries:
//
//	$1 host company  $2 caller's home company  $3 row limit  $4 caller
//	$5 now  $6 newest a row may be (now - minimum age)  $7 oldest a row may be
const (
	issue3787MentionsMe = `jsonb_build_array(jsonb_build_object('Kind','PERSON_MENTION','TenantID',$2::text,'ID',$4::text))`
	// A post the caller wrote: the author id plus the caller's home company
	// (older rows carry an empty author company).
	issue3787Mine = `AND %s.author_home_tenant_id IN ('',$2)`
	// The membership the caller reads history through.
	issue3787Visible = `(m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND %s.created_at>=m.joined_at))`
	// The person's own choice leaves the row listed only when it is unset,
	// kept, or a snooze that has run out.
	issue3787StateOpen = `(s.status IS NULL OR s.status='kept' OR (s.status='snoozed' AND s.snooze_until<=$5))`
	issue3787Columns   = `p.conversation_id,p.id,p.sequence,p.author_id,p.body,p.parent_id,p.created_at`
)

func issue3787Vis(alias string) string { return strings.ReplaceAll(issue3787Visible, "%s", alias) }

// owedQuery finds messages that mention the caller and that the caller has not
// answered in the message's own thread. A reaction, or the caller's next post
// in the channel, is returned as evidence and never hides the row.
var issue3787OwedQuery = `SELECT ` + issue3787Columns + `,'' AS them,
 CASE WHEN nx.author_id=$4 THEN 'posted_later' WHEN rx.at IS NOT NULL THEN 'reacted' ELSE '' END,
 COALESCE(CASE WHEN nx.author_id=$4 THEN nx.at ELSE rx.at END,'epoch'::timestamptz),
 COALESCE(s.status,'')
 FROM chat_membership m
 JOIN chat_post p ON p.tenant_id=m.tenant_id AND p.conversation_id=m.conversation_id
 LEFT JOIN chat_followup_state s ON s.tenant_id=p.tenant_id AND s.home_tenant_id=$2 AND s.member_id=$4 AND s.conversation_id=p.conversation_id AND s.post_id=p.id AND s.direction='owed'
 LEFT JOIN LATERAL (SELECT x.created_at AS at FROM chat_reaction x WHERE x.tenant_id=p.tenant_id AND x.post_id=p.id AND x.home_tenant_id=$2 AND x.member_id=$4 ORDER BY x.created_at DESC LIMIT 1) rx ON true
 LEFT JOIN LATERAL (SELECT n.author_id,n.created_at AS at FROM chat_post n WHERE p.parent_id='' AND n.tenant_id=p.tenant_id AND n.conversation_id=p.conversation_id AND n.sequence>p.sequence AND n.parent_id='' AND NOT n.tombstoned AND ` + issue3787Vis("n") + issue3916PostAudience("n") + ` ORDER BY n.sequence LIMIT 1) nx ON true
 WHERE m.tenant_id=$1 AND m.home_tenant_id=$2 AND m.member_id=$4 AND m.state='active'
 AND NOT p.tombstoned AND p.author_id<>$4 AND p.created_at>=$7 AND p.created_at<=$6
 AND p.references_json @> ` + issue3787MentionsMe + `
 AND p.body NOT LIKE chr(8291)||'member-added:%'
 AND ` + issue3787Vis("p") + issue3916PostAudience("p") + `
 AND ` + issue3787StateOpen + `
 AND NOT EXISTS (SELECT 1 FROM chat_post r WHERE r.tenant_id=p.tenant_id AND r.conversation_id=p.conversation_id AND r.author_id=$4 ` + strings.ReplaceAll(issue3787Mine, "%s", "r") + ` AND NOT r.tombstoned AND r.sequence>p.sequence AND r.parent_id=COALESCE(NULLIF(p.parent_id,''),p.id))
 ORDER BY p.created_at DESC,p.id DESC LIMIT $3`

// waitingQuery finds the caller's own messages that mention someone or end in
// a question. A reply by the people asked in the thread, a reaction, or their
// next post in the channel is evidence only (it may answer a different
// question): the row stays until the caller closes it.
var issue3787WaitingQuery = `SELECT ` + issue3787Columns + `,array_to_string(t.ids,chr(31)),
 CASE WHEN rp.at IS NOT NULL THEN 'replied' WHEN nx.author_id IS NOT NULL AND nx.author_id<>$4 AND (cardinality(t.ids)=0 OR nx.author_id=ANY(t.ids)) THEN 'posted_later' WHEN rx.at IS NOT NULL THEN 'reacted' ELSE '' END,
 COALESCE(CASE WHEN rp.at IS NOT NULL THEN rp.at WHEN nx.author_id IS NOT NULL AND nx.author_id<>$4 AND (cardinality(t.ids)=0 OR nx.author_id=ANY(t.ids)) THEN nx.at ELSE rx.at END,'epoch'::timestamptz),
 COALESCE(s.status,'')
 FROM chat_membership m
 JOIN chat_post p ON p.tenant_id=m.tenant_id AND p.conversation_id=m.conversation_id
 CROSS JOIN LATERAL (SELECT ARRAY(SELECT DISTINCT r->>'ID' FROM jsonb_array_elements(CASE WHEN jsonb_typeof(p.references_json)='array' THEN p.references_json ELSE '[]'::jsonb END) r
   WHERE r->>'Kind' IN ('PERSON_MENTION','AGENT_MENTION') AND COALESCE(r->>'ID','')<>'' AND r->>'ID'<>$4) AS ids) t
 LEFT JOIN chat_followup_state s ON s.tenant_id=p.tenant_id AND s.home_tenant_id=$2 AND s.member_id=$4 AND s.conversation_id=p.conversation_id AND s.post_id=p.id AND s.direction='waiting'
 LEFT JOIN LATERAL (SELECT x.created_at AS at FROM chat_reaction x WHERE x.tenant_id=p.tenant_id AND x.post_id=p.id AND NOT (x.member_id=$4 AND x.home_tenant_id=$2) AND (cardinality(t.ids)=0 OR x.member_id=ANY(t.ids)) ORDER BY x.created_at DESC LIMIT 1) rx ON true
 LEFT JOIN LATERAL (SELECT n.author_id,n.created_at AS at FROM chat_post n WHERE p.parent_id='' AND n.tenant_id=p.tenant_id AND n.conversation_id=p.conversation_id AND n.sequence>p.sequence AND n.parent_id='' AND NOT n.tombstoned AND ` + issue3787Vis("n") + issue3916PostAudience("n") + ` ORDER BY n.sequence LIMIT 1) nx ON true
 LEFT JOIN LATERAL (SELECT r.created_at AS at FROM chat_post r WHERE r.tenant_id=p.tenant_id AND r.conversation_id=p.conversation_id AND r.sequence>p.sequence AND r.parent_id=COALESCE(NULLIF(p.parent_id,''),p.id)
   AND NOT r.tombstoned AND r.author_id<>$4 AND (cardinality(t.ids)=0 OR r.author_id=ANY(t.ids)) AND ` + issue3787Vis("r") + issue3916PostAudience("r") + ` ORDER BY r.sequence LIMIT 1) rp ON true
 WHERE m.tenant_id=$1 AND m.home_tenant_id=$2 AND m.member_id=$4 AND m.state='active'
 AND p.author_id=$4 ` + strings.ReplaceAll(issue3787Mine, "%s", "p") + ` AND NOT p.tombstoned AND p.created_at>=$7 AND p.created_at<=$6
 AND (cardinality(t.ids)>0 OR p.body ~ '[?？؟]\s*$')
 AND ` + issue3787Vis("p") + issue3916PostAudience("p") + `
 AND ` + issue3787StateOpen + `

 ORDER BY p.created_at DESC,p.id DESC LIMIT $3`

// ListFollowUps returns both directions of the caller's follow-ups, each
// bounded by limit, newest first within the bound. It is a read: it writes no
// read cursor and no state row, and it never sends anything.
func (r *RecipientStateStore) ListFollowUps(ctx context.Context, host, home, subject string, now time.Time, minAge time.Duration, limit int) ([]chatrecipient.FollowUp, error) {
	if limit <= 0 || limit > chatrecipient.FollowUpsLimit {
		limit = chatrecipient.FollowUpsLimit
	}
	if minAge < 0 {
		minAge = 0
	}
	out := []chatrecipient.FollowUp{}
	err := r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		for _, direction := range []string{chatrecipient.FollowUpOwed, chatrecipient.FollowUpWaiting} {
			query := issue3787OwedQuery
			if direction == chatrecipient.FollowUpWaiting {
				query = issue3787WaitingQuery
			}
			rows, err := tx.Query(ctx, query, host, home, limit, subject, now, now.Add(-minAge), now.Add(-chatrecipient.FollowUpWindow))
			if err != nil {
				return err
			}
			for rows.Next() {
				var (
					row      = chatrecipient.FollowUp{Direction: direction}
					sequence int64
					parent   string
					them     string
					status   string
					evAt     time.Time
				)
				if err := rows.Scan(&row.ConversationID, &row.PostID, &sequence, &row.AuthorID, &row.Body, &parent, &row.AskedAt, &them, &row.Evidence, &evAt, &status); err != nil {
					rows.Close()
					return err
				}
				row.Sequence, row.ThreadRootID, row.Kept = uint64(sequence), parent, status == chatrecipient.FollowUpKept
				if them != "" {
					row.WaitingOn = strings.Split(them, "\x1f")
				}
				if row.Evidence != "" && evAt.Unix() > 0 {
					row.EvidenceAt = evAt
				}
				out = append(out, row)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// PutFollowUpState stores the caller's choice for one row. The caller must be
// an active member who may read the message; a waiting row is only for the
// caller's own message. Anything else is refused before a row is written.
func (r *RecipientStateStore) PutFollowUpState(ctx context.Context, id chatrecipient.Identity, postID, direction, status string, until time.Time) error {
	return r.store.RunTenantTx(ctx, id.HostTenantID, func(tx dbport.Tx) error {
		if err := recipientMember(ctx, tx, id); err != nil {
			return err
		}
		var author string
		err := tx.QueryRow(ctx, `SELECT p.author_id FROM chat_post p JOIN chat_membership m
            ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$5 AND m.member_id=$4 AND m.state='active'
            WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.id=$3 AND `+issue3787Vis("p")+issue3916PostAudience("p"),
			id.HostTenantID, id.ConversationID, postID, id.SubjectID, id.HomeTenantID).Scan(&author)
		if errors.Is(err, dbport.ErrNoRows) {
			return chat.ErrNotFound
		}
		if err != nil {
			return err
		}
		if direction == chatrecipient.FollowUpWaiting && author != id.SubjectID {
			return chat.ErrInvalidArgument
		}
		var snooze any
		if status == chatrecipient.FollowUpSnoozed {
			snooze = until
		}
		_, err = tx.Exec(ctx, `INSERT INTO chat_followup_state(tenant_id,home_tenant_id,member_id,conversation_id,post_id,direction,status,snooze_until)
            VALUES($1,$2,$3,$4,$5,$6,$7,$8)
            ON CONFLICT (tenant_id,home_tenant_id,member_id,conversation_id,post_id,direction)
            DO UPDATE SET status=EXCLUDED.status,snooze_until=EXCLUDED.snooze_until,revision=chat_followup_state.revision+1,updated_at=now()`,
			id.HostTenantID, id.HomeTenantID, id.SubjectID, id.ConversationID, postID, direction, status, snooze)
		return err
	})
}
