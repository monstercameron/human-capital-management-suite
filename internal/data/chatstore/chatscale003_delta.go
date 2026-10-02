package chatstore

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATSCALE-003. The sidebar read keeps a person's cached badge valid across
// plain sends (migration 00046): the cached row records the conversation head
// it was counted at and the number of posts in its counting window, and the
// read adds only the posts after that head. Edits, removals, deletions,
// membership and cursor changes still invalidate the row, and a row whose
// window would overflow the display limit is recounted, because then the
// oldest posts drop out of the window and a mention among them must drop too.
//
// A row that has to be counted (nothing cached, or invalidated) is counted
// without reading a post body where it can be: eligible posts are counted from
// chatscale_unread alone, the "added people" lines come from their own small
// partial index and the posts that mention somebody from another, so only
// mention candidates and edited posts are read from the heap. A conversation
// the fast count cannot answer exactly (a saturated window that also holds an
// "added people" line, or a window made too large by edits) falls back to the
// bounded scan in chatscaleRebuildSQL, which stays the oracle.

// chatscaleEligible is what any count must take from a post before looking at
// its body: not removed, not the person's own, inside the history they may see.
// The alias names a row that carries conversation_id, history_visibility and
// joined_at.
func chatscaleEligible(alias string) string {
	return ` p.tenant_id=$1 AND p.conversation_id=` + alias + `.conversation_id AND NOT p.tombstoned
 AND NOT (p.author_home_tenant_id=$2 AND p.author_id=$3)
 AND (` + alias + `.history_visibility='FULL_HISTORY' OR (` + alias + `.history_visibility='FROM_JOIN' AND p.created_at>=` + alias + `.joined_at))`
}

const chatscaleMentionsMe = `jsonb_build_array(jsonb_build_object('Kind','PERSON_MENTION','TenantID',$2::text,'ID',$3::text))`

// chatscaleAnyMention is the predicate of chatscale_mention_posts.
const chatscaleAnyMention = `'[{"Kind":"PERSON_MENTION"}]'::jsonb`

// chatscaleSystemLine is the predicate of chatscale_system_lines.
const chatscaleSystemLine = `p.body LIKE '` + chat.MembershipAddedMarker + `%'`

