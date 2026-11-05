package agentinvocationstore

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestTodo_AGENTUX_078_Integration: the no-model outcome the run path writes
// for a question asked on a server with no model is storable through the store
// against a real database: it records once, reads back with its code and no
// retry, and is refused as retryable. (The constraint-text comparison lives in
// TestTodo_AGENTUX_078_FailureCodesMatchConstraint.)
func TestTodo_AGENTUX_078_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1)`, tenant); err != nil {
		t.Fatal(err)
	}
	login, password := roleName("agentux078"), uuid.NewString()
	if err := createLogin(ctx, db.SQL, login, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.SQL.ExecContext(context.Background(), "DROP ROLE "+login) })
	pool, err := agentstore.New(ctx, agentstore.Config{DSN: testDSN(t, db.URL, db.Schema, login, password, "postgres"), CoreDSN: "postgres://core:pw@127.0.0.1:5432/core", MaxConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, _ := New(pool)
	failure := PostFailure{TenantID: tenant.String(), InvokerID: "alice", PostID: "post-1", ConversationID: "room", ThreadID: "post-1", Code: NoModelCode}
	if err := store.RecordPostFailure(ctx, failure); err != nil {
		t.Fatalf("the table refuses the no-model outcome: %v", err)
	}
	if err := store.RecordPostFailure(ctx, failure); err != nil {
		t.Fatalf("replay = %v", err)
	}
	got, err := store.LookupPostFailure(ctx, tenant.String(), "alice", "post-1")
	if err != nil || got.Code != NoModelCode || got.Retryable {
		t.Fatalf("stored = %+v, %v", got, err)
	}
	rows, err := store.ListPostFailures(ctx, tenant.String(), "alice", "room")
	if err != nil || len(rows) != 1 || rows[0].Code != NoModelCode || rows[0].Retryable {
		t.Fatalf("listed = %+v, %v", rows, err)
	}
	retryable := failure
	retryable.PostID = "post-2"
	retryable.ThreadID = "post-2"
	retryable.Retryable = true
	if err := store.RecordPostFailure(ctx, retryable); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a retryable no-model outcome was stored: %v", err)
	}
}
