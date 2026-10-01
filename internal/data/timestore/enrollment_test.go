package timestore

import (
	"context"
	"errors"
	"io/fs"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

func TestTodo_TCLOCK_002(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-tclock002"
	if err := s.CreateEnrollmentCode(ctx, tenant, "ABC123", "site-1", "profile-1", "America/New_York", "admin-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create code: %v", err)
	}
	d, err := s.RedeemEnrollmentCode(ctx, tenant, "ABC123", "device-1", []byte("pubkey-1"), "admin-1", time.Now())
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if d.SiteID != "site-1" || d.ProfileID != "profile-1" || d.Timezone != "America/New_York" || d.State != DeviceStateActive || d.Revision != 1 {
		t.Fatalf("device = %+v", d)
	}
	// Reused code is refused.
	if _, err := s.RedeemEnrollmentCode(ctx, tenant, "ABC123", "device-2", []byte("pubkey-2"), "admin-1", time.Now()); !errors.Is(err, ErrEnrollmentCodeInvalid) {
		t.Fatalf("reuse: %v", err)
	}
}

func TestTodo_TCLOCK_002_Security(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if err := s.CreateEnrollmentCode(ctx, "tenant-a", "SECRET1", "site-1", "profile-1", "UTC", "admin-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create code: %v", err)
	}
	// The raw code never appears in the stored row.
	rec, err := s.GetEnrollmentCode(ctx, "tenant-a", "SECRET1")
	if err != nil {
		t.Fatalf("get code: %v", err)
	}
	if rec.CodeHash == "SECRET1" || rec.CodeHash != HashEnrollmentCode("SECRET1") {
		t.Fatalf("code hash = %q", rec.CodeHash)
	}
	if len(rec.CodeHash) != 64 { // sha256 hex
		t.Fatalf("code hash length = %d", len(rec.CodeHash))
	}
	// A code created under one tenant does not resolve under another.
	if _, err := s.GetEnrollmentCode(ctx, "tenant-b", "SECRET1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant get: %v", err)
	}
	if _, err := s.RedeemEnrollmentCode(ctx, "tenant-b", "SECRET1", "device-x", []byte("k"), "admin-1", time.Now()); !errors.Is(err, ErrEnrollmentCodeInvalid) {
		t.Fatalf("cross-tenant redeem: %v", err)
	}
	// Tenant A can still redeem its own code.
	if _, err := s.RedeemEnrollmentCode(ctx, "tenant-a", "SECRET1", "device-y", []byte("k"), "admin-1", time.Now()); err != nil {
		t.Fatalf("same-tenant redeem: %v", err)
	}
}

func TestTodo_TCLOCK_002_Integration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-integration"
	if err := s.CreateEnrollmentCode(ctx, tenant, "CODE1", "site-1", "profile-1", "UTC", "admin-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create code: %v", err)
	}
	d, err := s.RedeemEnrollmentCode(ctx, tenant, "CODE1", "device-1", []byte("k1"), "admin-1", time.Now())
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}

	d, err = s.RotateDeviceKey(ctx, tenant, d.ID, []byte("k2"), "admin-1", "scheduled rotation", d.Revision)
	if err != nil || d.Revision != 2 {
		t.Fatalf("rotate: %v %+v", err, d)
	}
	d, err = s.SuspendDevice(ctx, tenant, d.ID, "admin-1", "maintenance", d.Revision)
	if err != nil || d.State != DeviceStateSuspended || d.Revision != 3 {
		t.Fatalf("suspend: %v %+v", err, d)
	}
	d, err = s.ResumeDevice(ctx, tenant, d.ID, "admin-1", "maintenance done", d.Revision)
	if err != nil || d.State != DeviceStateActive || d.Revision != 4 {
		t.Fatalf("resume: %v %+v", err, d)
	}
	d, err = s.ReassignDeviceSite(ctx, tenant, d.ID, "site-2", "Europe/Berlin", "admin-1", "moved", d.Revision)
	if err != nil || d.SiteID != "site-2" || d.Timezone != "Europe/Berlin" || d.Revision != 5 {
		t.Fatalf("reassign: %v %+v", err, d)
	}
	d, err = s.RevokeDevice(ctx, tenant, d.ID, "admin-1", "decommissioned", d.Revision)
	if err != nil || d.State != DeviceStateRevoked || d.Revision != 6 {
		t.Fatalf("revoke: %v %+v", err, d)
	}

	hist, err := s.DeviceHistory(ctx, tenant, d.ID, 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	wantKinds := []string{DeviceEnrolled, DeviceKeyRotated, DeviceSuspended, DeviceResumed, DeviceSiteReassigned, DeviceRevoked}
	if len(hist) != len(wantKinds) {
		t.Fatalf("history len = %d, want %d: %+v", len(hist), len(wantKinds), hist)
	}
	for i, want := range wantKinds {
		if hist[i].Kind != want || hist[i].Revision != int64(i+1) {
			t.Fatalf("history[%d] = %+v, want kind %s revision %d", i, hist[i], want, i+1)
		}
	}

	listed, err := s.DevicesBySite(ctx, tenant, "site-2", 10)
	if err != nil || len(listed) != 1 || listed[0].ID != d.ID {
		t.Fatalf("devices by site: %v %+v", err, listed)
	}

	// A stale revision is refused.
	if _, err := s.SuspendDevice(ctx, tenant, d.ID, "admin-1", "again", 1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision: %v", err)
	}
}

