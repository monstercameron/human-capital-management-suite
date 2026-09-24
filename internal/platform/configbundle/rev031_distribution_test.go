package configbundle

import (
	"errors"
	"testing"
	"time"
)

func TestTodo_REV_031_01(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	scope := Scope{TenantID: "rev031-tenant", CellID: "cell-a"}
	bundle, registry := cp004PublishedBundle(t, scope)
	private, keyring := cp004Keys(t)
	placement := cp004Placement(scope, 8)

	distributor, err := NewDistributor("cp004-signer", "v1", private, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	first, err := distributor.Publish(bundle, placement, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := distributor.Publish(bundle, placement, 1); err != nil {
		t.Fatalf("same versioned publication should be idempotent: %v", err)
	}
	if _, err := distributor.Publish(bundle, placement, 0); !errors.Is(err, ErrInvalidEpoch) {
		t.Fatalf("zero epoch publication error = %v", err)
	}
	if _, err := distributor.Publish(bundle, placement, 2); err != nil {
		t.Fatalf("next epoch publication: %v", err)
	}
	if _, err := distributor.Publish(bundle, placement, 1); !errors.Is(err, ErrEpochReplay) {
		t.Fatalf("replayed publication error = %v", err)
	}

	receiver := NewReceiver(keyring, registry, ReceiverOptions{
		Scope: scope, Placement: placement,
		Freshness: FreshnessPolicy{MaxAge: time.Hour, HardMaxAge: 2 * time.Hour},
		Now:       func() time.Time { return now },
	})
	result, err := receiver.Receive(first)
	if err != nil || result.Decision != ApplyAccepted || result.Snapshot.DesiredDigest != first.Digest {
		t.Fatalf("receive publication = %+v, err=%v", result, err)
	}
	result, err = receiver.Receive(first)
	if err != nil || result.Decision != ApplyIdempotent {
		t.Fatalf("repeated receive = %+v, err=%v", result, err)
	}

	// A valid signature for the same bundle and epoch but a changed placement
	// is a different desired state and must not be treated as an idempotent ack.
	otherPlacement := placement
	otherPlacement.Region = "us-west"
	otherDistributor, err := NewDistributor("cp004-signer", "v1", private, func() time.Time { return now.Add(time.Minute) })
	if err != nil {
		t.Fatal(err)
	}
	changed, err := otherDistributor.Publish(bundle, otherPlacement, 1)
	if err != nil {
		t.Fatal(err)
	}
	result, err = receiver.Receive(changed, otherPlacement)
	if !errors.Is(err, ErrEpochConflict) || result.Decision != ApplyRefused || result.Snapshot.DesiredDigest != first.Digest {
		t.Fatalf("same-epoch changed desired state = %+v, err=%v", result, err)
	}

	// Adoption acknowledgements bind both desired and applied digests to the
	// exact epoch, and missing consumers keep the watermark pending.
	_, _, receiptPrivate, receiptPublic := cp003Keys(t)
	signer, err := NewEd25519ReceiptSigner("receipt-signer", "v1", receiptPrivate)
	if err != nil {
		t.Fatal(err)
	}
	receipts := NewReceiptStore(signer, func() time.Time { return now })
	receipt, err := receipts.Record(ApplicationReceipt{
		TenantID: scope.TenantID, CellID: scope.CellID, Service: "api", Build: "build-1",
		DesiredDigest: first.Digest, AppliedDigest: first.Digest, Epoch: first.Epoch, Validation: ValidationValid,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := receipt.Verify(receiptPublic); err != nil {
		t.Fatalf("signed application acknowledgement: %v", err)
	}
	watermark, err := receipts.Watermark(scope.TenantID, scope.CellID, first.Digest, first.Epoch, []string{"api", "worker"})
	if err != nil || watermark.Status != AdoptionPending || len(watermark.Adopted) != 1 || len(watermark.Missing) != 1 {
		t.Fatalf("partial adoption watermark = %+v, err=%v", watermark, err)
	}
	if _, err := receipts.Record(ApplicationReceipt{
		TenantID: scope.TenantID, CellID: scope.CellID, Service: "worker", Build: "build-1",
		DesiredDigest: first.Digest, AppliedDigest: first.Digest, Epoch: first.Epoch, Validation: ValidationValid,
	}); err != nil {
		t.Fatal(err)
	}
	watermark, err = receipts.Watermark(scope.TenantID, scope.CellID, first.Digest, first.Epoch, []string{"api", "worker"})
	if err != nil || watermark.Status != AdoptionComplete {
		t.Fatalf("complete adoption watermark = %+v, err=%v", watermark, err)
	}
}

func TestTodo_REV_031_01_Recovery(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	scope := Scope{TenantID: "rev031-recovery", CellID: "cell-a"}
	activator, _, bundlePrivate, _, prior, current := cp009Setup(t, scope, now)
	first := cp009Activate(t, activator, prior, scope, 1, now)
	second := cp009Activate(t, activator, current, scope, 2, now)
	beforeCount := activator.ActivationCount(scope.TenantID)

	_, err := Rollback(activator, RollbackRequest{
		Tenant: scope.TenantID, Prior: prior, PriorDigest: prior.Digest, NewEpoch: 3,
		Scope:       Scope{TenantID: "another-tenant", CellID: scope.CellID},
		Environment: "PRODUCTION", TrustProfile: "control-plane",
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		Sign: func(bundle Bundle) (SignedBundle, error) {
			return SignBundle(bundle, "bundle-signer", "v1", bundlePrivate)
		},
	})
	if err == nil || CodeOf(err) != "ROLLBACK_SCOPE_MISMATCH" {
		t.Fatalf("cross-tenant rollback error = %v (code %q)", err, CodeOf(err))
	}
	if activator.CurrentEpoch(scope.TenantID) != 2 || activator.ActivationCount(scope.TenantID) != beforeCount {
		t.Fatal("rejected cross-tenant rollback changed activation state")
	}

	receipt, err := cp009Rollback(t, activator, prior, scope, 3, now, bundlePrivate, nil)
	if err != nil || receipt.BundleDigest != first.BundleDigest || receipt.Epoch != 3 {
		t.Fatalf("revert to exact prior bundle = %+v, err=%v", receipt, err)
	}
	for epoch, expected := range map[uint64]ActivationReceipt{1: first, 2: second} {
		got, ok := activator.Receipt(scope.TenantID, epoch)
		if !ok || got != expected {
			t.Fatalf("rollback rewrote epoch %d history: got=%+v want=%+v", epoch, got, expected)
		}
	}
}
