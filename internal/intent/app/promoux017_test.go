package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// backdateWireGuard sets a reservation's opened_at through the engine's own
// database handle, so reclaim-fence tests control staleness without
// sleeping.
func backdateWireGuard(t *testing.T, e *journeyEngine, principal *trust.Principal, guardID uuid.UUID, at time.Time) {
	t.Helper()
	ctx := trust.WithPrincipal(context.Background(), principal)
	tx, err := e.db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin backdate: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, e.svc.tenantUUID(principal.Tenant())); err != nil {
		t.Fatalf("scope backdate: %v", err)
	}
	n, err := tx.Exec(ctx,
		`UPDATE promotion_active_intent_guard SET opened_at=$1 WHERE guard_id=$2`, at.UTC(), guardID)
	if err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if n != 1 {
		t.Fatalf("backdate affected %d rows, want 1", n)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit backdate: %v", err)
	}
}

// wireGuardState reads one reservation's durable status through the engine's
// own database handle.
func wireGuardState(t *testing.T, e *journeyEngine, principal *trust.Principal, guardID uuid.UUID) (string, string) {
	t.Helper()
	ctx := trust.WithPrincipal(context.Background(), principal)
	tx, err := e.db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin read: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, e.svc.tenantUUID(principal.Tenant())); err != nil {
		t.Fatalf("scope read: %v", err)
	}
	var (
		status   string
		intentID *uuid.UUID
	)
	if err := tx.QueryRow(ctx,
		`SELECT status, intent_id FROM promotion_active_intent_guard WHERE guard_id=$1`, guardID,
	).Scan(&status, &intentID); err != nil {
		t.Fatalf("read guard: %v", err)
	}
	var confirmed string
	if intentID != nil {
		confirmed = intentID.String()
	}
	return status, confirmed
}

