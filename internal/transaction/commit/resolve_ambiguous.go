// Ambiguous-commit recovery wiring for the local commit boundary.
//
// The transaction coordinator reports a lost commit acknowledgement as
// ErrCommitAmbiguous and never replays the closure. The only evidence that
// can decide the outcome is the durable store the commit boundary already
// maintains: the idempotency record, the commit/abort receipt and the ledger
// rows. AmbiguousResolver binds one plan's durable identity to the
// coordinator's ResolveAmbiguous seam so a severed connection resolves to a
// recovered COMMITTED verdict or a typed absent verdict instead of a bare
// ambiguous error.
package commit

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	transactioncoordinator "github.com/monstercameron/human-capital-management-suite/internal/transaction/coordinator"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/recovery"
)

// ErrCommitAbsent reports an ambiguous commit that durable evidence proves
// never happened: no commit receipt, no completed idempotency record and no
// ledger effect. It is deliberately independent of
// transactioncoordinator.ErrCommitAmbiguous so callers can tell a recovered
// absence (safe to attempt again under normal admission) from an unresolved
// ambiguity (never safe to replay). Through CommitWithRetry the coordinator
// still wraps every resolver failure as ambiguous, so end-to-end callers
// observe both sentinels together; direct hook callers observe this one.
var ErrCommitAbsent = errors.New("transaction commit: ambiguous commit resolved as absent")

// ambiguousCommitCapability is the idempotency capability CommitInTx guards
// under. The resolver rebuilds the exact scope the commit wrote so recovery
// reads the same durable record instead of a neighbouring one.
const ambiguousCommitCapability = "transaction.commit"

// AmbiguousResolver builds the coordinator ResolveAmbiguous hook for one
// plan. store is the durable reader recovery consults (a pool or connection,
// never the failed transaction); tenant, planID and idempotencyKey are the
// exact identity CommitInTx guarded under. A receipt for any other plan is
// refused without touching the store, so one plan's ambiguity can never
// resolve from another plan's evidence.
func AmbiguousResolver(store dbport.Querier, tenant uuid.UUID, planID, idempotencyKey string) (func(context.Context, transactioncoordinator.Receipt) (transactioncoordinator.Receipt, error), error) {
	if store == nil {
		return nil, fmt.Errorf("%w: ambiguous-commit resolution needs a durable reader", ErrInvalidPlan)
	}
	if tenant == uuid.Nil {
		return nil, fmt.Errorf("%w: ambiguous-commit resolution needs a tenant", ErrInvalidPlan)
	}
	if planID == "" || idempotencyKey == "" {
		return nil, fmt.Errorf("%w: ambiguous-commit resolution needs a plan id and an idempotency key", ErrInvalidPlan)
	}
	return func(ctx context.Context, receipt transactioncoordinator.Receipt) (transactioncoordinator.Receipt, error) {
		if receipt.PlanID != planID {
			return transactioncoordinator.Receipt{}, fmt.Errorf("%w: ambiguous resolver bound to plan %q cannot resolve receipt for plan %q", transactioncoordinator.ErrCommitAmbiguous, planID, receipt.PlanID)
		}
		result, err := recovery.Resolve(ctx, store, recovery.Request{
			Tenant: tenant,
			PlanID: planID,
			Scope: idempotency.Scope{
				Tenant:      tenant,
				Capability:  ambiguousCommitCapability,
				EffectScope: planID,
				Key:         idempotencyKey,
			},
		})
		if err != nil {
			if errors.Is(err, recovery.ErrEvidenceConflict) {
				return transactioncoordinator.Receipt{}, fmt.Errorf("%w: contradictory durable evidence for plan %s: %v", transactioncoordinator.ErrCommitAmbiguous, planID, err)
			}
			var invalid recovery.ErrInvalidRequest
			if errors.As(err, &invalid) {
				return transactioncoordinator.Receipt{}, fmt.Errorf("%w: ambiguous-commit resolution refused: %v", ErrInvalidPlan, err)
			}
			return transactioncoordinator.Receipt{}, fmt.Errorf("%w: durable resolution failed for plan %s: %v", transactioncoordinator.ErrCommitAmbiguous, planID, err)
		}
		switch result.Outcome {
		case recovery.OutcomeCommitted:
			return receipt, nil
		case recovery.OutcomeNotCommitted, recovery.OutcomeAborted:
			return transactioncoordinator.Receipt{}, fmt.Errorf("%w: plan %s (%s)", ErrCommitAbsent, planID, result.Outcome)
		default:
			return transactioncoordinator.Receipt{}, fmt.Errorf("%w: plan %s requires recovery (%s)", transactioncoordinator.ErrCommitAmbiguous, planID, result.Outcome)
		}
	}, nil
}

// WithAmbiguousRecovery returns base with ResolveAmbiguous wired to the
// durable ledger/idempotency store for one plan. Every other retry field is
// carried through unchanged, so admission, backoff and preparation keep the
// caller's policy and only the ambiguity seam gains recovery.
func WithAmbiguousRecovery(base transactioncoordinator.RetryOptions, store dbport.Querier, tenant uuid.UUID, planID, idempotencyKey string) (transactioncoordinator.RetryOptions, error) {
	hook, err := AmbiguousResolver(store, tenant, planID, idempotencyKey)
	if err != nil {
		return transactioncoordinator.RetryOptions{}, err
	}
	return transactioncoordinator.RetryOptions{
		MaxAttempts:      base.MaxAttempts,
		BaseDelay:        base.BaseDelay,
		MaxDelay:         base.MaxDelay,
		Admit:            base.Admit,
		Sleep:            base.Sleep,
		Jitter:           base.Jitter,
		OnRetry:          base.OnRetry,
		ResolveAmbiguous: hook,
		Prepare:          base.Prepare,
	}, nil
}
