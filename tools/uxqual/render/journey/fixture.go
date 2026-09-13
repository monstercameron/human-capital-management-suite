package journey

// This file is the page's reference data: one promotion, Omar Reyes moving
// from OPS-HRBP2/P2 to OPS-HRBP3/P3 with base pay going from USD 93,000.00
// to 98,000.00 effective 1 June 2026, shown at each of the three states the
// journey has a distinct layout for.
//
// It exists so the tests have a fully populated Page: every section, every
// tone, every field kind and every gauge is present, because an empty
// fixture proves only that a renderer does not crash, not that it renders
// anything. The values are shaped like the engine's -- ULID-ish
// identifiers, decimal pay strings, dates already formatted for display by
// the projecting lane -- but they are invented, and nothing here is loaded
// at run time.
//
// None of the pages here carry live callbacks. That is deliberate: with the
// callbacks nil, Build produces the plain-form tree the no-browser tests
// assert against; the live wiring is exercised separately, by tests that
// set the callbacks themselves.

const (
	fixtureIntentID   = "int_01JX6Y8B2C7D9EFG"
	fixtureInstanceID = "wfi_01JX7Q2M4K8N3RA6"
	fixtureHref       = "/workspace/journeys/int_01JX6Y8B2C7D9EFG"
	fixtureCSRF       = "PxK9m2Ub7Tq4Zc1Ve6Ns8Dw3Rj5Hy0Lg"
)

func fixturePrincipal() Principal {
	return Principal{
		Subject:    "avery.okafor@northwind.example",
		Roles:      []string{"hr.business_partner", "promotion.approver"},
		Purpose:    "promotion_review",
		LogoutHref: "/workspace/session/end",
	}
}

func fixtureNav(currentJourneys bool) []NavLink {
	return []NavLink{
		{Label: "Promotion journeys", Href: "/workspace/journeys", Current: currentJourneys},
		{Label: "Workspace", Href: "/workspace"},
		{Label: "Receipts", Href: "/workspace/receipts"},
	}
}

func fixtureFooter() Footer {
	return Footer{
		PolicyVersion: "policy@2026.05.3",
		CellID:        "cell-us-east-1a",
		BuildRef:      "human-capital-management-suite 0.9.4+1faafb5",
		Lines: []string{
			"Every figure here was read through the governed worker read under the purpose above; nothing is cached in the browser.",
		},
	}
}

func fixtureSubject() JourneyCard {
	return JourneyCard{
		IntentID:      fixtureIntentID,
		Href:          fixtureHref,
		WorkerName:    "Omar Reyes",
		WorkerRef:     "worker:NW-40118",
		Headline:      "OPS-HRBP2 · P2 → OPS-HRBP3 · P3",
		PayLine:       "USD 93,000.00 → 98,000.00 (+5.4%)",
		EffectiveDate: "1 Jun 2026",
		Stage:         "AWAITING_APPROVAL",
		StageLabel:    "Awaiting approval",
		StageTone:     toneWarning,
		Updated:       "12 May 2026, 09:12 UTC",
		InstanceID:    fixtureInstanceID,
		// fixturePrincipal is signed in with a role this fixture treats as
		// PROMOUX-008 diagnostics-authorized, so the reference page exercises
		// every section a renderer test walks, per this file's own doc
		// comment. fixture_test.go and components_test.go cover the
		// unauthorized shape directly.
		DiagnosticsAuthorized: true,
	}
}

// SampleListPage is the journeys overview: four journeys spanning every
// stage tone the list can show, and the manager's proposal form with one
// field of each kind the contract defines.
func SampleListPage() Page {
	return Page{
		Title:       "Promotion journeys · Northwind People",
		Brand:       "Northwind People",
		TenantLabel: "Northwind Trading · US",
		Principal:   fixturePrincipal(),
		Nav:         fixtureNav(true),
		Notice: &Notice{
			Tone:   toneSuccess,
			Title:  "Proposal recorded for Priya Raghunathan",
			Detail: "The simulation produced an executable plan. It is waiting for the execution authority to admit it.",
		},
		List: &ListView{
			Journeys:        fixtureJourneys(),
			Empty:           "No promotion has been proposed in this tenant yet. The form below starts one.",
			Form:            fixtureProposalForm(),
			EngineAvailable: true,
			People:          fixturePeople(),
		},
		Footer: fixtureFooter(),
	}
}

