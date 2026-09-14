package productui

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PageID identifies one stable product surface. The server resolves whether a
// page is discoverable before it includes the page in View.Navigation.
type PageID string

const (
	PageHome     PageID = "home"
	PageMyself   PageID = "myself"
	PageJourneys PageID = "journeys"
	PageWork     PageID = "work"
	// PageJourneyDiagnostics is not a navigable route: it names PROMOUX-008's
	// authorized diagnostics disclosure (raw identifiers such as a journey's
	// work-item id) within the Journeys and My Work pages. It shares its
	// wire id with roleaccess.PageJourneyDiagnostics so a single role grant
	// governs both surfaces.
	PageJourneyDiagnostics      PageID = "journey-diagnostics"
	PageHistory                 PageID = "history"
	PagePeople                  PageID = "people"
	PagePerson                  PageID = "person"
	PageHeadcount               PageID = "headcount"
	PagePosition                PageID = "position"
	PageRequisition             PageID = "requisition"
	PageCandidates              PageID = "candidates"
	PageCandidate               PageID = "candidate"
	PageInterviews              PageID = "interviews"
	PageEvaluation              PageID = "evaluation"
	PageOffer                   PageID = "offer"
	PagePortal                  PageID = "portal"
	PageOnboarding              PageID = "onboarding"
	PageOnboardingTasks         PageID = "onboarding-tasks"
	PageActivationReadiness     PageID = "activation-readiness"
	PageTimeHub                 PageID = "time-hub"
	PageTimeEntry               PageID = "time-entry"
	PageTimeCorrection          PageID = "time-correction"
	PageTimeApproval            PageID = "time-approval"
	PageTimeExceptions          PageID = "time-exceptions"
	PageTimeOff                 PageID = "time-off"
	PageTimeOffRequest          PageID = "time-off-request"
	PageTeamCoverage            PageID = "team-coverage"
	PageProtectedLeave          PageID = "protected-leave"
	PageLeaveEvidence           PageID = "leave-evidence"
	PageLeaveTimeline           PageID = "leave-timeline"
	PageReturnToWork            PageID = "return-to-work"
	PagePaySummary              PageID = "pay-summary"
	PagePayStatements           PageID = "pay-statements"
	PagePayDiscrepancy          PageID = "pay-discrepancy"
	PageCompProposals           PageID = "comp-proposals"
	PageSalaryComparison        PageID = "salary-comparison"
	PageCyclePopulations        PageID = "cycle-populations"
	PageCompWorksheet           PageID = "comp-worksheet"
	PageCompCalibration         PageID = "comp-calibration"
	PageBenefitsOverview        PageID = "benefits-overview"
	PageBenefitsCompare         PageID = "benefits-compare"
	PageBenefitsEnroll          PageID = "benefits-enroll"
	PagePayBenefitRecon         PageID = "pay-benefit-recon"
	PageGrowthHome              PageID = "growth-home"
	PageGoalPlanning            PageID = "goal-planning"
	PageGovernedFeedback        PageID = "governed-feedback"
	PageManagerCheckins         PageID = "manager-checkins"
	PagePerfReview              PageID = "perf-review"
	PageReviewParticipants      PageID = "review-participants"
	PageSkillsProfile           PageID = "skills-profile"
	PageAssignedLearning        PageID = "assigned-learning"
	PageCareerDiscovery         PageID = "career-discovery"
	PageTalentWorkbench         PageID = "talent-workbench"
	PageTalentCalibration       PageID = "talent-calibration"
	PageSuccessionPlanning      PageID = "succession-planning"
	PageOrgExplorer             PageID = "org-explorer"
	PageOrgOutline              PageID = "org-outline"
	PageOrgEffectiveDate        PageID = "org-effective-date"
	PagePositionObject          PageID = "position-object"
	PagePositionOccupancy       PageID = "position-occupancy"
	PageHeadcountPlan           PageID = "headcount-plan"
	PageWorkforceScenario       PageID = "workforce-scenario"
	PageGovernedPopulation      PageID = "governed-population"
	PageCostCapacity            PageID = "cost-capacity"
	PageReorgProposals          PageID = "reorg-proposals"
	PagePlannedVsCommitted      PageID = "planned-vs-committed"
	PageOrgResponsive           PageID = "org-responsive"
	PageHelpHub                 PageID = "help-hub"
	PageKnowledgeSearch         PageID = "knowledge-search"
	PageHRServiceRequest        PageID = "hr-service-request"
	PageConfidentialCase        PageID = "confidential-case"
	PageCaseStatus              PageID = "case-status"
	PageCaseMessaging           PageID = "case-messaging"
	PageCaseCenter              PageID = "case-center"
	PageCaseAssignment          PageID = "case-assignment"
	PageCaseEvidence            PageID = "case-evidence"
	PageCaseDisposition         PageID = "case-disposition"
	PageCaseAppeal              PageID = "case-appeal"
	PageCaseRedaction           PageID = "case-redaction"
	PageExitInitiation          PageID = "exit-initiation"
	PageExitDetails             PageID = "exit-details"
	PageOffboardingImpact       PageID = "offboarding-impact"
	PageExitReview              PageID = "exit-review"
	PageOffboardingPlan         PageID = "offboarding-plan"
	PageReassignmentReview      PageID = "reassignment-review"
	PageFinalPay                PageID = "final-pay"
	PageAccessEquipment         PageID = "access-equipment"
	PageFinalDocuments          PageID = "final-documents"
	PageOffboardingEffects      PageID = "offboarding-effects"
	PageRetainedObligations     PageID = "retained-obligations"
	PageExitCompletion          PageID = "exit-completion"
	PageReportCatalog           PageID = "report-catalog"
	PageReportTypes             PageID = "report-types"
	PageAnalysisFloorplan       PageID = "analysis-floorplan"
	PageAnalysisFilters         PageID = "analysis-filters"
	PageResultLineage           PageID = "result-lineage"
	PageDataFreshness           PageID = "data-freshness"
	PageAggregateSuppression    PageID = "aggregate-suppression"
	PageReportExport            PageID = "report-export"
	PageReportSharing           PageID = "report-sharing"
	PageNLAnalysis              PageID = "nl-analysis"
	PageAnalysisHandoff         PageID = "analysis-handoff"
	PageAccessibleViz           PageID = "accessible-viz"
	PagePolicyStudio            PageID = "policy-studio"
	PagePolicySimulation        PageID = "policy-simulation"
	PageConfigurationCenter     PageID = "configuration-center"
	PageIntegrationOperations   PageID = "integration-operations"
	PageReconciliationWorkbench PageID = "reconciliation-workbench"
	PagePrivacyTelemetry        PageID = "privacy-telemetry"
	PagePerformanceBudgets      PageID = "performance-budgets"
	PageBrowserMatrix           PageID = "browser-matrix"
	PageAssistiveTech           PageID = "assistive-tech"
	PageDisasterRecovery        PageID = "disaster-recovery"
	PageReleaseGate             PageID = "release-gate"
	PageOrganization            PageID = "organization"
	PageInsights                PageID = "insights"
	PageAdmin                   PageID = "admin"
	PageWorkerIDs               PageID = "worker-ids"
	PageRoles                   PageID = "roles"
	PageOrganizationVisibility  PageID = "organization-visibility"
	PageAppearance              PageID = "appearance"
	PageStudio                  PageID = "studio"
	PageHelp                    PageID = "help"
	PageSettings                PageID = "settings"
)

