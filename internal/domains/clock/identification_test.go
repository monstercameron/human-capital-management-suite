package clock

import (
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
	"time"
)

func mustSalt(t *testing.T) []byte {
	t.Helper()
	salt := make([]byte, 32)
	if _, err := cryptorand.Read(salt); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return salt
}

// TestTodo_TCLOCK_005 is the PRIMARY test: each identification method
// (PIN, badge, QR, supervisor override) authenticates the correct worker
// and records its fixed assurance level.
func TestTodo_TCLOCK_005(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	cred, err := HashPIN("worker-1", "4821", mustSalt(t))
	if err != nil {
		t.Fatalf("HashPIN: %v", err)
	}
	pinEvidence, err := IdentifyWorker(IdentificationRequest{
		Method: MethodPIN, DeviceRef: "device-1", WorkerRef: "worker-1", Now: now,
		PINCredential: cred, PINEntered: "4821",
	})
	if err != nil {
		t.Fatalf("pin identification: %v", err)
	}
	if pinEvidence.AssuranceLevel != AssuranceLow || pinEvidence.WorkerRef != "worker-1" {
		t.Fatalf("unexpected pin evidence %+v", pinEvidence)
	}

	badge := BadgeCredential{WorkerRef: "worker-2", BadgeID: "badge-abc", IssuedAt: now.Add(-time.Hour)}
	badgeEvidence, err := IdentifyWorker(IdentificationRequest{Method: MethodBadge, DeviceRef: "device-1", WorkerRef: "worker-2", Now: now, Badge: badge})
	if err != nil {
		t.Fatalf("badge identification: %v", err)
	}
	if badgeEvidence.AssuranceLevel != AssuranceMedium {
		t.Fatalf("unexpected badge assurance %+v", badgeEvidence)
	}

	qr := QRCredential{WorkerRef: "worker-3", KeyID: "qr-xyz", IssuedAt: now.Add(-time.Hour)}
	qrEvidence, err := IdentifyWorker(IdentificationRequest{Method: MethodQR, DeviceRef: "device-1", WorkerRef: "worker-3", Now: now, QR: qr})
	if err != nil {
		t.Fatalf("qr identification: %v", err)
	}
	if qrEvidence.AssuranceLevel != AssuranceMedium {
		t.Fatalf("unexpected qr assurance %+v", qrEvidence)
	}

	override := SupervisorOverride{SupervisorRef: "supervisor-1", EvidenceRef: "evidence-ref-1", Reason: "worker forgot badge", At: now}
	overrideEvidence, err := IdentifyWorker(IdentificationRequest{Method: MethodSupervisorOverride, DeviceRef: "device-1", WorkerRef: "worker-4", Now: now, Override: override})
	if err != nil {
		t.Fatalf("override identification: %v", err)
	}
	if overrideEvidence.AssuranceLevel != AssuranceHigh || overrideEvidence.SupervisorRef != "supervisor-1" {
		t.Fatalf("unexpected override evidence %+v", overrideEvidence)
	}
}

// TestTodo_TCLOCK_005_Security proves a brute-forced PIN locks out after
// the policy's ceiling, a copied/mismatched badge cannot authenticate a
// different worker, and a supervisor override without a reason is
// rejected.
func TestTodo_TCLOCK_005_Security(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	policy := RateLimitPolicy{MaxAttempts: 3, Window: time.Minute, LockoutDuration: 5 * time.Minute}
	cred, err := HashPIN("worker-1", "9999", mustSalt(t))
	if err != nil {
		t.Fatalf("HashPIN: %v", err)
	}

	window := AttemptWindow{}
	var outcome AttemptOutcome
	for i := 0; i < 3; i++ {
		at := now.Add(time.Duration(i) * time.Second)
		outcome, window, err = EvaluateAttempt(policy, window, at, false)
		if err != nil {
			t.Fatalf("EvaluateAttempt: %v", err)
		}
	}
	if outcome != AttemptLockedOut {
		t.Fatalf("expected lockout after %d failures, got %s", policy.MaxAttempts, outcome)
	}
	if _, err := IdentifyWorker(IdentificationRequest{
		Method: MethodPIN, DeviceRef: "device-1", WorkerRef: "worker-1", Now: now,
		PINCredential: cred, PINEntered: "0000", WorkerOutcome: outcome,
	}); !errors.Is(err, ErrIdentificationRejected) {
		t.Fatalf("expected locked-out identification to be rejected, got %v", err)
	}

	badge := BadgeCredential{WorkerRef: "worker-2", BadgeID: "badge-abc", IssuedAt: now.Add(-time.Hour)}
	if _, err := IdentifyWorker(IdentificationRequest{Method: MethodBadge, DeviceRef: "device-1", WorkerRef: "worker-9", Now: now, Badge: badge}); !errors.Is(err, ErrIdentificationRejected) {
		t.Fatalf("expected badge/worker mismatch to be rejected, got %v", err)
	}

	revokedBadge := BadgeCredential{WorkerRef: "worker-2", BadgeID: "badge-abc", IssuedAt: now.Add(-time.Hour), RevokedAt: now.Add(-time.Minute)}
	if _, err := IdentifyWorker(IdentificationRequest{Method: MethodBadge, DeviceRef: "device-1", WorkerRef: "worker-2", Now: now, Badge: revokedBadge}); !errors.Is(err, ErrIdentificationRejected) {
		t.Fatalf("expected revoked badge to be rejected, got %v", err)
	}

	noReason := SupervisorOverride{SupervisorRef: "supervisor-1", EvidenceRef: "evidence-1", At: now}
	if _, err := IdentifyWorker(IdentificationRequest{Method: MethodSupervisorOverride, DeviceRef: "device-1", WorkerRef: "worker-4", Now: now, Override: noReason}); !errors.Is(err, ErrIdentificationRejected) {
		t.Fatalf("expected override without reason to be rejected, got %v", err)
	}
}

// TestTodo_TCLOCK_005_Property proves, over many random pins and salts,
// that a PIN credential always verifies against the exact pin it was
// hashed from and never verifies against a different pin.
func TestTodo_TCLOCK_005_Property(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 500; i++ {
		pin := fmt.Sprintf("%04d", rng.IntN(10000))
		wrong := fmt.Sprintf("%04d", (rng.IntN(10000)+1)%10000)
		if wrong == pin {
			wrong = fmt.Sprintf("%04d", (rng.IntN(10000)+2)%10000)
		}
		salt := mustSalt(t)
		cred, err := HashPIN("worker-x", pin, salt)
		if err != nil {
			t.Fatalf("HashPIN: %v", err)
		}
		if !cred.Verify(pin) {
			t.Fatalf("case %d: correct pin %q did not verify", i, pin)
		}
		if wrong != pin && cred.Verify(wrong) {
			t.Fatalf("case %d: wrong pin %q verified against credential for %q", i, wrong, pin)
		}
	}
}

// TestTodo_TCLOCK_005_Race identifies many distinct workers concurrently
// with independent PIN, badge and QR credentials; the package holds no
// shared mutable state so no run may corrupt another's result.
func TestTodo_TCLOCK_005_Race(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	const n = 50
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			worker := fmt.Sprintf("worker-%d", i)
			cred, err := HashPIN(worker, "1234", mustSalt(t))
			if err != nil {
				errCh <- err
				return
			}
			_, err = IdentifyWorker(IdentificationRequest{Method: MethodPIN, DeviceRef: "device-race", WorkerRef: worker, Now: now, PINCredential: cred, PINEntered: "1234"})
			errCh <- err
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent identification failed: %v", err)
		}
	}
}
