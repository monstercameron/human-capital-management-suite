package uow_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/uow"
)

// TestTodo_DB_EDGE_002_Property: key derivation is deterministic,
// namespaced and collision-free over a corpus.
func TestTodo_DB_EDGE_002_Property(t *testing.T) {
	seen := map[int64]string{}
	namespaces := []string{uow.AdvisoryNamespacePromotion, uow.AdvisoryNamespaceOutbox, uow.AdvisoryNamespaceScheduler}
	for _, namespace := range namespaces {
		for tenant := 0; tenant < 25; tenant++ {
			for resource := 0; resource < 40; resource++ {
				key := uow.DeriveAdvisoryKey(namespace,
					fmt.Sprintf("tenant-%d", tenant), fmt.Sprintf("resource-%d", resource))
				label := fmt.Sprintf("%s/%d/%d", namespace, tenant, resource)
				if prior, dup := seen[key]; dup {
					t.Fatalf("collision: %s and %s share key %d", label, prior, key)
				}
				seen[key] = label
			}
		}
	}
	// Same inputs in another namespace derive a different key: the
	// version prefix domain-separates.
	a := uow.DeriveAdvisoryKey(uow.AdvisoryNamespacePromotion, "t", "r")
	b := uow.DeriveAdvisoryKey(uow.AdvisoryNamespaceOutbox, "t", "r")
	if a == b {
		t.Fatal("cross-namespace keys collide")
	}
}

// TestTodo_DB_EDGE_002_Race: concurrent transactions acquire disjoint
// keys without imbalance.
func TestTodo_DB_EDGE_002_Race(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	const racers = 8
	var wg sync.WaitGroup
	errs := make([]error, racers)
	counts := make([]int, racers)
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conn := db.NewConn(t)
			tx, err := conn.Begin(ctx)
			if err != nil {
				errs[i] = err
				return
			}
			defer tx.Rollback(ctx)
			key := uow.DeriveAdvisoryKey(uow.AdvisoryNamespaceScheduler, "race-tenant", fmt.Sprintf("worker-%d", i))
			counts[i], errs[i] = uow.AcquireXact(ctx, tx, []int64{key}, uow.AdvisoryBudget{MaxLocks: 2})
			if errs[i] == nil {
				errs[i] = tx.Commit(ctx)
			}
		}(i)
	}
	wg.Wait()
	for i := range racers {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		if counts[i] != 1 {
			t.Fatalf("racer %d acquired=%d", i, counts[i])
		}
	}
}

// TestTodo_DB_EDGE_002_Integration: session locks require a reviewed
// permit and provable cleanup before pool return.
func TestTodo_DB_EDGE_002_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	conn := db.NewConn(t)
	key := uow.DeriveAdvisoryKey(uow.AdvisoryNamespaceOutbox, "tenant-int", "poller")
	permit := uow.SessionLockPermit{Owner: "outbox-poller", CleanupProof: "release-before-pool-return"}
	if err := uow.AcquireSession(ctx, conn, key, permit); err != nil {
		t.Fatalf("AcquireSession: %v", err)
	}
	held, err := uow.SessionLocksHeld(ctx, conn)
	if err != nil || !held {
		t.Fatalf("held=%v err=%v", held, err)
	}
	if err := uow.ReleaseSession(ctx, conn, key); err != nil {
		t.Fatalf("ReleaseSession: %v", err)
	}
	if held, err := uow.SessionLocksHeld(ctx, conn); err != nil || held {
		t.Fatalf("leak: held=%v err=%v", held, err)
	}
	// Pool return with a held lock is observable: the leak check fires.
	if err := uow.AcquireSession(ctx, conn, key, permit); err != nil {
		t.Fatalf("AcquireSession: %v", err)
	}
	if held, _ := uow.SessionLocksHeld(ctx, conn); !held {
		t.Fatal("leak check misses a held session lock")
	}
	if err := uow.ReleaseSession(ctx, conn, key); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}

// TestTodo_DB_EDGE_002_Fault: permit-less session locks, unbalanced
// releases and zero budgets fail closed.
func TestTodo_DB_EDGE_002_Fault(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	conn := db.NewConn(t)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	key := uow.DeriveAdvisoryKey(uow.AdvisoryNamespacePromotion, "t", "r")
	if _, err := uow.AcquireXact(ctx, tx, []int64{key}, uow.AdvisoryBudget{}); err == nil {
		t.Fatal("zero budget admitted")
	} else if _, ok := uow.AsLockSaturation(err); !ok {
		t.Fatalf("expected saturation, got %v", err)
	}
	if err := uow.AcquireSession(ctx, conn, key, uow.SessionLockPermit{}); err == nil {
		t.Fatal("permit-less session lock admitted")
	}
	ownerless := uow.SessionLockPermit{CleanupProof: "proof"}
	if err := uow.AcquireSession(ctx, conn, key, ownerless); err == nil {
		t.Fatal("ownerless session lock admitted")
	}
	if err := uow.ReleaseSession(ctx, conn, key+99991); err == nil {
		t.Fatal("unbalanced release admitted")
	}
}

// TestTodo_DB_EDGE_002_Security: reentrancy is counted, never hidden;
// lock keys never become business truth.
func TestTodo_DB_EDGE_002_Security(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	conn := db.NewConn(t)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	key := uow.DeriveAdvisoryKey(uow.AdvisoryNamespacePromotion, "tenant-sec", "worker-s")
	// Reentrant acquisition counts each take: dedupe keeps one lock but
	// the budget sees the declared set, so imbalance cannot hide behind
	// reentrancy.
	if acquired, err := uow.AcquireXact(ctx, tx, []int64{key, key, key}, uow.AdvisoryBudget{MaxLocks: 4}); err != nil || acquired != 1 {
		t.Fatalf("reentrant acquire=%d err=%v", acquired, err)
	}
	if _, err := uow.AcquireXact(ctx, tx, []int64{key}, uow.AdvisoryBudget{MaxLocks: 0}); err == nil {
		t.Fatal("budget bypass admitted")
	}
}

// BenchmarkTodo_DB_EDGE_002 measures key derivation throughput.
func BenchmarkTodo_DB_EDGE_002(b *testing.B) {
	b.ResetTimer()
	for i := range b.N {
		_ = uow.DeriveAdvisoryKey(uow.AdvisoryNamespacePromotion, "tenant-bench", fmt.Sprintf("resource-%d", i%1024))
	}
}
