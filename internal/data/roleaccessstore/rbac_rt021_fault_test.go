package roleaccessstore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_RBAC_RT_021_Fault(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Ledger fault','ACTIVE',$3)`, tenantID, "rt21-fault", time.Now().UTC())
	store := New(db.Conn, func(tenant values.TenantId) uuid.UUID {
		if tenant == "rt21-fault" {
			return tenantID
		}
		return uuid.Nil
	})
	ctx := context.Background()
	if err := store.Bootstrap(ctx, "rt21-fault", "system:bootstrap"); err != nil {
		t.Fatal(err)
	}
	streamKey := "tenant:" + tenantID.String() + ":role-access"
	var eventsBefore int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_event WHERE tenant_id=$1 AND stream_key=$2`, tenantID, streamKey).Scan(&eventsBefore); err != nil {
		t.Fatal(err)
	}
	var revisionsBefore int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM access_role_revision WHERE tenant_id=$1`, tenantID).Scan(&revisionsBefore); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `CREATE FUNCTION reject_rt21_permission_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced RBAC-RT-021 event failure'; END $$`)
	db.Exec(t, `CREATE TRIGGER reject_rt21_permission_event BEFORE INSERT ON ledger_event FOR EACH ROW WHEN (NEW.source_ref='hcmnext:data:roleaccessstore') EXECUTE FUNCTION reject_rt21_permission_event()`)
	_, err := store.SaveRole(ctx, "rt21-fault", "principal:admin", roleaccess.Role{
		ID: "atomic_probe", Name: "Atomic probe", Active: true, Reason: "Create role for atomicity proof",
	})
	if err == nil {
		t.Fatal("SaveRole succeeded despite forced ledger event failure")
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM access_role WHERE tenant_id=$1 AND role_id='atomic_probe'`, tenantID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("permission row count = %d after ledger fault, want rollback", count)
	}
	var eventsAfter int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_event WHERE tenant_id=$1 AND stream_key=$2`, tenantID, streamKey).Scan(&eventsAfter); err != nil {
		t.Fatal(err)
	}
	if eventsAfter != eventsBefore {
		t.Fatalf("ledger event count = %d after event fault, want rollback to %d", eventsAfter, eventsBefore)
	}
	var revisionsAfter int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM access_role_revision WHERE tenant_id=$1`, tenantID).Scan(&revisionsAfter); err != nil {
		t.Fatal(err)
	}
	if revisionsAfter != revisionsBefore {
		t.Fatalf("revision count = %d after event fault, want rollback to %d", revisionsAfter, revisionsBefore)
	}
}
