package workforce_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
)

// withEmploymentFacts fills the six columns migrations/00316 added, in the
// vocabulary the demo seed writes.
func withEmploymentFacts(row workforce.WorkerRow) workforce.WorkerRow {
	row.EmploymentType = workforce.EmploymentTypeFixedTerm
	row.TimeType = workforce.TimeTypePartTime
	row.FTE = "0.6000"
	row.Company = "HarborCare Health Services, Inc."
	row.BusinessUnit = "Care Operations"
	row.CostCenter = "CC-2200 Care Coordination"
	row.WorkArrangement = workforce.WorkArrangementOnSite
	return row
}

func assertEmploymentFacts(t *testing.T, where string, got workforce.WorkerRow) {
	t.Helper()
	for _, tc := range []struct{ name, got, want string }{
		{"employment_type", got.EmploymentType, workforce.EmploymentTypeFixedTerm},
		{"time_type", got.TimeType, workforce.TimeTypePartTime},
		{"fte", got.FTE, "0.6000"},
		{"company", got.Company, "HarborCare Health Services, Inc."},
		{"business_unit", got.BusinessUnit, "Care Operations"},
		{"cost_center", got.CostCenter, "CC-2200 Care Coordination"},
		{"work_arrangement", got.WorkArrangement, workforce.WorkArrangementOnSite},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: %s = %q, want %q", where, tc.name, tc.got, tc.want)
		}
	}
}

// The six new columns survive the write and every read shape. Create returns
// the stored row, and Get and List re-read it, so a column missing from any
// one of the three projections is caught here rather than as an empty field on
// a page.
func TestEmploymentFactsRoundTripThroughEveryReadShape(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "employment-facts")
	conn := appConn(t, db)
	row := withEmploymentFacts(newRow(tenant, "rosa-santos"))

	var created, fetched workforce.WorkerRow
	var listed []workforce.WorkerRow
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		if created, err = (workforce.Store{}).Create(context.Background(), tx, row); err != nil {
			return err
		}
		var found bool
		if fetched, found, err = (workforce.Store{}).Get(context.Background(), tx, tenant, "rosa-santos"); err != nil {
			return err
		} else if !found {
			t.Fatal("the worker just created was not found")
		}
		listed, err = (workforce.Store{}).List(context.Background(), tx, tenant)
		return err
	})

	assertEmploymentFacts(t, "Create", created)
	assertEmploymentFacts(t, "Get", fetched)
	if len(listed) != 1 {
		t.Fatalf("List returned %d rows, want 1", len(listed))
	}
	assertEmploymentFacts(t, "List", listed[0])
}

// A row that asserts none of them reads back as empty rather than as
// something. The columns are nullable precisely so that "nobody said" stays
// distinguishable, and COALESCE turning NULL into "" is what the object page
// reports as unreported.
func TestUnassertedEmploymentFactsReadBackEmpty(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "employment-facts-absent")
	conn := appConn(t, db)

	var created workforce.WorkerRow
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		created, err = (workforce.Store{}).Create(context.Background(), tx, newRow(tenant, "unstated"))
		return err
	})

	for _, tc := range []struct{ name, got string }{
		{"employment_type", created.EmploymentType},
		{"time_type", created.TimeType},
		{"company", created.Company},
		{"business_unit", created.BusinessUnit},
		{"cost_center", created.CostCenter},
		{"work_arrangement", created.WorkArrangement},
	} {
		if tc.got != "" {
			t.Errorf("unasserted %s = %q, want empty", tc.name, tc.got)
		}
	}

	// And the row really carries NULL rather than an empty string, which is
	// the difference the CHECK constraints are written against.
	var nulls int
	row := db.QueryRow(context.Background(), `
		SELECT count(*) FROM journey_worker
		WHERE tenant_id = $1
		  AND employment_type IS NULL AND time_type IS NULL AND company IS NULL
		  AND business_unit IS NULL AND cost_center IS NULL AND work_arrangement IS NULL`,
		tenant)
	if err := row.Scan(&nulls); err != nil {
		t.Fatalf("count NULL employment facts: %v", err)
	}
	if nulls != 1 {
		t.Fatalf("%d rows carry NULL employment facts, want 1", nulls)
	}
}

