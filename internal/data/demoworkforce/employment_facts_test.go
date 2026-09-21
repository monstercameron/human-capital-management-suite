package demoworkforce

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
)

// planOrFail builds the deterministic plan for a throwaway tenant.
func planOrFail(t *testing.T) []Employee {
	t.Helper()
	employees, err := Plan(uuid.New())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return employees
}

// Every planned worker carries all six employment facts. The object page
// renders exactly these, so one worker missing one of them is one page
// showing "not reported" -- which is the defect this lane exists to remove.
func TestEveryPlannedWorkerCarriesItsEmploymentFacts(t *testing.T) {
	employees := planOrFail(t)
	if len(employees) != NewWorkerCount {
		t.Fatalf("planned %d workers, want %d", len(employees), NewWorkerCount)
	}
	for _, employee := range employees {
		row := employee.Row
		for _, tc := range []struct{ name, value string }{
			{"employment_type", row.EmploymentType},
			{"time_type", row.TimeType},
			{"company", row.Company},
			{"business_unit", row.BusinessUnit},
			{"cost_center", row.CostCenter},
			{"work_arrangement", row.WorkArrangement},
		} {
			if strings.TrimSpace(tc.value) == "" {
				t.Errorf("%s: %s is unset", row.WorkerKey, tc.name)
			}
		}
		// Validate is the storage boundary's own contradiction check, so a
		// plan that would be refused on insert fails here first.
		if err := row.Validate(); err != nil {
			t.Errorf("%s: %v", row.WorkerKey, err)
		}
	}
}

// The facts agree with the rest of the record. Each assertion names a
// specific way two tables could disagree, because "looks populated" is not
// the bar: a cost center from the wrong unit or a hybrid worker with no
// office nearby is data that reads as real and is not.
func TestPlannedEmploymentFactsAreInternallyConsistent(t *testing.T) {
	employees := planOrFail(t)
	units := make(map[string]OrganizationUnit, len(HarborCare.Units))
	for _, unit := range HarborCare.Units {
		units[unit.Code] = unit
	}

	partTime, fixedTerm, onSite, hybrid, remote := 0, 0, 0, 0, 0
	for _, employee := range employees {
		row := employee.Row
		unit := units[row.OrgUnit]

		// The company is the one legal entity the organization seed records.
		if row.Company != HarborCare.LegalEntity {
			t.Errorf("%s: company = %q, want the recorded legal entity %q", row.WorkerKey, row.Company, HarborCare.LegalEntity)
		}

		// The business unit is the unit's own parent line, walked from the
		// recorded hierarchy rather than asserted beside it.
		wantBusinessUnit := units[unit.ParentCode].Name
		if unit.ParentCode == "" {
			wantBusinessUnit = unit.Name
		}
		if row.BusinessUnit != wantBusinessUnit {
			t.Errorf("%s in %s: business unit = %q, want the parent line %q", row.WorkerKey, row.OrgUnit, row.BusinessUnit, wantBusinessUnit)
		}

		// The cost center is the unit's, and every worker in a unit charges
		// to the same one.
		if want := costCenters[row.OrgUnit]; row.CostCenter != want {
			t.Errorf("%s in %s: cost center = %q, want %q", row.WorkerKey, row.OrgUnit, row.CostCenter, want)
		}

		// Time type agrees with the allocation.
		switch row.TimeType {
		case workforce.TimeTypeFullTime:
			if row.FTE != "1.0000" {
				t.Errorf("%s: full time at fte %s", row.WorkerKey, row.FTE)
			}
		case workforce.TimeTypePartTime:
			partTime++
			if row.FTE == "1.0000" {
				t.Errorf("%s: part time at a whole allocation", row.WorkerKey)
			}
		default:
			t.Errorf("%s: time type %q is outside the vocabulary", row.WorkerKey, row.TimeType)
		}

		if row.EmploymentType == workforce.EmploymentTypeFixedTerm {
			fixedTerm++
		}

		// The work arrangement agrees with the location and with what the
		// unit's work requires: nobody in a care-delivery or facilities unit
		// is remote, and nobody away from the single office is hybrid.
		switch row.WorkArrangement {
		case workforce.WorkArrangementOnSite:
			onSite++
			if !onSiteUnits[row.OrgUnit] {
				t.Errorf("%s in %s: on-site in a unit whose work is not site-bound", row.WorkerKey, row.OrgUnit)
			}
		case workforce.WorkArrangementHybrid:
			hybrid++
			if row.Location != headquarters {
				t.Errorf("%s: hybrid at %s, which has no HarborCare office", row.WorkerKey, row.Location)
			}
		case workforce.WorkArrangementRemote:
			remote++
			if row.Location == headquarters {
				t.Errorf("%s: remote at the headquarters location", row.WorkerKey)
			}
			if onSiteUnits[row.OrgUnit] {
				t.Errorf("%s in %s: remote in a site-bound unit", row.WorkerKey, row.OrgUnit)
			}
		default:
			t.Errorf("%s: work arrangement %q is outside the vocabulary", row.WorkerKey, row.WorkArrangement)
		}

		// A non-exempt worker is overtime-eligible, which only means anything
		// if the pay basis is one overtime can be computed from. Every demo
		// worker is salaried, so this is the check that a later hourly
		// worker cannot be introduced without revisiting the bands.
		if row.PayBasis != "ANNUAL_SALARY" {
			t.Errorf("%s: pay basis = %q, want ANNUAL_SALARY", row.WorkerKey, row.PayBasis)
		}
	}

	// The population is varied but not implausibly so: a demo where every
	// worker is identical teaches a reader nothing, and one where half the
	// company is fixed-term is not a care provider.
	if partTime != 2 {
		t.Errorf("%d part-time workers, want the 2 the plan records", partTime)
	}
	if fixedTerm != 2 {
		t.Errorf("%d fixed-term workers, want the 2 the plan records", fixedTerm)
	}
	if onSite == 0 || hybrid == 0 || remote == 0 {
		t.Errorf("work arrangements are not varied: on-site=%d hybrid=%d remote=%d", onSite, hybrid, remote)
	}
}

