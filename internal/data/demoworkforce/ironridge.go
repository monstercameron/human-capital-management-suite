package demoworkforce

import "time"

// Ironridge Builders is the second demo company: a 38-person commercial
// general contractor headquartered in Denver, Colorado. It is wholly
// fictional. Unlike HarborCare, whose people are derived from staffing
// headcounts, Ironridge's people are authored one by one (ironridgeRoster),
// because a construction company's reporting chain -- crew, foreman,
// superintendent, vice-president of operations, owner -- is a fact about
// named people, not an artefact of unit order.
//
// Field crews are paid by the hour. Their roles are listed in
// ironridgeHourlyJobs, their recorded base pay is an hourly rate, their pay
// basis is HOURLY_RATE and they are paid through the weekly hourly pay group;
// everybody else is salaried and paid semi-monthly.

// IronridgeKey is the tenant key Ironridge Builders is served under.
const IronridgeKey = "ironridge-demo"

// Ironridge is the company's identity and organization hierarchy.
var Ironridge = Company{
	Key:          IronridgeKey,
	Name:         "Ironridge Builders",
	Description:  "A Denver commercial general contractor building schools, medical offices and multifamily housing across the Front Range.",
	LegalEntity:  "Ironridge Builders, Inc.",
	Headquarters: "Denver, CO",
	Units: []OrganizationUnit{
		{Code: "ironridge", Name: "Ironridge Builders", Type: "BUSINESS_UNIT", Purpose: "Company-wide ownership, governance and strategy."},
		{Code: "executive", Name: "Executive", Type: "DEPARTMENT", ParentCode: "ironridge", Purpose: "Ownership, operations leadership and financial control."},
		{Code: "operations", Name: "Operations", Type: "DIVISION", ParentCode: "ironridge", Purpose: "Building the work safely, on schedule and to spec."},
		{Code: "project-management", Name: "Project Management", Type: "DEPARTMENT", ParentCode: "operations", Purpose: "Owner relationships, schedules, budgets, RFIs and change orders."},
		{Code: "field-operations", Name: "Field Operations", Type: "DEPARTMENT", ParentCode: "operations", Purpose: "Self-performed carpentry, concrete and site work, and the equipment yard."},
		{Code: "safety-quality", Name: "Safety & Quality", Type: "DEPARTMENT", ParentCode: "operations", Purpose: "Jobsite safety, inspections and quality control."},
		{Code: "warranty-service", Name: "Warranty & Service", Type: "DEPARTMENT", ParentCode: "operations", Purpose: "Warranty callbacks and small-works service."},
		{Code: "preconstruction", Name: "Preconstruction", Type: "DIVISION", ParentCode: "ironridge", Purpose: "Winning and pricing the right work."},
		{Code: "estimating-preconstruction", Name: "Estimating & Preconstruction", Type: "DEPARTMENT", ParentCode: "preconstruction", Purpose: "Takeoffs, bids, budgets and constructability reviews."},
		{Code: "business-development", Name: "Business Development", Type: "DEPARTMENT", ParentCode: "preconstruction", Purpose: "Client relationships and the bid pipeline."},
		{Code: "administration", Name: "Administration", Type: "DIVISION", ParentCode: "ironridge", Purpose: "Money, people and the office."},
		{Code: "finance-admin", Name: "Finance & Admin", Type: "DEPARTMENT", ParentCode: "administration", Purpose: "Payroll, payables, purchasing and the front office."},
		{Code: "people", Name: "People", Type: "DEPARTMENT", ParentCode: "administration", Purpose: "Hiring, onboarding, benefits and employee relations."},
	},
}

