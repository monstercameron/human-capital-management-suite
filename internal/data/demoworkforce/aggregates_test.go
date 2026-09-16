package demoworkforce

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func seedAggregateTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'demoworkforce aggregates', 'ACTIVE', timestamptz '2020-01-01T00:00:00Z')`,
		tenantID, "demo-agg-"+tenantID.String()[:8])
	return tenantID
}

func inAggregateTx(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope: %v", err)
	}
	if err := fn(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// TestProjectWorkerRecordsTheServedWorkerAsAggregates proves every aggregate
// the promotion commit reads is recorded from the journey_worker row, under
// deterministic identities, that a replay writes nothing, and that the
// manager reference resolves to the manager's worker id (or to no manager at
// the top of the recorded chain).
func TestProjectWorkerRecordsTheServedWorkerAsAggregates(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedAggregateTenant(t, db)
	ctx := context.Background()
	employees, err := Plan(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		if _, err := SeedOrganization(ctx, tx, tenantID); err != nil {
			return err
		}
		if _, err := Seed(ctx, tx, tenantID); err != nil {
			return err
		}
		for _, e := range employees {
			projected, err := ProjectWorker(ctx, tx, e.Row, HarborCare.LegalEntity)
			if err != nil {
				return err
			}
			if !projected {
				t.Errorf("%s was not projected on the first pass", e.Row.WorkerKey)
			}
		}
		return nil
	})
	subject := employees[50].Row // a people-operations worker reporting to the first in its unit
	top := employees[0].Row      // the chief executive reports to the board
	ids := AggregateIDsFor(subject)
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		people, comp, org := aggregates.PeopleStore{}, aggregates.CompensationStore{}, aggregates.OrganizationStore{}
		employment, err := people.ActiveEmploymentForWorker(ctx, tx, tenantID, subject.WorkerID, at)
		if err != nil || employment.EntityID != ids.Employment || employment.EmploymentStatus != "ACTIVE" {
			t.Fatalf("employment = %+v, %v", employment, err)
		}
		assignment, err := people.PrimaryAssignmentForEmployment(ctx, tx, tenantID, ids.Employment, at)
		if err != nil || assignment.JobCode != subject.JobCode || assignment.Grade != subject.Grade {
			t.Fatalf("assignment = %+v, %v", assignment, err)
		}
		manager, found, err := (workforce.Store{}).Get(ctx, tx, tenantID, subject.ManagerRelationshipRef)
		if err != nil || !found || assignment.ManagerRelationshipRef != manager.WorkerID.String() {
			t.Fatalf("assignment manager = %q, want worker id of %s (%v)", assignment.ManagerRelationshipRef, subject.ManagerRelationshipRef, err)
		}
		topAssignment, err := people.PrimaryAssignmentForEmployment(ctx, tx, tenantID, AggregateIDsFor(top).Employment, at)
		if err != nil || topAssignment.ManagerRelationshipRef != "" {
			t.Fatalf("board-reporting assignment manager = %q, %v; want no recorded manager", topAssignment.ManagerRelationshipRef, err)
		}
		pkg, err := comp.ActivePackageForWorker(ctx, tx, tenantID, subject.WorkerID, at)
		if err != nil || pkg.EntityID != ids.Package || pkg.EmploymentRef == nil || *pkg.EmploymentRef != ids.Employment {
			t.Fatalf("package = %+v, %v", pkg, err)
		}
		base, err := comp.BasePayComponentForPackage(ctx, tx, tenantID, ids.Package, at)
		if err != nil || base.Amount != subject.BasePay+"00" || base.Frequency != "ANNUAL" {
			t.Fatalf("base pay = %+v, %v; want %s", base, err, subject.BasePay)
		}
		position, err := org.CurrentJobPosition(ctx, tx, tenantID, FilledPositionID(subject.PositionID), at)
		if err != nil || position.LifecycleState != "FILLED" || position.JobRef != JobID(subject.JobCode, subject.Grade) {
			t.Fatalf("filled position = %+v, %v", position, err)
		}
		occupied, err := OccupiedFTE(ctx, tx, tenantID, FilledPositionID(subject.PositionID), at)
		if err != nil || occupied != "1.0000" {
			t.Fatalf("filled position occupancy = %q, %v", occupied, err)
		}
		projected, err := ProjectWorker(ctx, tx, subject, HarborCare.LegalEntity)
		if err != nil || projected {
			t.Fatalf("replay projected=%t err=%v, want an idempotent no-op", projected, err)
		}
		return nil
	})
	var workers int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM worker WHERE tenant_id = $1`, tenantID).Scan(&workers); err != nil || workers != len(employees) {
		t.Fatalf("worker rows = %d, %v; want %d", workers, err, len(employees))
	}
}

