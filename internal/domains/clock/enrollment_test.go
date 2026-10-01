package clock

import (
	"crypto/ed25519"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func testRegistry(t *testing.T) ProfileRegistry {
	t.Helper()
	reg, err := NewProfileRegistry(sampleProfiles())
	if err != nil {
		t.Fatalf("NewProfileRegistry: %v", err)
	}
	return reg
}

func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

func testProof(t *testing.T, pub ed25519.PublicKey, priv ed25519.PrivateKey, challenge []byte) KeyPossessionProof {
	t.Helper()
	return KeyPossessionProof{PublicKey: pub, Challenge: challenge, Signature: ed25519.Sign(priv, challenge)}
}

func baseCode(now time.Time) EnrollmentCode {
	return EnrollmentCode{
		CodeRef: "code-1", Tenant: values.TenantId("acme-co"), SiteRef: "site-riverside",
		Profile: SourceManagedKiosk, Timezone: "America/Los_Angeles",
		IssuedAt: now, ExpiresAt: now.Add(10 * time.Minute),
	}
}

// TestTodo_TCLOCK_002 is the PRIMARY test: a device enrolls with a valid
// code and key-possession proof, receiving a scoped, revisioned identity,
// and rotation, suspension, resumption and site reassignment each advance
// the revision.
func TestTodo_TCLOCK_002(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	reg := testRegistry(t)
	pub, priv := testKeyPair(t)
	challenge := []byte("server-challenge-1")

	identity, err := Enroll(EnrollmentRequest{
		Code: baseCode(now), Proof: testProof(t, pub, priv, challenge), DeviceRef: "device-1", Registry: reg, Now: now,
	})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if identity.Revision != 1 || identity.State != DeviceActive {
		t.Fatalf("unexpected identity %+v", identity)
	}
	if identity.Tenant != "acme-co" || identity.SiteRef != "site-riverside" || identity.Timezone != "America/Los_Angeles" {
		t.Fatalf("identity not correctly scoped: %+v", identity)
	}

	suspended, err := TransitionDevice(DeviceTransitionRequest{Previous: identity, Kind: TransitionSuspend, Reason: "maintenance window", Now: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if suspended.Revision != 2 || suspended.State != DeviceSuspended {
		t.Fatalf("unexpected suspended identity %+v", suspended)
	}

	resumed, err := TransitionDevice(DeviceTransitionRequest{Previous: suspended, Kind: TransitionResume, Reason: "maintenance complete", Now: now.Add(2 * time.Hour)})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed.Revision != 3 || resumed.State != DeviceActive {
		t.Fatalf("unexpected resumed identity %+v", resumed)
	}

	newPub, newPriv := testKeyPair(t)
	newChallenge := []byte("server-challenge-2")
	rotated, err := TransitionDevice(DeviceTransitionRequest{
		Previous: resumed, Kind: TransitionRotateKey, Reason: "scheduled rotation", Now: now.Add(3 * time.Hour),
		NewProof: &KeyPossessionProof{PublicKey: newPub, Challenge: newChallenge, Signature: ed25519.Sign(newPriv, newChallenge)},
	})
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated.PublicKeyFingerprint == resumed.PublicKeyFingerprint {
		t.Fatalf("fingerprint did not change after rotation")
	}

	reassigned, err := TransitionDevice(DeviceTransitionRequest{
		Previous: rotated, Kind: TransitionReassignSite, Reason: "crew moved sites", Now: now.Add(4 * time.Hour),
		NewSiteRef: "site-oakwood", NewTimezone: "America/Denver",
	})
	if err != nil {
		t.Fatalf("reassign: %v", err)
	}
	if reassigned.SiteRef != "site-oakwood" || reassigned.Timezone != "America/Denver" {
		t.Fatalf("reassignment did not update site/timezone: %+v", reassigned)
	}

	revoked, err := TransitionDevice(DeviceTransitionRequest{Previous: reassigned, Kind: TransitionRevoke, Reason: "device retired", Now: now.Add(5 * time.Hour)})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if revoked.State != DeviceRevoked {
		t.Fatalf("expected revoked state, got %+v", revoked)
	}
	if ClassifyQueuedPunch(revoked) != DispositionException {
		t.Fatalf("expected revoked device's queued punches to become exceptions")
	}
	if ClassifyQueuedPunch(reassigned) != DispositionAccepted {
		t.Fatalf("expected active device's queued punches to be accepted")
	}

	if _, err := TransitionDevice(DeviceTransitionRequest{Previous: revoked, Kind: TransitionSuspend, Reason: "too late", Now: now.Add(6 * time.Hour)}); !errors.Is(err, ErrEnrollmentRejected) {
		t.Fatalf("expected transition on revoked device to be rejected, got %v", err)
	}
}

// TestTodo_TCLOCK_002_Security proves that a reused enrollment code, an
// expired code, an unregistered profile class and an unverified
// key-possession proof are all rejected.
func TestTodo_TCLOCK_002_Security(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	reg := testRegistry(t)
	pub, priv := testKeyPair(t)
	challenge := []byte("server-challenge-1")
	proof := testProof(t, pub, priv, challenge)

	consumed := baseCode(now)
	consumed.Consumed = true
	if _, err := Enroll(EnrollmentRequest{Code: consumed, Proof: proof, DeviceRef: "device-1", Registry: reg, Now: now}); !errors.Is(err, ErrEnrollmentRejected) {
		t.Fatalf("expected consumed code to be rejected, got %v", err)
	}

	expired := baseCode(now)
	if _, err := Enroll(EnrollmentRequest{Code: expired, Proof: proof, DeviceRef: "device-1", Registry: reg, Now: now.Add(time.Hour)}); !errors.Is(err, ErrEnrollmentRejected) {
		t.Fatalf("expected expired code to be rejected, got %v", err)
	}

	unregistered := baseCode(now)
	unregistered.Profile = SourceClass("ROGUE_CLASS")
	if _, err := Enroll(EnrollmentRequest{Code: unregistered, Proof: proof, DeviceRef: "device-1", Registry: reg, Now: now}); err == nil {
		t.Fatalf("expected unregistered profile class to be rejected")
	}

	otherPub, _ := testKeyPair(t)
	forged := KeyPossessionProof{PublicKey: otherPub, Challenge: challenge, Signature: proof.Signature}
	if _, err := Enroll(EnrollmentRequest{Code: baseCode(now), Proof: forged, DeviceRef: "device-1", Registry: reg, Now: now}); !errors.Is(err, ErrEnrollmentRejected) {
		t.Fatalf("expected forged signature to be rejected, got %v", err)
	}
}

// TestTodo_TCLOCK_002_Race enrolls and transitions many devices
// concurrently. The package holds no package-level mutable state, so
// concurrent calls with independent inputs must never race or corrupt one
// another's result.
func TestTodo_TCLOCK_002_Race(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	reg := testRegistry(t)
	const n = 50
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pub, priv := testKeyPair(t)
			challenge := []byte("challenge")
			code := baseCode(now)
			code.CodeRef = "code-race"
			identity, err := Enroll(EnrollmentRequest{Code: code, Proof: testProof(t, pub, priv, challenge), DeviceRef: "device-race", Registry: reg, Now: now})
			if err != nil {
				errCh <- err
				return
			}
			if _, err := TransitionDevice(DeviceTransitionRequest{Previous: identity, Kind: TransitionSuspend, Reason: "race", Now: now.Add(time.Minute)}); err != nil {
				errCh <- err
				return
			}
			errCh <- nil
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent enroll/transition failed: %v", err)
		}
	}
}

// TestTodo_TCLOCK_002_Recovery proves that a device identity rebuilt from
// an ordered event list (ReplayDevice) equals the state built by applying
// the same transitions incrementally, one call at a time.
func TestTodo_TCLOCK_002_Recovery(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	reg := testRegistry(t)
	pub, priv := testKeyPair(t)
	challenge := []byte("challenge")
	identity, err := Enroll(EnrollmentRequest{Code: baseCode(now), Proof: testProof(t, pub, priv, challenge), DeviceRef: "device-recovery", Registry: reg, Now: now})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	transitions := []DeviceTransitionRequest{
		{Kind: TransitionSuspend, Reason: "r1", Now: now.Add(time.Hour)},
		{Kind: TransitionResume, Reason: "r2", Now: now.Add(2 * time.Hour)},
		{Kind: TransitionReassignSite, Reason: "r3", Now: now.Add(3 * time.Hour), NewSiteRef: "site-oakwood", NewTimezone: "America/Denver"},
	}

	incremental := identity
	for _, transition := range transitions {
		transition.Previous = incremental
		next, err := TransitionDevice(transition)
		if err != nil {
			t.Fatalf("incremental transition: %v", err)
		}
		incremental = next
	}

	replayed, err := ReplayDevice(identity, transitions)
	if err != nil {
		t.Fatalf("ReplayDevice: %v", err)
	}
	if replayed != incremental {
		t.Fatalf("replayed state %+v does not equal incremental state %+v", replayed, incremental)
	}
	if replayed.Digest == "" || replayed.Revision != 4 {
		t.Fatalf("unexpected replayed state %+v", replayed)
	}
}
