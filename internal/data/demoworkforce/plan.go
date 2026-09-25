// Package demoworkforce defines and seeds the fictional HarborCare workforce
// used by the production Go/WASM demonstration. It is deliberately separate
// from the four-worker conformance corpus: expanding a visual demo must not
// change the frozen promotion vectors.
package demoworkforce

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
)

const (
	CompanyKey     = "harborcare-demo"
	CompanyName    = "HarborCare Health Services"
	NewWorkerCount = 60
	PhotoCount     = 45
)

var demoNamespace = uuid.MustParse("5bd94389-b627-4f25-8755-68e44d53fddd")

type Company struct {
	Key          string
	Name         string
	Description  string
	LegalEntity  string
	Headquarters string
	Units        []OrganizationUnit
}

type OrganizationUnit struct {
	Code       string
	Name       string
	Type       string
	ParentCode string
	Purpose    string
}

type Role struct {
	Code        string
	Title       string
	Grade       string
	BasePay     string
	BonusTarget string
}

type Employee struct {
	Row              workforce.WorkerRow
	JobTitle         string
	Organization     OrganizationUnit
	ManagerKey       string
	HasProfilePhoto  bool
	PhotoSourceName  string
	PhotoOriginalRef string
	PhotoProxyRef    string
}

// HarborCare is a wholly fictional multi-state community-care company. Its
// hierarchy is stable seed metadata, not an inferred reporting structure.
var HarborCare = Company{
	Key:          CompanyKey,
	Name:         CompanyName,
	Description:  "A multi-state community-care provider that combines clinical operations, care coordination, and a digital member platform.",
	LegalEntity:  "HarborCare Health Services, Inc.",
	Headquarters: "Boston, MA",
	Units: []OrganizationUnit{
		{Code: "harborcare", Name: "HarborCare Health Services", Type: "BUSINESS_UNIT", Purpose: "Company-wide governance and operating strategy."},
		{Code: "executive-office", Name: "Executive Office", Type: "DEPARTMENT", ParentCode: "harborcare", Purpose: "Enterprise leadership and operating governance."},
		{Code: "care-operations", Name: "Care Operations", Type: "DIVISION", ParentCode: "harborcare", Purpose: "Safe, consistent delivery of community care."},
		{Code: "clinical-operations", Name: "Clinical Operations", Type: "DEPARTMENT", ParentCode: "care-operations", Purpose: "Clinical practice and regional care delivery."},
		{Code: "care-coordination", Name: "Care Coordination", Type: "DEPARTMENT", ParentCode: "care-operations", Purpose: "Member navigation and continuity of care."},
		{Code: "quality-safety", Name: "Quality & Safety", Type: "DEPARTMENT", ParentCode: "care-operations", Purpose: "Clinical quality, safety, and accreditation."},
		{Code: "product-technology", Name: "Product & Technology", Type: "DIVISION", ParentCode: "harborcare", Purpose: "Digital products and secure technology operations."},
		{Code: "engineering-platform", Name: "Engineering Platform", Type: "DEPARTMENT", ParentCode: "product-technology", Purpose: "Member, clinician, and enterprise software platforms."},
		{Code: "product-management", Name: "Product Management", Type: "DEPARTMENT", ParentCode: "product-technology", Purpose: "Product strategy, discovery, and delivery."},
		{Code: "data-analytics", Name: "Data & Analytics", Type: "DEPARTMENT", ParentCode: "product-technology", Purpose: "Governed analytics and operational intelligence."},
		{Code: "security-it", Name: "Security & IT", Type: "DEPARTMENT", ParentCode: "product-technology", Purpose: "Security, identity, devices, and service operations."},
		{Code: "growth-customer", Name: "Growth & Customer", Type: "DIVISION", ParentCode: "harborcare", Purpose: "Customer partnerships and sustainable growth."},
		{Code: "customer-success", Name: "Customer Success", Type: "DEPARTMENT", ParentCode: "growth-customer", Purpose: "Implementation, adoption, and customer outcomes."},
		{Code: "sales", Name: "Sales", Type: "DEPARTMENT", ParentCode: "growth-customer", Purpose: "Employer and health-plan partnerships."},
		{Code: "marketing", Name: "Marketing", Type: "DEPARTMENT", ParentCode: "growth-customer", Purpose: "Market education, communications, and brand."},
		{Code: "corporate-services", Name: "Corporate Services", Type: "DIVISION", ParentCode: "harborcare", Purpose: "People, financial, legal, and workplace stewardship."},
		{Code: "people-operations", Name: "People Operations", Type: "DEPARTMENT", ParentCode: "corporate-services", Purpose: "Talent, people services, and organizational effectiveness."},
		{Code: "finance", Name: "Finance", Type: "DEPARTMENT", ParentCode: "corporate-services", Purpose: "Planning, accounting, treasury, and procurement."},
		{Code: "legal-compliance", Name: "Legal & Compliance", Type: "DEPARTMENT", ParentCode: "corporate-services", Purpose: "Legal counsel, privacy, and regulatory compliance."},
		{Code: "workplace-services", Name: "Workplace Services", Type: "DEPARTMENT", ParentCode: "corporate-services", Purpose: "Facilities, physical safety, and workplace experience."},
	},
}

