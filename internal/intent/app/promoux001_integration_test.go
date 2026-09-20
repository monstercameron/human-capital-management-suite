package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workeridstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_PROMOUX_001_Integration reaches a real PostgreSQL server: it
// seeds the demo company's workforce (the same demoworkforce.SeedOrganization
// + demoworkforce.Seed pair cmd/migrate's demo-people subcommand runs) and
// then reads the durable rows back, proving two things the RED clause names
// together.
//
//  1. The seeding path itself works end to end against a real store: this is
//     the DB-write half of PROMOUX-001's reported demo-people failure (the
//     photo-publish half is covered by cmd/migrate's own
//     TestIngestDemoPhotosSkipsAlreadyPublishedProxy).
//  2. Every one of the resulting durable workers resolves its promotion
//     eligibility through the SAME job-architecture catalog workforceOptions
//     publishes to the fixed four-worker corpus -- proving GREEN #3 (one
//     worker identity and job-architecture model, not two that disagree)
//     against the real read path rather than against the pure
//     demoworkforce.PromotionPaths function in isolation.
func TestTodo_PROMOUX_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, "promoux-001-it", "PROMOUX-001 integration tenant")

	seedTx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed transaction: %v", err)
	}
	if _, err := demoworkforce.SeedOrganization(ctx, seedTx, tenantID); err != nil {
		_ = seedTx.Rollback(ctx)
		t.Fatalf("SeedOrganization: %v", err)
	}
	summary, err := demoworkforce.Seed(ctx, seedTx, tenantID)
	if err != nil {
		_ = seedTx.Rollback(ctx)
		t.Fatalf("Seed: %v", err)
	}
	if err := seedTx.Commit(ctx); err != nil {
		t.Fatalf("commit seed transaction: %v", err)
	}
	if summary.Inserted != demoworkforce.NewWorkerCount || summary.Skipped != 0 {
		t.Fatalf("seed summary = %+v, want %d fresh inserts and zero skips on an empty tenant", summary, demoworkforce.NewWorkerCount)
	}

	// A second run against the same tenant must be a pure replay: this is
	// the reproducibility half of PROMOUX-001's brief ("re-running it
	// against an existing asset must not fail").
	replayTx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin replay transaction: %v", err)
	}
	replaySummary, err := demoworkforce.Seed(ctx, replayTx, tenantID)
	if err != nil {
		_ = replayTx.Rollback(ctx)
		t.Fatalf("replay Seed: %v", err)
	}
	if err := replayTx.Commit(ctx); err != nil {
		t.Fatalf("commit replay transaction: %v", err)
	}
	if replaySummary.Inserted != 0 || replaySummary.Skipped != demoworkforce.NewWorkerCount {
		t.Fatalf("replay summary = %+v, want zero inserts and %d skips", replaySummary, demoworkforce.NewWorkerCount)
	}

	readTx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin read transaction: %v", err)
	}
	defer func() { _ = readTx.Rollback(ctx) }()
	rows, err := (workforce.Store{}).List(ctx, readTx, tenantID)
	if err != nil {
		t.Fatalf("workforce.Store.List: %v", err)
	}
	if len(rows) != demoworkforce.NewWorkerCount {
		t.Fatalf("durable workforce rows = %d, want %d", len(rows), demoworkforce.NewWorkerCount)
	}

	corpus, err := fixtures.Workers()
	if err != nil {
		t.Fatalf("fixtures.Workers: %v", err)
	}
	// The RED clause's "64 seeded workers" was the defect, not the contract:
	// ListWorkers used to concatenate the fixed corpus onto every tenant's
	// own durable population, so this tenant's census read sixty-four when
	// sixty people exist. The corpus is a fallback now (see ListWorkers), so
	// the visible census for a seeded tenant is exactly its own seed, and
	// none of its worker keys is a corpus one.
	if got, want := len(rows), demoworkforce.NewWorkerCount; got != want {
		t.Fatalf("demo seed census = %d, want %d", got, want)
	}
	corpusKeys := map[string]bool{}
	for _, profile := range corpus {
		corpusKeys[profile.Key] = true
	}
	for _, row := range rows {
		if corpusKeys[row.WorkerKey] {
			t.Errorf("the demo seed carries the corpus key %q", row.WorkerKey)
		}
	}

	options, err := workforceOptions()
	if err != nil {
		t.Fatalf("workforceOptions: %v", err)
	}
	hasPath := func(jobCode, grade string) bool {
		for _, path := range options.PromotionPaths {
			if path.SourceJobCode == jobCode && path.SourceGrade == grade {
				return true
			}
		}
		return false
	}
	ladderSources := map[string]bool{}
	for _, edge := range demoworkforce.PromotionPaths() {
		ladderSources[edge.SourceJobCode+"|"+edge.SourceGrade] = true
	}
	checked := 0
	for _, row := range rows {
		if !ladderSources[row.JobCode+"|"+row.Grade] {
			// Genuinely at the top of its unit's ladder: no assertion, this
			// is the structural PromotionIneligible case, not a catalog gap.
			continue
		}
		checked++
		if !hasPath(row.JobCode, row.Grade) {
			t.Errorf("durable worker %s (%s/%s) has a published ladder edge but workforceOptions does not carry its promotion path", row.WorkerKey, row.JobCode, row.Grade)
		}
	}
	if checked == 0 {
		t.Fatal("no durable worker exercised a published ladder edge; the assertion above would pass vacuously")
	}
}