// fixtureSelectedRef is the employee the list fixture has picked: Omar
// Reyes, the same promotion the detail fixtures are about, so the two halves
// of the sample set tell one story.
const fixtureSelectedRef = "worker:NW-40118"

// fixturePeople is the workforce panel: the four employees the corpus ships
// with plus two recorded through this page, so both provenances render, one
// of them selected, and a New employee form carrying every field kind the
// panel uses.
func fixturePeople() *PeopleView {
	return &PeopleView{
		Workers:     fixtureWorkers(),
		Empty:       "No employee in this tenant is readable under this purpose yet. The form below records the first one.",
		Form:        fixtureWorkerForm(),
		SelectedRef: fixtureSelectedRef,
		Note:        "Employees you add are recorded as facts in this cell's workforce table.",
	}
}

func fixtureWorkers() []WorkerCard {
	return []WorkerCard{
		{
			Ref: "worker:NW-40092", Name: "Jane Doe", Number: "NW-40092",
			Title: "Senior HR Business Partner", JobCode: "OPS-HRBP3", Grade: "P3",
			OrgUnit: "People Operations · EMEA", Location: "London, UK",
			PayLine: "USD 104,500.00", HireDate: "4 Feb 2019",
			Source: sourceCorpus, SourceLabel: "From corpus",
			ProposeHref: "#/journeys/new?worker=worker:NW-40092",
		},
		{
			Ref: fixtureSelectedRef, Name: "Omar Reyes", Number: "NW-40118",
			Title: "HR Business Partner", JobCode: "OPS-HRBP2", Grade: "P2",
			OrgUnit: "People Operations · EMEA", Location: "Madrid, ES",
			PayLine: "USD 93,000.00", HireDate: "11 Sep 2021",
			Source: sourceCorpus, SourceLabel: "From corpus", Tone: toneWarning,
			Selected: true, OpenJourneys: 1,
			ProposeHref: "#/journeys/new?worker=" + fixtureSelectedRef,
		},
		{
			Ref: "worker:NW-40203", Name: "Lena Park", Number: "NW-40203",
			Title: "Compensation Analyst", JobCode: "FIN-COMP2", Grade: "P2",
			OrgUnit: "Total Rewards · APAC", Location: "Seoul, KR",
			PayLine: "USD 81,250.00", HireDate: "3 Mar 2022",
			Source: sourceCorpus, SourceLabel: "From corpus",
			ProposeHref: "#/journeys/new?worker=worker:NW-40203",
		},
		{
			Ref: "worker:NW-40311", Name: "Noor Haddad", Number: "NW-40311",
			Title: "Talent Acquisition Partner", JobCode: "TAL-TAP2", Grade: "P2",
			OrgUnit: "Talent · EMEA", Location: "Dubai, AE",
			PayLine: "USD 76,400.00", HireDate: "18 Jul 2023",
			Source: sourceCorpus, SourceLabel: "From corpus",
			ProposeHref: "#/journeys/new?worker=worker:NW-40311",
		},
		{
			Ref: "worker:NW-51007", Name: "Diego Marchetti", Number: "NW-51007",
			Title: "Payroll Specialist", JobCode: "OPS-PAY2", Grade: "P2",
			OrgUnit: "People Operations · AMER", Location: "Austin, US",
			PayLine: "USD 68,900.00", HireDate: "1 Sep 2026",
			Source: sourceCreated, SourceLabel: "Added here", Tone: toneInfo,
			ProposeHref: "#/journeys/new?worker=worker:NW-51007",
		},
		{
			Ref: "worker:NW-51008", Name: "Aiko Tanaka", Number: "NW-51008",
			Title: "Workforce Data Analyst", JobCode: "OPS-WFA2", Grade: "P2",
			OrgUnit: "People Analytics · APAC", Location: "Osaka, JP",
			PayLine: "USD 72,300.00", HireDate: "15 Sep 2026",
			Source: sourceCreated, SourceLabel: "Added here", Tone: toneInfo,
			ProposeHref: "#/journeys/new?worker=worker:NW-51008",
		},
	}
}