type unitStaffing struct {
	Code  string
	Count int
	Roles []Role
}

var staffing = []unitStaffing{
	{Code: "executive-office", Count: 4, Roles: []Role{{"EXEC-CEO", "Chief Executive Officer", "E7", "285000.00", "0.4000"}, {"EXEC-COO", "Chief Operating Officer", "E7", "245000.00", "0.3500"}, {"EXEC-CTO", "Chief Technology Officer", "E7", "250000.00", "0.3500"}, {"EXEC-CPO", "Chief People Officer", "E7", "225000.00", "0.3000"}}},
	{Code: "clinical-operations", Count: 6, Roles: []Role{{"CLN-DIR", "Director of Clinical Operations", "M4", "156000.00", "0.1800"}, {"CLN-RN3", "Senior Registered Nurse", "P4", "118000.00", "0.1000"}, {"CLN-RN2", "Registered Nurse", "P3", "101000.00", "0.0800"}, {"CLN-NP2", "Nurse Practitioner", "P4", "132000.00", "0.1000"}}},
	{Code: "care-coordination", Count: 5, Roles: []Role{{"CARE-MGR", "Care Coordination Manager", "M3", "112000.00", "0.1200"}, {"CARE-CC3", "Senior Care Coordinator", "P3", "82000.00", "0.0700"}, {"CARE-CC2", "Care Coordinator", "P2", "69000.00", "0.0500"}}},
	{Code: "quality-safety", Count: 4, Roles: []Role{{"QLT-DIR", "Director of Quality & Safety", "M4", "148000.00", "0.1800"}, {"QLT-PS3", "Patient Safety Specialist", "P3", "96000.00", "0.0800"}, {"QLT-DA3", "Clinical Quality Analyst", "P3", "92000.00", "0.0800"}}},
	{Code: "engineering-platform", Count: 7, Roles: []Role{{"ENG-DIR", "Director of Engineering", "M4", "198000.00", "0.2000"}, {"ENG-SWE4", "Staff Software Engineer", "P5", "184000.00", "0.1800"}, {"ENG-SWE3", "Senior Software Engineer", "P4", "158000.00", "0.1500"}, {"ENG-SRE3", "Site Reliability Engineer", "P4", "162000.00", "0.1500"}, {"ENG-QA3", "Quality Engineer", "P3", "126000.00", "0.1000"}}},
	{Code: "product-management", Count: 4, Roles: []Role{{"PRD-DIR", "Director of Product", "M4", "188000.00", "0.2000"}, {"PRD-PM3", "Senior Product Manager", "P4", "158000.00", "0.1500"}, {"PRD-UX3", "Senior Product Designer", "P4", "146000.00", "0.1200"}}},
	{Code: "data-analytics", Count: 4, Roles: []Role{{"DAT-DIR", "Director of Data", "M4", "190000.00", "0.2000"}, {"DAT-DE3", "Senior Data Engineer", "P4", "156000.00", "0.1500"}, {"DAT-AN3", "Senior Data Analyst", "P3", "122000.00", "0.1000"}}},
	{Code: "security-it", Count: 3, Roles: []Role{{"SEC-DIR", "Director of Security & IT", "M4", "188000.00", "0.2000"}, {"SEC-SE3", "Security Engineer", "P4", "160000.00", "0.1500"}, {"IT-SA2", "IT Systems Administrator", "P3", "108000.00", "0.0800"}}},
	{Code: "customer-success", Count: 5, Roles: []Role{{"CS-DIR", "Director of Customer Success", "M4", "165000.00", "0.2200"}, {"CS-CSM3", "Senior Customer Success Manager", "P4", "128000.00", "0.1800"}, {"CS-IMP3", "Implementation Manager", "P3", "112000.00", "0.1200"}}},
	{Code: "sales", Count: 4, Roles: []Role{{"SAL-DIR", "Sales Director", "M4", "170000.00", "0.3500"}, {"SAL-AE3", "Senior Account Executive", "P4", "135000.00", "0.4000"}, {"SAL-SOL3", "Solutions Consultant", "P3", "125000.00", "0.1800"}}},
	{Code: "marketing", Count: 3, Roles: []Role{{"MKT-DIR", "Marketing Director", "M4", "158000.00", "0.1800"}, {"MKT-CNT3", "Content Strategy Lead", "P3", "112000.00", "0.1000"}, {"MKT-DG3", "Demand Generation Manager", "P3", "118000.00", "0.1200"}}},
	{Code: "people-operations", Count: 4, Roles: []Role{{"PPL-DIR", "Director of People Operations", "M4", "165000.00", "0.1800"}, {"PPL-HRBP3", "Senior People Partner", "P4", "132000.00", "0.1200"}, {"PPL-TA3", "Talent Acquisition Partner", "P3", "108000.00", "0.1000"}}},
	{Code: "finance", Count: 3, Roles: []Role{{"FIN-DIR", "Finance Director", "M4", "172000.00", "0.2000"}, {"FIN-FPA3", "Senior FP&A Analyst", "P3", "126000.00", "0.1200"}, {"FIN-ACC3", "Senior Accountant", "P3", "112000.00", "0.1000"}}},
	{Code: "legal-compliance", Count: 2, Roles: []Role{{"LEG-GC", "General Counsel", "E6", "215000.00", "0.2800"}, {"LEG-CMP3", "Compliance Manager", "P4", "136000.00", "0.1200"}}},
	{Code: "workplace-services", Count: 2, Roles: []Role{{"WRK-MGR", "Workplace Services Manager", "M2", "98000.00", "0.1000"}, {"WRK-CO2", "Workplace Coordinator", "P2", "68000.00", "0.0500"}}},
}