// ironridgeStaffing is the company's role catalog by unit. The counts are the
// roster's own; the roster decides who holds which role.
var ironridgeStaffing = []unitStaffing{
	{Code: "executive", Count: 3, Roles: []Role{
		{"IR-OWN", "Owner & President", "E3", "255000.00", "0.3000"},
		{"IR-VPO", "Vice President, Operations", "E2", "215000.00", "0.2500"},
		{"IR-CTL", "Controller", "E1", "182000.00", "0.1500"},
	}},
	{Code: "project-management", Count: 4, Roles: []Role{
		{"IR-SPM", "Senior Project Manager", "S4", "128000.00", "0.1000"},
		{"IR-PM", "Project Manager", "S3", "98000.00", "0.0800"},
		{"IR-APM", "Assistant Project Manager", "S2", "76000.00", "0.0500"},
		{"IR-PC", "Project Coordinator", "S1", "58000.00", "0.0300"},
	}},
	{Code: "field-operations", Count: 18, Roles: []Role{
		{"IR-SUP", "Superintendent", "S3", "108000.00", "0.0800"},
		{"IR-FMN", "Foreman", "C4", "40.00", "0.0300"},
		{"IR-JCP", "Journeyman Carpenter", "C3", "34.50", "0.0000"},
		{"IR-APC", "Apprentice Carpenter", "C2", "28.00", "0.0000"},
		{"IR-LAB", "Laborer", "C1", "21.50", "0.0000"},
		{"IR-EQO", "Equipment Operator", "C3", "33.00", "0.0000"},
		{"IR-YRD", "Yard & Logistics Lead", "C3", "29.00", "0.0000"},
	}},
	{Code: "estimating-preconstruction", Count: 2, Roles: []Role{
		{"IR-CEST", "Chief Estimator", "S4", "132000.00", "0.1000"},
		{"IR-EST", "Estimator", "S2", "82000.00", "0.0500"},
	}},
	{Code: "safety-quality", Count: 2, Roles: []Role{
		{"IR-SAF", "Safety Manager", "S3", "96000.00", "0.0600"},
		{"IR-QAQC", "QA/QC Inspector", "S2", "74000.00", "0.0300"},
	}},
	{Code: "finance-admin", Count: 3, Roles: []Role{
		{"IR-PUR", "Purchasing Manager", "S2", "72000.00", "0.0300"},
		{"IR-PAY", "Payroll & AP Specialist", "S1", "56000.00", "0.0200"},
		{"IR-OFM", "Office Manager", "S1", "54000.00", "0.0200"},
	}},
	{Code: "people", Count: 2, Roles: []Role{
		{"IR-HRM", "HR Manager", "S3", "94000.00", "0.0600"},
		{"IR-REC", "Recruiter", "S1", "55000.00", "0.0300"},
	}},
	{Code: "business-development", Count: 1, Roles: []Role{
		{"IR-BDM", "Business Development Manager", "S3", "110000.00", "0.1500"},
	}},
	{Code: "warranty-service", Count: 3, Roles: []Role{
		{"IR-SVL", "Service Lead", "C4", "36.00", "0.0200"},
		{"IR-SVT", "Service Technician", "C3", "30.50", "0.0000"},
	}},
}

// ironridgeCatalogOnly are rungs Ironridge publishes but nobody holds yet:
// the general superintendent above the superintendents and a senior
// estimator between the estimator and the chief estimator.
var ironridgeCatalogOnly = []unitStaffing{
	{Code: "field-operations", Roles: []Role{
		{"IR-GSUP", "General Superintendent", "S4", "132000.00", "0.1000"},
	}},
	{Code: "estimating-preconstruction", Roles: []Role{
		{"IR-SEST", "Senior Estimator", "S3", "104000.00", "0.0800"},
	}},
}

// The Denver HQ, the equipment yard and the four active jobsites. Every
// location is in one pay zone: this is a one-metro company.
const (
	ironridgeHQ        = "Denver, CO"
	ironridgeYard      = "Commerce City, CO yard"
	ironridgeAurora    = "Aurora, CO jobsite"
	ironridgeLakewood  = "Lakewood, CO jobsite"
	ironridgeLittleton = "Littleton, CO jobsite"
	ironridgeGolden    = "Golden, CO jobsite"
	ironridgeZone      = "US-MOUNTAIN"
)

