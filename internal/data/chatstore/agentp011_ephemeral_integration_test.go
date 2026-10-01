package chatstore

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"testing"
	"time"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

func TestTodo_AGENTP_011_ChatstoreRestartIntegration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(context.Background()); err != nil {
		t.Fatalf("chat migrations: %v", err)
	}
	u, err := url.Parse(db.URL)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", db.Schema)
	u.RawQuery = query.Encode()
	dsn := u.String()
	open := func() *Store {
		t.Helper()
		store, openErr := New(context.Background(), Config{DSN: dsn})
		if openErr != nil {
			t.Fatal(openErr)
		}
		return store
	}

	first := open()
	seedConversationRow(t, first, Conversation{ID: "room", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{
		{MemberID: "alice", HomeTenantID: "tenant-a", Role: "manager", State: "active"},
		{MemberID: "bob", HomeTenantID: "tenant-a", Role: "member", State: "active"},
	})
	now := time.Now().UTC().Truncate(time.Microsecond)
	input := chat.EphemeralPost{
		ID: "ephemeral-one", TenantID: "tenant-a", ConversationID: "room", ThreadID: "root-post",
		RecipientHomeTenantID: "tenant-a", RecipientSubjectID: "alice", Body: "salary detail",
		OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
		DurableCopyConversationID: "alice-persona-dm", DurableCopyPostID: "durable-copy-1",
		ThreadLink: "/chat/share/signed-locator",
	}
	firstAdapter := NewAdapter(first)
	stored, err := firstAdapter.PutEphemeral(context.Background(), input)
	if err != nil {
		first.Close()
		t.Fatal(err)
	}
	if stored.Sequence == 0 {
		first.Close()
		t.Fatal("ephemeral write has no shared stream offset")
	}
	first.Close()

	// Reopen the independent chat database as a new process would. The pending
	// body and its stream position must survive adapter/store reconstruction.
	second := open()
	t.Cleanup(second.Close)
	adapter := NewAdapter(second)
	alice := chat.Principal{TenantID: "tenant-a", SubjectID: "alice"}
	bob := chat.Principal{TenantID: "tenant-a", SubjectID: "bob"}
	got, next, err := adapter.ListEphemeral(context.Background(), alice, "tenant-a", "room", 0, 10)
	if err != nil || len(got) != 1 || got[0].ID != stored.ID || got[0].Sequence != stored.Sequence || got[0].Body != input.Body || next != stored.Sequence {
		t.Fatalf("restart replay=%+v next=%d err=%v", got, next, err)
	}
	posts, watermark, listErr := adapter.ListEphemeral(context.Background(), bob, "tenant-a", "room", 0, 10)
	if listErr != nil {
		t.Fatalf("active nonrecipient list: %v", listErr)
	}
	if len(posts) != 0 || watermark != stored.Sequence {
		t.Fatalf("active nonrecipient saw posts=%+v watermark=%d", posts, watermark)
	}
	unknown := chat.Principal{TenantID: "tenant-a", SubjectID: "unknown"}
	if _, _, listErr = adapter.ListEphemeral(context.Background(), unknown, "tenant-a", "room", 0, 10); !errors.Is(listErr, chat.ErrPermissionDenied) {
		t.Fatalf("unknown nonmember list error=%v; want permission denied", listErr)
	}

	for _, test := range []struct {
		principal chat.Principal
		wantBody  bool
	}{{alice, true}, {bob, false}} {
		page, readErr := adapter.ReadConversationEvents(context.Background(), chat.WatchConversationRequest{
			Principal: test.principal, TenantID: "tenant-a", ConversationID: "room",
		}, 0, 10)
		if readErr != nil {
			t.Fatalf("merged stream for %s: %v", test.principal.SubjectID, readErr)
		}
		if page.NextOffset < stored.Sequence || len(page.EphemeralPosts) != boolCount(test.wantBody) {
			t.Fatalf("merged stream for %s: next=%d ephemeral=%+v", test.principal.SubjectID, page.NextOffset, page.EphemeralPosts)
		}
		if test.wantBody && (page.EphemeralPosts[0].Body != input.Body || page.EphemeralPosts[0].Sequence != stored.Sequence) {
			t.Fatalf("recipient stream payload=%+v", page.EphemeralPosts[0])
		}
	}

	var envelopes, outbox int64
	if err = second.RunTenantTx(context.Background(), "tenant-a", func(tx dbport.Tx) error {
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM chat_ephemeral_post WHERE tenant_id=$1 AND conversation_id=$2`, "tenant-a", "room").Scan(&envelopes); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM chat_outbox WHERE tenant_id=$1`, "tenant-a").Scan(&outbox)
	}); err != nil {
		t.Fatal(err)
	}
	if envelopes != 1 || outbox != 0 {
		t.Fatalf("ephemeral envelope/outbox rows=%d/%d; want 1/0", envelopes, outbox)
	}

	retried, err := adapter.PutEphemeral(context.Background(), input)
	if err != nil || retried.ID != stored.ID || retried.Sequence != stored.Sequence {
		t.Fatalf("idempotent retry=%+v err=%v; original=%+v", retried, err, stored)
	}
	var afterRetry int64
	if err = second.RunTenantTx(context.Background(), "tenant-a", func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM chat_ephemeral_post WHERE tenant_id=$1 AND conversation_id=$2`, "tenant-a", "room").Scan(&afterRetry)
	}); err != nil {
		t.Fatal(err)
	}
	if afterRetry != 1 {
		t.Fatalf("idempotent retry created %d envelope rows", afterRetry)
	}

	expiredInput := input
	expiredInput.ID = "ephemeral-expired"
	expiredInput.CreatedAt = now.Add(-48 * time.Hour)
	expiredInput.ExpiresAt = now.Add(-24 * time.Hour)
	expired, err := adapter.PutEphemeral(context.Background(), expiredInput)
	if err != nil {
		t.Fatal(err)
	}
	posts, watermark, err = adapter.ListEphemeral(context.Background(), alice, "tenant-a", "room", stored.Sequence, 10)
	if err != nil || len(posts) != 0 || watermark != expired.Sequence {
		t.Fatalf("expired envelope replay=%+v watermark=%d err=%v", posts, watermark, err)
	}
	page, err := adapter.ReadConversationEvents(context.Background(), chat.WatchConversationRequest{Principal: alice, TenantID: "tenant-a", ConversationID: "room"}, stored.Sequence, 10)
	if err != nil || len(page.EphemeralPosts) != 0 || page.NextOffset < expired.Sequence {
		t.Fatalf("expired merged replay=%+v err=%v", page, err)
	}
	removed, err := NewDurableEphemeralStore(second).PruneExpiredEphemeral(context.Background(), "tenant-a", now, 1)
	if err != nil || removed != 1 {
		t.Fatalf("expired envelope prune removed=%d err=%v", removed, err)
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
