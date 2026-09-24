package queryplans_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/queryplans"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

// insertTenant registers one active tenant and returns its identifier. It
// mirrors internal/data/aggregates/fixtures_test.go's own helper of the same
// shape (see that file's doc comment for why the duplication is deliberate
// rather than a shared import).
func insertTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, "tenant-"+id.String()[:8], "Tenant "+id.String()[:8])
	return id
}

// asAppRole opens a fresh connection scoped to hcmnext_app under tenant, the
// same way a request-scoped connection reaches these tables in production
// (migrations/00008_tenant_isolation.sql's row level security policies are
// enforced for hcmnext_app, never for the migration-running superuser
// db.Conn otherwise uses).
func asAppRole(t *testing.T, db *pgtest.DB, tenant uuid.UUID) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	ctx := context.Background()
	if _, err := conn.Exec(ctx, "SET ROLE hcmnext_app"); err != nil {
		t.Fatalf("SET ROLE hcmnext_app: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('app.tenant_id', $1, false)`, tenant.String()); err != nil {
		t.Fatalf("set app.tenant_id: %v", err)
	}
	return conn
}

func dumpPlan(t *testing.T, plan queryplans.PlanNode) string {
	t.Helper()
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("marshal plan for a failure message: %v", err)
	}
	return string(b)
}

// TestTodo_DB_020 is DB-020's PRIMARY test: every declared critical query,
// seeded to its own declared row threshold, proves no sequential scan on the
// table it names and uses the index the catalogue declares -- captured under
// hcmnext_app with row level security enforced, exactly as production reaches
// these tables.
func TestTodo_DB_020(t *testing.T) {
	// Subtests run sequentially, not with t.Parallel(): db.Conn is one
	// connection (pgtest.DB's own doc comment says as much), and Prepare's
	// seeding runs on it, so two subtests seeding concurrently would race
	// for that one connection rather than proving anything about plans.
	db := pgtest.New(t)
	for _, e := range queryplans.Catalog() {
		e := e
		t.Run(e.Name, func(t *testing.T) {
			tenant := insertTenant(t, db)
			app := asAppRole(t, db, tenant)
			proof, err := queryplans.Prove(context.Background(), db.Conn, app, tenant, e)
			if err != nil {
				t.Fatalf("Prove(%s): %v", e.Name, err)
			}
			if proof.SeqScanOnTable {
				t.Errorf("%s (%s): plan sequentially scans %s at %d seeded rows\nstatement: %s\nplan:\n%s",
					e.Name, e.Owner, e.Table, e.RowThreshold, proof.Call.SQL, dumpPlan(t, proof.Plan))
			}
			if !proof.UsesExpectedIndex {
				t.Errorf("%s (%s): plan uses no index among %v\nstatement: %s\nplan:\n%s",
					e.Name, e.Owner, e.ExpectedIndexSubstrings, proof.Call.SQL, dumpPlan(t, proof.Plan))
			}
		})
	}
}

// TestTodo_DB_020_Integration proves an entry's proof is repeatable within
// one schema rather than an artifact of being the first and only tenant
// Prepare ever seeded: it runs [queryplans.Prove] twice for two independent
// tenants, each with its own [seedWithDecoys]-shaped population, and
// requires both runs to hold. A regression that made an entry's Prepare
// depend on being called exactly once per schema (a fixture keyed only by a
// package-level constant, say) would pass TestTodo_DB_020 alone and fail
// here.
func TestTodo_DB_020_Integration(t *testing.T) {
	db := pgtest.New(t)
	for _, e := range queryplans.Catalog() {
		e := e
		t.Run(e.Name, func(t *testing.T) {
			ctx := context.Background()
			for run := 0; run < 2; run++ {
				tenant := insertTenant(t, db)
				app := asAppRole(t, db, tenant)
				proof, err := queryplans.Prove(ctx, db.Conn, app, tenant, e)
				if err != nil {
					t.Fatalf("run %d: Prove(%s): %v", run, e.Name, err)
				}
				if !proof.OK() {
					t.Errorf("run %d: %s: proof failed (seq scan on %s = %v, uses expected index = %v)\nplan:\n%s",
						run, e.Name, e.Table, proof.SeqScanOnTable, proof.UsesExpectedIndex, dumpPlan(t, proof.Plan))
				}
			}
		})
	}
}

// TestTodo_DB_020_Security proves row level security, not just index shape:
// a tenant with no rows of its own must see zero rows from a table another
// tenant just seeded, through the same hcmnext_app role the plans above were
// captured under. A regression here (a grant that leaked SELECT past RLS, a
// FORCE ROW LEVEL SECURITY dropped from one of these tables) would let the
// "no seq scan" proof above pass while the query itself answered the wrong
// tenant's data -- which is the failure this test exists to catch that the
// plan shape alone cannot.
func TestTodo_DB_020_Security(t *testing.T) {
	db := pgtest.New(t)
	for _, e := range queryplans.Catalog() {
		e := e
		t.Run(e.Name, func(t *testing.T) {
			ctx := context.Background()

			tenantA := insertTenant(t, db)
			// A small seed is enough here: this test proves isolation, not
			// scale, which TestTodo_DB_020 and its Integration sibling
			// already cover.
			if _, err := e.Prepare(ctx, db.Conn, tenantA, 50); err != nil {
				t.Fatalf("seed tenant A: %v", err)
			}

			tenantB := insertTenant(t, db)
			appB := asAppRole(t, db, tenantB)
			var count int64
			if err := appB.QueryRow(ctx, "SELECT count(*) FROM "+e.Table).Scan(&count); err != nil {
				t.Fatalf("count %s as tenant B: %v", e.Table, err)
			}
			if count != 0 {
				t.Errorf("%s: tenant B's hcmnext_app connection sees %d row(s) of %s seeded for tenant A; "+
					"row level security is not isolating this table", e.Name, count, e.Table)
			}
		})
	}
}

// TestTodo_DB_020_Golden compares every entry's plan shape -- node types and
// index names only, per plan.go's [queryplans.Shape] -- against
// testdata/plan_shapes.golden.json. Run with UPDATE_QUERYPLANS_GOLDEN=1 set
// to regenerate the file after a deliberate index or query change.
func TestTodo_DB_020_Golden(t *testing.T) {
	db := pgtest.New(t)
	shapes := map[string]queryplans.Shape{}
	for _, e := range queryplans.Catalog() {
		tenant := insertTenant(t, db)
		app := asAppRole(t, db, tenant)
		proof, err := queryplans.Prove(context.Background(), db.Conn, app, tenant, e)
		if err != nil {
			t.Fatalf("Prove(%s): %v", e.Name, err)
		}
		shapes[e.Name] = proof.Plan.Shape().Normalized()
	}

	got, err := json.MarshalIndent(shapes, "", "  ")
	if err != nil {
		t.Fatalf("marshal plan shapes: %v", err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "plan_shapes.golden.json")
	if os.Getenv("UPDATE_QUERYPLANS_GOLDEN") != "" {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", golden, err)
		}
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read %s: %v (run with UPDATE_QUERYPLANS_GOLDEN=1 to create it)", golden, err)
	}
	if strings.TrimSpace(string(want)) != strings.TrimSpace(string(got)) {
		t.Errorf("plan shapes differ from %s.\n--- got ---\n%s\n--- want ---\n%s", golden, got, want)
	}
}
