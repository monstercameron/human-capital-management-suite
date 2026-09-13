package orgfacts_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/orgfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

const orgFactsTenant values.TenantId = "orgfacts-tenant"

var fixedInstant = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("scope tenant: %v", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("tenant transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// newRow builds a complete, insertable worker row. managerKey is the WORKER
// KEY of this worker's direct manager -- exactly what
// internal/data/demoworkforce.plan.go stores in ManagerRelationshipRef in
// production -- or "" for a worker with no manager (the top of a chain).
func newRow(tenant uuid.UUID, key, managerKey string) workforce.WorkerRow {
	id := uuid.New()
	return workforce.WorkerRow{
		TenantID: tenant, WorkerID: id, WorkerKey: key,
		LegalName: "Worker " + key, PreferredName: key, WorkerNumber: "W-" + key,
		WorkerType: "employee", LifecycleStatus: "active",
		EmploymentID: "emp_" + key, AssignmentID: "asg_" + key,
		JobCode: "OPS-HRBP2", JobTitle: "Manager", Grade: "P2", OrgUnit: "people-ops",
		PositionID: "POS-HRBP-900", Location: "Boston, MA", PayZone: "US-EAST", FTE: "1.0000",
		ManagerRelationshipRef: managerKey,
		HireDate:               "2021-04-05", EffectiveFrom: "2021-04-05",
		BasePay: "90000.00", Currency: "USD", PayBasis: "ANNUAL_SALARY", BonusTarget: "0.0500",
		RevisionStream: "people.worker." + id.String(), RevisionSequence: 1,
		KnownAt: fixedInstant, RecordedAt: fixedInstant,
		CreatedBy: "principal:manager-1", Source: workforce.SourceCreated,
	}
}

func tenantMap(tenant uuid.UUID) func(values.TenantId) uuid.UUID {
	return func(values.TenantId) uuid.UUID { return tenant }
}

func workerRef(row workforce.WorkerRow) values.EntityRef {
	return values.EntityRef{Tenant: orgFactsTenant, Kind: people.KindWorker, Id: row.WorkerID.String()}
}

func allowAll() org.Authorizer {
	return func(org.ManagerRelationshipFact) people.AuthorizationDecision {
		return people.AuthorizationDecision{
			PolicyVersion: "orgfacts-test/v1", Purpose: "cycle-check", SubjectDisclosable: true,
			Fields: map[people.FieldID]people.FieldRuling{people.FieldManagerRelation: {Effect: people.EffectAllow}},
		}
	}
}

// TestTodo_PROMOUX_005_Integration reaches a real PostgreSQL database (via
// pgtest, exactly like every other data-plane Integration test in this
// tree). It seeds a genuine three-hop reporting chain -- worker A reports to
// B, who reports to C -- as real journey_worker rows, the same table
// internal/data/demoworkforce seeds the live corpus into, then proves two
// things against that real store rather than an in-memory fixture:
//
//  1. org.ResolveManagerRelationships, driven through orgfacts.Reader, walks
//     the real rows and reproduces the chain A -> B -> C.
//  2. org.DetectManagerCycle, driven through the same real reader, refuses
//     "promote A to manage C" -- the exact RED scenario -- because C is two
//     real hops above A, and admits an unrelated worker D who never appears
//     in A's chain.
func TestTodo_PROMOUX_005_Integration(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "promoux005-integration")
	conn := appConn(t, db)

	c := newRow(tenant, "promoux005-c", "board:harborcare")
	b := newRow(tenant, "promoux005-b", c.WorkerKey)
	a := newRow(tenant, "promoux005-a", b.WorkerKey)
	d := newRow(tenant, "promoux005-d", "board:harborcare") // unrelated: no manager, not in A's chain.
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		for _, row := range []workforce.WorkerRow{c, b, a, d} {
			if _, err := (workforce.Store{}).Create(context.Background(), tx, row); err != nil {
				return err
			}
		}
		return nil
	})

	reader := orgfacts.NewReader(appConn(t, db), tenantMap(tenant))
	asOf := values.NewInstant(fixedInstant.Add(24 * time.Hour))

	resolution, err := org.ResolveManagerRelationships(context.Background(), reader, org.ManagerResolutionRequest{
		Tenant: orgFactsTenant, Worker: workerRef(a), AsOf: asOf, MaxDepth: 5, Authorize: allowAll(),
	})
	if err != nil {
		t.Fatalf("ResolveManagerRelationships: %v", err)
	}
	if resolution.Status != org.StatusResolved || len(resolution.Chain) != 2 {
		t.Fatalf("resolution = %+v, want a resolved two-hop chain over real rows", resolution)
	}
	if resolution.Chain[0].Manager.Value != workerRef(b) || resolution.Chain[1].Manager.Value != workerRef(c) {
		t.Fatalf("chain = %+v, want A -> B -> C read back from PostgreSQL", resolution.Chain)
	}

	// The genuine multi-hop cycle: "promote A to manage C" sets C's manager
	// to A. Walking up from A (the proposed manager) reaches C two hops up,
	// over real database rows -- not a single-hop "is C == A" check and not
	// a fixture.
	cycle, err := org.DetectManagerCycle(context.Background(), reader, org.CycleQuery{
		Tenant: orgFactsTenant, Node: workerRef(c), ProposedManager: workerRef(a),
		AsOf: asOf, MaxDepth: 5, Authorize: allowAll(),
	})
	if err != nil {
		t.Fatalf("DetectManagerCycle(C, A): %v", err)
	}
	if cycle.Status != org.CycleStatusCycle || !cycle.WouldCycle() || !cycle.Certain() || cycle.Depth != 2 {
		t.Fatalf("cycle = %+v, want a certain cycle at depth 2 against the real chain", cycle)
	}

	// The negative proof: D never appears above A, so proposing D's manager
	// become A is safe, over the same real store and the same walk.
	safe, err := org.DetectManagerCycle(context.Background(), reader, org.CycleQuery{
		Tenant: orgFactsTenant, Node: workerRef(d), ProposedManager: workerRef(a),
		AsOf: asOf, MaxDepth: 5, Authorize: allowAll(),
	})
	if err != nil {
		t.Fatalf("DetectManagerCycle(D, A): %v", err)
	}
	if safe.Status != org.CycleStatusSafe || safe.WouldCycle() || !safe.Certain() {
		t.Fatalf("cycle(D, A) = %+v, want a certain SAFE verdict", safe)
	}
}

