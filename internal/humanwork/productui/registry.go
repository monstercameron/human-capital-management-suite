package productui

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PageDefinition is the stable application-level contract for one product
// surface. Routing, page identity, navigation eligibility, and rendering live
// together so new enterprise modules do not grow a second switch statement.
type PageDefinition struct {
	ID          PageID
	Route       string
	Label       string
	Icon        string
	Title       string
	Subtitle    string
	LabelKey    string
	TitleKey    string
	SubtitleKey string
	// SearchTerms are stable aliases and business-language concepts for the
	// navigation command palette. Label and localized subtitle are searched
	// automatically; these terms cover vocabulary customers commonly use.
	SearchTerms []string
	PrimaryNav  bool
	ParentNav   PageID
	RenderOrder int
	// Admitted declares that a genuinely usable, released capability stands
	// behind this page: PrimaryNav and ParentNav wire a route into the menu
	// tree, but only an admitted page may actually render as a navigation
	// destination there. The zero value is false, so a page nobody has
	// explicitly admitted never claims a menu slot even if it declares
	// PrimaryNav or ParentNav -- a future contributor who wires a new
	// governed-service fallback into the tree cannot reintroduce this
	// defect by omission. Admitted never gates direct route access; that
	// remains PageVisible's job. It only controls whether the registry
	// presents the destination as a first-class menu item.
	Admitted bool
	render   func(View) ui.Node
}

const RoleHCMAdmin = "hcm_admin"

// PageVisible reports product-route access for server-admitted roles. An
// empty role set receives only the safe shell baseline and cannot discover
// workforce or administrative surfaces.
func PageVisible(page PageID, roles []string) bool {
	if hasAnyProductRole(roles, RoleHCMAdmin, "comp_admin") {
		return true
	}
	switch page {
	case PageHome, PageHelp, PageSettings:
		return true
	case PagePortal:
		// External candidates hold no workspace roles; the
		// portal admits them role-less. It is not primary
		// navigation and exposes no workforce surfaces.
		return true
	case PageMyself, PageTimeHub, PageTimeEntry, PageTimeCorrection, PageTimeOff, PageTimeOffRequest, PageProtectedLeave, PageLeaveTimeline, PageReturnToWork, PagePaySummary, PagePayStatements, PagePayDiscrepancy, PageBenefitsOverview, PageBenefitsCompare, PageBenefitsEnroll, PageGrowthHome, PageGoalPlanning, PageGovernedFeedback, PageSkillsProfile, PageAssignedLearning, PageCareerDiscovery, PageOrgExplorer, PageOrgOutline, PageOrgEffectiveDate, PagePositionObject, PagePositionOccupancy, PageHeadcountPlan, PageWorkforceScenario, PageGovernedPopulation, PageCostCapacity, PageReorgProposals, PagePlannedVsCommitted, PageOrgResponsive, PageHelpHub, PageKnowledgeSearch, PageHRServiceRequest, PageConfidentialCase, PageCaseStatus, PageCaseMessaging, PageCaseAppeal, PageExitInitiation, PageExitDetails, PageOffboardingImpact, PageFinalPay, PageFinalDocuments, PageRetainedObligations, PageOrganization:
		return hasAnyProductRole(roles, "worker_self", "manager", "hr_partner", "hiring_manager", "payroll_manager")
	// Restricted evidence surfaces admit HR partners only;
	// the platform admin bypass above still applies.
	case PageLeaveEvidence:
		return hasAnyProductRole(roles, "hr_partner")
	case PageJourneys, PageWork, PageHistory, PagePeople, PagePerson, PageHeadcount, PagePosition, PageRequisition, PageCandidates, PageCandidate, PageInterviews, PageEvaluation, PageOffer, PageOnboarding, PageOnboardingTasks, PageActivationReadiness, PageTimeApproval, PageTimeExceptions, PageTeamCoverage, PageCompProposals, PageSalaryComparison, PageCyclePopulations, PageCompWorksheet, PageCompCalibration, PagePayBenefitRecon, PageManagerCheckins, PagePerfReview, PageReviewParticipants, PageTalentWorkbench, PageTalentCalibration, PageSuccessionPlanning, PageCaseCenter, PageCaseAssignment, PageCaseEvidence, PageCaseDisposition, PageCaseRedaction, PageExitReview, PageOffboardingPlan, PageReassignmentReview, PageAccessEquipment, PageOffboardingEffects, PageExitCompletion, PageReportCatalog, PageReportTypes, PageAnalysisFloorplan, PageAnalysisFilters, PageResultLineage, PageDataFreshness, PageAggregateSuppression, PageReportExport, PageReportSharing, PageNLAnalysis, PageAnalysisHandoff, PageAccessibleViz, PageInsights:
		return hasAnyProductRole(roles, "manager", "hr_partner", "comp_admin", "hiring_manager", "payroll_manager")
	default:
		return false
	}
}

func hasAnyProductRole(roles []string, wanted ...string) bool {
	for _, role := range wanted {
		if hasProductRole(roles, role) {
			return true
		}
	}
	return false
}

func hasProductRole(roles []string, wanted string) bool {
	for _, role := range roles {
		if role == wanted {
			return true
		}
	}
	return false
}

