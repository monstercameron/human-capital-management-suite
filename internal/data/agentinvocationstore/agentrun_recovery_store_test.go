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

// A claim with nothing after it is listed as unfinished until its one final
// outcome is recorded, which any process may do and which is recorded once.
func TestTodo_AGENTRUN_004_ListUnfinished(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	a, b := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1),($2)`, a, b); err != nil {
		t.Fatal(err)
	}
	login, password := roleName("unfinished"), uuid.NewString()
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

	if _, created, err := store.Claim(ctx, invocation(a.String(), "", "persona", "post-1", "user")); err != nil || !created {
		t.Fatalf("claim: created=%t err=%v", created, err)
	}
	hour := time.Now().Add(-time.Hour)
	items, err := store.ListUnfinished(ctx, a.String(), hour, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("unfinished=%+v err=%v", items, err)
	}
	item := items[0]
	if item.InvocationID != "inv-post-1" || item.PostID != "post-1" || item.InvokerID != "user" || item.ConversationID != "conversation" || item.CreatedAt.IsZero() ||
		item.RequestID != "" || item.Decision != "" || !item.Deadline.IsZero() || item.RunState != "" || item.RunRevision != 0 || !item.LeaseUntil.IsZero() || item.ModelCallStarted {
		t.Fatalf("a claim that never reached admission is described as %+v", item)
	}
	if later, err := store.ListUnfinished(ctx, a.String(), time.Now().Add(time.Hour), 10); err != nil || len(later) != 0 {
		t.Fatalf("an invocation older than the window was listed: %+v %v", later, err)
	}
	if other, err := store.ListUnfinished(ctx, b.String(), hour, 10); err != nil || len(other) != 0 {
		t.Fatalf("another tenant's claim was listed: %+v %v", other, err)
	}

	if err := store.FinishUnfinished(ctx, a.String(), item, "ANSWER_INTERRUPTED", true); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if err := store.FinishUnfinished(ctx, a.String(), item, "ANSWER_INTERRUPTED", true); err != nil {
		t.Fatalf("a second process finishing the same invocation: %v", err)
	}
	if err := store.FinishUnfinished(ctx, a.String(), item, "ADMISSION_REFUSED", false); !errors.Is(err, ErrConflict) {
		t.Fatalf("a different outcome replaced the first: %v", err)
	}
	if left, err := store.ListUnfinished(ctx, a.String(), hour, 10); err != nil || len(left) != 0 {
		t.Fatalf("a finished invocation is still unfinished: %+v %v", left, err)
	}
	failures, err := store.ListPostFailures(ctx, a.String(), "user", "conversation")
	if err != nil || len(failures) != 1 || failures[0].Code != "ANSWER_INTERRUPTED" || !failures[0].Retryable || failures[0].ThreadID != "thread" {
		t.Fatalf("failures=%+v err=%v", failures, err)
	}

	if err := store.FinishUnfinished(ctx, a.String(), UnfinishedInvocation{}, "ANSWER_INTERRUPTED", true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an invocation with no identity was finished: %v", err)
	}
	for _, call := range []func() error{
		func() error { _, err := store.ListUnfinished(ctx, a.String(), time.Time{}, 10); return err },
		func() error { _, err := store.ListUnfinished(ctx, a.String(), hour, 0); return err },
		func() error { _, err := store.ListUnfinished(ctx, a.String(), hour, 1001); return err },
		func() error { _, err := store.ListUnfinished(ctx, " ", hour, 10); return err },
		func() error { _, err := (*Store)(nil).ListUnfinished(ctx, a.String(), hour, 10); return err },
	} {
		if err := call(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid arguments accepted: %v", err)
		}
	}
	if !validFailureCode("ANSWER_INTERRUPTED", true) || !validFailureCode("ANSWER_INTERRUPTED", false) {
		t.Fatal("an interrupted answer is not a recordable outcome")
	}
}
