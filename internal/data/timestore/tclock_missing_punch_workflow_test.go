package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

func TestTodo_TCLOCK011_WorkflowRequestAndCorrectionAreDurable(t *testing.T) {
	db := pgtest.NewEmpty(t)
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationsFS(t), goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
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
	tenant := "missing-punch-workflow"
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	if _, err := store.OpenSession(ctx, tenant, SessionRow{ID: "session-1", TenantID: tenant, WorkerRef: "worker-1", AssignmentRef: "assignment-1", Status: "OPEN", Source: "APP", OpenedAt: at}, EventRow{SessionID: "session-1", Kind: "OPENED", ActorRef: "worker-1", IdempotencyKey: "open-1", Digest: "open-digest"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AppendObservation(ctx, tenant, ObservationRow{ID: "observation-1", TenantID: tenant, WorkerRef: "worker-1", AssignmentRef: "assignment-1", Source: "APP", EventType: "CLOCK_IN", OccurredAt: at, ReceivedAt: at, IdempotencyKey: "in-1", Digest: "in-digest"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplySessionTransition(ctx, tenant, "session-1", 1, SessionRow{ID: "session-1", TenantID: tenant, WorkerRef: "worker-1", AssignmentRef: "assignment-1", Status: "OPEN", Source: "APP", Payload: json.RawMessage(`{"open_exceptions":[{"kind":"MISSING_OUT"},{"kind":"OTHER"}]}`)}, []EventRow{{SessionID: "session-1", Kind: "MISSING_OUT", ActorRef: "workflow", IdempotencyKey: "missing-out-1", Digest: "missing-out-digest", Payload: json.RawMessage(`{"terminal":"TIME_SESSION_MISSING_OUT"}`)}}); err != nil {
		t.Fatal(err)
	}
	instance := uuid.New()
	if err := store.BindWorkflowSessionRun(ctx, WorkflowSessionRun{TenantID: tenant, SessionID: "session-1", InstanceID: instance, WorkflowID: "clock.workflow", PlanDigest: "sha256:plan", StartKey: "start-1", CorrelationID: "corr-1", CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	req := MissingPunchWorkflowRequest{TenantID: tenant, ID: "request-1", SessionID: "session-1", OriginalObservationID: "observation-1", WorkerRef: "worker-1", ClaimedOutAt: at.Add(time.Hour), At: at.Add(time.Minute), Reason: "forgot out", RequestedBy: "worker-1", IdempotencyKey: "request-key", ExpectedSessionRevision: 2, OriginalWorkflowInstanceRef: instance, WorkflowInstanceID: instance, WorkflowTraceID: "trace-request", WorkflowNodeID: "commit_missing_punch_request", WorkflowPlanDigest: "sha256:plan", WorkflowAttempt: 1, WorkflowInstanceVersion: 3}
	created, err := store.CommitMissingPunchWorkflowRequest(ctx, req)
	if err != nil || created.Status != "PENDING" || created.RequestRevision != 1 {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	if replay, err := store.CommitMissingPunchWorkflowRequest(ctx, req); err != nil || replay.RequestRevision != 1 {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	mutated := req
	mutated.Reason = "changed after retry"
	if _, err := store.CommitMissingPunchWorkflowRequest(ctx, mutated); !errors.Is(err, ErrMissingPunchWorkflowReplay) {
		t.Fatalf("mutated request replay err=%v, want conflict", err)
	}
	decision := MissingPunchWorkflowDecision{TenantID: tenant, RequestID: req.ID, ActorRef: "supervisor-1", IdempotencyKey: "decision-key", ExpectedRequestRevision: 1, Approve: true, At: at.Add(2 * time.Hour), WorkflowInstanceID: instance, WorkflowTraceID: "trace-correction", WorkflowNodeID: "append_correction", WorkflowPlanDigest: "sha256:plan", WorkflowAttempt: 1, WorkflowInstanceVersion: 5}
	workerDecision := decision
	workerDecision.ActorRef = req.WorkerRef
	workerDecision.IdempotencyKey = "worker-decision-key"
	if _, err := store.CommitMissingPunchWorkflowDecision(ctx, workerDecision); !errors.Is(err, ErrInvalid) {
		t.Fatalf("worker approval err=%v, want invalid", err)
	}
	corrected, err := store.CommitMissingPunchWorkflowDecision(ctx, decision)
	if err != nil || corrected.Status != "APPROVED" || corrected.CorrectionObservationID == "" {
		t.Fatalf("corrected=%+v err=%v", corrected, err)
	}
	var count int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM time_observation WHERE tenant_id=$1 AND corrects_id=$2`, tenant, req.OriginalObservationID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("correction observations=%d err=%v", count, err)
	}
	var originalEvent, originalDigest string
	if err := db.Conn.QueryRow(ctx, `SELECT event_type,digest FROM time_observation WHERE tenant_id=$1 AND id=$2`, tenant, req.OriginalObservationID).Scan(&originalEvent, &originalDigest); err != nil {
		t.Fatal(err)
	}
	if originalEvent != "CLOCK_IN" || originalDigest != "in-digest" {
		t.Fatalf("original observation mutated: event=%q digest=%q", originalEvent, originalDigest)
	}
	var sessionPayload []byte
	if err := db.Conn.QueryRow(ctx, `SELECT payload FROM time_session WHERE tenant_id=$1 AND id=$2`, tenant, req.SessionID).Scan(&sessionPayload); err != nil {
		t.Fatal(err)
	}
	var sessionState map[string]any
	if err := json.Unmarshal(sessionPayload, &sessionState); err != nil {
		t.Fatal(err)
	}
	openExceptions, ok := sessionState["open_exceptions"].([]any)
	if !ok || len(openExceptions) != 1 || openExceptions[0].(map[string]any)["kind"] != "OTHER" {
		t.Fatalf("session exceptions after correction=%v", sessionState["open_exceptions"])
	}
	if replay, err := store.CommitMissingPunchWorkflowDecision(ctx, decision); err != nil || replay.CorrectionObservationID != corrected.CorrectionObservationID {
		t.Fatalf("decision replay=%+v err=%v", replay, err)
	}
	proof := MissingPunchWorkflowProof{TenantID: tenant, RequestID: req.ID, ExpectedProofRevision: 0, WorkflowInstanceID: instance, WorkflowTraceID: "trace-final", WorkflowNodeID: "effect-completed", WorkflowPlanDigest: "sha256:plan", WorkflowAttempt: 2, WorkflowInstanceVersion: 6, ProofDigest: "sha256:completed", ProofPayload: json.RawMessage(`{"effect":"correction-committed"}`), CompletedAt: at.Add(3 * time.Hour)}
	proofRecord, err := store.AppendCompletedMissingPunchProof(ctx, proof)
	if err != nil || proofRecord.CompletedProofRevision != 1 || proofRecord.CompletedProofDigest != proof.ProofDigest {
		t.Fatalf("proof=%+v err=%v", proofRecord, err)
	}
	if replay, err := store.AppendCompletedMissingPunchProof(ctx, proof); err != nil || replay.CompletedProofRevision != 1 {
		t.Fatalf("proof replay=%+v err=%v", replay, err)
	}
	mutatedProof := proof
	mutatedProof.ProofDigest = "sha256:mutated"
	if _, err := store.AppendCompletedMissingPunchProof(ctx, mutatedProof); !errors.Is(err, ErrMissingPunchWorkflowReplay) {
		t.Fatalf("mutated proof replay err=%v, want conflict", err)
	}
	mutatedDecision := decision
	mutatedDecision.DecisionNote = "mutated"
	if _, err := store.CommitMissingPunchWorkflowDecision(ctx, mutatedDecision); !errors.Is(err, ErrMissingPunchWorkflowReplay) {
		t.Fatalf("mutated decision replay err=%v, want conflict", err)
	}
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM time_outbox WHERE tenant_id=$1 AND event_type IN ('clock.missing_punch.requested','clock.missing_punch.approved','clock.missing_punch.proof_completed')`, tenant).Scan(&count); err != nil || count != 3 {
		t.Fatalf("workflow outbox=%d err=%v", count, err)
	}
	// A second request demonstrates the request CAS under concurrent review.
	if _, err := store.OpenSession(ctx, tenant, SessionRow{ID: "session-2", TenantID: tenant, WorkerRef: "worker-2", AssignmentRef: "assignment-2", Status: "OPEN", Source: "APP", OpenedAt: at}, EventRow{SessionID: "session-2", Kind: "OPENED", ActorRef: "worker-2", IdempotencyKey: "open-2", Digest: "open-2"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AppendObservation(ctx, tenant, ObservationRow{ID: "observation-2", TenantID: tenant, WorkerRef: "worker-2", AssignmentRef: "assignment-2", Source: "APP", EventType: "CLOCK_IN", OccurredAt: at, ReceivedAt: at, IdempotencyKey: "in-2", Digest: "in-2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplySessionTransition(ctx, tenant, "session-2", 1, SessionRow{ID: "session-2", TenantID: tenant, WorkerRef: "worker-2", AssignmentRef: "assignment-2", Status: "OPEN", Source: "APP", Payload: json.RawMessage(`{"open_exceptions":[{"kind":"MISSING_OUT"}]}`)}, []EventRow{{SessionID: "session-2", Kind: "MISSING_OUT", ActorRef: "workflow", IdempotencyKey: "missing-out-2", Digest: "missing-out-2", Payload: json.RawMessage(`{"terminal":"TIME_SESSION_MISSING_OUT"}`)}}); err != nil {
		t.Fatal(err)
	}
	instance2 := uuid.New()
	if err := store.BindWorkflowSessionRun(ctx, WorkflowSessionRun{TenantID: tenant, SessionID: "session-2", InstanceID: instance2, WorkflowID: "clock.workflow", PlanDigest: "sha256:plan", StartKey: "start-2", CorrelationID: "corr-2", CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	req2 := req
	req2.ID, req2.SessionID, req2.OriginalObservationID, req2.WorkerRef = "request-2", "session-2", "observation-2", "worker-2"
	req2.IdempotencyKey, req2.OriginalWorkflowInstanceRef, req2.WorkflowInstanceID = "request-key-2", instance2, instance2
	if _, err := store.CommitMissingPunchWorkflowRequest(ctx, req2); err != nil {
		t.Fatal(err)
	}
	decision2 := decision
	decision2.RequestID, decision2.IdempotencyKey, decision2.ExpectedRequestRevision = req2.ID, "decision-key-2", 1
	decision2.ActorRef, decision2.WorkflowInstanceID = "supervisor-2", instance2
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		key := fmt.Sprintf("decision-key-2-%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			attempt := decision2
			attempt.IdempotencyKey = key
			_, err := store.CommitMissingPunchWorkflowDecision(ctx, attempt)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	var wins, conflicts int
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflicts++
		} else {
			t.Errorf("concurrent decision err=%v", err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("concurrent decisions wins=%d conflicts=%d", wins, conflicts)
	}
}

func TestTodo_TCLOCK011_WorkflowRejectsCrossTenantObservation(t *testing.T) {
	db := pgtest.NewEmpty(t)
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationsFS(t), goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
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
	_, err = store.CommitMissingPunchWorkflowRequest(context.Background(), MissingPunchWorkflowRequest{TenantID: "tenant-a", ID: "request-a", SessionID: "session-a", OriginalObservationID: "observation-b", WorkerRef: "worker-a", ClaimedOutAt: time.Now().UTC(), At: time.Now().UTC(), Reason: "reason", RequestedBy: "worker-a", IdempotencyKey: "key", ExpectedSessionRevision: 1, OriginalWorkflowInstanceRef: uuid.New(), WorkflowInstanceID: uuid.New(), WorkflowTraceID: "trace", WorkflowNodeID: "node", WorkflowPlanDigest: "plan", WorkflowAttempt: 1, WorkflowInstanceVersion: 1})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross tenant/missing observation err=%v, want not found", err)
	}
}

func migrationsFS(t *testing.T) fs.FS {
	t.Helper()
	root, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	return root
}