type NavItem struct {
	Page        PageID
	Label       string
	LabelKey    string
	Description string
	Keywords    []string
	Icon        string
	// Href is supplied by an authorized navigation projection. Registry-built
	// preview items leave it empty and use the canonical route instead.
	Href     string
	Count    int
	Children []NavItem
}

// AuthorizedNavigationItem is the bounded, display-safe record emitted by a
// server-side navigation resolver. Authorized is deliberately explicit: a
// malformed or denied record is never treated as a hint to consult roles,
// URLs, preferences, or the page registry.
type AuthorizedNavigationItem struct {
	Page        PageID
	Label       string
	LabelKey    string
	Description string
	Keywords    []string
	Icon        string
	Href        string
	Count       int
	Authorized  bool
	Children    []AuthorizedNavigationItem
}

// AuthorizedNavigationProjection is the complete navigation answer for one
// admitted presentation context. A non-nil projection is authoritative even
// when it is empty or invalid; callers must never fall back to default nav.
// Support contains safe secondary destinations such as Help and Settings.
type AuthorizedNavigationProjection struct {
	Version int64
	Items   []AuthorizedNavigationItem
	Support []AuthorizedNavigationItem
}

type WorkItem struct {
	ID       string
	Initials string
	PhotoURL string
	Title    string
	// TitleKey and StatusKey are semantic presentation keys supplied by an
	// authorized adapter. They let locale changes re-project labels without
	// treating English service copy as a translation identifier.
	TitleKey  string
	Person    string
	PersonRef string
	// AssigneeRef is the server-selected principal who currently owes the
	// human decision. PersonRef remains the subject of the workflow.
	AssigneeRef     string
	Summary         string
	Status          string
	StatusKey       string
	Due             string
	Tone            string
	Href            string
	EffectiveDate   string
	CompletedAt     string
	InstanceID      string
	InstanceVersion int64
	MaterialDigest  string
	CurrentBase     values.Money
	ProposedBase    values.Money
	Terminal        bool
	// NextStep and WaitingOn are the stable codes of the single next step and
	// the role class a journey's server stage names (UXAUDIT-017; see
	// tools/uxqual/journeyclient.StageStatusDimension). They are workflow
	// facts, never action authority, and render as text only. AwaitsPerson
	// is true when a person rather than the workflow holds that step; the
	// queue ranks those first. Empty codes render nothing.
	NextStep     string
	WaitingOn    string
	AwaitsPerson bool
	// WorkSummary is true when the server disclosed the journey's current
	// work item to this viewer (UXAUDIT-017). The fields below are set only
	// then, and only as far as the server's work item rules disclosed them:
	// AssigneeRef/AssigneeName for a directly routed item whose context the
	// viewer may see, WorkDue (YYYY-MM-DD) for the item's real deadline,
	// ViewerMembership (NONE, CANDIDATE, ASSIGNEE, CLAIMANT) and the viewer's
	// PermittedActions tokens.
	WorkSummary bool
	// ViewerRelationships (INITIATOR, ASSIGNEE, CANDIDATE) and
	// ViewerResponsibility (ACTION_REQUIRED, TRACKING, OBSERVING, CLOSED) are
	// the server's PROMOUX-012 viewer projection. Empty responsibility means
	// the server resolved none, which is never actionable. My Work, tracked
	// requests, the attention counts and the person profile all read these,
	// never re-derive them from Status.
	ViewerRelationships  []string
	ViewerResponsibility string
	AssigneeName         string
	WorkDue              string
	ViewerMembership     string
	PermittedActions     []string
	// StatusProjection is supplied by the authorized service adapter when
	// available. The page never treats it as action authority.
	StatusProjection StatusProjection
	// Provenance is supplied by the authorized service adapter when available.
	// It is presentation evidence only and never grants action authority.
	Provenance ProvenanceProjection
	// Disposition is PROMOUX-003's approval verdict for this item, adapted
	// from internal/humanwork/workitem.ApprovalDisposition
	// (ApprovalDispositionProjectionFrom). Nil means no disposition was
	// computed for this item -- not an approval work item, or an adapter
	// that has not wired one yet -- and the render path shows nothing for
	// it rather than a default, misleading availability.
	Disposition *ApprovalDispositionProjection
}