func registeredPages() []PageDefinition {
	return []PageDefinition{
		{ID: PageHome, Route: "/workspace/app/home", Label: "Home", Icon: "home", Title: "Home", Subtitle: "Review live requests and keep your work moving.", LabelKey: "page.home.label", TitleKey: "page.home.title", SubtitleKey: "page.home.subtitle", SearchTerms: []string{"dashboard", "overview", "landing", "start"}, PrimaryNav: true, RenderOrder: 10, Admitted: true, render: homePage},
		{ID: PageMyself, Route: "/workspace/app/myself", Label: "Myself", Icon: "people", Title: "Myself", Subtitle: "Your employment, organization, payroll, and workflow information.", LabelKey: "page.myself.label", TitleKey: "page.myself.title", SubtitleKey: "page.myself.subtitle", SearchTerms: []string{"me", "my profile", "self service", "employment", "payroll", "compensation", "salary", "payslip", "personal information"}, PrimaryNav: true, RenderOrder: 12, Admitted: true, render: myselfPage},
		{ID: PageJourneys, Route: "/workspace/app/journeys", Label: "Journeys", Icon: "journeys", Title: "Journeys", Subtitle: "Start, follow, and complete governed employee workflows.", LabelKey: "page.journeys.label", TitleKey: "page.journeys.title", SubtitleKey: "page.journeys.subtitle", SearchTerms: []string{"workflow", "promotion", "request", "approval", "lifecycle"}, PrimaryNav: true, RenderOrder: 15, Admitted: true, render: journeysPage},
		{ID: PageWork, Route: "/workspace/app/work", Label: "My Work", Icon: "work", Title: "My Work", Subtitle: "Promotion decisions and tasks that need your action.", LabelKey: "page.work.label", TitleKey: "page.work.title", SubtitleKey: "page.work.subtitle", SearchTerms: []string{"tasks", "inbox", "queue", "assigned", "pending", "approvals"}, PrimaryNav: true, RenderOrder: 20, Admitted: true, render: workPage},
		{ID: PageHistory, Route: "/workspace/app/history", Label: "Work History", Icon: "history", Title: "Workflow History", Subtitle: "Review completed, rejected, and failed workflow records.", LabelKey: "page.history.label", TitleKey: "page.history.title", SubtitleKey: "page.history.subtitle", SearchTerms: []string{"past", "completed", "rejected", "failed", "audit", "records"}, ParentNav: PageWork, RenderOrder: 25, Admitted: true, render: historyPage},
		{ID: PagePeople, Route: "/workspace/app/people", Label: "People", Icon: "people", Title: "People", Subtitle: "People you're authorized to view across the organization.", LabelKey: "page.people.label", TitleKey: "page.people.title", SubtitleKey: "page.people.subtitle", SearchTerms: []string{"employees", "workers", "directory", "profiles", "staff", "team", "colleagues"}, PrimaryNav: true, RenderOrder: 30, Admitted: true, render: peoplePage},
		{ID: PagePerson, Route: "/workspace/app/person", Label: "Person", Icon: "people", Title: "Person profile", Subtitle: "Worker facts and available governed workflows.", LabelKey: "page.person.label", TitleKey: "page.person.title", SubtitleKey: "page.person.subtitle", SearchTerms: []string{"employee", "worker", "profile", "employment"}, RenderOrder: 35, Admitted: true, render: personPage},
		{ID: PageHeadcount, Route: "/workspace/app/headcount", Label: "Headcount", Icon: "people", Title: "Headcount requests", Subtitle: "Request headcount through the governed requisition service.", LabelKey: "page.headcount.label", TitleKey: "page.headcount.title", SubtitleKey: "page.headcount.subtitle", SearchTerms: []string{"headcount", "head count", "requisition", "hiring", "open roles"}, RenderOrder: 36, render: headcountPage},
		{ID: PagePosition, Route: "/workspace/app/position", Label: "Positions", Icon: "people", Title: "Position requests", Subtitle: "Request positions through the governed position service.", LabelKey: "page.position.label", TitleKey: "page.position.title", SubtitleKey: "page.position.subtitle", SearchTerms: []string{"positions", "roles", "job requisition", "openings", "vacancies"}, RenderOrder: 37, render: positionPage},
		{ID: PageRequisition, Route: "/workspace/app/requisition", Label: "Requisitions", Icon: "people", Title: "Requisition workspace", Subtitle: "Track requisitions through the governed requisition service.", LabelKey: "page.requisition.label", TitleKey: "page.requisition.title", SubtitleKey: "page.requisition.subtitle", SearchTerms: []string{"requisitions", "hiring workspace", "openings", "candidates", "interviews"}, RenderOrder: 38, render: requisitionPage},
		{ID: PageCandidates, Route: "/workspace/app/candidates", Label: "Candidates", Icon: "people", Title: "Candidate pipeline", Subtitle: "Follow candidates through the governed candidacy service.", LabelKey: "page.candidates.label", TitleKey: "page.candidates.title", SubtitleKey: "page.candidates.subtitle", SearchTerms: []string{"candidates", "pipeline", "applicants", "interviews", "hiring"}, RenderOrder: 39, render: candidatesPage},
		{ID: PageCandidate, Route: "/workspace/app/candidate", Label: "Candidate", Icon: "people", Title: "Candidate profile", Subtitle: "Candidate facts from the governed candidacy service.", LabelKey: "page.candidate.label", TitleKey: "page.candidate.title", SubtitleKey: "page.candidate.subtitle", SearchTerms: []string{"candidate", "applicant", "profile", "resume"}, RenderOrder: 40, render: candidatePage},
		{ID: PageInterviews, Route: "/workspace/app/interviews", Label: "Interviews", Icon: "people", Title: "Interview scheduling", Subtitle: "Schedule interviews through the governed scheduling service.", LabelKey: "page.interviews.label", TitleKey: "page.interviews.title", SubtitleKey: "page.interviews.subtitle", SearchTerms: []string{"interviews", "scheduling", "slots", "interviewers", "calendar"}, RenderOrder: 41, render: interviewsPage},
		{ID: PageEvaluation, Route: "/workspace/app/evaluation", Label: "Evaluation", Icon: "people", Title: "Candidate evaluation", Subtitle: "Evaluate candidates through the governed evaluation service.", LabelKey: "page.evaluation.label", TitleKey: "page.evaluation.title", SubtitleKey: "page.evaluation.subtitle", SearchTerms: []string{"evaluation", "scorecard", "ratings", "feedback", "assessment"}, RenderOrder: 42, render: evaluationPage},
		{ID: PageOffer, Route: "/workspace/app/offer", Label: "Offer", Icon: "people", Title: "Offer review", Subtitle: "Review offers through the governed offer service.", LabelKey: "page.offer.label", TitleKey: "page.offer.title", SubtitleKey: "page.offer.subtitle", SearchTerms: []string{"offer", "offer letter", "acceptance", "compensation", "signing"}, RenderOrder: 43, render: offerPage},
		{ID: PagePortal, Route: "/workspace/app/portal", Label: "Portal", Icon: "people", Title: "Candidate portal", Subtitle: "External candidate portal for the governed candidacy service.", LabelKey: "page.portal.label", TitleKey: "page.portal.title", SubtitleKey: "page.portal.subtitle", SearchTerms: []string{"portal", "external", "candidates", "apply", "careers"}, RenderOrder: 44, render: portalPage},
		{ID: PageOnboarding, Route: "/workspace/app/onboarding", Label: "Onboarding", Icon: "people", Title: "Onboarding plans", Subtitle: "Guide new hires through the governed onboarding service.", LabelKey: "page.onboarding.label", TitleKey: "page.onboarding.title", SubtitleKey: "page.onboarding.subtitle", SearchTerms: []string{"onboarding", "new hire", "orientation", "checklist", "first day"}, RenderOrder: 45, render: onboardingPage},
		{ID: PageOnboardingTasks, Route: "/workspace/app/onboarding/tasks", Label: "Onboarding tasks", Icon: "people", Title: "Onboarding task completion", Subtitle: "Complete onboarding tasks through the governed onboarding service.", LabelKey: "page.onboarding_tasks.label", TitleKey: "page.onboarding_tasks.title", SubtitleKey: "page.onboarding_tasks.subtitle", SearchTerms: []string{"onboarding tasks", "checklist", "complete", "requirements", "due"}, RenderOrder: 46, render: onboardingTasksPage},
		{ID: PageActivationReadiness, Route: "/workspace/app/onboarding/readiness", Label: "Activation readiness", Icon: "people", Title: "Worker-activation readiness", Subtitle: "Prove workers ready to activate through the governed activation service.", LabelKey: "page.activation_readiness.label", TitleKey: "page.activation_readiness.title", SubtitleKey: "page.activation_readiness.subtitle", SearchTerms: []string{"activation", "readiness", "go-live", "start date", "clearance"}, RenderOrder: 47, render: activationReadinessPage},
		{ID: PageTimeHub, Route: "/workspace/app/time", Label: "Time hub", Icon: "people", Title: "Employee time hub", Subtitle: "Balances, requests, and leave cases from the governed time service.", LabelKey: "page.time_hub.label", TitleKey: "page.time_hub.title", SubtitleKey: "page.time_hub.subtitle", SearchTerms: []string{"time", "balances", "time off", "leave", "requests"}, RenderOrder: 48, render: timeHubPage},
		{ID: PageTimeEntry, Route: "/workspace/app/time/entry", Label: "Time entry", Icon: "people", Title: "Accessible time entry", Subtitle: "Record hours worked through the governed time service.", LabelKey: "page.time_entry.label", TitleKey: "page.time_entry.title", SubtitleKey: "page.time_entry.subtitle", SearchTerms: []string{"time entry", "hours", "timesheet", "clock", "record"}, RenderOrder: 49, render: timeEntryPage},
		{ID: PageTimeCorrection, Route: "/workspace/app/time/correction", Label: "Time correction", Icon: "people", Title: "Time correction", Subtitle: "Fix recorded time through the governed time service.", LabelKey: "page.time_correction.label", TitleKey: "page.time_correction.title", SubtitleKey: "page.time_correction.subtitle", SearchTerms: []string{"correction", "fix", "adjust", "amend", "timesheet"}, RenderOrder: 50, render: timeCorrectionPage},
		{ID: PageTimeApproval, Route: "/workspace/app/time/approval", Label: "Time approval", Icon: "people", Title: "Manager time approval", Subtitle: "Approve team time through the governed time service.", LabelKey: "page.time_approval.label", TitleKey: "page.time_approval.title", SubtitleKey: "page.time_approval.subtitle", SearchTerms: []string{"approve", "team time", "requests", "timesheet", "manager"}, RenderOrder: 51, render: timeApprovalPage},
		{ID: PageTimeExceptions, Route: "/workspace/app/time/exceptions", Label: "Time exceptions", Icon: "people", Title: "Time-exception workbench", Subtitle: "Resolve time exceptions through the governed time service.", LabelKey: "page.time_exceptions.label", TitleKey: "page.time_exceptions.title", SubtitleKey: "page.time_exceptions.subtitle", SearchTerms: []string{"exceptions", "errors", "missing", "resolve", "workbench"}, RenderOrder: 52, render: timeExceptionsPage},
		{ID: PageTimeOff, Route: "/workspace/app/time/off", Label: "Time off", Icon: "people", Title: "Time-off balance and calendar", Subtitle: "Balances and calendar from the governed time service.", LabelKey: "page.time_off.label", TitleKey: "page.time_off.title", SubtitleKey: "page.time_off.subtitle", SearchTerms: []string{"time off", "vacation", "calendar", "balances", "holiday"}, RenderOrder: 53, render: timeOffPage},
		{ID: PageTimeOffRequest, Route: "/workspace/app/time/off/request", Label: "Request time off", Icon: "people", Title: "Time-off request journey", Subtitle: "Ask for time off through the governed time service.", LabelKey: "page.time_off_request.label", TitleKey: "page.time_off_request.title", SubtitleKey: "page.time_off_request.subtitle", SearchTerms: []string{"request", "time off", "vacation", "ask", "journey"}, RenderOrder: 54, render: timeOffRequestPage},
		{ID: PageTeamCoverage, Route: "/workspace/app/time/coverage", Label: "Team coverage", Icon: "people", Title: "Team-coverage review", Subtitle: "Review team coverage through the governed time service.", LabelKey: "page.team_coverage.label", TitleKey: "page.team_coverage.title", SubtitleKey: "page.team_coverage.subtitle", SearchTerms: []string{"coverage", "team", "absent", "review", "staffing"}, RenderOrder: 55, render: teamCoveragePage},
		{ID: PageProtectedLeave, Route: "/workspace/app/time/protected-leave", Label: "Protected leave", Icon: "people", Title: "Protected-leave intake", Subtitle: "Start a protected leave case through the governed leave service.", LabelKey: "page.protected_leave.label", TitleKey: "page.protected_leave.title", SubtitleKey: "page.protected_leave.subtitle", SearchTerms: []string{"protected", "leave", "medical", "intake", "case"}, RenderOrder: 56, render: protectedLeavePage},
		{ID: PageLeaveEvidence, Route: "/workspace/app/time/leave-evidence", Label: "Leave evidence", Icon: "people", Title: "Restricted leave-evidence tasks", Subtitle: "Complete restricted evidence through the governed leave service.", LabelKey: "page.leave_evidence.label", TitleKey: "page.leave_evidence.title", SubtitleKey: "page.leave_evidence.subtitle", SearchTerms: []string{"evidence", "restricted", "documents", "tasks", "leave"}, RenderOrder: 57, render: leaveEvidencePage},
		{ID: PageLeaveTimeline, Route: "/workspace/app/time/leave-timeline", Label: "Leave timeline", Icon: "people", Title: "Leave-status timeline", Subtitle: "Follow a leave case through the governed leave service.", LabelKey: "page.leave_timeline.label", TitleKey: "page.leave_timeline.title", SubtitleKey: "page.leave_timeline.subtitle", SearchTerms: []string{"timeline", "status", "history", "case", "leave"}, RenderOrder: 58, render: leaveTimelinePage},
		{ID: PageReturnToWork, Route: "/workspace/app/time/return-to-work", Label: "Return to work", Icon: "people", Title: "Return-to-work planning", Subtitle: "Plan a return from leave through the governed leave service.", LabelKey: "page.return_to_work.label", TitleKey: "page.return_to_work.title", SubtitleKey: "page.return_to_work.subtitle", SearchTerms: []string{"return", "back to work", "plan", "recovery", "leave"}, RenderOrder: 59, render: returnToWorkPage},
		{ID: PageOrganization, Route: "/workspace/app/organization", Label: "Organization", Icon: "organization", Title: "Organization", Subtitle: "See where each visible employee sits in the organization.", LabelKey: "page.organization.label", TitleKey: "page.organization.title", SubtitleKey: "page.organization.subtitle", SearchTerms: []string{"org chart", "departments", "teams", "structure", "hierarchy", "reporting"}, PrimaryNav: true, RenderOrder: 60, Admitted: true, render: organizationPage},
		{ID: PageInsights, Route: "/workspace/app/insights", Label: "Insights", Icon: "insights", Title: "Insights", Subtitle: "Operational counts derived from live journey states.", LabelKey: "page.insights.label", TitleKey: "page.insights.title", SubtitleKey: "page.insights.subtitle", SearchTerms: []string{"analytics", "reports", "metrics", "trends", "workforce data"}, PrimaryNav: true, RenderOrder: 61, Admitted: true, render: insightsPage},
		{ID: PageAdmin, Route: "/workspace/app/admin", Label: "Admin", Icon: "admin", Title: "Admin", Subtitle: "Published service capabilities and configuration availability.", LabelKey: "page.admin.label", TitleKey: "page.admin.title", SubtitleKey: "page.admin.subtitle", SearchTerms: []string{"administration", "configuration", "system", "capabilities", "manage"}, PrimaryNav: true, RenderOrder: 62, Admitted: true, render: adminPage},
		{ID: PageWorkerIDs, Route: "/workspace/app/admin/worker-ids", Label: "Worker IDs", Icon: "people", Title: "Worker ID rules", Subtitle: "Configure how this organization issues unique worker numbers.", LabelKey: "page.worker_ids.label", TitleKey: "page.worker_ids.title", SubtitleKey: "page.worker_ids.subtitle", SearchTerms: []string{"worker number", "personnel number", "prefix", "sequence", "identifier", "numbering"}, ParentNav: PageAdmin, RenderOrder: 63, Admitted: true, render: workerIDsPage},
		{ID: PageRoles, Route: "/workspace/app/admin/roles", Label: "Roles & access", Icon: "admin", Title: "Roles & access", Subtitle: "Create roles and assign one or more roles across the workforce.", LabelKey: "page.roles.label", TitleKey: "page.roles.title", SubtitleKey: "page.roles.subtitle", SearchTerms: []string{"authorization", "roles", "permissions", "workforce access", "assignment", "rbac"}, ParentNav: PageAdmin, RenderOrder: 64, Admitted: true, render: rolesPage},
		{ID: PageOrganizationVisibility, Route: "/workspace/app/admin/organization-visibility", Label: "Organization visibility", Icon: "organization", Title: "Organization visibility", Subtitle: "Control which organization units each role can discover.", LabelKey: "page.organization_visibility.label", TitleKey: "page.organization_visibility.title", SubtitleKey: "page.organization_visibility.subtitle", SearchTerms: []string{"org chart access", "directory visibility", "role visibility", "allowlist", "denylist", "own team", "organization units"}, ParentNav: PageAdmin, RenderOrder: 65, Admitted: true, render: organizationVisibilityPage},
		{ID: PageAppearance, Route: "/workspace/app/appearance", Label: "Brand & appearance", Icon: "palette", Title: "Brand & appearance", Subtitle: "Shape a consistent workspace identity with governed, accessible theme choices.", LabelKey: "page.appearance.label", TitleKey: "page.appearance.title", SubtitleKey: "page.appearance.subtitle", SearchTerms: []string{"branding", "theme", "colors", "logo", "dark mode", "styling", "shapes", "glyphs"}, ParentNav: PageAdmin, RenderOrder: 66, Admitted: true, render: appearancePage},
		// UXAUDIT-011: Experience Studio keeps its ParentNav wiring and
		// registered route -- direct navigation still renders its honest
		// "not available" state -- but it is deliberately left Admitted:
		// false (the zero value). No governed page-builder service backs
		// it, so it must not claim a menu slot under Admin.
		{ID: PageStudio, Route: "/workspace/app/studio", Label: "Experience Studio", Icon: "studio", Title: "Experience Studio", Subtitle: "Customer page configuration requires its governed service.", LabelKey: "page.studio.label", TitleKey: "page.studio.title", SubtitleKey: "page.studio.subtitle", SearchTerms: []string{"custom pages", "layout", "builder", "designer", "experience", "configuration"}, ParentNav: PageAdmin, RenderOrder: 70, render: studioPage},
		{ID: PageHelp, Route: "/workspace/app/help", Label: "Help", Icon: "help", Title: "Help center", Subtitle: "Guidance for the live promotion workflow.", LabelKey: "page.help.label", TitleKey: "page.help.title", SubtitleKey: "page.help.subtitle", SearchTerms: []string{"support", "guidance", "documentation", "docs", "assistance"}, RenderOrder: 80, Admitted: true, render: helpPage},
		{ID: PageSettings, Route: "/workspace/app/settings", Label: "Settings", Icon: "settings", Title: "Settings", Subtitle: "Current authenticated session and available preferences.", LabelKey: "page.settings.label", TitleKey: "page.settings.title", SubtitleKey: "page.settings.subtitle", SearchTerms: []string{"preferences", "locale", "language", "accessibility", "account", "session"}, RenderOrder: 90, Admitted: true, render: settingsPage},
		// Post-settings extension range: unpublished fallback
		// surfaces land here in delivery order so the governed
		// tail above never renumbers.
		{ID: PagePaySummary, Route: "/workspace/app/pay/summary", Label: "Pay summary", Icon: "people", Title: "Employee pay summary", Subtitle: "Pay figures from the governed pay service.", LabelKey: "page.pay_summary.label", TitleKey: "page.pay_summary.title", SubtitleKey: "page.pay_summary.subtitle", SearchTerms: []string{"pay", "salary", "summary", "earnings", "wages"}, RenderOrder: 91, render: paySummaryPage},
		{ID: PagePayStatements, Route: "/workspace/app/pay/statements", Label: "Pay statements", Icon: "people", Title: "Accessible pay statements", Subtitle: "Statements from the governed pay service.", LabelKey: "page.pay_statements.label", TitleKey: "page.pay_statements.title", SubtitleKey: "page.pay_statements.subtitle", SearchTerms: []string{"statements", "payslip", "period", "pay", "documents"}, RenderOrder: 92, render: payStatementsPage},
		{ID: PagePayDiscrepancy, Route: "/workspace/app/pay/discrepancy", Label: "Pay discrepancy", Icon: "people", Title: "Pay-discrepancy intake", Subtitle: "Report a pay problem through the governed pay service.", LabelKey: "page.pay_discrepancy.label", TitleKey: "page.pay_discrepancy.title", SubtitleKey: "page.pay_discrepancy.subtitle", SearchTerms: []string{"discrepancy", "wrong pay", "report", "problem", "intake"}, RenderOrder: 93, render: payDiscrepancyPage},
		{ID: PageCompProposals, Route: "/workspace/app/pay/comp-proposals", Label: "Comp proposals", Icon: "people", Title: "Manager compensation proposals", Subtitle: "Propose compensation through the governed compensation service.", LabelKey: "page.comp_proposals.label", TitleKey: "page.comp_proposals.title", SubtitleKey: "page.comp_proposals.subtitle", SearchTerms: []string{"compensation", "propose", "merit", "bonus", "manager"}, RenderOrder: 94, render: compProposalsPage},
		{ID: PageSalaryComparison, Route: "/workspace/app/pay/salary-comparison", Label: "Salary comparison", Icon: "people", Title: "Salary-range and budget comparison", Subtitle: "Compare ranges and budgets through the governed compensation service.", LabelKey: "page.salary_comparison.label", TitleKey: "page.salary_comparison.title", SubtitleKey: "page.salary_comparison.subtitle", SearchTerms: []string{"salary", "range", "budget", "bands", "compare"}, RenderOrder: 95, render: salaryComparisonPage},
		{ID: PageCyclePopulations, Route: "/workspace/app/pay/cycle-populations", Label: "Cycle populations", Icon: "people", Title: "Compensation-cycle populations", Subtitle: "Scope cycle populations through the governed compensation service.", LabelKey: "page.cycle_populations.label", TitleKey: "page.cycle_populations.title", SubtitleKey: "page.cycle_populations.subtitle", SearchTerms: []string{"cycle", "population", "scope", "eligibility", "compensation"}, RenderOrder: 96, render: cyclePopulationsPage},
		{ID: PageCompWorksheet, Route: "/workspace/app/pay/comp-worksheet", Label: "Comp worksheet", Icon: "people", Title: "Compensation worksheet", Subtitle: "Work the cycle through the governed compensation service.", LabelKey: "page.comp_worksheet.label", TitleKey: "page.comp_worksheet.title", SubtitleKey: "page.comp_worksheet.subtitle", SearchTerms: []string{"worksheet", "cycle", "adjust", "rows", "compensation"}, RenderOrder: 97, render: compWorksheetPage},
		{ID: PageCompCalibration, Route: "/workspace/app/pay/comp-calibration", Label: "Comp calibration", Icon: "people", Title: "Compensation calibration", Subtitle: "Calibrate awards through the governed compensation service.", LabelKey: "page.comp_calibration.label", TitleKey: "page.comp_calibration.title", SubtitleKey: "page.comp_calibration.subtitle", SearchTerms: []string{"calibration", "ratings", "awards", "session", "compensation"}, RenderOrder: 98, render: compCalibrationPage},
		{ID: PageBenefitsOverview, Route: "/workspace/app/benefits/overview", Label: "Benefits overview", Icon: "people", Title: "Benefit-program overview", Subtitle: "Programs from the governed benefits service.", LabelKey: "page.benefits_overview.label", TitleKey: "page.benefits_overview.title", SubtitleKey: "page.benefits_overview.subtitle", SearchTerms: []string{"benefits", "programs", "overview", "enrollment", "coverage"}, RenderOrder: 99, render: benefitsOverviewPage},
		{ID: PageBenefitsCompare, Route: "/workspace/app/benefits/compare", Label: "Benefits compare", Icon: "people", Title: "Benefit-plan comparison", Subtitle: "Compare plans through the governed benefits service.", LabelKey: "page.benefits_compare.label", TitleKey: "page.benefits_compare.title", SubtitleKey: "page.benefits_compare.subtitle", SearchTerms: []string{"compare", "plans", "versus", "options", "benefits"}, RenderOrder: 100, render: benefitsComparePage},
		{ID: PageBenefitsEnroll, Route: "/workspace/app/benefits/enroll", Label: "Benefits enroll", Icon: "people", Title: "Benefit enrollment", Subtitle: "Enroll through the governed benefits service.", LabelKey: "page.benefits_enroll.label", TitleKey: "page.benefits_enroll.title", SubtitleKey: "page.benefits_enroll.subtitle", SearchTerms: []string{"enroll", "election", "sign up", "choose", "benefits"}, RenderOrder: 101, render: benefitsEnrollPage},
		{ID: PagePayBenefitRecon, Route: "/workspace/app/pay/reconciliation", Label: "Pay-benefit recon", Icon: "people", Title: "Payroll and benefit reconciliation status", Subtitle: "Agreement status from the governed reconciliation service.", LabelKey: "page.pay_benefit_recon.label", TitleKey: "page.pay_benefit_recon.title", SubtitleKey: "page.pay_benefit_recon.subtitle", SearchTerms: []string{"reconciliation", "agree", "breaks", "payroll", "benefits"}, RenderOrder: 102, render: payBenefitReconPage},
		{ID: PageGrowthHome, Route: "/workspace/app/growth", Label: "Growth home", Icon: "people", Title: "Employee Growth home", Subtitle: "Growth from the governed growth service.", LabelKey: "page.growth_home.label", TitleKey: "page.growth_home.title", SubtitleKey: "page.growth_home.subtitle", SearchTerms: []string{"growth", "career", "home", "develop", "progress"}, RenderOrder: 103, render: growthHomePage},
		{ID: PageGoalPlanning, Route: "/workspace/app/growth/goals", Label: "Goal planning", Icon: "people", Title: "Goal planning", Subtitle: "Plan goals through the governed growth service.", LabelKey: "page.goal_planning.label", TitleKey: "page.goal_planning.title", SubtitleKey: "page.goal_planning.subtitle", SearchTerms: []string{"goals", "plan", "target", "milestone", "objective"}, RenderOrder: 104, render: goalPlanningPage},
		{ID: PageGovernedFeedback, Route: "/workspace/app/growth/feedback", Label: "Governed feedback", Icon: "people", Title: "Governed feedback", Subtitle: "Exchange feedback through the governed growth service.", LabelKey: "page.governed_feedback.label", TitleKey: "page.governed_feedback.title", SubtitleKey: "page.governed_feedback.subtitle", SearchTerms: []string{"feedback", "praise", "kudos", "exchange", "growth"}, RenderOrder: 105, render: governedFeedbackPage},
		{ID: PageManagerCheckins, Route: "/workspace/app/growth/checkins", Label: "Manager check-ins", Icon: "people", Title: "Manager check-ins", Subtitle: "Run check-ins through the governed growth service.", LabelKey: "page.manager_checkins.label", TitleKey: "page.manager_checkins.title", SubtitleKey: "page.manager_checkins.subtitle", SearchTerms: []string{"check-ins", "one-on-one", "notes", "manager", "growth"}, RenderOrder: 106, render: managerCheckinsPage},
		{ID: PagePerfReview, Route: "/workspace/app/growth/perf-review", Label: "Perf review", Icon: "people", Title: "Performance-review workspace", Subtitle: "Review performance through the governed growth service.", LabelKey: "page.perf_review.label", TitleKey: "page.perf_review.title", SubtitleKey: "page.perf_review.subtitle", SearchTerms: []string{"performance", "review", "rating", "workspace", "growth"}, RenderOrder: 107, render: perfReviewPage},
		{ID: PageReviewParticipants, Route: "/workspace/app/growth/review-participants", Label: "Review participants", Icon: "people", Title: "Review-participant visibility", Subtitle: "Disclose participants through the governed growth service.", LabelKey: "page.review_participants.label", TitleKey: "page.review_participants.title", SubtitleKey: "page.review_participants.subtitle", SearchTerms: []string{"participants", "reviewer", "reviewee", "visibility", "growth"}, RenderOrder: 108, render: reviewParticipantsPage},
		{ID: PageSkillsProfile, Route: "/workspace/app/growth/skills", Label: "Skills profile", Icon: "people", Title: "Governed skills profile", Subtitle: "Skills from the governed growth service.", LabelKey: "page.skills_profile.label", TitleKey: "page.skills_profile.title", SubtitleKey: "page.skills_profile.subtitle", SearchTerms: []string{"skills", "profile", "proficiency", "endorsed", "growth"}, RenderOrder: 109, render: skillsProfilePage},
		{ID: PageAssignedLearning, Route: "/workspace/app/growth/learning", Label: "Assigned learning", Icon: "people", Title: "Assigned learning", Subtitle: "Assignments from the governed learning service.", LabelKey: "page.assigned_learning.label", TitleKey: "page.assigned_learning.title", SubtitleKey: "page.assigned_learning.subtitle", SearchTerms: []string{"learning", "assigned", "courses", "training", "growth"}, RenderOrder: 110, render: assignedLearningPage},
		{ID: PageCareerDiscovery, Route: "/workspace/app/growth/opportunities", Label: "Career discovery", Icon: "people", Title: "Career-opportunity discovery", Subtitle: "Openings from the governed growth service.", LabelKey: "page.career_discovery.label", TitleKey: "page.career_discovery.title", SubtitleKey: "page.career_discovery.subtitle", SearchTerms: []string{"career", "opportunities", "openings", "jobs", "discover"}, RenderOrder: 111, render: careerDiscoveryPage},
		{ID: PageTalentWorkbench, Route: "/workspace/app/growth/talent-workbench", Label: "Talent workbench", Icon: "people", Title: "Manager talent workbench", Subtitle: "Work team talent through the governed growth service.", LabelKey: "page.talent_workbench.label", TitleKey: "page.talent_workbench.title", SubtitleKey: "page.talent_workbench.subtitle", SearchTerms: []string{"talent", "workbench", "team", "successors", "manager"}, RenderOrder: 112, render: talentWorkbenchPage},
		{ID: PageTalentCalibration, Route: "/workspace/app/growth/talent-calibration", Label: "Talent calibration", Icon: "people", Title: "Talent calibration", Subtitle: "Calibrate team talent through the governed growth service.", LabelKey: "page.talent_calibration.label", TitleKey: "page.talent_calibration.title", SubtitleKey: "page.talent_calibration.subtitle", SearchTerms: []string{"talent", "calibration", "grid", "session", "manager"}, RenderOrder: 113, render: talentCalibrationPage},
		{ID: PageSuccessionPlanning, Route: "/workspace/app/growth/succession", Label: "Succession planning", Icon: "people", Title: "Succession planning", Subtitle: "Plan succession through the governed growth service.", LabelKey: "page.succession_planning.label", TitleKey: "page.succession_planning.title", SubtitleKey: "page.succession_planning.subtitle", SearchTerms: []string{"succession", "successors", "bench", "slate", "manager"}, RenderOrder: 114, render: successionPlanningPage},
		{ID: PageOrgExplorer, Route: "/workspace/app/organization/explorer", Label: "Org explorer", Icon: "people", Title: "Organization explorer", Subtitle: "Browse the organization through the governed organization service.", LabelKey: "page.org_explorer.label", TitleKey: "page.org_explorer.title", SubtitleKey: "page.org_explorer.subtitle", SearchTerms: []string{"organization", "explorer", "browse", "units", "chart"}, RenderOrder: 115, render: orgExplorerPage},
		{ID: PageOrgOutline, Route: "/workspace/app/organization/outline", Label: "Org outline", Icon: "people", Title: "Accessible organization outline", Subtitle: "Outline the organization through the governed organization service.", LabelKey: "page.org_outline.label", TitleKey: "page.org_outline.title", SubtitleKey: "page.org_outline.subtitle", SearchTerms: []string{"outline", "structure", "hierarchy", "accessible", "organization"}, RenderOrder: 116, render: orgOutlinePage},
		{ID: PageOrgEffectiveDate, Route: "/workspace/app/organization/effective-date", Label: "Org effective date", Icon: "people", Title: "Effective-date organization navigation", Subtitle: "Navigate the organization as of a date through the governed organization service.", LabelKey: "page.org_effective_date.label", TitleKey: "page.org_effective_date.title", SubtitleKey: "page.org_effective_date.subtitle", SearchTerms: []string{"effective date", "history", "as of", "navigate", "organization"}, RenderOrder: 117, render: orgEffectiveDatePage},
		{ID: PagePositionObject, Route: "/workspace/app/organization/position", Label: "Position object", Icon: "people", Title: "Position object", Subtitle: "Inspect a governed position through the governed position service.", LabelKey: "page.position_object.label", TitleKey: "page.position_object.title", SubtitleKey: "page.position_object.subtitle", SearchTerms: []string{"position", "role", "object", "inspect", "organization"}, RenderOrder: 118, render: positionObjectPage},
		{ID: PagePositionOccupancy, Route: "/workspace/app/organization/position-occupancy", Label: "Position occupancy", Icon: "people", Title: "Position occupancy presentation", Subtitle: "Present position occupancy through the governed position service.", LabelKey: "page.position_occupancy.label", TitleKey: "page.position_occupancy.title", SubtitleKey: "page.position_occupancy.subtitle", SearchTerms: []string{"occupancy", "holder", "vacancy", "present", "position"}, RenderOrder: 119, render: positionOccupancyPage},
		{ID: PageHeadcountPlan, Route: "/workspace/app/organization/headcount-plan", Label: "Headcount plan", Icon: "people", Title: "Headcount-plan workspace", Subtitle: "Plan headcount through the governed headcount service.", LabelKey: "page.headcount_plan.label", TitleKey: "page.headcount_plan.title", SubtitleKey: "page.headcount_plan.subtitle", SearchTerms: []string{"headcount", "plan", "heads", "workspace", "organization"}, RenderOrder: 120, render: headcountPlanPage},
		{ID: PageWorkforceScenario, Route: "/workspace/app/organization/workforce-scenario", Label: "Workforce scenario", Icon: "people", Title: "Workforce scenario authoring", Subtitle: "Author workforce scenarios through the governed headcount service.", LabelKey: "page.workforce_scenario.label", TitleKey: "page.workforce_scenario.title", SubtitleKey: "page.workforce_scenario.subtitle", SearchTerms: []string{"scenario", "author", "workforce", "draft", "organization"}, RenderOrder: 121, render: workforceScenarioPage},
		{ID: PageGovernedPopulation, Route: "/workspace/app/organization/governed-population", Label: "Governed population", Icon: "people", Title: "Governed population building", Subtitle: "Build governed populations through the governed headcount service.", LabelKey: "page.governed_population.label", TitleKey: "page.governed_population.title", SubtitleKey: "page.governed_population.subtitle", SearchTerms: []string{"population", "build", "governed", "scope", "organization"}, RenderOrder: 122, render: governedPopulationPage},
		{ID: PageCostCapacity, Route: "/workspace/app/organization/cost-capacity", Label: "Cost and capacity", Icon: "people", Title: "Cost and capacity simulation", Subtitle: "Simulate cost and capacity through the governed headcount service.", LabelKey: "page.cost_capacity.label", TitleKey: "page.cost_capacity.title", SubtitleKey: "page.cost_capacity.subtitle", SearchTerms: []string{"cost", "capacity", "simulate", "budget", "organization"}, RenderOrder: 123, render: costCapacityPage},
		{ID: PageReorgProposals, Route: "/workspace/app/organization/reorg-proposals", Label: "Reorg proposals", Icon: "people", Title: "Reorganization proposals", Subtitle: "Propose reorganizations through the governed organization service.", LabelKey: "page.reorg_proposals.label", TitleKey: "page.reorg_proposals.title", SubtitleKey: "page.reorg_proposals.subtitle", SearchTerms: []string{"reorg", "propose", "restructure", "merge", "organization"}, RenderOrder: 124, render: reorgProposalsPage},
		{ID: PagePlannedVsCommitted, Route: "/workspace/app/organization/planned-vs-committed", Label: "Planned vs committed", Icon: "people", Title: "Planned versus committed", Subtitle: "Distinguish planned state from committed truth through the governed organization service.", LabelKey: "page.planned_committed.label", TitleKey: "page.planned_committed.title", SubtitleKey: "page.planned_committed.subtitle", SearchTerms: []string{"planned", "committed", "draft", "truth", "organization"}, RenderOrder: 125, render: plannedCommittedPage},
		{ID: PageOrgResponsive, Route: "/workspace/app/organization/responsive", Label: "Org responsive", Icon: "people", Title: "Responsive organization exploration", Subtitle: "Explore the organization responsively through the governed organization service.", LabelKey: "page.org_responsive.label", TitleKey: "page.org_responsive.title", SubtitleKey: "page.org_responsive.subtitle", SearchTerms: []string{"responsive", "viewport", "mobile", "explore", "organization"}, RenderOrder: 126, render: orgResponsivePage},
		{ID: PageHelpHub, Route: "/workspace/app/help/hub", Label: "Help hub", Icon: "help", Title: "Employee Help hub", Subtitle: "Reach employee help through the governed help service.", LabelKey: "page.help_hub.label", TitleKey: "page.help_hub.title", SubtitleKey: "page.help_hub.subtitle", SearchTerms: []string{"help", "hub", "support", "articles", "employee"}, RenderOrder: 127, render: helpHubPage},
		{ID: PageKnowledgeSearch, Route: "/workspace/app/help/knowledge-search", Label: "Knowledge search", Icon: "help", Title: "Authorized knowledge search", Subtitle: "Search help knowledge through the governed help service.", LabelKey: "page.knowledge_search.label", TitleKey: "page.knowledge_search.title", SubtitleKey: "page.knowledge_search.subtitle", SearchTerms: []string{"knowledge", "search", "articles", "authorized", "help"}, RenderOrder: 128, render: knowledgeSearchPage},
		{ID: PageHRServiceRequest, Route: "/workspace/app/help/hr-service-request", Label: "HR service request", Icon: "help", Title: "HR service-request intake", Subtitle: "Raise HR service requests through the governed help service.", LabelKey: "page.hr_service_request.label", TitleKey: "page.hr_service_request.title", SubtitleKey: "page.hr_service_request.subtitle", SearchTerms: []string{"HR", "service", "request", "intake", "help"}, RenderOrder: 129, render: hrServiceRequestPage},
		{ID: PageConfidentialCase, Route: "/workspace/app/help/confidential-case", Label: "Confidential case", Icon: "help", Title: "Confidential case intake", Subtitle: "Raise confidential cases through the governed help service.", LabelKey: "page.confidential_case.label", TitleKey: "page.confidential_case.title", SubtitleKey: "page.confidential_case.subtitle", SearchTerms: []string{"confidential", "sealed", "case", "intake", "help"}, RenderOrder: 130, render: confidentialCasePage},
		{ID: PageCaseStatus, Route: "/workspace/app/help/case-status", Label: "Case status", Icon: "help", Title: "Safe participant case status", Subtitle: "Check case status safely through the governed help service.", LabelKey: "page.case_status.label", TitleKey: "page.case_status.title", SubtitleKey: "page.case_status.subtitle", SearchTerms: []string{"case", "status", "participant", "safe", "help"}, RenderOrder: 131, render: caseStatusPage},
		{ID: PageCaseMessaging, Route: "/workspace/app/help/case-messaging", Label: "Case messaging", Icon: "help", Title: "Restricted case messaging", Subtitle: "Exchange case messages through the governed help service.", LabelKey: "page.case_messaging.label", TitleKey: "page.case_messaging.title", SubtitleKey: "page.case_messaging.subtitle", SearchTerms: []string{"case", "message", "restricted", "exchange", "help"}, RenderOrder: 132, render: caseMessagingPage},
		{ID: PageCaseCenter, Route: "/workspace/app/help/case-center", Label: "Case Center", Icon: "help", Title: "Specialist Case Center", Subtitle: "Work cases as a specialist through the governed help service.", LabelKey: "page.case_center.label", TitleKey: "page.case_center.title", SubtitleKey: "page.case_center.subtitle", SearchTerms: []string{"case", "center", "specialist", "queue", "help"}, RenderOrder: 133, render: caseCenterPage},
		{ID: PageCaseAssignment, Route: "/workspace/app/help/case-assignment", Label: "Case assignment", Icon: "help", Title: "Case assignment and recusal", Subtitle: "Assign cases and record recusal through the governed help service.", LabelKey: "page.case_assignment.label", TitleKey: "page.case_assignment.title", SubtitleKey: "page.case_assignment.subtitle", SearchTerms: []string{"case", "assign", "recusal", "workload", "help"}, RenderOrder: 134, render: caseAssignmentPage},
		{ID: PageCaseEvidence, Route: "/workspace/app/help/case-evidence", Label: "Case evidence", Icon: "help", Title: "Restricted case evidence review", Subtitle: "Review case evidence through the governed help service.", LabelKey: "page.case_evidence.label", TitleKey: "page.case_evidence.title", SubtitleKey: "page.case_evidence.subtitle", SearchTerms: []string{"case", "evidence", "restricted", "review", "help"}, RenderOrder: 135, render: caseEvidencePage},
		{ID: PageCaseDisposition, Route: "/workspace/app/help/case-disposition", Label: "Case disposition", Icon: "help", Title: "Case finding and disposition", Subtitle: "Record findings and disposition through the governed help service.", LabelKey: "page.case_disposition.label", TitleKey: "page.case_disposition.title", SubtitleKey: "page.case_disposition.subtitle", SearchTerms: []string{"case", "finding", "disposition", "record", "help"}, RenderOrder: 136, render: caseDispositionPage},
		{ID: PageCaseAppeal, Route: "/workspace/app/help/case-appeal", Label: "Case appeal", Icon: "help", Title: "Case appeal", Subtitle: "Appeal case findings through the governed help service.", LabelKey: "page.case_appeal.label", TitleKey: "page.case_appeal.title", SubtitleKey: "page.case_appeal.subtitle", SearchTerms: []string{"case", "appeal", "finding", "challenge", "help"}, RenderOrder: 137, render: caseAppealPage},
		{ID: PageCaseRedaction, Route: "/workspace/app/help/case-redaction", Label: "Case redaction", Icon: "help", Title: "Case-view redaction and audit", Subtitle: "Prove case-view redaction and audit through the governed help service.", LabelKey: "page.case_redaction.label", TitleKey: "page.case_redaction.title", SubtitleKey: "page.case_redaction.subtitle", SearchTerms: []string{"case", "redaction", "audit", "prove", "help"}, RenderOrder: 138, render: caseRedactionPage},
		{ID: PageExitInitiation, Route: "/workspace/app/lifecycle/exit-initiation", Label: "Exit initiation", Icon: "people", Title: "Exit initiation", Subtitle: "Start a worker exit through the governed lifecycle service.", LabelKey: "page.exit_initiation.label", TitleKey: "page.exit_initiation.title", SubtitleKey: "page.exit_initiation.subtitle", SearchTerms: []string{"exit", "initiate", "resign", "terminate", "lifecycle"}, RenderOrder: 139, render: exitInitiationPage},
		{ID: PageExitDetails, Route: "/workspace/app/lifecycle/exit-details", Label: "Exit details", Icon: "people", Title: "Exit reason and effective date", Subtitle: "Collect the exit reason and effective date through the governed lifecycle service.", LabelKey: "page.exit_details.label", TitleKey: "page.exit_details.title", SubtitleKey: "page.exit_details.subtitle", SearchTerms: []string{"exit", "reason", "effective date", "collect", "lifecycle"}, RenderOrder: 140, render: exitDetailsPage},
		{ID: PageOffboardingImpact, Route: "/workspace/app/lifecycle/offboarding-impact", Label: "Offboarding impact", Icon: "people", Title: "Offboarding impact simulation", Subtitle: "Simulate offboarding impact through the governed lifecycle service.", LabelKey: "page.offboarding_impact.label", TitleKey: "page.offboarding_impact.title", SubtitleKey: "page.offboarding_impact.subtitle", SearchTerms: []string{"offboarding", "impact", "simulate", "coverage", "lifecycle"}, RenderOrder: 141, render: offboardingImpactPage},
		{ID: PageExitReview, Route: "/workspace/app/lifecycle/exit-review", Label: "Exit review", Icon: "people", Title: "Exit review and approval", Subtitle: "Review and approve exits through the governed lifecycle service.", LabelKey: "page.exit_review.label", TitleKey: "page.exit_review.title", SubtitleKey: "page.exit_review.subtitle", SearchTerms: []string{"exit", "review", "approve", "authorize", "lifecycle"}, RenderOrder: 142, render: exitReviewPage},
		{ID: PageOffboardingPlan, Route: "/workspace/app/lifecycle/offboarding-plan", Label: "Offboarding plan", Icon: "people", Title: "Offboarding plan", Subtitle: "Plan a worker exit through the governed lifecycle service.", LabelKey: "page.offboarding_plan.label", TitleKey: "page.offboarding_plan.title", SubtitleKey: "page.offboarding_plan.subtitle", SearchTerms: []string{"offboarding", "plan", "handover", "tasks", "lifecycle"}, RenderOrder: 143, render: offboardingPlanPage},
		{ID: PageReassignmentReview, Route: "/workspace/app/lifecycle/reassignment-review", Label: "Reassignment review", Icon: "people", Title: "Manager and work reassignment review", Subtitle: "Review manager and work reassignment through the governed lifecycle service.", LabelKey: "page.reassignment_review.label", TitleKey: "page.reassignment_review.title", SubtitleKey: "page.reassignment_review.subtitle", SearchTerms: []string{"reassignment", "manager", "work", "review", "lifecycle"}, RenderOrder: 144, render: reassignmentReviewPage},
		{ID: PageFinalPay, Route: "/workspace/app/lifecycle/final-pay", Label: "Final pay", Icon: "people", Title: "Final-pay and benefit status", Subtitle: "Check final pay and benefit status through the governed lifecycle service.", LabelKey: "page.final_pay.label", TitleKey: "page.final_pay.title", SubtitleKey: "page.final_pay.subtitle", SearchTerms: []string{"final pay", "benefits", "status", "check", "lifecycle"}, RenderOrder: 145, render: finalPayPage},
		{ID: PageAccessEquipment, Route: "/workspace/app/lifecycle/access-equipment", Label: "Access and equipment", Icon: "people", Title: "Access and equipment reconciliation", Subtitle: "Reconcile access and equipment through the governed lifecycle service.", LabelKey: "page.access_equipment.label", TitleKey: "page.access_equipment.title", SubtitleKey: "page.access_equipment.subtitle", SearchTerms: []string{"access", "equipment", "reconcile", "badge", "lifecycle"}, RenderOrder: 146, render: accessEquipmentPage},
		{ID: PageFinalDocuments, Route: "/workspace/app/lifecycle/final-documents", Label: "Final documents", Icon: "people", Title: "Final-document delivery", Subtitle: "Receive final documents through the governed lifecycle service.", LabelKey: "page.final_documents.label", TitleKey: "page.final_documents.title", SubtitleKey: "page.final_documents.subtitle", SearchTerms: []string{"final", "documents", "delivery", "letter", "lifecycle"}, RenderOrder: 147, render: finalDocumentsPage},
		{ID: PageOffboardingEffects, Route: "/workspace/app/lifecycle/offboarding-effects", Label: "Offboarding effects", Icon: "people", Title: "Offboarding external-effect status", Subtitle: "Check offboarding external effects through the governed lifecycle service.", LabelKey: "page.offboarding_effects.label", TitleKey: "page.offboarding_effects.title", SubtitleKey: "page.offboarding_effects.subtitle", SearchTerms: []string{"offboarding", "effects", "external", "status", "lifecycle"}, RenderOrder: 148, render: offboardingEffectsPage},
		{ID: PageRetainedObligations, Route: "/workspace/app/lifecycle/retained-obligations", Label: "Retained obligations", Icon: "people", Title: "Retained-obligation presentation", Subtitle: "Review retained obligations through the governed lifecycle service.", LabelKey: "page.retained_obligations.label", TitleKey: "page.retained_obligations.title", SubtitleKey: "page.retained_obligations.subtitle", SearchTerms: []string{"retained", "obligations", "non-compete", "present", "lifecycle"}, RenderOrder: 149, render: retainedObligationsPage},
		{ID: PageExitCompletion, Route: "/workspace/app/lifecycle/exit-completion", Label: "Exit completion", Icon: "people", Title: "Exit completion and correction", Subtitle: "Complete and correct exits through the governed lifecycle service.", LabelKey: "page.exit_completion.label", TitleKey: "page.exit_completion.title", SubtitleKey: "page.exit_completion.subtitle", SearchTerms: []string{"exit", "complete", "correct", "close", "lifecycle"}, RenderOrder: 150, render: exitCompletionPage},
		{ID: PageReportCatalog, Route: "/workspace/app/reports/catalog", Label: "Report catalog", Icon: "insights", Title: "Report catalog", Subtitle: "Browse governed reports through the governed reporting service.", LabelKey: "page.report_catalog.label", TitleKey: "page.report_catalog.title", SubtitleKey: "page.report_catalog.subtitle", SearchTerms: []string{"reports", "catalog", "browse", "analytics", "intelligence"}, RenderOrder: 151, render: reportCatalogPage},
		{ID: PageReportTypes, Route: "/workspace/app/reports/types", Label: "Report types", Icon: "insights", Title: "Certified and customer reports", Subtitle: "Distinguish certified from customer reports through the governed reporting service.", LabelKey: "page.report_types.label", TitleKey: "page.report_types.title", SubtitleKey: "page.report_types.subtitle", SearchTerms: []string{"certified", "customer", "types", "distinguish", "reports"}, RenderOrder: 152, render: reportTypesPage},
		{ID: PageAnalysisFloorplan, Route: "/workspace/app/reports/analysis", Label: "Analysis floorplan", Icon: "insights", Title: "Analysis floorplan", Subtitle: "Lay out governed analysis through the governed reporting service.", LabelKey: "page.analysis_floorplan.label", TitleKey: "page.analysis_floorplan.title", SubtitleKey: "page.analysis_floorplan.subtitle", SearchTerms: []string{"analysis", "floorplan", "layout", "dashboard", "reports"}, RenderOrder: 153, render: analysisFloorplanPage},
		{ID: PageAnalysisFilters, Route: "/workspace/app/reports/analysis-filters", Label: "Analysis filters", Icon: "insights", Title: "Authorized analysis filters", Subtitle: "Filter governed analysis through the governed reporting service.", LabelKey: "page.analysis_filters.label", TitleKey: "page.analysis_filters.title", SubtitleKey: "page.analysis_filters.subtitle", SearchTerms: []string{"analysis", "filters", "authorized", "dimensions", "reports"}, RenderOrder: 154, render: analysisFiltersPage},
		{ID: PageResultLineage, Route: "/workspace/app/reports/result-lineage", Label: "Result lineage", Icon: "insights", Title: "Result lineage presentation", Subtitle: "Trace result lineage through the governed reporting service.", LabelKey: "page.result_lineage.label", TitleKey: "page.result_lineage.title", SubtitleKey: "page.result_lineage.subtitle", SearchTerms: []string{"lineage", "trace", "source", "present", "reports"}, RenderOrder: 155, render: resultLineagePage},
		{ID: PageDataFreshness, Route: "/workspace/app/reports/data-freshness", Label: "Data freshness", Icon: "insights", Title: "Data-freshness presentation", Subtitle: "Check data freshness through the governed reporting service.", LabelKey: "page.data_freshness.label", TitleKey: "page.data_freshness.title", SubtitleKey: "page.data_freshness.subtitle", SearchTerms: []string{"freshness", "stale", "lag", "present", "reports"}, RenderOrder: 156, render: dataFreshnessPage},
		{ID: PageAggregateSuppression, Route: "/workspace/app/reports/aggregate-suppression", Label: "Aggregate suppression", Icon: "insights", Title: "Aggregate suppression states", Subtitle: "Show suppression states through the governed reporting service.", LabelKey: "page.aggregate_suppression.label", TitleKey: "page.aggregate_suppression.title", SubtitleKey: "page.aggregate_suppression.subtitle", SearchTerms: []string{"suppression", "small cells", "privacy", "states", "reports"}, RenderOrder: 157, render: aggregateSuppressionPage},
		{ID: PageReportExport, Route: "/workspace/app/reports/export", Label: "Report export", Icon: "insights", Title: "Governed report export", Subtitle: "Export governed reports through the governed reporting service.", LabelKey: "page.report_export.label", TitleKey: "page.report_export.title", SubtitleKey: "page.report_export.subtitle", SearchTerms: []string{"export", "download", "CSV", "governed", "reports"}, RenderOrder: 158, render: reportExportPage},
		{ID: PageReportSharing, Route: "/workspace/app/reports/sharing", Label: "Report sharing", Icon: "insights", Title: "Authorized report sharing", Subtitle: "Share governed reports through the governed reporting service.", LabelKey: "page.report_sharing.label", TitleKey: "page.report_sharing.title", SubtitleKey: "page.report_sharing.subtitle", SearchTerms: []string{"share", "authorized", "link", "access", "reports"}, RenderOrder: 159, render: reportSharingPage},
		{ID: PageNLAnalysis, Route: "/workspace/app/reports/nl-analysis", Label: "NL analysis", Icon: "insights", Title: "Safe natural-language analysis", Subtitle: "Ask governed questions through the governed reporting service.", LabelKey: "page.nl_analysis.label", TitleKey: "page.nl_analysis.title", SubtitleKey: "page.nl_analysis.subtitle", SearchTerms: []string{"questions", "plain language", "ask", "safe", "reports"}, RenderOrder: 160, render: nlAnalysisPage},
		{ID: PageAnalysisHandoff, Route: "/workspace/app/reports/analysis-handoff", Label: "Analysis handoff", Icon: "insights", Title: "Analysis-to-proposal handoff", Subtitle: "Hand analysis to proposals through the governed reporting service.", LabelKey: "page.analysis_handoff.label", TitleKey: "page.analysis_handoff.title", SubtitleKey: "page.analysis_handoff.subtitle", SearchTerms: []string{"handoff", "proposal", "analysis", "transfer", "reports"}, RenderOrder: 161, render: analysisHandoffPage},
		{ID: PageAccessibleViz, Route: "/workspace/app/reports/accessible-viz", Label: "Accessible viz", Icon: "insights", Title: "Accessible data visualization", Subtitle: "Visualize governed data through the governed reporting service.", LabelKey: "page.accessible_viz.label", TitleKey: "page.accessible_viz.title", SubtitleKey: "page.accessible_viz.subtitle", SearchTerms: []string{"visualize", "charts", "accessible", "graphs", "reports"}, RenderOrder: 162, render: accessibleVizPage},
		{ID: PagePolicyStudio, Route: "/workspace/app/admin/policy-studio", Label: "Policy Studio", Icon: "admin", Title: "Policy Studio", Subtitle: "Author governance policy through the governed policy service.", ParentNav: PageAdmin, LabelKey: "page.policy_studio.label", TitleKey: "page.policy_studio.title", SubtitleKey: "page.policy_studio.subtitle", SearchTerms: []string{"policy", "studio", "author", "governance", "rules"}, RenderOrder: 163, render: policyStudioPage},
		{ID: PagePolicySimulation, Route: "/workspace/app/admin/policy-simulation", Label: "Policy simulation", Icon: "admin", Title: "Authorization-policy simulation", Subtitle: "Simulate authorization policy through the governed policy service.", ParentNav: PageAdmin, LabelKey: "page.policy_simulation.label", TitleKey: "page.policy_simulation.title", SubtitleKey: "page.policy_simulation.subtitle", SearchTerms: []string{"policy", "simulate", "authorization", "what-if", "rules"}, RenderOrder: 164, render: policySimulationPage},
		{ID: PageConfigurationCenter, Route: "/workspace/app/admin/configuration-center", Label: "Configuration center", Icon: "admin", Title: "Configuration center", Subtitle: "Center governed configuration through the governed configuration service.", ParentNav: PageAdmin, LabelKey: "page.configuration_center.label", TitleKey: "page.configuration_center.title", SubtitleKey: "page.configuration_center.subtitle", SearchTerms: []string{"configuration", "center", "settings", "governed", "admin"}, RenderOrder: 165, render: configurationCenterPage},
		{ID: PageIntegrationOperations, Route: "/workspace/app/admin/integration-operations", Label: "Integration operations", Icon: "admin", Title: "Integration operations", Subtitle: "Operate governed integrations through the governed integration service.", ParentNav: PageAdmin, LabelKey: "page.integration_operations.label", TitleKey: "page.integration_operations.title", SubtitleKey: "page.integration_operations.subtitle", SearchTerms: []string{"integration", "operations", "connectors", "sync", "admin"}, RenderOrder: 166, render: integrationOperationsPage},
		{ID: PageReconciliationWorkbench, Route: "/workspace/app/admin/reconciliation-workbench", Label: "Reconciliation workbench", Icon: "admin", Title: "Reconciliation and repair workbench", Subtitle: "Reconcile and repair through the governed repair service.", ParentNav: PageAdmin, LabelKey: "page.reconciliation_workbench.label", TitleKey: "page.reconciliation_workbench.title", SubtitleKey: "page.reconciliation_workbench.subtitle", SearchTerms: []string{"reconciliation", "repair", "workbench", "breaks", "admin"}, RenderOrder: 167, render: reconciliationWorkbenchPage},
		{ID: PagePrivacyTelemetry, Route: "/workspace/app/admin/privacy-telemetry", Label: "Privacy telemetry", Icon: "admin", Title: "Privacy-safe frontend telemetry", Subtitle: "Instrument privacy-safe telemetry through the governed telemetry service.", ParentNav: PageAdmin, LabelKey: "page.privacy_telemetry.label", TitleKey: "page.privacy_telemetry.title", SubtitleKey: "page.privacy_telemetry.subtitle", SearchTerms: []string{"telemetry", "privacy", "instrument", "events", "admin"}, RenderOrder: 168, render: privacyTelemetryPage},
		{ID: PagePerformanceBudgets, Route: "/workspace/app/admin/performance-budgets", Label: "Performance budgets", Icon: "admin", Title: "Frontend performance budgets", Subtitle: "Enforce frontend budgets through the governed performance service.", ParentNav: PageAdmin, LabelKey: "page.performance_budgets.label", TitleKey: "page.performance_budgets.title", SubtitleKey: "page.performance_budgets.subtitle", SearchTerms: []string{"performance", "budgets", "p95", "enforce", "admin"}, RenderOrder: 169, render: performanceBudgetsPage},
		{ID: PageBrowserMatrix, Route: "/workspace/app/admin/browser-matrix", Label: "Browser matrix", Icon: "admin", Title: "Production browser matrix", Subtitle: "Qualify the browser matrix through the governed qualification service.", ParentNav: PageAdmin, LabelKey: "page.browser_matrix.label", TitleKey: "page.browser_matrix.title", SubtitleKey: "page.browser_matrix.subtitle", SearchTerms: []string{"browser", "matrix", "qualify", "support", "admin"}, RenderOrder: 170, render: browserMatrixPage},
		{ID: PageAssistiveTech, Route: "/workspace/app/admin/assistive-tech", Label: "Assistive tech", Icon: "admin", Title: "Assistive-technology compatibility", Subtitle: "Qualify compatibility through the governed qualification service.", ParentNav: PageAdmin, LabelKey: "page.assistive_tech.label", TitleKey: "page.assistive_tech.title", SubtitleKey: "page.assistive_tech.subtitle", SearchTerms: []string{"assistive", "screen reader", "qualify", "a11y", "admin"}, RenderOrder: 171, render: assistiveTechPage},
		{ID: PageDisasterRecovery, Route: "/workspace/app/admin/disaster-recovery", Label: "Disaster recovery", Icon: "admin", Title: "Frontend disaster recovery", Subtitle: "Prove recovery through the governed recovery service.", ParentNav: PageAdmin, LabelKey: "page.disaster_recovery.label", TitleKey: "page.disaster_recovery.title", SubtitleKey: "page.disaster_recovery.subtitle", SearchTerms: []string{"disaster", "recovery", "failover", "prove", "admin"}, RenderOrder: 172, render: disasterRecoveryPage},
		{ID: PageReleaseGate, Route: "/workspace/app/admin/release-gate", Label: "Release gate", Icon: "admin", Title: "Production frontend release gate", Subtitle: "Gate the release through the governed release service.", ParentNav: PageAdmin, LabelKey: "page.release_gate.label", TitleKey: "page.release_gate.title", SubtitleKey: "page.release_gate.subtitle", SearchTerms: []string{"release", "gate", "production", "approve", "admin"}, RenderOrder: 173, render: releaseGatePage},
	}
}

