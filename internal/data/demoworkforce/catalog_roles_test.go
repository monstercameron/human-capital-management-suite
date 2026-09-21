package demoworkforce

import (
	"testing"

	"github.com/google/uuid"
)

// TestCatalogRolesAreDistinctAndClassified proves the published catalog is one
// coherent list: no job code appears twice, every code the company publishes
// belongs to a unit the organization records, and every one carries the grade,
// family and FLSA classification the rest of the seed reads it through. A rung
// added without its classification would reach the job architecture as a
// profile belonging to no family.
func TestCatalogRolesAreDistinctAndClassified(t *testing.T) {
	units := map[string]bool{}
	for _, unit := range HarborCare.Units {
		units[unit.Code] = true
	}
	seats := allRoles()
	if len(seats) == 0 {
		t.Fatal("the company publishes no roles at all")
	}
	seen := map[string]bool{}
	for _, seat := range seats {
		role := seat.Role
		if seen[role.Code] {
			t.Errorf("job code %s is published twice", role.Code)
		}
		seen[role.Code] = true
		if !units[seat.Unit] {
			t.Errorf("job code %s is published by %q, which is not a recorded organization unit", role.Code, seat.Unit)
		}
		if _, ranked := gradeRank[role.Grade]; !ranked {
			t.Errorf("job code %s carries grade %q, which the grade ladder does not rank", role.Code, role.Grade)
		}
		if _, titled := levelTitles[role.Grade]; !titled {
			t.Errorf("grade %q (job code %s) has no career level title", role.Grade, role.Code)
		}
		if JobFamilyFor(role.Code) == "" {
			t.Errorf("job code %s belongs to no job family", role.Code)
		}
		if status := JobExemptStatusFor(role.Code); status != "EXEMPT" && status != "NON_EXEMPT" {
			t.Errorf("job code %s carries FLSA status %q", role.Code, status)
		}
		if _, exact := exactCents(role.BasePay); !exact {
			t.Errorf("job code %s publishes base pay %q, which is not exact cents", role.Code, role.BasePay)
		}
		if role.Title == "" {
			t.Errorf("job code %s has no title", role.Code)
		}
	}
}

// TestCatalogOnlyRolesAreUnstaffed proves the rungs added to close the ladder's
// gaps changed the catalog and nothing else: the plan still carries exactly the
// sixty workers it did, and none of them holds a catalog-only code. A promotion
// needs a target job with open vacancies, not an incumbent, and the seeded
// population is evidence other lanes' tests already rest on.
func TestCatalogOnlyRolesAreUnstaffed(t *testing.T) {
	employees, err := Plan(uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(employees) != NewWorkerCount {
		t.Fatalf("the plan carries %d workers, want %d", len(employees), NewWorkerCount)
	}
	unstaffed := map[string]string{}
	for _, group := range catalogOnly {
		for _, role := range group.Roles {
			unstaffed[role.Code] = group.Code
		}
	}
	if len(unstaffed) == 0 {
		t.Fatal("the catalog publishes no unstaffed rung; this test would prove nothing")
	}
	for _, employee := range employees {
		if unit, only := unstaffed[employee.Row.JobCode]; only {
			t.Errorf("%s holds %s, which %s publishes as a catalog-only rung", employee.Row.WorkerKey, employee.Row.JobCode, unit)
		}
	}
	// The staffed table and the catalog-only table must not both claim a
	// code: which one wins would then decide whether anybody holds it.
	staffed := map[string]bool{}
	for _, group := range staffing {
		for _, role := range group.Roles {
			staffed[role.Code] = true
		}
	}
	for code := range unstaffed {
		if staffed[code] {
			t.Errorf("job code %s is declared both staffed and catalog-only", code)
		}
	}
}

// TestEveryPublishedRoleIsPricedAndProfiled proves the three catalogs the
// ladder depends on cover the same set of job codes. A rung priced but not
// profiled, or profiled but not priced, is a promotion target whose band
// lookup or whose architecture read comes back empty at the worst moment.
func TestEveryPublishedRoleIsPricedAndProfiled(t *testing.T) {
	specs, err := PayBandSpecs()
	if err != nil {
		t.Fatalf("PayBandSpecs: %v", err)
	}
	architecture, err := JobArchitecture()
	if err != nil {
		t.Fatalf("JobArchitecture: %v", err)
	}
	priced := map[string]int{}
	for _, spec := range specs {
		priced[spec.JobCode]++
	}
	profiled := map[string]bool{}
	for _, profile := range architecture.Profiles {
		profiled[profile.JobCode] = true
	}
	zones := len(PayZones())
	for _, seat := range allRoles() {
		code := seat.Role.Code
		if priced[code] != zones {
			t.Errorf("job code %s is priced in %d pay zones, want %d", code, priced[code], zones)
		}
		if !profiled[code] {
			t.Errorf("job code %s has no published job profile", code)
		}
	}
	if len(priced) != len(allRoles()) {
		t.Errorf("the pay-band policy prices %d job codes, want the %d the company publishes", len(priced), len(allRoles()))
	}
	if len(profiled) != len(allRoles()) {
		t.Errorf("the architecture profiles %d job codes, want the %d the company publishes", len(profiled), len(allRoles()))
	}
}