type Person struct {
	ID            string
	WorkerID      string
	Initials      string
	PhotoURL      string
	Name          string
	LegalName     string
	PreferredName string
	Role          string
	Team          string
	Manager       string
	// ManagerRelationship and ManagerWorkerRef are the service-authorized
	// reporting projection. ManagerWorkerRef is populated only when the
	// manager is another admitted Person; hierarchy code never uses Manager
	// display text as identity.
	ManagerRelationship OrganizationRelationshipState
	ManagerWorkerRef    string
	Location            string
	WorkerNumber        string
	// PromotionAvailability is the server's four-state promotion-workflow
	// verdict for this worker (see ResolvePromotionAvailability). The zero
	// value means no verdict was recorded and fails closed: no launchable
	// action, and a generic non-revealing reason. The real read path
	// (tools/uxqual/productclient) always sets an explicit code, and a
	// fixture that wants the eligible rendering must say so, so that a
	// caller which forgets this field can never silently offer a promotion
	// the server never authorized.
	PromotionAvailability PromotionAvailabilityCode
	JobCode               string
	Grade                 string
	PositionID            string
	PayZone               string
	BasePay               values.Money
	BonusTarget           string
	HireDate              string
	Source                string
	CreatedAt             string
	// ManagerID is the manager's own WorkerID (the raw canonical worker
	// identity workforce.WorkerRow.WorkerID carries, distinct from Manager --
	// a display name -- and from ID/WorkerRef, the public routing reference).
	// UXAUDIT-004: this, not the Manager display name, is what
	// org.ResolveManagerRelationships resolves against, so reporting-line
	// nesting agrees with the authorized manager edge rather than a name
	// match that a shared or ambiguous name silently breaks. Empty means no
	// manager relationship is on record for this worker.
	ManagerID string
	// ManagerRelationshipWithheld is set when the manager relationship exists
	// but this viewer's authorization does not disclose it. UXAUDIT-004 wires
	// this end to end (org.ResolveManagerRelationships reports the withheld
	// hop as an explained root, never a fabricated placement); no upstream
	// production adapter sets it true yet, which is called out as a boundary
	// rather than claimed as covered.
	ManagerRelationshipWithheld bool
	// normalized is an immutable client-side search/sort index populated once
	// when a workforce projection arrives. Keeping it beside the projection
	// avoids allocating lower-cased copies for every filter and sort render.
	normalized personNormalizedIndex
}

