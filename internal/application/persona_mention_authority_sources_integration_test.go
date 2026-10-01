package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_008_AgentPersonaAuthorityStoreScopesRealPostgresReads(t *testing.T) {
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(context.Background(), db.SQL); err != nil {
		t.Fatalf("migrate isolated agent store: %v", err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id) VALUES ($1),($2)`, tenantA, tenantB)
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatalf("select agent app role: %v", err)
	}
	root, err := agentpersonastore.New(conn, func(tenant values.TenantId) uuid.UUID {
		switch tenant {
		case "tenant-a":
			return tenantA
		case "tenant-b":
			return tenantB
		default:
			return uuid.Nil
		}
	})
	if err != nil {
		t.Fatalf("construct persona store: %v", err)
	}
	reader, err := (AgentPersonaAuthorityStore{Store: root}).ForTenant(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("scope production authority reader: %v", err)
	}
	_, _, err = reader.ReadCurrentPersonaAuthority(context.Background(), "room-a", "persona-a")
	if !errors.Is(err, agentpersonastore.ErrNotFound) {
		t.Fatalf("empty real-store authority lookup error = %v, want not found", err)
	}
	if _, err := (AgentPersonaAuthorityStore{Store: root}).ForTenant(context.Background(), "unknown-tenant"); !errors.Is(err, agentpersonastore.ErrInvalid) {
		t.Fatalf("unknown tenant scope error = %v, want invalid", err)
	}
}
