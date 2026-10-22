package agentinvocationstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestTodo_AGENTP_015_MentionLimitOutcomeStoresRetryTime: a mention refused by a
// mention ceiling is stored with the time the person can ask again and the
// number of times they asked; the runtime role can write and read both, the
// table refuses the outcome without a time and a time on any other outcome, and
// the outcome cannot be marked retryable.
func TestTodo_AGENTP_015_MentionLimitOutcomeStoresRetryTime(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1)`, tenant); err != nil {
		t.Fatal(err)
	}
	login, password := roleName("mentionlimit"), uuid.NewString()
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
	retryAt := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	limit := PostFailure{TenantID: tenant.String(), InvokerID: "alice", PostID: "post-31", ConversationID: "room", ThreadID: "post-31", Code: MentionLimitCode, RetryAt: retryAt, MentionsAsked: 30}
	if err := store.RecordPostFailure(ctx, limit); err != nil {
		t.Fatal(err)
	}
	// A replay at another time is the same outcome, not a conflict.
	replay := limit
	replay.RetryAt = retryAt.Add(time.Minute)
	if err := store.RecordPostFailure(ctx, replay); err != nil {
		t.Fatalf("replay = %v", err)
	}
	got, err := store.LookupPostFailure(ctx, tenant.String(), "alice", "post-31")
	if err != nil || got.Code != MentionLimitCode || got.Retryable || !got.RetryAt.Equal(retryAt) || got.MentionsAsked != 30 {
		t.Fatalf("stored = %+v, %v", got, err)
	}
	rows, err := store.ListPostFailures(ctx, tenant.String(), "alice", "room")
	if err != nil || len(rows) != 1 || !rows[0].RetryAt.Equal(retryAt) || rows[0].MentionsAsked != 30 {
		t.Fatalf("listed = %+v, %v", rows, err)
	}
	other := PostFailure{TenantID: tenant.String(), InvokerID: "alice", PostID: "post-1", ConversationID: "room", ThreadID: "post-1", Code: "MODEL_UNAVAILABLE", Retryable: true}
	if err := store.RecordPostFailure(ctx, other); err != nil {
		t.Fatal(err)
	}
	if got, err := store.LookupPostFailure(ctx, tenant.String(), "alice", "post-1"); err != nil || !got.RetryAt.IsZero() || got.MentionsAsked != 0 {
		t.Fatalf("an unrelated outcome carries a retry time: %+v, %v", got, err)
	}
	for name, bad := range map[string]PostFailure{
		"no retry time":       {TenantID: tenant.String(), InvokerID: "alice", PostID: "p2", ConversationID: "room", ThreadID: "p2", Code: MentionLimitCode},
		"retryable":           {TenantID: tenant.String(), InvokerID: "alice", PostID: "p3", ConversationID: "room", ThreadID: "p3", Code: MentionLimitCode, Retryable: true, RetryAt: retryAt},
		"time on other code":  {TenantID: tenant.String(), InvokerID: "alice", PostID: "p4", ConversationID: "room", ThreadID: "p4", Code: "ADMISSION_REFUSED", RetryAt: retryAt},
		"count on other code": {TenantID: tenant.String(), InvokerID: "alice", PostID: "p5", ConversationID: "room", ThreadID: "p5", Code: "ADMISSION_REFUSED", MentionsAsked: 3},
		"negative count":      {TenantID: tenant.String(), InvokerID: "alice", PostID: "p6", ConversationID: "room", ThreadID: "p6", Code: MentionLimitCode, RetryAt: retryAt, MentionsAsked: -1},
	} {
		if err := store.RecordPostFailure(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s was stored: %v", name, err)
		}
	}
	// The table holds the same rule beneath the store.
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO persona_invocation_failure (tenant_id,invoker_id,post_id,conversation_id,thread_id,failure_code,retryable) VALUES ($1,'alice','raw','room','raw','MENTION_LIMIT_REACHED',false)`, tenant); err == nil {
		t.Fatal("the table stored a mention limit with no retry time")
	}
}
