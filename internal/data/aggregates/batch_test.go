package aggregates_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// The batched readers are proved against their own single-entity siblings:
// for every key, the batch either answers exactly what the sibling answers or
// is absent exactly where the sibling reports aggregates.ErrNotFound. That
// oracle is what makes them substitutable in a read path (see
// internal/data/committedfacts and internal/data/positionfacts) without
// re-deciding anything.

// batchGraph is one small but complete organization: a legal entity, an org
// unit, a job, two positions, and three workers each with an employment, a
// primary assignment, a compensation package and a base pay component.
type batchGraph struct {
	tenant      uuid.UUID
	legalEntity uuid.UUID
	orgUnit     uuid.UUID
	job         uuid.UUID
	positions   []uuid.UUID
	workers     []uuid.UUID
	employments []uuid.UUID
	assignments []uuid.UUID
	packages    []uuid.UUID
	components  []uuid.UUID
}

func seedBatchGraph(t *testing.T, db *pgtest.DB, workers int) batchGraph {
	t.Helper()
	ctx := context.Background()
	from := date(t, "2024-01-01")
	recorded := instant(t, "2024-01-01T00:00:00Z")
	g := batchGraph{tenant: insertTenant(t, db), legalEntity: uuid.New(), orgUnit: uuid.New(), job: uuid.New()}
	org, people, comp := aggregates.OrganizationStore{}, aggregates.PeopleStore{}, aggregates.CompensationStore{}

	inTx(t, db, func(tx dbport.Tx) error {
		entity, err := aggregates.NewLegalEntity(g.tenant, g.legalEntity, from, nil, recorded, "Batch Test Holdings", "ACTIVE")
		if err != nil {
			return err
		}
		if _, err := org.PutLegalEntity(ctx, tx, entity); err != nil {
			return err
		}
		unit, err := aggregates.NewOrganizationUnit(g.tenant, g.orgUnit, from, nil, recorded,
			"DEPARTMENT", "people-ops", "People Operations", &g.legalEntity, nil, "ACTIVE")
		if err != nil {
			return err
		}
		if _, err := org.PutOrganizationUnit(ctx, tx, unit); err != nil {
			return err
		}
		job, err := aggregates.NewJob(g.tenant, g.job, from, nil, recorded, "OPS-HRBP2", "People Partner", "OPS", "P2", "EXEMPT")
		if err != nil {
			return err
		}
		if _, err := org.PutJob(ctx, tx, job); err != nil {
			return err
		}
		for i := 0; i < 2; i++ {
			id := uuid.New()
			g.positions = append(g.positions, id)
			jobPosition, err := aggregates.NewJobPosition(g.tenant, id, g.job, g.orgUnit, &g.legalEntity,
				from, nil, recorded, "POS-BATCH-"+string(rune('A'+i)), "Boston, MA", "1.0000", "OPEN")
			if err != nil {
				return err
			}
			if _, err := org.PutJobPosition(ctx, tx, jobPosition); err != nil {
				return err
			}
		}
		for i := 0; i < workers; i++ {
			personID, workerID := uuid.New(), uuid.New()
			employmentID, assignmentID := uuid.New(), uuid.New()
			packageID, componentID := uuid.New(), uuid.New()
			g.workers = append(g.workers, workerID)
			g.employments = append(g.employments, employmentID)
			g.assignments = append(g.assignments, assignmentID)
			g.packages = append(g.packages, packageID)
			g.components = append(g.components, componentID)

			person, err := aggregates.NewPerson(g.tenant, personID, from, nil, recorded, "ACTIVE", "Worker Batch", "Batch")
			if err != nil {
				return err
			}
			if _, err := people.PutPerson(ctx, tx, person); err != nil {
				return err
			}
			worker, err := aggregates.NewWorker(g.tenant, workerID, personID, from, nil, recorded,
				"W-BATCH-"+workerID.String()[:8], "EMPLOYEE", "ACTIVE")
			if err != nil {
				return err
			}
			if _, err := people.PutWorker(ctx, tx, worker); err != nil {
				return err
			}
			employment, err := aggregates.NewEmployment(g.tenant, employmentID, workerID, g.legalEntity,
				from, nil, recorded, "EMPLOYEE", "ACTIVE", nil)
			if err != nil {
				return err
			}
			if _, err := people.PutEmployment(ctx, tx, employment); err != nil {
				return err
			}
			positionRef := g.positions[i%len(g.positions)]
			assignment, err := aggregates.NewAssignment(g.tenant, assignmentID, employmentID, true, from, nil, recorded,
				"OPS-HRBP2", "P2", &g.orgUnit, &positionRef, "Boston, MA", "US-EAST", "1.0000", "rel_mgr_batch")
			if err != nil {
				return err
			}
			if _, err := people.PutAssignment(ctx, tx, assignment); err != nil {
				return err
			}
			pkg, err := aggregates.NewCompensationPackage(g.tenant, packageID, workerID, &employmentID, &assignmentID,
				from, nil, recorded, "USD")
			if err != nil {
				return err
			}
			if _, err := comp.PutCompensationPackage(ctx, tx, pkg); err != nil {
				return err
			}
			component, err := aggregates.NewCompensationComponent(g.tenant, componentID, packageID, from, nil, recorded,
				"BASE_PAY", usd(t, "90000.00"), "ANNUAL")
			if err != nil {
				return err
			}
			if _, err := comp.PutCompensationComponent(ctx, tx, component); err != nil {
				return err
			}
		}
		return nil
	})
	return g
}

