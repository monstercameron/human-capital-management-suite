package app_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PROMOUX-015: the served worker directory reads in a bounded number of
// statements, and offers only people this tenant actually has.
//
// Two defects sat in the same read. It resolved one committed placement per
// worker and validated one position per vacancy, each in its own round trip,
// so p95 on the sixty-worker demo tenant was 2.45s against a 750ms budget;
// and it appended the release's fixed four-worker corpus to every tenant's
// own population, so the product showed sixty-four people where sixty exist.

// promoux015ReadHarness is a composed cell over a real PostgreSQL database,
// with the statement recorder rev09002 already uses.
type promoux015ReadHarness struct {
	engine   workspace.JourneyEngine
	ctx      context.Context
	recorder *rev09002Recorder
	pool     *pgxadapter.Pool
	tenant   values.TenantId
	tenantID uuid.UUID
}

func promoux015ReadCell(t *testing.T) *promoux015ReadHarness {
	t.Helper()
	return promoux015ReadCellWith(t, true)
}

// promoux015ReadCellWith composes the cell with or without the physical
// tenant mapping its durable population is keyed by. Without it the journey
// has no created population to read at all -- the same branch a cell composed
// with no execution database takes, and the one the corpus fallback exists
// for. (The mapping is omitted rather than the database itself because a cell
// with no execution database publishes no journey engine to ask.)
func promoux015ReadCellWith(t *testing.T, tenantMapping bool) *promoux015ReadHarness {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}
	if err := store.Bootstrap(context.Background(), string(fixtures.Tenant)); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	recorder := &rev09002Recorder{}
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	cfg := app.CellConfig{
		Store:    store,
		Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return nil, nil }),
		Audience: "hcm-next-api",
		Executor: promoux012NoExecutor{},
		Now:      func() time.Time { return now },
	}
	cfg.ExecutionDB = rev09002Beginner{inner: pool, recorder: recorder}
	if tenantMapping {
		cfg.TenantUUID = func(tenant values.TenantId) uuid.UUID { return pgstore.TenantID(string(tenant)) }
	}
	cell, err := app.NewCell(cfg)
	if err != nil {
		t.Fatalf("NewCell: %v", err)
	}
	return &promoux015ReadHarness{
		engine: cell.Journey, ctx: trust.WithPrincipal(context.Background(), promoux015ReadPrincipal(t, now)),
		recorder: recorder, pool: pool, tenant: fixtures.Tenant, tenantID: pgstore.TenantID(string(fixtures.Tenant)),
	}
}

