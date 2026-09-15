package promotionguard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Abandon closes one ACTIVE, unconfirmed reservation the caller opened but
// never turned into an intent (PROMOUX-017): the changed request whose
// CreateIntent refused its digest after the reservation for the new window
// already committed, leaving a second ACTIVE reservation behind.
//
// It reports whether it closed a row. It closes nothing -- and reports
// false with a nil error -- when the reservation is already confirmed, when
// an intent_instance row names its idempotency key (a lost confirmation the
// retry path still owns), when the key does not match (a stray cleanup must
// not free a window it does not own), or when the row is already closed or
// never existed. Cleanup running on top of an already-failed CreateIntent
// must report "nothing to close", never a second failure.
//
// Abandon is safe to call only for a reservation this call freshly admitted
// (Decision.Replay is false). A replayed reservation predates this call, so
// a failed retry must leave it ACTIVE for the original attempt to confirm.
// One residual is named rather than solved here: a concurrent retry
// presenting the same key may be mid-CreateIntent when Abandon checks for an
// intent row, and its commit landing afterwards attaches to a closed row --
// except [Confirm] now refuses closed rows, so that interleave fails loudly
// in logs and traces instead of silently leaving an unguarded intent, and
// the reservation's own key still resolves a later retry to a fresh
// admission once the window is free.
func Abandon(
	ctx context.Context, ex Executor,
	tenantID, guardID uuid.UUID, idempotencyKey string,
) (bool, error) {
	if err := requireTenant(tenantID); err != nil {
		return false, err
	}
	if guardID == uuid.Nil {
		return false, fmt.Errorf("%w: guard id is nil", ErrInvalid)
	}
	if err := requireField("idempotency_key", idempotencyKey); err != nil {
		return false, err
	}
	if ex == nil {
		return false, fmt.Errorf("%w: executor is nil", ErrInvalid)
	}
	key := strings.TrimSpace(idempotencyKey)
	closed, err := ex.Exec(ctx, `
		UPDATE promotion_active_intent_guard
		SET status = 'CLOSED', closed_at = now()
		WHERE tenant_id = $1 AND guard_id = $2 AND idempotency_key = $3
		  AND status = 'ACTIVE' AND intent_id IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM intent_instance i
		      WHERE i.tenant_id = $1 AND i.idempotency_key = $4)`,
		tenantID, guardID, key, key,
	)
	if err != nil {
		return false, fmt.Errorf("promotionguard: abandon: %w", err)
	}
	return closed > 0, nil
}

// Reclaim closes every ACTIVE, unconfirmed reservation one worker holds that
// is older than cutoff and names no committed intent (PROMOUX-017): the
// crash orphans a process that died between admission and CreateIntent
// leaves behind indefinitely. It returns how many rows it closed.
//
// The cutoff is the fence that makes this safe rather than merely eager. A
// reservation newer than the cutoff may still have a CreateIntent in flight
// -- its commit simply has not landed yet -- so reclaim never touches it,
// and admission passes a cutoff of now minus the orphan grace period for
// exactly that reason. A reservation older than the cutoff with no intent
// row for its key cannot still be in flight past any honest CreateIntent
// latency, so closing it cannot strand a committed promotion: committed
// intents are fenced off twice, once by intent_id and once by the
// intent_instance row the retry path still resolves.
//
// Reclaim is scoped to one worker: a sweep for a proposing worker never
// closes another worker's rows, and closing is by status transition to
// CLOSED (with closed_at, which the table's CHECK requires), never by
// DELETE, so the window admits again afterwards.
func Reclaim(
	ctx context.Context, ex Executor,
	tenantID uuid.UUID, workerRef string, cutoff time.Time,
) (int64, error) {
	if err := requireTenant(tenantID); err != nil {
		return 0, err
	}
	if err := requireField("worker_ref", workerRef); err != nil {
		return 0, err
	}
	if cutoff.IsZero() {
		return 0, fmt.Errorf("%w: reclaim cutoff is unset", ErrInvalid)
	}
	if ex == nil {
		return 0, fmt.Errorf("%w: executor is nil", ErrInvalid)
	}
	closed, err := ex.Exec(ctx, `
		UPDATE promotion_active_intent_guard g
		SET status = 'CLOSED', closed_at = now()
		WHERE g.tenant_id = $1 AND g.worker_ref = $2 AND g.status = 'ACTIVE'
		  AND g.intent_id IS NULL AND g.opened_at < $3
		  AND NOT EXISTS (
		      SELECT 1 FROM intent_instance i
		      WHERE i.tenant_id = $1 AND i.idempotency_key = g.idempotency_key)`,
		tenantID, strings.TrimSpace(workerRef), cutoff.UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("promotionguard: reclaim: %w", err)
	}
	return closed, nil
}
