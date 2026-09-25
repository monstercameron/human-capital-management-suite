package demoworkforce

import (
	"sort"
	"strings"
)

// A Pack is one fictional demo company: its organization, staffing catalog,
// people, pay policy, career ladder, performance and payroll history, the
// quick-pick personas the dev sign-in page offers and the approvers its
// promotions route to.
//
// Every function in this package that used to read HarborCare's package
// globals is a method on a Pack, and the old package-level functions are thin
// HarborCare wrappers, so a caller that never names a pack keeps exactly the
// behaviour it had. A second company is a second Pack value, resolved by the
// tenant key through [PackFor]; nothing about it is a branch inside the
// HarborCare code path.
type Pack struct {
	// Key is the tenant key the company is served under.
	Key string
	// Company is the company's identity and organization hierarchy.
	Company Company
	// DisplayName is the short name the shell header and the sign-in page
	// show ("HarborCare", "Ironridge Builders").
	DisplayName string
	// Tagline is the one-line description the sign-in page's company
	// selector shows under the name.
	Tagline string
	// LogoAsset is the workspace asset file name of the company's logo.
	LogoAsset string
	// WorkerCount is how many people the plan staffs.
	WorkerCount int
	// Theme is the organization appearance the company's tenant is seeded
	// with, and Logo the brand asset revision its theme points at. Both nil
	// keeps the product's default look (HarborCare).
	Theme *PackTheme
	Logo  *PackLogo

	// Personas are the four quick-pick personas, in display order.
	Personas []PersonaSpec
	// OrgScopeUnit is the organization unit a quick-pick persona's
	// credential is scoped to ("org:<tenant>:<unit>").
	OrgScopeUnit string
	// FinancePartnerKey, ExecutionApproverKey and ManagerApproverKey are the
	// worker keys the company's promotion approvals route to by default.
	FinancePartnerKey    string
	ExecutionApproverKey string
	ManagerApproverKey   string

	keyPrefix        string // "hc" -> hc-001-given-family
	numberPrefix     string // "HC" -> HC-21001
	numberBase       int
	employmentPrefix string
	assignmentPrefix string
	positionPrefix   string
	recordedBy       string

	staffing    []unitStaffing
	catalogOnly []unitStaffing
	roster      []rosterEntry
	givenNames  []string
	familyNames []string
	locations   []location
	// sponsorIndex names, per unit, the index of the worker the unit's head
	// reports to when the unit's own head is not the manager.
	sponsorIndex  map[string]int
	executiveUnit string
	boardRef      string
	photoPrefix   string
	photoFor      func(index int) bool

	costCenters      map[string]string
	onSiteUnits      map[string]bool
	headquarters     string
	partTimeFTE      map[string]string
	fixedTermWorkers map[string]bool
	jobFamilies      map[string]string
	nonExemptJobs    map[string]bool
	hourlyJobs       map[string]bool
	standardHours    int

	gradeRank               map[string]int
	levelTitles             map[string]string
	adjacentFamily          map[string][]string
	executiveStep           map[string][]string
	divisionVicePresident   map[string]string
	divisionLeadershipGrade string
	executiveEntryGrade     string

	payBandPolicyVersion string
	idPrefix             string // "harborcare-demo" -> harborcare-demo.band/...
	ladderVersion        string
	jobArchitectureID    string

	policyPrefix       string // "harborcare" -> harborcare.performance.rating
	performanceCycles  []PerformanceCycleSpec
	peopleOfficerIndex int
	topPerformers      []int
	strongPerformers   []int
	calibrated         []int
	belowExpectation   []int

	payGroups []PayGroupSpec

	personaRoleAssignments map[string][]string
	peopleUnit             string
	financeUnit            string
	roleAssignmentActor    string
	// executiveJobs and financeJobs, when set, name the job codes that hold
	// the executive and finance bundles instead of the E-grade and
	// finance-unit rules HarborCare uses.
	executiveJobs map[string]bool
	financeJobs   map[string]bool
	peopleJobs    map[string]bool
}