// TestTodo_PROMOUX_001_WorkerCreationReservationIsAtomicWithItsWorker is the
// regression the coordinator asked for after independently verifying this
// todo's fixes against the live dev database: `demo-people` reported
// success while the corpus did not exist, because an earlier failed
// creation had already reserved worker numbers that then survived rollback
// while the worker rows that would have used them never did.
// internal/data/workeridstore.Store.Reserve commits its own reservation the
// instant it returns, in a transaction separate from whatever the caller
// does with the number afterward; journeyEngine.CreateWorker's original
// shape (reserve, then separately open a transaction to insert) inherited
// that gap. Since worker_id_reservation is append-only, an orphaned
// reservation can never be released, so every retry against a poisoned
// tenant treats an already-claimed-but-never-used number as proof the
// worker exists and skips it forever.
//
// This test reaches a real PostgreSQL server (pgtest) and drives the actual
// production path -- journeyEngine.newWorkerRowContext followed by
// insertWorker, sharing one transaction, backed by the real
// *workeridstore.Store -- exactly as journeyEngine.CreateWorker itself now
// calls them. It simulates the mid-run failure the coordinator described
// (reservation succeeds, the worker insert that follows it fails) and
// proves: the reservation does not survive that rollback, and a genuine
// retry recovers the same number and actually creates the worker.
func TestTodo_PROMOUX_001_WorkerCreationReservationIsAtomicWithItsWorker(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, "promoux-001-createworker-it", "PROMOUX-001 CreateWorker atomicity tenant")

	// A fixed id source: the first two mints (the pre-existing worker that
	// occupies the identity, and the failing "retry-shaped" attempt that
	// collides with it) return the SAME uuid, forcing the same worker_key
	// and the same primary key -- the deterministic stand-in for "a
	// worker creation attempt fails for a reason that has nothing to do
	// with the reservation itself." The third mint is a fresh id, so the
	// genuine, differently-named retry does not also collide.
	const collidingID = "00000000-0000-7000-8000-0000000000aa"
	const freshID = "00000000-0000-7000-8000-0000000000bb"
	mints := 0
	svc := &IntentService{
		ids: func() (string, error) {
			mints++
			if mints <= 2 {
				return collidingID, nil
			}
			return freshID, nil
		},
		tenantUUID: func(values.TenantId) uuid.UUID { return tenantID },
	}
	store := workeridstore.New(db.Conn, svc.tenantUUID)
	fixedNow := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	engine := newJourneyEngine(svc, db.Conn, "", func() time.Time { return fixedNow }, nil, store)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: fixtures.Tenant, Subject: "user-0191f3c4-atomic", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "org:promoux-001-createworker-it", Roles: []string{"intent_author"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-atomic", Purposes: []string{"hcm_operations"},
		IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour),
		CredentialDigest: "digest:atomic-fixture",
	})
	if err != nil {
		t.Fatalf("trust.NewPrincipal: %v", err)
	}
	options, err := workforceOptions()
	if err != nil {
		t.Fatalf("workforceOptions: %v", err)
	}
	in := workerInputFixture()

	// Step 1: the worker that already, legitimately exists -- created and
	// committed for real, occupying collidingID's identity.
	preTx, err := engine.beginTenant(ctx, principal)
	if err != nil {
		t.Fatalf("begin pre-existing worker: %v", err)
	}
	preRow, err := engine.newWorkerRowContext(ctx, preTx, principal, in, options)
	if err != nil {
		t.Fatalf("build pre-existing worker: %v", err)
	}
	if _, err := engine.insertWorker(ctx, preTx, preRow); err != nil {
		t.Fatalf("insert pre-existing worker: %v", err)
	}
	if err := preTx.Commit(ctx); err != nil {
		t.Fatalf("commit pre-existing worker: %v", err)
	}

	// Step 2: the failing attempt -- same identity (collidingID again), so
	// the reservation succeeds but the insert collides on the primary key
	// (and the worker_key) preRow already claimed. This is the mid-run
	// failure: a reservation was made, and the row it names was never
	// created.
	failTx, err := engine.beginTenant(ctx, principal)
	if err != nil {
		t.Fatalf("begin failing attempt: %v", err)
	}
	failRow, err := engine.newWorkerRowContext(ctx, failTx, principal, in, options)
	if err != nil {
		t.Fatalf("build failing attempt: %v", err)
	}
	if failRow.WorkerNumber == preRow.WorkerNumber {
		t.Fatalf("the failing attempt's reservation collided with the pre-existing worker's own number %q; the test fixture is not isolating the two reservations", preRow.WorkerNumber)
	}
	if _, err := engine.insertWorker(ctx, failTx, failRow); err == nil {
		t.Fatal("expected the colliding worker identity to fail the insert")
	}
	if err := failTx.Rollback(ctx); err != nil {
		t.Fatalf("rollback failing attempt: %v", err)
	}

	// The reservation the failing attempt made must not have survived --
	// this is the exact defect the coordinator's live-database evidence
	// named (a reservation left behind by a worker that was never created).
	var reservationCount int
	if err := db.Conn.QueryRow(ctx,
		`SELECT count(*) FROM worker_id_reservation WHERE tenant_id=$1 AND worker_number=$2`,
		tenantID, failRow.WorkerNumber,
	).Scan(&reservationCount); err != nil {
		t.Fatalf("count orphaned reservation: %v", err)
	}
	if reservationCount != 0 {
		t.Fatalf("reservation %q survived the failed attempt's rollback: found %d row(s), want 0", failRow.WorkerNumber, reservationCount)
	}

	// Step 3: the genuine retry -- a different, fresh identity (freshID),
	// so nothing collides this time. It must recover the exact number the
	// failed attempt tried and never actually spent, and the worker must
	// actually exist afterward.
	retryIn := in
	retryIn.LegalName, retryIn.PreferredName = "Grace Hopper", "Grace"
	retryTx, err := engine.beginTenant(ctx, principal)
	if err != nil {
		t.Fatalf("begin retry: %v", err)
	}
	retryRow, err := engine.newWorkerRowContext(ctx, retryTx, principal, retryIn, options)
	if err != nil {
		t.Fatalf("build retry: %v", err)
	}
	if retryRow.WorkerNumber != failRow.WorkerNumber {
		t.Fatalf("retry reserved %q, want the same number %q the failed attempt never actually spent", retryRow.WorkerNumber, failRow.WorkerNumber)
	}
	stored, err := engine.insertWorker(ctx, retryTx, retryRow)
	if err != nil {
		t.Fatalf("insert retry: %v", err)
	}
	if err := retryTx.Commit(ctx); err != nil {
		t.Fatalf("commit retry: %v", err)
	}

	readTx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin read: %v", err)
	}
	defer func() { _ = readTx.Rollback(ctx) }()
	found, ok, err := (workforce.Store{}).Get(ctx, readTx, tenantID, stored.WorkerKey)
	if err != nil {
		t.Fatalf("read back the recovered worker: %v", err)
	}
	if !ok {
		t.Fatalf("the worker the retry reported creating does not actually exist: %s", stored.WorkerKey)
	}
	if found.WorkerNumber != failRow.WorkerNumber {
		t.Fatalf("durable worker number = %q, want the recovered %q", found.WorkerNumber, failRow.WorkerNumber)
	}
}