// fixtureWorkerForm is the New employee form with every control the panel
// declares: the two names, the three coded placements, the two money fields
// with their adornments, the hire date and the manager reference.
func fixtureWorkerForm() WorkerForm {
	return WorkerForm{
		Action: "/workspace/people/create",
		Hidden: map[string]string{
			"csrf_token": fixtureCSRF,
			"form":       "create_worker",
			"return_to":  "/workspace/journeys",
		},
		Submit: "Add employee",
		Fields: []Field{
			{
				ID: "worker-legal-name", Name: "legal_name", Label: "Legal name", Kind: fieldKindText,
				Required: true, Value: "Rosa Iglesias", Placeholder: "As it appears on the contract",
				Help: "Recorded verbatim; corrections are a later fact, not an edit to this one.",
			},
			{
				ID: "worker-preferred-name", Name: "preferred_name", Label: "Preferred name", Kind: fieldKindText,
				Value: "Rosa", Placeholder: "How the workspace addresses them",
			},
			{
				ID: "worker-job-code", Name: "job_code", Label: "Job code", Kind: fieldKindSelect,
				Required: true,
				Options: []Option{
					{Value: "OPS-HRBP2", Label: "OPS-HRBP2 — HR Business Partner", Selected: true},
					{Value: "OPS-PAY2", Label: "OPS-PAY2 — Payroll Specialist"},
					{Value: "FIN-COMP2", Label: "FIN-COMP2 — Compensation Analyst"},
					{Value: "TAL-TAP2", Label: "TAL-TAP2 — Talent Acquisition Partner"},
				},
				Help: "Only jobs published for this tenant are listed.",
			},
			{
				ID: "worker-grade", Name: "grade", Label: "Grade", Kind: fieldKindSelect,
				Required: true,
				Options: []Option{
					{Value: "P1", Label: "P1"},
					{Value: "P2", Label: "P2", Selected: true},
					{Value: "P3", Label: "P3"},
					{Value: "P4", Label: "P4"},
					{Value: "P5", Label: "P5"},
				},
			},
			{
				ID: "worker-org-unit", Name: "org_unit", Label: "Org unit", Kind: fieldKindSelect,
				Required: true,
				Options: []Option{
					{Value: "OU-PEOPLE-EMEA", Label: "People Operations · EMEA", Selected: true},
					{Value: "OU-PEOPLE-AMER", Label: "People Operations · AMER"},
					{Value: "OU-REWARDS-APAC", Label: "Total Rewards · APAC"},
					{Value: "OU-TALENT-EMEA", Label: "Talent · EMEA"},
				},
			},
			{
				ID: "worker-position", Name: "position_id", Label: "Position id", Kind: fieldKindText,
				Value: "POS-6041", Placeholder: "POS-0000",
				Help: "Leave blank to open a new position for this job code.",
			},
			{
				ID: "worker-location", Name: "location", Label: "Location", Kind: fieldKindText,
				Value: "Barcelona, ES", Placeholder: "City, country",
			},
			{
				ID: "worker-pay-zone", Name: "pay_zone", Label: "Pay zone", Kind: fieldKindSelect,
				Required: true,
				Options: []Option{
					{Value: "ZONE-1", Label: "ZONE-1 — metro"},
					{Value: "ZONE-2", Label: "ZONE-2 — national", Selected: true},
					{Value: "ZONE-3", Label: "ZONE-3 — remote"},
				},
				Help: "Decides which pay band the record is scored against.",
			},
			{
				ID: "worker-base-pay", Name: "base_pay", Label: "Base pay", Kind: fieldKindNumber,
				Required: true, Value: "72500.00", Step: "0.01", Min: "0",
				Prefix: "USD", Suffix: "per year",
				Help: "In the tenant's reporting currency.",
			},
			{
				ID: "worker-bonus-target", Name: "bonus_target", Label: "Bonus target", Kind: fieldKindNumber,
				Value: "10", Step: "0.1", Min: "0", Suffix: "%",
				Help: "Share of base pay at plan. Zero when the job carries no bonus.",
			},
			{
				ID: "worker-hire-date", Name: "hire_date", Label: "Hire date", Kind: fieldKindDate,
				Required: true, Value: "2026-09-14", Min: "2020-01-01",
				Help: "The date the employment relationship starts, not the date you record it.",
			},
			{
				ID: "worker-manager", Name: "manager_ref", Label: "Manager", Kind: fieldKindText,
				Value: "worker:NW-40092", Placeholder: "worker:NW-00000",
				Help: "Whose approval routes for this person's changes.",
			},
		},
	}
}