// PersonaSpec binds one quick-pick sign-in persona to the worker it signs in
// as and the purpose its credential declares.
type PersonaSpec struct {
	// ID is the persona's slot: admin, hiring-manager, finance-partner or
	// individual-contributor. The role bundle comes from that slot.
	ID           string
	WorkerNumber string
	Access       string
	Purpose      string
}

// PayGroupSpec is one pay group the company runs payroll for: the pay basis
// that places a worker in it and its closed pay periods.
type PayGroupSpec struct {
	Ref     string
	Basis   string
	Periods []PayPeriodSpec
	// RunPrefix prefixes every run id; PolicyPrefix names the group's period
	// and population definitions ("harborcare.payroll").
	RunPrefix    string
	PolicyPrefix string
}

// location is one work location and its pay zone.
type location struct{ Name, Zone string }

// rosterEntry is one explicitly authored person. A pack with a roster plans
// its people from it rather than from the staffing headcounts, which is how
// a company with a real reporting chain (crew -> foreman -> superintendent)
// is expressed: the manager is named, not derived from unit order.
type rosterEntry struct {
	Given, Family string
	Unit, Role    string
	Location      string
	// Manager is the 1-based index of the manager in the roster, or 0 for
	// the top of the company.
	Manager  int
	HireDate string
	// Photo is the index of the seed headshot (person-hc-NNN) the person
	// wears, or 0 for none.
	Photo int
}

// HarborCarePack is the original HarborCare company. Its data is the
// package's own HarborCare tables, unchanged.
var HarborCarePack = &Pack{
	Key: CompanyKey, Company: HarborCare, DisplayName: "HarborCare",
	Tagline:     "Community-care provider · clinical operations, care coordination and a member platform",
	LogoAsset:   "harborcare-logo.svg",
	WorkerCount: NewWorkerCount,
	Personas: []PersonaSpec{
		{ID: "admin", WorkerNumber: "HC-21050", Access: "HCM administrator", Purpose: "compensation_review"},
		{ID: "hiring-manager", WorkerNumber: "HC-21004", Access: "Hiring manager", Purpose: "compensation_review"},
		{ID: "finance-partner", WorkerNumber: "HC-21054", Access: "Finance partner", Purpose: "compensation_review"},
		{ID: "individual-contributor", WorkerNumber: "HC-21051", Access: "Individual contributor", Purpose: "self_service_view"},
	},
	OrgScopeUnit:         "people-ops",
	FinancePartnerKey:    "hc-054-thomas-baker",
	ExecutionApproverKey: "hc-054-thomas-baker",
	ManagerApproverKey:   "hc-052-dominic-collins",

	keyPrefix: "hc", numberPrefix: "HC", numberBase: 21000,
	employmentPrefix: "hc-emp-", assignmentPrefix: "hc-asg-", positionPrefix: "HC-POS-",
	recordedBy: "hcmnext.demo-seed",
	staffing:   staffing, catalogOnly: catalogOnly,
	givenNames: givenNames, familyNames: familyNames, locations: locations,
	sponsorIndex: map[string]int{
		"clinical-operations": 1, "care-coordination": 1, "quality-safety": 1,
		"engineering-platform": 2, "product-management": 2, "data-analytics": 2, "security-it": 2,
		"customer-success": 1, "sales": 1, "marketing": 1,
		"people-operations": 3, "finance": 0, "legal-compliance": 0, "workplace-services": 1,
	},
	executiveUnit: "executive-office", boardRef: "board:harborcare",
	photoPrefix: "hc", photoFor: func(index int) bool { return index%4 != 0 },

	costCenters: costCenters, onSiteUnits: onSiteUnits, headquarters: headquarters,
	partTimeFTE: partTimeFTE, fixedTermWorkers: fixedTermWorkers,
	jobFamilies: jobFamilies, nonExemptJobs: nonExemptJobs, standardHours: 2080,

	gradeRank: gradeRank, levelTitles: levelTitles,
	adjacentFamily: adjacentFamily, executiveStep: executiveStep, divisionVicePresident: divisionVicePresident,
	divisionLeadershipGrade: divisionLeadershipGrade, executiveEntryGrade: executiveEntryGrade,

	payBandPolicyVersion: PayBandPolicyVersion, idPrefix: CompanyKey,
	ladderVersion: PromotionLadderVersion, jobArchitectureID: JobArchitectureID,

	policyPrefix: "harborcare", performanceCycles: PerformanceCycles, peopleOfficerIndex: 3,
	topPerformers: topPerformerOrdinals, strongPerformers: strongPerformerOrdinals,
	calibrated: calibratedOrdinals, belowExpectation: belowExpectationOrdinals,

	payGroups: []PayGroupSpec{{Ref: DemoPayGroupRef, Basis: DemoPayBasis, Periods: DemoPayPeriods,
		RunPrefix: "harborcare-payroll-", PolicyPrefix: "harborcare.payroll"}},

	personaRoleAssignments: DevPersonaRoleAssignments,
	peopleUnit:             PeopleOperationsUnit, financeUnit: FinanceUnit,
	roleAssignmentActor: RoleAssignmentActor,
}