// TestTodo_UXAUDIT_004_Integration is UXAUDIT-004's INTEGRATION contract,
// reaching a real PostgreSQL database (via pgtest) rather than an in-memory
// fixture. UXAUDIT-004's own presentation-layer adapter
// (internal/humanwork/productui's organizationHopFacts) never queries this
// reader directly -- it resolves each page's already-authorized View.People
// -- but org.ResolveManagerRelationships is the SAME function both call, and
// this test proves the production-backed org.WorkerFacts adapter that
// function is built for (this reader, reading real journey_worker rows)
// produces exactly the semantics productui's mapping is designed to be
// compatible with: a genuine multi-hop chain resolves hop by hop, and a
// manager reference that does not resolve to a real worker in this tenant --
// including one that points at a real worker who exists ONLY in a different
// tenant, so it is never a fabricated cross-tenant hop -- comes back as no
// relationship at all (VACANT), never invented, exactly like
// internal/humanwork/productui's own "manager not visible" placement expects
// to receive from an honest reader.
func TestTodo_UXAUDIT_004_Integration(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "uxaudit004-integration")
	otherTenant := insertTenant(t, db, "uxaudit004-other-tenant")
	conn := appConn(t, db)

	ceo := newRow(tenant, "uxaudit004-ceo", "board:harborcare")
	vp := newRow(tenant, "uxaudit004-vp", ceo.WorkerKey)
	director := newRow(tenant, "uxaudit004-director", vp.WorkerKey)
	elsewhere := newRow(otherTenant, "uxaudit004-elsewhere", "board:harborcare")
	// orphan's manager reference names a real worker id -- just one that
	// exists only in a different tenant, so this tenant's reader can never
	// resolve it. That is exactly the "not visible to this viewer" case
	// UXAUDIT-004's placement mapping must render as an honest, explained
	// root rather than a fabricated hop.
	orphan := newRow(tenant, "uxaudit004-orphan", elsewhere.WorkerID.String())

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		for _, row := range []workforce.WorkerRow{ceo, vp, director, orphan} {
			if _, err := (workforce.Store{}).Create(context.Background(), tx, row); err != nil {
				return err
			}
		}
		return nil
	})
	inTenantTx(t, conn, otherTenant, func(tx dbport.Tx) error {
		_, err := (workforce.Store{}).Create(context.Background(), tx, elsewhere)
		return err
	})

	reader := orgfacts.NewReader(appConn(t, db), tenantMap(tenant))
	asOf := values.NewInstant(fixedInstant.Add(24 * time.Hour))

	directorResolution, err := org.ResolveManagerRelationships(context.Background(), reader, org.ManagerResolutionRequest{
		Tenant: orgFactsTenant, Worker: workerRef(director), AsOf: asOf, MaxDepth: 5, Authorize: allowAll(),
	})
	if err != nil {
		t.Fatalf("ResolveManagerRelationships(director): %v", err)
	}
	if directorResolution.Status != org.StatusResolved || len(directorResolution.Chain) != 2 {
		t.Fatalf("director resolution = %+v, want a resolved two-hop chain over real rows", directorResolution)
	}
	if directorResolution.Direct == nil || directorResolution.Direct.Manager.Value != workerRef(vp) {
		t.Fatalf("director's direct manager = %+v, want VP read back from PostgreSQL", directorResolution.Direct)
	}
	if directorResolution.Chain[1].Manager.Value != workerRef(ceo) {
		t.Fatalf("director's chain = %+v, want the second hop to reach the CEO", directorResolution.Chain)
	}

	orphanResolution, err := org.ResolveManagerRelationships(context.Background(), reader, org.ManagerResolutionRequest{
		Tenant: orgFactsTenant, Worker: workerRef(orphan), AsOf: asOf, MaxDepth: 5, Authorize: allowAll(),
	})
	if err != nil {
		t.Fatalf("ResolveManagerRelationships(orphan): %v", err)
	}
	if orphanResolution.Status != org.StatusVacant || orphanResolution.Direct != nil {
		t.Fatalf("orphan resolution = %+v, want VACANT with no fabricated hop across the tenant boundary", orphanResolution)
	}
}

