package demoworkforce

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
)

// This file holds the employment facts the worker object page shows beside a
// person's placement: the employment and time types, the employing company,
// the business unit, the cost center, the work arrangement, and -- for the
// job catalog rather than the worker -- the job family and FLSA exempt
// status.
//
// They live here rather than inside the plan so that the staffing table stays
// a list of roles and headcounts. Every value is a pure function of data the
// plan already fixes (the org unit, the job code, the work location, the
// FTE), so the demo stays a deterministic function of its inputs: no clock,
// no RNG, no map iteration order.
//
// The overriding rule is that nothing here may contradict anything already
// recorded. A PART_TIME token accompanies a fractional FTE and nothing else.
// An FLSA status agrees with the role's duties and with its pay basis. A work
// arrangement agrees with the worker's location and with what the unit's work
// physically requires.

// costCenters is the general-ledger cost center each staffed unit charges to.
//
// The codes are grouped by division -- 1000 executive, 2000 care, 3000
// product and technology, 4000 growth, 5000 corporate -- so that a reader
// scanning a report can tell which part of the company a charge came from
// without a lookup, which is how a real chart of accounts is numbered. They
// are stable: a cost center that moved between reporting periods would make
// every historical charge ambiguous.
var costCenters = map[string]string{
	"executive-office":     "CC-1000 Executive Office",
	"clinical-operations":  "CC-2100 Clinical Operations",
	"care-coordination":    "CC-2200 Care Coordination",
	"quality-safety":       "CC-2300 Quality & Safety",
	"engineering-platform": "CC-3100 Engineering Platform",
	"product-management":   "CC-3200 Product Management",
	"data-analytics":       "CC-3300 Data & Analytics",
	"security-it":          "CC-3400 Security & IT",
	"customer-success":     "CC-4100 Customer Success",
	"sales":                "CC-4200 Sales",
	"marketing":            "CC-4300 Marketing",
	"people-operations":    "CC-5100 People Operations",
	"finance":              "CC-5200 Finance",
	"legal-compliance":     "CC-5300 Legal & Compliance",
	"workplace-services":   "CC-5400 Workplace Services",
}

// onSiteUnits are the units whose work is performed where the members and the
// buildings are. Community care is delivered in homes and clinics and
// workplace services maintain the sites themselves, so nobody in these units
// is remote regardless of which city they are based in.
var onSiteUnits = map[string]bool{
	"clinical-operations": true,
	"care-coordination":   true,
	"quality-safety":      true,
	"workplace-services":  true,
}

// headquarters is HarborCare's only office. Everyone else works from where
// they live, which is what makes the work arrangement a function of the
// recorded work location rather than a separate assertion about it.
const headquarters = "Boston, MA"

// partTimeFTE names the workers who hold less than a full allocation, keyed by
// worker key, with the FTE they actually hold.
//
// Both are coordinator roles at the bottom of the professional ladder, which
// is where a real community-care employer carries reduced schedules: a
// job-share on care coordination and a part-week workplace coordinator. Their
// base pay stays the role's full-time-equivalent annual rate, because that is
// the figure pay_bands.go builds the published band around and the figure a
// compa-ratio is computed against; the allocation, not the rate, is what is
// part time.
var partTimeFTE = map[string]string{
	// Care Coordinator, job-sharing a caseload.
	"hc-015-rosa-santos": "0.6000",
	// Workplace Coordinator, four days a week.
	"hc-060-isaac-ward": "0.8000",
}

// fixedTermWorkers are the workers on an agreed end date rather than an
// open-ended relationship. They remain EMPLOYEE workers: fixed term is a
// question about the term of the employment, not about whether somebody is an
// employee at all.
//
// One is a quality analyst covering an accreditation cycle, the other an
// implementation manager attached to a single customer programme -- the two
// places a care company genuinely staffs against a finite piece of work.
var fixedTermWorkers = map[string]bool{
	// Clinical Quality Analyst, engaged for an accreditation cycle.
	"hc-018-henry-clarke": true,
	// Implementation Manager, attached to one customer programme.
	"hc-040-marcus-reed": true,
}

