package timestore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTodo_TCLOCK_006(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-tclock006"
	destruction := time.Now().Add(48 * time.Hour)

	c, err := s.RecordBiometricConsent(ctx, tenant, "worker-1", "notice-v1", "BIPA", "IL", "custody-ref-1", "admin-1", destruction)
	if err != nil || c.State != ConsentGranted || c.Revision != 1 || c.TemplateCustodyRef != "custody-ref-1" {
		t.Fatalf("record consent: %v %+v", err, c)
	}

	revoked, err := s.RevokeBiometricConsent(ctx, tenant, c.ID, "worker withdrew consent", "admin-1", c.Revision)
	if err != nil || revoked.State != ConsentRevoked || revoked.Revision != 2 {
		t.Fatalf("revoke: %v %+v", err, revoked)
	}

	destroyed, err := s.DestroyBiometricConsent(ctx, tenant, c.ID, "retention window elapsed", "admin-1", revoked.Revision)
	if err != nil || destroyed.State != ConsentDestroyed || destroyed.Revision != 3 || destroyed.DestroyedAt == nil {
		t.Fatalf("destroy: %v %+v", err, destroyed)
	}
}

func TestTodo_TCLOCK_006_Golden(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-golden"
	c, err := s.RecordBiometricConsent(ctx, tenant, "worker-1", "notice-v3", "TEXAS_CUBI", "TX", "custody-ref-9", "admin-1", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("record consent: %v", err)
	}
	// The written record echoes exactly the notice version, legal basis
	// and custody reference it was given, byte for byte, and nothing else
	// leaks a template.
	got, err := s.GetBiometricConsent(ctx, tenant, c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.NoticeVersion != "notice-v3" || got.LegalBasis != "TEXAS_CUBI" || got.Jurisdiction != "TX" || got.TemplateCustodyRef != "custody-ref-9" {
		t.Fatalf("consent = %+v", got)
	}
}

func TestTodo_TCLOCK_006_Security(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-a"
	c, err := s.RecordBiometricConsent(ctx, tenant, "worker-1", "notice-v1", "GDPR_ART9", "DE", "custody-ref-1", "admin-1", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	// Cross-tenant isolation: tenant B cannot read or transition tenant A's consent.
	if _, err := s.GetBiometricConsent(ctx, "tenant-b", c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant get: %v", err)
	}
	if _, err := s.RevokeBiometricConsent(ctx, "tenant-b", c.ID, "x", "admin-1", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant revoke: %v", err)
	}
	// Missing required policy facts are rejected outright.
	if _, err := s.RecordBiometricConsent(ctx, tenant, "worker-2", "", "GDPR_ART9", "DE", "custody-ref-2", "admin-1", time.Now().Add(time.Hour)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing notice version: %v", err)
	}
}

func TestTodo_TCLOCK_006_Property(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-property-bio"
	bases := []string{"BIPA", "TEXAS_CUBI", "WASHINGTON_HB1493", "GDPR_ART9"}
	for i, basis := range bases {
		c, err := s.RecordBiometricConsent(ctx, tenant, "worker-"+basis, "notice-v1", basis, "US", "ref-"+basis, "admin-1", time.Now().Add(time.Duration(i+1)*time.Hour))
		if err != nil {
			t.Fatalf("record %s: %v", basis, err)
		}
		if c.LegalBasis != basis {
			t.Fatalf("legal basis = %q, want %q", c.LegalBasis, basis)
		}
		// Every recorded consent starts GRANTED at revision 1 regardless
		// of which legal basis or jurisdiction it names.
		if c.State != ConsentGranted || c.Revision != 1 {
			t.Fatalf("consent %+v", c)
		}
	}
}

// TestTodo_TCLOCK_006_Recovery proves the destruction-schedule query finds
// exactly the consents whose schedule has passed, and none that have not.
func TestTodo_TCLOCK_006_Recovery(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-destruction"
	now := time.Now()

	overdue1, err := s.RecordBiometricConsent(ctx, tenant, "worker-1", "notice-v1", "BIPA", "IL", "ref-1", "admin-1", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("record overdue1: %v", err)
	}
	overdue2, err := s.RecordBiometricConsent(ctx, tenant, "worker-2", "notice-v1", "BIPA", "IL", "ref-2", "admin-1", now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("record overdue2: %v", err)
	}
	future, err := s.RecordBiometricConsent(ctx, tenant, "worker-3", "notice-v1", "BIPA", "IL", "ref-3", "admin-1", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("record future: %v", err)
	}
	alreadyDestroyed, err := s.RecordBiometricConsent(ctx, tenant, "worker-4", "notice-v1", "BIPA", "IL", "ref-4", "admin-1", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("record already-destroyed: %v", err)
	}
	if _, err := s.DestroyBiometricConsent(ctx, tenant, alreadyDestroyed.ID, "swept already", "admin-1", alreadyDestroyed.Revision); err != nil {
		t.Fatalf("pre-destroy: %v", err)
	}

	due, err := s.DueForDestruction(ctx, tenant, now, 100)
	if err != nil {
		t.Fatalf("due for destruction: %v", err)
	}
	got := map[string]bool{}
	for _, c := range due {
		got[c.ID] = true
	}
	if !got[overdue1.ID] || !got[overdue2.ID] {
		t.Fatalf("missing overdue consents: %+v", due)
	}
	if got[future.ID] {
		t.Fatalf("future consent returned as due: %+v", due)
	}
	if got[alreadyDestroyed.ID] {
		t.Fatalf("already-destroyed consent returned as due: %+v", due)
	}
}
