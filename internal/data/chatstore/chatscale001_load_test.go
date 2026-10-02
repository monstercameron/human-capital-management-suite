package chatstore

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// The CHATSCALE-001 volume tool. The test in chatscale001_volume_test.go does
// nothing unless HCMNEXT_CHATSCALE001 is set, because loading a million posts
// does not belong in an ordinary package run. Every parameter is an
// environment variable so the same file serves a two-minute run on a laptop and
// a longer run elsewhere:
//
//	HCMNEXT_CHATSCALE001=1             run the tool
//	CHATSCALE001_STAGES=100000,1000000 cumulative post counts, each measured
//	CHATSCALE001_CHANNELS=300          channels (the reader belongs to all)
//	CHATSCALE001_MEMBERS=2000          distinct people
//	CHATSCALE001_SAMPLES=5             measured executions per statement
//	CHATSCALE001_SENDS=100             committed sends in each phase's send loop
//	CHATSCALE001_CANDIDATES=a.sql,b.sql  candidate migrations applied in order
//	                                   to the measurement schema, each measured
//	CHATSCALE001_OUT=report.json       where the JSON report goes

func c001Env(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func c001EnvInt(name string, fallback int) int {
	n, err := strconv.Atoi(c001Env(name, ""))
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

func c001Stages(t *testing.T) []int {
	t.Helper()
	var out []int
	for _, field := range strings.Split(c001Env("CHATSCALE001_STAGES", "100000,1000000"), ",") {
		n, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || n < 5000 || n%5000 != 0 || (len(out) > 0 && n <= out[len(out)-1]) {
			t.Fatalf("CHATSCALE001_STAGES: %q must be increasing multiples of 5000", field)
		}
		out = append(out, n)
	}
	return out
}

func c001PostID(n int) string { return fmt.Sprintf("chatscale-p-%010d", n) }

func c001Channel(n int) string { return fmt.Sprintf("chatscale-c-%03d", n) }

// c001Run runs one load statement under the tenant setting. Durability is
// relaxed for the load only; the measured statements run with the server's own
// settings.
func c001Run(t *testing.T, s *Store, sql string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL synchronous_commit=off`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SET LOCAL work_mem='64MB'`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, sql, args...)
		return err
	}); err != nil {
		t.Fatalf("load statement failed: %v\n%s", err, sql)
	}
}

// c001SeedStatic creates the channels and who belongs to them: the reader is in
// every channel, three channels hold every person, and each other person is in
// five channels, so channel size is skewed the way post volume is.
func c001SeedStatic(t *testing.T, s *Store, channels, members int) {
	t.Helper()
	c001Run(t, s, `INSERT INTO chat_conversation(id,tenant_id,kind,name,owner_id)
 SELECT 'chatscale-c-'||lpad(g::text,3,'0'),$1,'PUBLIC_CHANNEL','Channel '||g,$2
 FROM generate_series(0,$3::int-1) g`, chatscaleTenant, chatscaleReader, channels)
	c001Run(t, s, `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,joined_at)
 SELECT $1,'chatscale-c-'||lpad(x.ch::text,3,'0'),$1,x.mid,'2020-01-01'::timestamptz FROM (
  SELECT g AS ch,$2::text AS mid FROM generate_series(0,$3::int-1) g
  UNION ALL
  SELECT (m+k*37)%$3::int,'chatscale-u-'||m FROM generate_series(1,$4::int-1) m,generate_series(0,4) k
  UNION ALL
  SELECT ch,'chatscale-u-'||m FROM generate_series(0,2) ch,generate_series(1,$4::int-1) m
 ) x ON CONFLICT DO NOTHING`, chatscaleTenant, chatscaleReader, channels, members)
}

