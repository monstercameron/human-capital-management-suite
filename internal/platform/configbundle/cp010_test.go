package configbundle

import (
	"testing"
	"time"
)

// cp010RecoveryStack provisions the fenced recovery stack: a separate
// Activator with an independent copy of the trust root, so a compromised
// production identity cannot alter what recovery adopts.
func cp010RecoveryStack(t *testing.T, scope Scope, now time.Time) *Activator {
	t.Helper()
	_, bundlePublic, receiptPrivate, _ := cp003Keys(t)
	keyring := NewKeyring()
	if err := keyring.Put(BundleKey{Handle: "bundle-signer", Version: "v1", PublicKey: bundlePublic, TrustProfile: "control-plane"}); err != nil {
		t.Fatal(err)
	}
	receiptSigner, err := NewEd25519ReceiptSigner("receipt-signer", "v1", receiptPrivate)
	if err != nil {
		t.Fatal(err)
	}
	policy := ActivationPolicy{Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane", AntiRollbackFloor: "go1.26.0"}
	return NewActivator(keyring, policy, receiptSigner, func() time.Time { return now })
}

// CP-010 RED: control-plane outage, corruption and recovery before
// outage.go exists.
func TestTodo_CP_010(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	scope := Scope{TenantID: "cp010-tenant", CellID: "cell-a"}
	activator, _, _, receiptPublic, signedA, signedB := cp009Setup(t, scope, now)
	first := cp009Activate(t, activator, signedA, scope, 1, now)
	second := cp009Activate(t, activator, signedB, scope, 2, now)
	_ = first

	cell := NewOutageCell(activator)
	cell.SetPartitioned(true)

	// GREEN: bounded last-known-good operation — reads keep serving the
	// epoch-2 receipt, digest-verified, while the plane is dark.
	served, err := cell.ServeKnownGood(scope.TenantID)
	if err != nil {
		t.Fatalf("ServeKnownGood(partitioned): %v", err)
	}
	if served.Epoch != 2 || served.BundleDigest != second.BundleDigest {
		t.Fatalf("served=%+v, want the epoch-2 receipt", served)
	}
	if err := served.Verify(receiptPublic); err != nil {
		t.Fatalf("served receipt Verify: %v", err)
	}

	// RED: a new activation during the outage fails closed instead of
	// running on an unsafe default.
	if _, err := cell.Activate(ActivationRequest{
		Bundle: signedA, Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane",
		Epoch: 3, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}); err == nil {
		t.Fatal("activation during partition succeeded, want fail-closed")
	}

	// RED: a corrupt bundle never activates and moves no epoch.
	cell.SetPartitioned(false)
	corrupt := signedB
	corrupt.Digest = "sha256:deadbeef"
	if _, err := activator.Activate(ActivationRequest{
		Bundle: corrupt, Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane",
		Epoch: 3, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}); err == nil {
		t.Fatal("corrupt bundle activated")
	}
	if activator.CurrentEpoch(scope.TenantID) != 2 {
		t.Fatal("corrupt activation moved the epoch: split-brain risk")
	}

	// GREEN: the fenced recovery stack replays the known-good lineage on
	// an independent trust root and converges digest-for-digest, with a
	// signed adoption receipt per epoch.
	recovery := cp010RecoveryStack(t, scope, now)
	adopt1, err := cell.AdoptRecovery(recovery, ActivationRequest{
		Bundle: signedA, Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane",
		Epoch: 1, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("AdoptRecovery(1): %v", err)
	}
	adopt2, err := cell.AdoptRecovery(recovery, ActivationRequest{
		Bundle: signedB, Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane",
		Epoch: 2, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("AdoptRecovery(2): %v", err)
	}
	if adopt2.Receipt.Epoch != 2 || adopt2.Receipt.BundleDigest != second.BundleDigest || adopt2.ConvergedEpochs != 2 {
		t.Fatalf("adopted=%+v, want epoch-2 convergence", adopt2)
	}
	if err := adopt2.Receipt.Verify(receiptPublic); err != nil {
		t.Fatalf("adoption receipt Verify: %v", err)
	}
	// Divergent lineage is not adoption: a different digest at a
	// converged epoch refuses instead of forking history.
	if _, err := cell.AdoptRecovery(recovery, ActivationRequest{
		Bundle: signedA, Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane",
		Epoch: 2, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}); err == nil {
		t.Fatal("divergent epoch-2 adoption succeeded, want refusal")
	}
	_ = adopt1
}
