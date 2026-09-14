package promotionguard_test

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionguard"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// seedTenant inserts the one tenant row every guard test scopes its
// transactions to.
func seedTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, key, "promotionguard test tenant "+key)
	return tenantID
}

// beginScoped opens a tenant-scoped transaction, the shape every method in
// this package requires of its caller.
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

// mutatingStatement matches the leading keyword and target table of a
// mutating SQL statement, tolerant of this package's own formatting (a
// leading newline and tabs before the keyword). Mirrors
// internal/humanwork/workitem's own countingExecutor exactly, because
// PROMOUX-002's zero-domain-mutation proof is the same claim EP-WORK-003's
// is: prove an absence by naming every table a call actually wrote to, not
// by asserting "no error".
var mutatingStatement = regexp.MustCompile(`(?is)^\s*(INSERT INTO|UPDATE|DELETE FROM)\s+([a-zA-Z_][a-zA-Z0-9_]*)`)

// countingExecutor wraps a real dbport.Tx and counts every mutating
// statement (INSERT/UPDATE/DELETE) by the table it targets, leaving every
// read (Query/QueryRow) to pass straight through unmodified.
type countingExecutor struct {
	dbport.Tx
	mu     sync.Mutex
	writes map[string]int
}

func (c *countingExecutor) record(sql string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writes == nil {
		c.writes = map[string]int{}
	}
	if m := mutatingStatement.FindStringSubmatch(sql); m != nil {
		c.writes[m[2]]++
	}
}

func (c *countingExecutor) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	c.record(sql)
	return c.Tx.Exec(ctx, sql, args...)
}

func (c *countingExecutor) QueryRow(ctx context.Context, sql string, args ...any) dbport.Row {
	c.record(sql)
	return c.Tx.QueryRow(ctx, sql, args...)
}