// PageDefinitions returns a copy so callers can inspect the page inventory
// without mutating the application registry.
func PageDefinitions() []PageDefinition {
	return registeredPages()
}

// LookupPage resolves the canonical definition for a stable page identity.
func LookupPage(id PageID) (PageDefinition, bool) {
	for _, definition := range registeredPages() {
		if definition.ID == id {
			return definition, true
		}
	}
	return PageDefinition{}, false
}

// LookupRoute keeps transport routing coupled to the canonical page registry,
// not to assumptions about PageID spelling.
func LookupRoute(route string) (PageDefinition, bool) {
	for _, definition := range registeredPages() {
		if definition.Route == route {
			return definition, true
		}
	}
	return PageDefinition{}, false
}

func renderPage(view View) (ui.Node, error) {
	definition, ok := LookupPage(view.Page)
	if !ok || definition.render == nil {
		return nil, fmt.Errorf("productui: unknown page %q", view.Page)
	}
	return definition.render(view), nil
}

func defaultNavigation(locale LocaleContext) []NavItem {
	return navigationFor(locale, nil)
}

func navigationForRoles(locale LocaleContext, roles []string) []NavItem {
	restricted := roles != nil
	return navigationFor(locale, func(page PageID) bool { return !restricted || PageVisible(page, roles) })
}

