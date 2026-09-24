package integrationregistry_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/human-capital-management-suite/internal/data/integrationregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func registryFixture(t *testing.T, suffix string) (*integrationregistry.Store, *pgtest.DB, uuid.UUID) {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenant, "registry-"+suffix, suffix)
	conn := appConn(t, db)
	return integrationregistry.New(conn), db, tenant
}

func fixturePublication(t *testing.T, version connectivity.Version) connectivity.Publication {
	t.Helper()
	d := fakeincumbent.DefaultDefinition()
	d.Version = version
	p, err := connectivity.NewRegistry().Publish(d, connectivity.PublicationMeta{
		PublishedBy: "user:registry-owner",
		PublishedAt: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("publish fixture: %v", err)
	}
	return p
}

// TestTodo_REV_030_01_Integration proves definitions survive a fresh store and
// registry reconstruction with every INTG-001 field, version and publication
// envelope intact, while tenant and organization scopes remain distinct.
func TestTodo_REV_030_01_Integration(t *testing.T) {
	store, db, tenant := registryFixture(t, "integration")
	ctx := context.Background()
	orgA, orgB := "org:tenant-a:people", "org:tenant-b:engineering"
	versions := []connectivity.Version{{Major: 1, Minor: 2, Patch: 3}, {Major: 4294967295, Minor: 4000000000, Patch: 3900000000}}
	for _, v := range versions {
		if err := store.Publish(ctx, integrationregistry.Scope{TenantID: tenant}, fixturePublication(t, v)); err != nil {
			t.Fatalf("publish tenant-wide %s: %v", v, err)
		}
	}
	orgPub := fixturePublication(t, connectivity.Version{Major: 2, Minor: 1, Patch: 9})
	if err := store.Publish(ctx, integrationregistry.Scope{TenantID: tenant, OrganizationScopeID: orgA}, orgPub); err != nil {
		t.Fatalf("publish org-specific: %v", err)
	}
	// A new adapter simulates process restart; the runtime registry is rebuilt
	// solely from committed rows.
	restarted, err := integrationregistry.New(appConn(t, db)).LoadRegistry(ctx, tenant, orgA)
	if err != nil {
		t.Fatalf("load after restart: %v", err)
	}
	wantVersions := append(append([]connectivity.Version(nil), versions...), orgPub.Definition.Version)
	for _, version := range wantVersions {
		want := fixturePublication(t, version)
		got, err := restarted.Resolve(want.Definition.ConnectorID, version)
		if err != nil {
			t.Fatalf("resolve %s: %v", version, err)
		}
		if !reflect.DeepEqual(got.Definition, want.Definition) || got.Digest != want.Digest || got.PublishedBy != want.PublishedBy || !got.PublishedAt.Equal(want.PublishedAt) {
			t.Fatalf("round trip %s lost definition contract or publication envelope", version)
		}
	}
	orgBRegistry, err := store.LoadRegistry(ctx, tenant, orgB)
	if err != nil {
		t.Fatalf("load other organization: %v", err)
	}
	if _, err := orgBRegistry.Resolve(orgPub.Definition.ConnectorID, orgPub.Definition.Version); !errors.Is(err, connectivity.ErrNotFound) {
		t.Fatalf("organization A publication visible to B: %v", err)
	}
}

// storeDBConn returns another role-bound connection against the same schema,
// proving reconstruction is not backed by the original Store's state.
// TestTodo_REV_030_01_Security proves app-role RLS denies foreign-tenant reads
// and the publication table refuses direct mutation even with tenant scope.
func TestTodo_REV_030_01_Security(t *testing.T) {
	store, db, tenant := registryFixture(t, "security")
	pub := fixturePublication(t, connectivity.Version{Major: 1, Minor: 0, Patch: 0})
	if err := store.Publish(context.Background(), integrationregistry.Scope{TenantID: tenant}, pub); err != nil {
		t.Fatalf("publish: %v", err)
	}
	foreign, err := store.LoadRegistry(context.Background(), uuid.New(), "")
	if err != nil {
		t.Fatalf("foreign tenant load: %v", err)
	}
	if len(foreign.ConnectorIDs()) != 0 {
		t.Fatalf("foreign tenant saw connector ids %v", foreign.ConnectorIDs())
	}
	conn := appConn(t, db)
	if err := tenancy.WithTenant(context.Background(), conn, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(context.Background(), `UPDATE published_connector_definition SET published_by='attacker'
		WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("app role mutated append-only publication")
	}
}

// TestTodo_REV_030_01_Recovery proves an empty durable catalogue stays empty
// and an altered same-version publication cannot replace committed history.
func TestTodo_REV_030_01_Recovery(t *testing.T) {
	store, db, tenant := registryFixture(t, "recovery")
	ctx := context.Background()
	empty, err := store.LoadRegistry(ctx, tenant, "")
	if err != nil {
		t.Fatalf("load empty registry: %v", err)
	}
	if len(empty.ConnectorIDs()) != 0 {
		t.Fatalf("empty registry fabricated incumbent defaults: %v", empty.ConnectorIDs())
	}
	pub := fixturePublication(t, connectivity.Version{Major: 1, Minor: 0, Patch: 0})
	scope := integrationregistry.Scope{TenantID: tenant}
	if err := store.Publish(ctx, scope, pub); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := store.Publish(ctx, scope, pub); err != nil {
		t.Fatalf("idempotent publish: %v", err)
	}
	changed := pub.Definition
	changed.Product += " altered"
	changedPub, err := connectivity.NewRegistry().Publish(changed, connectivity.PublicationMeta{PublishedBy: pub.PublishedBy, PublishedAt: pub.PublishedAt})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(ctx, scope, changedPub); !errors.Is(err, connectivity.ErrImmutable) {
		t.Fatalf("changed same-version publication = %v, want ErrImmutable", err)
	}
	loaded, err := integrationregistry.New(appConn(t, db)).LoadRegistry(ctx, tenant, "")
	if err != nil {
		t.Fatalf("recover registry: %v", err)
	}
	got, err := loaded.Resolve(pub.Definition.ConnectorID, pub.Definition.Version)
	if err != nil || got.Digest != pub.Digest {
		t.Fatalf("recovered publication = %+v, %v; want original digest", got, err)
	}
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}
