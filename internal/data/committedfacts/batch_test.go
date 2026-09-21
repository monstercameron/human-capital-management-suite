package committedfacts_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/committedfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CurrentPlacements is proved against CurrentPlacement itself: for every
// worker asked about, the batch answers the placement the single-worker read
// answers, and is absent exactly where that read reports false. Anything
// weaker would let the served directory quietly disclose a different
// placement than the one the product's other surfaces read.

// projectPopulation creates and projects n workers, returning their rows in
// the order they were created.
func projectPopulation(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, n int) []workforce.WorkerRow {
	t.Helper()
	recorded := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	rows := make([]workforce.WorkerRow, 0, n)
	for i := 0; i < n; i++ {
		short := fmt.Sprintf("%03d", i)
		rows = append(rows, workforce.WorkerRow{
			TenantID: tenantID, WorkerID: uuid.New(), WorkerKey: "cfb-" + short + "-worker",
			LegalName: "Batch Worker " + short, PreferredName: "Batch", WorkerNumber: "W-CFB-" + short,
			WorkerType: "employee", LifecycleStatus: "active", EmploymentID: "emp_cfb" + short, AssignmentID: "asg_cfb" + short,
			JobCode: "OPS-HRBP2", JobTitle: "People Partner", Grade: "P2", OrgUnit: "people-ops",
			PositionID: "POS-CFB-" + short, Location: "Boston, MA", PayZone: "US-EAST", FTE: "1.0000",
			HireDate: "2021-04-05", EffectiveFrom: "2021-04-05", BasePay: "90000.00", Currency: "USD",
			PayBasis: "ANNUAL_SALARY", BonusTarget: "0.0500", ManagerRelationshipRef: "rel_mgr_cfb" + short,
			RevisionStream: "people.worker.cfb" + short, RevisionSequence: 1,
			KnownAt: recorded, RecordedAt: recorded, CreatedBy: "test", Source: workforce.SourceCreated,
		})
	}
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		for _, row := range rows {
			if _, err := (workforce.Store{}).Create(context.Background(), tx, row); err != nil {
				return err
			}
			if _, err := demoworkforce.ProjectWorker(context.Background(), tx, row, "Committed Facts Batch Entity"); err != nil {
				return err
			}
		}
		return nil
	})
	return rows
}

// countingTx counts the statements one read issues.
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

func workerIDs(rows []workforce.WorkerRow) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.WorkerID)
	}
	return out
}

// TestCurrentPlacementsAnswersWhatCurrentPlacementAnswers is the PRIMARY
// case: every worker's batched placement is field for field the single-worker
// read's own, before and after a promotion commits successor facts for one of
// them.
func TestCurrentPlacementsAnswersWhatCurrentPlacementAnswers(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedTenant(t, db)
	ctx := context.Background()
	rows := projectPopulation(t, db, tenantID, 5)

	assertAgrees := func(when string) {
		t.Helper()
		inTx(t, db, tenantID, func(tx dbport.Tx) error {
			batched, err := committedfacts.CurrentPlacements(ctx, tx, tenantID, workerIDs(rows), at)
			if err != nil {
				t.Fatalf("%s: CurrentPlacements: %v", when, err)
			}
			if len(batched) != len(rows) {
				t.Fatalf("%s: batched %d placements for %d workers", when, len(batched), len(rows))
			}
			for _, row := range rows {
				want, found, err := committedfacts.CurrentPlacement(ctx, tx, tenantID, row.WorkerID, at)
				if err != nil || !found {
					t.Fatalf("%s: CurrentPlacement(%s) = %t, %v", when, row.WorkerKey, found, err)
				}
				if got := batched[row.WorkerID]; got != want {
					t.Errorf("%s: batched placement for %s =\n%+v\nsingle read answered\n%+v", when, row.WorkerKey, got, want)
				}
			}
			return nil
		})
	}
	assertAgrees("before the commit")

	// Commit one worker's promotion, exactly as the promotion commit writer
	// appends its successor facts, and prove the batch follows it too.
	promoted := rows[2]
	ids := demoworkforce.AggregateIDsFor(promoted)
	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		assignment, err := aggregates.NewAssignment(tenantID, ids.Assignment, ids.Employment, true, at, nil, time.Now().UTC(),
			"OPS-HRBP3", "P3", nil, nil, "Boston, MA", "US-EAST", "1.0000", "")
		if err != nil {
			return err
		}
		if _, err := (aggregates.PeopleStore{}).PutAssignment(ctx, tx, assignment); err != nil {
			return err
		}
		pay, err := values.NewMoney("98000.00", "USD", 2, values.RoundingExactRequired)
		if err != nil {
			return err
		}
		component, err := aggregates.NewCompensationComponent(tenantID, ids.BasePay, ids.Package, at, nil, time.Now().UTC(),
			"BASE_PAY", pay, "ANNUAL")
		if err != nil {
			return err
		}
		_, err = (aggregates.CompensationStore{}).PutCompensationComponent(ctx, tx, component)
		return err
	})
	assertAgrees("after the commit")

	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		batched, err := committedfacts.CurrentPlacements(ctx, tx, tenantID, workerIDs(rows), at)
		if err != nil {
			return err
		}
		if got := batched[promoted.WorkerID]; got.JobCode != "OPS-HRBP3" || got.Grade != "P3" || got.BasePay != "98000.00" {
			t.Fatalf("the promoted worker's batched placement = %+v, want the committed job, grade and pay", got)
		}
		if got := batched[rows[0].WorkerID]; got.JobCode != "OPS-HRBP2" || got.BasePay != "90000.00" {
			t.Fatalf("an unpromoted worker's batched placement moved: %+v", got)
		}
		return nil
	})
}

