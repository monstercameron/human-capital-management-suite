package chatstore

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3786: the personal digest. chat_digest_pref keeps only the person's
// choice and when they last saw a digest. The candidates are a read-time
// projection over posts the person can read right now (active membership,
// history and post audience, as in the notification feed): mentions of them
// and replies in threads they follow, whatever their notification choices, so
// the service can tell which were held back. Parameters:
//
//	$1 host company  $2 caller's home company  $3 row limit  $4 caller
//	$5 oldest a post may be
var issue3786Candidates = `SELECT p.id,p.conversation_id,c.name,c.kind,p.sequence,p.parent_id,COALESCE(root.sequence,0),p.author_id,p.created_at,
 CASE WHEN p.references_json @> ` + issue3733Mention + ` THEN 'mention' ELSE 'reply' END,
 COALESCE((pref.value->>'Muted')::boolean,false),
 (nr.post_id IS NOT NULL OR COALESCE(cur.last_sequence,0)>=p.sequence)
 FROM chat_membership m
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
 AND (p.references_json @> ` + issue3733Mention + ` OR (c.kind<>'DIRECT' AND f.root_post_id IS NOT NULL))
 ORDER BY p.created_at DESC,p.id DESC LIMIT $3`

var _ chatrecipient.DigestStore = (*RecipientStateStore)(nil)

// DigestPref returns the person's digest choice and whether they ever made one.
func (r *RecipientStateStore) DigestPref(ctx context.Context, home, subject string) (chatrecipient.DigestPref, bool, error) {
	var out chatrecipient.DigestPref
	found := true
	err := r.store.RunTenantTx(ctx, home, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT cadence,minute,timezone,last_digest_at FROM chat_digest_pref WHERE tenant_id=$1 AND member_id=$2`, home, subject).
			Scan(&out.Cadence, &out.Minute, &out.Timezone, &out.LastDigestAt)
		if errors.Is(err, dbport.ErrNoRows) {
			found = false
			return nil
		}
		return err
	})
	out.LastDigestAt = out.LastDigestAt.UTC()
	return out, found, err
}

// PutDigestPref stores the person's digest choice.
func (r *RecipientStateStore) PutDigestPref(ctx context.Context, home, subject string, pref chatrecipient.DigestPref) error {
	return r.store.RunTenantTx(ctx, home, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_digest_pref(tenant_id,member_id,cadence,minute,timezone,last_digest_at) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT (tenant_id,member_id) DO UPDATE SET cadence=EXCLUDED.cadence,minute=EXCLUDED.minute,timezone=EXCLUDED.timezone,last_digest_at=EXCLUDED.last_digest_at,updated_at=now()`,
			home, subject, pref.Cadence, pref.Minute, pref.Timezone, pref.LastDigestAt)
		return err
	})
}

// AdvanceDigest moves the moment the person last saw a digest forward only.
func (r *RecipientStateStore) AdvanceDigest(ctx context.Context, home, subject string, through time.Time) error {
	return r.store.RunTenantTx(ctx, home, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_digest_pref SET last_digest_at=GREATEST(last_digest_at,$3),updated_at=now() WHERE tenant_id=$1 AND member_id=$2`, home, subject, through)
		return err
	})
}

// DigestCandidates lists what could be in the person's digest; the service
// decides what was held back.
func (r *RecipientStateStore) DigestCandidates(ctx context.Context, host, home, subject string, since time.Time, limit int) ([]chatrecipient.DigestItem, error) {
	if limit <= 0 || limit > chatrecipient.DigestScanLimit {
		limit = chatrecipient.DigestScanLimit
	}
	out := []chatrecipient.DigestItem{}
	err := r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, issue3786Candidates, host, home, limit, subject, since)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				n        chatrecipient.DigestItem
				sequence int64
				rootSeq  int64
			)
			if err := rows.Scan(&n.PostID, &n.ConversationID, &n.ConversationName, &n.ConversationKind, &sequence, &n.ThreadRootID, &rootSeq, &n.AuthorID, &n.At, &n.Kind, &n.Muted, &n.Read); err != nil {
				return err
			}
			n.Sequence, n.ThreadRootSequence = uint64(sequence), uint64(rootSeq)
			n.At = n.At.UTC()
			out = append(out, n)
		}
		return rows.Err()
	})
	return out, err
}
