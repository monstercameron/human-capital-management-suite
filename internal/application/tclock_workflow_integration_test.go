package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestTodo_WTIME003004_Integration proves that the evidence reader accepts
// only a real successful commit_punch execution linked to the exact durable
// observation and session references. The rows are read from PostgreSQL after
// insertion through the same tenant-scoped reader used by the application.
func TestTodo_WTIME003004_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := uuid.New()
	instanceID := uuid.New()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	observationID := "observation:clock:1"
	sessionID := "session:clock:1"
	traceID := "trace-clock-1"
	planDigest := strings.Repeat("a", 64)
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-clock','Clock proof','ACTIVE',$3)`, tenantID, "clock-proof-"+tenantID.String(), now.Add(-time.Hour))
	db.Exec(t, `INSERT INTO workflow_instance (
		 tenant_id,instance_id,cell_id,workflow_id,workflow_version,compiled_plan_hash,
		 business_subject_refs,execution_mode,runtime_status,completion_dimensions,input_ref,
		 variable_revision_head,current_node_ids,instance_version,correlation_id,created_at)
		 VALUES ($1,$2,'cell-clock','hcmnext.workflows.time.punch_session',1,$3,$4,'EXECUTE','RUNNING','{}'::jsonb,$5,0,$6,4,$7,$8)`,
		tenantID, instanceID, planDigest, []string{"time_session:" + sessionID}, strings.Repeat("b", 64), []string{"await_session_event"}, "clock-proof", now)
	db.Exec(t, `INSERT INTO workflow_node_execution (
		 tenant_id,node_execution_id,instance_id,node_id,attempt,step_type,status,
		 output_artifact_ref,effect_refs,trace_id,started_at,completed_at,recorded_at)
		 VALUES ($1,$2,$3,'commit_punch',1,'CAPABILITY','SUCCEEDED',$4,$5,$6,$7,$8,$8)`,
		tenantID, uuid.New(), instanceID, "sha256:"+strings.Repeat("c", 64),
		[]string{"time_observation:" + observationID, "time_session:" + sessionID}, traceID, now, now)

	reader := PostgresClockWorkflowEvidenceReader{DB: db.Conn}
	evidence, found, err := reader.LoadPunchNodeEvidence(ctx, tenantID, instanceID, observationID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("successful commit_punch evidence was not found")
	}
	if evidence.TenantID != tenantID || evidence.InstanceID != instanceID || evidence.NodeID != "commit_punch" || evidence.ObservationID != observationID || evidence.SessionID != sessionID || evidence.PlanDigest != planDigest || evidence.TraceID != traceID || evidence.CompletedState != "SUCCEEDED" || evidence.Attempt != 1 || evidence.InstanceVersion != 4 || evidence.OutputDigest == "" {
		t.Fatalf("evidence=%+v, want exact durable linkage", evidence)
	}

	missing, found, err := reader.LoadPunchNodeEvidence(ctx, tenantID, instanceID, "observation:other")
	if err != nil {
		t.Fatal(err)
	}
	if found || missing != (clockservice.PunchNodeEvidence{}) {
		t.Fatalf("unlinked observation returned evidence: found=%v evidence=%+v", found, missing)
	}
}