func fixtureJourneys() []JourneyCard {
	return []JourneyCard{
		fixtureSubject(),
		{
			IntentID:      "int_01JX6ZK4N1P8QRS2",
			Href:          "/workspace/journeys/int_01JX6ZK4N1P8QRS2",
			WorkerName:    "Priya Raghunathan",
			WorkerRef:     "worker:NW-38204",
			Headline:      "FIN-ANALYST2 · P2 → FIN-ANALYST3 · P3",
			PayLine:       "USD 88,400.00 → 94,000.00 (+6.3%)",
			EffectiveDate: "1 Jul 2026",
			Stage:         "PROPOSED",
			StageLabel:    "Proposed",
			StageTone:     toneInfo,
			Updated:       "12 May 2026, 08:02 UTC",
		},
		{
			IntentID:      "int_01JX5V3H9M2K7TUW",
			Href:          "/workspace/journeys/int_01JX5V3H9M2K7TUW",
			WorkerName:    "Marcus Devlin",
			WorkerRef:     "worker:NW-21955",
			Headline:      "SLS-AE3 · P3 → SLS-AE4 · P4",
			PayLine:       "USD 121,000.00 → 138,000.00 (+14.0%)",
			EffectiveDate: "1 Jun 2026",
			Stage:         "BLOCKED",
			StageLabel:    "Blocked",
			StageTone:     toneDanger,
			Updated:       "11 May 2026, 16:20 UTC",
		},
		{
			IntentID:      "int_01JX2B6D8F4G1HJK",
			Href:          "/workspace/journeys/int_01JX2B6D8F4G1HJK",
			WorkerName:    "Hana Sato",
			WorkerRef:     "worker:NW-17740",
			Headline:      "ENG-SWE3 · P3 → ENG-SWE4 · P4",
			PayLine:       "USD 152,000.00 → 164,500.00 (+8.2%)",
			EffectiveDate: "1 May 2026",
			Stage:         "COMPLETED",
			StageLabel:    "Completed",
			StageTone:     toneSuccess,
			Updated:       "30 Apr 2026, 13:55 UTC",
			InstanceID:    "wfi_01JWQ8T5R2V9XYZA",
		},
	}
}

func fixtureProposalForm() ProposalForm {
	return ProposalForm{
		Action: "/workspace/journeys/propose",
		Hidden: map[string]string{
			"csrf_token": fixtureCSRF,
			"form":       "propose",
			"return_to":  "/workspace/journeys",
		},
		Submit: "Propose and simulate",
		Fields: []Field{
			{
				ID: "propose-worker", Name: "worker_ref", Label: "Worker", Kind: fieldKindSelect,
				Required: true,
				Help:     "Only workers whose records you may read under this purpose are listed.",
				Options: []Option{
					{Value: "", Label: "Select a worker"},
					{Value: "worker:NW-40118", Label: "Omar Reyes — OPS-HRBP2 · P2"},
					{Value: "worker:NW-38204", Label: "Priya Raghunathan — FIN-ANALYST2 · P2", Selected: true},
					{Value: "worker:NW-21955", Label: "Marcus Devlin — SLS-AE3 · P3"},
					{Value: "worker:NW-17740", Label: "Hana Sato — ENG-SWE4 · P4"},
				},
			},
			{
				ID: "propose-job", Name: "target_job_code", Label: "Target job code", Kind: fieldKindText,
				Required: true, Value: "FIN-ANALYST3", Placeholder: "FIN-ANALYST3",
				Help: "The job the worker moves into. Their current placement is read from the record, not this form.",
			},
			{
				ID: "propose-grade", Name: "target_grade", Label: "Target grade", Kind: fieldKindSelect,
				Required: true,
				Options: []Option{
					{Value: "P2", Label: "P2"},
					{Value: "P3", Label: "P3", Selected: true},
					{Value: "P4", Label: "P4"},
					{Value: "P5", Label: "P5"},
				},
				Help: "More than one grade step needs a second approval.",
			},
			{
				ID: "propose-position", Name: "target_position_id", Label: "Target position", Kind: fieldKindText,
				Value: "POS-5512", Placeholder: "POS-0000",
				Help: "Leave blank to keep the worker in their current position.",
			},
			{
				ID: "propose-base", Name: "proposed_base", Label: "Proposed base pay", Kind: fieldKindNumber,
				Required: true, Value: "94000.00", Step: "0.01", Min: "0",
				Prefix: "USD", Suffix: "per year",
				Help: "In the worker's current currency. The budget envelope is checked during simulation.",
			},
			{
				ID: "propose-effective", Name: "effective_date", Label: "Effective date", Kind: fieldKindDate,
				Required: true, Value: "2026-07-01", Min: "2026-05-13",
				Help: "Must fall on or after today.",
			},
			{
				ID: "propose-reason", Name: "business_reason", Label: "Business reason", Kind: fieldKindTextarea,
				Required: true,
				Value:    "Took over the EMEA close after Q1 and now runs it end to end; scope matches the P3 profile.",
				Help:     "Kept with the intent and carried into the ledger event's evidence.",
			},
		},
	}
}