func navigationForPermissions(locale LocaleContext, permissions []RolePagePermission) []NavItem {
	allowed := make(map[PageID]bool, len(permissions))
	for _, permission := range permissions {
		allowed[permission.Page] = allowed[permission.Page] || permission.View
	}
	return navigationFor(locale, func(page PageID) bool { return allowed[page] })
}

// authorizedNavigationForRoles preserves the established role adapter while
// marking its result as an authoritative projection. Production adapters can
// replace this compatibility constructor with a resolver-owned projection;
// presentation never derives access after this boundary.
func authorizedNavigationForRoles(locale LocaleContext, roles []string) AuthorizedNavigationProjection {
	return authorizedNavigationProjection(locale, navigationForRoles(locale, roles), func(page PageID) bool { return PageVisible(page, roles) })
}

func authorizedNavigationForPermissions(locale LocaleContext, permissions []RolePagePermission) AuthorizedNavigationProjection {
	allowed := make(map[PageID]bool, len(permissions))
	for _, permission := range permissions {
		allowed[permission.Page] = allowed[permission.Page] || permission.View
	}
	return authorizedNavigationProjection(locale, navigationForPermissions(locale, permissions), func(page PageID) bool { return allowed[page] })
}

func authorizedNavigationProjection(locale LocaleContext, items []NavItem, supportAllowed func(PageID) bool) AuthorizedNavigationProjection {
	// Version one is the compatibility adapter's schema version. Production
	// resolvers supply their own positive version; zero is never an admitted
	// answer because it cannot be distinguished from an unversioned payload.
	projection := AuthorizedNavigationProjection{Version: 1}
	for _, item := range items {
		projection.Items = append(projection.Items, authorizedNavigationItem(item))
	}
	for _, page := range []PageID{PageHelp, PageSettings} {
		item, ok := navigationItemForPage(page, locale)
		if ok && supportAllowed(page) {
			projection.Support = append(projection.Support, authorizedNavigationItem(item))
		}
	}
	return projection
}

