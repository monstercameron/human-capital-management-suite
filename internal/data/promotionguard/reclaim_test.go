package promotionguard_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionguard"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// backdateGuard sets opened_at directly so reclaim tests control staleness
// without sleeping: a row backdated well before the cutoff is a crashed
// attempt, a row opened at PG now() is a live one.
func backdateGuard(t *testing.T, db *pgtest.DB, tenantID, guardID uuid.UUID, at time.Time) {
	t.Helper()
	tx := beginScoped(t, db, tenantID)
	defer func() { _ = tx.Rollback(context.Background()) }()
	n, err := tx.Exec(context.Background(),
		`UPDATE promotion_active_intent_guard SET opened_at=$3 WHERE tenant_id=$1 AND guard_id=$2`,
		tenantID, guardID, at.UTC())
	if err != nil {
		t.Fatalf("backdate guard: %v", err)
	}
	if n != 1 {
		t.Fatalf("backdate guard affected %d rows, want 1", n)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit backdate: %v", err)
	}
}

// guardState reads one reservation's durable state: status, confirmed intent
// (uuid.Nil when unconfirmed) and whether closed_at is set.
func guardState(t *testing.T, db *pgtest.DB, tenantID, guardID uuid.UUID) (string, uuid.UUID, bool) {
	t.Helper()
	tx := beginScoped(t, db, tenantID)
	defer func() { _ = tx.Rollback(context.Background()) }()
	var (
		status   string
		intentID *uuid.UUID
		closedAt *time.Time
	)
	err := tx.QueryRow(context.Background(),
		`SELECT status, intent_id, closed_at FROM promotion_active_intent_guard WHERE tenant_id=$1 AND guard_id=$2`,
		tenantID, guardID).Scan(&status, &intentID, &closedAt)
	if err != nil {
		t.Fatalf("read guard state: %v", err)
	}
	var confirmed uuid.UUID
	if intentID != nil {
		confirmed = *intentID
	}
	return status, confirmed, closedAt != nil
}

// countActiveWindows counts ACTIVE reservations for one worker and date: the
// "exactly one intended active window" invariant, read from the table rather
// than inferred from return codes.
func countActiveWindows(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, worker, date string) int {
	t.Helper()
	tx := beginScoped(t, db, tenantID)
	defer func() { _ = tx.Rollback(context.Background()) }()
	var n int
	if err := tx.QueryRow(context.Background(),
		`SELECT count(*) FROM promotion_active_intent_guard WHERE tenant_id=$1 AND worker_ref=$2 AND effective_date=$3 AND status='ACTIVE'`,
		tenantID, worker, date).Scan(&n); err != nil {
		t.Fatalf("count active windows: %v", err)
	}
	return n
}

// seedIntentRow records a committed intent_instance row for key, the durable
// proof a CreateIntent actually landed. Reclaim and Abandon must never close
// a reservation whose key names a row here, confirmed or not.
func seedIntentRow(t *testing.T, db *pgtest.DB, tenantID, intentID uuid.UUID, key string, at time.Time) {
	t.Helper()
	sum := sha256.Sum256([]byte("request:" + key))
	db.Exec(t, `
		INSERT INTO intent_instance (
			tenant_id, intent_id, definition_ref, definition_version,
			request_digest, idempotency_key,
			request_state, execution_state, business_state, consistency_state, obligation_state,
			created_at, last_transition_at)
		VALUES ($1, $2, 'promotion.request', 1, $3, $4,
			'DRAFT', 'NOT_PLANNED', 'NOT_STARTED', 'NOT_APPLICABLE', 'NOT_APPLICABLE',
			$5, $5)`,
		tenantID, intentID, hex.EncodeToString(sum[:]), key, at.UTC())
}

func admitFresh(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, worker, date, key string) promotionguard.Decision {
	t.Helper()
	tx := beginScoped(t, db, tenantID)
	defer func() { _ = tx.Rollback(context.Background()) }()
	d, err := promotionguard.Admit(context.Background(), tx, tenantID, uuid.New(), worker, date, key)
	if err != nil {
		t.Fatalf("admit %s/%s: %v", worker, date, err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit admit %s/%s: %v", worker, date, err)
	}
	return d
}

// abandonCommitted runs Abandon in a tenant-scoped transaction and commits,
// the shape the journey engine's own wiring uses.
func abandonCommitted(t *testing.T, db *pgtest.DB, tenantID, guardID uuid.UUID, key string) (bool, error) {
	t.Helper()
	tx := beginScoped(t, db, tenantID)
	defer func() { _ = tx.Rollback(context.Background()) }()
	closed, err := promotionguard.Abandon(context.Background(), tx, tenantID, guardID, key)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit abandon: %v", err)
	}
	return closed, nil
}

