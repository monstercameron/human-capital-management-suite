package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionguard"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// promotionGuardOrphanGracePeriod bounds the uncertainty Reclaim must respect:
// a reservation newer than this may still have a CreateIntent in flight whose
// commit has simply not landed yet, so admission never reclaims it. Five
// minutes is orders of magnitude past any honest CreateIntent latency and
// still bounds how long a crashed page-form admission (whose retry mints a
// fresh key and would otherwise be refused as a conflict) blocks its worker
// and window.
const promotionGuardOrphanGracePeriod = 5 * time.Minute

// reasonPromotionActiveConflict is promotion.propose's admission refusal:
// this worker already has a nonterminal promotion whose effective window
// overlaps the one being proposed (PROMOUX-002). It is named once here and
// shared by [journeyEngine.Propose] and [journeyEngine.ProposePromotion]
// because it is one refusal with two callers, not two refusals that happen
// to read alike.
const reasonPromotionActiveConflict = "promotion.propose.active_conflict"

// admitPromotionWindow is PROMOUX-002's admission boundary: the one call
// both [journeyEngine.Propose] (the page's own form) and
// [journeyEngine.ProposePromotion] (the intent-only contract) make before
// either ever calls CreateIntent.
//
// It is the guard REFACTOR names outright: "the guard belongs to promotion
// admission and is projected to clients; UI suppression is not the
// integrity boundary." A conflicting caller is refused here, in a
// transaction that is rolled back and never sees CreateIntent, rather than
// by a page that simply declines to render a Start button -- a direct API
// call, a replayed request, a stale tab or a second operator all reach this
// same statement regardless of what any UI did or did not show them.
//
// The actual exclusivity guarantee is internal/data/promotionguard.Admit's:
// see its package doc and migrations/00286's partial unique index. This
// method only supplies the three facts that decision needs (the tenant, the
// worker, the effective window) and translates its one failure mode into
// the refusal the page renders.
//
// idempotencyKey must be the exact string the caller is about to pass to
// CreateIntent: a retry of the same request presents the same key both
// here and there, and CreateIntent's own idempotency handling is what makes
// that retry resolve to the one intent this reservation protects, without
// this method needing to know or branch on whether it does.
func (e *journeyEngine) admitPromotionWindow(
	ctx context.Context, principal *trust.Principal, workerRef, effectiveDate, idempotencyKey string,
) (uuid.UUID, bool, error) {
	if e.db == nil {
		return uuid.Nil, false, fmt.Errorf("%w: this cell was composed with no execution database", workspace.ErrJourneyUnavailable)
	}
	guardIDText, err := e.svc.ids()
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("app: journey: mint a promotion guard reservation id: %w", err)
	}
	guardID, err := uuid.Parse(guardIDText)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("app: journey: promotion guard reservation id is not a uuid: %w", err)
	}
	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return uuid.Nil, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	tenantID := e.svc.tenantUUID(principal.Tenant())
	// PROMOUX-017: admission reclaims this worker's crashed orphans in the
	// same transaction, before deciding. Only reservations older than the
	// orphan grace period with no committed intent behind them close, so a
	// CreateIntent still in flight is never reclaimed; a reclaim failure
	// fails admission closed rather than admitting past broken cleanup.
	if _, err := promotionguard.Reclaim(ctx, tx, tenantID, workerRef,
		time.Now().UTC().Add(-promotionGuardOrphanGracePeriod)); err != nil {
		return uuid.Nil, false, fmt.Errorf("app: journey: reclaim orphaned promotion windows: %w", err)
	}
	decision, admitErr := promotionguard.Admit(ctx, tx, tenantID, guardID, workerRef, effectiveDate, idempotencyKey)
	if admitErr != nil {
		if errors.Is(admitErr, promotionguard.ErrActiveConflict) {
			// The rollback above is what makes this refusal free of side
			// effects: the reservation attempt itself never commits, and
			// CreateIntent is never reached, so no intent, proposal
			// revision, work item or ledger row exists for this call.
			return uuid.Nil, false, journeyError(envelope.New(envelope.CodeAlreadyExists, reasonPromotionActiveConflict,
				"the resource already exists").
				WithViolation("subject_worker_ref",
					"this worker already has an active promotion whose effective window overlaps the one requested; open it instead of starting a new one",
					reasonPromotionActiveConflict))
		}
		return uuid.Nil, false, fmt.Errorf("app: journey: admit promotion window: %w", admitErr)
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, false, fmt.Errorf("app: journey: commit promotion window admission: %w", err)
	}
	committed = true
	return decision.GuardID, decision.Replay, nil
}

