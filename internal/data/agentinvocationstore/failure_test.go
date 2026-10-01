package agentinvocationstore

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_AGENTP_020_DurablePostFailureIsolationAndImmutability(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	a, b := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1),($2)`, a, b); err != nil {
		t.Fatal(err)
	}
	login, password := roleName("postfailure"), uuid.NewString()
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
	failure := PostFailure{TenantID: a.String(), InvokerID: "invoker", PostID: "post", ConversationID: "room", ThreadID: "thread", Code: "MODEL_UNAVAILABLE", Retryable: true}
	if err := store.RecordPostFailure(ctx, failure); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPostFailure(ctx, failure); err != nil {
		t.Fatalf("idempotent replay=%v", err)
	}
	changed := failure
	changed.Code = "ADMISSION_UNAVAILABLE"
	if err := store.RecordPostFailure(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed immutable outcome=%v", err)
	}
	rows, err := store.ListPostFailures(ctx, a.String(), "invoker", "room")
	if err != nil || len(rows) != 1 || rows[0].Code != "MODEL_UNAVAILABLE" || !rows[0].Retryable || rows[0].OccurredAt.IsZero() {
		t.Fatalf("durable failures=%+v %v", rows, err)
	}
	loaded, err := store.LookupPostFailure(ctx, a.String(), "invoker", "post")
	if err != nil || loaded.ThreadID != "thread" {
		t.Fatalf("owner lookup=%+v %v", loaded, err)
	}
	for _, scope := range []struct{ tenant, owner, room string }{{b.String(), "invoker", "room"}, {a.String(), "other", "room"}, {a.String(), "invoker", "other-room"}} {
		rows, err := store.ListPostFailures(ctx, scope.tenant, scope.owner, scope.room)
		if err != nil || len(rows) != 0 {
			t.Fatalf("scope %+v leaked %+v %v", scope, rows, err)
		}
	}
	if _, err := store.LookupPostFailure(ctx, a.String(), "other", "post"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other owner lookup=%v", err)
	}
	if _, err := db.SQL.ExecContext(ctx, `UPDATE persona_invocation_failure SET failure_code='INVOCATION_FAILED' WHERE tenant_id=$1`, a); err == nil {
		t.Fatal("append-only failure was mutated")
	}
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM persona_invocation_failure WHERE tenant_id=$1`, a); err == nil {
		t.Fatal("append-only failure was deleted")
	}
	if err := pool.RunTenantTx(ctx, b, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO persona_invocation_failure (tenant_id,invoker_id,post_id,conversation_id,thread_id,failure_code,retryable) VALUES ($1,'invoker','forged','room','thread','MODEL_UNAVAILABLE',true)`, a)
		return err
	}); err == nil {
		t.Fatal("RLS accepted a cross-tenant failure")
	}
	mapped, err := NewWithTenantUUID(pool, func(value string) uuid.UUID {
		if value == "logical-tenant" {
			return a
		}
		return uuid.Nil
	})
	if err != nil {
		t.Fatal(err)
	}
	failure.TenantID, failure.PostID, failure.ConversationID = "logical-tenant", "mapped-post", "mapped-room"
	if err := mapped.RecordPostFailure(ctx, failure); err != nil {
		t.Fatalf("logical tenant write=%v", err)
	}
	rows, err = mapped.ListPostFailures(ctx, "logical-tenant", "invoker", "mapped-room")
	if err != nil || len(rows) != 1 || rows[0].TenantID != "logical-tenant" {
		t.Fatalf("logical tenant list=%+v %v", rows, err)
	}
	loaded, err = mapped.LookupPostFailure(ctx, "logical-tenant", "invoker", "mapped-post")
	if err != nil || loaded.TenantID != "logical-tenant" {
		t.Fatalf("logical tenant lookup=%+v %v", loaded, err)
	}
}

func TestTodo_AGENTP_020_FailureStoreRejectsRawErrorsAndUnsafeRetry(t *testing.T) {
	for _, test := range []struct {
		code  string
		retry bool
		valid bool
	}{{"MODEL_UNAVAILABLE", true, true}, {"OUTPUT_REJECTED", true, false}, {"ADMISSION_REFUSED", false, true}, {"database password raw error", false, false}} {
		if got := validFailureCode(test.code, test.retry); got != test.valid {
			t.Fatalf("code=%q retry=%v got=%v", test.code, test.retry, got)
		}
	}
	if err := (*Store)(nil).RecordPostFailure(context.Background(), PostFailure{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil store=%v", err)
	}
	if _, err := (*Store)(nil).ListPostFailures(context.Background(), "bad", "invoker", "room"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil list=%v", err)
	}
	if _, err := (*Store)(nil).LookupPostFailure(context.Background(), "bad", "invoker", "post"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil lookup=%v", err)
	}
}
