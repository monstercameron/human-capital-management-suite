package demoworkforce

import (
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

var ironridgeTestTenant = uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")

func TestPackRegistryResolvesBothCompaniesAndOnlyThem(t *testing.T) {
	harbor, ok := PackFor("harborcare-demo")
	if !ok || harbor != HarborCarePack || harbor.Company.Name != HarborCare.Name {
		t.Fatalf("PackFor(harborcare-demo) = %v %v", harbor, ok)
	}
	iron, ok := PackFor(" ironridge-demo ")
	if !ok || iron != IronridgePack || iron.DisplayName != "Ironridge Builders" {
		t.Fatalf("PackFor(ironridge-demo) = %v %v", iron, ok)
	}
	if _, ok := PackFor("another-tenant"); ok {
		t.Fatal("an unknown tenant resolved to a demo company")
	}
	if got := strings.Join(PackKeys(), ","); got != "harborcare-demo,ironridge-demo" {
		t.Fatalf("PackKeys = %s", got)
	}
	if HarborCarePack.OrgScope() != "org:harborcare-demo:people-ops" || IronridgePack.OrgScope() != "org:ironridge-demo:people" {
		t.Fatalf("org scopes = %s %s", HarborCarePack.OrgScope(), IronridgePack.OrgScope())
	}
}

// TestPackJobCodesAreDisjoint is what lets JobFamilyFor, JobExemptStatusFor
// and JobHomeUnit resolve a job code without being told the company.
func TestPackJobCodesAreDisjoint(t *testing.T) {
	owner := map[string]string{}
	for _, pack := range Packs() {
		for _, seat := range pack.allRoles() {
			if other, taken := owner[seat.Role.Code]; taken {
				t.Fatalf("job code %s is published by %s and %s", seat.Role.Code, other, pack.Key)
			}
			owner[seat.Role.Code] = pack.Key
			if found, ok := PackForJob(seat.Role.Code); !ok || found != pack {
				t.Fatalf("PackForJob(%s) = %v", seat.Role.Code, found)
			}
			if JobFamilyFor(seat.Role.Code) == "" || JobHomeUnit(seat.Role.Code) != seat.Unit {
				t.Fatalf("%s: family %q home %q", seat.Role.Code, JobFamilyFor(seat.Role.Code), JobHomeUnit(seat.Role.Code))
			}
		}
	}
	if JobExemptStatusFor("IR-JCP") != "NON_EXEMPT" || JobExemptStatusFor("IR-SUP") != "EXEMPT" {
		t.Fatal("Ironridge FLSA classification did not resolve through the package lookup")
	}
	if JobExemptStatusFor("CARE-CC2") != "NON_EXEMPT" || JobFamilyFor("ENG-SWE3") != "Software Engineering" {
		t.Fatal("HarborCare classification changed")
	}
}

func TestIronridgePlanIsA38PersonConstructionCompany(t *testing.T) {
	employees, err := IronridgePack.Plan(ironridgeTestTenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(employees) != 38 {
		t.Fatalf("workers = %d, want 38", len(employees))
	}
	wantUnits := map[string]int{
		"executive": 3, "project-management": 4, "field-operations": 18, "estimating-preconstruction": 2,
		"safety-quality": 2, "finance-admin": 3, "people": 2, "business-development": 1, "warranty-service": 3,
	}
	gotUnits := map[string]int{}
	keys := map[string]bool{"board:ironridge": true}
	for _, employee := range employees {
		keys[employee.Row.WorkerKey] = true
	}
	keyPattern := regexp.MustCompile(`^ir-0\d\d-[a-z]+(-[a-z]+)+$`)
	numberPattern := regexp.MustCompile(`^IR-0\d{4}$`)
	photos, hourly := 0, 0
	seenPhoto := map[string]bool{}
	for _, employee := range employees {
		row := employee.Row
		gotUnits[row.OrgUnit]++
		if err := row.Validate(); err != nil {
			t.Errorf("%s: %v", row.WorkerKey, err)
		}
		if !keyPattern.MatchString(row.WorkerKey) || !numberPattern.MatchString(row.WorkerNumber) {
			t.Errorf("identity %s / %s does not follow ir-0NN-given-family / IR-0NNNN", row.WorkerKey, row.WorkerNumber)
		}
		if !keys[employee.ManagerKey] || employee.ManagerKey == row.WorkerKey {
			t.Errorf("%s reports to unknown manager %q", row.WorkerKey, employee.ManagerKey)
		}
		if row.Company != "Ironridge Builders, Inc." || row.PayZone != "US-MOUNTAIN" || row.Currency != "USD" {
			t.Errorf("%s company/zone/currency = %s %s %s", row.WorkerKey, row.Company, row.PayZone, row.Currency)
		}
		cents, exact := exactCents(row.BasePay)
		if !exact {
			t.Fatalf("%s base pay %q is not exact cents", row.WorkerKey, row.BasePay)
		}
		switch row.PayBasis {
		case PayBasisHourly:
			hourly++
			if cents < 1900 || cents > 4200 {
				t.Errorf("%s hourly rate %s outside the Denver craft range", row.WorkerKey, row.BasePay)
			}
		case DemoPayBasis:
			if cents < 5200000 || cents > 26000000 {
				t.Errorf("%s salary %s outside the published range", row.WorkerKey, row.BasePay)
			}
		default:
			t.Errorf("%s pay basis %q", row.WorkerKey, row.PayBasis)
		}
		if employee.HasProfilePhoto {
			photos++
			if seenPhoto[employee.PhotoProxyRef] {
				t.Errorf("headshot %s worn twice", employee.PhotoProxyRef)
			}
			seenPhoto[employee.PhotoProxyRef] = true
			if !regexp.MustCompile(`^/workspace/assets/person-hc-\d{3}-small\.jpg$`).MatchString(employee.PhotoProxyRef) {
				t.Errorf("%s photo %s is not a shipped seed headshot", row.WorkerKey, employee.PhotoProxyRef)
			}
		}
	}
	for unit, want := range wantUnits {
		if gotUnits[unit] != want {
			t.Errorf("unit %s staffs %d, want %d", unit, gotUnits[unit], want)
		}
	}
	if photos != 34 {
		t.Errorf("photos = %d, want 34 of 38", photos)
	}
	if hourly != 19 {
		t.Errorf("hourly workers = %d, want the 16 field and 3 service craft", hourly)
	}
}

// TestIronridgeReportingChainIsCrewForemanSuperintendentVPOwner walks the
// chain the brief names: crew -> foreman -> superintendent -> VP Ops ->
// owner, and office -> controller or HR manager -> owner.
func TestIronridgeReportingChainIsCrewForemanSuperintendentVPOwner(t *testing.T) {
	employees, err := IronridgePack.Plan(ironridgeTestTenant)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]Employee{}
	for _, employee := range employees {
		byKey[employee.Row.WorkerKey] = employee
	}
	chain := func(key string) []string {
		var titles []string
		for key != "" {
			employee, ok := byKey[key]
			if !ok {
				break
			}
			titles = append(titles, employee.Row.JobCode)
			key = employee.ManagerKey
		}
		return titles
	}
	if got := strings.Join(chain("ir-013-ana-flores"), ">"); got != "IR-JCP>IR-FMN>IR-SUP>IR-VPO>IR-OWN" {
		t.Errorf("crew chain = %s", got)
	}
	if got := strings.Join(chain("ir-031-mei-lin-park"), ">"); got != "IR-PAY>IR-CTL>IR-OWN" {
		t.Errorf("office chain = %s", got)
	}
	if got := strings.Join(chain("ir-034-kelsey-moran"), ">"); got != "IR-REC>IR-HRM>IR-OWN" {
		t.Errorf("people chain = %s", got)
	}
}

func TestIronridgeLadderCarriesHourlyAndSalariedSteps(t *testing.T) {
	edges := map[string]PromotionPathEdge{}
	for _, edge := range IronridgePack.PromotionPaths() {
		edges[edge.SourceJobCode+">"+edge.TargetJobCode] = edge
		if !strings.HasPrefix(edge.SourceJobCode, "IR-") || !strings.HasPrefix(edge.TargetJobCode, "IR-") {
			t.Fatalf("an Ironridge edge leaves the company: %+v", edge)
		}
	}
	for _, want := range []string{"IR-LAB>IR-APC", "IR-APC>IR-JCP", "IR-JCP>IR-FMN", "IR-FMN>IR-SUP", "IR-PM>IR-SPM", "IR-EST>IR-SEST", "IR-SVT>IR-SVL"} {
		if _, ok := edges[want]; !ok {
			t.Errorf("the ladder lacks %s", want)
		}
	}
	if HarborCarePack.PromotionPaths()[0].SourceJobCode == "" || len(PromotionPaths()) != len(HarborCarePack.PromotionPaths()) {
		t.Fatal("the package ladder is no longer HarborCare's")
	}
}

func TestIronridgeCatalogBandsLadderRowsAndArchitectureBuild(t *testing.T) {
	specs, err := IronridgePack.PayBandSpecs()
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != len(IronridgePack.allRoles()) {
		t.Fatalf("bands = %d, want one per role in the single zone", len(specs))
	}
	for _, spec := range specs {
		if !strings.HasPrefix(spec.ID, "ironridge-demo.band/") || spec.Version != "ironridge.pay_bands/2026.09.1" {
			t.Fatalf("band %s version %s", spec.ID, spec.Version)
		}
	}
	architecture, err := IronridgePack.JobArchitecture()
	if err != nil {
		t.Fatal(err)
	}
	if architecture.ID != "ironridge-demo.job-architecture" || len(architecture.Profiles) != len(IronridgePack.allRoles()) {
		t.Fatalf("architecture %s with %d profiles", architecture.ID, len(architecture.Profiles))
	}
	for _, edge := range IronridgePack.PromotionLadderEdges() {
		if edge.PolicyVersion != "ironridge.promotion_ladder/2026.09.1" || !strings.HasPrefix(edge.SourceProfileID, "ironridge-demo.profile/") {
			t.Fatalf("ladder row %+v", edge)
		}
	}
}

func TestIronridgePayrollRunsHourlyWeeklyAndSalariedSemiMonthly(t *testing.T) {
	plan, err := IronridgePack.PlanPayroll(ironridgeTestTenant)
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]int{}
	for _, run := range plan.Runs {
		groups[run.Revisions[0].PayGroupRef]++
		for _, member := range run.Population.Members {
			if member.PayGroupRef != run.Revisions[0].PayGroupRef {
				t.Fatalf("member %s in the wrong group", member.WorkerRef)
			}
		}
	}
	if groups[IronridgeHourlyPayGroupRef] != 4 || groups[IronridgeSalariedPayGroupRef] != 4 {
		t.Fatalf("runs by group = %v", groups)
	}
	for _, run := range plan.Runs {
		want := 19
		if run.Revisions[0].PayGroupRef == IronridgeHourlyPayGroupRef {
			want = 19
		}
		if len(run.Population.Members)+len(run.LateEntries) > want {
			t.Errorf("%s has %d members, more than the %d on that basis", run.Revisions[0].RunID, len(run.Population.Members), want)
		}
	}
	// HarborCare's payroll is its one salaried group, unchanged.
	harbor, err := PlanPayroll(ironridgeTestTenant)
	if err != nil || len(harbor.Runs) != len(DemoPayPeriods) || harbor.Runs[0].Revisions[0].RunID != "harborcare-payroll-2026-06" {
		t.Fatalf("HarborCare payroll changed: %v", err)
	}
}

