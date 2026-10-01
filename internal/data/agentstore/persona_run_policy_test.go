package agentstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_AGENT_015_Integration_CurrentPersonaRunPolicyIsScopedAndUnique(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatalf("migrate agent store: %v", err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO tenant(tenant_id) VALUES ($1),($2)`, tenantA, tenantB); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO persona_run_policy
		(tenant_id,legal_entity_id,revision,effective_from,effective_until,max_cost_micros,max_input_tokens,max_output_tokens,max_run_duration_ms)
		VALUES ($1,'entity-a',1,$2,NULL,700,8000,1500,90000),
		       ($3,'entity-a',1,$2,NULL,70,800,150,9000)`, tenantA, now.Add(-time.Hour), tenantB); err != nil {
		t.Fatal(err)
	}
	login, password := roleName("persona_policy"), uuid.NewString()
	if err := createAgentLogin(ctx, db.SQL, login, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.SQL.ExecContext(context.Background(), "DROP ROLE "+login); err != nil {
			t.Errorf("drop test role: %v", err)
		}
	})
	app, err := New(ctx, Config{DSN: testDSN(t, db.URL, db.Schema, login, password, "postgres"), CoreDSN: testDSN(t, db.URL, "", roleName("core"), "unused", "core"), MaxConns: 1})
	if err != nil {
		t.Fatalf("open app store: %v", err)
	}
	t.Cleanup(app.Close)
	policy, err := app.CurrentPersonaRunPolicy(ctx, tenantA, "entity-a", now)
	if err != nil {
		t.Fatalf("read current policy: %v", err)
	}
	if policy.TenantID != tenantA || policy.LegalEntityID != "entity-a" || policy.Revision != 1 || policy.MaxCostMicros != 700 || policy.MaxInputTokens != 8000 || policy.MaxOutputTokens != 1500 || policy.MaxRunDuration != 90*time.Second {
		t.Fatalf("policy = %+v, want tenant A's effective immutable policy", policy)
	}
	if _, err := app.CurrentPersonaRunPolicy(ctx, tenantA, "missing", now); !errors.Is(err, ErrPersonaRunPolicyNotFound) {
		t.Fatalf("missing current policy error = %v", err)
	}
	if _, err := app.CurrentPersonaRunPolicy(ctx, tenantA, "entity-a", now.Add(-2*time.Hour)); !errors.Is(err, ErrPersonaRunPolicyNotFound) {
		t.Fatalf("before-effective policy error = %v", err)
	}
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO persona_run_policy
		(tenant_id,legal_entity_id,revision,effective_from,effective_until,max_cost_micros,max_input_tokens,max_output_tokens,max_run_duration_ms)
		VALUES ($1,'entity-a',2,$2,NULL,1,1,1,1)`, tenantA, now.Add(-30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CurrentPersonaRunPolicy(ctx, tenantA, "entity-a", now); !errors.Is(err, ErrPersonaRunPolicyAmbiguous) {
		t.Fatalf("overlap error = %v, want ambiguous current policy", err)
	}
	if _, err := app.CurrentPersonaRunPolicy(ctx, tenantA, "entity-a", time.Time{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("zero effective time error = %v, want invalid config", err)
	}
	assertPersonaRunPolicyTenantIsolation(t, app, tenantB)
}

func assertPersonaRunPolicyTenantIsolation(t *testing.T, store *Store, tenantB uuid.UUID) {
	t.Helper()
	var count int
	err := store.RunTenantTx(context.Background(), tenantB, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM persona_run_policy`).Scan(&count)
	})
	if err != nil {
		t.Fatalf("read tenant B policy count: %v", err)
	}
	if count != 1 {
		t.Fatalf("tenant B sees %d current policy rows, want only its own row", count)
	}
}