func authorizedNavigationItem(item NavItem) AuthorizedNavigationItem {
	result := AuthorizedNavigationItem{
		Page: item.Page, Label: item.Label, LabelKey: item.LabelKey, Description: item.Description,
		Keywords: append([]string(nil), item.Keywords...), Icon: item.Icon, Href: pageHref(item.Page), Count: item.Count, Authorized: true,
	}
	for _, child := range item.Children {
		result.Children = append(result.Children, authorizedNavigationItem(child))
	}
	return result
}

const (
	maxAuthorizedNavigationItems    = 64
	maxAuthorizedNavigationDepth    = 2
	maxAuthorizedNavigationText     = 256
	maxAuthorizedNavigationKeywords = 24
)

// ApplyNavigationProjection adopts a complete server answer. Validation is a
// safety boundary only; it does not grant access. Invalid answers remain
// authoritative and render no destinations, rather than reintroducing the
// registry's default catalogue.
func ApplyNavigationProjection(view View, projection AuthorizedNavigationProjection) View {
	// Validate the caller-owned graph before allocating or recursively copying
	// any of it. The validator charges each node before descending and rejects
	// excess depth, fan-out, and total size, so hostile graphs cannot turn this
	// display boundary into unbounded stack or heap work.
	if err := validateAuthorizedNavigationProjection(projection); err != nil {
		projection = AuthorizedNavigationProjection{Version: projection.Version}
	} else {
		projection = cloneAuthorizedNavigationProjection(projection, view.Locale)
	}
	view.NavigationProjection = &projection
	view.Navigation = navigationItemsFromProjection(projection.Items, view.Locale)
	view.NavigationSupport = navigationItemsFromProjection(projection.Support, view.Locale)
	return view
}

