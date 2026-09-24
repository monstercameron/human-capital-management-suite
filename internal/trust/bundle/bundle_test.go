package bundle

import (
	"crypto/x509"
	"errors"
	"testing"
	"time"
)

var (
	t2020 = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t2035 = time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)

	v1ActivatesAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	v1ExpiresAt   = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	v2ActivatesAt = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC) // overlaps v1
	v2ExpiresAt   = time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
)

// fixture bundles a full two-generation rotation: chain A (rootA,
// intermediateA, leafA) is what bundle v1 pins; chain B is what bundle v2
// pins after rotation.
type fixture struct {
	rootA, intA testCA
	rootB, intB testCA
	leafA       *x509.Certificate
	leafB       *x509.Certificate
	v1, v2      *Bundle
}

func buildFixture(t *testing.T) fixture {
	t.Helper()
	rootA := genRoot(t, "root-a", t2020, t2035)
	intA := genIntermediate(t, rootA, "intermediate-a", t2020, t2035)
	_, leafA := genLeaf(t, intA, "leaf-a.example.com", v1ActivatesAt, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))

	rootB := genRoot(t, "root-b", t2020, t2035)
	intB := genIntermediate(t, rootB, "intermediate-b", t2020, t2035)
	_, leafB := genLeaf(t, intB, "leaf-b.example.com", v2ActivatesAt, v2ExpiresAt)

	v1, err := New(1, "mtls", []PinnedCert{mustPin(t, rootA)}, []PinnedCert{mustPin(t, intA)}, v1ActivatesAt, v1ExpiresAt)
	if err != nil {
		t.Fatalf("New v1: %v", err)
	}
	v2, err := Rotate(v1, []PinnedCert{mustPin(t, rootB)}, []PinnedCert{mustPin(t, intB)}, v2ActivatesAt, v2ExpiresAt)
	if err != nil {
		t.Fatalf("Rotate to v2: %v", err)
	}

	return fixture{rootA: rootA, intA: intA, rootB: rootB, intB: intB, leafA: leafA, leafB: leafB, v1: v1, v2: v2}
}

// TestTodo_TRUST_023 is the primary acceptance test: bundle versions with
// pinned digests, activation windows, rotation with overlap and dual
// validation, revocation, and expiry all have the transitions the RED/GREEN
// criteria require.
func TestTodo_TRUST_023(t *testing.T) {
	fx := buildFixture(t)

	// Within v1's window, chain A verifies.
	v := NewVerifier(fx.v1)
	if _, err := v.Verify(fx.leafA, nil, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}); err != nil {
		t.Fatalf("chain A must verify within v1's active window: %v", err)
	}

	// Before v1 activates, nothing is active.
	if _, err := v.Verify(fx.leafA, nil, time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC), nil); !errors.Is(err, ErrNoActiveBundle) {
		t.Fatalf("before activation: err = %v, want ErrNoActiveBundle", err)
	}

	// After v1 expires (and with only v1 known), nothing is active.
	if _, err := v.Verify(fx.leafA, nil, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), nil); !errors.Is(err, ErrNoActiveBundle) {
		t.Fatalf("after v1 expiry with only v1 known: err = %v, want ErrNoActiveBundle", err)
	}

	// Dual validation during the rotation overlap: both v1 (chain A) and v2
	// (chain B) verify against the same two-bundle verifier.
	dual := NewVerifier(fx.v1, fx.v2)
	overlapInstant := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	if _, err := dual.Verify(fx.leafA, nil, overlapInstant, nil); err != nil {
		t.Fatalf("chain A must still verify during overlap: %v", err)
	}
	if _, err := dual.Verify(fx.leafB, nil, overlapInstant, nil); err != nil {
		t.Fatalf("chain B must verify during overlap: %v", err)
	}

	// Before v2 activates, chain B is not yet trusted even though v1 is
	// still active (dual validation does not mean premature trust).
	beforeV2 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if _, err := dual.Verify(fx.leafB, nil, beforeV2, nil); err == nil {
		t.Fatalf("chain B must not verify before v2 activates")
	}

	// After v1 expires, chain A is no longer trusted even though v2 is
	// active, because v2 does not pin root A.
	afterV1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if _, err := dual.Verify(fx.leafA, nil, afterV1, nil); !errors.Is(err, ErrChainInvalid) {
		t.Fatalf("chain A after v1 expiry: err = %v, want ErrChainInvalid", err)
	}

	// Revocation: revoking root A blocks chain A even while v1 is still
	// otherwise active, and evidence is inspectable via Revocation().
	if err := fx.v1.Revoke("compromise suspected", time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if fx.v1.StatusAt(time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)) != StatusRevoked {
		t.Fatalf("revoked bundle must report StatusRevoked even inside its window")
	}
	if _, reason, revoked := fx.v1.Revocation(); !revoked || reason != "compromise suspected" {
		t.Fatalf("revocation not recorded correctly")
	}
	if _, err := v.Verify(fx.leafA, nil, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), nil); !errors.Is(err, ErrNoActiveBundle) {
		t.Fatalf("revoked bundle must never be used: err = %v, want ErrNoActiveBundle", err)
	}
}