// TestSeedAggregateCatalogAndSelectVacancy proves the catalog records jobs,
// OPEN vacancies and pools once, that SelectVacancy picks the lowest-ordinal
// position with capacity, moves on once it is occupied, and refuses with
// ErrNoVacancy when none remains.
func TestSeedAggregateCatalogAndSelectVacancy(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedAggregateTenant(t, db)
	ctx := context.Background()
	catalog := AggregateCatalog{
		LegalEntityName: "Catalog Test Legal Entity",
		Jobs:            []CatalogJob{{Code: "OPS-HRBP3", Grade: "P3", Title: "Senior HRBP"}},
		Vacancies:       []CatalogVacancy{{OrgUnit: "people-ops", JobCode: "OPS-HRBP3", Grade: "P3", Location: "Boston, MA"}},
		BudgetOrgUnits:  []string{"people-ops"}, BudgetCurrency: "USD", BudgetAmount: "1000000.00",
		RecordedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	var summary CatalogSummary
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		summary, err = SeedAggregateCatalog(ctx, tx, tenantID, catalog)
		return err
	})
	if summary.Jobs != 1 || summary.Vacancies != VacanciesPerTarget || summary.Budgets != 1 {
		t.Fatalf("first seed = %+v", summary)
	}
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		again, err := SeedAggregateCatalog(ctx, tx, tenantID, catalog)
		if err != nil || again != (CatalogSummary{}) {
			t.Fatalf("replayed seed = %+v, %v; want nothing written", again, err)
		}
		return nil
	})
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for n := 1; n <= VacanciesPerTarget; n++ {
		inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
			vacancy, err := SelectVacancy(ctx, tx, tenantID, "people-ops", "OPS-HRBP3", "P3", at)
			if err != nil {
				return err
			}
			if want := VacancyPositionID("people-ops", "OPS-HRBP3", "P3", n); vacancy.PositionID != want {
				t.Fatalf("selection %d = %s, want %s", n, vacancy.PositionID, want)
			}
			occupancy, err := aggregates.NewPositionOccupancy(tenantID, uuid.New(), vacancy.PositionID, nil, nil, at, nil, time.Now().UTC(), "1.0000", true)
			if err != nil {
				return err
			}
			_, err = aggregates.OrganizationStore{}.PutPositionOccupancy(ctx, tx, occupancy)
			return err
		})
	}
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		if _, err := SelectVacancy(ctx, tx, tenantID, "people-ops", "OPS-HRBP3", "P3", at); !errors.Is(err, ErrNoVacancy) {
			t.Fatalf("SelectVacancy with every vacancy filled = %v, want ErrNoVacancy", err)
		}
		budget, err := aggregates.CompensationStore{}.CurrentWorkforceBudget(ctx, tx, tenantID, BudgetID("people-ops"), at)
		if err != nil || budget.AvailableQuantity != "1000000.0000" || budget.BudgetType != BudgetPoolType {
			t.Fatalf("budget = %+v, %v", budget, err)
		}
		if _, err := SelectVacancy(ctx, tx, tenantID, "people-ops", "OPS-UNKNOWN", "P9", at); !errors.Is(err, ErrNoCatalogVacancy) {
			t.Fatalf("SelectVacancy for an uncatalogued target = %v, want ErrNoCatalogVacancy", err)
		}
		if _, err := SeedAggregateCatalog(ctx, nil, tenantID, catalog); err == nil {
			t.Fatal("a catalog seed without a transaction was accepted")
		}
		if _, err := ProjectWorker(ctx, tx, workforce.WorkerRow{}, "x"); err == nil {
			t.Fatal("an invalid worker row was projected")
		}
		return nil
	})
}

func TestAggregateTokenMappings(t *testing.T) {
	for in, want := range map[string]string{"employee": "EMPLOYEE", "contractor": "CONTRACTOR", "intern": "INTERN"} {
		if got := aggregateWorkerType(in); got != want {
			t.Errorf("worker type %q = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"active": "ACTIVE", "terminated": "TERMINATED", "leave": "ON_LEAVE", "x": "PENDING"} {
		if got := aggregateLifecycle(in); got != want {
			t.Errorf("lifecycle %q = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"active": "ACTIVE", "ended": "ENDED", "on_leave": "LEAVE", "x": "PENDING"} {
		if got := aggregateEmploymentStatus(in); got != want {
			t.Errorf("employment status %q = %q, want %q", in, got, want)
		}
	}
	if payFrequency("HOURLY_RATE") != "HOURLY" || payFrequency("ANNUAL_SALARY") != "ANNUAL" {
		t.Error("pay frequency mapping")
	}
}