// c001PostsSQL loads posts from+1 .. to. A group of five consecutive numbers
// shares a channel and the fifth replies to the first, so 20 percent of posts
// are thread replies. The channel is a cubed hash fraction: with 300 channels
// the busiest holds about 15 percent of the posts and the ten busiest about 35.
// Sequence numbers continue from the channel's current counter.
const c001PostsSQL = `WITH g AS (
 SELECT x AS g,(x-1)/5 AS grp FROM generate_series($2::bigint+1,$3::bigint) x
), c AS (
 SELECT g,grp,LEAST($4::int-1,floor($4::int*power(((grp*2654435761)%4294967296)/4294967296.0,3))::int) AS ch FROM g
), numbered AS (
 SELECT g,grp,ch,row_number() OVER (PARTITION BY ch ORDER BY g) AS rn FROM c
) INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,parent_id,references_json,revision,tombstoned,created_at,updated_at)
 SELECT 'chatscale-p-'||lpad(n.g::text,10,'0'),$1,'chatscale-c-'||lpad(n.ch::text,3,'0'),
 'chatscale-u-'||(1+(n.grp*31)%($5::int-1)),$1,
 COALESCE(cv.post_sequence,0)+n.rn,
 CASE WHEN n.g%101=0 THEN '' ELSE 'Payroll review: '||substr(repeat('regional team confirmed the onboarding schedule for the quarter. ',3),1,(40+(n.g%4)*30)::int)||' #'||n.g||CASE WHEN n.g%997=0 THEN ' zebrafish' ELSE '' END END,
 CASE WHEN n.g%5=0 THEN 'chatscale-p-'||lpad((n.g-4)::text,10,'0') ELSE '' END,
 CASE WHEN n.g%13=0 THEN jsonb_build_array(jsonb_build_object('Kind','PERSON_MENTION','TenantID',$1::text,'ID',$6::text))
 WHEN n.g%19=0 THEN jsonb_build_array(jsonb_build_object('Kind','ARTIFACT','TenantID',$1::text,'ID','chatscale-file-'||n.g))
 ELSE '[]'::jsonb END,
 CASE WHEN n.g%17=0 THEN 2 ELSE 1 END,n.g%101=0,
 '2024-01-01'::timestamptz+n.g*interval '20 seconds',
 '2024-01-01'::timestamptz+n.g*interval '20 seconds'+CASE WHEN n.g%17=0 THEN interval '1 day' ELSE interval '0' END
 FROM numbered n LEFT JOIN chat_conversation cv ON cv.tenant_id=$1 AND cv.id='chatscale-c-'||lpad(n.ch::text,3,'0')`

