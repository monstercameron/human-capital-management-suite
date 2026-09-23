package roleaccessstore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_RBAC_RT_021_Integration is the INTEGRATION matrix entry: the
// 00327 ledger table records a revision row per role change on the real
// store and refuses every in-place rewrite afterwards.
func TestTodo_RBAC_RT_021_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Ledger','ACTIVE',$3)`, tenantID, "rt21-ledger", time.Now().UTC())
	tenants := map[values.TenantId]uuid.UUID{"rt21-ledger": tenantID}
	store := New(db.Conn, func(tenant values.TenantId) uuid.UUID { return tenants[tenant] })
	ctx := context.Background()
	if err := store.Bootstrap(ctx, "rt21-ledger", "system:test"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	revisionID := uuid.New()
	entry, err := NewRevision(revisionID, "system:test", RevisionPagePermission, "manager", "", "journeys", "", nil, map[string]bool{"view": true}, nil)
	if err != nil {
		t.Fatalf("NewRevision: %v", err)
	}
	db.Exec(t, `INSERT INTO access_role_revision (tenant_id,revision_id,actor_ref,change_kind,role_id,worker_ref,page_id,feature_id,before_row,after_row,prior_revision) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		tenantID, entry.RevisionID, entry.ActorRef, string(entry.Kind), entry.RoleID, entry.WorkerRef, entry.PageID, entry.FeatureID, entry.Before, entry.After, entry.PriorRevision)

	var recordedKind, recordedRole string
	if err := db.QueryRow(ctx, `SELECT change_kind, role_id FROM access_role_revision WHERE tenant_id=$1 AND revision_id=$2`, tenantID, revisionID).Scan(&recordedKind, &recordedRole); err != nil {
		t.Fatalf("read back revision: %v", err)
	}
	if recordedKind != "PAGE_PERMISSION" || recordedRole != "manager" {
		t.Fatalf("ledger row = %s/%s, want PAGE_PERMISSION/manager", recordedKind, recordedRole)
	}

	if err := db.ExecErr(`UPDATE access_role_revision SET actor_ref='mallory' WHERE tenant_id=$1`, tenantID); err == nil {
		t.Fatal("ledger UPDATE succeeded, want the append-only trigger to refuse it")
	}
	if err := db.ExecErr(`DELETE FROM access_role_revision WHERE tenant_id=$1`, tenantID); err == nil {
		t.Fatal("ledger DELETE succeeded, want the append-only trigger to refuse it")
	}
	var remaining int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM access_role_revision WHERE tenant_id=$1`, tenantID).Scan(&remaining); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("ledger holds %d rows, want the single recorded revision", remaining)
	}
}