// countingTx counts the statements a batched read issues, so a test can prove
// the count does not grow with the number of keys asked about.
type countingTx struct {
	dbport.Tx
	queries int
}

func (c *countingTx) Query(ctx context.Context, sql string, args ...any) (dbport.Rows, error) {
	c.queries++
	return c.Tx.Query(ctx, sql, args...)
}

func (c *countingTx) QueryRow(ctx context.Context, sql string, args ...any) dbport.Row {
	c.queries++
	return c.Tx.QueryRow(ctx, sql, args...)
}

// TestBatchedReadsAgreeWithTheirSingleEntitySiblings is the PRIMARY case:
// every batched reader answers, key for key, what its sibling answers.
func TestBatchedReadsAgreeWithTheirSingleEntitySiblings(t *testing.T) {
	db := pgtest.New(t)
	g := seedBatchGraph(t, db, 3)
	ctx := context.Background()
	at := date(t, "2026-06-01")
	people, org, comp := aggregates.PeopleStore{}, aggregates.OrganizationStore{}, aggregates.CompensationStore{}

	inTx(t, db, func(tx dbport.Tx) error {
		employments, err := people.ActiveEmploymentsForWorkers(ctx, tx, g.tenant, g.workers, at)
		if err != nil {
			return err
		}
		if len(employments) != len(g.workers) {
			t.Fatalf("batched employments = %d, want %d", len(employments), len(g.workers))
		}
		for _, workerID := range g.workers {
			want, err := people.ActiveEmploymentForWorker(ctx, tx, g.tenant, workerID, at)
			if err != nil {
				return err
			}
			if got := employments[workerID]; got.EntityID != want.EntityID || got.Digest != want.Digest {
				t.Errorf("employment for %s = %s/%s, sibling answered %s/%s",
					workerID, got.EntityID, got.Digest, want.EntityID, want.Digest)
			}
		}

		assignments, err := people.PrimaryAssignmentsForEmployments(ctx, tx, g.tenant, g.employments, at)
		if err != nil {
			return err
		}
		for _, employmentID := range g.employments {
			want, err := people.PrimaryAssignmentForEmployment(ctx, tx, g.tenant, employmentID, at)
			if err != nil {
				return err
			}
			got := assignments[employmentID]
			if got.EntityID != want.EntityID || got.JobCode != want.JobCode || got.FTE != want.FTE ||
				got.PayZone != want.PayZone || got.ManagerRelationshipRef != want.ManagerRelationshipRef {
				t.Errorf("assignment for %s = %+v, sibling answered %+v", employmentID, got, want)
			}
			if (got.PositionRef == nil) != (want.PositionRef == nil) ||
				(got.PositionRef != nil && *got.PositionRef != *want.PositionRef) {
				t.Errorf("assignment %s position ref = %v, sibling answered %v", employmentID, got.PositionRef, want.PositionRef)
			}
		}

		units, err := org.CurrentOrganizationUnits(ctx, tx, g.tenant, []uuid.UUID{g.orgUnit}, at)
		if err != nil {
			return err
		}
		wantUnit, err := org.CurrentOrganizationUnit(ctx, tx, g.tenant, g.orgUnit, at)
		if err != nil {
			return err
		}
		if units[g.orgUnit].Code != wantUnit.Code || units[g.orgUnit].Digest != wantUnit.Digest {
			t.Errorf("organization unit = %+v, sibling answered %+v", units[g.orgUnit], wantUnit)
		}

		positions, err := org.CurrentJobPositions(ctx, tx, g.tenant, g.positions, at)
		if err != nil {
			return err
		}
		for _, positionID := range g.positions {
			want, err := org.CurrentJobPosition(ctx, tx, g.tenant, positionID, at)
			if err != nil {
				return err
			}
			got := positions[positionID]
			if got.PositionCode != want.PositionCode || got.CapacityFTE != want.CapacityFTE ||
				got.LifecycleState != want.LifecycleState || got.Digest != want.Digest {
				t.Errorf("job position %s = %+v, sibling answered %+v", positionID, got, want)
			}
		}

		jobs, err := org.CurrentJobs(ctx, tx, g.tenant, []uuid.UUID{g.job}, at)
		if err != nil {
			return err
		}
		wantJob, err := org.CurrentJob(ctx, tx, g.tenant, g.job, at)
		if err != nil {
			return err
		}
		if jobs[g.job].Code != wantJob.Code || jobs[g.job].Title != wantJob.Title {
			t.Errorf("job = %+v, sibling answered %+v", jobs[g.job], wantJob)
		}

		entities, err := org.CurrentLegalEntities(ctx, tx, g.tenant, []uuid.UUID{g.legalEntity}, at)
		if err != nil {
			return err
		}
		wantEntity, err := org.CurrentLegalEntity(ctx, tx, g.tenant, g.legalEntity, at)
		if err != nil {
			return err
		}
		if entities[g.legalEntity].RegisteredName != wantEntity.RegisteredName {
			t.Errorf("legal entity = %+v, sibling answered %+v", entities[g.legalEntity], wantEntity)
		}

		packages, err := comp.ActivePackagesForWorkers(ctx, tx, g.tenant, g.workers, at)
		if err != nil {
			return err
		}
		components, err := comp.BasePayComponentsForPackages(ctx, tx, g.tenant, g.packages, at)
		if err != nil {
			return err
		}
		for i, workerID := range g.workers {
			wantPackage, err := comp.ActivePackageForWorker(ctx, tx, g.tenant, workerID, at)
			if err != nil {
				return err
			}
			if packages[workerID].EntityID != wantPackage.EntityID {
				t.Errorf("package for %s = %s, sibling answered %s", workerID, packages[workerID].EntityID, wantPackage.EntityID)
			}
			wantBase, err := comp.BasePayComponentForPackage(ctx, tx, g.tenant, g.packages[i], at)
			if err != nil {
				return err
			}
			got := components[g.packages[i]]
			if got.Amount != wantBase.Amount || got.Currency != wantBase.Currency || got.ComponentType != "BASE_PAY" {
				t.Errorf("base pay for package %s = %+v, sibling answered %+v", g.packages[i], got, wantBase)
			}
		}
		return nil
	})
}