type personNormalizedIndex struct {
	ready                    bool
	search, name, role, team string
	manager, location        string
}

// ViewerProfile is the presentation identity associated with the admitted
// application principal. Principal remains the security identity; Viewer is
// the authorized worker-facing profile used for account UI.
type ViewerProfile struct {
	// PersonID is the authorized worker projection bound to the admitted
	// principal. It is presentation identity, not action authority.
	PersonID string
	Name     string
	Initials string
	PhotoURL string
	Role     string
}

// PersonWorkflow is a workflow launcher the live product adapter has made
// available for a person. It is presentation metadata, not action authority;
// the destination service still authorizes and validates every proposal.
type PersonWorkflow struct {
	ID          string
	Name        string
	Category    string
	Description string
	Href        string
	UseCount    int64
	LaunchHref  func(string) string
}

type StoredTablePreferences struct {
	PageSize        int
	Filters         map[string]string
	Sort, Direction string
}
type StoredUserPreferences struct {
	Version          int64
	Locale           string
	Accessibility    AccessibilityPreferences
	NavCollapsed     bool
	NavigationGroups map[PageID]bool
	FavoritePages    []PageID
	Tables           map[string]StoredTablePreferences
	WorkflowUses     map[string]int64
}

// WorkerIDPolicy is the organization-admin projection of human-facing worker
// number rules. NextSequence and IssuedCount are read-only allocation state.
type WorkerIDPolicy struct {
	Version, StartAt, NextSequence, IncrementBy, IssuedCount          int64
	Prefix, Suffix, Separator, YearFormat, CheckDigit, ExcludedRanges string
	SequenceDigits                                                    int
	ZeroPad, IncludeUnitCode                                          bool
	Previews                                                          []string
}