// jobFamilies groups the 47 job codes into the families a job architecture is
// actually navigated by. The family is a property of the job, not of the
// person holding it, which is why it is recorded on the job catalog row.
var jobFamilies = map[string]string{
	"EXEC-CEO": "Executive Leadership", "EXEC-COO": "Executive Leadership",
	"EXEC-CTO": "Executive Leadership", "EXEC-CPO": "Executive Leadership",
	"LEG-GC": "Executive Leadership",

	"CLN-DIR": "Nursing & Clinical Practice", "CLN-RN3": "Nursing & Clinical Practice",
	"CLN-RN2": "Nursing & Clinical Practice", "CLN-NP2": "Nursing & Clinical Practice",

	"CARE-MGR": "Care Coordination", "CARE-CC3": "Care Coordination", "CARE-CC2": "Care Coordination",

	"QLT-DIR": "Quality & Patient Safety", "QLT-PS3": "Quality & Patient Safety",
	"QLT-DA3": "Quality & Patient Safety",

	"ENG-DIR": "Software Engineering", "ENG-SWE4": "Software Engineering",
	"ENG-SWE3": "Software Engineering", "ENG-SRE3": "Software Engineering",
	"ENG-QA3": "Software Engineering",

	"PRD-DIR": "Product Management", "PRD-PM3": "Product Management",
	"PRD-UX3": "Product Design",

	"DAT-DIR": "Data & Analytics", "DAT-DE3": "Data & Analytics", "DAT-AN3": "Data & Analytics",

	"SEC-DIR": "Information Security", "SEC-SE3": "Information Security",
	"IT-SA2": "IT Operations",

	"CS-DIR": "Customer Success", "CS-CSM3": "Customer Success", "CS-IMP3": "Customer Success",

	"SAL-DIR": "Sales", "SAL-AE3": "Sales", "SAL-SOL3": "Sales Engineering",

	"MKT-DIR": "Marketing", "MKT-CNT3": "Marketing", "MKT-DG3": "Marketing",

	"PPL-DIR": "Human Resources", "PPL-HRBP3": "Human Resources", "PPL-TA3": "Human Resources",

	"FIN-DIR": "Finance & Accounting", "FIN-FPA3": "Finance & Accounting",
	"FIN-ACC3": "Finance & Accounting",

	"LEG-CMP3": "Legal & Compliance",

	"WRK-MGR": "Workplace Services", "WRK-CO2": "Workplace Services",

	// The catalog-only rungs (catalog_roles.go). They are classified here
	// with the staffed roles rather than beside themselves: a job's family
	// is a property of the job, and whether anybody holds it yet is not.
	"CARE-CC4":  "Care Coordination",
	"PRD-UX4":   "Product Design",
	"PPL-HRBP4": "Human Resources",
	"WRK-SR":    "Workplace Services",

	// The division vice-presidencies are general management: the discipline
	// is running a division, not the craft the division practises.
	"CARE-VP": "General Management", "PT-VP": "General Management",
	"GC-VP": "General Management", "CORP-VP": "General Management",
}

// nonExemptJobs are the job codes whose duties fail the FLSA exemption tests,
// so the holder is overtime-eligible.
//
// The line is drawn on duties, not on pay or seniority, which is why it
// separates roles the old hard-coded "EXEMPT" lumped together. Care
// coordinators follow a care plan somebody else approved rather than
// exercising independent judgement on matters of significance, so neither the
// professional nor the administrative exemption reaches them -- a distinction
// their manager (CARE-MGR, exempt under the executive test) does not share. A
// systems administrator maintains systems rather than designing or modifying
// them, which is the line the computer-employee exemption is drawn on and the
// reason IT-SA2 differs from every ENG- code. A workplace coordinator does
// scheduling and support work.
//
// The analysts stay exempt: a clinical quality analyst, a data analyst and an
// FP&A analyst each exercise discretion over analyses the business relies on,
// which is the administrative and learned-professional ground the exemption
// actually rests on. So do the nurses, who are licensed professionals paid on
// a salary basis.
//
// Every one of these roles is salaried (plan.go writes ANNUAL_SALARY for all
// 60), and salaried non-exempt is a coherent, common classification: the
// exemption turns on duties, and the salary basis only sets a floor the
// exemption additionally requires. No role here is hourly, because every
// published pay band in pay_bands.go is an annual figure and an hourly rate
// recorded as base pay would fall outside the band its own job publishes.
var nonExemptJobs = map[string]bool{
	"CARE-CC2": true,
	"CARE-CC3": true,
	// A lead care coordinator leads the queue and the hand-offs; the care
	// plan is still somebody else's clinical judgement, so the same duties
	// reasoning that reaches CARE-CC3 reaches this rung too.
	"CARE-CC4": true,
	"IT-SA2":   true,
	"WRK-CO2":  true,
}