// TestTodo_TRUST_023_Security asserts the rollback scenario: an issuer
// revoked via the independent [RevocationList] stays refused even when a
// caller presents an older, still-structurally-valid bundle version that
// still pins that issuer -- rollback cannot reintroduce a revoked root.
func TestTodo_TRUST_023_Security(t *testing.T) {
	fx := buildFixture(t)
	list := NewRevocationList()
	rootADigest := mustPin(t, fx.rootA).Digest
	list.Revoke(rootADigest, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))

	// Simulate an operator "rolling back" to v1 (still structurally valid,
	// not itself revoked as a bundle) after the compromise was discovered.
	rollback := NewVerifier(fx.v1).WithRevocationList(list)
	if _, err := rollback.Verify(fx.leafA, nil, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), nil); !errors.Is(err, ErrChainRevoked) {
		t.Fatalf("rollback to a bundle pinning a revoked issuer: err = %v, want ErrChainRevoked", err)
	}

	// A verifier refuses a bundle whose pin has been tampered with after
	// construction, even though the bundle's own activation window is
	// otherwise valid -- defense in depth beyond New's own validation.
	tampered := *fx.v1
	tampered.Roots = append([]PinnedCert(nil), fx.v1.Roots...)
	tampered.Roots[0].Digest = Digest("0000000000000000000000000000000000000000000000000000000000000000")
	tv := NewVerifier(&tampered)
	if _, err := tv.Verify(fx.leafA, nil, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), nil); !errors.Is(err, ErrNoActiveBundle) {
		t.Fatalf("verifier must refuse a bundle with a tampered pin: err = %v, want ErrNoActiveBundle", err)
	}

	// Constructing a bundle whose pin digest does not match its own
	// certificate is refused outright.
	badPin := mustPin(t, fx.rootA)
	badPin.Digest = "deadbeef"
	if _, err := New(9, "mtls", []PinnedCert{badPin}, nil, v1ActivatesAt, v1ExpiresAt); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("mismatched pin digest: err = %v, want ErrDigestMismatch", err)
	}

	// A non-CA certificate cannot be pinned as a root or intermediate.
	_, leafOnly := genLeaf(t, fx.intA, "not-a-ca.example.com", t2020, t2035)
	leafPin, err := NewPinnedCert(leafOnly.Raw)
	if err != nil {
		t.Fatalf("pin leaf: %v", err)
	}
	if _, err := New(9, "mtls", []PinnedCert{leafPin}, nil, v1ActivatesAt, v1ExpiresAt); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("non-CA pinned as root: err = %v, want ErrInvalidBundle", err)
	}
}