// c001SeedPosts appends posts from+1 .. to in chunks of at most 250,000 and the
// rows that travel with them: revisions, reactions, pins, an outbox window and
// the reader's read cursors. Seventy percent of channels are read up to the last
// twenty to fifty posts; the rest have a backlog of three quarters of their
// history, so some channels exceed the 5,000 unread display limit.
func c001SeedPosts(t *testing.T, s *Store, from, to, channels, members, outboxKeep int) {
	t.Helper()
	for lo := from; lo < to; lo += 250000 {
		hi := min(lo+250000, to)
		first, last := c001PostID(lo), c001PostID(hi)
		c001Run(t, s, c001PostsSQL, chatscaleTenant, lo, hi, channels, members, chatscaleReader)
		c001Run(t, s, `UPDATE chat_conversation c SET post_sequence=m.mx
 FROM (SELECT conversation_id,max(sequence) mx FROM chat_post WHERE tenant_id=$1 GROUP BY conversation_id) m
 WHERE c.tenant_id=$1 AND c.id=m.conversation_id`, chatscaleTenant)
		c001Run(t, s, `INSERT INTO chat_post_revision(tenant_id,post_id,revision,author_id,body,parent_id,references_json,tombstoned,created_at)
 SELECT tenant_id,id,1,author_id,body,parent_id,references_json,tombstoned,created_at FROM chat_post
 WHERE tenant_id=$1 AND id>$2 AND id<=$3`, chatscaleTenant, first, last)
		c001Run(t, s, `INSERT INTO chat_post_revision(tenant_id,post_id,revision,author_id,body,parent_id,references_json,tombstoned,created_at)
 SELECT tenant_id,id,2,author_id,body,parent_id,references_json,tombstoned,updated_at FROM chat_post
 WHERE tenant_id=$1 AND id>$2 AND id<=$3 AND revision=2`, chatscaleTenant, first, last)
		c001Run(t, s, `INSERT INTO chat_reaction(tenant_id,home_tenant_id,post_id,member_id,emoji)
 SELECT tenant_id,tenant_id,id,$4,'thumbsup' FROM chat_post WHERE tenant_id=$1 AND id>$2 AND id<=$3 AND sequence%7=0 AND NOT tombstoned`,
			chatscaleTenant, first, last, chatscaleReader)
		c001Run(t, s, `INSERT INTO chat_reaction(tenant_id,home_tenant_id,post_id,member_id,emoji)
 SELECT tenant_id,tenant_id,id,'chatscale-u-2','heart' FROM chat_post WHERE tenant_id=$1 AND id>$2 AND id<=$3 AND sequence%11=0 AND NOT tombstoned`,
			chatscaleTenant, first, last)
		c001Run(t, s, `INSERT INTO chat_pin(tenant_id,home_tenant_id,conversation_id,post_id,member_id,created_at)
 SELECT tenant_id,tenant_id,conversation_id,id,$4,created_at FROM chat_post WHERE tenant_id=$1 AND id>$2 AND id<=$3 AND sequence%200=0 AND NOT tombstoned`,
			chatscaleTenant, first, last, chatscaleReader)
	}
	// The outbox holds only the newest window: delivered events are pruned by the
	// retention job, so its size follows the backlog and not the history.
	var firstOutbox int64
	if err := s.RunTenantTx(context.Background(), chatscaleTenant, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT COALESCE(max(id),0) FROM chat_outbox`).Scan(&firstOutbox)
	}); err != nil {
		t.Fatal(err)
	}
	keep := min(outboxKeep, to-from)
	c001Run(t, s, `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload,created_at)
 SELECT tenant_id,id,'post.created',jsonb_build_object(
 'ConversationID',conversation_id,'ActorID',author_id,'ActorHomeTenantID',author_home_tenant_id,
 'TargetID',id,'Revision',revision,'PolicyRevision',1,'EventSequence',sequence,'SchemaVersion',1,
 'CorrelationID',id,'Value',jsonb_build_object('ID',id,'TenantID',tenant_id,'ConversationID',conversation_id,
 'AuthorID',author_id,'AuthorHomeTenantID',author_home_tenant_id,'Sequence',sequence,
 'Body',body,'ParentID',parent_id,'References',references_json,'Revision',revision,
 'Tombstoned',tombstoned,'CreatedAt',created_at)),created_at
 FROM chat_post WHERE tenant_id=$1 AND id>$2 AND id<=$3 ORDER BY id`, chatscaleTenant, c001PostID(to-keep), c001PostID(to))
	c001Run(t, s, `INSERT INTO chat_outbox_receipt(tenant_id,outbox_id)
 SELECT tenant_id,id FROM chat_outbox WHERE tenant_id=$1 AND id>$2 ORDER BY id LIMIT $3`, chatscaleTenant, firstOutbox, keep*4/5)
	c001Run(t, s, `INSERT INTO chat_cursor(tenant_id,home_tenant_id,member_id,conversation_id,last_sequence,updated_at)
 SELECT $1,$1,$2,c.id,GREATEST(0,c.post_sequence-CASE WHEN right(c.id,3)::int%10<7 THEN 20+right(c.id,3)::int%30 ELSE c.post_sequence*3/4 END),
 '2024-01-01'::timestamptz+($3::bigint*9/10)*interval '20 seconds' FROM chat_conversation c WHERE c.tenant_id=$1
 ON CONFLICT (tenant_id,home_tenant_id,member_id,conversation_id) DO UPDATE SET last_sequence=EXCLUDED.last_sequence,updated_at=EXCLUDED.updated_at`,
		chatscaleTenant, chatscaleReader, to)
}

// c001Maintain does what a maintained database would have done by now: the
// visibility map and the statistics are current before anything is measured.
func c001Maintain(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.pool.Exec(context.Background(), `VACUUM (ANALYZE) chat_conversation,chat_membership,chat_post,chat_post_revision,chat_reaction,chat_pin,chat_cursor,chat_outbox,chat_outbox_receipt,chatscale_read_state`); err != nil {
		t.Fatal(err)
	}
}