type OrganizationVisibilityPolicy struct {
	Version           int64
	RoleID            string
	Mode              string
	OrganizationUnits []string
	// DataDomains is the presentation admit-list naming which spec data
	// domains this role's surfaces may present. It grants no authority;
	// the owning domain services still authorize every read.
	DataDomains []string
}

type AccessRole struct {
	Version     int64
	ID          string
	Name        string
	Description string
	System      bool
	Active      bool
}

type WorkerRoleAssignment struct {
	Version   int64
	WorkerRef string
	RoleIDs   []string
}

// RolePagePermission is one role's page-level CRUD boundary. This client
// projection controls affordances; the service independently enforces every
// operation from authenticated role state.
type RolePagePermission struct {
	Version int64
	RoleID  string
	Page    PageID
	View    bool
	Create  bool
	Update  bool
	Delete  bool
}

// View is an already-authorized presentation projection. It contains no
// credential or raw sensitive record and grants no action authority.
type View struct {
	Page              PageID
	Title             string
	Subtitle          string
	Tenant            string
	Principal         string
	Viewer            ViewerProfile
	Scope             string
	Roles             []string
	LogoutHref        string
	Navigation        []NavItem
	NavigationSupport []NavItem
	// NavigationProjection is nil only for isolated legacy/component previews.
	// Once supplied, it owns discoverability and suppresses all registry
	// fallback, including when its answer is empty or malformed.
	NavigationProjection *AuthorizedNavigationProjection
	Work                 []WorkItem
	People               []Person
	PersonWorkflows      []PersonWorkflow
	// LauncherActions is the server-resolved semantic-action projection for
	// the current principal. It is deliberately separate from page CRUD and
	// PersonWorkflows, neither of which grants authority to start an action.
	LauncherActions []LauncherActionProjection
	SelectedWork    string
	SelectedPerson  string
	Query           string
	RolePage        int
	PeoplePage      int
	PeoplePageSize  int
	PeopleTeam      string
	PeopleLocation  string
	// PeopleEligibleOnly filters the directory to workers whose
	// PromotionAvailability resolves to PromotionEligible for the current
	// viewer, so an authorized reader can find candidates without knowing
	// their names in advance.
	PeopleEligibleOnly     bool
	PeopleSort             string
	PeopleDirection        string
	OrganizationView       string
	WorkflowQuery          string
	HistoryQuery           string
	HistoryOutcome         string
	HistoryPerson          string
	HistoryYear            string
	HistorySort            string
	HistoryDirection       string
	HistoryPage            int
	HistoryPageSize        int
	WorkflowUses           map[string]int64
	PreferenceVersion      int64
	AppearanceVersion      int64
	WorkerIDPolicy         WorkerIDPolicy
	WorkerIDValidation     ValidationState
	OrganizationVisibility OrganizationVisibilityPolicy
	AccessRoles            []AccessRole
	RoleAssignments        []WorkerRoleAssignment
	RoleVisibilityPolicies []OrganizationVisibilityPolicy
	RolePagePermissions    []RolePagePermission
	EffectivePermissions   []RolePagePermission
	StoredPreferences      StoredUserPreferences
	Mode                   string
	WorkFilter             string
	JourneyID              string
	JourneyWorker          string
	JourneyMode            string
	NavCollapsed           bool
	MenuQuery              string
	FavoritePages          []PageID
	// NavigationGroupOpen contains the authenticated user's server-side
	// disclosure preferences. Missing entries retain the contextual default.
	NavigationGroupOpen        map[PageID]bool
	Locale                     LocaleContext
	Appearance                 CustomerTheme
	Accessibility              AccessibilityPreferences
	PreviewTheme               func(CustomerTheme)
	SaveTheme                  func(CustomerTheme)
	ResetTheme                 func()
	SaveWorkerIDPolicy         func(WorkerIDPolicy)
	SaveOrganizationVisibility func(OrganizationVisibilityPolicy)
	SaveAccessRole             func(AccessRole)
	SaveWorkerRoleAssignment   func(WorkerRoleAssignment)
	SaveRoleVisibility         func(OrganizationVisibilityPolicy)
	SaveRolePagePermission     func(RolePagePermission)
	PreviewAccessibility       func(AccessibilityPreferences)
	SaveAccessibility          func(AccessibilityPreferences)
	ResetAccessibility         func()
	UpdatePeopleDirectory      func(PeopleDirectoryChange)
	Source                     string
	LoadError                  string
	// Loading is presentation state set only while the route's authorized
	// database-backed projection is resolving. It never implies authority or
	// substitutes empty records for an answer from the service.
	Loading bool
	// ContentLoading is set for an in-app route transition after the shell has
	// already resolved. It keeps application chrome mounted and limits pending
	// semantics and loading geometry to the destination content region.
	ContentLoading bool
	// Refreshing keeps an already-authorized page projection mounted while a
	// newer projection resolves. This prevents fast filter, sort, and paging
	// requests from replacing useful content with a one-frame loading proxy.
	Refreshing bool
	// RefreshingRegion narrows a warm refresh to the component whose data is
	// changing. The surrounding shell remains stable while that component
	// exposes its own aria-busy state and progress cue.
	RefreshingRegion string
	// Navigate is installed by the WASM history router. A nil callback keeps
	// server rendering and tests as ordinary progressive-enhancement links.
	Navigate                  func(string)
	NavigateDebounced         func(string)
	CancelDebouncedNavigation func()
	HistoryNavigation         HistoryNavigationProps
	// ContextSwitcher is the server-resolved tenant and acting-authority
	// projection. It is intentionally separate from Tenant/Scope strings so
	// presentation cannot mint a context or authority from a URL value.
	ContextSwitcher ContextSwitcherProps
	// FederationEntries is the server-composed tenant-federation entry
	// projection, derived from the issuer registry. Presentation renders the
	// entries it is given; it never authors issuers, protocols, or
	// destinations.
	FederationEntries []FederationEntry
	// RecoveryOptions carries the server-composed sign-in recovery
	// destinations for the entry gate. Presentation validates each scheme
	// and drops unsafe ones without inventing replacements.
	RecoveryOptions []RecoveryOption
	// SessionWarning carries the server-projected expiring-session
	// warning. Nil means the session is not expiring; presentation never
	// derives expiry itself.
	SessionWarning *SessionWarningProps
	// StepUpChallenge carries the server-projected step-up challenge for
	// one sensitive action. Nil means no elevation is required; the prompt
	// authorizes nothing either way.
	StepUpChallenge *StepUpChallengeProps
	// BreakGlassActivation carries the server-projected break-glass
	// activation for emergency access. Nil means no emergency elevation
	// is offered; the prompt authorizes nothing either way.
	BreakGlassActivation *BreakGlassActivationProps
	// PolicySimulation carries the server-projected view-as policy
	// simulation. Nil means the viewer sees their own view; the panel
	// assumes no authority either way.
	PolicySimulation *PolicySimulationProps
	// SignedOut carries the server-projected signed-out state. Non-nil
	// converges every authority surface to revoked and makes the
	// signed-out panel the content; nil means the session stands.
	SignedOut *SignedOutProps
	// RecordVerdicts carries the server's per-record authorization
	// verdicts keyed by record ID. Nil or empty means the server is
	// silent and discovery keeps its current set; once present,
	// discovery surfaces admit only disclosable records and project
	// their labels. Presentation never authors verdicts.
	RecordVerdicts map[string]AuthorizedRecord
}