// The counts are bigint; the limit parameter $5 is the display limit.
var chatscaleCachedSQL = chatscaleReaders + `,
 cached0 AS MATERIALIZED (
 SELECT r.conversation_id,r.history_visibility,r.joined_at,r.head,s.unread,s.mentions,s.head_sequence,s.window_rows
 FROM reader r JOIN chatscale_read_state s
 ON s.tenant_id=$1 AND s.home_tenant_id=$2 AND s.member_id=$3 AND s.conversation_id=r.conversation_id
 AND s.history_revision=r.history_revision AND s.member_revision=r.member_revision
 AND s.history_visibility=r.history_visibility AND s.joined_at=r.joined_at
 AND s.read_revision=r.read_revision AND s.last_sequence=r.last_sequence
 AND s.read_at=COALESCE(r.read_at,'epoch'::timestamptz)
 AND s.head_sequence IS NOT NULL AND s.window_rows IS NOT NULL AND s.head_sequence<=r.head
 ), added AS MATERIALIZED (
 SELECT c.conversation_id,count(*) AS unread,count(*) FILTER(WHERE p.references_json @> ` + chatscaleMentionsMe + `) AS mentions
 FROM cached0 c CROSS JOIN LATERAL (SELECT p.references_json FROM chat_post p
 WHERE ` + chatscaleEligible("c") + ` AND p.sequence>c.head_sequence AND p.sequence<=c.head AND NOT ` + chatscaleSystemLine + `
 ORDER BY p.sequence DESC LIMIT $5) p
 WHERE c.head>c.head_sequence GROUP BY c.conversation_id
 ), cached AS MATERIALIZED (
 SELECT c.conversation_id,c.unread+COALESCE(a.unread,0) AS unread,c.mentions+COALESCE(a.mentions,0) AS mentions
 FROM cached0 c LEFT JOIN added a USING (conversation_id)
 WHERE c.window_rows+COALESCE(a.unread,0)<=$5
 ), missing AS MATERIALIZED (
 SELECT r.* FROM reader r WHERE NOT EXISTS(SELECT 1 FROM cached c WHERE c.conversation_id=r.conversation_id)
 ), counted AS MATERIALIZED (
 SELECT r.*,sy.n AS nsys,ra.n AS raw,ra.low
 FROM missing r
 CROSS JOIN LATERAL (SELECT count(*) AS n FROM (SELECT 1 FROM chat_post p
 WHERE ` + chatscaleEligible("r") + ` AND p.sequence>r.last_sequence AND ` + chatscaleSystemLine + ` LIMIT $5) q) sy
 CROSS JOIN LATERAL (SELECT count(*) AS n,min(q.sequence) AS low FROM (SELECT p.sequence FROM chat_post p
 WHERE ` + chatscaleEligible("r") + ` AND p.sequence>r.last_sequence ORDER BY p.sequence DESC LIMIT $5+sy.n) q) ra
 ), sized AS MATERIALIZED (
 -- With no system line in range, a saturated window ends at the lowest sequence
 -- the capped scan above returned.
 SELECT t.*,t.raw-t.nsys AS unseen,CASE WHEN t.nsys=0 AND t.raw>=$5 THEN t.low END AS bound FROM counted t
 ), edited AS MATERIALIZED (
 SELECT t.conversation_id,count(*) AS n,count(*) FILTER(WHERE x.references_json @> ` + chatscaleMentionsMe + `) AS m
 FROM sized t CROSS JOIN LATERAL (SELECT p.references_json FROM chat_post p
 WHERE ` + chatscaleEligible("t") + ` AND p.sequence<=t.last_sequence AND p.revision>1 AND p.updated_at>t.read_at
 AND NOT ` + chatscaleSystemLine + ` ORDER BY p.sequence DESC LIMIT $5) x
 WHERE t.unseen<$5 GROUP BY t.conversation_id
 ), ready AS MATERIALIZED (
 SELECT t.*,COALESCE(e.n,0) AS edit_n,COALESCE(e.m,0) AS edit_m,
 ((t.unseen<$5 AND t.nsys<$5 AND t.unseen+COALESCE(e.n,0)<=$5) OR (t.unseen>=$5 AND t.nsys=0)) AS fast
 FROM sized t LEFT JOIN edited e USING (conversation_id)
 ), mentioned AS MATERIALIZED (
 SELECT t.conversation_id,count(*) FILTER(WHERE x.references_json @> ` + chatscaleMentionsMe + `) AS m
 FROM ready t CROSS JOIN LATERAL (SELECT p.references_json FROM chat_post p
 WHERE ` + chatscaleEligible("t") + ` AND p.sequence>t.last_sequence AND p.sequence>=COALESCE(t.bound,0)
 AND p.references_json @> ` + chatscaleAnyMention + ` AND NOT ` + chatscaleSystemLine + `
 ORDER BY p.sequence DESC LIMIT $5) x
 WHERE t.fast GROUP BY t.conversation_id
 ), rebuilt AS (
 SELECT t.conversation_id,t.history_revision,t.member_revision,t.history_visibility,t.joined_at,
 t.read_revision,t.last_sequence,COALESCE(t.read_at,'epoch'::timestamptz) AS read_at,t.last_activity_at,t.head,
 LEAST($5::bigint,t.unseen) AS unread,COALESCE(m.m,0)+t.edit_m AS mentions,LEAST($5::bigint,t.unseen)+t.edit_n AS window_rows
 FROM ready t LEFT JOIN mentioned m USING (conversation_id) WHERE t.fast
 UNION ALL
 SELECT r.conversation_id,r.history_revision,r.member_revision,r.history_visibility,r.joined_at,
 r.read_revision,r.last_sequence,COALESCE(r.read_at,'epoch'::timestamptz),r.last_activity_at,r.head,
 count(*) FILTER(WHERE p.unseen),count(*) FILTER(WHERE p.references_json @> ` + chatscaleMentionsMe + `),count(*)
 FROM ready r LEFT JOIN LATERAL (` + chatscaleCandidates + `) p ON true WHERE NOT r.fast
 GROUP BY r.conversation_id,r.history_revision,r.member_revision,r.history_visibility,r.joined_at,
 r.read_revision,r.last_sequence,r.read_at,r.last_activity_at,r.head
 ), saved AS (
 INSERT INTO chatscale_read_state(tenant_id,home_tenant_id,member_id,conversation_id,
 history_revision,member_revision,history_visibility,joined_at,read_revision,last_sequence,read_at,last_activity_at,unread,mentions,head_sequence,window_rows)
 SELECT $1,$2,$3,conversation_id,history_revision,member_revision,history_visibility,joined_at,
 read_revision,last_sequence,read_at,last_activity_at,unread,mentions,head,window_rows FROM rebuilt
 ON CONFLICT(tenant_id,home_tenant_id,member_id,conversation_id) DO UPDATE SET
 history_revision=EXCLUDED.history_revision,member_revision=EXCLUDED.member_revision,
 history_visibility=EXCLUDED.history_visibility,joined_at=EXCLUDED.joined_at,
 read_revision=EXCLUDED.read_revision,last_sequence=EXCLUDED.last_sequence,read_at=EXCLUDED.read_at,
 last_activity_at=EXCLUDED.last_activity_at,unread=EXCLUDED.unread,mentions=EXCLUDED.mentions,
 head_sequence=EXCLUDED.head_sequence,window_rows=EXCLUDED.window_rows
 RETURNING conversation_id,unread,mentions
 ) SELECT conversation_id,unread,mentions FROM cached UNION ALL SELECT conversation_id,unread,mentions FROM saved`

// chatscaleBypassCache turns a cached-read statement into one that recounts
// every conversation, for the repair path.
func chatscaleBypassCache(sql string) string {
	return strings.Replace(sql, "AND s.history_revision=r.history_revision", "AND false AND s.history_revision=r.history_revision", 1)
}