var ironridgeLocations = []location{
	{ironridgeHQ, ironridgeZone}, {ironridgeYard, ironridgeZone},
	{ironridgeAurora, ironridgeZone}, {ironridgeLakewood, ironridgeZone},
	{ironridgeLittleton, ironridgeZone}, {ironridgeGolden, ironridgeZone},
}

// ironridgeRoster is the 38 people, in worker-number order. Manager is the
// 1-based roster index of the person's manager of record; Photo is the seed
// headshot they wear (0 = none). Every unit lists its head first, which is
// who chairs the unit's calibration.
var ironridgeRoster = []rosterEntry{
	// Executive (3)
	{Given: "Walt", Family: "Brennan", Unit: "executive", Role: "IR-OWN", Location: ironridgeHQ, Manager: 0, HireDate: "2004-03-15", Photo: 17},
	{Given: "Marcus", Family: "Whitfield", Unit: "executive", Role: "IR-VPO", Location: ironridgeHQ, Manager: 1, HireDate: "2011-06-06", Photo: 38},
	{Given: "Loretta", Family: "Haynes", Unit: "executive", Role: "IR-CTL", Location: ironridgeHQ, Manager: 1, HireDate: "2013-01-14", Photo: 34},
	// Project Management (4)
	{Given: "Priya", Family: "Raman", Unit: "project-management", Role: "IR-SPM", Location: ironridgeHQ, Manager: 2, HireDate: "2015-04-20", Photo: 31},
	{Given: "Greg", Family: "Novak", Unit: "project-management", Role: "IR-PM", Location: ironridgeHQ, Manager: 4, HireDate: "2018-08-06", Photo: 43},
	{Given: "Sofia", Family: "Beltran", Unit: "project-management", Role: "IR-APM", Location: ironridgeHQ, Manager: 5, HireDate: "2022-02-28", Photo: 27},
	{Given: "Hannah", Family: "Lindqvist", Unit: "project-management", Role: "IR-PC", Location: ironridgeHQ, Manager: 4, HireDate: "2024-05-13", Photo: 37},
	// Field Operations (18): two superintendents, three foremen and their crews,
	// the equipment operator and the yard lead.
	{Given: "Curtis", Family: "Bell", Unit: "field-operations", Role: "IR-SUP", Location: ironridgeAurora, Manager: 2, HireDate: "2009-09-08", Photo: 57},
	{Given: "Sam", Family: "Haddad", Unit: "field-operations", Role: "IR-SUP", Location: ironridgeLakewood, Manager: 2, HireDate: "2014-03-03", Photo: 14},
	{Given: "Luis", Family: "Ortega", Unit: "field-operations", Role: "IR-FMN", Location: ironridgeAurora, Manager: 8, HireDate: "2012-05-21", Photo: 22},
	{Given: "Jake", Family: "Sullivan", Unit: "field-operations", Role: "IR-FMN", Location: ironridgeLittleton, Manager: 8, HireDate: "2016-07-11", Photo: 6},
	{Given: "DeShawn", Family: "Carter", Unit: "field-operations", Role: "IR-FMN", Location: ironridgeLakewood, Manager: 9, HireDate: "2017-10-02", Photo: 29},
	{Given: "Ana", Family: "Flores", Unit: "field-operations", Role: "IR-JCP", Location: ironridgeAurora, Manager: 10, HireDate: "2019-03-18", Photo: 13},
	{Given: "Ben", Family: "Whitaker", Unit: "field-operations", Role: "IR-JCP", Location: ironridgeAurora, Manager: 10, HireDate: "2015-08-24", Photo: 0},
	{Given: "Arjun", Family: "Mehta", Unit: "field-operations", Role: "IR-JCP", Location: ironridgeLittleton, Manager: 11, HireDate: "2020-06-15", Photo: 49},
	{Given: "Mateo", Family: "Ruiz", Unit: "field-operations", Role: "IR-JCP", Location: ironridgeLakewood, Manager: 12, HireDate: "2018-04-09", Photo: 55},
	{Given: "Danny", Family: "Nguyen", Unit: "field-operations", Role: "IR-JCP", Location: ironridgeGolden, Manager: 12, HireDate: "2021-01-25", Photo: 35},
	{Given: "Jordan", Family: "Ellis", Unit: "field-operations", Role: "IR-APC", Location: ironridgeAurora, Manager: 10, HireDate: "2025-03-10", Photo: 33},
	{Given: "Maya", Family: "Okonkwo", Unit: "field-operations", Role: "IR-APC", Location: ironridgeLittleton, Manager: 11, HireDate: "2024-09-16", Photo: 15},
	{Given: "Chris", Family: "Yazzie", Unit: "field-operations", Role: "IR-APC", Location: ironridgeLakewood, Manager: 12, HireDate: "2025-06-02", Photo: 0},
	{Given: "Eddie", Family: "Ramirez", Unit: "field-operations", Role: "IR-LAB", Location: ironridgeAurora, Manager: 10, HireDate: "2026-04-06", Photo: 0},
	{Given: "Imran", Family: "Qureshi", Unit: "field-operations", Role: "IR-LAB", Location: ironridgeLittleton, Manager: 11, HireDate: "2025-10-13", Photo: 19},
	{Given: "Rosa", Family: "Jimenez", Unit: "field-operations", Role: "IR-LAB", Location: ironridgeGolden, Manager: 12, HireDate: "2023-05-01", Photo: 47},
	{Given: "Glen", Family: "Sato", Unit: "field-operations", Role: "IR-EQO", Location: ironridgeYard, Manager: 8, HireDate: "2010-11-01", Photo: 53},
	{Given: "Terrell", Family: "Brooks", Unit: "field-operations", Role: "IR-YRD", Location: ironridgeYard, Manager: 9, HireDate: "2013-07-22", Photo: 9},
	// Estimating & Preconstruction (2)
	{Given: "Nabil", Family: "Farouk", Unit: "estimating-preconstruction", Role: "IR-CEST", Location: ironridgeHQ, Manager: 1, HireDate: "2012-02-06", Photo: 46},
	{Given: "Andrew", Family: "Cho", Unit: "estimating-preconstruction", Role: "IR-EST", Location: ironridgeHQ, Manager: 26, HireDate: "2021-09-13", Photo: 3},
	// Safety & Quality (2)
	{Given: "Rachel", Family: "Stone", Unit: "safety-quality", Role: "IR-SAF", Location: ironridgeHQ, Manager: 2, HireDate: "2017-01-09", Photo: 51},
	{Given: "Grace", Family: "Adeyemi", Unit: "safety-quality", Role: "IR-QAQC", Location: ironridgeHQ, Manager: 28, HireDate: "2022-10-17", Photo: 45},
	// Finance & Admin (3)
	{Given: "Vikram", Family: "Shah", Unit: "finance-admin", Role: "IR-PUR", Location: ironridgeHQ, Manager: 3, HireDate: "2019-11-04", Photo: 25},
	{Given: "Mei Lin", Family: "Park", Unit: "finance-admin", Role: "IR-PAY", Location: ironridgeHQ, Manager: 3, HireDate: "2020-03-02", Photo: 42},
	{Given: "Barb", Family: "Kowalski", Unit: "finance-admin", Role: "IR-OFM", Location: ironridgeHQ, Manager: 3, HireDate: "2008-06-16", Photo: 59},
	// People (2)
	{Given: "Nicole", Family: "Greene", Unit: "people", Role: "IR-HRM", Location: ironridgeHQ, Manager: 1, HireDate: "2016-05-02", Photo: 2},
	{Given: "Kelsey", Family: "Moran", Unit: "people", Role: "IR-REC", Location: ironridgeHQ, Manager: 33, HireDate: "2023-08-21", Photo: 23},
	// Business Development (1)
	{Given: "Elena", Family: "Vasquez", Unit: "business-development", Role: "IR-BDM", Location: ironridgeHQ, Manager: 1, HireDate: "2014-10-06", Photo: 1},
	// Warranty & Service (3)
	{Given: "Ray", Family: "Castillo", Unit: "warranty-service", Role: "IR-SVL", Location: ironridgeYard, Manager: 2, HireDate: "2011-04-18", Photo: 41},
	{Given: "Hector", Family: "Salas", Unit: "warranty-service", Role: "IR-SVT", Location: ironridgeYard, Manager: 36, HireDate: "2019-07-08", Photo: 0},
	{Given: "Lily", Family: "Chen", Unit: "warranty-service", Role: "IR-SVT", Location: ironridgeYard, Manager: 36, HireDate: "2023-02-13", Photo: 18},
}