// abandonPromotionWindow closes a reservation its own call opened but never
// turned into an intent, after CreateIntent refused it (PROMOUX-017).
//
// It is deliberately best-effort like [journeyEngine.confirmPromotionWindow]:
// the caller's CreateIntent already failed with the error that matters, and
// cleanup must not fail on top of it. A reservation this misses -- abandon
// itself erroring, or a crash before abandon runs -- is exactly what the
// admission-time reclaim sweep closes once it ages past the orphan grace
// period, so every path ends with no orphan window.
func (e *journeyEngine) abandonPromotionWindow(
	ctx context.Context, principal *trust.Principal, guardID uuid.UUID, idempotencyKey string,
) error {
	if e.db == nil || guardID == uuid.Nil {
		return nil
	}
	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tenantID := e.svc.tenantUUID(principal.Tenant())
	if _, err := promotionguard.Abandon(ctx, tx, tenantID, guardID, idempotencyKey); err != nil {
		return fmt.Errorf("app: journey: abandon promotion window: %w", err)
	}
	return tx.Commit(ctx)
}

// createGuardedIntent is the one admit-create-confirm sequence both Propose
// paths share (PROMOUX-017): admit the window (reclaiming this worker's
// crashed orphans first), run the caller's CreateIntent, and confirm the
// minted intent -- or abandon the reservation the failed call opened.
//
// create builds the caller's own CreateIntent request and returns the minted
// intent id. Only a fresh admission is ever abandoned on failure: a replayed
// reservation predates this call, so a failed retry leaves it ACTIVE for the
// original attempt (or its own retry) to still confirm. The caller's own
// CreateIntent error travels unchanged; confirm and abandon stay best-effort
// with span events, exactly as before.
func (e *journeyEngine) createGuardedIntent(
	ctx context.Context, principal *trust.Principal, workerRef, effectiveDate, idempotencyKey string,
	create func() (string, error),
) (string, error) {
	guardID, replay, err := e.admitPromotionWindow(ctx, principal, workerRef, effectiveDate, idempotencyKey)
	if err != nil {
		return "", err
	}
	intentIDText, createErr := create()
	if createErr != nil {
		if !replay {
			if abandonErr := e.abandonPromotionWindow(ctx, principal, guardID, idempotencyKey); abandonErr != nil {
				trace.SpanFromContext(ctx).AddEvent("promotion.guard.abandon_failed")
			}
		}
		return "", journeyError(createErr)
	}
	if confirmErr := e.confirmPromotionWindow(ctx, principal, guardID, idempotencyKey, intentIDText); confirmErr != nil {
		trace.SpanFromContext(ctx).AddEvent("promotion.guard.confirm_failed")
	}
	return intentIDText, nil
}

// confirmPromotionWindow attaches the real intent id CreateIntent minted to
// the reservation [admitPromotionWindow] opened.
//
// A failure here is deliberately not surfaced as this call's own failure:
// by the time this runs, CreateIntent has already durably recorded the
// promotion the caller asked for, and telling them the request failed
// because a bookkeeping update on top of that success could not complete
// would be a lie. A lost confirmation does not invalidate admission:
// internal/data/promotionguard.Admit's idempotency handling never depends on
// this column, and CreateIntent's replay of the same key still resolves to
// the one real intent. The caller logs the error and continues. An ACTIVE
// unconfirmed reservation still protects the promotion while it runs;
// terminal Release reconciles it
// by the intent's durable idempotency key before closing the window.
func (e *journeyEngine) confirmPromotionWindow(
	ctx context.Context, principal *trust.Principal, guardID uuid.UUID, idempotencyKey, intentIDText string,
) error {
	if e.db == nil || guardID == uuid.Nil {
		return nil
	}
	intentID, err := uuid.Parse(intentIDText)
	if err != nil {
		return fmt.Errorf("app: journey: confirm promotion window: intent id %q is not a uuid: %w", intentIDText, err)
	}
	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tenantID := e.svc.tenantUUID(principal.Tenant())
	if err := promotionguard.Confirm(ctx, tx, tenantID, guardID, idempotencyKey, intentID); err != nil {
		return fmt.Errorf("app: journey: confirm promotion window: %w", err)
	}
	return tx.Commit(ctx)
}
