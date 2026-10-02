package agentinvocationstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestAgentUXR5Srv_Feedback_Integration(t *testing.T) {
	store, tenantA, _, cleanup := agentUXR5SrvFeedbackStore(t)
	defer cleanup()
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)

	first, err := store.SubmitAnswerFeedback(ctx, tenantA.String(), "alice", "invocation-a", true, "", "feedback-001", at)
	if err != nil || !first.Active || !first.Helpful || first.Revision != 1 || first.OutputID != "output-a" {
		t.Fatalf("first feedback=%+v err=%v", first, err)
	}
	replay, err := store.SubmitAnswerFeedback(ctx, tenantA.String(), "alice", "invocation-a", true, "", "feedback-001", at.Add(time.Minute))
	if err != nil || replay.Revision != first.Revision || replay.UpdatedAt != first.UpdatedAt {
		t.Fatalf("replayed feedback changed state: first=%+v replay=%+v err=%v", first, replay, err)
	}
	changed, err := store.SubmitAnswerFeedback(ctx, tenantA.String(), "alice", "invocation-a", false, "The policy version is old.", "feedback-002", at.Add(2*time.Minute))
	if err != nil || !changed.Active || changed.Helpful || changed.Reason == "" || changed.Revision != 2 {
		t.Fatalf("changed feedback=%+v err=%v", changed, err)
	}
	undone, err := store.UndoAnswerFeedback(ctx, tenantA.String(), "alice", "invocation-a", "feedback-undo-001", at.Add(3*time.Minute))
	if err != nil || undone.Active || undone.Reason != "" || undone.Revision != 3 {
		t.Fatalf("undo feedback=%+v err=%v", undone, err)
	}
	undoReplay, err := store.UndoAnswerFeedback(ctx, tenantA.String(), "alice", "invocation-a", "feedback-undo-001", at.Add(4*time.Minute))
	if err != nil || undoReplay.Revision != undone.Revision || undoReplay.UpdatedAt != undone.UpdatedAt {
		t.Fatalf("undo replay changed state: first=%+v replay=%+v err=%v", undone, undoReplay, err)
	}
	if _, err := store.SubmitAnswerFeedback(ctx, tenantA.String(), "alice", "invocation-a", false, "changed replay", "feedback-001", at.Add(5*time.Minute)); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed replay error=%v, want conflict", err)
	}
}

func TestAgentUXR5Srv_Feedback_Security(t *testing.T) {
	store, tenantA, tenantB, cleanup := agentUXR5SrvFeedbackStore(t)
	defer cleanup()
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, tenant, person string
	}{
		{name: "other conversation member", tenant: tenantA.String(), person: "bob"},
		{name: "other tenant", tenant: tenantB.String(), person: "alice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.SubmitAnswerFeedback(ctx, tc.tenant, tc.person, "invocation-a", true, "", "feedback-denied", at); !errors.Is(err, ErrNotFound) {
				t.Fatalf("foreign feedback error=%v, want not found", err)
			}
		})
	}
	if _, err := store.SubmitAnswerFeedback(ctx, tenantA.String(), "alice", "invocation-a", true, strings.Repeat("x", 501), "feedback-long", at); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized reason error=%v", err)
	}
}

func agentUXR5SrvFeedbackStore(t *testing.T) (*Store, uuid.UUID, uuid.UUID, func()) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1),($2)`, tenantA, tenantB); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO persona_final_outputs
		(tenant_id,invocation_id,output_id,invoker_id,conversation_id,thread_id,parent_post_id,persona_id,persona_version,installation_id,admission_digest,agent026_digest,persistence_digest,recovery_receipt,materials,citations,sealed_payload,created_at,admission_id,run_id)
		VALUES ($1,'invocation-a','output-a','alice','room-a','thread-a','post-a','persona-a','1','installation-a',$2,$2,$2,'receipt','[]','[]','{}',now(),'run-a','run-a')`, tenantA, digest); err != nil {
		t.Fatal(err)
	}
	login, password := roleName("r5feedback"), uuid.NewString()
	if err := createLogin(ctx, db.SQL, login, password); err != nil {
		t.Fatal(err)
	}
	pool, err := agentstore.New(ctx, agentstore.Config{DSN: testDSN(t, db.URL, db.Schema, login, password, "postgres"), CoreDSN: "postgres://core:pw@127.0.0.1:5432/core", MaxConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	receipt := ReplyReceipt{TenantID: tenantA.String(), InvocationID: "invocation-a", RunID: "run-a", OutputID: "output-a", OutputDigest: digest, InvokerID: "alice", ConversationID: "room-a", ThreadID: "thread-a", InvokingPostID: "post-a", PersonaID: "persona-a", PersonaVersion: "1", AgentID: "agent-a", Display: "Policy Helper", InvokerHandle: "alice", PublicPostID: "answer-a"}
	if err := store.RecordReplyReceipt(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	return store, tenantA, tenantB, func() {
		pool.Close()
		_, _ = db.SQL.ExecContext(context.Background(), "DROP ROLE "+login)
	}
}