// reclaimCommitted runs Reclaim scoped and committed, like admission does.
func reclaimCommitted(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, worker string, cutoff time.Time) (int64, error) {
	t.Helper()
	tx := beginScoped(t, db, tenantID)
	defer func() { _ = tx.Rollback(context.Background()) }()
	n, err := promotionguard.Reclaim(context.Background(), tx, tenantID, worker, cutoff)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit reclaim: %v", err)
	}
	return n, nil
}

// confirmScoped confirms a reservation in its own scoped transaction.
func confirmScoped(t *testing.T, db *pgtest.DB, tenantID, guardID uuid.UUID, key string, intentID uuid.UUID) {
	t.Helper()
	tx := beginScoped(t, db, tenantID)
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := promotionguard.Confirm(context.Background(), tx, tenantID, guardID, key, intentID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit confirm: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PRIMARY
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_017 is the primary: abandoning a fresh reservation whose
// CreateIntent never landed, and reclaiming crashed orphans under a fence,
// while a committed intent's guard is never closed.
//
// RED's first defect is a changed request reusing an idempotency key on a
// different effective day: CreateIntent refuses the changed digest after a
// fresh reservation already committed for the new window, leaving a second
// ACTIVE reservation. Abandon closes exactly that reservation. RED's second
// defect is a crash between admission and CreateIntent: the orphan has no
// intent, so the next admission for the worker reclaims it when it is older
// than the fence, and only then.
func TestTodo_PROMOUX_017(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedTenant(t, db, "promoux017-primary")
	const worker = "EMPLOYMENT:promoux017-worker"
	old := time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)
	cutoff := time.Now().UTC().Add(-time.Minute)

	// A fresh reservation whose CreateIntent is refused is abandoned: the
	// call reports it closed exactly one row.
	d1 := admitFresh(t, db, tenantID, worker, "2027-03-01", "promoux017-key-1")
	if d1.Replay {
		t.Fatal("first admission must not report a replay")
	}
	closed, err := abandonCommitted(t, db, tenantID, d1.GuardID, "promoux017-key-1")
	if err != nil {
		t.Fatalf("Abandon(fresh orphan) = %v, want nil", err)
	}
	if !closed {
		t.Fatal("Abandon(fresh orphan) = false, want true: the refused reservation must close")
	}
	if status, _, hasClosedAt := guardState(t, db, tenantID, d1.GuardID); status != "CLOSED" || !hasClosedAt {
		t.Fatalf("abandoned row = %q closed_at-set=%t, want CLOSED with closed_at", status, hasClosedAt)
	}

	// The window is genuinely free again: the same worker and date admit a
	// new key, which is what "no orphan window" means durably.
	d2 := admitFresh(t, db, tenantID, worker, "2027-03-01", "promoux017-key-2")
	if d2.Replay || d2.GuardID == d1.GuardID {
		t.Fatal("abandoned window must admit a new reservation, not replay the closed one")
	}

	// A confirmed reservation is never abandoned: the committed intent's
	// guard survives, with its intent binding intact.
	confirmScoped(t, db, tenantID, d2.GuardID, "promoux017-key-2", uuid.New())
	closed, err = abandonCommitted(t, db, tenantID, d2.GuardID, "promoux017-key-2")
	if err != nil {
		t.Fatalf("Abandon(confirmed) = %v, want nil", err)
	}
	if closed {
		t.Fatal("Abandon(confirmed) = true: a committed intent's guard must never close")
	}
	if status, confirmed, _ := guardState(t, db, tenantID, d2.GuardID); status != "ACTIVE" || confirmed == uuid.Nil {
		t.Fatalf("confirmed row after Abandon = %q intent %v, want ACTIVE with intent bound", status, confirmed)
	}

	// A lost confirmation (intent committed, Confirm never ran) is also
	// never abandoned: the intent row, not the guard's intent_id column, is
	// what proves the promotion exists.
	d3 := admitFresh(t, db, tenantID, worker, "2027-04-01", "promoux017-key-3")
	seedIntentRow(t, db, tenantID, uuid.New(), "promoux017-key-3", time.Now().UTC())
	closed, err = abandonCommitted(t, db, tenantID, d3.GuardID, "promoux017-key-3")
	if err != nil {
		t.Fatalf("Abandon(intent-backed) = %v, want nil", err)
	}
	if closed {
		t.Fatal("Abandon(intent-backed) = true: an existing intent protects its window even unconfirmed")
	}

	// Reclaim closes only the stale, intentless orphan: the recent orphan
	// (a CreateIntent may still be in flight), the stale intent-backed row
	// and the other worker's stale orphan all survive.
	stale := admitFresh(t, db, tenantID, worker, "2027-05-01", "promoux017-key-4")
	backdateGuard(t, db, tenantID, stale.GuardID, old)
	recent := admitFresh(t, db, tenantID, worker, "2027-06-01", "promoux017-key-5")
	backed := admitFresh(t, db, tenantID, worker, "2027-07-01", "promoux017-key-6")
	seedIntentRow(t, db, tenantID, uuid.New(), "promoux017-key-6", time.Now().UTC())
	backdateGuard(t, db, tenantID, backed.GuardID, old)
	foreign := admitFresh(t, db, tenantID, "EMPLOYMENT:promoux017-other", "2027-05-01", "promoux017-key-7")
	backdateGuard(t, db, tenantID, foreign.GuardID, old)

	reclaimed, err := reclaimCommitted(t, db, tenantID, worker, cutoff)
	if err != nil {
		t.Fatalf("Reclaim = %v, want nil", err)
	}
	if reclaimed != 1 {
		t.Fatalf("Reclaim closed %d rows, want exactly 1 (the stale intentless orphan)", reclaimed)
	}
	if status, _, _ := guardState(t, db, tenantID, stale.GuardID); status != "CLOSED" {
		t.Fatalf("stale orphan = %q, want CLOSED", status)
	}
	for name, id := range map[string]uuid.UUID{"recent": recent.GuardID, "intent-backed": backed.GuardID, "other-worker": foreign.GuardID} {
		if status, _, _ := guardState(t, db, tenantID, id); status != "ACTIVE" {
			t.Fatalf("%s row = %q, want ACTIVE: reclaim must not touch it", name, status)
		}
	}

	// The reclaimed window admits again, and the abandoned key stays usable
	// for a genuinely new window: neither operation poisons later requests.
	again := admitFresh(t, db, tenantID, worker, "2027-05-01", "promoux017-key-8")
	if again.Replay {
		t.Fatal("reclaimed window must admit fresh, not replay")
	}
	fresh := admitFresh(t, db, tenantID, worker, "2027-08-01", "promoux017-key-1")
	if fresh.Replay {
		t.Fatal("an abandoned key must admit fresh on a new window")
	}
}

// ---------------------------------------------------------------------------
// FAULT
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_017_Fault proves every abandon/reclaim failpoint reaches
// an allowed durable state: unknown rows report "nothing closed" rather than
// erroring, invalid inputs are refused before any statement runs, a dead
// transaction surfaces its error, and both operations are idempotent.
func TestTodo_PROMOUX_017_Fault(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := seedTenant(t, db, "promoux017-fault")
	const worker = "EMPLOYMENT:promoux017-fault-worker"

	for name, fn := range map[string]func() error{
		"abandon nil executor": func() error {
			_, err := promotionguard.Abandon(ctx, nil, tenantID, uuid.New(), "k")
			return err
		},
		"abandon nil tenant": func() error {
			tx := beginScoped(t, db, tenantID)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err := promotionguard.Abandon(ctx, tx, uuid.Nil, uuid.New(), "k")
			return err
		},
		"abandon nil guard": func() error {
			tx := beginScoped(t, db, tenantID)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err := promotionguard.Abandon(ctx, tx, tenantID, uuid.Nil, "k")
			return err
		},
		"abandon blank key": func() error {
			tx := beginScoped(t, db, tenantID)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err := promotionguard.Abandon(ctx, tx, tenantID, uuid.New(), "  ")
			return err
		},
		"reclaim nil executor": func() error {
			_, err := promotionguard.Reclaim(ctx, nil, tenantID, worker, time.Now().UTC())
			return err
		},
		"reclaim nil tenant": func() error {
			tx := beginScoped(t, db, tenantID)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err := promotionguard.Reclaim(ctx, tx, uuid.Nil, worker, time.Now().UTC())
			return err
		},
		"reclaim blank worker": func() error {
			tx := beginScoped(t, db, tenantID)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err := promotionguard.Reclaim(ctx, tx, tenantID, "  ", time.Now().UTC())
			return err
		},
		"reclaim zero cutoff": func() error {
			tx := beginScoped(t, db, tenantID)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err := promotionguard.Reclaim(ctx, tx, tenantID, worker, time.Time{})
			return err
		},
	} {
		if err := fn(); !errors.Is(err, promotionguard.ErrInvalid) {
			t.Fatalf("%s = %v, want ErrInvalid", name, err)
		}
	}

	// An unknown guard is "nothing to close", not a failure: the caller's
	// CreateIntent already failed, and cleanup must not fail on top of it.
	closed, err := abandonCommitted(t, db, tenantID, uuid.New(), "promoux017-missing")
	if err != nil {
		t.Fatalf("Abandon(unknown) = %v, want nil", err)
	}
	if closed {
		t.Fatal("Abandon(unknown) = true, want false")
	}

	// Abandon is idempotent: the second call finds the row already closed
	// and reports nothing closed, without erroring or reopening it.
	d := admitFresh(t, db, tenantID, worker, "2027-09-01", "promoux017-fault-key")
	if closed, err := abandonCommitted(t, db, tenantID, d.GuardID, "promoux017-fault-key"); err != nil || !closed {
		t.Fatalf("first Abandon = %t/%v, want true/nil", closed, err)
	}
	if closed, err := abandonCommitted(t, db, tenantID, d.GuardID, "promoux017-fault-key"); err != nil || closed {
		t.Fatalf("second Abandon = %t/%v, want false/nil", closed, err)
	}
	if status, _, _ := guardState(t, db, tenantID, d.GuardID); status != "CLOSED" {
		t.Fatalf("double-abandoned row = %q, want CLOSED", status)
	}

	// A dead transaction surfaces its error rather than reporting success:
	// cleanup running on a broken connection must be loud, not silent.
	dead := beginScoped(t, db, tenantID)
	_ = dead.Rollback(ctx)
	if _, err := promotionguard.Abandon(ctx, dead, tenantID, uuid.New(), "k"); err == nil {
		t.Fatal("Abandon on a rolled-back transaction = nil, want the transaction error")
	}
	deadReclaim := beginScoped(t, db, tenantID)
	_ = deadReclaim.Rollback(ctx)
	if _, err := promotionguard.Reclaim(ctx, deadReclaim, tenantID, worker, time.Now().UTC()); err == nil {
		t.Fatal("Reclaim on a rolled-back transaction = nil, want the transaction error")
	}

	// Reclaim with no match closes nothing and changes nothing: running it
	// twice proves it neither errors on an empty set nor double-counts.
	for i := 0; i < 2; i++ {
		n, err := reclaimCommitted(t, db, tenantID, worker, time.Now().UTC().Add(-time.Minute))
		if err != nil {
			t.Fatalf("Reclaim(empty) = %v, want nil", err)
		}
		if i == 1 && n != 0 {
			t.Fatalf("second Reclaim = %d, want 0", n)
		}
	}
	if got := countActiveWindows(t, db, tenantID, worker, "2027-09-01"); got != 0 {
		t.Fatalf("ACTIVE rows after abandon = %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// RECOVERY
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_017_Recovery proves the crash-point matrix: a reservation
// orphaned by a crash before CreateIntent is reclaimed and its window admits
// again, while a reservation whose intent committed but whose Confirm was
// lost is never reclaimed and the same caller's retry resolves back to it
// and confirms it.
func TestTodo_PROMOUX_017_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedTenant(t, db, "promoux017-recovery")
	const worker = "EMPLOYMENT:promoux017-recovery-worker"
	old := time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)
	cutoff := time.Now().UTC().Add(-time.Minute)

	// Crash between admission and CreateIntent: no intent row exists, the
	// orphan is older than the fence, reclaim closes it, and the window
	// admits a new key -- exactly one ACTIVE row, durably.
	crashed := admitFresh(t, db, tenantID, worker, "2027-10-01", "promoux017-crash-key")
	backdateGuard(t, db, tenantID, crashed.GuardID, old)
	n, err := reclaimCommitted(t, db, tenantID, worker, cutoff)
	if err != nil {
		t.Fatalf("Reclaim(crash orphan) = %v, want nil", err)
	}
	if n != 1 {
		t.Fatalf("Reclaim(crash orphan) closed %d, want 1", n)
	}
	after := admitFresh(t, db, tenantID, worker, "2027-10-01", "promoux017-retry-key")
	if after.Replay {
		t.Fatal("reclaimed window must admit the retry fresh")
	}
	if got := countActiveWindows(t, db, tenantID, worker, "2027-10-01"); got != 1 {
		t.Fatalf("ACTIVE rows after crash recovery = %d, want exactly 1", got)
	}

	// Crash between CreateIntent and Confirm: the intent row exists, so the
	// stale reservation is fenced off from reclaim; the same key replays to
	// the same reservation and Confirm attaches the intent.
	lost := admitFresh(t, db, tenantID, worker, "2027-11-01", "promoux017-lost-key")
	intentID := uuid.New()
	seedIntentRow(t, db, tenantID, intentID, "promoux017-lost-key", time.Now().UTC())
	backdateGuard(t, db, tenantID, lost.GuardID, old)
	if n, err := reclaimCommitted(t, db, tenantID, worker, cutoff); err != nil || n != 0 {
		t.Fatalf("Reclaim(lost confirmation) = %d/%v, want 0/nil: the committed intent protects its window", n, err)
	}
	retry := admitFresh(t, db, tenantID, worker, "2027-11-01", "promoux017-lost-key")
	if !retry.Replay || retry.GuardID != lost.GuardID {
		t.Fatalf("retry = replay=%t guard=%v, want replay of %v", retry.Replay, retry.GuardID, lost.GuardID)
	}
	if retry.IntentID != uuid.Nil {
		t.Fatalf("retry IntentID = %v, want zero: Confirm never ran", retry.IntentID)
	}
	confirmScoped(t, db, tenantID, lost.GuardID, "promoux017-lost-key", intentID)
	if status, confirmed, _ := guardState(t, db, tenantID, lost.GuardID); status != "ACTIVE" || confirmed != intentID {
		t.Fatalf("recovered row = %q intent %v, want ACTIVE bound to %v", status, confirmed, intentID)
	}

	// A stale confirmed reservation is never reclaimed either: age alone
	// closes nothing that an intent still owns.
	backdateGuard(t, db, tenantID, lost.GuardID, old)
	if n, err := reclaimCommitted(t, db, tenantID, worker, cutoff); err != nil || n != 0 {
		t.Fatalf("Reclaim(stale confirmed) = %d/%v, want 0/nil", n, err)
	}
}

// ---------------------------------------------------------------------------
// RACE
// ---------------------------------------------------------------------------

// TestTodo_PROMOUX_017_Race storms genuinely concurrent fresh admissions,
// same-key replays and a reclaim sweep against one worker, and proves the
// fence holds under concurrency: exactly one fresh admission wins, every
// replay resolves to the pre-committed reservation, the planted stale orphan
// is reclaimed mid-storm, and no live row is ever closed.
func TestTodo_PROMOUX_017_Race(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := seedTenant(t, db, "promoux017-race")
	const worker = "EMPLOYMENT:promoux017-race-worker"
	const contestedDate = "2027-12-01"
	const replayDate = "2027-12-02"
	const staleDate = "2027-12-03"

	anchor := admitFresh(t, db, tenantID, worker, replayDate, "promoux017-race-replay")
	anchorIntent := uuid.New()
	confirmScoped(t, db, tenantID, anchor.GuardID, "promoux017-race-replay", anchorIntent)
	orphan := admitFresh(t, db, tenantID, worker, staleDate, "promoux017-race-orphan")
	backdateGuard(t, db, tenantID, orphan.GuardID, time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC))

	// The fence predates every row this storm opens (they carry PG now())
	// but postdates the planted orphan: reclaim can only ever match the
	// orphan, never a live row, no matter how the goroutines interleave.
	cutoff := time.Now().UTC().Add(-time.Minute)
	const fresh = 8
	const replays = 4
	const sweeps = 2

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		admitted  []promotionguard.Decision
		replayed  []promotionguard.Decision
		conflicts int
		swept     int64
		failures  []error
	)
	start := make(chan struct{})
	// Each goroutine holds its own connection and transaction
	// (db.Conn is not safe for concurrent use; NewConn is the escape hatch
	// TestTodo_PROMOUX_002_Race's own comment names), so exclusion comes
	// from PostgreSQL serializing the commits, not from shared-session
	// turn-taking.
	storm := func(isReplay bool) {
		defer wg.Done()
		<-start
		conn := db.NewConn(t)
		tx, err := conn.Begin(ctx)
		if err != nil {
			mu.Lock()
			failures = append(failures, err)
			mu.Unlock()
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
			mu.Lock()
			failures = append(failures, err)
			mu.Unlock()
			return
		}
		if isReplay {
			d, err := promotionguard.Admit(ctx, tx, tenantID, uuid.New(), worker, replayDate, "promoux017-race-replay")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, err)
				return
			}
			if err := tx.Commit(ctx); err != nil {
				failures = append(failures, err)
				return
			}
			replayed = append(replayed, d)
			return
		}
		d, err := promotionguard.Admit(ctx, tx, tenantID, uuid.New(), worker, contestedDate, "promoux017-race-"+uuid.NewString())
		mu.Lock()
		defer mu.Unlock()
		if err == nil {
			if err := tx.Commit(ctx); err != nil {
				failures = append(failures, err)
				return
			}
			admitted = append(admitted, d)
			return
		}
		if errors.Is(err, promotionguard.ErrActiveConflict) {
			conflicts++
			return
		}
		failures = append(failures, err)
	}
	sweep := func() {
		defer wg.Done()
		<-start
		conn := db.NewConn(t)
		tx, err := conn.Begin(ctx)
		if err != nil {
			mu.Lock()
			failures = append(failures, err)
			mu.Unlock()
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
			mu.Lock()
			failures = append(failures, err)
			mu.Unlock()
			return
		}
		n, err := promotionguard.Reclaim(ctx, tx, tenantID, worker, cutoff)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			failures = append(failures, err)
			return
		}
		if err := tx.Commit(ctx); err != nil {
			failures = append(failures, err)
			return
		}
		swept += n
	}

	for i := 0; i < fresh+replays+sweeps; i++ {
		wg.Add(1)
		switch {
		case i < fresh:
			go storm(false)
		case i < fresh+replays:
			go storm(true)
		default:
			go sweep()
		}
	}
	close(start)
	wg.Wait()

	for _, err := range failures {
		t.Errorf("storm failure: %v", err)
	}
	if len(admitted) != 1 {
		t.Fatalf("fresh admissions won = %d, want exactly 1", len(admitted))
	}
	if conflicts != fresh-1 {
		t.Fatalf("conflicts = %d, want %d", conflicts, fresh-1)
	}
	if len(replayed) != replays {
		t.Fatalf("replays resolved = %d, want %d", len(replayed), replays)
	}
	for _, d := range replayed {
		if !d.Replay || d.GuardID != anchor.GuardID || d.IntentID != anchorIntent {
			t.Fatalf("replay = %+v, want replay of anchor %v bound to %v", d, anchor.GuardID, anchorIntent)
		}
	}
	if swept != 1 {
		t.Fatalf("reclaim sweeps closed %d rows mid-storm, want exactly 1 (the planted orphan)", swept)
	}
	if status, _, _ := guardState(t, db, tenantID, orphan.GuardID); status != "CLOSED" {
		t.Fatalf("planted orphan = %q, want CLOSED", status)
	}
	if status, confirmed, _ := guardState(t, db, tenantID, admitted[0].GuardID); status != "ACTIVE" || confirmed != uuid.Nil {
		t.Fatalf("winner = %q intent %v, want ACTIVE unconfirmed: reclaim must not kill live rows", status, confirmed)
	}
	if status, confirmed, _ := guardState(t, db, tenantID, anchor.GuardID); status != "ACTIVE" || confirmed != anchorIntent {
		t.Fatalf("anchor = %q intent %v, want ACTIVE bound: reclaim must not touch confirmed rows", status, confirmed)
	}
	if got := countActiveWindows(t, db, tenantID, worker, contestedDate); got != 1 {
		t.Fatalf("durable ACTIVE rows for the contested window = %d, want exactly 1", got)
	}

	// Abandonment binds the key as well as the guard: closing with a
	// mismatched key closes nothing, so a stray cleanup cannot free a
	// window it does not own.
	closed, err := abandonCommitted(t, db, tenantID, admitted[0].GuardID, admitted[0].GuardID.String())
	if err != nil {
		t.Fatalf("Abandon(winner, mismatched key) = %v, want nil", err)
	}
	if closed {
		t.Fatal("Abandon with a mismatched key must close nothing")
	}
}
