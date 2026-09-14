package promotionguard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Executor is the minimal database capability this package needs. A
// [dbport.Tx] and a [dbport.Conn] both satisfy it.
//
// Every method takes it explicitly rather than holding a handle: the guard
// table is row-level-security protected, so the caller has to have scoped
// its transaction to a tenant (internal/data/tenancy.WithTenant) before any
// statement here runs, exactly as internal/data/intentcontrol already
// requires of its own callers.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// ErrInvalid means a required input was empty, zero, or malformed.
var ErrInvalid = errors.New("promotionguard: invalid input")

// ErrActiveConflict is what [Admit] returns when a different request already
// holds the (worker, effective date) window: the caller's idempotency key
// does not match the reservation already recorded for that window, so this
// is a genuinely different, conflicting promotion rather than a replay of
// the same one.
var ErrActiveConflict = errors.New("promotionguard: an active promotion already claims this worker and effective window")

// Decision is what [Admit] resolved.
type Decision struct {
	// GuardID is the reservation row's own identity: the caller's own minted
	// id on a fresh admission, or the id of the row a replay matched.
	GuardID uuid.UUID
	// IntentID is the confirmed promotion this reservation protects. It is
	// uuid.Nil until [Confirm] has run -- including immediately after a
	// fresh Admit, and after a replay of a request whose own Confirm has not
	// happened yet (see the package doc's "two-phase reservation").
	IntentID uuid.UUID
	// Replay is true when this call matched a reservation already recorded
	// under the same idempotency key, rather than creating a new one.
	Replay bool
}

func requireTenant(tenantID uuid.UUID) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("%w: tenant id is nil", ErrInvalid)
	}
	return nil
}

func requireField(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s is empty", ErrInvalid, field)
	}
	return nil
}

// Admit is the one write that decides admission.
//
// It is never a SELECT followed by a conditional INSERT: the single
// statement below is an INSERT .. ON CONFLICT .. DO UPDATE .. RETURNING that
// is itself the decision, so the guarantee migrations/00286's partial unique
// index makes -- no two ACTIVE rows for the same (tenant, worker_ref,
// effective_date) can ever both commit -- is what actually decides between
// two genuinely concurrent callers, not this function's control flow. See
// the package doc for the full argument and TestTodo_PROMOUX_002_Race for the
// proof against real concurrent PostgreSQL sessions.
//
// guardID is the caller's own freshly minted identity for a prospective new
// reservation; it is only actually stored when this call creates one. A
// replay (idempotencyKey matches an existing ACTIVE row for this window)
// reports that row's own GuardID instead, which is how the caller tells the
// two cases apart without a second read.
func Admit(
	ctx context.Context, ex Executor,
	tenantID, guardID uuid.UUID, workerRef string, effectiveDate string, idempotencyKey string,
) (Decision, error) {
	if err := requireTenant(tenantID); err != nil {
		return Decision{}, err
	}
	if guardID == uuid.Nil {
		return Decision{}, fmt.Errorf("%w: guard id is nil", ErrInvalid)
	}
	if err := requireField("worker_ref", workerRef); err != nil {
		return Decision{}, err
	}
	if err := requireField("effective_date", effectiveDate); err != nil {
		return Decision{}, err
	}
	if err := requireField("idempotency_key", idempotencyKey); err != nil {
		return Decision{}, err
	}
	if ex == nil {
		return Decision{}, fmt.Errorf("%w: executor is nil", ErrInvalid)
	}

	var (
		rowGuardID  uuid.UUID
		rowIntentID *uuid.UUID
	)
	err := ex.QueryRow(ctx, `
		INSERT INTO promotion_active_intent_guard
			(tenant_id, guard_id, worker_ref, effective_date, idempotency_key, status, opened_at)
		VALUES ($1, $2, $3, $4::date, $5, 'ACTIVE', now())
		ON CONFLICT (tenant_id, worker_ref, effective_date) WHERE status = 'ACTIVE'
		DO UPDATE SET opened_at = promotion_active_intent_guard.opened_at
		WHERE promotion_active_intent_guard.idempotency_key = EXCLUDED.idempotency_key
		RETURNING guard_id, intent_id`,
		tenantID, guardID, strings.TrimSpace(workerRef), effectiveDate, strings.TrimSpace(idempotencyKey),
	).Scan(&rowGuardID, &rowIntentID)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			// The INSERT's own conflict path ran and its DO UPDATE's WHERE
			// clause matched nothing: a row already claims this window under
			// a different idempotency key. Nothing was written -- the
			// statement above is the only mutating statement this function
			// issues, and it affected zero rows here -- so this is reported,
			// not retried or masked.
			return Decision{}, ErrActiveConflict
		}
		return Decision{}, fmt.Errorf("promotionguard: admit: %w", err)
	}

	decision := Decision{GuardID: rowGuardID, Replay: rowGuardID != guardID}
	if rowIntentID != nil {
		decision.IntentID = *rowIntentID
	}
	return decision, nil
}