// ---------------------------------------------------------------------------
// PRIMARY
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_002 is the primary: [promotionguard.Admit] is the whole
// admission decision, called directly with no UI, no journey engine and no
// transport in the way -- this is REFACTOR's claim ("the guard belongs to
// promotion admission ... UI suppression is not the integrity boundary")
// proved rather than asserted, because nothing here renders anything.
//
// It proves every clause of GREEN's first sentence: a worker gets exactly
// one admitted promotion per overlapping effective window (same date, first
// caller wins); a worker with a promotion pending for one date may still
// start a genuinely different, non-overlapping one for another date (GREEN's
// explicit carve-out); a retry presenting the same idempotency key resolves
// to the same reservation rather than refusing or duplicating; and a refused
// conflicting admission writes to nothing but its own bookkeeping row, and
// even that with zero rows affected -- proved by counting every mutating
// statement's target table, not by asserting "no error".
func TestTodo_PROMOUX_002(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedTenant(t, db, "promoux002-primary")
	ctx := context.Background()
	const worker = "EMPLOYMENT:promoux002-worker-1"

	t.Run("a fresh admission is granted and unconfirmed", func(t *testing.T) {
		tx := beginScoped(t, db, tenantID)
		defer func() { _ = tx.Rollback(ctx) }()
		guardID := uuid.New()
		decision, err := promotionguard.Admit(ctx, tx, tenantID, guardID, worker, "2027-01-01", "req-alpha")
		if err != nil {
			t.Fatalf("Admit = %v, want nil", err)
		}
		if decision.Replay {
			t.Fatal("a fresh admission must not report Replay")
		}
		if decision.GuardID != guardID {
			t.Fatalf("GuardID = %s, want the caller's own %s", decision.GuardID, guardID)
		}
		if decision.IntentID != uuid.Nil {
			t.Fatalf("IntentID = %s, want uuid.Nil before Confirm", decision.IntentID)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
	})

	t.Run("a second, differently-dated promotion for the same worker is not a conflict", func(t *testing.T) {
		// GREEN's explicit carve-out: "two promotions for the same worker
		// with genuinely non-overlapping windows are not a conflict." This
		// worker already has an ACTIVE window on 2027-01-01 from the
		// previous subtest (same tenant, same worker); a different
		// effective date must still be admitted.
		tx := beginScoped(t, db, tenantID)
		defer func() { _ = tx.Rollback(ctx) }()
		decision, err := promotionguard.Admit(ctx, tx, tenantID, uuid.New(), worker, "2028-06-01", "req-beta")
		if err != nil {
			t.Fatalf("Admit(non-overlapping date) = %v, want nil (not a conflict)", err)
		}
		if decision.Replay {
			t.Fatal("a genuinely new window must not report Replay")
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
	})

	t.Run("a different idempotency key for the same worker and date is refused, and writes nothing", func(t *testing.T) {
		tx := beginScoped(t, db, tenantID)
		defer func() { _ = tx.Rollback(ctx) }()
		counting := &countingExecutor{Tx: tx}
		_, err := promotionguard.Admit(ctx, counting, tenantID, uuid.New(), worker, "2027-01-01", "req-gamma-conflicting")
		if !errors.Is(err, promotionguard.ErrActiveConflict) {
			t.Fatalf("Admit(conflicting) = %v, want ErrActiveConflict", err)
		}
		// The absence proof: the one statement Admit issued targeted only
		// promotion_active_intent_guard, and even that wrote zero rows --
		// its ON CONFLICT DO UPDATE's WHERE clause matched nothing, so
		// PostgreSQL performed no update and returned no row. Any other
		// table appearing here, or any row actually written to this one,
		// would mean a refused conflicting start still had a side effect.
		for table, count := range counting.writes {
			if table != "promotion_active_intent_guard" {
				t.Fatalf("a refused Admit issued a mutating statement against unexpected table %q (%d statements)", table, count)
			}
		}
		var activeRows int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM promotion_active_intent_guard WHERE tenant_id=$1 AND worker_ref=$2 AND effective_date='2027-01-01' AND status='ACTIVE'`,
			tenantID, worker).Scan(&activeRows); err != nil {
			t.Fatalf("count active rows: %v", err)
		}
		if activeRows != 1 {
			t.Fatalf("active rows for the contested window = %d, want exactly 1 (the original admission, untouched)", activeRows)
		}
	})

	t.Run("a replay under the same idempotency key resolves to the same reservation", func(t *testing.T) {
		tx := beginScoped(t, db, tenantID)
		defer func() { _ = tx.Rollback(ctx) }()
		// req-alpha already claimed (worker, 2027-01-01) in the first
		// subtest. A second call presenting that same key -- a retried
		// "Start" click, a resumed client -- must resolve to that same
		// reservation, not refuse and not create a second one.
		decision, err := promotionguard.Admit(ctx, tx, tenantID, uuid.New(), worker, "2027-01-01", "req-alpha")
		if err != nil {
			t.Fatalf("Admit(replay) = %v, want nil", err)
		}
		if !decision.Replay {
			t.Fatal("a same-idempotency-key call must report Replay")
		}
		var totalRows int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM promotion_active_intent_guard WHERE tenant_id=$1 AND worker_ref=$2 AND effective_date='2027-01-01'`,
			tenantID, worker).Scan(&totalRows); err != nil {
			t.Fatalf("count rows: %v", err)
		}
		if totalRows != 1 {
			t.Fatalf("rows for (worker, 2027-01-01) = %d, want exactly 1 -- a replay must never create a second row", totalRows)
		}
	})

	t.Run("Confirm attaches the real intent id, once, and Release frees the window", func(t *testing.T) {
		tx := beginScoped(t, db, tenantID)
		defer func() { _ = tx.Rollback(ctx) }()
		guardID := uuid.New()
		decision, err := promotionguard.Admit(ctx, tx, tenantID, guardID, worker, "2029-03-01", "req-delta")
		if err != nil {
			t.Fatalf("Admit: %v", err)
		}
		intentID := uuid.New()
		if err := promotionguard.Confirm(ctx, tx, tenantID, decision.GuardID, "req-delta", intentID); err != nil {
			t.Fatalf("Confirm: %v", err)
		}
		// Idempotent: a second Confirm with the same arguments must not error.
		if err := promotionguard.Confirm(ctx, tx, tenantID, decision.GuardID, "req-delta", intentID); err != nil {
			t.Fatalf("Confirm (repeat): %v", err)
		}
		if err := promotionguard.Confirm(ctx, tx, tenantID, uuid.New(), "req-delta", intentID); !errors.Is(err, dbport.ErrNoRows) {
			t.Fatalf("Confirm (unknown guard) = %v, want missing-row refusal", err)
		}
		if err := promotionguard.Confirm(ctx, tx, tenantID, decision.GuardID, "req-delta", uuid.New()); !errors.Is(err, dbport.ErrNoRows) {
			t.Fatalf("Confirm (different intent) = %v, want binding refusal", err)
		}
		redecision, err := promotionguard.Admit(ctx, tx, tenantID, uuid.New(), worker, "2029-03-01", "req-delta")
		if err != nil {
			t.Fatalf("Admit(replay after confirm): %v", err)
		}
		if redecision.IntentID != intentID {
			t.Fatalf("IntentID after confirm = %s, want %s", redecision.IntentID, intentID)
		}

		if err := promotionguard.Release(ctx, tx, tenantID, intentID, time.Date(2029, 3, 2, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatalf("Release: %v", err)
		}
		// The window is free again: a different idempotency key for the
		// same worker and date must now be admitted rather than refused.
		reopened, err := promotionguard.Admit(ctx, tx, tenantID, uuid.New(), worker, "2029-03-01", "req-delta-two")
		if err != nil {
			t.Fatalf("Admit after Release = %v, want nil (the window was freed)", err)
		}
		if reopened.Replay {
			t.Fatal("a reservation opened after Release must not be reported as a replay of the closed one")
		}
	})

	t.Run("the zero value fails closed: every input is validated", func(t *testing.T) {
		tx := beginScoped(t, db, tenantID)
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := promotionguard.Admit(ctx, tx, uuid.Nil, uuid.New(), worker, "2027-01-01", "req"); !errors.Is(err, promotionguard.ErrInvalid) {
			t.Fatalf("Admit(nil tenant) = %v, want ErrInvalid", err)
		}
		if _, err := promotionguard.Admit(ctx, tx, tenantID, uuid.Nil, worker, "2027-01-01", "req"); !errors.Is(err, promotionguard.ErrInvalid) {
			t.Fatalf("Admit(nil guard id) = %v, want ErrInvalid", err)
		}
		if _, err := promotionguard.Admit(ctx, tx, tenantID, uuid.New(), "", "2027-01-01", "req"); !errors.Is(err, promotionguard.ErrInvalid) {
			t.Fatalf("Admit(empty worker) = %v, want ErrInvalid", err)
		}
		if _, err := promotionguard.Admit(ctx, tx, tenantID, uuid.New(), worker, "", "req"); !errors.Is(err, promotionguard.ErrInvalid) {
			t.Fatalf("Admit(empty date) = %v, want ErrInvalid", err)
		}
		if _, err := promotionguard.Admit(ctx, tx, tenantID, uuid.New(), worker, "2027-01-01", ""); !errors.Is(err, promotionguard.ErrInvalid) {
			t.Fatalf("Admit(empty idempotency key) = %v, want ErrInvalid", err)
		}
		if err := promotionguard.Confirm(ctx, tx, tenantID, uuid.Nil, "req", uuid.New()); !errors.Is(err, promotionguard.ErrInvalid) {
			t.Fatalf("Confirm(nil guard id) = %v, want ErrInvalid", err)
		}
		if err := promotionguard.Release(ctx, tx, tenantID, uuid.Nil, time.Now()); !errors.Is(err, promotionguard.ErrInvalid) {
			t.Fatalf("Release(nil intent id) = %v, want ErrInvalid", err)
		}
	})
}

// ---------------------------------------------------------------------------
// RECOVERY
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_002_Recovery proves the two-phase reservation survives a
// crash between Admit and Confirm: a caller mints a guard reservation, then
// (standing in for a process that died before it could call CreateIntent and
// Confirm) simply retries Admit with the same idempotency key. The retry
// must resolve to the same reservation rather than refusing or duplicating,
// and once the retry does confirm a real intent, that id is what every
// future replay reports.
func TestTodo_PROMOUX_002_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedTenant(t, db, "promoux002-recovery")
	ctx := context.Background()
	const worker = "EMPLOYMENT:promoux002-recovery-worker"

	tx := beginScoped(t, db, tenantID)
	defer func() { _ = tx.Rollback(ctx) }()

	// Phase A only: the reservation lands, but the process "crashes" before
	// CreateIntent or Confirm ever run.
	firstAttempt := uuid.New()
	first, err := promotionguard.Admit(ctx, tx, tenantID, firstAttempt, worker, "2027-09-01", "req-crash")
	if err != nil {
		t.Fatalf("Admit (pre-crash): %v", err)
	}
	if first.IntentID != uuid.Nil {
		t.Fatal("a fresh reservation must not already carry an intent id")
	}

	// The retry: same caller, same idempotency key, a newly minted (and
	// this time unused) candidate guard id -- exactly what a retried
	// request looks like from this package's point of view, since it has no
	// way to know the earlier attempt's guard id survived.
	retryAttempt := uuid.New()
	retry, err := promotionguard.Admit(ctx, tx, tenantID, retryAttempt, worker, "2027-09-01", "req-crash")
	if err != nil {
		t.Fatalf("Admit (retry after crash) = %v, want nil", err)
	}
	if !retry.Replay {
		t.Fatal("the retry must be reported as a replay of the pre-crash reservation")
	}
	if retry.GuardID != first.GuardID {
		t.Fatalf("retry GuardID = %s, want the original reservation's %s", retry.GuardID, first.GuardID)
	}
	if retry.IntentID != uuid.Nil {
		t.Fatal("the retry must still report no confirmed intent -- Confirm never ran")
	}

	var rowCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM promotion_active_intent_guard WHERE tenant_id=$1 AND worker_ref=$2 AND effective_date='2027-09-01'`,
		tenantID, worker).Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("rows for the crashed-and-retried window = %d, want exactly 1", rowCount)
	}

	// The retry now succeeds where the crashed attempt never got to:
	// CreateIntent mints a real id and the retry confirms it.
	recovered := uuid.New()
	if err := promotionguard.Confirm(ctx, tx, tenantID, retry.GuardID, "req-crash", recovered); err != nil {
		t.Fatalf("Confirm (recovered): %v", err)
	}
	final, err := promotionguard.Admit(ctx, tx, tenantID, uuid.New(), worker, "2027-09-01", "req-crash")
	if err != nil {
		t.Fatalf("Admit (post-recovery replay): %v", err)
	}
	if final.IntentID != recovered {
		t.Fatalf("post-recovery IntentID = %s, want the recovered %s", final.IntentID, recovered)
	}
}