// TestTodo_TRUST_023_Mutation flips the activation-window boundaries to
// confirm StatusAt uses the intended inclusive/exclusive comparisons.
func TestTodo_TRUST_023_Mutation(t *testing.T) {
	fx := buildFixture(t)

	if fx.v1.StatusAt(v1ActivatesAt) != StatusActive {
		t.Fatalf("bundle must be active exactly at its activation instant")
	}
	if fx.v1.StatusAt(v1ActivatesAt.Add(-time.Nanosecond)) != StatusDraft {
		t.Fatalf("bundle must be draft one tick before activation")
	}
	if fx.v1.StatusAt(v1ExpiresAt) != StatusExpired {
		t.Fatalf("bundle must be expired exactly at its expiry instant")
	}
	if fx.v1.StatusAt(v1ExpiresAt.Add(-time.Nanosecond)) != StatusActive {
		t.Fatalf("bundle must still be active one tick before expiry")
	}

	// Rotation overlap boundary: overlapStart exactly at old.ExpiresAt is
	// not an overlap (no instant where both are active).
	if _, err := Rotate(fx.v1, fx.v2.Roots, fx.v2.Intermediates, fx.v1.ExpiresAt, v2ExpiresAt); !errors.Is(err, ErrRotationNoOverlap) {
		t.Fatalf("overlapStart at old expiry: err = %v, want ErrRotationNoOverlap", err)
	}
	if _, err := Rotate(fx.v1, fx.v2.Roots, fx.v2.Intermediates, fx.v1.ExpiresAt.Add(-time.Nanosecond), v2ExpiresAt); err != nil {
		t.Fatalf("overlapStart one tick before old expiry must be a valid rotation: %v", err)
	}
}

// TestTodo_TRUST_023_Fault covers malformed bundle construction and
// verification against a leaf with no matching issuer at all.
func TestTodo_TRUST_023_Fault(t *testing.T) {
	fx := buildFixture(t)

	if _, err := New(0, "mtls", []PinnedCert{mustPin(t, fx.rootA)}, nil, v1ActivatesAt, v1ExpiresAt); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("version 0: err = %v, want ErrInvalidBundle", err)
	}
	if _, err := New(1, "  ", []PinnedCert{mustPin(t, fx.rootA)}, nil, v1ActivatesAt, v1ExpiresAt); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("blank purpose: err = %v, want ErrInvalidBundle", err)
	}
	if _, err := New(1, "mtls", nil, nil, v1ActivatesAt, v1ExpiresAt); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("no roots: err = %v, want ErrInvalidBundle", err)
	}
	if _, err := New(1, "mtls", []PinnedCert{mustPin(t, fx.rootA)}, nil, v1ExpiresAt, v1ActivatesAt); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("activation after expiry: err = %v, want ErrInvalidBundle", err)
	}

	v := NewVerifier(fx.v1)
	if _, err := v.Verify(nil, nil, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), nil); err == nil {
		t.Fatalf("nil leaf must be refused")
	}

	// A leaf from an entirely unrelated CA never verifies, even against an
	// active bundle.
	otherRoot := genRoot(t, "other-root", t2020, t2035)
	_, otherLeaf := genLeaf(t, otherRoot, "other.example.com", v1ActivatesAt, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if _, err := v.Verify(otherLeaf, nil, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), nil); !errors.Is(err, ErrChainInvalid) {
		t.Fatalf("unrelated leaf: err = %v, want ErrChainInvalid", err)
	}
}

// FuzzTodo_TRUST_023 checks that Verify never returns a chain when no
// bundle is active at the fuzzed instant, and never returns a chain for an
// unrelated leaf certificate.
func FuzzTodo_TRUST_023(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(200 * 24 * time.Hour))
	f.Add(int64(-200 * 24 * time.Hour))
	f.Fuzz(func(t *testing.T, offsetNanos int64) {
		fx := buildFixture(t)
		v := NewVerifier(fx.v1, fx.v2)
		now := v1ActivatesAt.Add(time.Duration(offsetNanos % int64(3*365*24*time.Hour)))

		chain, err := v.Verify(fx.leafA, nil, now, nil)
		v1Active := fx.v1.StatusAt(now) == StatusActive
		v2Active := fx.v2.StatusAt(now) == StatusActive

		if !v1Active && !v2Active && err == nil {
			t.Fatalf("verified a chain at %s with no active bundle", now)
		}
		if err == nil && len(chain) == 0 {
			t.Fatalf("nil error but empty chain")
		}
		// Chain A can only ever succeed through v1 (v2 does not pin root
		// A), so a successful verification implies v1 was active.
		if err == nil && !v1Active {
			t.Fatalf("chain A verified at %s without v1 being active", now)
		}
	})
}