// ironridgeHourlyJobs are the craft roles paid by the hour.
var ironridgeHourlyJobs = map[string]bool{
	"IR-FMN": true, "IR-JCP": true, "IR-APC": true, "IR-LAB": true,
	"IR-EQO": true, "IR-YRD": true, "IR-SVL": true, "IR-SVT": true,
}

var ironridgeJobFamilies = map[string]string{
	"IR-OWN": "Executive Leadership", "IR-VPO": "Executive Leadership",
	"IR-CTL": "Finance & Accounting", "IR-PAY": "Finance & Accounting",
	"IR-SPM": "Project Management", "IR-PM": "Project Management",
	"IR-APM": "Project Management", "IR-PC": "Project Management",
	"IR-GSUP": "Field Construction", "IR-SUP": "Field Construction", "IR-FMN": "Field Construction",
	"IR-JCP": "Field Construction", "IR-APC": "Field Construction", "IR-LAB": "Field Construction",
	"IR-EQO": "Field Construction", "IR-YRD": "Field Construction",
	"IR-CEST": "Estimating & Preconstruction", "IR-SEST": "Estimating & Preconstruction",
	"IR-EST": "Estimating & Preconstruction",
	"IR-SAF": "Safety & Quality", "IR-QAQC": "Safety & Quality",
	"IR-PUR": "Office Administration", "IR-OFM": "Office Administration",
	"IR-HRM": "Human Resources", "IR-REC": "Human Resources",
	"IR-BDM": "Business Development",
	"IR-SVL": "Warranty & Service", "IR-SVT": "Warranty & Service",
}