// TestBatchedReadsReportAbsenceAndStayInTheTenant proves an unknown key is
// absent rather than an error (the sibling's ErrNotFound), that an empty key
// set issues no statement at all, and that another tenant's key answers
// nothing.
func TestBatchedReadsReportAbsenceAndStayInTheTenant(t *testing.T) {
	db := pgtest.New(t)
	g := seedBatchGraph(t, db, 2)
	other := seedBatchGraph(t, db, 1)
	ctx := context.Background()
	at := date(t, "2026-06-01")
	people, org := aggregates.PeopleStore{}, aggregates.OrganizationStore{}

	inTx(t, db, func(tx dbport.Tx) error {
		stranger := uuid.New()
		employments, err := people.ActiveEmploymentsForWorkers(ctx, tx, g.tenant, append([]uuid.UUID{stranger}, g.workers...), at)
		if err != nil {
			return err
		}
		if _, present := employments[stranger]; present {
			t.Error("a worker with no employment was answered rather than left absent")
		}
		if _, err := people.ActiveEmploymentForWorker(ctx, tx, g.tenant, stranger, at); !errors.Is(err, aggregates.ErrNotFound) {
			t.Errorf("the sibling answered %v for the same key, want ErrNotFound", err)
		}
		if len(employments) != len(g.workers) {
			t.Errorf("batched employments = %d, want %d", len(employments), len(g.workers))
		}

		// Another tenant's workers are that tenant's; asking for them under
		// this tenant answers nothing.
		crossTenant, err := people.ActiveEmploymentsForWorkers(ctx, tx, g.tenant, other.workers, at)
		if err != nil {
			return err
		}
		if len(crossTenant) != 0 {
			t.Errorf("reading another tenant's %d workers under this tenant answered %d rows", len(other.workers), len(crossTenant))
		}

		counting := &countingTx{Tx: tx}
		empty, err := org.CurrentJobPositions(ctx, counting, g.tenant, nil, at)
		if err != nil {
			return err
		}
		if len(empty) != 0 || counting.queries != 0 {
			t.Errorf("an empty key set issued %d statements and answered %d rows, want 0 and 0", counting.queries, len(empty))
		}
		return nil
	})
}