var givenNames = []string{"Amina", "Mateo", "Evelyn", "Darius", "Mei", "Jonah", "Fatima", "Lucas", "Nia", "Theodore", "Sofia", "Malik", "Anika", "Gabriel", "Rosa", "Ethan", "Layla", "Henry", "Zuri", "Daniel", "Maya", "Samuel", "Imani", "Leo", "Valentina", "Julian", "Priyanka", "Caleb", "Amara", "Felix", "Lucia", "Micah", "Aya", "Nathan", "Elena", "Andre", "Mina", "Owen", "Leila", "Marcus", "Isabel", "Victor", "Camila", "Adrian", "Naomi", "Hugo", "Soraya", "Benjamin", "Jasmine", "Rafael", "Linh", "Dominic", "Selene", "Thomas", "Khadija", "Peter", "Yara", "Wesley", "Marisol", "Isaac"}
var familyNames = []string{"Rahman", "Alvarez", "Morgan", "Bennett", "Chen", "Foster", "Okafor", "Silva", "Brooks", "Wright", "Petrov", "Johnson", "Desai", "Martinez", "Santos", "Kim", "Hassan", "Clarke", "Mensah", "Nguyen", "Patel", "Rivera", "Thompson", "Garcia", "Rossi", "Miller", "Sharma", "Williams", "Diallo", "Laurent", "Romano", "Carter", "Tanaka", "Evans", "Vega", "Lewis", "Sato", "Turner", "Mansour", "Reed", "Costa", "Diaz", "Morales", "Young", "King", "Dubois", "Azizi", "Scott", "Price", "Torres", "Tran", "Collins", "Navarro", "Baker", "Ibrahim", "Murphy", "Saleh", "Cooper", "Herrera", "Ward"}