// SampleDetailPage is the journey at the decision point: admitted by the
// execution authority, parked on its approval work item, with the two
// governed decisions available in the rail and no ledger fact yet.
func SampleDetailPage() Page {
	return Page{
		Title:       "Omar Reyes · Promotion journey · Northwind People",
		Brand:       "Northwind People",
		TenantLabel: "Northwind Trading · US",
		Principal:   fixturePrincipal(),
		Nav:         fixtureNav(true),
		Notice: &Notice{
			Tone:   toneInfo,
			Title:  "This journey is waiting on a decision you are routed to make",
			Detail: "The approval work item is routed to the compensation approver. Deciding here acts as that principal and is recorded that way.",
		},
		Detail: &DetailView{
			Journey:    fixtureSubject(),
			Steps:      fixtureSteps(false),
			Proposal:   fixtureProposalFacts(),
			Comparison: fixtureComparison(),
			Findings:   fixtureFindings(),
			Engine:     fixtureEngineFacts("RUNNING", "approval"),
			Nodes:      fixtureNodes(false),
			WorkItems:  fixtureWorkItems(false),
			Evidence:   fixtureEvidence(false),
			Timeline:   fixtureTimeline(false),
			Actions:    fixtureActions(),

			PayBand:         fixturePayBand(),
			Budget:          fixtureBudget(),
			EffectiveWindow: fixtureEffectiveWindow(),
		},
		Footer: fixtureFooter(),
	}
}

// SampleCompletedDetailPage is the same journey after the approver decided:
// every step done, the terminal ledger fact recorded, and the two decisions
// present but refused because the work item is closed.
func SampleCompletedDetailPage() Page {
	p := SampleDetailPage()
	j := fixtureSubject()
	j.Stage = "COMPLETED"
	j.StageLabel = "Completed"
	j.StageTone = toneSuccess
	j.Updated = "12 May 2026, 10:04 UTC"

	actions := fixtureActions()
	for i := range actions {
		actions[i].Disabled = true
		actions[i].DisabledReason = "This journey completed on 12 May 2026; its approval work item is closed."
	}

	p.Title = "Omar Reyes · Promotion recorded · Northwind People"
	p.Notice = &Notice{
		Tone:   toneSuccess,
		Title:  "Promotion recorded for Omar Reyes",
		Detail: "The workflow reached its APPROVED terminal and wrote one promotion fact, effective 1 Jun 2026.",
	}
	p.Detail = &DetailView{
		Journey:    j,
		Steps:      fixtureSteps(true),
		Proposal:   fixtureProposalFacts(),
		Comparison: fixtureComparison(),
		Findings:   fixtureFindings(),
		Engine:     fixtureEngineFacts("COMPLETED", "record"),
		Nodes:      fixtureNodes(true),
		WorkItems:  fixtureWorkItems(true),
		Ledger:     fixtureLedger(),
		Evidence:   fixtureEvidence(true),
		Timeline:   fixtureTimeline(true),
		Actions:    actions,

		PayBand:         fixturePayBand(),
		Budget:          fixtureBudget(),
		EffectiveWindow: fixtureEffectiveWindow(),
	}
	return p
}

func fixtureSteps(complete bool) []Step {
	steps := []Step{
		{ID: "proposed", Label: "Proposed", Detail: "The manager submitted the change and the intent was created.",
			State: stepDone, At: "12 May 2026, 08:58 UTC"},
		{ID: "simulated", Label: "Simulated", Detail: "Preflight and transaction simulation produced an executable plan.",
			State: stepDone, At: "12 May 2026, 08:58 UTC"},
		{ID: "admitted", Label: "Admitted", Detail: "The P1B execution authority admitted the plan and started the workflow.",
			State: stepDone, At: "12 May 2026, 09:12 UTC"},
		{ID: "approval", Label: "Awaiting approval", Detail: "Parked on the approval work item routed to the compensation approver.",
			State: stepActive, At: "12 May 2026, 09:12 UTC"},
		{ID: "recorded", Label: "Recorded", Detail: "The terminal node writes exactly one promotion fact to the ledger.",
			State: stepUpcoming},
	}
	if complete {
		steps[3].State = stepDone
		steps[3].Detail = "The routed approver completed the work item and the driver resumed."
		steps[3].At = "12 May 2026, 10:02 UTC"
		steps[4].State = stepDone
		steps[4].At = "12 May 2026, 10:04 UTC"
	}
	return steps
}