func promoux015ReadPrincipal(t *testing.T, now time.Time) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: fixtures.Tenant, Subject: "principal:promoux015-reader", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID:  "org-north-america",
		Roles:                []string{"intent_author", "comp_admin"},
		AuthorityRefs:        []string{"authority:position:vp-people"},
		Purposes:             []string{"compensation_review"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-promoux015-read", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		CredentialDigest: "credential-digest-promoux015-read",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

// seedOrganizationAndPositions writes the demo organization and the promotion
// catalog, which is where the OPEN positions the vacancy list validates come
// from. It returns how many positions the tenant now publishes.
func (h *promoux015ReadHarness) seedOrganizationAndPositions(t *testing.T) int {
	t.Helper()
	ctx := context.Background()
	catalog, err := app.PromotionAggregateCatalog(time.Date(2026, time.September, 1, 13, 45, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("PromotionAggregateCatalog: %v", err)
	}
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := demoworkforce.SeedOrganization(ctx, tx, h.tenantID); err != nil {
		t.Fatalf("SeedOrganization: %v", err)
	}
	if _, err := demoworkforce.SeedAggregateCatalog(ctx, tx, h.tenantID, catalog); err != nil {
		t.Fatalf("SeedAggregateCatalog: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
	var positions int
	if err := h.pool.QueryRow(ctx,
		`SELECT count(*) FROM job_position WHERE tenant_id = $1 AND superseded_at IS NULL`, h.tenantID).Scan(&positions); err != nil {
		t.Fatalf("count positions: %v", err)
	}
	return positions
}

// seedDemoPopulation writes the sixty-worker HarborCare population and
// projects every row into the aggregates, exactly as the served cell's own
// bootstrap does.
func (h *promoux015ReadHarness) seedDemoPopulation(t *testing.T) []workforce.WorkerRow {
	t.Helper()
	ctx := context.Background()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin population seed: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := demoworkforce.Seed(ctx, tx, h.tenantID); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	rows, err := (workforce.Store{}).List(ctx, tx, h.tenantID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, row := range rows {
		if _, err := demoworkforce.ProjectWorker(ctx, tx, row, demoworkforce.HarborCare.LegalEntity); err != nil {
			t.Fatalf("ProjectWorker(%s): %v", row.WorkerKey, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit population seed: %v", err)
	}
	return rows
}

// addWorkers appends n synthetic durable workers and projects them, so a test
// can grow the population without reseeding anything else.
func (h *promoux015ReadHarness) addWorkers(t *testing.T, prefix string, n int) {
	t.Helper()
	ctx := context.Background()
	recorded := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin add workers: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for i := 0; i < n; i++ {
		short := fmt.Sprintf("%s%03d", prefix, i)
		row := workforce.WorkerRow{
			TenantID: h.tenantID, WorkerID: uuid.New(), WorkerKey: "px15-" + short,
			LegalName: "Px15 Worker " + short, PreferredName: "Px15", WorkerNumber: "W-PX15-" + short,
			WorkerType: "employee", LifecycleStatus: "active", EmploymentID: "emp_px15" + short, AssignmentID: "asg_px15" + short,
			JobCode: "OPS-HRBP2", JobTitle: "People Partner", Grade: "P2", OrgUnit: "people-ops",
			PositionID: "POS-PX15-" + short, Location: "Boston, MA", PayZone: "US-EAST", FTE: "1.0000",
			HireDate: "2021-04-05", EffectiveFrom: "2021-04-05", BasePay: "90000.00", Currency: "USD",
			PayBasis: "ANNUAL_SALARY", BonusTarget: "0.0500", ManagerRelationshipRef: "rel_mgr_px15" + short,
			RevisionStream: "people.worker.px15" + short, RevisionSequence: 1,
			KnownAt: recorded, RecordedAt: recorded, CreatedBy: "test", Source: workforce.SourceCreated,
		}
		if _, err := (workforce.Store{}).Create(ctx, tx, row); err != nil {
			t.Fatalf("create %s: %v", row.WorkerKey, err)
		}
		if _, err := demoworkforce.ProjectWorker(ctx, tx, row, demoworkforce.HarborCare.LegalEntity); err != nil {
			t.Fatalf("project %s: %v", row.WorkerKey, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit add workers: %v", err)
	}
}

// list reads the directory with the statement recorder on.
func (h *promoux015ReadHarness) list(t *testing.T) ([]workspace.WorkerSummary, workspace.WorkforceOptions, []string) {
	t.Helper()
	var (
		workers []workspace.WorkerSummary
		options workspace.WorkforceOptions
	)
	statements := h.recorder.capture(func() {
		var err error
		workers, options, err = h.engine.ListWorkers(h.ctx)
		if err != nil {
			t.Fatalf("ListWorkers: %v", err)
		}
	})
	return workers, options, statements
}

// TestTodo_PROMOUX_015_Workforce is the PRIMARY case: a tenant with its own
// durable population is served exactly that population, and no corpus worker
// appears anywhere in it.
func TestTodo_PROMOUX_015_Workforce(t *testing.T) {
	h := promoux015ReadCell(t)
	h.seedOrganizationAndPositions(t)
	rows := h.seedDemoPopulation(t)
	if len(rows) != demoworkforce.NewWorkerCount {
		t.Fatalf("seeded %d workers, want %d", len(rows), demoworkforce.NewWorkerCount)
	}

	workers, _, _ := h.list(t)
	if len(workers) != len(rows) {
		t.Fatalf("the directory lists %d people for a tenant with %d workers", len(workers), len(rows))
	}
	durable := map[string]bool{}
	for _, row := range rows {
		durable[row.WorkerKey] = true
	}
	corpus, err := fixtures.Workers()
	if err != nil {
		t.Fatalf("fixtures.Workers: %v", err)
	}
	phantom := map[string]bool{}
	for _, profile := range corpus {
		phantom[profile.Key] = true
	}
	if len(phantom) == 0 {
		t.Fatal("the corpus is empty: this test would pass vacuously")
	}
	for _, w := range workers {
		if w.Source == workspace.WorkerSourceCorpus {
			t.Errorf("the directory offers the corpus worker %q, who is in no table of this tenant", w.WorkerRef)
		}
		if phantom[w.WorkerRef] {
			t.Errorf("the directory offers %q, a corpus reference rather than one of this tenant's own", w.WorkerRef)
		}
		if !durable[w.WorkerRef] {
			t.Errorf("the directory offers %q, which is not a durable worker of this tenant", w.WorkerRef)
		}
	}
}

// TestTodo_PROMOUX_015_Workforce_Fallback is the other half of the same rule:
// a cell that has no population to serve still serves the corpus, because a
// read-only cell whose directory answered "nobody works here" would be worse
// than one that offers the release's reference population.
func TestTodo_PROMOUX_015_Workforce_Fallback(t *testing.T) {
	corpus, err := fixtures.Workers()
	if err != nil {
		t.Fatalf("fixtures.Workers: %v", err)
	}

	t.Run("a tenant that has created nobody", func(t *testing.T) {
		h := promoux015ReadCell(t)
		h.seedOrganizationAndPositions(t)
		workers, _, _ := h.list(t)
		if len(workers) != len(corpus) {
			t.Fatalf("an empty tenant's directory lists %d people, want the %d-worker corpus", len(workers), len(corpus))
		}
		for _, w := range workers {
			if w.Source != workspace.WorkerSourceCorpus {
				t.Errorf("%q is listed as %q, want CORPUS", w.WorkerRef, w.Source)
			}
		}
	})

	t.Run("a cell that cannot reach a durable population", func(t *testing.T) {
		h := promoux015ReadCellWith(t, false)
		// The tenant's own rows exist; this cell has no physical tenant
		// mapping to read them through, which is the read-only composition's
		// own branch. It serves the corpus rather than an empty directory or
		// a refusal.
		h.seedOrganizationAndPositions(t)
		h.seedDemoPopulation(t)
		workers, _, err := h.engine.ListWorkers(h.ctx)
		if err != nil {
			t.Fatalf("ListWorkers on a read-only cell: %v", err)
		}
		if len(workers) != len(corpus) {
			t.Fatalf("a cell with no durable population lists %d people, want the %d-worker corpus", len(workers), len(corpus))
		}
		for _, w := range workers {
			if w.Source != workspace.WorkerSourceCorpus {
				t.Errorf("%q is listed as %q, want CORPUS", w.WorkerRef, w.Source)
			}
		}
	})
}

// TestTodo_PROMOUX_015_Workforce_Benchmark is the PERFORMANCE case: the
// number of execution-database statements the directory issues does not grow
// with the number of people in it, and stays far below the number of
// positions the vacancy list validates.
func TestTodo_PROMOUX_015_Workforce_Benchmark(t *testing.T) {
	h := promoux015ReadCell(t)
	positions := h.seedOrganizationAndPositions(t)
	if positions < 20 {
		t.Fatalf("the seeded catalog publishes %d positions; the vacancy half of this test proves nothing", positions)
	}

	h.addWorkers(t, "a", 3)
	few, _, small := h.list(t)
	h.addWorkers(t, "b", 27)
	many, _, large := h.list(t)

	t.Logf("%d positions: %d workers cost %d statements, %d workers cost %d",
		positions, len(few), len(small), len(many), len(large))
	if len(few) != 3 || len(many) != 30 {
		t.Fatalf("listed %d then %d workers, want 3 then 30", len(few), len(many))
	}
	if len(large) != len(small) {
		t.Fatalf("listing %d workers issued %d statements and listing %d issued %d: the read is O(N)\n%s",
			len(many), len(large), len(few), len(small), strings.Join(large, "\n---\n"))
	}
	// The whole read: the tenant's ladder, the position directory, the one
	// batched position preload and the one batched placement resolution, plus
	// each transaction's own tenant scoping. It is a fixed plan, so a
	// regression that reintroduced a per-row read shows up here immediately.
	if limit := 24; len(large) > limit {
		t.Fatalf("ListWorkers issued %d statements, want at most %d:\n%s", len(large), limit, strings.Join(large, "\n---\n"))
	}
	if len(large) >= positions {
		t.Fatalf("ListWorkers issued %d statements for %d positions: the vacancy validation is still per position",
			len(large), positions)
	}
}