func TestBundle_PublicAPIs_ConstructionPoolsRotationAndRevocation(t *testing.T) {
	fx := buildFixture(t)
	if DigestOf(fx.rootA.cert.Raw) == DigestOf(fx.intA.cert.Raw) {
		t.Fatal("different certificates received the same pin digest")
	}
	if _, err := NewPinnedCert([]byte("not-a-certificate")); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("malformed certificate err=%v", err)
	}
	leafPin, err := NewPinnedCert(fx.leafA.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(1, "mtls", []PinnedCert{mustPin(t, fx.rootA)}, []PinnedCert{leafPin}, v1ActivatesAt, v1ExpiresAt); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("leaf intermediate err=%v", err)
	}
	if _, err := New(1, "mtls", []PinnedCert{{}}, nil, v1ActivatesAt, v1ExpiresAt); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("nil pinned root err=%v", err)
	}

	roots := []PinnedCert{mustPin(t, fx.rootA)}
	intermediates := []PinnedCert{mustPin(t, fx.intA)}
	copyBundle, err := New(7, "mtls", roots, intermediates, v1ActivatesAt, v1ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	roots[0] = PinnedCert{}
	intermediates[0] = PinnedCert{}
	//lint:ignore SA1019 deliberate Subjects use: these pools are built with AddCert (never SystemCertPool), where Subjects correctly enumerates the pins. owner=security-trust expires=2027-03-24
	if len(copyBundle.RootPool().Subjects()) != 1 || len(copyBundle.IntermediatePool().Subjects()) != 1 {
		//lint:ignore SA1019 deliberate Subjects use: same enumeration as above, for the failure message. owner=security-trust expires=2027-03-24
		t.Fatalf("pools did not retain copied pins: roots=%d intermediates=%d", len(copyBundle.RootPool().Subjects()), len(copyBundle.IntermediatePool().Subjects()))
	}
	if got := copyBundle.StatusAt(v1ActivatesAt.Add(-time.Nanosecond)); got != StatusDraft {
		t.Fatalf("draft status=%q", got)
	}
	if at, reason, revoked := copyBundle.Revocation(); revoked || !at.IsZero() || reason != "" {
		t.Fatalf("unrevoked state at=%v reason=%q revoked=%v", at, reason, revoked)
	}
	if err := copyBundle.Revoke("key compromise", v1ActivatesAt); err != nil {
		t.Fatal(err)
	}
	if err := copyBundle.Revoke("again", v1ActivatesAt); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("second revoke err=%v", err)
	}
	if at, reason, revoked := copyBundle.Revocation(); !revoked || !at.Equal(v1ActivatesAt) || reason != "key compromise" {
		t.Fatalf("revocation=%v/%q/%v", at, reason, revoked)
	}

	if _, err := Rotate(nil, roots, intermediates, v2ActivatesAt, v2ExpiresAt); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("nil rotation source err=%v", err)
	}
	if _, err := Rotate(fx.v1, roots, intermediates, v2ActivatesAt, v2ActivatesAt); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("invalid rotated window err=%v", err)
	}
	list := NewRevocationList()
	digest := mustPin(t, fx.rootA).Digest
	at := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	list.Revoke(digest, at.Add(time.Hour))
	list.Revoke(digest, at)
	if list.IsRevoked(digest, at.Add(-time.Nanosecond)) || !list.IsRevoked(digest, at) || !list.IsRevoked(digest, at.Add(time.Hour)) {
		t.Fatal("revocation list did not keep the earliest revocation instant")
	}
	if list.IsRevoked(Digest("other"), at) {
		t.Fatal("unlisted digest was reported revoked")
	}
	v := NewVerifier(fx.v1)
	listVerifier := v.WithRevocationList(list)
	if listVerifier != v || v.revocations != list {
		t.Fatal("WithRevocationList did not attach and return receiver")
	}
}
