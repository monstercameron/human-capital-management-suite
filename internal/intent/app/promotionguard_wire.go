package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionguard"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

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
) (uuid.UUID, error) {
	if e.db == nil {
		return uuid.Nil, fmt.Errorf("%w: this cell was composed with no execution database", workspace.ErrJourneyUnavailable)
	}
	guardIDText, err := e.svc.ids()
	if err != nil {
		return uuid.Nil, fmt.Errorf("app: journey: mint a promotion guard reservation id: %w", err)
	}
	guardID, err := uuid.Parse(guardIDText)
	if err != nil {
		return uuid.Nil, fmt.Errorf("app: journey: promotion guard reservation id is not a uuid: %w", err)
	}
	tx, err := e.beginTenant(ctx, principal)
	if err != nil {
		return uuid.Nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	tenantID := e.svc.tenantUUID(principal.Tenant())
	decision, admitErr := promotionguard.Admit(ctx, tx, tenantID, guardID, workerRef, effectiveDate, idempotencyKey)
	if admitErr != nil {
		if errors.Is(admitErr, promotionguard.ErrActiveConflict) {
			// The rollback above is what makes this refusal free of side
			// effects: the reservation attempt itself never commits, and
			// CreateIntent is never reached, so no intent, proposal
			// revision, work item or ledger row exists for this call.
			return uuid.Nil, journeyError(envelope.New(envelope.CodeAlreadyExists, reasonPromotionActiveConflict,
				"the resource already exists").
				WithViolation("subject_worker_ref",
					"this worker already has an active promotion whose effective window overlaps the one requested; open it instead of starting a new one",
					reasonPromotionActiveConflict))
		}
		return uuid.Nil, fmt.Errorf("app: journey: admit promotion window: %w", admitErr)
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("app: journey: commit promotion window admission: %w", err)
	}
	committed = true
	return decision.GuardID, nil
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
