package chatstore

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// A rolling server rebuild can precede the orchestrator's migration step.
// Resolve the schema inside this connection's search path, keeping reads
// available until the transactional read-state migration has committed.
func chatscaleSchemaReady(ctx context.Context, tx dbport.Tx) (bool, error) {
	var ready bool
	err := tx.QueryRow(ctx, `SELECT to_regclass('chatscale_read_state') IS NOT NULL`).Scan(&ready)
	return ready, err
}

// chatscaleSchemaLevel tells which generation of the read-state schema this
// connection sees: 0 none, 1 the revision-fenced table, 2 with the head stamp
// of migration 00046. A rolling rebuild can precede the migration step.
func chatscaleSchemaLevel(ctx context.Context, tx dbport.Tx) (int, error) {
	var level int
	err := tx.QueryRow(ctx, `SELECT CASE WHEN to_regclass('chatscale_read_state') IS NULL THEN 0
 WHEN EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('chatscale_read_state') AND attname='window_rows' AND NOT attisdropped) THEN 2
 ELSE 1 END`).Scan(&level)
	return level, err
}

func chatscaleActivityQuery(ctx context.Context, tx dbport.Tx, query string) (string, error) {
	ready, err := chatscaleSchemaReady(ctx, tx)
	if err != nil {
		return "", err
	}
	if !ready {
		query = strings.Replace(query, lastActivitySubquery, `(SELECT max(lp.created_at) FROM chat_post lp WHERE lp.tenant_id=c.tenant_id AND lp.conversation_id=c.id AND lp.tombstoned=false)`, 1)
	}
	return query, nil
}

// Two disjoint ranges replace the former OR over the entire conversation.
// Each branch takes at most the display limit, then the union takes that limit
// again. A recent edit below the read cursor still contributes a mention, while
// an edited unread post occurs only once. This remains the history oracle.
//
// The "added people" system line is not something to read: it never counts as
// unread, so it cannot light a badge or ring the new-message sound.
const chatscaleNotSystemLine = ` AND p.body NOT LIKE '` + chat.MembershipAddedMarker + `%'`

const chatscaleCandidates = `
 SELECT sequence,references_json,unseen FROM (
 (SELECT p.sequence,p.references_json,true AS unseen FROM chat_post p
 WHERE p.tenant_id=$1 AND p.conversation_id=r.conversation_id
 AND p.sequence>r.last_sequence AND NOT p.tombstoned` + chatscaleNotSystemLine + `
 AND NOT (p.author_home_tenant_id=$2 AND p.author_id=$3)
 AND (r.history_visibility='FULL_HISTORY' OR (r.history_visibility='FROM_JOIN' AND p.created_at>=r.joined_at))
 ORDER BY p.sequence DESC LIMIT $5)
 UNION ALL
 (SELECT p.sequence,p.references_json,false AS unseen FROM chat_post p
 WHERE p.tenant_id=$1 AND p.conversation_id=r.conversation_id
 AND p.sequence<=r.last_sequence AND p.revision>1 AND p.updated_at>r.read_at AND NOT p.tombstoned` + chatscaleNotSystemLine + `
 AND NOT (p.author_home_tenant_id=$2 AND p.author_id=$3)
 AND (r.history_visibility='FULL_HISTORY' OR (r.history_visibility='FROM_JOIN' AND p.created_at>=r.joined_at))
 ORDER BY p.sequence DESC LIMIT $5)
 ) bounded ORDER BY sequence DESC LIMIT $5`

const chatscaleReaders = `WITH reader AS MATERIALIZED (
 SELECT m.conversation_id,m.history_visibility,m.joined_at,m.revision AS member_revision,
 COALESCE(c.last_sequence,0) AS last_sequence,c.updated_at AS read_at,
 COALESCE(c.revision,1) AS read_revision,h.chatscale_history_revision AS history_revision,
 h.chatscale_last_activity_at AS last_activity_at,h.post_sequence AS head
 FROM chat_membership m JOIN chat_conversation h ON h.tenant_id=m.tenant_id AND h.id=m.conversation_id
 LEFT JOIN chat_cursor c ON c.tenant_id=m.tenant_id
 AND c.conversation_id=m.conversation_id AND c.home_tenant_id=m.home_tenant_id AND c.member_id=m.member_id
 WHERE m.tenant_id=$1 AND m.home_tenant_id=$2 AND m.member_id=$3
 AND m.state='active' AND m.conversation_id=ANY($4::text[])
 )`

