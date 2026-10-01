package agentpersonastore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
)

func TestTodo_AGENTP_011_ToolJournalIntegration(t *testing.T) {
	f := newFixture(t, "tenant-a", "tenant-b")
	a := f.store(t, "tenant-a")
	b := f.store(t, "tenant-b")
	output := []byte(`{ "Hits" : [{"DocumentID":"doc-a","VersionID":"version-a","Title":"Leave policy"}], "Total" : 1 }`)
	sum := sha256.Sum256(output)
	r := ToolResultRecord{DataClass: "INTERNAL", TenantID: "tenant-a", RunID: "run-a", AdmissionDigest: "sha256:" + strings.Repeat("a", 64), InvocationID: "invocation-a", InvokerID: "user-a", ConversationID: "room-a", ThreadID: "thread-a", PersonaID: "policy-helper", PersonaVersion: "v1", InstallationID: "install-a", AgentID: "agent-a", ToolCallID: "call-a", ToolName: "documents_search", SkillID: "skill-a", SkillVersion: 1, SkillDigest: "sha256:" + strings.Repeat("b", 64), Arguments: []byte(`{ "query" : "leave" }`), Output: output, OutputDigest: "sha256:" + hex.EncodeToString(sum[:]), CreatedAt: f.when}
	ctx := context.Background()
	// Registry pins retain their canonical bare hex digest. Admission and
	// tool-output envelopes use the prefixed digest vocabulary.
	r.SkillDigest = strings.TrimPrefix(r.SkillDigest, "sha256:")
	if err := a.PutToolResult(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := a.PutToolResult(ctx, r); err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	restarted := f.store(t, "tenant-a")
	rows, err := restarted.ListToolResults(ctx, "run-a")
	if err != nil || len(rows) != 1 || !sameToolResult(rows[0], r) {
		t.Fatalf("restart read rows=%+v err=%v", rows, err)
	}
	foreign, err := b.ListToolResults(ctx, "run-a")
	if err != nil || len(foreign) != 0 {
		t.Fatalf("tenant leak: %+v %v", foreign, err)
	}
	if err := b.PutToolResult(ctx, r); !errors.Is(err, ErrInvalid) {
		t.Fatalf("foreign row accepted: %v", err)
	}
	changed := r
	changed.InvokerID = "other-user"
	if err := a.PutToolResult(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("identity reuse accepted: %v", err)
	}
	changed = r
	changed.OutputDigest = "sha256:" + strings.Repeat("0", 64)
	if err := a.PutToolResult(ctx, changed); !errors.Is(err, ErrInvalid) {
		t.Fatalf("forged bytes digest accepted: %v", err)
	}
	conn := f.db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, f.ids["tenant-a"].String()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE persona_tool_results SET record=$1 WHERE tenant_id=$2`, []byte(`{}`), f.ids["tenant-a"]); err == nil {
		t.Fatal("append-only journal mutation succeeded")
	}
}