var locations = []location{{"Boston, MA", "US-EAST"}, {"Atlanta, GA", "US-EAST"}, {"Chicago, IL", "US-CENTRAL"}, {"Dallas, TX", "US-CENTRAL"}, {"San Francisco, CA", "US-WEST"}, {"Seattle, WA", "US-WEST"}, {"New York, NY", "US-EAST"}, {"Denver, CO", "US-MOUNTAIN"}}

// Plan is HarborCare's workforce plan.
func Plan(tenant uuid.UUID) ([]Employee, error) { return HarborCarePack.Plan(tenant) }

// Plan derives the company's deterministic workforce for tenant.
func (p *Pack) Plan(tenant uuid.UUID) ([]Employee, error) {
	if tenant == uuid.Nil {
		return nil, fmt.Errorf("demoworkforce: tenant is required")
	}
	if len(p.roster) > 0 {
		return p.planRoster(tenant)
	}
	units := make(map[string]OrganizationUnit, len(p.Company.Units))
	for _, unit := range p.Company.Units {
		units[unit.Code] = unit
	}
	if len(p.givenNames) != p.WorkerCount || len(p.familyNames) != p.WorkerCount {
		return nil, fmt.Errorf("demoworkforce: name corpus must contain %d entries", p.WorkerCount)
	}

	recordedAt := time.Date(2026, time.September, 1, 14, 0, 0, 0, time.UTC)
	employees := make([]Employee, 0, p.WorkerCount)
	firstByUnit := map[string]string{}
	staffIndex := 0
	for _, group := range p.staffing {
		unit, ok := units[group.Code]
		if !ok || len(group.Roles) == 0 {
			return nil, fmt.Errorf("demoworkforce: staffing references invalid unit %q", group.Code)
		}
		for position := 0; position < group.Count; position++ {
			index := staffIndex + 1
			role := staffRoleAt(group.Roles, position)
			key := fmt.Sprintf("%s-%03d-%s-%s", p.keyPrefix, index, slug(p.givenNames[staffIndex]), slug(p.familyNames[staffIndex]))
			if position == 0 {
				firstByUnit[group.Code] = key
			}
			location := p.locations[(staffIndex+position)%len(p.locations)]
			hireYear := 2016 + staffIndex%10
			hireMonth := 1 + staffIndex%12
			hireDay := 1 + staffIndex%27
			employee, err := p.plannedEmployee(tenant, index, key, p.givenNames[staffIndex], p.familyNames[staffIndex], role, unit, location,
				fmt.Sprintf("%04d-%02d-%02d", hireYear, hireMonth, hireDay), p.photoIndex(index), recordedAt.Add(time.Duration(staffIndex)*time.Minute))
			if err != nil {
				return nil, err
			}
			employees = append(employees, employee)
			staffIndex++
		}
	}
	if len(employees) != p.WorkerCount {
		return nil, fmt.Errorf("demoworkforce: planned %d workers, want %d", len(employees), p.WorkerCount)
	}

	sponsors := make(map[string]string, len(p.sponsorIndex))
	for unit, sponsor := range p.sponsorIndex {
		sponsors[unit] = employees[sponsor].Row.WorkerKey
	}
	for index := range employees {
		unitCode := employees[index].Organization.Code
		manager := firstByUnit[unitCode]
		if manager == employees[index].Row.WorkerKey {
			manager = sponsors[unitCode]
		}
		if unitCode == p.executiveUnit {
			manager = employees[0].Row.WorkerKey
			if index == 0 {
				manager = p.boardRef
			}
		}
		employees[index].ManagerKey = manager
		employees[index].Row.ManagerRelationshipRef = manager
	}
	return employees, nil
}