// ironridgeNonExemptJobs are overtime-eligible: every hourly craft role, and
// the salaried coordinator and clerical roles whose duties fail the
// exemption tests.
var ironridgeNonExemptJobs = map[string]bool{
	"IR-FMN": true, "IR-JCP": true, "IR-APC": true, "IR-LAB": true,
	"IR-EQO": true, "IR-YRD": true, "IR-SVL": true, "IR-SVT": true,
	"IR-PC": true, "IR-PAY": true, "IR-OFM": true,
}

var ironridgeGradeRank = map[string]int{
	"C1": 1, "C2": 2, "C3": 3, "C4": 4,
	"S1": 5, "S2": 6, "S3": 7, "S4": 8,
	"E1": 9, "E2": 10, "E3": 11,
}

var ironridgeLevelTitles = map[string]string{
	"C1": "Entry Craft", "C2": "Apprentice", "C3": "Journeyman", "C4": "Lead Craft",
	"S1": "Associate", "S2": "Professional", "S3": "Manager", "S4": "Senior Manager",
	"E1": "Officer", "E2": "Vice President", "E3": "President",
}

// ironridgeAdjacentFamily is who actually moves between disciplines here: a
// superintendent into project management, a service lead into the field, an
// estimator into business development, a payroll specialist into purchasing.
var ironridgeAdjacentFamily = map[string][]string{
	"Field Construction":           {"Project Management", "Safety & Quality", "Warranty & Service"},
	"Warranty & Service":           {"Field Construction"},
	"Safety & Quality":             {"Field Construction", "Project Management"},
	"Project Management":           {"Field Construction"},
	"Estimating & Preconstruction": {"Business Development"},
	"Business Development":         {"Estimating & Preconstruction"},
	"Finance & Accounting":         {"Office Administration"},
	"Office Administration":        {"Finance & Accounting", "Human Resources"},
	"Human Resources":              {"Office Administration"},
}