// Packs lists every demo company this build ships, HarborCare first.
func Packs() []*Pack { return []*Pack{HarborCarePack, IronridgePack} }

// PackFor resolves the demo company served under tenantKey.
func PackFor(tenantKey string) (*Pack, bool) {
	key := strings.TrimSpace(tenantKey)
	for _, pack := range Packs() {
		if pack.Key == key {
			return pack, true
		}
	}
	return nil, false
}

// PackKeys lists every shipped pack's tenant key, sorted.
func PackKeys() []string {
	keys := make([]string, 0, len(Packs()))
	for _, pack := range Packs() {
		keys = append(keys, pack.Key)
	}
	sort.Strings(keys)
	return keys
}

// PackForJob resolves the company whose catalog publishes jobCode. Job codes
// are distinct across the shipped packs (TestPackJobCodesAreDisjoint), so
// the answer is unique.
func PackForJob(jobCode string) (*Pack, bool) {
	code := strings.TrimSpace(jobCode)
	for _, pack := range Packs() {
		if _, ok := pack.jobFamilies[code]; ok {
			return pack, true
		}
	}
	return nil, false
}

// PersonaFor returns the pack's quick-pick persona for a slot id.
func (p *Pack) PersonaFor(id string) (PersonaSpec, bool) {
	for _, persona := range p.Personas {
		if persona.ID == id {
			return persona, true
		}
	}
	return PersonaSpec{}, false
}

// OrgScope is the organization scope a quick-pick persona's credential
// carries.
func (p *Pack) OrgScope() string { return "org:" + p.Key + ":" + p.OrgScopeUnit }

// StandardHours is the annual scheduled hours an hourly rate is annualized
// over when it is compared with a salary.
func (p *Pack) StandardHours() int {
	if p.standardHours <= 0 {
		return 2080
	}
	return p.standardHours
}

// IsHourlyJob reports whether the pack pays jobCode by the hour.
func (p *Pack) IsHourlyJob(jobCode string) bool { return p.hourlyJobs[strings.TrimSpace(jobCode)] }

// PayBasisFor is the workforce pay basis token a holder of jobCode is
// recorded under.
func (p *Pack) PayBasisFor(jobCode string) string {
	if p.IsHourlyJob(jobCode) {
		return PayBasisHourly
	}
	return DemoPayBasis
}

// PayBasisHourly is the workforce pay basis of an hourly worker.
const PayBasisHourly = "HOURLY_RATE"

// PayBandPolicyVersion is the pack's published pay-band policy version.
func (p *Pack) PayBandPolicyVersion() string { return p.payBandPolicyVersion }

// Headquarters is the company's head office location.
func (p *Pack) Headquarters() string { return p.headquarters }

// Units is the company's organization hierarchy.
func (p *Pack) Units() []OrganizationUnit { return p.Company.Units }