// TestTodo_TCLOCK_002_Race redeems the same enrollment code from two
// goroutines concurrently and asserts exactly one device is created.
func TestTodo_TCLOCK_002_Race(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-race"
	if err := s.CreateEnrollmentCode(ctx, tenant, "RACE1", "site-1", "profile-1", "UTC", "admin-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create code: %v", err)
	}
	const attempts = 8
	var wg sync.WaitGroup
	results := make([]error, attempts)
	devices := make([]Device, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d, err := s.RedeemEnrollmentCode(ctx, tenant, "RACE1", deviceIDFor(i), []byte("pk"), "admin-1", time.Now())
			results[i], devices[i] = err, d
		}(i)
	}
	wg.Wait()

	wins, losses := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrEnrollmentCodeInvalid):
			losses++
		default:
			t.Fatalf("unexpected redeem error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("wins = %d, want exactly 1 (losses=%d)", wins, losses)
	}
	if wins+losses != attempts {
		t.Fatalf("wins+losses = %d, want %d", wins+losses, attempts)
	}

	rec, err := s.GetEnrollmentCode(ctx, tenant, "RACE1")
	if err != nil {
		t.Fatalf("get code: %v", err)
	}
	if rec.UsedAt == nil || rec.UsedByDeviceID == "" {
		t.Fatalf("code not marked used: %+v", rec)
	}
	// Exactly one device exists at the site.
	listed, err := s.DevicesBySite(ctx, tenant, "site-1", attempts+1)
	if err != nil {
		t.Fatalf("devices by site: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("devices created = %d, want 1: %+v", len(listed), listed)
	}
}

func deviceIDFor(i int) string {
	return "race-device-" + string(rune('a'+i))
}

// TestTodo_TCLOCK_002_Recovery closes and reopens a store on the same
// database and asserts a revoked device's state survives.
func TestTodo_TCLOCK_002_Recovery(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Up(ctx); err != nil {
		t.Fatal(err)
	}

	s, err := New(ctx, Config{DSN: db.URL, Schema: db.Schema})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	tenant := "tenant-recovery"
	if err := s.CreateEnrollmentCode(ctx, tenant, "REC1", "site-1", "profile-1", "UTC", "admin-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create code: %v", err)
	}
	d, err := s.RedeemEnrollmentCode(ctx, tenant, "REC1", "device-1", []byte("k"), "admin-1", time.Now())
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if _, err := s.RevokeDevice(ctx, tenant, d.ID, "admin-1", "compromised", d.Revision); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	s.Close()

	// Reopen a fresh Store handle against the same schema, simulating a
	// process restart, and confirm the revocation persisted.
	reopened, err := New(ctx, Config{DSN: db.URL, Schema: db.Schema})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	got, err := reopened.GetDevice(ctx, tenant, d.ID)
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if got.State != DeviceStateRevoked {
		t.Fatalf("state after reopen = %q, want REVOKED", got.State)
	}
}
