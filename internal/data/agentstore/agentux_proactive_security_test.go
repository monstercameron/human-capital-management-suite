package agentstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestAgentUXProactive_ServiceSecurityFence_Security_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenant)
	seedPersonaSecurityRun(t, db, tenant)
	store, err := New(ctx, Config{DSN: testDSN(t, db.URL, db.Schema, "postgres", "", "postgres"), CoreDSN: "postgres://core:pw@127.0.0.1:5433/core"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	called := false
	if err = store.WithAnnouncementSecurityFence(ctx, tenant, "admission-persona-1", func() error { called = true; return nil }); err == nil || called {
		t.Fatal("mention admission entered sponsored fence")
	}
	var payload map[string]any
	var raw []byte
	if err = db.SQL.QueryRowContext(ctx, `SELECT request_payload FROM agent_run_request WHERE tenant_id=$1`, tenant).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	payload["principal_chain"] = map[string]string{"mode": "SPONSORED", "agent_principal_id": "service-principal", "sponsor_id": "workload:policy", "requester_id": "owner"}
	raw, err = json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO agent_run_request (tenant_id,request_id,source_kind,source_key_digest,source_ref,request_digest,request_payload,decision,authority_snapshot,admitted_at,deadline,agent_id,agent_version,agent_digest,installation_id,legal_entity_id,principal_chain,purpose,audience,context_scope,budget,cause_id) SELECT tenant_id,'announcement-admission','ANNOUNCEMENT',repeat('3',64),source_ref,request_digest,$2::jsonb,decision,authority_snapshot,admitted_at,deadline,agent_id,agent_version,agent_digest,installation_id,legal_entity_id,$2::jsonb->'principal_chain',purpose,audience,context_scope,budget,cause_id FROM agent_run_request WHERE tenant_id=$1 AND request_id='admission-persona-1'`, tenant, string(raw))
	if err = store.WithAnnouncementSecurityFence(ctx, tenant, "announcement-admission", func() error { called = true; return nil }); err != nil || !called {
		t.Fatalf("service fence: called=%t %v", called, err)
	}
	if _, err = store.RevokePersonaSecurityScope(ctx, tenant, PersonaSecurityScope{Kind: "PRINCIPAL", Key: "service-principal"}, "revoked", time.Now()); err != nil {
		t.Fatal(err)
	}
	called = false
	if err = store.WithAnnouncementSecurityFence(ctx, tenant, "announcement-admission", func() error { called = true; return nil }); !errors.Is(err, ErrPersonaSecurityLeaseRevoked) || called {
		t.Fatalf("revoked service performed effect: called=%t %v", called, err)
	}
	called = false
	if err = store.WithAnnouncementSecurityFence(ctx, uuid.New(), "announcement-admission", func() error { called = true; return nil }); err == nil || called {
		t.Fatal("cross-tenant admission performed effect")
	}
}
