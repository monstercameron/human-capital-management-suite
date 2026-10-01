package timestore

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
	"github.com/pressly/goose/v3"
)

func TestTodo_TCLOCK011_StateLayerPersistenceAndReplay(t *testing.T) {
	db := pgtest.NewEmpty(t)
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, layerMigrationsFS(t), goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	store, err := New(context.Background(), Config{DSN: db.URL, Schema: db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	ctx := context.Background()
	tenant, subject := "layer-tenant", "session-layer"
	db.Exec(t, `INSERT INTO time_observation(tenant_id,id,worker_ref,assignment_ref,source,event_type,occurred_at,received_at,idempotency_key,digest) VALUES($1,$2,'worker','assignment','test','CLOCK_IN',$3,$3,'obs-key','sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')`, tenant, "obs-1", time.Now().UTC())
	db.Exec(t, `INSERT INTO time_session(tenant_id,id,worker_ref,assignment_ref,status,source,opened_at,payload) VALUES($1,$2,'worker','assignment','OPEN','test',$3,'{"base":"immutable"}')`, tenant, subject, time.Now().UTC())
	proof := timesession.WorkflowProof{InstanceID: uuid.NewString(), PlanID: "clock", PlanDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", TraceID: "trace", NodeID: "append_correction", Attempt: 1, Version: 1}
	layer := timesession.StateCorrectionLayer{Tenant: tenant, Subject: subject, BaseRevision: 1, Revision: 1, ParentRevision: 0, Approved: true, Provenance: timesession.LayerProvenance{Actor: "supervisor", Reason: "reason", WorkflowProof: proof}, Patches: []timesession.StateLayerPatch{{Path: "reason", Value: timesession.StringValue("reason")}}}
	layer.Digest = layer.DigestValue()
	if err := store.BindWorkflowSessionRun(ctx, WorkflowSessionRun{TenantID: tenant, SessionID: subject, InstanceID: uuid.MustParse(proof.InstanceID), WorkflowID: "clock", PlanDigest: proof.PlanDigest, StartKey: "start-layer", CorrelationID: "corr-layer", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO missed_punch_workflow_request(tenant_id,id,session_id,original_observation_id,worker_ref,claimed_out_at,reason,requested_by,status,request_revision,expected_session_revision,period_closed,original_workflow_instance_ref,workflow_instance_id,workflow_trace_id,workflow_node_id,workflow_plan_digest,workflow_attempt,workflow_instance_version) VALUES($1,'request-1',$2,'obs-1','worker',$3,'reason','worker','APPROVED',1,1,false,$4,$4,'trace','append_correction',$5,1,1)`, tenant, subject, time.Now().UTC(), uuid.MustParse(proof.InstanceID), proof.PlanDigest)
	db.Exec(t, `INSERT INTO missed_punch_workflow_execution_proof(tenant_id,id,request_id,proof_revision,workflow_instance_id,workflow_trace_id,workflow_node_id,workflow_plan_digest,workflow_attempt,workflow_instance_version,proof_digest,proof_payload,completed_at) VALUES($1,'proof-1','request-1',1,$2,'trace','append_correction',$3,1,1,'sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc','{}',now())`, tenant, uuid.MustParse(proof.InstanceID), proof.PlanDigest)
	db.Exec(t, `INSERT INTO missed_punch_workflow_version(tenant_id,id,request_id,revision,action,status,actor_ref,idempotency_key,input_digest,workflow_instance_id,workflow_trace_id,workflow_node_id,workflow_plan_digest,workflow_attempt,workflow_instance_version) VALUES($1,'version-approved','request-1',2,'APPROVE','APPROVED','supervisor','decision-key','decision-digest',$2,'trace','append_correction',$3,1,1)`, tenant, uuid.MustParse(proof.InstanceID), proof.PlanDigest)
	appendLayer := func(candidate timesession.StateCorrectionLayer, key string) (StateLayerRecord, bool, error) {
		return store.AppendStateLayerRequest(ctx, StateLayerAppendRequest{Tenant: tenant, RequestID: "request-1", Layer: candidate, IdempotencyKey: key, OriginalObservation: "obs-1"})
	}
	if err := store.CaptureStateBase(ctx, tenant, subject, 2); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("wrong base revision=%v", err)
	}
	if err := store.CaptureStateBase(ctx, tenant, subject, 1); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `UPDATE time_session SET revision=2,payload='{"reason":"mutable projection"}' WHERE tenant_id=$1 AND id=$2`, tenant, subject)
	first, replay, err := appendLayer(layer, "idem-1")
	if err != nil || replay || first.LayerID == "" {
		t.Fatalf("first=%+v replay=%v err=%v", first, replay, err)
	}
	var persisted int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM time_state_layer WHERE tenant_id=$1 AND subject_id=$2 AND approved=true`, tenant, subject).Scan(&persisted); err != nil || persisted != 1 {
		t.Fatalf("persisted layers=%d err=%v", persisted, err)
	}
	if visible, err := store.ListStateLayers(ctx, tenant, subject, 1); err != nil || len(visible) != 1 {
		t.Fatalf("visible layers=%d err=%v", len(visible), err)
	}
	second := layer
	second.Revision, second.ParentRevision = 2, 1
	second.Patches = []timesession.StateLayerPatch{{Path: "x.note", Value: timesession.StringValue("second")}}
	second.Digest = second.DigestValue()
	if _, replay, err := appendLayer(second, "idem-2"); err != nil || replay {
		t.Fatalf("second replay=%v err=%v", replay, err)
	}
	if historical, err := store.ListStateLayers(ctx, tenant, subject, 1); err != nil || len(historical) != 1 {
		t.Fatalf("historical=%+v err=%v", historical, err)
	}
	if current, err := store.ListStateLayers(ctx, tenant, subject, 2); err != nil || len(current) != 2 {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	again, replay, err := appendLayer(layer, "idem-1")
	if err != nil || !replay || again.LayerID != first.LayerID {
		t.Fatalf("replay=%+v replay=%v err=%v", again, replay, err)
	}
	historicalState, err := store.GetStateAsOf(ctx, tenant, subject, 1)
	if err != nil {
		t.Fatal(err)
	}
	historicalValue, historicalProvenance, err := historicalState.Get("reason", 1)
	if err != nil || historicalValue.String != "reason" || historicalProvenance.WorkflowProof.TraceID != proof.TraceID {
		t.Fatalf("historical value=%+v proof=%+v err=%v", historicalValue, historicalProvenance, err)
	}
	currentState, err := store.GetStateAsOf(ctx, tenant, subject, 2)
	if err != nil {
		t.Fatal(err)
	}
	currentValue, _, err := currentState.Get("x.note", 2)
	if err != nil || currentValue.String != "second" {
		t.Fatalf("current value=%+v err=%v", currentValue, err)
	}
	if _, err := store.GetStateAsOf(ctx, tenant, subject, 3); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("future revision err=%v", err)
	}
	for _, mutate := range []struct {
		name   string
		change func(*timesession.StateCorrectionLayer)
	}{
		{"trace", func(v *timesession.StateCorrectionLayer) { v.Provenance.WorkflowProof.TraceID = "forged" }},
		{"attempt", func(v *timesession.StateCorrectionLayer) { v.Provenance.WorkflowProof.Attempt++ }},
		{"version", func(v *timesession.StateCorrectionLayer) { v.Provenance.WorkflowProof.Version++ }},
		{"node", func(v *timesession.StateCorrectionLayer) { v.Provenance.WorkflowProof.NodeID = "supervisor_approval" }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			bad := second
			bad.Revision, bad.ParentRevision = 3, 2
			mutate.change(&bad)
			bad.Digest = bad.DigestValue()
			if _, _, err := appendLayer(bad, "forged-"+mutate.name); !errors.Is(err, ErrInvalid) {
				t.Fatalf("forged proof err=%v", err)
			}
		})
	}
	if _, err := db.Conn.Exec(ctx, `UPDATE time_state_base SET base_payload='{}' WHERE tenant_id=$1 AND subject_id=$2`, tenant, subject); err == nil {
		t.Fatal("immutable base accepted mutation")
	}
	durable, found, err := store.LookupStateLayer(ctx, tenant, subject, "idem-1")
	if err != nil || !found || durable.LayerID != first.LayerID || durable.CreatedAt.IsZero() {
		t.Fatalf("durable=%+v found=%v err=%v", durable, found, err)
	}
	if _, found, err := store.LookupStateLayer(ctx, "other", subject, "idem-1"); err != nil || found {
		t.Fatalf("foreign layer found=%v err=%v", found, err)
	}
	mutated := layer
	mutated.Patches = []timesession.StateLayerPatch{{Path: "reason", Value: timesession.StringValue("tampered")}}
	mutated.Digest = mutated.DigestValue()
	if _, _, err := appendLayer(mutated, "idem-1"); !(errors.Is(err, ErrIdempotencyConflict) || errors.Is(err, ErrInvalid)) {
		t.Fatalf("mutated replay err=%v", err)
	}
	rows, err := store.ListStateLayers(ctx, tenant, subject, 1)
	if err != nil || len(rows) != 1 || rows[0].Layer.Digest != layer.Digest {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	projection, err := store.ReadStateLayerProjection(ctx, tenant, subject)
	if err != nil || projection.CurrentRevision != 2 || !bytes.Contains(projection.BasePayload, []byte(`"base"`)) {
		t.Fatalf("projection=%+v err=%v", projection, err)
	}
	if foreign, err := store.ListStateLayers(ctx, "foreign-tenant", subject, 1); err != nil || len(foreign) != 0 {
		t.Fatalf("foreign tenant read rows=%d err=%v", len(foreign), err)
	}
	unapproved := layer
	unapproved.Approved = false
	if _, _, err := appendLayer(unapproved, "idem-unapproved"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unapproved append err=%v", err)
	}
	wrong := layer
	wrong.Revision, wrong.ParentRevision = 1, 0
	wrong.Digest = wrong.DigestValue()
	if _, _, err := appendLayer(wrong, "stale-key"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale CAS err=%v", err)
	}
	t.Run("concurrent corrections have one winner", func(t *testing.T) {
		start := make(chan struct{})
		results := make(chan error, 2)
		var workers sync.WaitGroup
		for _, key := range []string{"concurrent-a", "concurrent-b"} {
			workers.Add(1)
			go func(key string) {
				defer workers.Done()
				candidate := second
				candidate.Revision, candidate.ParentRevision = 3, 2
				candidate.Patches = []timesession.StateLayerPatch{{Path: "x.note", Value: timesession.StringValue(key)}}
				candidate.Digest = candidate.DigestValue()
				<-start
				_, _, err := appendLayer(candidate, key)
				results <- err
			}(key)
		}
		close(start)
		workers.Wait()
		close(results)
		var accepted, conflicted int
		for err := range results {
			switch {
			case err == nil:
				accepted++
			case errors.Is(err, ErrRevisionConflict):
				conflicted++
			default:
				t.Fatalf("concurrent append: %v", err)
			}
		}
		if accepted != 1 || conflicted != 1 {
			t.Fatalf("accepted=%d conflicted=%d", accepted, conflicted)
		}
		current, err := store.ReadStateLayerProjection(ctx, tenant, subject)
		if err != nil || current.CurrentRevision != 3 {
			t.Fatalf("current=%+v err=%v", current, err)
		}
	})
}

func layerMigrationsFS(t *testing.T) fs.FS {
	t.Helper()
	root, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	return root
}