// TestBatchedReadsIssueOneStatementPerQuestion is the PERFORMANCE case: the
// statement count is one per question and does not grow with the number of
// keys, which is the whole reason these readers exist.
func TestBatchedReadsIssueOneStatementPerQuestion(t *testing.T) {
	db := pgtest.New(t)
	small := seedBatchGraph(t, db, 2)
	large := seedBatchGraph(t, db, 12)
	ctx := context.Background()
	at := date(t, "2026-06-01")

	count := func(g batchGraph) int {
		issued := 0
		inTx(t, db, func(tx dbport.Tx) error {
			counting := &countingTx{Tx: tx}
			people, comp := aggregates.PeopleStore{}, aggregates.CompensationStore{}
			if _, err := people.ActiveEmploymentsForWorkers(ctx, counting, g.tenant, g.workers, at); err != nil {
				return err
			}
			if _, err := people.PrimaryAssignmentsForEmployments(ctx, counting, g.tenant, g.employments, at); err != nil {
				return err
			}
			if _, err := comp.ActivePackagesForWorkers(ctx, counting, g.tenant, g.workers, at); err != nil {
				return err
			}
			if _, err := comp.BasePayComponentsForPackages(ctx, counting, g.tenant, g.packages, at); err != nil {
				return err
			}
			issued = counting.queries
			return nil
		})
		return issued
	}

	if got, want := count(small), 4; got != want {
		t.Fatalf("four batched questions about 2 workers issued %d statements, want %d", got, want)
	}
	if got, want := count(large), 4; got != want {
		t.Fatalf("four batched questions about 12 workers issued %d statements, want %d: the read is O(N)", got, want)
	}
}

// TestBatchedReadsKeepTheirSiblingsCoordinate proves the batch honours the
// same bitemporal window: a row that is not yet effective, or already closed,
// is invisible to both.
func TestBatchedReadsKeepTheirSiblingsCoordinate(t *testing.T) {
	db := pgtest.New(t)
	g := seedBatchGraph(t, db, 2)
	ctx := context.Background()
	before := date(t, "2023-06-01")

	inTx(t, db, func(tx dbport.Tx) error {
		people := aggregates.PeopleStore{}
		employments, err := people.ActiveEmploymentsForWorkers(ctx, tx, g.tenant, g.workers, before)
		if err != nil {
			return err
		}
		if len(employments) != 0 {
			t.Errorf("a coordinate before every row answered %d employments, want none", len(employments))
		}
		for _, workerID := range g.workers {
			if _, err := people.ActiveEmploymentForWorker(ctx, tx, g.tenant, workerID, before); !errors.Is(err, aggregates.ErrNotFound) {
				t.Errorf("the sibling answered %v at the same coordinate, want ErrNotFound", err)
			}
		}
		return nil
	})
}