// Every job code the company staffs has a family and an FLSA status its
// duties support, and the two classifications genuinely differ across roles.
// The defect this replaces was a literal "EXEMPT" for all 47 codes and an
// empty family for all of them.
func TestHarborCareJobCatalogFactsAreComplete(t *testing.T) {
	seen := map[string]bool{}
	nonExempt := 0
	families := map[string]bool{}
	for _, group := range staffing {
		for _, role := range group.Roles {
			if seen[role.Code] {
				continue
			}
			seen[role.Code] = true

			family := JobFamilyFor(role.Code)
			if strings.TrimSpace(family) == "" {
				t.Errorf("job %s (%s) has no job family", role.Code, role.Title)
			}
			families[family] = true

			switch JobExemptStatusFor(role.Code) {
			case "NON_EXEMPT":
				nonExempt++
			case "EXEMPT":
			default:
				t.Errorf("job %s has an FLSA status outside the vocabulary", role.Code)
			}
		}
	}
	if len(seen) != 47 {
		t.Fatalf("staffing declares %d distinct job codes, want 47", len(seen))
	}
	if nonExempt == 0 || nonExempt == len(seen) {
		t.Fatalf("%d of %d jobs are non-exempt: the classification does not distinguish roles", nonExempt, len(seen))
	}
	if len(families) < 10 {
		t.Errorf("%d distinct job families for 47 codes, want a real architecture", len(families))
	}

	// The specific distinction the brief names: a care coordinator and an
	// analyst are classified differently, and a coordinator's own manager
	// differs from the coordinator.
	for _, tc := range []struct{ code, want string }{
		{"CARE-CC2", "NON_EXEMPT"},
		{"CARE-CC3", "NON_EXEMPT"},
		{"CARE-MGR", "EXEMPT"},
		{"QLT-DA3", "EXEMPT"},
		{"DAT-AN3", "EXEMPT"},
		{"FIN-FPA3", "EXEMPT"},
		{"IT-SA2", "NON_EXEMPT"},
		{"ENG-SWE3", "EXEMPT"},
	} {
		if got := JobExemptStatusFor(tc.code); got != tc.want {
			t.Errorf("%s FLSA = %s, want %s", tc.code, got, tc.want)
		}
	}
}

