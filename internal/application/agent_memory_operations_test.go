package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/memory"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_AGENT_040_DurableBoundaryRefusesMissingPrincipal(t *testing.T) {
	var service *AgentMemoryOperations
	ctx := context.Background()
	if err := service.Put(ctx, nil, memory.Item{}); !errors.Is(err, memory.ErrDenied) {
		t.Fatalf("Put: %v", err)
	}
	if _, err := service.Read(ctx, nil, "raw", "id"); !errors.Is(err, memory.ErrDenied) {
		t.Fatalf("Read: %v", err)
	}
	if _, err := service.Export(ctx, nil); !errors.Is(err, memory.ErrDenied) {
		t.Fatalf("Export: %v", err)
	}
	if err := service.Delete(ctx, nil, "id", "reason"); !errors.Is(err, memory.ErrDenied) {
		t.Fatalf("Delete: %v", err)
	}
	if err := service.Revoke(ctx, nil, memory.SourcePin{}, "reason"); !errors.Is(err, memory.ErrDenied) {
		t.Fatalf("Revoke: %v", err)
	}
	if err := service.Sweep(ctx, nil); !errors.Is(err, memory.ErrDenied) {
		t.Fatalf("Sweep: %v", err)
	}
	if _, err := service.Inventory(ctx, nil); !errors.Is(err, memory.ErrDenied) {
		t.Fatalf("Inventory: %v", err)
	}
}

func TestTodo_AGENT_040_TaskExtractionAuthority(t *testing.T) {
	var service *AgentMemoryOperations
	ctx := context.Background()
	if err := service.BindTaskAuthority(nil); !errors.Is(err, memory.ErrInvalid) {
		t.Fatalf("nil task authority: %v", err)
	}
	if err := service.RetainTaskExtraction(ctx, agentrun.AgentTask{}, agentrun.PlanStep{}, "ref", agentsecurity.QuarantineExtraction{}); !errors.Is(err, memory.ErrInvalid) {
		t.Fatalf("invalid source: %v", err)
	}
	if _, err := service.ReadTaskExtraction(ctx, agentrun.AgentTask{}, agentrun.PlanStep{}, "ref"); !errors.Is(err, memory.ErrDenied) {
		t.Fatalf("unverified task: %v", err)
	}
	if taskExtractionID("t", "a", "bc") == taskExtractionID("t", "ab", "c") || taskExtractionID("t1", "task", "ref") == taskExtractionID("t2", "task", "ref") {
		t.Fatal("extraction reference lost task or tenant scope")
	}
}

