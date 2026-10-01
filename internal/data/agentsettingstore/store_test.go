package agentsettingstore

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type env struct {
	db  *pgtest.DB
	ids map[string]uuid.UUID
}

func (e *env) mapper(key values.TenantId) uuid.UUID { return e.ids[string(key)] }

func newEnv(t *testing.T, tenants ...string) *env {
	t.Helper()
	e := &env{db: pgtest.New(t), ids: map[string]uuid.UUID{}}
	for _, tenant := range tenants {
		id := uuid.New()
		e.ids[tenant] = id
		e.db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, tenant, tenant)
	}
	return e
}

type rawConn interface {
	dbport.Beginner
	Exec(context.Context, string, ...any) (int64, error)
	QueryRow(context.Context, string, ...any) dbport.Row
}

// appConn is a new connection on the application role, so RLS is enforced.
func (e *env) appConn(t *testing.T) rawConn {
	t.Helper()
	conn := e.db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume app role: %v", err)
	}
	return conn
}

func TestTodo_UXBLIND_122_SettingStore(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "acme", "globex")
	store, err := New(e.appConn(t), e.mapper)
	if err != nil {
		t.Fatal(err)
	}
	enabled, err := store.AgentsEnabled(ctx, "acme")
	if err != nil || enabled {
		t.Fatalf("a tenant with no stored setting must have agents off: enabled=%v err=%v", enabled, err)
	}
	if err := store.SetAgentsEnabled(ctx, "acme", true, "hc-050-rafael-torres"); err != nil {
		t.Fatalf("turn on: %v", err)
	}
	if enabled, err = store.AgentsEnabled(ctx, "acme"); err != nil || !enabled {
		t.Fatalf("stored setting not read back: enabled=%v err=%v", enabled, err)
	}
	if enabled, err = store.AgentsEnabled(ctx, "globex"); err != nil || enabled {
		t.Fatalf("one tenant's setting leaked into another: enabled=%v err=%v", enabled, err)
	}
	if err := store.SetAgentsEnabled(ctx, "acme", false, "hc-050-rafael-torres"); err != nil {
		t.Fatalf("turn off: %v", err)
	}
	if enabled, err = store.AgentsEnabled(ctx, "acme"); err != nil || enabled {
		t.Fatalf("turning agents off did not persist: enabled=%v err=%v", enabled, err)
	}
	var revision int64
	var actor string
	if err := e.db.QueryRow(ctx, `SELECT revision, updated_by FROM tenant_agent_setting WHERE tenant_id = $1`, e.ids["acme"]).Scan(&revision, &actor); err != nil {
		t.Fatal(err)
	}
	if revision != 2 || actor != "hc-050-rafael-torres" {
		t.Fatalf("revision/actor = %d/%q, want 2/hc-050-rafael-torres", revision, actor)
	}
}

func TestTodo_UXBLIND_122_SettingStore_Security(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, "acme", "globex")
	store, err := New(e.appConn(t), e.mapper)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(nil, e.mapper); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil database = %v", err)
	}
	if err := store.SetAgentsEnabled(ctx, "acme", true, "  "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an anonymous change was accepted: %v", err)
	}
	if _, err := store.AgentsEnabled(ctx, "unknown"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an unknown tenant was read: %v", err)
	}
	var nilStore *Store
	if _, err := nilStore.AgentsEnabled(ctx, "acme"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil store = %v", err)
	}
	if err := store.SetAgentsEnabled(ctx, "globex", true, "admin"); err != nil {
		t.Fatal(err)
	}
	// Row-level security: a transaction bound to acme cannot see or change
	// globex's row, and the row cannot be deleted.
	conn := e.appConn(t)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, e.ids["acme"]); err != nil {
		t.Fatal(err)
	}
	if affected, err := tx.Exec(ctx, `UPDATE tenant_agent_setting SET enabled = false, revision = revision + 1 WHERE tenant_id = $1`, e.ids["globex"]); err != nil || affected != 0 {
		t.Fatalf("cross-tenant update affected %d rows (err %v)", affected, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err = conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, e.ids["globex"]); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM tenant_agent_setting WHERE tenant_id = $1`, e.ids["globex"]); err == nil {
		t.Fatal("the tenant agents setting was deleted")
	}
}