// TestReaderReportsExistenceAndAbsence proves the port's basic existence
// contract against a real row: a seeded worker exists, an unknown id does
// not, and a manager reference that resolves to no journey_worker row (a
// sentinel like "board:harborcare", or a dangling reference) is reported as
// no relationship at all rather than a fabricated hop.
func TestReaderReportsExistenceAndAbsence(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "promoux005-existence")
	conn := appConn(t, db)
	row := newRow(tenant, "promoux005-top", "board:harborcare")
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := (workforce.Store{}).Create(context.Background(), tx, row)
		return err
	})

	reader := orgfacts.NewReader(appConn(t, db), tenantMap(tenant))
	asOf := values.NewInstant(fixedInstant.Add(24 * time.Hour))

	set, err := reader.WorkerFactsAt(context.Background(), org.WorkerFactsQuery{
		Tenant: orgFactsTenant, Worker: workerRef(row), AsOf: asOf,
	})
	if err != nil {
		t.Fatalf("WorkerFactsAt(existing): %v", err)
	}
	if !set.Exists || len(set.Relationships) != 0 {
		t.Fatalf("set = %+v, want an existing worker with zero relationships (dangling sentinel)", set)
	}

	unknown := values.EntityRef{Tenant: orgFactsTenant, Kind: people.KindWorker, Id: uuid.NewString()}
	set, err = reader.WorkerFactsAt(context.Background(), org.WorkerFactsQuery{Tenant: orgFactsTenant, Worker: unknown, AsOf: asOf})
	if err != nil {
		t.Fatalf("WorkerFactsAt(unknown): %v", err)
	}
	if set.Exists {
		t.Fatalf("an unknown worker was reported as existing: %+v", set)
	}
}

// TestReaderWithoutADatabaseAnswersAbsence mirrors
// internal/data/workforce.Facts's own composed-with-nothing behavior: a
// reader with no database or tenant map reports absence rather than
// panicking, which is the correct answer for a cell composed with no
// execution database.
func TestReaderWithoutADatabaseAnswersAbsence(t *testing.T) {
	t.Parallel()
	worker := values.EntityRef{Tenant: orgFactsTenant, Kind: people.KindWorker, Id: uuid.NewString()}
	for name, reader := range map[string]orgfacts.Reader{
		"no database":   orgfacts.NewReader(nil, tenantMap(uuid.New())),
		"no tenant map": orgfacts.NewReader(nil, nil),
	} {
		t.Run(name, func(t *testing.T) {
			set, err := reader.WorkerFactsAt(context.Background(), org.WorkerFactsQuery{
				Tenant: orgFactsTenant, Worker: worker, AsOf: values.NewInstant(fixedInstant),
			})
			if err != nil {
				t.Fatalf("WorkerFactsAt: %v", err)
			}
			if set.Exists {
				t.Fatal("a reader with no database reported a worker")
			}
		})
	}
}
