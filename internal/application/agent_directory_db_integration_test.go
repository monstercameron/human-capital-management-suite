package application

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENT2_027_Integration_CurrentPopulationUsesEffectiveRevisionAndRevocation(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	tenantKey := values.TenantId("population-tenant")
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID, tenantKey, string(tenantKey))
	db.Exec(t, `INSERT INTO agent_current_population (tenant_id,membership_id,subject_ref,population_id,source_identity,revision,effective_from,effective_until) VALUES
		($1,$2,'human-1','old-pop','directory',1,timestamptz '2026-01-01T00:00:00Z',timestamptz '2026-02-01T00:00:00Z'),
		($1,$3,'human-1','current-pop','directory',2,timestamptz '2026-02-01T00:00:00Z',NULL)`, tenantID, uuid.New(), uuid.New())
	app := appRoleConnForPopulation(t, db)
	d := NewAgentDirectoryDB(app, func(values.TenantId) uuid.UUID { return tenantID })
	got, err := d.CurrentPopulation(context.Background(), tenantKey, "human-1")
	if err != nil || got != "current-pop" {
		t.Fatalf("CurrentPopulation = %q, %v; want current-pop", got, err)
	}
	db.Exec(t, `UPDATE agent_current_population SET revoked_at=CURRENT_TIMESTAMP,revoked_by='admin',revocation_reason='withdrawn' WHERE tenant_id=$1 AND subject_ref='human-1' AND population_id='current-pop'`, tenantID)
	if _, err := d.CurrentPopulation(context.Background(), tenantKey, "human-1"); !errors.Is(err, ErrAgentDirectoryUnavailable) {
		t.Fatalf("revoked CurrentPopulation error = %v, want ErrAgentDirectoryUnavailable", err)
	}
}

func TestTodo_AGENT2_027_Security_CurrentPopulationRLSAndSubjectScope(t *testing.T) {
	db := pgtest.New(t)
	tenantA, tenantB := uuid.New(), uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'population-a','cell-local','a','ACTIVE',timestamptz '2026-01-01T00:00:00Z'),($2,'population-b','cell-local','b','ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantA, tenantB)
	db.Exec(t, `INSERT INTO agent_current_population (tenant_id,membership_id,subject_ref,population_id,source_identity,revision,effective_from) VALUES ($1,$3,'human-1','tenant-a-pop','directory',1,CURRENT_TIMESTAMP),($2,$4,'human-1','tenant-b-pop','directory',1,CURRENT_TIMESTAMP)`, tenantA, tenantB, uuid.New(), uuid.New())
	app := appRoleConnForPopulation(t, db)
	d := NewAgentDirectoryDB(app, func(values.TenantId) uuid.UUID { return tenantA })
	got, err := d.CurrentPopulation(context.Background(), values.TenantId("population-a"), "human-1")
	if err != nil || got != "tenant-a-pop" {
		t.Fatalf("tenant A CurrentPopulation = %q, %v", got, err)
	}
	if _, err := d.CurrentPopulation(context.Background(), values.TenantId("population-a"), "human-2"); !errors.Is(err, ErrAgentDirectoryUnavailable) {
		t.Fatalf("unknown subject error = %v, want ErrAgentDirectoryUnavailable", err)
	}
	// A foreign row is present in the same physical schema, but the transaction
	// is bound to tenant A and PostgreSQL RLS must keep tenant B invisible.
	got, err = d.CurrentPopulation(context.Background(), values.TenantId("population-a"), "human-1")
	if err != nil || got != "tenant-a-pop" {
		t.Fatalf("repeated tenant A CurrentPopulation = %q, %v", got, err)
	}
}

func TestTodo_AGENT2_027_Integration_CurrentPopulationRevisionEvidenceCannotBeRewritten(t *testing.T) {
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'guard-tenant','cell-local','guard','ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID)
	membershipID := uuid.New()
	db.Exec(t, `INSERT INTO agent_current_population (tenant_id,membership_id,subject_ref,population_id,source_identity,revision,effective_from) VALUES ($1,$2,'human-1','guard-pop','directory',1,CURRENT_TIMESTAMP)`, tenantID, membershipID)
	if err := db.ExecErr(`UPDATE agent_current_population SET population_id='forged-pop' WHERE tenant_id=$1 AND membership_id=$2`, tenantID, membershipID); err == nil {
		t.Fatal("population identity rewrite succeeded")
	}
	if err := db.ExecErr(`DELETE FROM agent_current_population WHERE tenant_id=$1 AND membership_id=$2`, tenantID, membershipID); err == nil {
		t.Fatal("population authority deletion succeeded")
	}
	db.Exec(t, `UPDATE agent_current_population SET superseded_at=CURRENT_TIMESTAMP,superseded_by=$2 WHERE tenant_id=$1 AND membership_id=$3`, tenantID, uuid.New(), membershipID)
}

func TestTodo_AGENT2_027_Race_CurrentPopulationTenantTransactionsStayIsolated(t *testing.T) {
	db := pgtest.New(t)
	tenantA, tenantB := uuid.New(), uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'race-a','cell-local','a','ACTIVE',timestamptz '2026-01-01T00:00:00Z'),($2,'race-b','cell-local','b','ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantA, tenantB)
	db.Exec(t, `INSERT INTO agent_current_population (tenant_id,membership_id,subject_ref,population_id,source_identity,revision,effective_from) VALUES ($1,$3,'human-1','race-a-pop','directory',1,CURRENT_TIMESTAMP),($2,$4,'human-1','race-b-pop','directory',1,CURRENT_TIMESTAMP)`, tenantA, tenantB, uuid.New(), uuid.New())
	type fixture struct {
		key values.TenantId
		id  uuid.UUID
		pop string
	}
	fixtures := []fixture{{"race-a", tenantA, "race-a-pop"}, {"race-b", tenantB, "race-b-pop"}}
	var wg sync.WaitGroup
	for _, fixture := range fixtures {
		fixture := fixture
		wg.Add(1)
		go func() {
			defer wg.Done()
			app := appRoleConnForPopulation(t, db)
			d := NewAgentDirectoryDB(app, func(values.TenantId) uuid.UUID { return fixture.id })
			for range 8 {
				got, err := d.CurrentPopulation(context.Background(), fixture.key, "human-1")
				if err != nil || got != fixture.pop {
					t.Errorf("CurrentPopulation = %q, %v; want %s", got, err, fixture.pop)
				}
			}
		}()
	}
	wg.Wait()
}

func appRoleConnForPopulation(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume app role: %v", err)
	}
	return conn
}
