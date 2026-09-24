package roleaccessstore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"google.golang.org/protobuf/proto"
)

// TestTodo_RBAC_RT_021_Integration is the INTEGRATION matrix entry: the
// 00327/00359 permission revisions and the separate ledger event commit on
// the real store, and neither append-only record accepts an in-place rewrite.
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
	streamKey := "tenant:" + tenantID.String() + ":role-access"
	var eventCountBefore int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_event WHERE tenant_id=$1 AND stream_key=$2`, tenantID, streamKey).Scan(&eventCountBefore); err != nil {
		t.Fatal(err)
	}
	role, err := store.SaveRole(ctx, "rt21-ledger", "principal:admin", roleaccess.Role{
		ID: "ledger_probe", Name: "Ledger probe", Active: true,
		Reason: "Add an auditor role for integration verification",
	})
	if err != nil {
		t.Fatalf("SaveRole: %v", err)
	}
	var actor, reason string
	var before, after []byte
	if err := db.QueryRow(ctx, `SELECT actor_ref,change_reason,before_row,after_row FROM access_role_revision WHERE tenant_id=$1 AND change_kind='ROLE' AND role_id=$2`, tenantID, role.ID).Scan(&actor, &reason, &before, &after); err != nil {
		t.Fatalf("read actual store revision: %v", err)
	}
	if actor != "principal:admin" || reason != "Add an auditor role for integration verification" || len(before) != 0 || len(after) == 0 {
		t.Fatalf("store revision actor=%q reason=%q before=%s after=%s", actor, reason, before, after)
	}
	var eventCountAfter int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_event WHERE tenant_id=$1 AND stream_key=$2`, tenantID, streamKey).Scan(&eventCountAfter); err != nil {
		t.Fatal(err)
	}
	if eventCountAfter != eventCountBefore+1 {
		t.Fatalf("ledger event count moved from %d to %d, want one event for the save", eventCountBefore, eventCountAfter)
	}
	var eventSchema, source string
	var eventPayload []byte
	if err := db.QueryRow(ctx, `SELECT schema_ref,source_ref,payload FROM ledger_event WHERE tenant_id=$1 AND stream_key=$2 ORDER BY sequence DESC LIMIT 1`, tenantID, streamKey).Scan(&eventSchema, &source, &eventPayload); err != nil {
		t.Fatal(err)
	}
	var event journeyv1.RoleAccessRevisionEvent
	if err := proto.Unmarshal(eventPayload, &event); err != nil {
		t.Fatalf("decode typed ledger event: %v", err)
	}
	if eventSchema != roleAccessRevisionSchemaRef || source != "hcmnext:data:roleaccessstore" || event.ActorRef != actor || event.Reason != reason || event.ChangeKind != "ROLE" || event.RoleId != role.ID || event.RevisionId == "" {
		t.Fatalf("ledger event schema/source/payload = %q/%q/%+v", eventSchema, source, &event)
	}
	var baselineCount int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM access_role_revision WHERE tenant_id=$1`, tenantID).Scan(&baselineCount); err != nil {
		t.Fatal(err)
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
	if remaining != baselineCount {
		t.Fatalf("ledger holds %d rows after rejected rewrites, want unchanged count %d", remaining, baselineCount)
	}
}