func fixtureProposalFacts() []Fact {
	return []Fact{
		{Label: "Business reason", Value: "Has run the EMEA HRBP portfolio single-handed since January and now carries the P3 scope in full."},
		{Label: "Requested by", Value: "avery.okafor@northwind.example"},
		{Label: "Intent", Value: fixtureIntentID, Mono: true},
		{Label: "Correlation", Value: "cor_01JX6Y8B2C7D9EFG", Mono: true},
		{Label: "Proposal revision", Value: "rev_01JX6Y8B3H5J7KLM", Mono: true},
		{Label: "Material digest", Value: "sha256:6f1c9e2a7b40d38f", Mono: true},
	}
}

func fixtureComparison() []ComparisonRow {
	return []ComparisonRow{
		{Label: "Job code", Current: "OPS-HRBP2", Proposed: "OPS-HRBP3", Changed: true},
		{Label: "Grade", Current: "P2", Proposed: "P3", Delta: "+1 step", Changed: true},
		{Label: "Position", Current: "POS-4471", Proposed: "POS-4471"},
		{Label: "Org unit", Current: "People Operations · EMEA", Proposed: "People Operations · EMEA"},
		{Label: "Pay zone", Current: "ZONE-2", Proposed: "ZONE-2"},
		{Label: "Base pay", Current: "USD 93,000.00", Proposed: "USD 98,000.00",
			Delta: "+USD 5,000.00 (+5.4%)", Changed: true},
		{Label: "Compa-ratio", Current: "0.91", Proposed: "0.96", Delta: "+0.05", Changed: true},
		{Label: "Effective date", Current: "—", Proposed: "1 Jun 2026", Delta: "start of cycle", Changed: true},
	}
}

func fixtureFindings() []Finding {
	return []Finding{
		{Severity: severitySuccess, Code: "budget.envelope_within_limit",
			Message: "The increase fits the approved FY26 merit envelope for People Operations, with USD 41,200.00 left."},
		{Severity: severityInfo, Code: "policy.grade_step_single",
			Message: "One grade step needs a single approval; two or more would route a second."},
		{Severity: severityWarning, Code: "compa.above_zone_midpoint",
			Message: "The proposed base sits 4.2% above the P3 zone midpoint. The approver sees this before deciding."},
		{Severity: severitySuccess, Code: "eligibility.tenure_satisfied",
			Message: "Nineteen months in grade, above the twelve-month minimum."},
	}
}

func fixtureEngineFacts(status, current string) []Fact {
	tone := toneInfo
	if status == "COMPLETED" {
		tone = toneSuccess
	}
	return []Fact{
		{Label: "Instance", Value: fixtureInstanceID, Mono: true},
		{Label: "Version", Value: "7", Mono: true},
		{Label: "Workflow", Value: "promotion.approval@3", Mono: true},
		{Label: "Plan digest", Value: "sha256:a41d0be8c37f5219", Mono: true},
		{Label: "Status", Value: status, Tone: tone},
		{Label: "Current node", Value: current, Mono: true},
	}
}

func fixtureNodes(complete bool) []NodeRow {
	nodes := []NodeRow{
		{NodeID: "start", StepType: "START", Status: "Completed", Attempt: "1",
			Started: "12 May 2026, 09:12 UTC", Completed: "12 May 2026, 09:12 UTC", Tone: toneSuccess},
		{NodeID: "simulate", StepType: "TASK", Status: "Completed", Attempt: "1",
			Started: "12 May 2026, 09:12 UTC", Completed: "12 May 2026, 09:12 UTC", Tone: toneSuccess},
		{NodeID: "gate.p1b", StepType: "GATE", Status: "Completed", Attempt: "1",
			Started: "12 May 2026, 09:12 UTC", Completed: "12 May 2026, 09:12 UTC", Tone: toneSuccess},
		{NodeID: "approval", StepType: "HUMAN_TASK", Status: "Running", Attempt: "1",
			Started: "12 May 2026, 09:12 UTC", Completed: "—", Tone: toneWarning},
		{NodeID: "record", StepType: "END", Status: "Pending", Attempt: "0",
			Started: "—", Completed: "—", Tone: toneNeutral},
	}
	if complete {
		nodes[3].Status = "Completed"
		nodes[3].Completed = "12 May 2026, 10:02 UTC"
		nodes[3].Tone = toneSuccess
		nodes[4].Status = "Completed"
		nodes[4].Attempt = "1"
		nodes[4].Started = "12 May 2026, 10:02 UTC"
		nodes[4].Completed = "12 May 2026, 10:04 UTC"
		nodes[4].Tone = toneSuccess
	}
	return nodes
}