const chatscaleRebuildSQL = chatscaleReaders + ` SELECT r.conversation_id,
 count(*) FILTER(WHERE p.unseen),count(*) FILTER(WHERE p.references_json @>
 jsonb_build_array(jsonb_build_object('Kind','PERSON_MENTION','TenantID',$2::text,'ID',$3::text)))
 FROM reader r LEFT JOIN LATERAL (` + chatscaleCandidates + `) p ON true
 GROUP BY r.conversation_id`

// chatscaleCachedLegacySQL serves a database that has the read-state table but
// not migration 00046: there every send bumps the history revision, so a cached
// row is valid exactly while the revision matches.
const chatscaleCachedLegacySQL = chatscaleReaders + `,
 cached AS MATERIALIZED (
 SELECT r.conversation_id,s.unread,s.mentions FROM reader r JOIN chatscale_read_state s
 ON s.tenant_id=$1 AND s.home_tenant_id=$2 AND s.member_id=$3 AND s.conversation_id=r.conversation_id
 AND s.history_revision=r.history_revision AND s.member_revision=r.member_revision
 AND s.history_visibility=r.history_visibility AND s.joined_at=r.joined_at
 AND s.read_revision=r.read_revision AND s.last_sequence=r.last_sequence
 AND s.read_at=COALESCE(r.read_at,'epoch'::timestamptz)
 ), missing AS MATERIALIZED (
 SELECT r.* FROM reader r WHERE NOT EXISTS(SELECT 1 FROM cached c WHERE c.conversation_id=r.conversation_id)
 ), rebuilt AS (
 SELECT r.conversation_id,r.history_revision,r.member_revision,r.history_visibility,r.joined_at,
 r.read_revision,r.last_sequence,COALESCE(r.read_at,'epoch'::timestamptz) AS read_at,r.last_activity_at,
 count(*) FILTER(WHERE p.unseen) AS unread,count(*) FILTER(WHERE p.references_json @>
 jsonb_build_array(jsonb_build_object('Kind','PERSON_MENTION','TenantID',$2::text,'ID',$3::text))) AS mentions
 FROM missing r LEFT JOIN LATERAL (` + chatscaleCandidates + `) p ON true
 GROUP BY r.conversation_id,r.history_revision,r.member_revision,r.history_visibility,r.joined_at,
 r.read_revision,r.last_sequence,r.read_at,r.last_activity_at
 ), saved AS (
 INSERT INTO chatscale_read_state(tenant_id,home_tenant_id,member_id,conversation_id,
 history_revision,member_revision,history_visibility,joined_at,read_revision,last_sequence,read_at,last_activity_at,unread,mentions)
 SELECT $1,$2,$3,conversation_id,history_revision,member_revision,history_visibility,joined_at,
 read_revision,last_sequence,read_at,last_activity_at,unread,mentions FROM rebuilt
 ON CONFLICT(tenant_id,home_tenant_id,member_id,conversation_id) DO UPDATE SET
 history_revision=EXCLUDED.history_revision,member_revision=EXCLUDED.member_revision,
 history_visibility=EXCLUDED.history_visibility,joined_at=EXCLUDED.joined_at,
 read_revision=EXCLUDED.read_revision,last_sequence=EXCLUDED.last_sequence,read_at=EXCLUDED.read_at,
 last_activity_at=EXCLUDED.last_activity_at,unread=EXCLUDED.unread,mentions=EXCLUDED.mentions
 RETURNING conversation_id,unread,mentions
 ) SELECT conversation_id,unread,mentions FROM cached UNION ALL SELECT conversation_id,unread,mentions FROM saved`