func cloneAuthorizedNavigationProjection(projection AuthorizedNavigationProjection, locale LocaleContext) AuthorizedNavigationProjection {
	copy := AuthorizedNavigationProjection{Version: projection.Version}
	copy.Items = make([]AuthorizedNavigationItem, 0, len(projection.Items))
	for _, item := range projection.Items {
		copy.Items = append(copy.Items, cloneAuthorizedNavigationItem(item, locale))
	}
	copy.Support = make([]AuthorizedNavigationItem, 0, len(projection.Support))
	for _, item := range projection.Support {
		copy.Support = append(copy.Support, cloneAuthorizedNavigationItem(item, locale))
	}
	return copy
}

func cloneAuthorizedNavigationItem(item AuthorizedNavigationItem, locale LocaleContext) AuthorizedNavigationItem {
	definition, _ := LookupPage(item.Page)
	copy := AuthorizedNavigationItem{
		Page: item.Page, Label: locale.Text(item.LabelKey), LabelKey: item.LabelKey,
		Description: locale.Text(definition.SubtitleKey), Keywords: append([]string(nil), definition.SearchTerms...),
		Icon: definition.Icon, Href: definition.Route, Count: item.Count, Authorized: true,
	}
	copy.Children = make([]AuthorizedNavigationItem, 0, len(item.Children))
	for _, child := range item.Children {
		copy.Children = append(copy.Children, cloneAuthorizedNavigationItem(child, locale))
	}
	return copy
}