func fixtureWorkItems(complete bool) []WorkItemCard {
	item := WorkItemCard{
		ID:       "wi_01JX7Q2M6P1T4UVW",
		Kind:     "Approve promotion",
		Status:   "Open",
		Owner:    "dana.whitfield@northwind.example (compensation approver)",
		NodeID:   "approval",
		Deadline: "15 May 2026, 17:00 UTC",
		Tone:     toneWarning,
	}
	if complete {
		item.Status = "Completed"
		item.Tone = toneSuccess
		item.Claimed = "12 May 2026, 09:58 UTC"
		item.Completed = "dana.whitfield@northwind.example, 12 May 2026, 10:02 UTC"
	}
	return []WorkItemCard{item}
}

func fixtureLedger() *LedgerCard {
	return &LedgerCard{
		StreamKey:      "worker:NW-40118/promotion",
		Sequence:       "4",
		SchemaRef:      "hcm.promotion.recorded.v1",
		Digest:         "sha256:0d7e4c9a15b8f632",
		IdempotencyKey: "idem_01JX7Q2M4K8N3RA6_record",
		RecordedAt:     "12 May 2026, 10:04 UTC",
		EffectiveAt:    "1 Jun 2026",
	}
}

func fixtureEvidence(complete bool) []string {
	evidence := []string{
		"evd_cap_01JX6Y8B2C7D9EFG_worker_read",
		"evd_cap_01JX6Y8B2C7D9EFG_compensation_read",
		"evd_sim_01JX6Y8B3H5J7KLM",
		"evd_gate_01JX7Q2M4K8N3RA6_p1b",
		"evd_exec_01JX7Q2M4K8N3RA6",
	}
	if complete {
		evidence = append(evidence,
			"evd_workitem_01JX7Q2M6P1T4UVW_complete",
			"evd_ledger_01JX7Q2M4K8N3RA6_record")
	}
	return evidence
}

func fixtureTimeline(complete bool) []TimelineEvent {
	events := []TimelineEvent{}
	if complete {
		events = append(events,
			TimelineEvent{At: "12 May 2026, 10:04 UTC", Actor: "workflow", Title: "Promotion fact recorded",
				Detail: "One event written to worker:NW-40118/promotion at sequence 4.",
				Ref:    "sha256:0d7e4c9a15b8f632", Tone: toneSuccess},
			TimelineEvent{At: "12 May 2026, 10:02 UTC", Actor: "dana.whitfield@northwind.example",
				Title: "Approval work item completed", Detail: "Approved: scope and budget both check out.",
				Ref: "wi_01JX7Q2M6P1T4UVW", Tone: toneSuccess},
			TimelineEvent{At: "12 May 2026, 09:58 UTC", Actor: "dana.whitfield@northwind.example",
				Title: "Approval work item claimed", Ref: "wi_01JX7Q2M6P1T4UVW", Tone: toneInfo},
		)
	}
	events = append(events,
		TimelineEvent{At: "12 May 2026, 09:12 UTC", Actor: "workflow", Title: "Approval work item routed",
			Detail: "Routed to the compensation approver for the EMEA People Operations org unit.",
			Ref:    "wi_01JX7Q2M6P1T4UVW", Tone: toneWarning},
		TimelineEvent{At: "12 May 2026, 09:12 UTC", Actor: "execution authority", Title: "Plan admitted (P1B)",
			Detail: "The gate admitted the simulated plan and started instance " + fixtureInstanceID + ".",
			Ref:    "evd_gate_01JX7Q2M4K8N3RA6_p1b", Tone: toneInfo},
		TimelineEvent{At: "12 May 2026, 08:58 UTC", Actor: "avery.okafor@northwind.example", Title: "Proposal simulated",
			Detail: "Four findings, none blocking. The plan is executable.",
			Ref:    "rev_01JX6Y8B3H5J7KLM", Tone: toneNeutral},
		TimelineEvent{At: "12 May 2026, 08:58 UTC", Actor: "avery.okafor@northwind.example", Title: "Promotion proposed",
			Detail: "OPS-HRBP2 · P2 to OPS-HRBP3 · P3, effective 1 Jun 2026.",
			Ref:    fixtureIntentID, Tone: toneNeutral},
	)
	return events
}

