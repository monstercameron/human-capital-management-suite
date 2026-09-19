package budgetfacts_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/budgetfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/budget"
	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "budgetfacts test tenant "+key)
	return id
}

// seedPool stores the DEMO/2099 compensation pool at 95000.00 available
// against baseline v1 and returns a reader resolving to its tenant.
func seedPool(t *testing.T, db *pgtest.DB, tenantID uuid.UUID) budgetfacts.Reader {
	t.Helper()
	recorded := time.Date(2099, 1, 2, 9, 0, 0, 0, time.UTC)
	pool, err := aggregates.NewWorkforceBudget(tenantID, demoworkforce.BudgetID("DEMO"),
		time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), nil, recorded,
		"COMPENSATION_POOL", "DEMO", "DEMO", "2099", "USD", "MONEY", "95000.00", "baseline:v1")
	if err != nil {
		t.Fatalf("NewWorkforceBudget: %v", err)
	}
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	if _, err := (aggregates.CompensationStore{}).PutWorkforceBudget(ctx, tx, pool); err != nil {
		t.Fatalf("PutWorkforceBudget: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return budgetfacts.Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }}
}

func query(tenant values.TenantId, asOf time.Time) promosnapshot.BudgetQuery {
	return promosnapshot.BudgetQuery{
		Tenant: tenant,
		Scope:  "DEMO",
		Period: "2099",
		AsOf:   values.NewInstant(asOf),
	}
}

// The scoped pool reads back: the ref's available quantity is the stored
// row's own figure, not a recomputation.
func TestTodo_Unit4_SeeksPoolHeldAgainstScope(t *testing.T) {
	db := pgtest.New(t)
	tenant := values.TenantId("hcm-health-pilot")
	reader := seedPool(t, db, insertTenant(t, db, "hcm-health-pilot"))

	ref, ok, err := reader.CompensationBudgetAt(context.Background(), query(tenant, time.Date(2099, 6, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("scoped pool is present, want the reader to find it")
	}
	if ref.BudgetType != budget.CompensationPool || ref.Unit != budget.UnitMoney {
		t.Fatalf("pool instrument mismatch: %+v", ref)
	}
	if ref.Scope != "DEMO" || ref.Period != "2099" || ref.BaselineVersion != "baseline:v1" {
		t.Fatalf("pool identity mismatch: %+v", ref)
	}
	if got := ref.AvailableQuantity.String(); got != "95000.0000" {
		t.Fatalf("available quantity = %q, want the row's own 95000.00 at stored scale", got)
	}
	if ref.Evidence.ObservationID == "" || ref.Evidence.Digest == "" {
		t.Fatalf("pool evidence is not the row's own currency: %+v", ref.Evidence)
	}
}

// Another scope holds no pool: withheld, not coerced into DEMO's.
func TestTodo_Unit4_NoPoolAtScopeIsWithheld(t *testing.T) {
	db := pgtest.New(t)
	tenant := values.TenantId("hcm-health-pilot")
	reader := seedPool(t, db, insertTenant(t, db, "hcm-health-pilot"))

	q := query(tenant, time.Now().UTC())
	q.Scope = "OTHER"
	_, ok, err := reader.CompensationBudgetAt(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("no pool at OTHER scope: want withhold, not a coerced neighbor")
	}
}

// A pool of another instrument at the same scope is not this pool
// misread: the money compensation pool is still absent.
func TestTodo_Unit4_OtherInstrumentIsNotThePool(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "hcm-other-instrument")
	recorded := time.Date(2099, 1, 2, 9, 0, 0, 0, time.UTC)
	headcount, err := aggregates.NewWorkforceBudget(tenantID, demoworkforce.BudgetID("DEMO"),
		time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), nil, recorded,
		"HEADCOUNT_CAPACITY", "DEMO", "DEMO", "2099", "", "FTE", "10.0000", "baseline:v1")
	if err != nil {
		t.Fatalf("NewWorkforceBudget: %v", err)
	}
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	if _, err := (aggregates.CompensationStore{}).PutWorkforceBudget(ctx, tx, headcount); err != nil {
		t.Fatalf("PutWorkforceBudget: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	reader := budgetfacts.Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }}

	if _, ok, err := reader.CompensationBudgetAt(context.Background(), query("hcm-other", time.Date(2099, 6, 1, 0, 0, 0, 0, time.UTC))); err != nil || ok {
		t.Fatalf("headcount pool at scope: err=%v ok=%v, want the money pool withheld", err, ok)
	}
}

// A tenant the resolver knows nothing about reads nothing.
func TestTodo_Unit4_UnknownTenantReadsNothing(t *testing.T) {
	db := pgtest.New(t)
	seedPool(t, db, insertTenant(t, db, "hcm-health-pilot"))
	reader := budgetfacts.Reader{DB: db.Conn, TenantUUID: func(values.TenantId) uuid.UUID { return uuid.Nil }}

	if _, ok, err := reader.CompensationBudgetAt(context.Background(), query("hcm-health-pilot", time.Now().UTC())); err != nil || ok {
		t.Fatalf("unmapped tenant: err=%v ok=%v, want clean withhold", err, ok)
	}
}

// A reader with no database refuses to invent a budget.
func TestTodo_Unit4_ReaderWithoutDatabaseRefuses(t *testing.T) {
	if _, _, err := (budgetfacts.Reader{}).CompensationBudgetAt(context.Background(), query("t", time.Now().UTC())); err == nil {
		t.Fatal("database-less reader must error, not return an absent budget")
	}
}
