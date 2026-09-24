package idempotency

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// EffectFunc performs the guarded effect inside the caller's own
// transaction, and reports the [ResultIdentity] to record alongside it. It
// must produce no observable side effect outside tx: everything [Guard]
// promises -- exactly-once, reserved atomically with the effect's own commit
// -- depends on the effect having no side channel of its own.
type EffectFunc func(ctx context.Context, tx dbport.Tx) (ResultIdentity, error)

// Guard is TX-006's whole idempotency contract in one call.
//
// It reserves scope under digest and policy (refusing outright, before fn
// ever runs, when policy cannot be honored or digest is malformed); when
// this call wins that reservation it runs fn inside tx and stores the
// identity fn returns, atomically with the caller's own commit of tx; when
// scope was already reserved under the same digest and has already
// completed, it returns the stored identity without running fn at all; when
// scope was already reserved under a different digest, it refuses with a
// [CodeConflict] [*Error] and mutates nothing.
//
// fn's error is returned unchanged and nothing is completed -- the
// reservation this call itself just inserted is still only a statement of
// tx, so when the caller rolls tx back (as it must on any error Guard or fn
// returns) the reservation disappears with it, and a later retry with the
// same scope and digest is free to try again from a clean reservation.
//
// now is the caller's own instant; this package never reads a wall clock.
func Guard(
	ctx context.Context,
	tx dbport.Tx,
	store Store,
	scope Scope,
	digest string,
	policy RetentionPolicy,
	now time.Time,
	fn EffectFunc,
) (Record, error) {
	if store == nil {
		return Record{}, refuse(CodeInvalidRecord, scope, "guard requires a store")
	}

	reserved, created, err := store.Reserve(ctx, tx, scope, digest, policy, now)
	if err != nil {
		return Record{}, err
	}

	if !created {
		if reserved.RequestDigest != digest {
			return Record{}, &Error{
				Code:           CodeConflict,
				Scope:          scope,
				RecordedDigest: reserved.RequestDigest,
				RequestDigest:  digest,
				Detail:         "idempotency key is already bound to a different request",
			}
		}
		if reserved.Status == StatusTombstone {
			return Record{}, refuse(CodeTombstoned, scope,
				"idempotency key is permanently bound to this request, but its result has expired; use a new key or perform an explicit duplication-risk check")
		}
		if reserved.Status == StatusCompleted {
			return reserved, nil
		}
		if reserved.Status == StatusCompensated {
			return reserved, nil
		}
		return Record{}, refuse(CodeInProgress, scope,
			"an in-flight attempt already reserved this key under the same request; it has not completed yet")
	}

	identity, err := fn(ctx, tx)
	if err != nil {
		return Record{}, err
	}

	completed, err := store.Complete(ctx, tx, scope, identity, now)
	if err != nil {
		return Record{}, err
	}
	return completed, nil
}
