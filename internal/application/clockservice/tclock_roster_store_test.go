package clockservice

import (
	"context"
	"crypto/ed25519"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/pressly/goose/v3"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type rosterDatabaseDevices struct {
	DeviceStore
	store *timestore.Store
}

func (d rosterDatabaseDevices) GetDevice(ctx context.Context, tenant, id string) (DeviceRecord, error) {
	row, err := d.store.GetDevice(ctx, tenant, id)
	return DeviceRecord{ID: row.ID, TenantID: row.TenantID, SiteID: row.SiteID, ProfileID: row.ProfileID, Timezone: row.Timezone, State: row.State, Revision: row.Revision, PublicKey: row.PublicKey}, err
}

func TestTodo_TCLOCK_004_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	migrations, err := fs.Sub(timestore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store, err := timestore.New(ctx, timestore.Config{DSN: db.URL, Schema: db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CreateEnrollmentCode(ctx, "ironridge", "roster-test-code", "riverside", "managed-kiosk/v1", "America/New_York", "admin", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	device, err := store.RedeemEnrollmentCode(ctx, "ironridge", "roster-test-code", "kiosk-1", pub, "admin", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s, _, source := rosterServiceFixture()
	s.Devices = rosterDatabaseDevices{store: store}
	p := rosterPrincipal(t, trust.SubjectKindService, "kiosk-1")
	if _, err = s.SyncRoster(ctx, p, "kiosk-1", ""); err != nil {
		t.Fatal(err)
	}
	if source.site != "riverside" {
		t.Fatalf("wrong site: %q", source.site)
	}
	device, err = store.ReassignDeviceSite(ctx, "ironridge", device.ID, "new-site", "UTC", "admin", "site move", device.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SyncRoster(ctx, p, "kiosk-1", ""); err != nil || source.site != "new-site" {
		t.Fatalf("moved roster site=%q err=%v", source.site, err)
	}
	if _, err = store.RevokeDevice(ctx, "ironridge", device.ID, "admin", "retired", device.Revision); err != nil {
		t.Fatal(err)
	}
	before := source.calls
	if _, err = s.SyncRoster(ctx, p, "kiosk-1", ""); !errors.Is(err, ErrDeviceNotEligible) || source.calls != before {
		t.Fatalf("revoked sync err=%v calls=%d", err, source.calls)
	}
	if _, err = store.GetDevice(ctx, "another-tenant", "kiosk-1"); !errors.Is(err, timestore.ErrNotFound) {
		t.Fatalf("cross tenant device: %v", err)
	}
}