// The employment facts are tenant-scoped like every other column on the row:
// another tenant's reader sees no worker at all, so it cannot see the facts
// either.
func TestEmploymentFactsAreTenantScoped(t *testing.T) {
	db := pgtest.New(t)
	owner := insertTenant(t, db, "employment-facts-owner")
	other := insertTenant(t, db, "employment-facts-other")
	conn := appConn(t, db)

	inTenantTx(t, conn, owner, func(tx dbport.Tx) error {
		_, err := (workforce.Store{}).Create(context.Background(), tx, withEmploymentFacts(newRow(owner, "owned")))
		return err
	})

	var found bool
	var rows []workforce.WorkerRow
	inTenantTx(t, conn, other, func(tx dbport.Tx) error {
		var err error
		if _, found, err = (workforce.Store{}).Get(context.Background(), tx, other, "owned"); err != nil {
			return err
		}
		rows, err = (workforce.Store{}).List(context.Background(), tx, other)
		return err
	})
	if found {
		t.Error("another tenant read the worker by key")
	}
	if len(rows) != 0 {
		t.Errorf("another tenant listed %d workers, want 0", len(rows))
	}
}

// Validation refuses the contradictions the page would otherwise have to
// render. Each case is a row that is wrong on the way in, and journey_worker
// is append-only, so refusing here is the only chance to refuse at all.
func TestEmploymentFactValidationRefusesContradictions(t *testing.T) {
	base := newRow(uuid.New(), "validate")
	for _, tc := range []struct {
		name string
		row  func(workforce.WorkerRow) workforce.WorkerRow
	}{
		{"full time with a fractional allocation", func(r workforce.WorkerRow) workforce.WorkerRow {
			r.TimeType, r.FTE = workforce.TimeTypeFullTime, "0.6000"
			return r
		}},
		{"part time with a whole allocation", func(r workforce.WorkerRow) workforce.WorkerRow {
			r.TimeType, r.FTE = workforce.TimeTypePartTime, "1.0000"
			return r
		}},
		{"employment type outside the vocabulary", func(r workforce.WorkerRow) workforce.WorkerRow {
			r.EmploymentType = "PERMANENT"
			return r
		}},
		{"time type outside the vocabulary", func(r workforce.WorkerRow) workforce.WorkerRow {
			r.TimeType = "FLEXIBLE"
			return r
		}},
		{"work arrangement outside the vocabulary", func(r workforce.WorkerRow) workforce.WorkerRow {
			r.WorkArrangement = "OFFSHORE"
			return r
		}},
		{"cost center that is only whitespace", func(r workforce.WorkerRow) workforce.WorkerRow {
			r.CostCenter = "   "
			return r
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.row(base).Validate()
			if !errors.Is(err, workforce.ErrInvalidRow) {
				t.Fatalf("Validate() = %v, want %v", err, workforce.ErrInvalidRow)
			}
		})
	}

	// The agreeing combinations are accepted, so the rule above is refusing
	// contradictions rather than refusing the feature.
	for _, tc := range []struct{ timeType, fte string }{
		{workforce.TimeTypeFullTime, "1.0000"},
		{workforce.TimeTypeFullTime, "1"},
		{workforce.TimeTypePartTime, "0.8000"},
	} {
		row := base
		row.TimeType, row.FTE = tc.timeType, tc.fte
		row.EmploymentType = workforce.EmploymentTypeRegular
		row.WorkArrangement = workforce.WorkArrangementRemote
		if err := row.Validate(); err != nil {
			t.Errorf("Validate() for %s at fte %s = %v, want nil", tc.timeType, tc.fte, err)
		}
	}
}

// The company column is what people.FieldLegalEntity is now answered from. A
// row that names one discloses it; a row that does not still discloses the
// field as absent, which is the distinction the projection is built on.
func TestLegalEntityFactComesFromTheCompanyColumn(t *testing.T) {
	stated := withEmploymentFacts(newRow(uuid.New(), "stated"))
	if got := workforce.FieldValues(stated)[people.FieldLegalEntity]; got != "HarborCare Health Services, Inc." {
		t.Errorf("legal entity = %q, want the recorded company", got)
	}

	unstated := newRow(uuid.New(), "unstated")
	if got, present := workforce.FieldValues(unstated)[people.FieldLegalEntity]; got != "" || !present {
		t.Errorf("unstated legal entity = %q (present=%v), want an empty value the projection reports as absent", got, present)
	}
}
