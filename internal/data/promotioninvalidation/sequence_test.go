package promotioninvalidation_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioninvalidation"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func seedTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, key, "promotioninvalidation test tenant "+key)
	return tenantID
}

func beginScoped(t *testing.T, db *pgtest.DB, tenantID uuid.UUID) dbport.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	return tx
}

// TestTodo_PROMOUX_011_Integration proves NextSequence is durable and
// strictly increasing across sequential calls against real PostgreSQL, and
// that two independent (tenant, projection) counters never interfere with
// each other -- a transition committed against "promotion_detail" does not
// consume a position from "promotion_journeys".
func TestTodo_PROMOUX_011_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := seedTenant(t, db, "promoux011-integration")

	tx := beginScoped(t, db, tenantID)
	defer func() { _ = tx.Rollback(ctx) }()

	for want := uint64(1); want <= 5; want++ {
		got, err := promotioninvalidation.NextSequence(ctx, tx, tenantID, "promotion_detail")
		if err != nil {
			t.Fatalf("NextSequence(%d): %v", want, err)
		}
		if got != want {
			t.Fatalf("NextSequence returned %d, want %d (sequential allocation must not skip or repeat)", got, want)
		}
	}

	// A second projection for the same tenant starts its own counter at 1
	// rather than continuing "promotion_detail"'s: the counters are keyed
	// per (tenant, projection), never shared.
	other, err := promotioninvalidation.NextSequence(ctx, tx, tenantID, "promotion_journeys")
	if err != nil {
		t.Fatalf("NextSequence(other projection): %v", err)
	}
	if other != 1 {
		t.Fatalf("independent projection counter = %d, want 1", other)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	readTx := beginScoped(t, db, tenantID)
	defer func() { _ = readTx.Rollback(ctx) }()
	var stored int64
	if err := readTx.QueryRow(ctx,
		`SELECT next_sequence FROM promotion_invalidation_sequence WHERE tenant_id=$1 AND projection=$2`,
		tenantID, "promotion_detail",
	).Scan(&stored); err != nil {
		t.Fatalf("read durable counter: %v", err)
	}
	if stored != 5 {
		t.Fatalf("durable next_sequence = %d, want 5 (the allocation must have committed, not just returned in-process)", stored)
	}

	if _, err := promotioninvalidation.NextSequence(ctx, tx, uuid.Nil, "promotion_detail"); !errors.Is(err, promotioninvalidation.ErrInvalid) {
		t.Fatalf("nil tenant error = %v, want ErrInvalid", err)
	}
	if _, err := promotioninvalidation.NextSequence(ctx, tx, tenantID, ""); !errors.Is(err, promotioninvalidation.ErrInvalid) {
		t.Fatalf("empty projection error = %v, want ErrInvalid", err)
	}
	if _, err := promotioninvalidation.NextSequence(ctx, nil, tenantID, "promotion_detail"); !errors.Is(err, promotioninvalidation.ErrInvalid) {
		t.Fatalf("nil executor error = %v, want ErrInvalid", err)
	}
}

// TestTodo_PROMOUX_011_Race runs genuinely concurrent transition commits for
// the same tenant and projection, each in its own goroutine, its own real
// PostgreSQL connection and its own transaction, released together from one
// barrier -- the same shape TestTodo_PROMOUX_002_Race and
// TestTodo_PROMOUX_004_Race already established for this schema.
//
// If NextSequence were "SELECT current value, then write back one more" this
// test would eventually show two goroutines reading the same value and
// handing out the same position to two different transitions -- a lost or
// duplicated invalidation exactly like the ones this todo's Race clause
// rules out. What actually decides the outcome is the row lock PostgreSQL
// takes as part of committing each INSERT .. ON CONFLICT .. DO UPDATE, so
// this test asserts the durable, complete result -- every position from 1 to
// N assigned exactly once -- rather than merely "no error".
func TestTodo_PROMOUX_011_Race(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := seedTenant(t, db, "promoux011-race")
	const projection = "promotion_detail"
	const concurrency = 12

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		assigned  = make([]uint64, 0, concurrency)
		otherErrs []error
	)
	start := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			// internal/data/pgtest.DB.Conn is documented as not safe for
			// concurrent use; NewConn is this package's own escape hatch for
			// concurrency tests, so the exclusion this test proves comes from
			// PostgreSQL serializing independent sessions' commits, not from
			// goroutines taking turns on one shared connection.
			conn := db.NewConn(t)
			tx, err := conn.Begin(ctx)
			if err != nil {
				mu.Lock()
				otherErrs = append(otherErrs, err)
				mu.Unlock()
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
				mu.Lock()
				otherErrs = append(otherErrs, err)
				mu.Unlock()
				return
			}
			next, err := promotioninvalidation.NextSequence(ctx, tx, tenantID, projection)
			if err != nil {
				mu.Lock()
				otherErrs = append(otherErrs, err)
				mu.Unlock()
				return
			}
			if commitErr := tx.Commit(ctx); commitErr != nil {
				mu.Lock()
				otherErrs = append(otherErrs, commitErr)
				mu.Unlock()
				return
			}
			mu.Lock()
			assigned = append(assigned, next)
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	for _, err := range otherErrs {
		t.Errorf("unexpected error from a concurrent NextSequence: %v", err)
	}
	if len(assigned) != concurrency {
		t.Fatalf("committed allocations = %d, want %d (no lost invalidation)", len(assigned), concurrency)
	}
	seen := make(map[uint64]int, concurrency)
	for _, v := range assigned {
		seen[v]++
	}
	for want := uint64(1); want <= concurrency; want++ {
		if count := seen[want]; count != 1 {
			t.Fatalf("position %d was assigned %d times, want exactly 1 (no lost or reordered invalidation)", want, count)
		}
	}

	readTx := beginScoped(t, db, tenantID)
	defer func() { _ = readTx.Rollback(ctx) }()
	var stored int64
	if err := readTx.QueryRow(ctx,
		`SELECT next_sequence FROM promotion_invalidation_sequence WHERE tenant_id=$1 AND projection=$2`,
		tenantID, projection,
	).Scan(&stored); err != nil {
		t.Fatalf("read durable counter: %v", err)
	}
	if stored != int64(concurrency) {
		t.Fatalf("durable next_sequence = %d, want %d", stored, concurrency)
	}
}