func TestIronridgeRoleBundlesFollowTheBrief(t *testing.T) {
	assignments, err := IronridgePack.PlanRoleAssignments(ironridgeTestTenant)
	if err != nil {
		t.Fatal(err)
	}
	roles := map[string]string{}
	for _, assignment := range assignments {
		roles[assignment.WorkerKey] = strings.Join(assignment.RoleIDs, ",")
	}
	for key, want := range map[string][]string{
		"ir-001-walt-brennan":   AdminBundle,       // Owner
		"ir-033-nicole-greene":  HRPartnerBundle,   // HR Manager
		"ir-003-loretta-haynes": FinanceBundle,     // Controller
		"ir-031-mei-lin-park":   FinanceBundle,     // Payroll
		"ir-005-greg-novak":     ManagerBundle,     // PM
		"ir-008-curtis-bell":    ManagerBundle,     // Superintendent
		"ir-010-luis-ortega":    ManagerBundle,     // Foreman
		"ir-013-ana-flores":     SelfServiceBundle, // Journeyman
		"ir-034-kelsey-moran":   SelfServiceBundle, // Recruiter
	} {
		if roles[key] != strings.Join(want, ",") {
			t.Errorf("%s holds %s, want %v", key, roles[key], want)
		}
	}
	for _, persona := range IronridgePack.Personas {
		if IronridgePack.personaRoleAssignments[persona.WorkerNumber] == nil {
			t.Errorf("quick pick %s (%s) is not pinned", persona.ID, persona.WorkerNumber)
		}
	}
}