// businessUnitFor is the unit's parent line: the division a department rolls
// up into, or the company itself for a unit that hangs directly off the root.
//
// It is walked from the recorded hierarchy rather than listed a second time,
// so a unit that is re-parented reports the new line without an edit here.
func businessUnitFor(unitCode string) (string, error) {
	units := make(map[string]OrganizationUnit, len(HarborCare.Units))
	for _, unit := range HarborCare.Units {
		units[unit.Code] = unit
	}
	unit, ok := units[unitCode]
	if !ok {
		return "", fmt.Errorf("demoworkforce: no organization unit %q", unitCode)
	}
	// A unit with no parent is its own line; that is the root business unit.
	if unit.ParentCode == "" {
		return unit.Name, nil
	}
	parent, ok := units[unit.ParentCode]
	if !ok {
		return "", fmt.Errorf("demoworkforce: organization unit %q names a parent %q that does not exist", unitCode, unit.ParentCode)
	}
	return parent.Name, nil
}

// costCenterFor is the unit's charge code.
func costCenterFor(unitCode string) (string, error) {
	code, ok := costCenters[unitCode]
	if !ok {
		return "", fmt.Errorf("demoworkforce: no cost center recorded for organization unit %q", unitCode)
	}
	return code, nil
}

// workArrangementFor decides where the work is performed from the two facts
// that actually settle it: what the unit's work requires, and whether the
// worker is based at the one office the company has.
func workArrangementFor(unitCode, location string) string {
	if onSiteUnits[unitCode] {
		return workforce.WorkArrangementOnSite
	}
	if strings.TrimSpace(location) == headquarters {
		return workforce.WorkArrangementHybrid
	}
	return workforce.WorkArrangementRemote
}

// employmentTypeFor is REGULAR unless the worker is one of the two on a fixed
// term.
func employmentTypeFor(workerKey string) string {
	if fixedTermWorkers[workerKey] {
		return workforce.EmploymentTypeFixedTerm
	}
	return workforce.EmploymentTypeRegular
}

// fteFor is the worker's allocation: a full unit unless the worker holds one
// of the recorded reduced schedules.
func fteFor(workerKey string) string {
	if fte, ok := partTimeFTE[workerKey]; ok {
		return fte
	}
	return "1.0000"
}

// timeTypeFor is derived from the allocation rather than asserted beside it,
// so the two can never disagree -- which is exactly what
// workforce.WorkerRow.Validate refuses.
func timeTypeFor(fte string) string {
	if fte == "1.0000" {
		return workforce.TimeTypeFullTime
	}
	return workforce.TimeTypePartTime
}

// JobFamilyFor is the family a job code belongs to, and JobExemptStatusFor its
// FLSA classification. They are exported because the aggregate projection
// records them on the job catalog row, which is a different package's call.
func JobFamilyFor(jobCode string) string {
	return jobFamilies[strings.TrimSpace(jobCode)]
}

// JobExemptStatusFor is the FLSA status the job's duties support.
//
// A code this package does not classify is EXEMPT, which is the safe default
// only because the catalog jobs that reach it (the promotion catalog's own
// targets) are all professional roles; every one of HarborCare's 47 codes is
// classified explicitly and TestHarborCareJobCatalogFactsAreComplete proves
// it.
func JobExemptStatusFor(jobCode string) string {
	if nonExemptJobs[strings.TrimSpace(jobCode)] {
		return "NON_EXEMPT"
	}
	return "EXEMPT"
}