func navigationItemsFromProjection(items []AuthorizedNavigationItem, locale LocaleContext) []NavItem {
	result := make([]NavItem, 0, len(items))
	for _, item := range items {
		result = append(result, navigationItemFromProjection(item, locale))
	}
	return result
}

func navigationItemFromProjection(item AuthorizedNavigationItem, locale LocaleContext) NavItem {
	definition, _ := LookupPage(item.Page)
	result := NavItem{
		Page: item.Page, Label: locale.Text(item.LabelKey), LabelKey: item.LabelKey,
		Description: locale.Text(definition.SubtitleKey), Keywords: append([]string(nil), definition.SearchTerms...),
		Icon: definition.Icon, Href: definition.Route, Count: item.Count,
	}
	for _, child := range item.Children {
		result.Children = append(result.Children, navigationItemFromProjection(child, locale))
	}
	return result
}

func validateAuthorizedNavigationProjection(projection AuthorizedNavigationProjection) error {
	if projection.Version <= 0 {
		return fmt.Errorf("productui: navigation projection has no positive version")
	}
	if len(projection.Items) > maxAuthorizedNavigationItems || len(projection.Support) > maxAuthorizedNavigationItems-len(projection.Items) {
		return fmt.Errorf("productui: navigation projection exceeds item limit")
	}
	seen := make(map[PageID]bool)
	overviews := make(map[PageID]bool)
	definitions := make(map[PageID]PageDefinition)
	for _, definition := range registeredPages() {
		definitions[definition.ID] = definition
	}
	count := 0
	for _, item := range projection.Items {
		if err := validateAuthorizedNavigationItem(item, true, 0, "", seen, overviews, &count, definitions); err != nil {
			return err
		}
	}
	for _, item := range projection.Support {
		if err := validateAuthorizedNavigationItem(item, false, 0, "", seen, overviews, &count, definitions); err != nil {
			return err
		}
	}
	return nil
}

