package agentinvocationstore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_AGENTP_011_DurableReplyReceiptRequiresSealedOutputAndVisibilityScope(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	a, b := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1),($2)`, a, b); err != nil {
		t.Fatal(err)
	}
	login, password := roleName("replyreceipt"), uuid.NewString()
	if err := createLogin(ctx, db.SQL, login, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.SQL.ExecContext(context.Background(), "DROP ROLE "+login) })
	pool, err := agentstore.New(ctx, agentstore.Config{DSN: testDSN(t, db.URL, db.Schema, login, password, "postgres"), CoreDSN: "postgres://core:pw@127.0.0.1:5432/core", MaxConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, _ := New(pool)
	digest := "sha256:" + strings.Repeat("a", 64)
	receipt := ReplyReceipt{TenantID: a.String(), InvocationID: "invocation", RunID: "run", OutputID: "output", OutputDigest: digest, InvokerID: "invoker", ConversationID: "room", ThreadID: "thread", InvokingPostID: "post", PersonaID: "persona", PersonaVersion: "1", AgentID: "agent:persona", Display: "Policy Helper", InvokerHandle: "invoker", PublicPostID: "reply"}
	if err := store.RecordReplyReceipt(ctx, receipt); !errors.Is(err, ErrConflict) {
		t.Fatalf("receipt without sealed output=%v", err)
	}
	_, err = db.SQL.ExecContext(ctx, `INSERT INTO persona_final_outputs (tenant_id,invocation_id,output_id,invoker_id,conversation_id,thread_id,parent_post_id,persona_id,persona_version,installation_id,admission_digest,agent026_digest,persistence_digest,recovery_receipt,materials,citations,sealed_payload,created_at,admission_id,run_id) VALUES ($1,'invocation','output','invoker','room','thread','post','persona','1','installation',$2,$2,$2,'receipt','[]','[]','{}',now(),'run','run')`, a, digest)
	if err != nil {
		t.Fatal(err)
	}
	wrongRun := receipt
	wrongRun.RunID = "different-run"
	if err := store.RecordReplyReceipt(ctx, wrongRun); !errors.Is(err, ErrConflict) {
		t.Fatalf("receipt with foreign execution=%v", err)
	}
	if err := store.RecordReplyReceipt(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordReplyReceipt(ctx, receipt); err != nil {
		t.Fatalf("receipt replay=%v", err)
	}
	changed := receipt
	changed.AgentID = "agent:forged"
	if err := store.RecordReplyReceipt(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("actor replay changed=%v", err)
	}
	rows, err := store.ListReplyReceipts(ctx, a.String(), "other-member", "room")
	if err != nil || len(rows) != 1 || rows[0].PublicPostID != "reply" || rows[0].AgentID != "agent:persona" {
		t.Fatalf("public receipt=%+v %v", rows, err)
	}
	rows, err = store.ListReplyReceipts(ctx, b.String(), "invoker", "room")
	if err != nil || len(rows) != 0 {
		t.Fatalf("foreign tenant receipt=%+v %v", rows, err)
	}
	if _, err := db.SQL.ExecContext(ctx, `UPDATE persona_reply_receipt SET public_post_id='forged' WHERE tenant_id=$1`, a); err == nil {
		t.Fatal("receipt mutated")
	}
	if _, err := db.SQL.ExecContext(ctx, `DELETE FROM persona_reply_receipt WHERE tenant_id=$1`, a); err == nil {
		t.Fatal("receipt deleted")
	}
	private := receipt
	private.InvocationID = "invocation-private"
	private.OutputID = "output-private"
	private.PublicPostID = ""
	private.EphemeralPostID = "ephemeral"
	private.PrivatePostID = "copy"
	private.PrivateConversationID = "private-dm"
	_, err = db.SQL.ExecContext(ctx, `INSERT INTO persona_final_outputs (tenant_id,invocation_id,output_id,invoker_id,conversation_id,thread_id,parent_post_id,persona_id,persona_version,installation_id,admission_digest,agent026_digest,persistence_digest,recovery_receipt,materials,citations,sealed_payload,created_at,admission_id,run_id) VALUES ($1,'invocation-private','output-private','invoker','room','thread','post','persona','1','installation',$2,$2,$2,'receipt','[]','[]','{}',now(),'run','run')`, a, digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordReplyReceipt(ctx, private); err != nil {
		t.Fatal(err)
	}
	rows, err = store.ListReplyReceipts(ctx, a.String(), "other-member", "room")
	if err != nil || len(rows) != 1 {
		t.Fatalf("private source leaked=%+v %v", rows, err)
	}
	rows, err = store.ListReplyReceipts(ctx, a.String(), "other-member", "private-dm")
	if err != nil || len(rows) != 0 {
		t.Fatalf("private destination leaked=%+v %v", rows, err)
	}
	rows, err = store.ListReplyReceipts(ctx, a.String(), "invoker", "private-dm")
	if err != nil || len(rows) != 1 || rows[0].PrivatePostID != "copy" {
		t.Fatalf("owner private receipt=%+v %v", rows, err)
	}
	mapped, err := NewWithTenantUUID(pool, func(value string) uuid.UUID {
		if value == "logical-tenant" {
			return a
		}
		return uuid.Nil
	})
	if err != nil {
		t.Fatal(err)
	}
	logical := receipt
	logical.TenantID, logical.InvocationID, logical.OutputID, logical.ConversationID = "logical-tenant", "mapped-invocation", "mapped-output", "mapped-room"
	_, err = db.SQL.ExecContext(ctx, `INSERT INTO persona_final_outputs (tenant_id,invocation_id,output_id,invoker_id,conversation_id,thread_id,parent_post_id,persona_id,persona_version,installation_id,admission_digest,agent026_digest,persistence_digest,recovery_receipt,materials,citations,sealed_payload,created_at,admission_id,run_id) VALUES ($1,'mapped-invocation','mapped-output','invoker','mapped-room','thread','post','persona','1','installation',$2,$2,$2,'receipt','[]','[]','{}',now(),'run','run')`, a, digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := mapped.RecordReplyReceipt(ctx, logical); err != nil {
		t.Fatalf("logical tenant receipt=%v", err)
	}
	rows, err = mapped.ListReplyReceipts(ctx, "logical-tenant", "invoker", "mapped-room")
	if err != nil || len(rows) != 1 || rows[0].TenantID != "logical-tenant" {
		t.Fatalf("logical tenant receipt read=%+v %v", rows, err)
	}
}

func TestTodo_AGENTP_011_ReplyReceiptRejectsIncompleteDelivery(t *testing.T) {
	if err := (*Store)(nil).RecordReplyReceipt(context.Background(), ReplyReceipt{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil receipt=%v", err)
	}
	if _, err := (*Store)(nil).ListReplyReceipts(context.Background(), "invalid", "invoker", "room"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil list=%v", err)
	}
}
