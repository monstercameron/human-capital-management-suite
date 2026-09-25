package demoworkforce

// The roles this company publishes but does not staff.
//
// A 47-role catalog with one rung per discipline gives eleven roles exactly
// one next step, and for most of them the reason is a career cliff a real
// company works to avoid: a Senior Product Designer whose only move is a
// directorship, a Senior Care Coordinator whose only move is their own
// manager, a department director with nothing between them and the C-suite.
// The rungs below close those gaps -- the senior individual-contributor step
// above a senior IC, and a division vice-president between a department
// director and an executive seat.
//
// They are deliberately unstaffed. A promotion needs a target job with open
// vacancies, not an incumbent; staffing them would have changed which of the
// sixty planned workers holds which role, and the demo population is a fact
// other lanes' evidence already rests on. [Plan] therefore reads `staffing`
// alone, while the pay-band policy, the job architecture and the career
// ladder read [allRoles], which is both.
//
// Every code here carries its grade, its job family and its FLSA status in
// the same tables the staffed roles use (internal/data/demoworkforce's
// jobFamilies and nonExemptJobs), so nothing about a published job depends
// on whether somebody happens to hold it.
var catalogOnly = []unitStaffing{
	// The senior individual-contributor rungs. Each sits above the unit's
	// existing senior IC and below its management step, so the ladder offers
	// a craft step before it offers a management one.
	{Code: "care-coordination", Roles: []Role{
		{"CARE-CC4", "Lead Care Coordinator", "P4", "95000.00", "0.0800"},
	}},
	{Code: "product-management", Roles: []Role{
		{"PRD-UX4", "Principal Product Designer", "P5", "168000.00", "0.1500"},
	}},
	{Code: "people-operations", Roles: []Role{
		{"PPL-HRBP4", "Principal People Partner", "P5", "152000.00", "0.1500"},
	}},
	// Workplace services has no director, so its manager's next step is a
	// senior manager rather than a principal IC.
	{Code: "workplace-services", Roles: []Role{
		{"WRK-SR", "Senior Workplace Services Manager", "M3", "116000.00", "0.1200"},
	}},

	// The division vice-presidents (grade M5). Each is published by its own
	// division rather than by a department, which is what makes it the next
	// seat for every department head in that division and for nobody outside
	// it: see divisionVicePresident in promotion_paths.go.
	{Code: "care-operations", Roles: []Role{
		{"CARE-VP", "Vice President, Care Operations", "M5", "195000.00", "0.2500"},
	}},
	{Code: "product-technology", Roles: []Role{
		{"PT-VP", "Vice President, Product & Technology", "M5", "225000.00", "0.2500"},
	}},
	{Code: "growth-customer", Roles: []Role{
		{"GC-VP", "Vice President, Growth & Customer", "M5", "210000.00", "0.3000"},
	}},
	{Code: "corporate-services", Roles: []Role{
		{"CORP-VP", "Vice President, Corporate Services", "M5", "200000.00", "0.2500"},
	}},
}

// roleSeat is one published role and the organization unit that publishes it.
type roleSeat struct {
	Unit string
	Role Role
}

// allRoles is every job code this company publishes, staffed or not, in a
// stable order: the staffing table first, then the catalog-only rungs. It is
// the one list the pay-band policy, the job architecture and the career
// ladder are built from, so a rung cannot exist in one of them and be missing
// from another.
//
// A job code declared twice is dropped on its second appearance rather than
// published twice; [TestCatalogRolesAreDistinctAndClassified] proves the real
// catalog never does that.
func allRoles() []roleSeat { return HarborCarePack.allRoles() }

func (p *Pack) allRoles() []roleSeat {
	seats := make([]roleSeat, 0, len(p.staffing)*4+len(p.catalogOnly))
	seen := make(map[string]bool, cap(seats))
	for _, groups := range [][]unitStaffing{p.staffing, p.catalogOnly} {
		for _, group := range groups {
			for _, role := range group.Roles {
				if seen[role.Code] {
					continue
				}
				seen[role.Code] = true
				seats = append(seats, roleSeat{Unit: group.Code, Role: role})
			}
		}
	}
	return seats
}

// JobHomeUnit is the organization unit that publishes a job code, or "" for a
// code this company's catalog does not hold.
//
// It is where a seat for that job belongs, and therefore where its vacancies
// open. Opening them in the unit a promotion is proposed FROM was the defect
// this function exists to close: every unit with somebody pointing at a
// target got its own copy of that target's requisition, so the propose form
// offered a Director of Product vacancy inside Engineering Platform. A seat
// follows its job, not the person reaching for it.
//
// Job codes are distinct across every shipped company, so the lookup reads
// whichever company publishes the code.
func JobHomeUnit(jobCode string) string {
	if pack, ok := PackForJob(jobCode); ok {
		return pack.JobHomeUnit(jobCode)
	}
	return ""
}

// JobHomeUnit is the unit of this company that publishes jobCode.
func (p *Pack) JobHomeUnit(jobCode string) string {
	for _, seat := range p.allRoles() {
		if seat.Role.Code == jobCode {
			return seat.Unit
		}
	}
	return ""
}