// ChatscaleSidebarCounts reads a person's cached rows in one query. Missing or dirty
// rows are rebuilt lazily; sends never fan out to disconnected members. The
// generation commits with posts, while membership and cursor fields fence
// revocation, mark-unread and history changes even before outbox replay.
func (r *RecipientStateStore) ChatscaleSidebarCounts(ctx context.Context, host, home, subject string, conversations []string) (map[string]chatrecipient.Counts, error) {
	return r.chatscaleReadCounts(ctx, host, home, subject, conversations, false)
}

// ChatscaleBackfillHeads initializes pre-migration heads without a table rewrite.
// SKIP LOCKED leaves conversations being sent to for a later batch. The head
// trigger and this update serialize on the same row, preventing stale heads.
func (s *Store) ChatscaleBackfillHeads(ctx context.Context, host string, limit int) (int64, error) {
	if host == "" || limit < 1 || limit > 500 {
		return 0, chat.ErrInvalidArgument
	}
	var n int64
	err := s.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		ready, err := chatscaleSchemaReady(ctx, tx)
		if err != nil {
			return err
		}
		if !ready {
			return chat.ErrUnavailable
		}
		n, err = tx.Exec(ctx, `WITH batch AS MATERIALIZED (
 SELECT id FROM chat_conversation WHERE tenant_id=$1 AND NOT chatscale_head_ready
 ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED
 ) UPDATE chat_conversation c SET chatscale_head_ready=true,
 (chatscale_last_activity_at,chatscale_last_author,chatscale_last_author_home)=
 (SELECT p.created_at,p.author_id,p.author_home_tenant_id FROM chat_post p
 WHERE p.tenant_id=c.tenant_id AND p.conversation_id=c.id AND NOT p.tombstoned
 ORDER BY p.created_at DESC,p.id DESC LIMIT 1)
 FROM batch b WHERE c.tenant_id=$1 AND c.id=b.id`, host, limit)
		return err
	})
	return n, err
}

// ChatscaleRebuildCounts bypasses and replaces cached values with the bounded history
// oracle. One transaction and one query serve up to 300 channels.
// Unknown and revoked memberships are absent, just as Counts returns zero.
func (r *RecipientStateStore) ChatscaleRebuildCounts(ctx context.Context, host, home, subject string, conversations []string) (map[string]chatrecipient.Counts, error) {
	return r.chatscaleReadCounts(ctx, host, home, subject, conversations, true)
}

func (r *RecipientStateStore) chatscaleReadCounts(ctx context.Context, host, home, subject string, conversations []string, recount bool) (map[string]chatrecipient.Counts, error) {
	return r.chatscaleReadCountsLimit(ctx, host, home, subject, conversations, recount, countScanLimit)
}

// chatscaleReadCountsLimit is the read with the display limit as an argument, so
// a test can reach the saturated window with a handful of posts.
func (r *RecipientStateStore) chatscaleReadCountsLimit(ctx context.Context, host, home, subject string, conversations []string, recount bool, limit int) (map[string]chatrecipient.Counts, error) {
	if len(conversations) > 300 || host == "" || home == "" || subject == "" {
		return nil, chat.ErrInvalidArgument
	}
	out := make(map[string]chatrecipient.Counts, len(conversations))
	err := r.store.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		level, err := chatscaleSchemaLevel(ctx, tx)
		if err != nil {
			return err
		}
		sql := chatscaleCachedSQL
		if level == 1 {
			sql = chatscaleCachedLegacySQL
		}
		if recount {
			sql = chatscaleBypassCache(sql)
		}
		if level == 0 {
			sql = strings.Replace(chatscaleRebuildSQL, "h.chatscale_history_revision AS history_revision", "0 AS history_revision", 1)
			sql = strings.Replace(sql, "h.chatscale_last_activity_at AS last_activity_at", "NULL::timestamptz AS last_activity_at", 1)
		}
		rows, err := tx.Query(ctx, sql, host, home, subject, conversations, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var room string
			var unread, mentions uint64
			if err := rows.Scan(&room, &unread, &mentions); err != nil {
				return err
			}
			out[room] = chatrecipient.Counts{Unread: unread, Mentions: mentions}
		}
		return rows.Err()
	})
	return out, err
}
