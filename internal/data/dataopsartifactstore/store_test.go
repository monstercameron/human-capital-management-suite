package dataopsartifactstore

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func artifactFixture(t *testing.T) (*Store, *pgtest.DB, uuid.UUID, values.TenantId, values.TenantId) {
	t.Helper()
	db := pgtest.New(t)
	first, second := uuid.New(), uuid.New()
	for _, entry := range []struct {
		id  uuid.UUID
		key string
	}{{first, "artifact-one"}, {second, "artifact-two"}} {
		db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, entry.id, entry.key, entry.key)
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	mapper := func(tenant values.TenantId) uuid.UUID {
		switch tenant {
		case "artifact-one":
			return first
		case "artifact-two":
			return second
		default:
			return uuid.Nil
		}
	}
	return New(conn, mapper), db, first, "artifact-one", "artifact-two"
}

func TestTodo_REV_030_01_Integration(t *testing.T) {
	store, _, _, tenant, foreign := artifactFixture(t)
	ctx := context.Background()
	body := []byte(`{"diff":"exact-inputs"}`)
	if err := store.Put(ctx, tenant, "diff", "sha256:diff-1", body); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Put(ctx, tenant, "diff", "sha256:diff-1", body); err != nil {
		t.Fatalf("idempotent Put: %v", err)
	}
	got, err := store.Get(ctx, tenant, "diff", "sha256:diff-1")
	if err != nil || string(got) != string(body) {
		t.Fatalf("Get = %q, %v; want original payload", got, err)
	}
	if _, err := store.Get(ctx, foreign, "diff", "sha256:diff-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign Get = %v, want ErrNotFound", err)
	}
	if err := store.Put(ctx, tenant, "repair_plan", "sha256:diff-1", body); err != nil {
		t.Fatalf("same digest under separate kind: %v", err)
	}
}

func TestTodo_REV_030_01_Security(t *testing.T) {
	store, _, tenantID, tenant, _ := artifactFixture(t)
	ctx := context.Background()
	payload := []byte(`{"plan":"immutable"}`)
	if err := store.Put(ctx, tenant, "repair_plan", "plan:locked", payload); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Put(ctx, tenant, "repair_plan", "plan:locked", []byte(`{"plan":"tampered"}`)); !errors.Is(err, ErrConflict) {
		t.Fatalf("same digest with different body = %v, want ErrConflict", err)
	}
	tx, err := store.db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin update: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE dataops_diagnostic_artifact SET payload=$1 WHERE tenant_id=$2 AND artifact_digest=$3`, []byte("forged"), tenantID, "plan:locked"); err == nil {
		t.Fatal("app role updated append-only diagnostic artifact")
	}
	_ = tx.Rollback(ctx)
	if _, err := store.Get(ctx, tenant, "unexpected", "plan:locked"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown kind = %v, want ErrInvalid", err)
	}
}
