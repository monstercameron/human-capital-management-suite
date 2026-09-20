package application

// Unit coverage for the served work-queue write adapters
// (workqueue_writes.go): input validation fails before any database touch,
// the session port enforces a present reference, and unknown items surface
// the store's not-found refusal through the transaction path.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
)

func TestServedWorkQueueWritesValidation(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	w := newServedWorkQueueWrites(nil, nil)

	if _, err := w.Claim(ctx, "harborcare-demo", "not-a-uuid", "principal:x", 1, now.Add(time.Hour), now, workitem.TransitionMeta{}); err == nil {
		t.Fatal("Claim with a malformed work item id succeeded; want the UUID refusal before any database touch")
	}
	if _, err := w.Release(ctx, "harborcare-demo", "not-a-uuid", "principal:x", 1, now, workitem.TransitionMeta{}); err == nil {
		t.Fatal("Release with a malformed work item id succeeded; want the UUID refusal before any database touch")
	}
	if _, err := w.Complete(ctx, "harborcare-demo", "not-a-uuid", "session:x", "principal:x", 1, "sha256:aa", now, workitem.TransitionMeta{}); err == nil {
		t.Fatal("Complete with a malformed work item id succeeded; want the UUID refusal before any database touch")
	}
	if _, err := w.Decide(ctx, "harborcare-demo", "not-a-uuid", "session:x", "principal:x", 1, "rev", workitem.ApprovalDecisionApprove, "reason", now, workitem.TransitionMeta{}); err == nil {
		t.Fatal("Decide with a malformed work item id succeeded; want the UUID refusal before any database touch")
	}

	// A well-formed id with no database composed refuses rather than
	// panicking on the nil pool.
	if _, err := w.Claim(ctx, "harborcare-demo", uuid.NewString(), "principal:x", 1, now.Add(time.Hour), now, workitem.TransitionMeta{}); err == nil {
		t.Fatal("Claim with no database composed succeeded; want the composition refusal")
	}

	var session workQueueSession
	if err := session.CheckRevocation(ctx, "", now); err == nil {
		t.Fatal("an empty session reference passed; want the required-reference refusal")
	}
	if err := session.CheckRevocation(ctx, "session:finance-partner", now); err != nil {
		t.Fatalf("the verified caller's own session reference was refused: %v", err)
	}
}

func TestServedWorkQueueWritesUnknownItem(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	w := newServedWorkQueueWrites(pool, nil)

	unknown := uuid.NewString()
	if _, err := w.Claim(ctx, "harborcare-demo", unknown, "principal:x", 1, now.Add(time.Hour), now, workitem.TransitionMeta{}); err == nil {
		t.Fatal("Claim on an unknown work item succeeded; want the store's not-found refusal")
	}
	if _, err := w.Release(ctx, "harborcare-demo", unknown, "principal:x", 1, now, workitem.TransitionMeta{}); err == nil {
		t.Fatal("Release on an unknown work item succeeded; want the store's not-found refusal")
	}
	if _, err := w.Complete(ctx, "harborcare-demo", unknown, "session:x", "principal:x", 1, "sha256:aa", now, workitem.TransitionMeta{}); err == nil {
		t.Fatal("Complete on an unknown work item succeeded; want the store's not-found refusal")
	}
	if _, err := w.Decide(ctx, "harborcare-demo", unknown, "session:x", "principal:x", 1, "rev", workitem.ApprovalDecisionApprove, "reason", now, workitem.TransitionMeta{}); err == nil {
		t.Fatal("Decide on an unknown work item succeeded; want the store's not-found refusal")
	}
}