// plannedEmployee builds one planned worker row from the facts the plan
// fixes for them. Every derived fact (business unit, cost center, FTE, time
// type, work arrangement, pay basis) comes from the pack's own tables.
func (p *Pack) plannedEmployee(tenant uuid.UUID, index int, key, given, family string, role Role, unit OrganizationUnit,
	place location, hireDate string, photo int, recorded time.Time) (Employee, error) {
	businessUnit, err := p.businessUnitFor(unit.Code)
	if err != nil {
		return Employee{}, err
	}
	costCenter, err := p.costCenterFor(unit.Code)
	if err != nil {
		return Employee{}, err
	}
	fte := p.fteFor(key)
	hasPhoto := photo > 0
	photoSource, originalRef, proxyRef := "", "", ""
	if hasPhoto {
		photoSource = fmt.Sprintf("%s-%03d.png", p.photoPrefix, photo)
		originalRef = "profile-originals/" + photoSource
		proxyRef = fmt.Sprintf("/workspace/assets/person-%s-%03d-small.jpg", p.photoPrefix, photo)
	}
	return Employee{
		Row: workforce.WorkerRow{
			TenantID: tenant, WorkerID: deterministicID("worker", key), WorkerKey: key,
			LegalName: given + " " + family, PreferredName: given, WorkerNumber: fmt.Sprintf("%s-%05d", p.numberPrefix, p.numberBase+index),
			WorkerType: "employee", LifecycleStatus: "active", EmploymentID: fmt.Sprintf("%s%05d", p.employmentPrefix, index), AssignmentID: fmt.Sprintf("%s%05d", p.assignmentPrefix, index),
			JobCode: role.Code, JobTitle: role.Title, Grade: role.Grade, OrgUnit: unit.Code, PositionID: fmt.Sprintf("%s%05d", p.positionPrefix, index), Location: place.Name, PayZone: place.Zone, FTE: fte,
			EmploymentType: p.employmentTypeFor(key), TimeType: timeTypeFor(fte),
			Company: p.Company.LegalEntity, BusinessUnit: businessUnit, CostCenter: costCenter,
			WorkArrangement: p.workArrangementFor(unit.Code, place.Name),
			HireDate:        hireDate, EffectiveFrom: "2026-01-01", BasePay: role.BasePay, Currency: "USD", PayBasis: p.PayBasisFor(role.Code), BonusTarget: role.BonusTarget,
			RevisionStream: "people.worker." + key, RevisionSequence: 1, KnownAt: recorded, RecordedAt: recorded, CreatedBy: p.recordedBy, Source: workforce.SourceCreated,
			ProfilePhotoOriginalRef: originalRef, ProfilePhotoProxyRef: proxyRef,
		},
		JobTitle: role.Title, Organization: unit, HasProfilePhoto: hasPhoto, PhotoSourceName: photoSource, PhotoOriginalRef: originalRef, PhotoProxyRef: proxyRef,
	}, nil
}