// The seed lands the employment facts in journey_worker for all 60 workers,
// scoped to the tenant, and a replay verifies rather than duplicates.
func TestSeedRecordsEmploymentFactsAndReplaysCleanly(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedAggregateTenant(t, db)
	other := seedAggregateTenant(t, db)

	var first Summary
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		first, err = Seed(context.Background(), tx, tenantID)
		return err
	})
	if first.Inserted != NewWorkerCount {
		t.Fatalf("first seed inserted %d workers, want %d", first.Inserted, NewWorkerCount)
	}

	var rows []workforce.WorkerRow
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		rows, err = (workforce.Store{}).List(context.Background(), tx, tenantID)
		return err
	})
	if len(rows) != NewWorkerCount {
		t.Fatalf("stored %d workers, want %d", len(rows), NewWorkerCount)
	}
	byKey := make(map[string]workforce.WorkerRow, len(rows))
	for _, row := range rows {
		byKey[row.WorkerKey] = row
		for _, tc := range []struct{ name, value string }{
			{"employment_type", row.EmploymentType},
			{"time_type", row.TimeType},
			{"company", row.Company},
			{"business_unit", row.BusinessUnit},
			{"cost_center", row.CostCenter},
			{"work_arrangement", row.WorkArrangement},
		} {
			if strings.TrimSpace(tc.value) == "" {
				t.Errorf("stored %s: %s is empty", row.WorkerKey, tc.name)
			}
		}
	}

	// Spot-check one worker by value, end to end from plan to stored row, so
	// a column silently dropped from the INSERT cannot pass by being
	// non-empty on every other field.
	rosa, ok := byKey["hc-015-rosa-santos"]
	if !ok {
		t.Fatal("the part-time care coordinator was not stored")
	}
	for _, tc := range []struct{ name, got, want string }{
		{"employment_type", rosa.EmploymentType, workforce.EmploymentTypeRegular},
		{"time_type", rosa.TimeType, workforce.TimeTypePartTime},
		{"fte", rosa.FTE, "0.6000"},
		{"company", rosa.Company, HarborCare.LegalEntity},
		{"business_unit", rosa.BusinessUnit, "Care Operations"},
		{"cost_center", rosa.CostCenter, "CC-2200 Care Coordination"},
		{"work_arrangement", rosa.WorkArrangement, workforce.WorkArrangementOnSite},
	} {
		if tc.got != tc.want {
			t.Errorf("hc-015-rosa-santos %s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	if clarke := byKey["hc-018-henry-clarke"]; clarke.EmploymentType != workforce.EmploymentTypeFixedTerm {
		t.Errorf("hc-018-henry-clarke employment type = %q, want %q", clarke.EmploymentType, workforce.EmploymentTypeFixedTerm)
	}

	// A replay verifies every existing row instead of inserting a second
	// copy or failing. sameSeedIdentity now compares the employment facts
	// too, so a row that drifted from the plan is a hard error rather than a
	// silent skip.
	var replay Summary
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		replay, err = Seed(context.Background(), tx, tenantID)
		return err
	})
	if replay.Inserted != 0 || replay.Skipped != NewWorkerCount {
		t.Fatalf("replay inserted %d and skipped %d, want 0 and %d", replay.Inserted, replay.Skipped, NewWorkerCount)
	}

	// The rows belong to their tenant alone.
	inAggregateTx(t, db, other, func(tx dbport.Tx) error {
		elsewhere, err := (workforce.Store{}).List(context.Background(), tx, other)
		if err != nil {
			return err
		}
		if len(elsewhere) != 0 {
			t.Errorf("another tenant listed %d seeded workers, want 0", len(elsewhere))
		}
		return nil
	})
}

// The job aggregate rows carry the family and the FLSA status rather than the
// empty string and the blanket "EXEMPT" they used to.
func TestProjectedJobsCarryFamilyAndExemptStatus(t *testing.T) {
	db := pgtest.New(t)
	tenantID := seedAggregateTenant(t, db)

	employees := planOrFail(t)
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		if _, err := SeedOrganization(context.Background(), tx, tenantID); err != nil {
			return err
		}
		for _, employee := range employees {
			row := employee.Row
			row.TenantID = tenantID
			if _, err := (workforce.Store{}).Create(context.Background(), tx, row); err != nil {
				return err
			}
		}
		stored, err := (workforce.Store{}).List(context.Background(), tx, tenantID)
		if err != nil {
			return err
		}
		for _, row := range stored {
			if _, err := ProjectWorker(context.Background(), tx, row, HarborCare.LegalEntity); err != nil {
				return err
			}
		}
		return nil
	})

	org := aggregates.OrganizationStore{}
	nonExempt := 0
	checked := map[string]bool{}
	inAggregateTx(t, db, tenantID, func(tx dbport.Tx) error {
		for _, employee := range employees {
			code, grade := employee.Row.JobCode, employee.Row.Grade
			if checked[code] {
				continue
			}
			checked[code] = true
			job, err := org.CurrentJob(context.Background(), tx, tenantID, JobID(code, grade), CatalogEffectiveFrom)
			if err != nil {
				return err
			}
			if job.JobFamily == "" {
				t.Errorf("job %s has no recorded family", code)
			}
			if want := JobFamilyFor(code); job.JobFamily != want {
				t.Errorf("job %s family = %q, want %q", code, job.JobFamily, want)
			}
			if want := JobExemptStatusFor(code); job.ExemptStatus != want {
				t.Errorf("job %s exempt status = %q, want %q", code, job.ExemptStatus, want)
			}
			if job.ExemptStatus == "NON_EXEMPT" {
				nonExempt++
			}
		}
		return nil
	})
	if len(checked) != 47 {
		t.Fatalf("recorded %d distinct jobs, want 47", len(checked))
	}
	if nonExempt == 0 {
		t.Fatal("every recorded job is exempt: the hard-coded classification is still in place")
	}
}
