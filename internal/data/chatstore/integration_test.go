package chatstore

import (
	"context"
	"io/fs"
	"net/url"
	"sync"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

func chatFixture(t *testing.T) (*Store, string) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Up(context.Background()); err != nil {
		t.Fatalf("chat migrations: %v", err)
	}
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", db.Schema)
	u.RawQuery = q.Encode()
	s, err := New(context.Background(), Config{DSN: u.String(), CoreDSN: "postgres://core:pw@127.0.0.1:1/core"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, db.Schema
}

// seedConversationRow writes a conversation and its members directly. The raw
// repository no longer carries a create helper of its own, because the adapter
// owns the create path; this keeps the raw send-path tests independent of the
// contract translation without leaving a parallel writer in the package.
func seedConversationRow(t *testing.T, s *Store, c Conversation, members []Membership) {
	t.Helper()
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, c.TenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO chat_conversation(id,tenant_id,kind,name,description,owner_id,settings_revision,lifecycle,route_shard,route_epoch,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,1,now()) ON CONFLICT (id) DO NOTHING`, c.ID, c.TenantID, c.Kind, c.Name, c.Description, c.OwnerID, c.SettingsRevision, c.Lifecycle, c.RouteShard); err != nil {
			return err
		}
		for _, m := range members {
			home := m.HomeTenantID
			if home == "" {
				home = c.TenantID
			}
			if _, err := tx.Exec(ctx, `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,role,state) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, c.TenantID, c.ID, home, m.MemberID, m.Role, m.State); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func seedConversation(t *testing.T, s *Store, tenantID string) {
	t.Helper()
	seedConversationRow(t, s, Conversation{ID: "c-1", TenantID: tenantID, Kind: "PUBLIC_CHANNEL", OwnerID: "u-1", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "u-1", Role: "manager", State: "active"}})
}

func TestTodo_CHAT_003_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	if _, err := s.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", ConversationID: "c-1", AuthorID: "u-1", ClientKey: "k-1", Body: "hello"}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_CHAT_004_Security(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	if _, err := s.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-b", ConversationID: "c-1", AuthorID: "u-1", ClientKey: "other", Body: "forged"}); err == nil {
		t.Fatal("foreign tenant write succeeded")
	}
}

func TestTodo_CHAT_017_Race(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := s.sendPostRaw(context.Background(), SendRequest{TenantID: "tenant-a", ConversationID: "c-1", AuthorID: "u-1", ClientKey: "k-" + string(rune('a'+i)), Body: "same"})
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	listed, err := NewAdapter(s).ListPosts(context.Background(), chat.Principal{TenantID: "tenant-a", SubjectID: "u-1"}, "tenant-a", "c-1", 0, chat.Page{PageSize: 200}, chat.PostWindow{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Posts) != n {
		t.Fatalf("posts=%d want %d", len(listed.Posts), n)
	}
}

func TestTodo_CHAT_017_IdempotencyConflict(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	r := SendRequest{TenantID: "tenant-a", ConversationID: "c-1", AuthorID: "u-1", ClientKey: "same", Body: "one"}
	if _, err := s.sendPostRaw(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	r.Body = "two"
	if _, err := s.sendPostRaw(context.Background(), r); err != ErrIdempotencyConflict {
		t.Fatalf("err=%v", err)
	}
}