var ironridgeExecutiveStep = map[string][]string{
	"Project Management":           {"IR-VPO"},
	"Estimating & Preconstruction": {"IR-VPO"},
	"Field Construction":           {"IR-VPO"},
}

// Weekly hourly periods (Monday to Monday) and semi-monthly salaried periods,
// the last several closed before the seed's recorded instant.
var ironridgeHourlyPeriods = []PayPeriodSpec{
	{ID: "2026-W32", Start: time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC)},
	{ID: "2026-W33", Start: time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.August, 17, 0, 0, 0, 0, time.UTC)},
	{ID: "2026-W34", Start: time.Date(2026, time.August, 17, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC)},
	{ID: "2026-W35", Start: time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC)},
}

var ironridgeSalariedPeriods = []PayPeriodSpec{
	{ID: "2026-07-A", Start: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.July, 16, 0, 0, 0, 0, time.UTC)},
	{ID: "2026-07-B", Start: time.Date(2026, time.July, 16, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)},
	{ID: "2026-08-A", Start: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC)},
	{ID: "2026-08-B", Start: time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)},
}

// IronridgeHourlyPayGroupRef and IronridgeSalariedPayGroupRef are the
// company's two pay groups.
const (
	IronridgeHourlyPayGroupRef   = "ironridge-us-hourly-weekly"
	IronridgeSalariedPayGroupRef = "ironridge-us-salaried-semimonthly"
)