func TestIronridgePerformanceRecordPlans(t *testing.T) {
	plan, err := IronridgePack.PlanPerformance(ironridgeTestTenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Cycles) != 3 || plan.Cycles[0].Spec.CycleID != "ironridge-2024-annual" || len(plan.Cycles[0].Sessions) == 0 {
		t.Fatalf("performance plan = %d cycles", len(plan.Cycles))
	}
	if IronridgePack.Theme == nil || IronridgePack.Logo == nil || len(IronridgePack.Logo.PNG) == 0 || HarborCarePack.Theme != nil {
		t.Fatal("only Ironridge carries its own brand")
	}
}

// TestHourlyBandsCarryTheirBasisAndAnnualEquivalent is P2's band rule: an
// hourly craft band is published per hour, annualized over standard hours
// for comparison, and a salaried band is its own annual equivalent.
func TestHourlyBandsCarryTheirBasisAndAnnualEquivalent(t *testing.T) {
	specs, err := IronridgePack.PayBandSpecs()
	if err != nil {
		t.Fatal(err)
	}
	byCode := map[string]PayBandSpec{}
	for _, spec := range specs {
		byCode[spec.JobCode] = spec
	}
	foreman := byCode["IR-FMN"]
	if foreman.Basis != PayBandBasisHourly || foreman.Midpoint.Amount().String() != "40.00" ||
		foreman.Minimum.Amount().String() != "32.00" || foreman.Maximum.Amount().String() != "48.00" ||
		foreman.StandardHours != 2080 || foreman.AnnualMidpoint.Amount().String() != "83200.00" {
		t.Fatalf("foreman band = %+v", foreman)
	}
	super := byCode["IR-SUP"]
	if super.Basis != PayBandBasisAnnual || super.AnnualMidpoint.Amount().String() != super.Midpoint.Amount().String() {
		t.Fatalf("superintendent band = %+v", super)
	}
	harbor, err := PayBandSpecs()
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range harbor {
		if spec.Basis != PayBandBasisAnnual || spec.AnnualMaximum.Amount().String() != spec.Maximum.Amount().String() {
			t.Fatalf("a HarborCare band is not annual: %+v", spec)
		}
	}
	if IronridgePack.PayBasisFor("IR-JCP") != PayBasisHourly || IronridgePack.PayBasisFor("IR-PM") != DemoPayBasis || HarborCarePack.PayBasisFor("ENG-SWE3") != DemoPayBasis {
		t.Fatal("pay basis by job is wrong")
	}
	// The ladder compares a foreman's annualized rate with a superintendent's
	// salary: 40.00 x 2080 = 83,200 against 108,000.
	if cents, ok := IronridgePack.annualCents(Role{Code: "IR-FMN", BasePay: "40.00"}); !ok || cents != 8320000 {
		t.Fatalf("annualized foreman = %d", cents)
	}
}