// Can reports whether the resolved role grants an operation on a page. An
// empty permission projection is the compatibility path for cells that have
// not composed role access yet; only legacy view visibility is retained.
func (view View) Can(page PageID, action string) bool {
	for _, permission := range view.EffectivePermissions {
		if permission.Page != page {
			continue
		}
		switch action {
		case "view":
			return permission.View
		case "create":
			return permission.Create
		case "update":
			return permission.Update
		case "delete":
			return permission.Delete
		}
	}
	return len(view.EffectivePermissions) == 0 && action == "view" && PageVisible(page, view.Roles)
}

// Allows keeps isolated component previews and legacy server-rendered tests
// usable before a permission projection is composed. A resolved production
// projection is always authoritative.
func (view View) Allows(page PageID, action string) bool {
	return len(view.EffectivePermissions) == 0 || view.Can(page, action)
}

// NewView creates an empty, honest presentation projection. Live adapters
// populate Work and People from server answers; the component library never
// manufactures business records to make a page look populated.
func NewView(page PageID, tenant, principal, scope string) View {
	if _, ok := LookupPage(page); !ok {
		page = PageHome
	}
	view := View{
		Page: page, Tenant: tenant,
		Principal: principal, Scope: scope, Source: "Workforce directory",
		Locale: ResolveProductLocale(""), Accessibility: DefaultAccessibilityPreferences(),
	}
	return ApplyLocale(view, view.Locale)
}

