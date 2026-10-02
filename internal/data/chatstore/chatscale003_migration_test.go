package chatstore

import (
	"context"
	"io/fs"
	"net/url"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

// Migration 00046 goes down and up again on a database that holds cached badges,
// posts and the indexes it touches: down restores the revision-fenced insert
// trigger and the dropped index and removes what it added; up puts the delta
// back, and a recount after the round trip equals the bounded scan.
func TestTodo_CHATSCALE_003_Integration_MigrationRoundTrip(t *testing.T) {
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = p.Up(ctx); err != nil {
		t.Fatalf("chat migrations: %v", err)
	}
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", db.Schema)
	u.RawQuery = q.Encode()
	s, err := New(ctx, Config{DSN: u.String(), CoreDSN: "postgres://core:pw@127.0.0.1:1/core"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)

	w := &chatscale003World{t: t, s: s, posts: map[string][]string{}, rooms: []string{"c3-room-0"}}
	w.exec(`INSERT INTO chat_conversation(id,tenant_id,kind,name,owner_id) VALUES('c3-room-0',$1,'PUBLIC_CHANNEL','c3',$2)`, chatscaleTenant, chatscaleReader)
	w.exec(`INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,joined_at) VALUES($1,'c3-room-0',$1,$2,'2024-01-01')`, chatscaleTenant, chatscaleReader)
	w.exec(`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('m-1',$1,'c3-room-0','other',$1,1,'hello'),('m-2',$1,'c3-room-0','other',$1,2,'again')`, chatscaleTenant)
	r := NewRecipientStateStore(s)
	read := func(label string) uint64 {
		t.Helper()
		got, err := r.ChatscaleSidebarCounts(ctx, chatscaleTenant, chatscaleTenant, chatscaleReader, w.rooms)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		return got["c3-room-0"].Unread
	}
	if n := read("before"); n != 2 {
		t.Fatalf("unread before=%d want 2", n)
	}
	indexes := func() map[string]bool {
		t.Helper()
		out := map[string]bool{}
		if err := s.RunTx(ctx, func(tx dbport.Tx) error {
			rows, err := tx.Query(ctx, `SELECT indexname FROM pg_indexes WHERE schemaname=current_schema() AND tablename='chat_post'`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					return err
				}
				out[name] = true
			}
			return rows.Err()
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := indexes(); got["chat_post_history"] || !got["chatscale_system_lines"] || !got["chatscale_mention_posts"] || !got["chat_post_touched"] {
		t.Fatalf("indexes after up: %v", got)
	}
	if _, err := p.Down(ctx); err != nil {
		t.Fatalf("down: %v", err)
	}
	if got := indexes(); !got["chat_post_history"] || got["chatscale_system_lines"] || got["chatscale_mention_posts"] || !got["chat_post_touched"] {
		t.Fatalf("indexes after down: %v", got)
	}
	// Back on the revision-fenced schema a send must still move the badge.
	w.exec(`UPDATE chat_conversation SET post_sequence=3 WHERE tenant_id=$1 AND id='c3-room-0'`, chatscaleTenant)
	w.exec(`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('m-3',$1,'c3-room-0','other',$1,3,'during down')`, chatscaleTenant)
	if n := read("after down"); n != 3 {
		t.Fatalf("unread after down=%d want 3", n)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if got := indexes(); got["chat_post_history"] || !got["chatscale_system_lines"] || !got["chatscale_mention_posts"] {
		t.Fatalf("indexes after second up: %v", got)
	}
	want := chatscale003Oracle(t, s, w.rooms, countScanLimit)
	if n := read("after up"); n != want["c3-room-0"].Unread || n != 3 {
		t.Fatalf("unread after up=%d scan=%v", n, want)
	}
	w.exec(`UPDATE chat_conversation SET post_sequence=4 WHERE tenant_id=$1 AND id='c3-room-0'`, chatscaleTenant)
	w.exec(`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('m-4',$1,'c3-room-0','other',$1,4,'after up')`, chatscaleTenant)
	if n := read("delta"); n != 4 {
		t.Fatalf("unread through the delta=%d want 4", n)
	}
}