// TestTodo_PROMOUX_017_Integration drives the engine's own admission wiring
// end to end against real PostgreSQL: a crashed orphan is reclaimed as a
// side effect of the next admission, a refused CreateIntent abandons exactly
// the reservation it opened, and a successful one confirms it.
//
// The injected create func stands in for CreateIntent at the exact seam the
// two Propose paths share, so the refused-changed-digest sequence RED names
// is proved without dragging the full intent kernel into this test: what is
// proved is that this engine abandons on exactly the failure its callers
// hand it, preserves the caller's own error, and never abandons a replayed
// reservation it did not open.
func TestTodo_PROMOUX_017_Integration(t *testing.T) {
	e, principal, _ := promotionGuardWireFixture(t)
	ctx := trust.WithPrincipal(context.Background(), principal)
	const worker = "EMPLOYMENT:promoux017-wire-worker"

	// A crashed orphan (admitted, never created, older than the fence) is
	// reclaimed by the next admission for the same worker: the admitted
	// call succeeds and the orphan row is durably CLOSED.
	orphan, replayed, err := e.admitPromotionWindow(ctx, principal, worker, "2027-01-01", "promoux017-wire-old")
	if err != nil {
		t.Fatalf("admit orphan: %v", err)
	}
	if replayed {
		t.Fatal("first admission must not replay")
	}
	backdateWireGuard(t, e, principal, orphan, time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC))
	next, replayed, err := e.admitPromotionWindow(ctx, principal, worker, "2027-02-01", "promoux017-wire-new")
	if err != nil {
		t.Fatalf("admit after crash: %v", err)
	}
	if replayed {
		t.Fatal("admission on a new window must not replay")
	}
	if status, _ := wireGuardState(t, e, principal, orphan); status != "CLOSED" {
		t.Fatalf("crashed orphan after next admission = %q, want CLOSED: admission must reclaim it", status)
	}
	if status, _ := wireGuardState(t, e, principal, next); status != "ACTIVE" {
		t.Fatalf("new reservation = %q, want ACTIVE", status)
	}

	// A refused CreateIntent abandons exactly the reservation the failed
	// call opened: the caller's own error travels unchanged and the window
	// admits again immediately.
	boom := errors.New("promoux017 refused changed digest")
	if _, err := e.createGuardedIntent(ctx, principal, worker, "2027-03-01", "promoux017-wire-fail",
		func() (string, error) { return "", boom }); !errors.Is(err, boom) {
		t.Fatalf("createGuardedIntent(refused) = %v, want the caller's own error", err)
	}
	// The failed call above admitted-then-abandoned, so the window is free:
	// proving it by re-admitting, then abandoning through the wire method
	// directly to prove that composition too.
	retry, replayed, err := e.admitPromotionWindow(ctx, principal, worker, "2027-03-01", "promoux017-wire-retry")
	if err != nil {
		t.Fatalf("re-admit after refused create: %v", err)
	}
	if replayed {
		t.Fatal("abandoned window must admit fresh")
	}
	if err := e.abandonPromotionWindow(ctx, principal, retry, "promoux017-wire-retry"); err != nil {
		t.Fatalf("abandonPromotionWindow = %v, want nil", err)
	}
	if status, _ := wireGuardState(t, e, principal, retry); status != "CLOSED" {
		t.Fatalf("abandoned reservation = %q, want CLOSED", status)
	}

	// A replayed reservation is never abandoned on failure: the row predates
	// this call, so the failed retry leaves it ACTIVE for the original
	// attempt (or its own retry) to still confirm.
	first, _, err := e.admitPromotionWindow(ctx, principal, worker, "2027-04-01", "promoux017-wire-shared")
	if err != nil {
		t.Fatalf("admit shared: %v", err)
	}
	second, replayed, err := e.admitPromotionWindow(ctx, principal, worker, "2027-04-01", "promoux017-wire-shared")
	if err != nil {
		t.Fatalf("re-admit shared: %v", err)
	}
	if !replayed || second != first {
		t.Fatalf("same-key admission = replay=%t %v, want replay of %v", replayed, second, first)
	}
	if _, err := e.createGuardedIntent(ctx, principal, worker, "2027-04-01", "promoux017-wire-shared",
		func() (string, error) { return "", boom }); !errors.Is(err, boom) {
		t.Fatalf("createGuardedIntent(replay, refused) = %v, want the caller's error", err)
	}
	if status, _ := wireGuardState(t, e, principal, first); status != "ACTIVE" {
		t.Fatalf("replayed reservation after failed retry = %q, want ACTIVE: a retry must not kill the original", status)
	}

	// A successful create confirms the reservation to the minted intent,
	// and a lost confirm (unparseable id, the way a crashed caller would
	// leave it) never fails the call: the intent exists, the booking is
	// best-effort, and the row stays ACTIVE for reclaim's intent check and
	// a later retry to resolve.
	intentID := uuid.NewString()
	got, err := e.createGuardedIntent(ctx, principal, worker, "2027-05-01", "promoux017-wire-ok",
		func() (string, error) { return intentID, nil })
	if err != nil {
		t.Fatalf("createGuardedIntent(ok) = %v, want nil", err)
	}
	if got != intentID {
		t.Fatalf("createGuardedIntent(ok) = %q, want %q", got, intentID)
	}
	confirmedID := ""
	{
		status, confirmed := wireGuardState(t, e, principal, mustWireGuard(t, e, principal, worker, "2027-05-01"))
		if status != "ACTIVE" {
			t.Fatalf("confirmed reservation = %q, want ACTIVE", status)
		}
		confirmedID = confirmed
	}
	if confirmedID != intentID {
		t.Fatalf("confirmed intent = %q, want %q", confirmedID, intentID)
	}
	lostID := "not-a-uuid"
	if got, err := e.createGuardedIntent(ctx, principal, worker, "2027-06-01", "promoux017-wire-lost",
		func() (string, error) { return lostID, nil }); err != nil || got != lostID {
		t.Fatalf("createGuardedIntent(lost confirm) = %q/%v, want %q/nil: a failed booking never fails the call", got, err, lostID)
	}
	if status, _ := wireGuardState(t, e, principal, mustWireGuard(t, e, principal, worker, "2027-06-01")); status != "ACTIVE" {
		t.Fatalf("unconfirmed reservation = %q, want ACTIVE", status)
	}
}

// mustWireGuard resolves the ACTIVE reservation one worker and date holds,
// failing the test when the window does not hold exactly one.
func mustWireGuard(t *testing.T, e *journeyEngine, principal *trust.Principal, worker, date string) uuid.UUID {
	t.Helper()
	ctx := trust.WithPrincipal(context.Background(), principal)
	tx, err := e.db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin resolve: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, e.svc.tenantUUID(principal.Tenant())); err != nil {
		t.Fatalf("scope resolve: %v", err)
	}
	var (
		id uuid.UUID
		n  int
	)
	if err := tx.QueryRow(ctx,
		`SELECT guard_id FROM promotion_active_intent_guard WHERE worker_ref=$1 AND effective_date=$2 AND status='ACTIVE'`,
		worker, date).Scan(&id); err != nil {
		t.Fatalf("resolve guard %s/%s: %v", worker, date, err)
	}
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM promotion_active_intent_guard WHERE worker_ref=$1 AND effective_date=$2 AND status='ACTIVE'`,
		worker, date).Scan(&n); err != nil {
		t.Fatalf("count guards %s/%s: %v", worker, date, err)
	}
	if n != 1 {
		t.Fatalf("ACTIVE guards for %s/%s = %d, want exactly 1", worker, date, n)
	}
	return id
}
