package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestPostgresWorkflowInstanceVersionReaderRejectsIncompleteIdentity(t *testing.T) {
	r := PostgresWorkflowInstanceVersionReader{}
	tests := []struct {
		name   string
		tenant uuid.UUID
		inst   uuid.UUID
	}{
		{name: "nil tenant", inst: uuid.New()},
		{name: "nil instance", tenant: uuid.New()},
		{name: "nil both"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := r.LoadWorkflowInstanceVersion(context.Background(), tc.tenant, tc.inst); err == nil {
				t.Fatal("incomplete identity was accepted")
			}
		})
	}
}

func TestPostgresWorkflowInstanceVersionReaderReadsTenantScopedVersion(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenant, foreign, instance := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for _, id := range []uuid.UUID{tenant, foreign} {
		db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-clock','Version test','ACTIVE',$3)`, id, "version-"+id.String(), now.Add(-time.Hour))
	}
	db.Exec(t, `INSERT INTO workflow_instance (tenant_id,instance_id,cell_id,workflow_id,workflow_version,compiled_plan_hash,execution_mode,runtime_status,input_ref,instance_version,correlation_id,created_at) VALUES ($1,$2,'cell-clock','workflow.clock',1,$3,'EXECUTE','RUNNING',$4,7,$5,$6)`, tenant, instance, strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64), "corr-"+instance.String(), now)
	r := PostgresWorkflowInstanceVersionReader{Pool: db.Conn}
	got, err := r.LoadWorkflowInstanceVersion(ctx, tenant, instance)
	if err != nil || got != 7 {
		t.Fatalf("version=%d err=%v, want 7", got, err)
	}
	if _, err := r.LoadWorkflowInstanceVersion(ctx, foreign, instance); err == nil {
		t.Fatal("foreign tenant instance was returned")
	}
	if _, err := r.LoadWorkflowInstanceVersion(ctx, tenant, uuid.New()); err == nil {
		t.Fatal("missing instance was returned")
	}
}

func TestWorkflowInstanceVersionReaderInterface(t *testing.T) {
	var _ WorkflowInstanceVersionReader = PostgresWorkflowInstanceVersionReader{}
}
