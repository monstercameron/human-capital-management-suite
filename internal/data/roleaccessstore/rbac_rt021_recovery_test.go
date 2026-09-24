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

func TestTodo_RBAC_RT_021_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Ledger recovery','ACTIVE',$3)`, tenantID, "rt21-recovery", time.Now().UTC())
	resolve := func(tenant values.TenantId) uuid.UUID {
		if tenant == "rt21-recovery" {
			return tenantID
		}
		return uuid.Nil
	}
	ctx := context.Background()
	firstStore := New(db.Conn, resolve)
	if err := firstStore.Bootstrap(ctx, "rt21-recovery", "system:bootstrap"); err != nil {
		t.Fatal(err)
	}
	role, err := firstStore.SaveRole(ctx, "rt21-recovery", "principal:admin", roleaccess.Role{
		ID: "recoverable", Name: "Initial role", Active: true, Reason: "Create role for historical recovery proof",
	})
	if err != nil {
		t.Fatal(err)
	}
	var firstRecorded time.Time
	if err := db.QueryRow(ctx, `SELECT recorded_at FROM access_role_revision WHERE tenant_id=$1 AND change_kind='ROLE' AND role_id='recoverable'`, tenantID).Scan(&firstRecorded); err != nil {
		t.Fatal(err)
	}
	role.Name = "Updated role"
	role.Reason = "Rename role after organizational review"
	role, err = firstStore.SaveRole(ctx, "rt21-recovery", "principal:admin", role)
	if err != nil {
		t.Fatal(err)
	}
	var secondRecorded time.Time
	if err := db.QueryRow(ctx, `SELECT recorded_at FROM access_role_revision WHERE tenant_id=$1 AND change_kind='ROLE' AND role_id='recoverable' ORDER BY recorded_at DESC LIMIT 1`, tenantID).Scan(&secondRecorded); err != nil {
		t.Fatal(err)
	}

	// A newly composed Store sees the durable ledger and reconstructs both the
	// state immediately after creation and the latest state after the rename.
	restarted := New(db.Conn, resolve)
	created, err := restarted.LoadAt(ctx, "rt21-recovery", "org:recovery", firstRecorded.Add(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	if got := findRole(created, "recoverable"); got == nil || got.Name != "Initial role" {
		t.Fatalf("historical role after create = %+v, want Initial role", got)
	}
	latest, err := restarted.LoadAt(ctx, "rt21-recovery", "org:recovery", secondRecorded.Add(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	if got := findRole(latest, "recoverable"); got == nil || got.Name != "Updated role" {
		t.Fatalf("historical role after rename = %+v, want Updated role", got)
	}
	streamKey := "tenant:" + tenantID.String() + ":role-access"
	rows, err := db.Conn.Query(ctx, `SELECT payload FROM ledger_event WHERE tenant_id=$1 AND stream_key=$2 ORDER BY sequence DESC LIMIT 2`, tenantID, streamKey)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		var event journeyv1.RoleAccessRevisionEvent
		if err := proto.Unmarshal(payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.RoleId == "recoverable" && event.Reason != "" {
			seen[event.Reason] = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !seen["Create role for historical recovery proof"] || !seen["Rename role after organizational review"] {
		t.Fatalf("recovered role ledger events contain reasons %v", seen)
	}
}

func findRole(snapshot roleaccess.Snapshot, roleID string) *roleaccess.Role {
	for index := range snapshot.Roles {
		if snapshot.Roles[index].ID == roleID {
			return &snapshot.Roles[index]
		}
	}
	return nil
}
