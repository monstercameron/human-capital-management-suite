package chatstore

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func chatscaleRooms(n int) []string {
	rooms := make([]string, n)
	for i := range rooms {
		rooms[i] = fmt.Sprintf("chatscale-c-%03d", i)
	}
	return rooms
}

func chatscaleAssertBudget(t *testing.T, name string, budget float64, run func() error) {
	t.Helper()
	if err := run(); err != nil {
		t.Fatal(err)
	}
	samples := make([]float64, 20)
	for i := range samples {
		start := time.Now()
		if err := run(); err != nil {
			t.Fatal(err)
		}
		samples[i] = float64(time.Since(start).Microseconds()) / 1000
	}
	p95 := chatscalePercentile(samples, .95)
	t.Logf("BUDGET %s p50=%.3fms p95=%.3fms p99=%.3fms limit=%.0fms", name, chatscalePercentile(samples, .5), p95, chatscalePercentile(samples, .99), budget)
	if p95 >= budget {
		t.Fatalf("%s p95=%.3fms exceeds %.0fms", name, p95, budget)
	}
}

func TestTodo_CHATSCALE_002(t *testing.T) {
	s, _ := chatFixture(t)
	err := s.RunTx(context.Background(), func(tx dbport.Tx) error {
		var valid int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname LIKE 'chatscale_%' AND i.indisvalid`).Scan(&valid); err != nil {
			return err
		}
		// Twelve from migrations 00034 to 00038, with the read-state primary key,
		// and the two partial indexes of 00046 behind the first-open count.
		if valid != 14 {
			return fmt.Errorf("valid hot-read indexes=%d want 14 including read-state primary key", valid)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	migration, err := Migrations.ReadFile("migrations/00034_chatscale_hot_reads.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(migration), "-- +goose NO TRANSACTION") || strings.Contains(string(migration), "to_tsvector") {
		t.Fatal("hot-read migration lost online index building or duplicates search index")
	}
}

func TestTodo_CHATSCALE_002_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, 1000)
	a := NewAdapter(s)
	ctx := context.Background()
	p := chat.Principal{TenantID: chatscaleTenant, SubjectID: chatscaleReader}
	page, err := a.ListPosts(ctx, p, chatscaleTenant, chatscaleRoom, 0, chat.Page{PageSize: 7}, chat.PostWindow{Descending: true})
	if err != nil || len(page.Posts) != 7 || page.NextCursor == "" {
		t.Fatalf("first=%+v err=%v", page, err)
	}
	older, err := a.ListPosts(ctx, p, chatscaleTenant, chatscaleRoom, 0, chat.Page{PageSize: 7, Cursor: page.NextCursor}, chat.PostWindow{Descending: true})
	if err != nil || len(older.Posts) != 7 {
		t.Fatalf("older=%+v err=%v", older, err)
	}
	seen := map[string]bool{}
	for _, post := range page.Posts {
		seen[post.ID] = true
	}
	for _, post := range older.Posts {
		if seen[post.ID] {
			t.Fatalf("duplicate at page edge: %s", post.ID)
		}
	}
	// A revoked caller cannot obtain posts through a backward cursor.
	if err := s.execTenant(ctx, chatscaleTenant, `UPDATE chat_membership SET state='removed' WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND member_id=$3`, chatscaleTenant, chatscaleRoom, chatscaleReader); err != nil {
		t.Fatal(err)
	}
	denied, err := a.ListPosts(ctx, p, chatscaleTenant, chatscaleRoom, 0, chat.Page{PageSize: 7, Cursor: page.NextCursor}, chat.PostWindow{Descending: true})
	if err != nil || len(denied.Posts) != 0 {
		t.Fatalf("revoked page=%v err=%v", denied, err)
	}
}

func TestTodo_CHATSCALE_002_Property(t *testing.T) {
	s, _ := chatFixture(t)
	chatscaleSeed(t, s, 1000)
	a := NewAdapter(s)
	ctx := context.Background()
	p := chat.Principal{TenantID: chatscaleTenant, SubjectID: chatscaleReader}
	var before []chat.ListPostsResponse
	for _, edge := range []uint64{math.MaxInt64, 65, 40, 20, 2} {
		page, err := a.ListPosts(ctx, p, chatscaleTenant, chatscaleRoom, 0, chat.Page{PageSize: 11}, chat.PostWindow{Descending: true, BeforeSequence: edge})
		if err != nil {
			t.Fatal(err)
		}
		before = append(before, page)
	}
	chatscaleIndexes(t, s, false)
	for i, edge := range []uint64{math.MaxInt64, 65, 40, 20, 2} {
		page, err := a.ListPosts(ctx, p, chatscaleTenant, chatscaleRoom, 0, chat.Page{PageSize: 11}, chat.PostWindow{Descending: true, BeforeSequence: edge})
		if err != nil || !reflect.DeepEqual(page, before[i]) {
			t.Fatalf("edge=%d optimized=%v baseline=%v err=%v", edge, before[i], page, err)
		}
	}
}

func TestTodo_CHATSCALE_002_Security(t *testing.T) {
	s, schema := chatFixture(t)
	chatscaleSeed(t, s, 1000)
	ctx := context.Background()
	role := createChatRLSRole(t, s, schema)
	r := NewRecipientStateStore(s)
	if _, err := r.ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, []string{chatscaleRoom}); err != nil {
		t.Fatal(err)
	}
	if err := runChatAsTenantRole(ctx, s, role, "foreign", func(tx dbport.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chatscale_read_state`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("foreign tenant read %d cached badges", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := runChatAsTenantRole(ctx, s, role, "foreign", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chatscale_read_state SET unread=0 WHERE tenant_id=$1`, chatscaleTenant)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err := runChatAsTenantRole(ctx, s, role, "foreign", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chatscale_read_state(tenant_id,home_tenant_id,member_id,conversation_id,
 history_revision,member_revision,history_visibility,joined_at,read_revision,last_sequence,read_at,unread,mentions)
 VALUES($1,$1,'chatscale-forged',$2,0,1,'FULL_HISTORY','2020-01-01',1,0,'epoch',1,0)`, chatscaleTenant, chatscaleRoom)
		return err
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "row-level security") {
		t.Fatalf("foreign cache insert error=%v; want row-level security rejection", err)
	}
	got, err := r.Counts(ctx, chatrecipient.Identity{HostTenantID: chatscaleTenant, HomeTenantID: chatscaleTenant, SubjectID: chatscaleReader, ConversationID: chatscaleRoom})
	if err != nil || got.Unread == 0 {
		t.Fatalf("foreign update changed badge=%v err=%v", got, err)
	}
	// Current membership is still checked when a valid cached badge exists.
	counts, err := r.ChatscaleSidebarCounts(ctx, chatscaleTenant, "foreign", chatscaleReader, []string{chatscaleRoom})
	if err != nil || len(counts) != 0 {
		t.Fatalf("foreign home read=%v err=%v", counts, err)
	}
}

func TestTodo_CHATSCALE_002_Fault(t *testing.T) {
	s, _ := chatFixture(t)
	// An interrupted batch is atomic; a failed index step can be retried without
	// changing history. Resume the lane's online migration from a partial set.
	chatscaleIndexes(t, s, false)
	if err := s.RunTx(context.Background(), func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `CREATE INDEX chatscale_activity ON chat_post(tenant_id,conversation_id,created_at DESC,id DESC) WHERE NOT tombstoned`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTx(context.Background(), func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `DROP INDEX chatscale_activity`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	chatscaleIndexes(t, s, true)
	if err := s.RunTx(context.Background(), func(tx dbport.Tx) error {
		var valid bool
		err := tx.QueryRow(context.Background(), `SELECT bool_and(i.indisvalid) FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname LIKE 'chatscale_%'`).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return fmt.Errorf("resumed index build left invalid indexes")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_CHATSCALE_002_Performance(t *testing.T) {
	s, resumed := chatscaleVolumeFixture(t)
	if !resumed {
		chatscaleSeed(t, s, chatscaleSize())
	}
	chatscaleHistoryBudget(t, s)
}

func chatscaleHistoryBudget(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	a := NewAdapter(s)
	p := chat.Principal{TenantID: chatscaleTenant, SubjectID: chatscaleReader}
	for _, window := range []struct {
		name string
		edge uint64
	}{{"open-channel", 0}, {"page-back", uint64(chatscaleSize() * 3 / 5 / 8 / 4)}} {
		chatscaleAssertBudget(t, window.name, 50, func() error {
			page, err := a.ListPosts(ctx, p, chatscaleTenant, chatscaleRoom, 0, chat.Page{PageSize: 50}, chat.PostWindow{Descending: true, BeforeSequence: window.edge})
			if err != nil {
				return err
			}
			if len(page.Posts) != 50 {
				return fmt.Errorf("%s returned %d posts", window.name, len(page.Posts))
			}
			return nil
		})
	}
	n := 0
	chatscaleAssertBudget(t, "send", 30, func() error {
		n++
		post, err := s.sendPostRaw(ctx, SendRequest{TenantID: chatscaleTenant, ConversationID: chatscaleRoom, AuthorID: chatscaleReader, ClientKey: fmt.Sprintf("chatscale-budget-%d-%d", time.Now().UnixNano(), n), Body: "Payroll follow-up", References: []byte("[]")})
		if err != nil {
			return err
		}
		if post.Sequence == 0 {
			return fmt.Errorf("send lost sequence")
		}
		return nil
	})
}

func TestTodo_CHATSCALE_003_Performance(t *testing.T) {
	s, resumed := chatscaleVolumeFixture(t)
	if !resumed {
		chatscaleSeed(t, s, chatscaleSize())
	}
	chatscaleSidebarBudget(t, s)
}

func chatscaleSidebarBudget(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	// The volume fixture has 200 rooms. Add 100 empty rooms to exercise the
	// sidebar's specified 300-membership budget, including zero-count entries.
	if err := s.execTenant(ctx, chatscaleTenant, `INSERT INTO chat_conversation(id,tenant_id,kind,owner_id) SELECT 'chatscale-c-'||g,$1,'PUBLIC_CHANNEL',$2 FROM generate_series(200,299) g ON CONFLICT DO NOTHING`, chatscaleTenant, chatscaleReader); err != nil {
		t.Fatal(err)
	}
	if err := s.execTenant(ctx, chatscaleTenant, `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id) SELECT $1,id,$1,$2 FROM chat_conversation WHERE tenant_id=$1 ON CONFLICT DO NOTHING`, chatscaleTenant, chatscaleReader); err != nil {
		t.Fatal(err)
	}
	r := NewRecipientStateStore(s)
	rooms := chatscaleRooms(300)
	chatscaleAssertBudget(t, "sidebar-300", 80, func() error {
		got, err := r.ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, rooms)
		if err != nil {
			return err
		}
		if len(got) != 300 {
			return fmt.Errorf("sidebar rows=%d want 300", len(got))
		}
		return nil
	})
	if err := s.RunTenantTx(ctx, chatscaleTenant, func(tx dbport.Tx) error {
		var raw string
		if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+chatscaleCachedSQL, chatscaleTenant, chatscaleTenant, chatscaleReader, rooms, countScanLimit).Scan(&raw); err != nil {
			return err
		}
		var plans []chatscalePlan
		if err := json.Unmarshal([]byte(raw), &plans); err != nil {
			return err
		}
		var walk func(chatscaleNode) error
		walk = func(n chatscaleNode) error {
			if n.Relation == "chat_post" && n.Loops > 0 {
				return fmt.Errorf("warm sidebar reads chat history: %s", raw)
			}
			for _, c := range n.Children {
				if err := walk(c); err != nil {
					return err
				}
			}
			return nil
		}
		return walk(plans[0].Plan)
	}); err != nil {
		t.Fatal(err)
	}
}
