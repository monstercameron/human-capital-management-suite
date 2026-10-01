package timeclockstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

// BiometricKeyCustody is the required production seam for the separately
// keyed vault. Implementations must destroy the referenced durable key; this
// interface intentionally has no erased boolean or in-memory fallback.
type BiometricKeyCustody interface {
	DestroyKeyWithHoldFence(context.Context, string, string, string, string, BiometricHoldFence) error
}

// BiometricHoldFence is issued by the same governance authority that serializes
// hold placement. Custody must reject an expired or superseded fence.
type BiometricHoldFence struct {
	Token     string
	Version   int64
	ExpiresAt time.Time
}

// BiometricHoldCoordinator obtains a lease that remains valid through the
// custody call. A boolean read cannot satisfy this contract.
type BiometricHoldCoordinator interface {
	AcquireBiometricDestructionFence(context.Context, string, string, time.Time) (BiometricHoldFence, error)
}

// BiometricKeyDestroyer is the legacy clockservice erasure shape. It is kept
// as a separate explicit seam so composition can wire a real vault adapter;
// this adapter deliberately cannot implement it without a disposition ID.
type BiometricKeyDestroyer interface {
	DestroyBiometricKey(context.Context, string, string, string) error
}

// BiometricDispositionAdapter binds the durable saga to key custody. Hold
// evaluation is injected so the caller can recheck immediately before erase.
type BiometricDispositionAdapter struct {
	Store   *timestore.Store
	Custody BiometricKeyCustody
	Holds   BiometricHoldCoordinator
	Clock   func() time.Time
}

// Withdraw revokes identification before placing a tombstone in the durable
// saga. A missing custody adapter remains visible to composition and fails
// closed when processing a keyed template.
func (a BiometricDispositionAdapter) Withdraw(ctx context.Context, tenant, consentID, workerID, actorID, reason, idempotencyKey string, expectedRevision int64) (timestore.BiometricDisposition, error) {
	if a.Store == nil {
		return timestore.BiometricDisposition{}, errors.New("timeclockstore: biometric disposition store is required")
	}
	return a.Store.WithdrawBiometricConsent(ctx, tenant, consentID, workerID, actorID, reason, idempotencyKey, expectedRevision)
}

// Destroy runs the recovery-safe saga. The hold check is repeated after the
// tombstone claim and immediately before the external key-destruction call.
func (a BiometricDispositionAdapter) Destroy(ctx context.Context, tenant, dispositionID, actorID string) error {
	if a.Store == nil || a.Custody == nil || a.Holds == nil || a.Clock == nil || strings.TrimSpace(actorID) == "" {
		return errors.New("timeclockstore: biometric custody composition is incomplete")
	}
	now := a.Clock().UTC()
	d, err := a.Store.ClaimBiometricTombstone(ctx, tenant, dispositionID, actorID, now)
	if err != nil {
		return err
	}
	if d.State == "COMPLETED" {
		return nil
	}
	fence, err := a.Holds.AcquireBiometricDestructionFence(ctx, tenant, dispositionID, a.Clock().UTC())
	if err != nil {
		return err
	}
	if fence.Token == "" || fence.ExpiresAt.IsZero() || !a.Clock().UTC().Before(fence.ExpiresAt) {
		if _, markErr := a.Store.MarkBiometricHoldBlocked(ctx, tenant, dispositionID, actorID, "hold fence unavailable or expired", a.Clock().UTC()); markErr != nil {
			return markErr
		}
		return errors.New("timeclockstore: biometric hold fence is unavailable")
	}
	if d.ClaimID == nil || *d.ClaimID == "" {
		return errors.New("timeclockstore: biometric destroy claim is missing")
	}
	if err := a.Custody.DestroyKeyWithHoldFence(ctx, tenant, d.TemplateCustodyRef, *d.ClaimID, actorID, fence); err != nil {
		return err
	}
	_, err = a.Store.MarkBiometricKeyDestroyed(ctx, tenant, dispositionID, actorID, a.Clock().UTC())
	return err
}

// Reconcile returns durable saga rows that need a retry after a crash.
func (a BiometricDispositionAdapter) Reconcile(ctx context.Context, tenant string, limit int) ([]timestore.BiometricDisposition, error) {
	if a.Store == nil {
		return nil, errors.New("timeclockstore: biometric disposition store is required")
	}
	return a.Store.ReconcileBiometricDisposition(ctx, tenant, limit)
}
