package chatstore

import (
	"context"
	"flag"
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var chatscaleLarge = flag.Bool("chatscale.large", false, "seed two million chat posts instead of 50,000")

const chatscaleTenant = "chatscale-tenant"
const chatscaleReader = "chatscale-reader"
const chatscaleRoom = "chatscale-c-000"

func chatscaleSize() int {
	if *chatscaleLarge {
		return 2000000
	}
	return 50000
}

func chatscaleVacuum(t *testing.T, s *Store) float64 {
	t.Helper()
	start := time.Now()
	// Bulk-loading a fresh schema leaves its visibility map empty. Measure the
	// maintenance explicitly before comparing plans; both phases use the same
	// map, rather than granting an index-only plan just to the optimized phase.
	if _, err := s.pool.Exec(context.Background(), `VACUUM (ANALYZE) chat_post,chat_pin,chat_reaction`); err != nil {
		t.Fatal(err)
	}
	ms := float64(time.Since(start).Microseconds()) / 1000
	t.Logf("VACUUM post/pin/reaction time=%.3fms", ms)
	return ms
}

func chatscaleSeedOutbox(t *testing.T, s *Store, posts int) {
	t.Helper()
	ctx := context.Background()
	run := func(sql string, args ...any) {
		t.Helper()
		if err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	run(`INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload,created_at)
 SELECT tenant_id,id,'post.created',jsonb_build_object(
 'ConversationID',conversation_id,'ActorID',author_id,'ActorHomeTenantID',author_home_tenant_id,
 'TargetID',id,'Revision',revision,'PolicyRevision',1,'EventSequence',sequence,'SchemaVersion',1,
 'CorrelationID',id,'Value',jsonb_build_object('ID',id,'TenantID',tenant_id,'ConversationID',conversation_id,
 'AuthorID',author_id,'AuthorHomeTenantID',author_home_tenant_id,'Sequence',sequence,
 'Body',body,'ParentID',parent_id,'References',references_json,'Revision',revision,
 'Tombstoned',tombstoned,'CreatedAt',created_at)),created_at
 FROM chat_post WHERE tenant_id=$1`, chatscaleTenant)
	run(`INSERT INTO chat_outbox_receipt(tenant_id,outbox_id)
 SELECT tenant_id,id FROM chat_outbox WHERE tenant_id=$1 ORDER BY id LIMIT $2`, chatscaleTenant, posts*4/5)
	run(`INSERT INTO chat_outbox_cursor(tenant_id,consumer,last_outbox_id)
 SELECT $1,'chatscale-fixture',max(outbox_id) FROM chat_outbox_receipt WHERE tenant_id=$1
 ON CONFLICT(tenant_id,consumer) DO UPDATE SET last_outbox_id=EXCLUDED.last_outbox_id`, chatscaleTenant)
}

// chatscaleSeed uses deterministic IDs and set-based batches, rather than two
// million network round trips. Sixty percent of posts land in eight channels;
// the remainder land in 192 small channels. Every fifth post is a thread reply.
// Inserts still run under the ordinary tenant setting and all database triggers.
func chatscaleSeed(t *testing.T, s *Store, posts int) {
	t.Helper()
	if posts < 1000 || posts%1000 != 0 {
		t.Fatal("volume fixture needs a positive multiple of 1,000 posts")
	}
	ctx := context.Background()
	run := func(sql string, args ...any) {
		t.Helper()
		if err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	run(`INSERT INTO chat_conversation(id,tenant_id,kind,name,owner_id)
 SELECT 'chatscale-c-'||lpad(g::text,3,'0'),$1,'PUBLIC_CHANNEL','Operations '||g,$2
 FROM generate_series(0,199) g`, chatscaleTenant, chatscaleReader)
	// 2,000 distinct people, with the reader in every channel and ten members
	// in each channel (one of those is the reader in channel zero).
	run(`INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,joined_at)
 SELECT $1,'chatscale-c-'||lpad((g/10)::text,3,'0'),$1,
 CASE WHEN g=0 THEN $2 ELSE 'chatscale-u-'||g END,'2020-01-01'::timestamptz
 FROM generate_series(0,1999) g`, chatscaleTenant, chatscaleReader)
	run(`INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,joined_at)
 SELECT $1,id,$1,$2,'2020-01-01'::timestamptz FROM chat_conversation
 WHERE tenant_id=$1 ON CONFLICT DO NOTHING`, chatscaleTenant, chatscaleReader)
	// The room formula gives neighboring rows in a thread the same channel.
	// Sequence is dense per channel, but IDs remain stable across fixture sizes.
	const insert = `WITH distributed AS (
 SELECT g,CASE WHEN g<=$4*3/5 THEN ((g-1)/5)%8 ELSE 8+((g-1)/5)%192 END room
 FROM generate_series($2::int,$3::int) g
 ) INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,
 sequence,body,parent_id,references_json,revision,tombstoned,created_at,updated_at)
 SELECT 'chatscale-p-'||lpad(g::text,10,'0'),$1,'chatscale-c-'||lpad(room::text,3,'0'),
 'chatscale-u-'||(room*10+1),$1,
 CASE WHEN g<=$4*3/5 THEN ((g-1)/40)*5+(g-1)%5+1
 ELSE ((g-1-$4*3/5)/960)*5+(g-1)%5+1 END,
 CASE WHEN g%101=0 THEN '' ELSE
 'Payroll review: regional team confirmed the onboarding schedule. '||repeat('Please check the attached operating notes. ',6)||g END,
 CASE WHEN g%5=0 THEN 'chatscale-p-'||lpad((g-4)::text,10,'0') ELSE '' END,
 CASE WHEN g%13=0 THEN jsonb_build_array(jsonb_build_object('Kind','PERSON_MENTION','TenantID',$1::text,'ID',$5::text))
 WHEN g%19=0 THEN jsonb_build_array(jsonb_build_object('Kind','ARTIFACT','TenantID',$1::text,'ID','chatscale-file-'||g))
 ELSE '[]'::jsonb END,
 CASE WHEN g%17=0 THEN 2 ELSE 1 END,g%101=0,
 '2024-01-01'::timestamptz+g*interval '20 seconds',
 '2024-01-01'::timestamptz+g*interval '20 seconds'+CASE WHEN g%17=0 THEN interval '1 day' ELSE interval '0' END
 FROM distributed`
	// Batch boundaries align with the five-post thread groups.
	for start := 1; start <= posts; start += 10000 {
		run(insert, chatscaleTenant, start, min(start+9999, posts), posts, chatscaleReader)
	}
	run(`INSERT INTO chat_post_revision(tenant_id,post_id,revision,author_id,body,parent_id,references_json,tombstoned,created_at)
 SELECT tenant_id,id,1,author_id,body,parent_id,references_json,tombstoned,created_at FROM chat_post WHERE tenant_id=$1`, chatscaleTenant)
	run(`INSERT INTO chat_post_revision(tenant_id,post_id,revision,author_id,body,parent_id,references_json,tombstoned,created_at)
	 SELECT tenant_id,id,2,author_id,body,parent_id,references_json,tombstoned,updated_at FROM chat_post WHERE tenant_id=$1 AND revision=2`, chatscaleTenant)
	run(`INSERT INTO chat_reaction(tenant_id,home_tenant_id,post_id,member_id,emoji)
 SELECT tenant_id,tenant_id,id,$2,'thumbsup' FROM chat_post WHERE tenant_id=$1 AND sequence%7=0 AND NOT tombstoned`, chatscaleTenant, chatscaleReader)
	run(`INSERT INTO chat_pin(tenant_id,home_tenant_id,conversation_id,post_id,member_id,created_at)
	 SELECT tenant_id,tenant_id,conversation_id,id,$2,created_at FROM chat_post WHERE tenant_id=$1 AND sequence%31=0 AND NOT tombstoned`, chatscaleTenant, chatscaleReader)
	run(`INSERT INTO chat_cursor(tenant_id,home_tenant_id,member_id,conversation_id,last_sequence,updated_at)
 SELECT $1,$1,$2,conversation_id,max(sequence)*3/4,max(created_at)-interval '1 day'
 FROM chat_post WHERE tenant_id=$1 GROUP BY conversation_id`, chatscaleTenant, chatscaleReader)
	run(`UPDATE chat_conversation c SET post_sequence=COALESCE((SELECT max(sequence) FROM chat_post p WHERE p.tenant_id=c.tenant_id AND p.conversation_id=c.id),0) WHERE c.tenant_id=$1`, chatscaleTenant)
	chatscaleSeedOutbox(t, s, posts)
	if err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.saved_home_tenant_id',$1,true),set_config('hcmnext.saved_person_id',$2,true)`, chatscaleTenant, chatscaleReader); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO chat_saved_item(tenant_id,host_tenant_id,home_tenant_id,person_id,conversation_id,post_id,created_at)
 SELECT tenant_id,tenant_id,tenant_id,$2,conversation_id,id,created_at FROM chat_post
 WHERE tenant_id=$1 AND NOT tombstoned ORDER BY created_at DESC LIMIT 200`, chatscaleTenant, chatscaleReader)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// ANALYZE is necessary after bulk loading; otherwise these measurements
	// primarily measure the planner's empty-table assumptions.
	for _, table := range []string{"chat_conversation", "chat_membership", "chat_post", "chat_post_revision", "chat_reaction", "chat_pin", "chat_cursor", "chat_saved_item", "chat_outbox", "chat_outbox_receipt"} {
		if err := s.RunTx(ctx, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, "ANALYZE "+table)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("fixture: tenant=1 conversations=200 members=2000 posts=%d; 60%% in eight rooms, 20%% replies, 1/17 edits, 1/101 tombstones, 1/13 mentions, 1/19 attachments", posts)
}

func TestTodo_CHATSCALE_001(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, 2000)
	err := s.RunTenantTx(context.Background(), chatscaleTenant, func(tx dbport.Tx) error {
		var posts, rooms, people, replies, revisions, reactions, pins, outbox int
		if err := tx.QueryRow(context.Background(), `SELECT
 (SELECT count(*) FROM chat_post),(SELECT count(*) FROM chat_conversation),
 (SELECT count(DISTINCT member_id) FROM chat_membership),
 (SELECT count(*) FROM chat_post WHERE parent_id<>''),
 (SELECT count(*) FROM chat_post_revision),(SELECT count(*) FROM chat_reaction),
 (SELECT count(*) FROM chat_pin),(SELECT count(*) FROM chat_outbox)`).Scan(&posts, &rooms, &people, &replies, &revisions, &reactions, &pins, &outbox); err != nil {
			return err
		}
		if posts != 2000 || rooms != 200 || people != 2000 || replies != 400 || revisions != 2117 || reactions == 0 || pins == 0 || outbox != posts {
			return fmt.Errorf("fixture shape: posts=%d rooms=%d people=%d replies=%d revisions=%d reactions=%d pins=%d outbox=%d", posts, rooms, people, replies, revisions, reactions, pins, outbox)
		}
		var broken int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM chat_post p LEFT JOIN chat_post root ON root.tenant_id=p.tenant_id AND root.conversation_id=p.conversation_id AND root.id=p.parent_id WHERE p.parent_id<>'' AND root.id IS NULL`).Scan(&broken); err != nil {
			return err
		}
		if broken != 0 {
			return fmt.Errorf("%d replies have missing or foreign-channel roots", broken)
		}
		var malformed int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM chat_outbox WHERE
 payload->>'ConversationID' IS NULL OR payload->>'TargetID' IS NULL OR payload->'Value' IS NULL`).Scan(&malformed); err != nil {
			return err
		}
		if malformed != 0 {
			return fmt.Errorf("%d outbox events lack the production envelope", malformed)
		}
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM chat_post p LEFT JOIN chat_post_revision r
 ON r.tenant_id=p.tenant_id AND r.post_id=p.id AND r.revision=p.revision WHERE
 r.post_id IS NULL OR (r.body,r.parent_id,r.references_json,r.tombstoned) IS DISTINCT FROM (p.body,p.parent_id,p.references_json,p.tombstoned)`).Scan(&malformed); err != nil {
			return err
		}
		if malformed != 0 {
			return fmt.Errorf("%d posts disagree with their current revision", malformed)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