// planRoster plans a pack whose people are authored one by one.
func (p *Pack) planRoster(tenant uuid.UUID) ([]Employee, error) {
	if len(p.roster) != p.WorkerCount {
		return nil, fmt.Errorf("demoworkforce: roster holds %d people, want %d", len(p.roster), p.WorkerCount)
	}
	units := make(map[string]OrganizationUnit, len(p.Company.Units))
	for _, unit := range p.Company.Units {
		units[unit.Code] = unit
	}
	roles := map[string]Role{}
	for _, group := range p.staffing {
		for _, role := range group.Roles {
			roles[group.Code+"|"+role.Code] = role
		}
	}
	places := make(map[string]location, len(p.locations))
	for _, place := range p.locations {
		places[place.Name] = place
	}
	recordedAt := time.Date(2026, time.September, 1, 14, 0, 0, 0, time.UTC)
	employees := make([]Employee, 0, len(p.roster))
	for position, entry := range p.roster {
		index := position + 1
		unit, ok := units[entry.Unit]
		if !ok {
			return nil, fmt.Errorf("demoworkforce: roster entry %d references invalid unit %q", index, entry.Unit)
		}
		role, ok := roles[entry.Unit+"|"+entry.Role]
		if !ok {
			return nil, fmt.Errorf("demoworkforce: roster entry %d references role %q not staffed in %q", index, entry.Role, entry.Unit)
		}
		place, ok := places[entry.Location]
		if !ok {
			return nil, fmt.Errorf("demoworkforce: roster entry %d references unknown location %q", index, entry.Location)
		}
		key := fmt.Sprintf("%s-%03d-%s-%s", p.keyPrefix, index, slug(entry.Given), slug(entry.Family))
		employee, err := p.plannedEmployee(tenant, index, key, entry.Given, entry.Family, role, unit, place,
			entry.HireDate, entry.Photo, recordedAt.Add(time.Duration(position)*time.Minute))
		if err != nil {
			return nil, err
		}
		employees = append(employees, employee)
	}
	for position, entry := range p.roster {
		manager := p.boardRef
		if entry.Manager > 0 {
			if entry.Manager > len(employees) || entry.Manager == position+1 {
				return nil, fmt.Errorf("demoworkforce: roster entry %d names invalid manager %d", position+1, entry.Manager)
			}
			manager = employees[entry.Manager-1].Row.WorkerKey
		}
		employees[position].ManagerKey = manager
		employees[position].Row.ManagerRelationshipRef = manager
	}
	return employees, nil
}

// staffRoleAt picks the role one position in a unit is staffed with.
//
// Position 0 is the unit's leader, and only position 0. The remaining roles
// cycle among themselves, so a unit whose headcount exceeds its role list
// repeats an individual-contributor title rather than re-issuing the
// leadership one.
//
// The plain `position % len(roles)` this replaces wrapped back onto index 0,
// which gave nine of the fifteen staffed units a second director or manager
// carrying the same title as the first -- and, because the unit's manager is
// whoever holds position 0, that second director then reported to somebody
// holding their own job. "Nia Brooks, Director of Clinical Operations,
// reports to Mei Chen, Director of Clinical Operations" is not a record a
// reader can make sense of.
//
// For a unit whose headcount fits its role list this is exactly the previous
// mapping: position p in [1, len(roles)-1] still resolves to roles[p]. Only
// the positions that used to wrap move, so the job-code catalog, the pay
// bands derived from it and the promotion ladder built on it are unchanged.
func staffRoleAt(roles []Role, position int) Role {
	if position <= 0 || len(roles) < 2 {
		return roles[0]
	}
	return roles[1+(position-1)%(len(roles)-1)]
}

func deterministicID(kind, key string) uuid.UUID {
	return uuid.NewSHA1(demoNamespace, []byte(kind+"\x00"+key))
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			out.WriteRune(r)
		} else if out.Len() > 0 && !strings.HasSuffix(out.String(), "-") {
			out.WriteByte('-')
		}
	}
	return strings.Trim(out.String(), "-")
}

// photoIndex is the seed headshot a staffing-planned worker at 1-based index
// wears, or 0 for none. HarborCare's worker N wears headshot N.
func (p *Pack) photoIndex(index int) int {
	if p.photoFor != nil && p.photoFor(index) {
		return index
	}
	return 0
}
