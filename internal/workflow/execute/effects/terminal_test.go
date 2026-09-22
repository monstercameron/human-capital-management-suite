package effects

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTerminal_Smoke(t *testing.T) {
	if t == nil {
		t.Fatalf("nil tester")
	}
}

func TestTerminal_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

// registerInTx runs one EnsureOutcomeSchema in its own tenant-scoped
// transaction on conn and commits it, returning the first failure. The
// registration itself lives in the promotion settlement capability; this
// helper exercises it through the same tenant-scoped path the terminal
// effect uses.
func registerInTx(ctx context.Context, conn *pgxadapter.Conn, tenantID uuid.UUID, schemaRef string, gate <-chan struct{}) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return fmt.Errorf("scope tenant: %w", err)
	}
	if gate != nil {
		<-gate
	}
	if err := promotioncommit.EnsureOutcomeSchema(ctx, tx, tenantID, schemaRef); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// TestEnsurePayloadSchemaConcurrentFirstRegistration is the regression for the
// intermittent TestTodo_SVC_004_Race failure: replicas completing their first
// workflow instances for one tenant at the same instant each register the
// outcome payload schema. payload_schema carries two unique constraints (the
// (tenant_id, schema_ref) key and payload_schema_version_unique). An
// ON CONFLICT naming only the first arbitrates only that one, so two inserts
// that both pass PostgreSQL's pre-check raise a unique violation on the second
// constraint, abort the terminal advance, and -- through the scheduler -- turn
// one logical execution into a redispatch loop. Every concurrent registration
// must instead observe the row the winner committed.
//
// The collision needs two inserts to pass the conflict pre-check together, so
// the test opens every transaction first and releases the inserts through one
// gate, over enough rounds (a fresh schema ref each) that the old statement
// fails reliably.
func TestEnsurePayloadSchemaConcurrentFirstRegistration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'effects-schema-race', 'cell-local', 'tenant effects-schema-race', 'ACTIVE', $2)`,
		tenantID, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC))

	const sessions, rounds = 6, 40
	conns := make([]*pgxadapter.Conn, sessions)
	for i := range conns {
		conns[i] = db.NewConn(t)
		if _, err := conns[i].Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
			t.Fatalf("assume %s: %v", tenancy.AppRole, err)
		}
	}

	for round := 0; round < rounds; round++ {
		schemaRef := fmt.Sprintf("hcmnext.test.effects.SchemaRace%d/v1", round)
		gate := make(chan struct{})
		errs := make([]error, sessions)
		var wg sync.WaitGroup
		for i, conn := range conns {
			wg.Add(1)
			go func(i int, conn *pgxadapter.Conn) {
				defer wg.Done()
				errs[i] = registerInTx(ctx, conn, tenantID, schemaRef, gate)
			}(i, conn)
		}
		close(gate)
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d session %d: concurrent registration failed instead of observing the committed row: %v",
					round, i, err)
			}
		}
		var rows int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM payload_schema WHERE tenant_id = $1 AND schema_ref = $2`,
			tenantID, schemaRef).Scan(&rows); err != nil {
			t.Fatalf("count payload schemas: %v", err)
		}
		if rows != 1 {
			t.Fatalf("round %d: %d payload_schema rows after %d concurrent registrations, want exactly 1", round, rows, sessions)
		}
	}

	// A later registration against a settled row is still a no-op.
	if err := registerInTx(ctx, conns[0], tenantID, "hcmnext.test.effects.SchemaRace0/v1", nil); err != nil {
		t.Fatalf("repeat registration: %v", err)
	}
}
