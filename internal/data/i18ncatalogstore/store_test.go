package i18ncatalogstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/i18n"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func catalogDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 350); err != nil {
		t.Fatalf("apply migrations through 00350: %v", err)
	}
	return db
}

func catalogTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', $4)`, id, key, key, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	return id
}

func catalogStore(t *testing.T, db *pgtest.DB, tenants map[string]uuid.UUID) *Store {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set app role: %v", err)
	}
	return New(conn, func(key string) uuid.UUID { return tenants[key] })
}

func catalogRevision(id, previous, text string, from, until time.Time) i18n.CatalogRevision {
	revision := i18n.CatalogRevision{
		ID: id, Locale: "en-US", PreviousRevision: previous, Version: "1.0.0",
		CreatedAt: from.Add(-time.Hour), EffectiveFrom: from, EffectiveUntil: until,
		Translations: []i18n.Translation{{
			Key: "home.title", Text: text, MeaningID: "home-title", Source: "Home", Classification: "PUBLIC",
			EffectiveFrom: from,
		}},
	}
	revision.CanonicalDigest = i18n.DigestRevision(revision)
	revision.Digest = revision.CanonicalDigest
	return revision
}

func TestTodo_I18N_004_Integration(t *testing.T) {
	db := catalogDB(t)
	tenantID := catalogTenant(t, db, "tenant-catalog-a")
	store := catalogStore(t, db, map[string]uuid.UUID{"tenant-catalog-a": tenantID})
	ctx := context.Background()
	scope := i18n.Scope{Tenant: "tenant-catalog-a", Product: "employee-portal"}
	from := time.Now().UTC().Add(-24 * time.Hour)
	first := catalogRevision("rev-1", "", "Welcome", from, time.Time{})
	second := catalogRevision("rev-2", first.ID, "Welcome back", from.Add(26*time.Hour), time.Time{})

	if _, err := store.Active(ctx, scope, first.Locale, from); !errors.Is(err, i18n.ErrNoActiveRevision) {
		t.Fatalf("Active before publication = %v, want no active revision", err)
	}
	if err := store.Publish(ctx, scope, first); err != nil {
		t.Fatalf("publish first revision: %v", err)
	}
	if err := store.Activate(ctx, scope, first.ID, "reviewer-1"); err != nil {
		t.Fatalf("activate first revision: %v", err)
	}
	var firstActivatedAt time.Time
	if err := db.SQL.QueryRowContext(ctx, `SELECT activated_at FROM activated_translation_catalog_event WHERE tenant_id = $1 AND product_id = $2 AND revision_id = $3`, tenantID, scope.Product, first.ID).Scan(&firstActivatedAt); err != nil {
		t.Fatalf("read first activation time: %v", err)
	}
	if err := store.Publish(ctx, scope, second); err != nil {
		t.Fatalf("publish second revision: %v", err)
	}
	if err := store.Activate(ctx, scope, second.ID, "reviewer-1"); err != nil {
		t.Fatalf("activate second revision: %v", err)
	}
	priorUntilEffective, err := store.Active(ctx, scope, first.Locale, time.Now().UTC().Add(time.Hour))
	if err != nil || priorUntilEffective.ID != first.ID {
		t.Fatalf("future-effective revision displaced the still-effective catalog: %#v, %v", priorUntilEffective, err)
	}

	// A fresh store and connection recover the last activation after restart.
	recovered := catalogStore(t, db, map[string]uuid.UUID{"tenant-catalog-a": tenantID})
	wantAt := time.Now().UTC().Add(3 * time.Hour)
	active, err := recovered.Active(ctx, scope, second.Locale, wantAt)
	if err != nil {
		t.Fatalf("recover active catalog: %v", err)
	}
	if active.ID != second.ID || active.Translations[0].Text != "Welcome back" {
		t.Fatalf("recovered catalog = %#v, want revision %q with updated copy", active, second.ID)
	}

	// The activation ledger also answers historical reads as of the supplied
	// instant, even after a later revision has been activated.
	historical, err := recovered.Active(ctx, scope, first.Locale, firstActivatedAt)
	if err != nil {
		t.Fatalf("load historical active catalog: %v", err)
	}
	if historical.ID != first.ID {
		t.Fatalf("historical revision = %q, want %q", historical.ID, first.ID)
	}
	if _, err := recovered.Active(ctx, scope, first.Locale, firstActivatedAt.Add(-time.Nanosecond)); !errors.Is(err, i18n.ErrNoActiveRevision) {
		t.Fatalf("Active before activation = %v, want no active revision", err)
	}
}