// ApplyRoleVisibility limits discoverability to the server-admitted roles.
// Transport authorization remains the enforcement boundary; this projection
// prevents navigation, favorites, and global search from advertising routes
// the active identity cannot open.
func ApplyRoleVisibility(view View, roles []string) View {
	// Start from a non-nil slice so an admitted identity with zero roles stays
	// distinguishable from NewView's unrestricted component-preview default.
	view.Roles = append([]string{}, roles...)
	return ApplyNavigationProjection(view, authorizedNavigationForRoles(view.Locale, view.Roles))
}

// ApplyPagePermissions replaces static navigation visibility with the
// effective union of the employee's durable role grants.
func ApplyPagePermissions(view View, permissions []RolePagePermission) View {
	view.EffectivePermissions = append([]RolePagePermission(nil), permissions...)
	return ApplyNavigationProjection(view, authorizedNavigationForPermissions(view.Locale, view.EffectivePermissions))
}

// ApplyLocale resolves all shell and page-registry copy from one immutable
// presentation context. Business records remain untouched.
func ApplyLocale(view View, locale LocaleContext) View {
	locale = locale.normalized()
	view.Locale = locale
	definition, ok := LookupPage(view.Page)
	if !ok {
		definition, _ = LookupPage(PageHome)
	}
	view.Title = locale.Text(definition.TitleKey)
	view.Subtitle = locale.Text(definition.SubtitleKey)
	if view.Page == PageHome && view.Principal != "" {
		name := strings.TrimSpace(view.Viewer.Name)
		if name == "" {
			name = view.Principal
		}
		view.Title = locale.Text("page.home.greeting", map[string]string{"name": name})
	}
	if view.NavigationProjection != nil {
		if err := validateAuthorizedNavigationProjection(*view.NavigationProjection); err != nil {
			// Preserve non-nil authority while dropping every untrusted field.
			// Locale changes must not revive registry navigation after an invalid
			// resolver answer.
			view.NavigationProjection = &AuthorizedNavigationProjection{Version: view.NavigationProjection.Version}
			view.Navigation = nil
			view.NavigationSupport = nil
		} else {
			projection := cloneAuthorizedNavigationProjection(*view.NavigationProjection, locale)
			view.NavigationProjection = &projection
			view.Navigation = navigationItemsFromProjection(projection.Items, locale)
			view.NavigationSupport = navigationItemsFromProjection(projection.Support, locale)
		}
	} else if len(view.Navigation) == 0 {
		view.Navigation = defaultNavigation(locale)
	} else {
		view.Navigation = localizeNavigation(view.Navigation, locale)
	}
	return view
}

func localizeNavigation(items []NavItem, locale LocaleContext) []NavItem {
	result := append([]NavItem(nil), items...)
	for index := range result {
		if result[index].LabelKey != "" {
			result[index].Label = locale.Text(result[index].LabelKey)
		}
		result[index].Children = localizeNavigation(result[index].Children, locale)
	}
	return result
}

func knownPage(page PageID) bool {
	_, ok := LookupPage(page)
	return ok
}