func fixtureActions() []Action {
	approver := "dana.whitfield@northwind.example (compensation approver)"
	base := map[string]string{
		"csrf_token": fixtureCSRF,
		"intent_id":  fixtureIntentID,
		"return_to":  fixtureHref,
	}
	withDecision := func(decision string) map[string]string {
		m := make(map[string]string, len(base)+1)
		for k, v := range base {
			m[k] = v
		}
		m["decision"] = decision
		return m
	}
	return []Action{
		{
			ID: "approve", Label: "Approve promotion", Variant: "primary",
			Description: "Claims and completes the approval work item as the routed approver, resumes the driver, and lets the terminal node record the promotion fact.",
			Action:      fixtureHref + "/decide",
			Hidden:      withDecision("approve"),
			ActsAs:      approver,
			Fields: []Field{{
				ID: "approve-reason", Name: "reason", Label: "Reason for the record", Kind: fieldKindTextarea,
				Placeholder: "What made this the right call?",
				Help:        "Kept on the work item transition and carried into the ledger event's evidence.",
			}},
		},
		{
			ID: "return", Label: "Return to the manager", Variant: "danger",
			Description: "Rejects the proposal. The instance ends at its REJECTED terminal and no promotion fact is written.",
			Action:      fixtureHref + "/decide",
			Hidden:      withDecision("reject"),
			ActsAs:      approver,
			Fields: []Field{{
				ID: "return-reason", Name: "reason", Label: "What needs to change", Kind: fieldKindTextarea,
				Required: true,
				Help:     "Required. The manager sees this verbatim.",
			}},
		},
		{
			ID: "execute", Label: "Send to execution", Variant: "secondary",
			Description: "Runs ExecuteIntent behind the P1B execution authority gate.",
			Action:      fixtureHref + "/execute",
			Hidden: map[string]string{
				"csrf_token": fixtureCSRF,
				"intent_id":  fixtureIntentID,
				"return_to":  fixtureHref,
			},
			Disabled:       true,
			DisabledReason: "This journey was admitted on 12 May 2026; an intent cannot be executed twice.",
		},
	}
}

// fixturePayBand puts the proposal just above the P3 midpoint: high enough
// that the gauge has something to say, low enough that it is not a refusal.
// The two percentages are the marker positions along min-to-max, so 46.7
// and 60.0 put the current base below the midpoint tick at 50 and the
// proposed base above it -- the whole point of the drawing.
func fixturePayBand() *PayBand {
	return &PayBand{
		Min:         "USD 84,000.00",
		Mid:         "USD 94,000.00",
		Max:         "USD 108,000.00",
		Current:     "USD 93,000.00",
		Proposed:    "USD 98,000.00",
		Currency:    "USD",
		CurrentPct:  37.5,
		ProposedPct: 58.3,
		Note:        "The proposal sits 4.2% above the P3 zone midpoint and 9.3% below the band maximum.",
	}
}

// fixtureBudget leaves the envelope comfortable rather than tight, so the
// meter renders in its healthy tone; the warning and danger tones are
// covered by unit tests rather than by making the reference page look like
// a problem.
func fixtureBudget() *Budget {
	return &Budget{
		Available: "USD 220,000.00",
		Committed: "USD 173,800.00",
		Requested: "USD 5,000.00",
		UsedPct:   81.3,
		Note:      "USD 41,200.00 of the FY26 People Operations merit envelope would remain.",
	}
}

func fixtureEffectiveWindow() *EffectiveWindow {
	return &EffectiveWindow{
		Start:         "1 Apr 2026",
		EffectiveDate: "1 Jun 2026",
		KnownAt:       "12 May 2026, 09:12 UTC",
		Note:          "Every figure above was read as known at that instant; a later correction to the same period would not change this page retroactively.",
	}
}
