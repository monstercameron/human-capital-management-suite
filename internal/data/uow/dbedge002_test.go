package uow_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/uow"
)

// TestPostgresAdvisoryLocksAreTransactionScopedNamespacedAndBudgeted is
// the PRIMARY test. Approved use defaults to transaction-scoped locks
// with versioned collision-tested key derivation and an explicit
// acquisition budget; rollback releases everything, cross-tenant keys
// never collide, and over-budget acquisition fails with a typed
// saturation result instead of exhausting shared memory.
func TestPostgresAdvisoryLocksAreTransactionScopedNamespacedAndBudgeted(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	conn := db.NewConn(t)
	tenantA := insertTenant(t, db, "lock-tenant-a")
	tenantB := insertTenant(t, db, "lock-tenant-b")

	keyA := uow.DeriveAdvisoryKey(uow.AdvisoryNamespacePromotion, tenantA.String(), "worker:w-1")
	keyB := uow.DeriveAdvisoryKey(uow.AdvisoryNamespacePromotion, tenantB.String(), "worker:w-1")
	if keyA == keyB {
		t.Fatal("cross-tenant keys collide")
	}
	if again := uow.DeriveAdvisoryKey(uow.AdvisoryNamespacePromotion, tenantA.String(), "worker:w-1"); again != keyA {
		t.Fatal("key derivation is not deterministic")
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	acquired, err := uow.AcquireXact(ctx, tx, []int64{keyA, keyB}, uow.AdvisoryBudget{MaxLocks: 4})
	if err != nil {
		t.Fatalf("AcquireXact: %v", err)
	}
	if acquired != 2 {
		t.Fatalf("acquired=%d", acquired)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// Rollback releases transaction-scoped locks: re-acquisition in a
	// fresh transaction succeeds immediately.
	tx2, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback(ctx)
	if _, err := uow.AcquireXact(ctx, tx2, []int64{keyA}, uow.AdvisoryBudget{MaxLocks: 4}); err != nil {
		t.Fatalf("re-acquire after rollback: %v", err)
	}
	// Over-budget acquisition fails typed, before touching the database.
	if _, err := uow.AcquireXact(ctx, tx2, []int64{keyA, keyB, 1, 2, 3}, uow.AdvisoryBudget{MaxLocks: 4}); err == nil {
		t.Fatal("over-budget acquisition admitted")
	} else if _, ok := uow.AsLockSaturation(err); !ok {
		t.Fatalf("expected saturation, got %v", err)
	}
}