func validateAuthorizedNavigationItem(item AuthorizedNavigationItem, primary bool, depth int, parent PageID, seen, overviews map[PageID]bool, count *int, definitions map[PageID]PageDefinition) error {
	if depth > maxAuthorizedNavigationDepth || *count >= maxAuthorizedNavigationItems {
		return fmt.Errorf("productui: navigation projection exceeds structural limit")
	}
	(*count)++
	if len(item.Children) > maxAuthorizedNavigationItems-*count {
		return fmt.Errorf("productui: navigation projection exceeds item limit")
	}
	definition, ok := definitions[item.Page]
	if !ok || !definition.Admitted || !item.Authorized || item.Count < 0 || len(item.Keywords) > maxAuthorizedNavigationKeywords {
		return fmt.Errorf("productui: malformed navigation projection item")
	}
	if !validNavigationText(item.Label, true) || !validNavigationText(item.LabelKey, true) || !validNavigationText(item.Icon, true) ||
		!validNavigationText(item.Description, false) {
		return fmt.Errorf("productui: unsafe navigation projection text")
	}
	if item.Icon != definition.Icon || !validNavigationHref(definition, item.Href) {
		return fmt.Errorf("productui: malformed navigation projection route")
	}
	for _, keyword := range item.Keywords {
		if !validNavigationText(keyword, true) {
			return fmt.Errorf("productui: malformed navigation projection keyword")
		}
	}
	if seen[item.Page] {
		// Only a direct, leaf overview may repeat its containing group's page,
		// and it may do so once. A repeated leaf elsewhere cannot smuggle an
		// unrelated route through duplicate handling or evade the item budget.
		if depth != 1 || parent == "" || item.Page != parent || len(item.Children) > 0 || overviews[item.Page] || item.LabelKey != navigationOverviewLabelKey(item.Page) {
			return fmt.Errorf("productui: duplicate navigation projection page %q", item.Page)
		}
		overviews[item.Page] = true
		return nil
	}
	if item.LabelKey != definition.LabelKey {
		return fmt.Errorf("productui: navigation projection label key is not canonical")
	}
	if parent != "" {
		if definition.ParentNav != parent {
			return fmt.Errorf("productui: navigation projection child has wrong parent")
		}
	} else if primary {
		if !definition.PrimaryNav {
			return fmt.Errorf("productui: navigation projection page is not primary navigation")
		}
	} else if item.Page != PageHelp && item.Page != PageSettings || len(item.Children) > 0 {
		return fmt.Errorf("productui: navigation projection page is not in its allowed region")
	}
	seen[item.Page] = true
	for _, child := range item.Children {
		if err := validateAuthorizedNavigationItem(child, false, depth+1, item.Page, seen, overviews, count, definitions); err != nil {
			return err
		}
	}
	return nil
}

func validNavigationHref(definition PageDefinition, href string) bool {
	if href == "" || strings.TrimSpace(href) != href || len(href) > maxAuthorizedNavigationText || !validNavigationText(href, true) {
		return false
	}
	parsed, err := url.Parse(href)
	return err == nil && !parsed.IsAbs() && parsed.Opaque == "" && parsed.Scheme == "" && parsed.User == nil && parsed.Host == "" &&
		parsed.Path == definition.Route && parsed.RawPath == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func validNavigationText(value string, required bool) bool {
	if value == "" {
		return !required
	}
	if len(value) > maxAuthorizedNavigationText || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

func navigationOverviewLabelKey(page PageID) string {
	switch page {
	case PageWork:
		return "nav.work_queue"
	case PageAdmin:
		return "nav.admin_overview"
	default:
		return "nav.overview"
	}
}

// navigationPrimaryEligible reports whether definition may appear as a
// top-level primary navigation destination. PrimaryNav, admission, and
// per-role visibility all gate independently and the zero value of
// Admitted is false, so a page the registry has not explicitly admitted
// never reaches navigation, even if it declares PrimaryNav.
func navigationPrimaryEligible(definition PageDefinition, visible func(PageID) bool) bool {
	return definition.PrimaryNav && definition.Admitted && (visible == nil || visible(definition.ID))
}

// navigationChildEligible is navigationPrimaryEligible's counterpart for a
// page nested under a ParentNav group. The same admission gate applies: a
// ParentNav wire alone (the mechanism Experience Studio and every other
// unpublished fallback surface already carries) is never sufficient on its
// own to claim a menu slot.
func navigationChildEligible(definition PageDefinition, visible func(PageID) bool) bool {
	return definition.ParentNav != "" && definition.Admitted && (visible == nil || visible(definition.ID))
}

func navigationFor(locale LocaleContext, visible func(PageID) bool) []NavItem {
	pages := registeredPages()
	items := make([]NavItem, 0, len(pages))
	indexes := make(map[PageID]int)
	for _, definition := range pages {
		if !navigationPrimaryEligible(definition, visible) {
			continue
		}
		item := navigationItemFromDefinition(definition, locale)
		items = append(items, item)
		indexes[definition.ID] = len(items) - 1
	}
	for _, definition := range pages {
		if !navigationChildEligible(definition, visible) {
			continue
		}
		index, ok := indexes[definition.ParentNav]
		if !ok {
			continue
		}
		parent := &items[index]
		if len(parent.Children) == 0 {
			label := locale.Text("nav.overview")
			switch parent.Page {
			case PageWork:
				label = locale.Text("nav.work_queue")
			case PageAdmin:
				label = locale.Text("nav.admin_overview")
			}
			labelKey := navigationOverviewLabelKey(parent.Page)
			overview := NavItem{Page: parent.Page, Label: label, LabelKey: labelKey, Description: parent.Description, Keywords: append([]string(nil), parent.Keywords...), Icon: parent.Icon}
			parent.Children = append(parent.Children, overview)
		}
		parent.Children = append(parent.Children, navigationItemFromDefinition(definition, locale))
	}
	return items
}

func navigationItemFromDefinition(definition PageDefinition, locale LocaleContext) NavItem {
	return NavItem{
		Page: definition.ID, Label: locale.Text(definition.LabelKey), LabelKey: definition.LabelKey,
		Description: locale.Text(definition.SubtitleKey), Keywords: append([]string(nil), definition.SearchTerms...), Icon: definition.Icon,
	}
}

func pageHref(page PageID) string {
	if definition, ok := LookupPage(page); ok {
		return definition.Route
	}
	return "/workspace/app/home"
}

// Path returns the canonical history-router path for a product page.
func Path(page PageID) string { return pageHref(page) }

// JourneyDetailHref returns a software-routed link to one journey while
// retaining the reader's navigation preferences.
func JourneyDetailHref(view View, intentID string) string {
	return statefulHref(view, PageJourneys, "journey", intentID)
}

// JourneyProposalHref returns a software-routed link to the focused workflow
// launcher for workerRef while retaining navigation preferences.
func JourneyProposalHref(view View, workerRef string) string {
	return statefulHref(view, PageJourneys, "mode", "new", "worker", workerRef)
}

func statefulHref(view View, page PageID, keyValues ...string) string {
	values := url.Values{}
	if view.NavCollapsed {
		values.Set("nav", "collapsed")
	}
	setMenuAddressState(values, view)
	for index := 0; index+1 < len(keyValues); index += 2 {
		if keyValues[index+1] != "" {
			values.Set(keyValues[index], keyValues[index+1])
		}
	}
	href := pageHref(page)
	if query := values.Encode(); query != "" {
		return href + "?" + query
	}
	return href
}

// withExplicitEmptyQuery distinguishes a deliberate clear from an absent
// address value. Absence asks the server-backed preference layer for its
// stored default; an encoded empty value replaces that stored default.
func withExplicitEmptyQuery(href string, names ...string) string {
	return withExplicitQueryValue(href, names, "")
}

func withExplicitQueryValue(href string, names []string, value string) string {
	parsed, err := url.Parse(href)
	if err != nil {
		return href
	}
	values := parsed.Query()
	for _, name := range names {
		values.Set(name, value)
	}
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func setMenuAddressState(values url.Values, view View) {
	if locale := view.Locale.normalized(); locale.Resolved != DefaultProductLocale || locale.Requested != "" {
		values.Set("locale", locale.Resolved)
	}
	if view.MenuQuery != "" {
		values.Set("menu_q", view.MenuQuery)
	}
	if favorites := authorizedFavoritePages(view.Navigation, view.FavoritePages); len(favorites) > 0 {
		pages := make([]string, 0, len(favorites))
		for _, page := range favorites {
			pages = append(pages, string(page))
		}
		values.Set("favorites", strings.Join(pages, ","))
	}
}