func exerciseAgentMemoryBoundary(t *testing.T, f *agentFixture, p *trust.Principal, taskID string, now time.Time) {
	t.Helper()
	ctx := context.Background()
	mapper := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	service, err := NewAgentMemoryOperations(f.pool, mapper, f.cell.RoleAccess, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Inventory(ctx, p); !errors.Is(err, memory.ErrDenied) {
		t.Fatalf("ungranted empty inventory: %v", err)
	}
	id := mapper(values.TenantId(f.tenant))
	item := memory.Item{ID: "prompt", TenantID: f.tenant, OwnerID: p.Subject(), Kind: memory.KindPrompt, SourceOwner: "agent_task", SourceID: taskID, SourceVersion: "1", SourceDigest: "sha256:task", Audience: []string{p.Subject()}, Purpose: "test.answer", DataClass: "PUBLIC", RetentionPolicyID: "records", RetentionVersion: "1", CreatedAt: now, TTL: time.Hour, Invalidators: []string{"task:" + taskID}, Payload: []byte("private prompt")}
	policy := memory.Policy{TenantID: f.tenant, OwnerID: p.Subject(), Purpose: item.Purpose, Version: "1", RetentionPolicyID: "records", RetentionVersion: "1", MaxTTL: time.Hour, AllowedClasses: []string{"PUBLIC"}, AllowedAudiences: item.Audience}
	body, _ := json.Marshal(policy)
	f.db.Exec(t, `INSERT INTO agent_memory_policy VALUES($1,$2,$3,$4::jsonb,$5)`, id, p.Subject(), item.Purpose, body, now.Add(2*time.Hour))
	source := memory.SourceDecision{Current: true, Version: item.SourceVersion, Digest: item.SourceDigest, DataClass: item.DataClass, Audience: item.Audience, Invalidators: item.Invalidators}
	body, _ = json.Marshal(source)
	f.db.Exec(t, `INSERT INTO agent_memory_source VALUES($1,$2,$3,$4,$5::jsonb,$6)`, id, item.SourceOwner, item.SourceID, item.Purpose, body, now.Add(2*time.Hour))
	f.db.Exec(t, `INSERT INTO agent_memory_grant VALUES($1,'memory',$2,$2,$3,ARRAY['WRITE','READ','INVENTORY','EXPORT','DELETE'],$4,$5,'review:memory',NULL)`, id, p.Subject(), item.Purpose, now.Add(-time.Minute), now.Add(time.Hour))
	for _, kind := range []memory.Kind{memory.KindPrompt, memory.KindToolResult} {
		copy := item
		if kind == memory.KindToolResult {
			copy.ID = "tool"
			copy.Kind = kind
			copy.Payload = []byte("private tool outcome")
		}
		if err = service.Put(ctx, p, copy); err != nil {
			t.Fatal(err)
		}
		held := memory.DispositionDecision{Resolved: true, Held: true, CanDelete: true, Reason: "legal-hold"}
		body, _ = json.Marshal(held)
		f.db.Exec(t, `INSERT INTO agent_memory_disposition VALUES($1,$2,$3::jsonb,$4)`, id, copy.ID, body, now.Add(time.Hour))
	}
	read, err := service.Read(ctx, p, "search", item.ID)
	if err != nil || string(read.Payload) != "private prompt" {
		t.Fatalf("authorized source read: %+v %v", read, err)
	}
	metadata, err := service.Inventory(ctx, p)
	if err != nil || len(metadata) != 2 || !metadata[0].Held || metadata[0].CanDelete {
		t.Fatalf("held metadata: %+v %v", metadata, err)
	}
	// A separately granted memory inventory remains usable without dashboard
	// authority. The served projection has no prompt or tool payload.
	surface := &AgentOwnerControls{Memory: service}
	reply, err := surface.Snapshot(trust.WithPrincipal(ctx, p))
	if err != nil || !reply.Snapshot.Available || len(reply.Snapshot.Memory) != 2 || !reply.Snapshot.CanExport || len(reply.Snapshot.Runs) != 0 {
		t.Fatalf("independent served memory grant: %+v %v", reply, err)
	}
	exported, err := service.Export(ctx, p)
	if err != nil || len(exported) != 2 || exported[0].Kind != memory.KindPrompt || exported[1].Kind != memory.KindToolResult {
		t.Fatalf("complete prompt/tool export: %+v %v", exported, err)
	}
	if err = service.Delete(ctx, p, item.ID, "delete"); !errors.Is(err, memory.ErrHeld) {
		t.Fatalf("held trace destruction: %v", err)
	}
	if err = service.Sweep(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err = service.Revoke(ctx, p, memory.SourcePin{}, "revocation"); !errors.Is(err, memory.ErrInvalid) {
		t.Fatalf("unscoped revoke: %v", err)
	}
}
func TestTodo_AGENT2_022_ServedBoundaryRefusesMissingPrincipal(t *testing.T) {
	surface := &AgentOwnerControls{}
	if _, err := surface.Snapshot(context.Background()); !errors.Is(err, agentcontrols.ErrUnauthenticated) {
		t.Fatalf("snapshot: %v", err)
	}
	if _, err := surface.Control(context.Background(), productui.AgentControlsCommand{}); !errors.Is(err, agentcontrols.ErrUnauthenticated) {
		t.Fatalf("control: %v", err)
	}
	if !errors.Is(agentControlsError(memory.ErrHeld), agentcontrols.ErrDenied) || !errors.Is(agentControlsError(memory.ErrInvalid), agentcontrols.ErrInvalid) {
		t.Fatal("typed error mapping is missing")
	}
}
