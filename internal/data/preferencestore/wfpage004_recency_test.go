package preferencestore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_WFPAGE_004_RecentOrdersByLastUse proves the Recent list's
// "last ten distinct starts" is true recency: the workflow recorded last
// sorts first no matter how often the others were recorded.
func TestTodo_WFPAGE_004_RecentOrdersByLastUse(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-test','Test','ACTIVE',$3)`, tenantID, "prefs-recency-test", time.Now().UTC())
	store := New(db.Conn, func(values.TenantId) uuid.UUID { return tenantID })
	ctx := context.Background()
	tenant := values.TenantId("prefs-recency-test")

	first, err := store.RecordWorkflowUse(ctx, tenant, "walt", "hire")
	if err != nil || first.WorkflowUses["hire"] != 1 {
		t.Fatalf("first record: %+v err=%v", first.WorkflowUses, err)
	}
	if _, err := store.RecordWorkflowUse(ctx, tenant, "walt", "hire"); err != nil {
		t.Fatal(err)
	}
	last, err := store.RecordWorkflowUse(ctx, tenant, "walt", "promotion")
	if err != nil {
		t.Fatal(err)
	}
	if last.WorkflowUses["promotion"] <= last.WorkflowUses["hire"] {
		t.Fatalf("last-recorded promotion does not sort first: %+v", last.WorkflowUses)
	}
	loaded, err := store.Load(ctx, tenant, "org:test:north", "walt")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.User.WorkflowUses["promotion"] <= loaded.User.WorkflowUses["hire"] {
		t.Fatalf("reloaded recency does not hold: %+v", loaded.User.WorkflowUses)
	}
}