// Confirm records the real intent id a reservation protects, once
// CreateIntent has minted it. It is idempotent for the same intent and refuses
// a missing guard or a conflicting binding rather than silently changing no
// row and pretending confirmation succeeded.
func Confirm(ctx context.Context, ex Executor, tenantID, guardID uuid.UUID, idempotencyKey string, intentID uuid.UUID) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	if guardID == uuid.Nil {
		return fmt.Errorf("%w: guard id is nil", ErrInvalid)
	}
	if intentID == uuid.Nil {
		return fmt.Errorf("%w: intent id is nil", ErrInvalid)
	}
	if err := requireField("idempotency_key", idempotencyKey); err != nil {
		return err
	}
	if ex == nil {
		return fmt.Errorf("%w: executor is nil", ErrInvalid)
	}
	var confirmed uuid.UUID
	err := ex.QueryRow(ctx, `
		UPDATE promotion_active_intent_guard
		SET intent_id = $4
		WHERE tenant_id = $1 AND guard_id = $2 AND idempotency_key = $3
		  AND (intent_id IS NULL OR intent_id = $4)
		RETURNING intent_id`,
		tenantID, guardID, strings.TrimSpace(idempotencyKey), intentID,
	).Scan(&confirmed)
	if err != nil {
		return fmt.Errorf("promotionguard: confirm: %w", err)
	}
	return nil
}

// Release closes the ACTIVE reservation protecting intentID, freeing its
// (worker, effective date) window for a future promotion. When confirmation
// was lost after intent creation, it reconciles the unconfirmed reservation
// through the intent's durable idempotency key. It is idempotent:
// closing an already-CLOSED or nonexistent reservation affects zero rows
// rather than erroring.
//
// The promotion terminal writer calls Release in the same transaction as its
// ledger and outbox fact. Other callers must still arrange terminal release.
func Release(ctx context.Context, ex Executor, tenantID, intentID uuid.UUID, closedAt time.Time) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	if intentID == uuid.Nil {
		return fmt.Errorf("%w: intent id is nil", ErrInvalid)
	}
	if closedAt.IsZero() {
		return fmt.Errorf("%w: closed_at is unset", ErrInvalid)
	}
	if ex == nil {
		return fmt.Errorf("%w: executor is nil", ErrInvalid)
	}
	_, err := ex.Exec(ctx, `
		UPDATE promotion_active_intent_guard
		SET status = 'CLOSED', closed_at = $3
		WHERE tenant_id = $1 AND status = 'ACTIVE'
		  AND (intent_id = $2 OR (intent_id IS NULL AND idempotency_key = (
		      SELECT idempotency_key FROM intent_instance
		      WHERE tenant_id = $1 AND intent_id = $2)))`,
		tenantID, intentID, closedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("promotionguard: release: %w", err)
	}
	return nil
}
