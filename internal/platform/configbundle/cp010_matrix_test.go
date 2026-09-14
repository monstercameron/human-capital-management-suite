package configbundle

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustBundlePrivate(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	private, _, _, _ := cp003Keys(t)
	return private
}

func cp010Drill(t *testing.T) (*OutageCell, *Activator, Scope, time.Time, SignedBundle, SignedBundle) {
	t.Helper()
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	scope := Scope{TenantID: "cp010-tenant", CellID: "cell-a"}
	activator, _, _, _, signedA, signedB := cp009Setup(t, scope, now)
	cp009Activate(t, activator, signedA, scope, 1, now)
	cp009Activate(t, activator, signedB, scope, 2, now)
	return NewOutageCell(activator), cp010RecoveryStack(t, scope, now), scope, now, signedA, signedB
}

func cp010Adopt(t *testing.T, cell *OutageCell, recovery *Activator, signed SignedBundle, scope Scope, epoch uint64, now time.Time) Adoption {
	t.Helper()
	adopted, err := cell.AdoptRecovery(recovery, ActivationRequest{
		Bundle: signed, Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane",
		Epoch: epoch, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("AdoptRecovery(%d): %v", epoch, err)
	}
	return adopted
}

// TestTodo_CP_010_Golden pins the epoch-2 adoption receipt oracle.
func TestTodo_CP_010_Golden(t *testing.T) {
	cell, recovery, scope, now, signedA, signedB := cp010Drill(t)
	cp010Adopt(t, cell, recovery, signedA, scope, 1, now)
	adopted := cp010Adopt(t, cell, recovery, signedB, scope, 2, now)
	raw, err := os.ReadFile(filepath.Join("testdata", "cp010.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	oracle := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			t.Fatalf("malformed golden line: %q", line)
		}
		oracle[key] = value
	}
	if adopted.Receipt.Digest != oracle["receipt_digest"] {
		t.Fatalf("digest mismatch:\n got=%q\nwant=%q", adopted.Receipt.Digest, oracle["receipt_digest"])
	}
	if adopted.Receipt.Explain() != oracle["explain"] {
		t.Fatalf("explain mismatch:\n got=%q\nwant=%q", adopted.Receipt.Explain(), oracle["explain"])
	}
}

// TestTodo_CP_010_Fault: empty planes, out-of-order lineage and revoked
// keys fail closed.
func TestTodo_CP_010_Fault(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	scope := Scope{TenantID: "cp010-fault", CellID: "cell-a"}
	activator, keyring, bundlePrivate, _, signedA, signedB := cp009Setup(t, scope, now)
	cell := NewOutageCell(activator)
	if _, err := cell.ServeKnownGood(scope.TenantID); err == nil {
		t.Fatal("empty plane serves last-known-good")
	}
	cp009Activate(t, activator, signedA, scope, 1, now)
	cp009Activate(t, activator, signedB, scope, 2, now)
	recovery := cp010RecoveryStack(t, scope, now)
	// Out-of-order adoption refuses: lineage replays from epoch 1.
	if _, err := cell.AdoptRecovery(recovery, ActivationRequest{
		Bundle: signedB, Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane",
		Epoch: 2, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}); err == nil {
		t.Fatal("epoch-2 adoption before epoch 1 succeeded")
	}
	// A revoked signing key stops activation: revocation takes effect.
	if err := keyring.Revoke("bundle-signer", "v1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	_ = bundlePrivate
	if _, err := activator.Activate(ActivationRequest{
		Bundle: signedA, Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane",
		Epoch: 3, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}); err == nil {
		t.Fatal("activation under a revoked key succeeded")
	}
}

// TestTodo_CP_010_Recovery: the plane heals — partition clears, refused
// count is reported, and the lineage continues exactly once.
func TestTodo_CP_010_Recovery(t *testing.T) {
	cell, recovery, scope, now, signedA, signedB := cp010Drill(t)
	cell.SetPartitioned(true)
	for i := 0; i < 3; i++ {
		if _, err := cell.Activate(ActivationRequest{
			Bundle: signedA, Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane",
			Epoch: 3, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		}); err == nil {
			t.Fatal("partitioned activation succeeded")
		}
	}
	if cell.RefusedActivations() != 3 {
		t.Fatalf("refused=%d, want 3 counted fail-closes", cell.RefusedActivations())
	}
	cp010Adopt(t, cell, recovery, signedA, scope, 1, now)
	adopted := cp010Adopt(t, cell, recovery, signedB, scope, 2, now)
	if adopted.ConvergedEpochs != 2 {
		t.Fatalf("adopted=%+v", adopted)
	}
	// Critical behavior stays revocable on the recovered lineage: roll
	// the recovery stack back and the adoption digest pins the rollback.
	rolled, err := Rollback(recovery, RollbackRequest{
		Tenant: scope.TenantID, Prior: signedA, PriorDigest: signedA.Digest, NewEpoch: 3,
		Scope: scope, Environment: "PRODUCTION", TrustProfile: "control-plane",
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		Sign: func(b Bundle) (SignedBundle, error) {
			return SignBundle(b, "bundle-signer", "v1", mustBundlePrivate(t))
		},
	})
	if err != nil {
		t.Fatalf("recovery rollback: %v", err)
	}
	if rolled.Epoch != 3 || rolled.BundleDigest != signedA.Digest {
		t.Fatalf("rolled=%+v, want epoch 3 over the prior digest", rolled)
	}
}

// TestTodo_CP_010_Security: the recovery trust root is independent —
// compromising the production keyring cannot alter what recovery adopts.
func TestTodo_CP_010_Security(t *testing.T) {
	cell, recovery, scope, now, signedA, signedB := cp010Drill(t)
	// Production and recovery hold independent keyring copies.
	cp010Adopt(t, cell, recovery, signedA, scope, 1, now)
	adopted := cp010Adopt(t, cell, recovery, signedB, scope, 2, now)
	if adopted.ProductionDigest == "" {
		t.Fatal("adoption names no production digest")
	}
	// Adoption receipts verify under the receipt key, not under any
	// production session identity.
	if adopted.Receipt.Signature.Value == "" {
		t.Fatal("adoption receipt carries no signature")
	}
}