func TestTodo_I18N_004_Security(t *testing.T) {
	db := catalogDB(t)
	owner := catalogTenant(t, db, "tenant-catalog-owner")
	other := catalogTenant(t, db, "tenant-catalog-other")
	store := catalogStore(t, db, map[string]uuid.UUID{"tenant-catalog-owner": owner, "tenant-catalog-other": other})
	ctx := context.Background()
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	revision := catalogRevision("private-rev", "", "Owner only", from, time.Time{})
	ownerScope := i18n.Scope{Tenant: "tenant-catalog-owner", Product: "employee-portal"}
	if err := store.Publish(ctx, ownerScope, revision); err != nil {
		t.Fatalf("publish owner revision: %v", err)
	}
	if err := store.Activate(ctx, ownerScope, revision.ID, "reviewer-owner"); err != nil {
		t.Fatalf("activate owner revision: %v", err)
	}
	otherScope := i18n.Scope{Tenant: "tenant-catalog-other", Product: "employee-portal"}
	if _, err := store.Active(ctx, otherScope, revision.Locale, from.Add(time.Hour)); !errors.Is(err, i18n.ErrNoActiveRevision) {
		t.Fatalf("other tenant Active = %v, want no active revision", err)
	}

	// The application role cannot read tenant A rows while scoped to tenant B,
	// even if the product and revision identities are known.
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set app role: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('app.tenant_id', $1, false)`, other.String()); err != nil {
		t.Fatalf("set other tenant: %v", err)
	}
	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM activated_translation_catalog_revision WHERE tenant_id = $1`, owner).Scan(&count); err != nil {
		t.Fatalf("query cross-tenant rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("cross-tenant revision count = %d, want 0", count)
	}
}

func TestTodo_I18N_004_Recovery(t *testing.T) {
	db := catalogDB(t)
	tenantID := catalogTenant(t, db, "tenant-catalog-recovery")
	store := catalogStore(t, db, map[string]uuid.UUID{"tenant-catalog-recovery": tenantID})
	ctx := context.Background()
	scope := i18n.Scope{Tenant: "tenant-catalog-recovery", Product: "employee-portal"}
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	first := catalogRevision("immutable-rev", "", "Reviewed wording", from, time.Time{})
	if err := store.Publish(ctx, scope, first); err != nil {
		t.Fatalf("publish revision: %v", err)
	}
	changed := catalogRevision("immutable-rev", "", "Changed wording", from, time.Time{})
	if err := store.Publish(ctx, scope, changed); err == nil || !strings.Contains(err.Error(), "immutable revision conflict") {
		t.Fatalf("publish changed immutable revision = %v, want conflict", err)
	}
	if err := store.Activate(ctx, scope, "unknown-rev", "reviewer-1"); err == nil {
		t.Fatal("activate unknown revision succeeded")
	}
	if err := store.Activate(ctx, scope, first.ID, " "); err == nil {
		t.Fatal("activate without actor succeeded")
	}
	if err := store.Activate(ctx, scope, first.ID, "reviewer-1"); err != nil {
		t.Fatalf("activate original revision: %v", err)
	}
	if err := store.Activate(ctx, scope, first.ID, "reviewer-1"); err == nil || !strings.Contains(err.Error(), "previous revision mismatch") {
		t.Fatalf("replay activation = %v, want previous revision conflict", err)
	}
	stillActive, err := store.Active(ctx, scope, first.Locale, time.Now().UTC().Add(time.Hour))
	if err != nil || stillActive.ID != first.ID {
		t.Fatalf("active catalog after failed activation = %#v, %v; want prior revision %q", stillActive, err, first.ID)
	}

	// An invalid digest must be rejected before any durable row is written.
	invalid := catalogRevision("bad-digest", "", "Unreviewed", from, time.Time{})
	invalid.CanonicalDigest = strings.Repeat("0", 64)
	invalid.Digest = invalid.CanonicalDigest
	if err := store.Publish(ctx, scope, invalid); err == nil {
		t.Fatal("publish invalid digest succeeded")
	}
}
