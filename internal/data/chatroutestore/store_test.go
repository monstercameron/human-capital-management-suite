package chatroutestore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_CHAT_002_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	conn := db.NewConn(t)
	store, err := New(conn)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	q := chatrouting.ReserveRequest{ConversationID: "c1", HostTenantID: "tenant-a", ShardID: "shard-a", IdempotencyKey: "create-1", PlacementPolicyVersion: 1}
	r, err := store.Reserve(context.Background(), q)
	if err != nil || r.State != chatrouting.StatePending {
		t.Fatalf("reserve = %+v, %v", r, err)
	}
	if _, err = store.Reserve(context.Background(), q); err != nil {
		t.Fatalf("idempotent reserve = %v", err)
	}
	if _, err = store.Lookup(context.Background(), "c1", "tenant-b"); !errors.Is(err, chatrouting.ErrNotFound) {
		t.Fatalf("foreign route leaked as %v", err)
	}
	if _, err = store.Activate(context.Background(), "c1", "tenant-a", 99); !errors.Is(err, chatrouting.ErrStaleEpoch) {
		t.Fatalf("stale activate = %v", err)
	}
	active, err := store.Activate(context.Background(), "c1", "tenant-a", 1)
	if err != nil || active.State != chatrouting.StateActive {
		t.Fatalf("activate = %+v, %v", active, err)
	}
	plan, err := store.BeginMove(context.Background(), "c1", "tenant-a", 1, "shard-b")
	if err != nil || plan.MoveEpoch != 2 {
		t.Fatalf("begin move = %+v, %v", plan, err)
	}
	if _, err = store.BeginMove(context.Background(), "c1", "tenant-a", 1, "shard-c"); !errors.Is(err, chatrouting.ErrMoveConflict) {
		t.Fatalf("second move = %v", err)
	}
	moved, err := store.Cutover(context.Background(), "c1", "tenant-a", 2)
	if err != nil || moved.ShardID != "shard-b" || moved.Epoch != 3 {
		t.Fatalf("cutover = %+v, %v", moved, err)
	}
}

// TestTodo_CHAT_009_IdempotencyKeyIsPerTenant reproduces the CHAT-009 review
// finding: create_idempotency_key used to be a bare global UNIQUE column, so
// a second tenant reusing a key another tenant had already used hit a raw
// 23505 (mapped only through happenstance, or not at all) on an insert for a
// completely different conversation. It must be scoped per host tenant and
// mapped to the package's domain error either way.
func TestTodo_CHAT_009_IdempotencyKeyIsPerTenant(t *testing.T) {
	db := pgtest.NewEmpty(t)
	conn := db.NewConn(t)
	store, err := New(conn)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := chatrouting.ReserveRequest{ConversationID: "conv-a", HostTenantID: "tenant-a", ShardID: "shard-a", IdempotencyKey: "shared-key", PlacementPolicyVersion: 1}
	if _, err = store.Reserve(context.Background(), first); err != nil {
		t.Fatalf("first tenant reserve = %v", err)
	}
	// A different tenant reusing the same idempotency key for a different
	// conversation must succeed: the constraint is per tenant now.
	other := chatrouting.ReserveRequest{ConversationID: "conv-b", HostTenantID: "tenant-b", ShardID: "shard-a", IdempotencyKey: "shared-key", PlacementPolicyVersion: 1}
	if _, err = store.Reserve(context.Background(), other); err != nil {
		t.Fatalf("second tenant reusing the same key should not collide: %v", err)
	}
	// The SAME tenant reusing that key for a different conversation must be
	// rejected as a domain error, not a raw driver error.
	reuse := chatrouting.ReserveRequest{ConversationID: "conv-c", HostTenantID: "tenant-a", ShardID: "shard-a", IdempotencyKey: "shared-key", PlacementPolicyVersion: 1}
	if _, err = store.Reserve(context.Background(), reuse); !errors.Is(err, chatrouting.ErrAlreadyExists) {
		t.Fatalf("same-tenant key reuse = %v, want %v", err, chatrouting.ErrAlreadyExists)
	}
}

// TestTodo_CHAT_009_MigrateIsTransactional confirms Migrate can be run
// repeatedly (as it would be re-run after a crash mid-migration) without
// leaving the table FORCE-RLS with no policy: previously five independent
// Execs meant a crash between DROP POLICY and CREATE POLICY was possible.
func TestTodo_CHAT_009_MigrateIsTransactional(t *testing.T) {
	db := pgtest.NewEmpty(t)
	conn := db.NewConn(t)
	store, err := New(conn)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatalf("re-running migrate = %v", err)
	}
	var policies int
	if err := db.SQL.QueryRowContext(context.Background(), `SELECT count(*) FROM pg_policies WHERE tablename='chat_conversation_route' AND schemaname=$1`, db.Schema).Scan(&policies); err != nil {
		t.Fatal(err)
	}
	if policies != 1 {
		t.Fatalf("chat_conversation_route policies = %d, want 1", policies)
	}
}
