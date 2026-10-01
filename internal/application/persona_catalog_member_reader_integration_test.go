package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_018_CoreMemberReaderResolvesDurableChatWorkerKey(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	const tenant = "ironridge-demo"
	store, err := pgstore.New(pool, pgstore.WithCellID("cell-persona-member-test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Bootstrap(ctx, tenant); err != nil {
		t.Fatalf("register tenant: %v", err)
	}
	if _, _, err := bootstrapLocalDevWorkforce(ctx, pool, tenant); err != nil {
		t.Fatalf("seed durable workforce: %v", err)
	}
	pack, ok := demoworkforce.PackFor(tenant)
	if !ok {
		t.Fatalf("no demo workforce pack for tenant %q", tenant)
	}
	employees, err := pack.Plan(pgstore.TenantID(tenant))
	if err != nil || len(employees) == 0 {
		t.Fatalf("demo employees = %d, err = %v", len(employees), err)
	}
	// Chat seeding derives membership subjects directly from Plan, so use the
	// same source of truth rather than duplicating the roster's generated key.
	workerKey := employees[0].Row.WorkerKey
	var want string
	for _, employee := range employees {
		if employee.Row.WorkerKey == workerKey {
			want = employee.Row.PreferredName
			if want == "" {
				want = employee.Row.LegalName
			}
		}
	}
	if want == "" {
		t.Fatalf("demo fixture has no named worker %q", workerKey)
	}

	// Production catalog wiring layers this same authoritative store behind
	// the cell's corpus reader. The membership identity is the exact WorkerKey;
	// no chat label is supplied to the reader.
	primary, err := fixtures.NewMemoryWorkerFacts()
	if err != nil {
		t.Fatalf("load core worker corpus: %v", err)
	}
	workers := workforce.NewLayeredWorkerFacts(primary, pool, tenantKeyMapper[values.TenantId](pgstore.TenantID))
	identity := personaCatalogWorkforceIdentity{facts: workforce.NewFacts(pool, tenantKeyMapper[values.TenantId](pgstore.TenantID))}
	reader, err := NewCorePersonaCatalogMemberReaderWithIdentity(workers, identity, func() time.Time {
		return time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := reader.ResolvePersonaCatalogMember(ctx, values.TenantId(tenant), workerKey)
	if err != nil {
		t.Fatalf("resolve durable chat worker key: %v", err)
	}
	if got.TenantID != values.TenantId(tenant) || got.SubjectID != workerKey || got.Label != want {
		t.Fatalf("resolved member = %+v, want tenant=%q subject=%q label=%q", got, tenant, workerKey, want)
	}
}