// TestCurrentPlacementsReportsAbsenceAndStaysInTheTenant proves a worker with
// no projection is absent rather than blank-but-present, that an empty
// population issues no statement, and that another tenant's worker is not
// answered.
func TestCurrentPlacementsReportsAbsenceAndStaysInTheTenant(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedTenant(t, db)
	otherTenant := seedTenant(t, db)
	ctx := context.Background()
	rows := projectPopulation(t, db, tenantID, 3)
	stranger := uuid.New()

	inTx(t, db, tenantID, func(tx dbport.Tx) error {
		batched, err := committedfacts.CurrentPlacements(ctx, tx, tenantID, append(workerIDs(rows), stranger), at)
		if err != nil {
			return err
		}
		if _, present := batched[stranger]; present {
			t.Error("a worker with no projection was answered rather than left absent")
		}
		if len(batched) != len(rows) {
			t.Errorf("batched %d placements, want %d", len(batched), len(rows))
		}
		if _, found, err := committedfacts.CurrentPlacement(ctx, tx, tenantID, stranger, at); found || err != nil {
			t.Errorf("the single-worker read answered %t, %v for the same worker", found, err)
		}

		crossTenant, err := committedfacts.CurrentPlacements(ctx, tx, otherTenant, workerIDs(rows), at)
		if err != nil {
			return err
		}
		if len(crossTenant) != 0 {
			t.Errorf("this tenant's workers answered %d placements under another tenant", len(crossTenant))
		}

		counting := &countingTx{Tx: tx}
		empty, err := committedfacts.CurrentPlacements(ctx, counting, tenantID, nil, at)
		if err != nil {
			return err
		}
		if len(empty) != 0 || counting.queries != 0 {
			t.Errorf("an empty population issued %d statements and answered %d placements", counting.queries, len(empty))
		}
		return nil
	})
}

// TestCurrentPlacementsDoesNotGrowWithThePopulation is the PERFORMANCE case
// the served worker directory needs: the statement count is bounded and flat,
// while the per-worker read it replaces is linear.
func TestCurrentPlacementsDoesNotGrowWithThePopulation(t *testing.T) {
	db := pgtest.New(t)
	small := seedTenant(t, db)
	large := seedTenant(t, db)
	ctx := context.Background()
	smallRows := projectPopulation(t, db, small, 2)
	largeRows := projectPopulation(t, db, large, 20)

	count := func(tenantID uuid.UUID, rows []workforce.WorkerRow) (batched, perWorker int) {
		inTx(t, db, tenantID, func(tx dbport.Tx) error {
			counting := &countingTx{Tx: tx}
			if _, err := committedfacts.CurrentPlacements(ctx, counting, tenantID, workerIDs(rows), at); err != nil {
				return err
			}
			batched = counting.queries
			counting.queries = 0
			for _, row := range rows {
				if _, _, err := committedfacts.CurrentPlacement(ctx, counting, tenantID, row.WorkerID, at); err != nil {
					return err
				}
			}
			perWorker = counting.queries
			return nil
		})
		return batched, perWorker
	}

	smallBatched, smallPerWorker := count(small, smallRows)
	largeBatched, largePerWorker := count(large, largeRows)
	if smallBatched != largeBatched {
		t.Fatalf("%d workers cost %d statements and %d workers cost %d: the batched read is O(N)",
			len(smallRows), smallBatched, len(largeRows), largeBatched)
	}
	if largeBatched > 6 {
		t.Fatalf("CurrentPlacements issued %d statements, want at most the six questions it asks", largeBatched)
	}
	if largePerWorker <= smallPerWorker {
		t.Fatalf("the per-worker read cost %d statements for %d workers and %d for %d: the fixture proves nothing",
			smallPerWorker, len(smallRows), largePerWorker, len(largeRows))
	}
}
