package application

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestAgentUXSearch_Directory_Security_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	db.Exec(t, `CREATE TABLE journey_worker(tenant_id uuid NOT NULL,worker_key text NOT NULL)`)
	db.Exec(t, `INSERT INTO journey_worker VALUES($1,'alice'),($1,'bob'),($2,'foreign')`, pgstore.TenantID("workspace"), pgstore.TenantID("other"))
	pool, err := pgxadapter.NewPool(context.Background(), personaChatSchemaDSN(t, db.URL, db.Schema), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	d := WorkspaceDocumentDirectory{DB: pool, TenantUUID: func(tenant values.TenantId) uuid.UUID { return pgstore.TenantID(tenant.String()) }}
	members, err := d.WorkspaceDocumentMembers(context.Background(), "workspace")
	if err != nil || !slices.Equal(members, []string{"alice", "bob"}) {
		t.Fatalf("incomplete or cross-tenant directory: %+v %v", members, err)
	}
	if _, err := d.WorkspaceDocumentMembers(context.Background(), "absent"); err == nil {
		t.Fatal("empty directory allowed")
	}
	if _, err := (WorkspaceDocumentDirectory{}).WorkspaceDocumentMembers(context.Background(), "workspace"); err == nil {
		t.Fatal("missing authority allowed")
	}
}
