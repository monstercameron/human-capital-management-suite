package timestore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestTodo_TCLOCK_005(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-tclock005"

	c, err := s.IssuePINCredential(ctx, tenant, "worker-1", "1234", "admin-1")
	if err != nil || c.Kind != CredentialPIN || c.State != CredentialActive || c.Revision != 1 {
		t.Fatalf("issue pin: %v %+v", err, c)
	}
	ok, err := s.VerifyPIN(ctx, tenant, "worker-1", "1234")
	if err != nil || !ok {
		t.Fatalf("verify correct pin: %v %v", err, ok)
	}
	ok, err = s.VerifyPIN(ctx, tenant, "worker-1", "9999")
	if err != nil || ok {
		t.Fatalf("verify wrong pin: %v %v", err, ok)
	}

	badge, err := s.IssueBadgeCredential(ctx, tenant, "worker-2", "BADGE-42", "admin-1")
	if err != nil || badge.ExternalID != "BADGE-42" || badge.Kind != CredentialBadge {
		t.Fatalf("issue badge: %v %+v", err, badge)
	}
	qr, err := s.IssueQRCredential(ctx, tenant, "worker-3", "QRKEY-9", "admin-1")
	if err != nil || qr.ExternalID != "QRKEY-9" || qr.Kind != CredentialQR {
		t.Fatalf("issue qr: %v %+v", err, qr)
	}

	// Reissue rotates in place rather than creating a second row.
	rotated, err := s.IssuePINCredential(ctx, tenant, "worker-1", "5678", "admin-1")
	if err != nil || rotated.ID != c.ID || rotated.Revision != 2 {
		t.Fatalf("rotate pin: %v %+v", err, rotated)
	}
	if ok, _ := s.VerifyPIN(ctx, tenant, "worker-1", "1234"); ok {
		t.Fatal("old pin still verifies after rotation")
	}
	if ok, _ := s.VerifyPIN(ctx, tenant, "worker-1", "5678"); !ok {
		t.Fatal("new pin does not verify after rotation")
	}

	revoked, err := s.RevokeCredential(ctx, tenant, "worker-1", CredentialPIN, "admin-1", rotated.Revision)
	if err != nil || revoked.State != CredentialRevoked || revoked.Revision != 3 {
		t.Fatalf("revoke: %v %+v", err, revoked)
	}
	if ok, _ := s.VerifyPIN(ctx, tenant, "worker-1", "5678"); ok {
		t.Fatal("revoked pin still verifies")
	}
}

func TestTodo_TCLOCK_005_Security(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-security"

	c, err := s.IssuePINCredential(ctx, tenant, "worker-1", "4242", "admin-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if string(c.VerifierHash) == "4242" || len(c.VerifierSalt) == 0 {
		t.Fatalf("pin stored unsalted or in the clear: %+v", c)
	}
	// Failed attempts lock out after the threshold.
	lockUntil := time.Now().Add(15 * time.Minute)
	var count int
	for i := 0; i < 5; i++ {
		count, err = s.RecordFailedDeviceAttempt(ctx, tenant, "device-1", 3, lockUntil)
		if err != nil {
			t.Fatalf("record failed attempt %d: %v", i, err)
		}
	}
	if count != 5 {
		t.Fatalf("failed count = %d, want 5", count)
	}
	lock, err := s.DeviceLockout(ctx, tenant, "device-1")
	if err != nil || lock.LockedUntil == nil {
		t.Fatalf("device lockout: %v %+v", err, lock)
	}
	if err := s.ResetDeviceAttempts(ctx, tenant, "device-1"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	lock, err = s.DeviceLockout(ctx, tenant, "device-1")
	if err != nil || lock.LockedUntil != nil || lock.FailedCount != 0 {
		t.Fatalf("lockout after reset: %v %+v", err, lock)
	}

	// A supervisor override is refused without a reason.
	if _, err := s.RecordSupervisorOverride(ctx, tenant, "device-1", "worker-1", "sup-cred-1", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("override without reason: %v", err)
	}
	ov, err := s.RecordSupervisorOverride(ctx, tenant, "device-1", "worker-1", "sup-cred-1", "worker forgot badge")
	if err != nil || ov.SupervisorCredentialRef != "sup-cred-1" || ov.Reason == "" {
		t.Fatalf("override: %v %+v", err, ov)
	}

	// Cross-tenant isolation: tenant B never sees tenant A's credential or lockout.
	if _, err := s.RevokeCredential(ctx, "tenant-b", "worker-1", CredentialPIN, "admin-1", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant revoke: %v", err)
	}
	lockB, err := s.DeviceLockout(ctx, "tenant-b", "device-1")
	if err != nil || lockB.FailedCount != 0 {
		t.Fatalf("cross-tenant lockout leaked: %v %+v", err, lockB)
	}
}

func TestTodo_TCLOCK_005_Property(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-property"
	pins := []string{"0000", "1111", "9999", "4242", "0001"}
	for i, pin := range pins {
		workerID := "worker-" + pin
		if _, err := s.IssuePINCredential(ctx, tenant, workerID, pin, "admin-1"); err != nil {
			t.Fatalf("issue %d: %v", i, err)
		}
		for j, other := range pins {
			ok, err := s.VerifyPIN(ctx, tenant, workerID, other)
			if err != nil {
				t.Fatalf("verify %d/%d: %v", i, j, err)
			}
			want := i == j
			if ok != want {
				t.Fatalf("worker %s verify %q = %v, want %v", workerID, other, ok, want)
			}
		}
	}
}

// TestTodo_TCLOCK_005_Race hammers one device's failed-attempt counter from
// many goroutines and asserts the final count is exactly the number of
// attempts: no increment is lost to a lost update.
func TestTodo_TCLOCK_005_Race(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-race-attempts"
	const attempts = 20
	var wg sync.WaitGroup
	lockUntil := time.Now().Add(time.Hour)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.RecordFailedDeviceAttempt(ctx, tenant, "device-race", 1000, lockUntil); err != nil {
				t.Errorf("record failed attempt: %v", err)
			}
		}()
	}
	wg.Wait()
	lock, err := s.DeviceLockout(ctx, tenant, "device-race")
	if err != nil {
		t.Fatalf("lockout: %v", err)
	}
	if lock.FailedCount != attempts {
		t.Fatalf("failed count = %d, want %d", lock.FailedCount, attempts)
	}
}