// IronridgePack is Ironridge Builders.
var IronridgePack = &Pack{
	Key: IronridgeKey, Company: Ironridge, DisplayName: "Ironridge Builders",
	Tagline:     "Commercial general contractor · schools, medical offices and multifamily across the Front Range",
	LogoAsset:   "ironridge-logo.svg",
	WorkerCount: len(ironridgeRoster),
	Theme:       ironridgeTheme,
	Logo:        ironridgeLogo,
	// Owner (admin), a superintendent (the proposer), the controller (the
	// finance approver) and a journeyman carpenter (the employee). Ana's
	// manager of record is her foreman, so the manager approval routes to
	// somebody other than the proposing superintendent.
	Personas: []PersonaSpec{
		{ID: "admin", WorkerNumber: "IR-00001", Access: "HCM administrator", Purpose: "compensation_review"},
		{ID: "hiring-manager", WorkerNumber: "IR-00008", Access: "Hiring manager", Purpose: "compensation_review"},
		{ID: "finance-partner", WorkerNumber: "IR-00003", Access: "Finance partner", Purpose: "compensation_review"},
		{ID: "individual-contributor", WorkerNumber: "IR-00013", Access: "Individual contributor", Purpose: "self_service_view"},
	},
	OrgScopeUnit:         "people",
	FinancePartnerKey:    "ir-003-loretta-haynes",
	ExecutionApproverKey: "ir-003-loretta-haynes",
	ManagerApproverKey:   "ir-002-marcus-whitfield",

	keyPrefix: "ir", numberPrefix: "IR", numberBase: 0,
	employmentPrefix: "ir-emp-", assignmentPrefix: "ir-asg-", positionPrefix: "IR-POS-",
	recordedBy: "hcmnext.demo-seed",
	staffing:   ironridgeStaffing, catalogOnly: ironridgeCatalogOnly, roster: ironridgeRoster,
	locations:     ironridgeLocations,
	executiveUnit: "executive", boardRef: "board:ironridge",
	photoPrefix: "hc",

	costCenters: map[string]string{
		"executive":                  "CC-100 Executive",
		"project-management":         "CC-200 Project Management",
		"field-operations":           "CC-300 Field Operations",
		"safety-quality":             "CC-320 Safety & Quality",
		"warranty-service":           "CC-340 Warranty & Service",
		"estimating-preconstruction": "CC-400 Estimating & Preconstruction",
		"business-development":       "CC-410 Business Development",
		"finance-admin":              "CC-500 Finance & Admin",
		"people":                     "CC-510 People",
	},
	onSiteUnits: map[string]bool{
		"field-operations": true, "safety-quality": true, "warranty-service": true, "project-management": true,
	},
	headquarters:     ironridgeHQ,
	partTimeFTE:      map[string]string{"ir-032-barb-kowalski": "0.8000"},
	fixedTermWorkers: map[string]bool{"ir-021-eddie-ramirez": true},
	jobFamilies:      ironridgeJobFamilies, nonExemptJobs: ironridgeNonExemptJobs,
	hourlyJobs: ironridgeHourlyJobs, standardHours: 2080,

	gradeRank: ironridgeGradeRank, levelTitles: ironridgeLevelTitles,
	adjacentFamily: ironridgeAdjacentFamily, executiveStep: ironridgeExecutiveStep,
	executiveEntryGrade: "S4",

	payBandPolicyVersion: "ironridge.pay_bands/2026.09.1", idPrefix: IronridgeKey,
	ladderVersion: "ironridge.promotion_ladder/2026.09.1", jobArchitectureID: IronridgeKey + ".job-architecture",

	policyPrefix: "ironridge",
	performanceCycles: []PerformanceCycleSpec{
		{CycleID: "ironridge-2024-annual", Year: 2024, Closed: true,
			OpenedAt: time.Date(2024, time.November, 4, 7, 0, 0, 0, time.UTC),
			ClosedAt: time.Date(2025, time.January, 31, 17, 0, 0, 0, time.UTC)},
		{CycleID: "ironridge-2025-annual", Year: 2025, Closed: true,
			OpenedAt: time.Date(2025, time.November, 3, 7, 0, 0, 0, time.UTC),
			ClosedAt: time.Date(2026, time.January, 30, 17, 0, 0, 0, time.UTC)},
		{CycleID: "ironridge-2026-midyear", Year: 2026, Closed: false,
			OpenedAt: time.Date(2026, time.August, 3, 7, 0, 0, 0, time.UTC)},
	},
	peopleOfficerIndex: 32,
	topPerformers:      []int{10, 13, 28},
	strongPerformers:   []int{4, 16, 25, 36},
	calibrated:         []int{5, 19, 29},
	belowExpectation:   []int{21, 22},

	payGroups: []PayGroupSpec{
		{Ref: IronridgeSalariedPayGroupRef, Basis: DemoPayBasis, Periods: ironridgeSalariedPeriods,
			RunPrefix: "ironridge-payroll-salaried-", PolicyPrefix: "ironridge.payroll.salaried"},
		{Ref: IronridgeHourlyPayGroupRef, Basis: PayBasisHourly, Periods: ironridgeHourlyPeriods,
			RunPrefix: "ironridge-payroll-hourly-", PolicyPrefix: "ironridge.payroll.hourly"},
	},

	personaRoleAssignments: map[string][]string{
		"IR-00001": AdminBundle,
		"IR-00008": ManagerBundle,
		"IR-00003": FinanceBundle,
		"IR-00013": SelfServiceBundle,
	},
	peopleUnit: "people", financeUnit: "finance-admin",
	roleAssignmentActor: "system:ironridge-demo-seed",
	executiveJobs:       map[string]bool{"IR-OWN": true},
	financeJobs:         map[string]bool{"IR-CTL": true, "IR-PAY": true},
	peopleJobs:          map[string]bool{"IR-HRM": true},
}
