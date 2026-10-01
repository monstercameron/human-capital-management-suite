package agentmemorystore

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/memory"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"testing"
	"time"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }
func TestTodo_AGENT_040_Integration(t *testing.T) {
	db := pgtest.New(t)
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,'memory-one','cell-local','Memory','ACTIVE',now())`, id)
	now := time.Now().UTC().Truncate(time.Microsecond)
	mapper := func(tenant values.TenantId) uuid.UUID {
		if tenant == "memory-one" {
			return id
		}
		return uuid.Nil
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	store, err := New(conn, mapper, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := store.Copy("raw")
	search, _ := store.Copy("search")
	item := memory.Item{ID: "item", TenantID: "memory-one", OwnerID: "owner", Kind: memory.KindPrompt, SourceOwner: "documents", SourceID: "document", SourceVersion: "1", SourceDigest: "sha256:d", Audience: []string{"user"}, Purpose: "answer", DataClass: "PUBLIC", RetentionPolicyID: "records", RetentionVersion: "1", CreatedAt: now, TTL: time.Hour, Invalidators: []string{"source:document"}, Payload: []byte("protected prompt")}
	policy := memory.Policy{TenantID: item.TenantID, OwnerID: item.OwnerID, Purpose: item.Purpose, Version: "1", RetentionPolicyID: item.RetentionPolicyID, RetentionVersion: item.RetentionVersion, MaxTTL: time.Hour, AllowedClasses: []string{"PUBLIC"}, AllowedAudiences: []string{"user"}}
	source := memory.SourceDecision{Current: true, Version: item.SourceVersion, Digest: item.SourceDigest, DataClass: item.DataClass, Audience: item.Audience, Invalidators: item.Invalidators}
	body, _ := json.Marshal(policy)
	db.Exec(t, `INSERT INTO agent_memory_policy VALUES($1,$2,$3,$4::jsonb,$5)`, id, item.OwnerID, item.Purpose, body, now.Add(2*time.Hour))
	body, _ = json.Marshal(source)
	db.Exec(t, `INSERT INTO agent_memory_source VALUES($1,$2,$3,$4,$5::jsonb,$6)`, id, item.SourceOwner, item.SourceID, item.Purpose, body, now.Add(2*time.Hour))
	db.Exec(t, `INSERT INTO agent_memory_grant VALUES($1,'grant','user','owner','answer',ARRAY['WRITE','READ','EXPORT','DELETE','INVENTORY'], $2,$3,'review:1',NULL)`, id, now.Add(-time.Minute), now.Add(2*time.Hour))
	disposition := memory.DispositionDecision{Resolved: true, CanDelete: true, Reason: "records-approved"}
	body, _ = json.Marshal(disposition)
	db.Exec(t, `INSERT INTO agent_memory_disposition VALUES($1,$2,$3::jsonb,$4)`, id, item.ID, body, now.Add(2*time.Hour))
	manager, err := memory.NewManager(memory.Config{Policies: store, Sources: store, Authorizer: store, Disposition: store, Stores: []memory.CopyStore{raw, search}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	actor := memory.Actor{TenantID: item.TenantID, PrincipalID: "user"}
	if err = store.AuthorizeOperation(context.Background(), actor, memory.OperationInventory); err != nil {
		t.Fatal(err)
	}
	if err = store.AuthorizeOperation(context.Background(), memory.Actor{TenantID: item.TenantID, PrincipalID: "other"}, memory.OperationInventory); !errors.Is(err, memory.ErrDenied) {
		t.Fatalf("empty inventory permission: %v", err)
	}
	if err = manager.Put(context.Background(), actor, item); err != nil {
		t.Fatal(err)
	}
	if err = raw.Put(context.Background(), item); err != nil {
		t.Fatalf("exact retry: %v", err)
	}
	got, err := manager.Read(context.Background(), actor, "search", item.ID)
	if err != nil || string(got.Payload) != "protected prompt" {
		t.Fatalf("read: %+v %v", got, err)
	}
	exported, err := manager.ExportTenant(context.Background(), actor)
	if err != nil || len(exported) != 1 || exported[0].Kind != memory.KindPrompt {
		t.Fatalf("export: %+v %v", exported, err)
	}
	inventory, err := raw.ListSource(context.Background(), item.TenantID, item.SourceOwner, item.SourceID)
	if err != nil || !inventory.Complete || inventory.Watermark == "" || len(inventory.Items) != 1 {
		t.Fatalf("inventory: %+v %v", inventory, err)
	}
	inventory, err = raw.ListItem(context.Background(), item.TenantID, item.ID)
	if err != nil || len(inventory.Items) != 1 {
		t.Fatalf("item inventory: %+v %v", inventory, err)
	}
	if _, err = manager.Read(context.Background(), memory.Actor{TenantID: item.TenantID, PrincipalID: "other"}, "raw", item.ID); !errors.Is(err, memory.ErrDenied) {
		t.Fatalf("missing grant: %v", err)
	}
	db.Exec(t, `UPDATE agent_memory_grant SET revoked_at=$2 WHERE tenant_id=$1`, id, now)
	if _, err = manager.Read(context.Background(), actor, "raw", item.ID); !errors.Is(err, memory.ErrDenied) {
		t.Fatalf("revoked grant: %v", err)
	}
	db.Exec(t, `INSERT INTO agent_memory_grant VALUES($1,'grant-2','user','owner','answer',ARRAY['WRITE','READ','EXPORT','DELETE','INVENTORY'], $2,$3,'review:2',NULL)`, id, now.Add(-time.Minute), now.Add(2*time.Hour))
	held := memory.DispositionDecision{Resolved: true, Held: true, CanDelete: true, Reason: "hold"}
	body, _ = json.Marshal(held)
	db.Exec(t, `UPDATE agent_memory_disposition SET decision=$2::jsonb WHERE tenant_id=$1`, id, body)
	if err = manager.Delete(context.Background(), actor, item.ID, "delete"); !errors.Is(err, memory.ErrHeld) {
		t.Fatalf("held destruction: %v", err)
	}
	metadata, err := manager.InventoryMetadata(context.Background(), actor)
	if err != nil || len(metadata) != 1 || !metadata[0].Held || metadata[0].CanDelete || !metadata[0].CanExport {
		t.Fatalf("held metadata: %+v %v", metadata, err)
	}
	body, _ = json.Marshal(disposition)
	db.Exec(t, `UPDATE agent_memory_disposition SET decision=$2::jsonb WHERE tenant_id=$1`, id, body)
	if err = manager.Delete(context.Background(), actor, item.ID, "delete"); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(conn, mapper, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	copy, _ := restarted.Copy("raw")
	if err = copy.Put(context.Background(), item); !errors.Is(err, memory.ErrStale) {
		t.Fatalf("restart resurrected deleted item: %v", err)
	}
	if _, err = copy.Get(context.Background(), item.TenantID, item.ID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("deleted bytes: %v", err)
	}
	if _, err = copy.Get(context.Background(), "foreign", item.ID); !errors.Is(err, memory.ErrDenied) {
		t.Fatalf("foreign tenant: %v", err)
	}
	now = now.Add(3 * time.Hour)
	if _, err = store.ResolveMemoryPolicy(context.Background(), item.TenantID, item.OwnerID, item.Purpose); !errors.Is(err, memory.ErrDenied) {
		t.Fatalf("expired policy: %v", err)
	}
	if _, err = store.CheckMemorySource(context.Background(), memory.SourcePin{TenantID: item.TenantID, Owner: item.SourceOwner, ID: item.SourceID}, item.Purpose); !errors.Is(err, memory.ErrDenied) {
		t.Fatalf("expired source: %v", err)
	}
	if _, err = store.ResolveMemoryDisposition(context.Background(), item, now); !errors.Is(err, memory.ErrDisposition) {
		t.Fatalf("expired disposition: %v", err)
	}
}
func TestTodo_AGENT_040_Security(t *testing.T) {
	if _, err := New(nil, nil, nil); !errors.Is(err, memory.ErrInvalid) {
		t.Fatalf("missing ports: %v", err)
	}
	var store *Store
	if _, err := store.Copy("raw"); !errors.Is(err, memory.ErrInvalid) {
		t.Fatalf("nil store: %v", err)
	}
}
