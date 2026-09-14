package workspace

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The Promotion journey is the workspace's second surface: one governed
// promotion followed from the manager's proposal, through the P1B execution
// authority gate, to the approver's decision and the one ledger fact the
// workflow's END node records. It is a vertical slice of the intended
// experience over the real engine path (CreateIntent, SimulateIntent,
// ExecuteIntent, the WorkItem store, the caller-driven driver's Resume), not
// a mock of it.
//
// This file is the port the page reads and acts through. The implementation
// is internal/intent/app, which owns the intent service, the execution
// driver adapter and the database; this package never imports it. Every
// answer is domain-shaped and every action is one of the engine's own
// governed operations; the page decides how to show them, never what they
// mean.

// Refusals a [JourneyEngine] reports, beyond [ErrDenied] which it shares with
// [Cell].
var (
	// ErrJourneyUnavailable means the cell was not composed with the P1B
	// execution authority (cmd/hcmnext -execution-authority=true), so the
	// journey can be read but nothing can be executed.
	ErrJourneyUnavailable = errors.New("workspace: the journey engine is not composed on this cell")
	// ErrJourneyUnknown means the intent id does not name a promotion
	// journey in the caller's tenant.
	ErrJourneyUnknown = errors.New("workspace: no such journey")
	// ErrJourneyStage means the requested action is not available at the
	// journey's current stage (for example approving a journey that has not
	// been executed, or executing one already complete).
	ErrJourneyStage = errors.New("workspace: the action is not available at this stage")
	// ErrJourneyInput means the proposal input is malformed. The message
	// names the field.
	ErrJourneyInput = errors.New("workspace: invalid proposal input")
	// ErrJourneyActiveConflict means this worker already has a nonterminal
	// promotion whose effective window overlaps the one being proposed
	// (PROMOUX-002). The admission decision is made by
	// internal/data/promotionguard, at the database, before any intent is
	// created; this sentinel is what the page renders instead of a second
	// proposal.
	ErrJourneyActiveConflict = errors.New("workspace: an active promotion already claims this worker and effective window")
)

// JourneyInputError carries a machine-readable correction target across the
// journey port. Detail is for internal diagnostics; transport must project only
// FieldPath, ReasonRef and an optional exact pay range, never infer any of
// them from or expose the detail text.
type JourneyInputError struct {
	FieldPath string
	ReasonRef string
	Detail    string
	PayRange  *JourneyPayRange
}

// JourneyPayRange is the inclusive server-owned correction for a rejected
// salary. Both values are exact Money in the same currency.
type JourneyPayRange struct {
	Minimum values.Money
	Maximum values.Money
}

func (e *JourneyInputError) Error() string {
	if e == nil {
		return ErrJourneyInput.Error()
	}
	return ErrJourneyInput.Error() + ": " + e.FieldPath + ": " + e.Detail
}

func (e *JourneyInputError) Unwrap() error { return ErrJourneyInput }

// JourneyStage is where one promotion journey currently stands. It is
// derived by the engine from durable state (the intent record, the workflow
// instance, its work items and the ledger), never asserted by the page.
type JourneyStage string

const (
	// JourneyStageProposed: the intent exists and has been simulated; no
	// execution has been requested.
	JourneyStageProposed JourneyStage = "PROPOSED"
	// JourneyStageBlocked: the simulation produced no executable plan, so
	// ExecuteIntent would be refused with p1b.no_executable_plan.
	JourneyStageBlocked JourneyStage = "BLOCKED"
	// JourneyStageAwaitingApproval: a workflow instance exists and is parked
	// on its approval WorkItem.
	JourneyStageAwaitingApproval JourneyStage = "AWAITING_APPROVAL"
	// JourneyStageCompleted: the instance reached its APPROVED terminal and
	// the promotion outcome is a recorded ledger fact.
	JourneyStageCompleted JourneyStage = "COMPLETED"
	// JourneyStageRejected: the approver rejected; the instance reached its
	// REJECTED terminal.
	JourneyStageRejected JourneyStage = "REJECTED"
	// JourneyStageFailed: the instance ended in some other terminal
	// (cancelled, superseded, failed).
	JourneyStageFailed JourneyStage = "FAILED"
	// JourneyStageFinanceApproval: the workflow is parked on finance's
	// approval decision.
	JourneyStageFinanceApproval JourneyStage = "FINANCE_APPROVAL"
	// JourneyStageManagerApproval: the workflow is parked on the manager's
	// approval decision.
	JourneyStageManagerApproval JourneyStage = "MANAGER_APPROVAL"
	// JourneyStageWaitingEffectiveDate: approvals are complete and the
	// workflow is waiting for the effective date safe point.
	JourneyStageWaitingEffectiveDate JourneyStage = "WAITING_EFFECTIVE_DATE"
	// JourneyStageRevalidation: the workflow is rechecking its pinned facts
	// before continuing.
	JourneyStageRevalidation JourneyStage = "REVALIDATION"
	// JourneyStageReapproval: a material change requires an approval decision
	// again.
	JourneyStageReapproval JourneyStage = "REAPPROVAL"
	// JourneyStageExecuted: the governed execution has completed its business
	// operation and observation has not yet closed.
	JourneyStageExecuted JourneyStage = "EXECUTED"
	// JourneyStageObservingEffects: the workflow is checking the effects of
	// its execution.
	JourneyStageObservingEffects JourneyStage = "OBSERVING_EFFECTS"
	// JourneyStageRecorded: the terminal promotion fact has been recorded.
	JourneyStageRecorded JourneyStage = "RECORDED"
	// JourneyStageRepairRequired: the workflow requires governed repair before
	// it can reach a consistent terminal state.
	JourneyStageRepairRequired JourneyStage = "REPAIR_REQUIRED"
)

// Manager relationship dispositions are the closed, authorization-safe
// reporting-line states exposed by a workforce listing. They deliberately do
// not expose the raw relationship reference when its endpoint is hidden.
const (
	ManagerRelationshipUnspecified = "UNSPECIFIED"
	ManagerRelationshipRoot        = "ROOT"
	ManagerRelationshipVisible     = "VISIBLE"
	ManagerRelationshipWithheld    = "WITHHELD"
	ManagerRelationshipOrphan      = "ORPHAN"
)

// JourneyPlacement is one side (current or target) of the placement change.
type JourneyPlacement struct {
	JobCode    string
	Grade      string
	PositionID string
	OrgUnit    string
	PayZone    string
}

// JourneySummary is one promotion journey as the list and the header show it.
type JourneySummary struct {
	IntentID      string
	CorrelationID string

	Worker     values.EntityRef
	WorkerName string

	Current JourneyPlacement
	Target  JourneyPlacement

	// CurrentBase and ProposedBase are decimal strings in Currency, as the
	// domain carries them.
	CurrentBase  string
	ProposedBase string
	Currency     string

	// EffectiveDate is ISO-8601 (YYYY-MM-DD).
	EffectiveDate  string
	BusinessReason string

	Stage JourneyStage

	// ProposalRevisionID and MaterialDigest identify the immutable proposal
	// the simulation minted; empty until simulated.
	ProposalRevisionID string
	MaterialDigest     string

	// InstanceID and InstanceVersion name the workflow instance once
	// executed; empty/zero before.
	InstanceID      string
	InstanceVersion int64
	// Approver is the chosen owner of the currently open human-work item. It
	// is presentation evidence for personal queues, never action authority.
	Approver string

	CreatedAt time.Time
	UpdatedAt time.Time

	// GovernanceVersion is the underlying intent record's own optimistic-
	// concurrency version -- unrelated to InstanceVersion above, which
	// names the workflow instance. It is required as
	// expectedInstanceVersion on [JourneyEngine.EditProposal] and as
	// JourneyInterventionRequest.ExpectedInstanceVersion on
	// [JourneyEngine.RequestIntervention]: those two calls act on the
	// intent record itself, not on the workflow instance.
	GovernanceVersion uint64

	// CurrentWorkItem is the journey's one open human work item as the
	// calling viewer is entitled to see it (UXAUDIT-017), or nil when there
	// is none or the work item visibility rules do not admit the viewer.
	CurrentWorkItem *JourneyWorkItemSummary

	// Viewer is how the calling viewer stands to this journey and what its
	// next transition is (PROMOUX-012). The engine resolves it for every
	// summary it returns; the zero value (empty Responsibility) means it was
	// not resolved, and clients treat that as nothing to act on.
	Viewer JourneyViewerProjection
}

// JourneyViewerRelationship is one way the calling viewer stands to a
// journey. Nothing records followers, so there is no follower relationship.
type JourneyViewerRelationship string

const (
	// JourneyViewerInitiator: the viewer is the intent's recorded initiator.
	JourneyViewerInitiator JourneyViewerRelationship = "INITIATOR"
	// JourneyViewerAssignee: the viewer holds the current work item.
	JourneyViewerAssignee JourneyViewerRelationship = "ASSIGNEE"
	// JourneyViewerCandidate: the viewer may claim the current work item.
	JourneyViewerCandidate JourneyViewerRelationship = "CANDIDATE"
)

// JourneyViewerResponsibility is what a journey asks of the calling viewer.
type JourneyViewerResponsibility string

const (
	// JourneyResponsibilityActionRequired: the viewer holds or may claim the
	// current work item, or initiated the journey and its next step is the
	// proposer's.
	JourneyResponsibilityActionRequired JourneyViewerResponsibility = "ACTION_REQUIRED"
	// JourneyResponsibilityTracking: the viewer initiated the journey and its
	// next step is someone else's or the workflow's.
	JourneyResponsibilityTracking JourneyViewerResponsibility = "TRACKING"
	// JourneyResponsibilityObserving: open, visible, no relationship.
	JourneyResponsibilityObserving JourneyViewerResponsibility = "OBSERVING"
	// JourneyResponsibilityClosed: the journey is closed.
	JourneyResponsibilityClosed JourneyViewerResponsibility = "CLOSED"
)

// JourneyNextStep and JourneyStepOwner are the closed vocabularies of a
// journey's next transition. Empty means none / unstated.
type (
	JourneyNextStep  string
	JourneyStepOwner string
)

const (
	JourneyNextStepStartApproval      JourneyNextStep = "START_APPROVAL"
	JourneyNextStepCorrectProposal    JourneyNextStep = "CORRECT_PROPOSAL"
	JourneyNextStepApprovalDecision   JourneyNextStep = "APPROVAL_DECISION"
	JourneyNextStepManagerDecision    JourneyNextStep = "MANAGER_DECISION"
	JourneyNextStepFinanceDecision    JourneyNextStep = "FINANCE_DECISION"
	JourneyNextStepReapprovalDecision JourneyNextStep = "REAPPROVAL_DECISION"
	JourneyNextStepRepair             JourneyNextStep = "REPAIR"
	JourneyNextStepAwaitEffectiveDate JourneyNextStep = "AWAIT_EFFECTIVE_DATE"
	JourneyNextStepSystemProcessing   JourneyNextStep = "SYSTEM_PROCESSING"

	JourneyStepOwnerProposer JourneyStepOwner = "PROPOSER"
	JourneyStepOwnerApprover JourneyStepOwner = "APPROVER"
	JourneyStepOwnerManager  JourneyStepOwner = "MANAGER"
	JourneyStepOwnerFinance  JourneyStepOwner = "FINANCE"
	JourneyStepOwnerSystem   JourneyStepOwner = "SYSTEM"
)

// JourneyViewerProjection is one journey as it stands for the calling viewer.
// It names only the viewer's own standing: it never carries another
// principal's identity, so a viewer with no relationship receives the same
// projection whoever initiated the journey or holds its work item.
type JourneyViewerProjection struct {
	// Relationships are sorted and unique; empty means none.
	Relationships  []JourneyViewerRelationship
	Responsibility JourneyViewerResponsibility
	NextStep       JourneyNextStep
	NextStepOwner  JourneyStepOwner
	// AwaitsPerson is true when a person holds NextStep; an open journey
	// whose next step no person holds is a passive wait.
	AwaitsPerson bool
	// Closed is true at a terminal stage.
	Closed bool
}

// JourneyWorkItemSummary is the list-level, viewer-scoped view of a journey's
// current open work item. The engine fills it under
// internal/humanwork/workitem's own read rules; every string is a token or an
// identifier, never presentation copy.
type JourneyWorkItemSummary struct {
	// Kind and Status are the workitem.Kind and workitem.Status tokens.
	Kind   string
	Status string
	// AssigneePrincipalID is set only for a directly routed item whose
	// identity-bearing context the viewer may see (workitem.ContextVisible).
	AssigneePrincipalID string
	// AssigneeDisplayName is the assignee's worker display name when the
	// principal id resolves to a worker; empty otherwise.
	AssigneeDisplayName string
	// DueAt is the item's deadline; zero when it has none.
	DueAt time.Time
	// ViewerPermittedActions is workitem.PermittedActions for the viewer,
	// sorted.
	ViewerPermittedActions []string
	// ViewerMembership is NONE, CANDIDATE, ASSIGNEE or CLAIMANT.
	ViewerMembership string
}

// JourneyListRequest is the server-owned query for the history projection.
// Cursor is opaque to callers; the engine validates it before use.
type JourneyListRequest struct {
	PageSize  int
	Page      int
	Cursor    string
	WorkerRef string
	Query     string
	Outcome   string
	Year      string
	Sort      string
	Direction string
}

type JourneyListPage struct {
	Journeys   []JourneySummary
	NextCursor string
	TotalCount int
}

// HistoryEngine is an optional extension implemented by live engines that
// can resolve history queries server-side. Keeping it separate preserves the
// small JourneyEngine test seam and makes uncomposed cells fail closed.
type HistoryEngine interface {
	ListJourneysPage(context.Context, JourneyListRequest) (JourneyListPage, error)
}

// ProposalInput is what the manager fills in. Everything else the intent
// needs (the worker's current placement and pay, the budget authority) the
// engine reads through the governed worker read, never from the form.
type ProposalInput struct {
	WorkerRef        string
	TargetJobCode    string
	TargetGrade      string
	TargetPositionID string
	// ProposedBase is a decimal string in the worker's current currency.
	ProposedBase string
	// EffectiveDate is ISO-8601 (YYYY-MM-DD).
	EffectiveDate  string
	BusinessReason string
}

// JourneyFinding is one simulation finding, as the artifact reports it.
type JourneyFinding struct {
	Severity string
	Code     string
	Message  string
}

// JourneyInstance is the workflow instance the journey is running as.
type JourneyInstance struct {
	InstanceID      string
	InstanceVersion int64
	WorkflowID      string
	WorkflowVersion uint32
	PlanDigest      string
	Status          string
	CurrentNodeIDs  []string
	CorrelationID   string
	CreatedAt       time.Time
	StartedAt       *time.Time
	CompletedAt     *time.Time
}

// JourneyNode is one durable node execution row.
type JourneyNode struct {
	NodeID      string
	Attempt     int
	StepType    string
	Status      string
	TraceID     string
	StartedAt   *time.Time
	CompletedAt *time.Time
	RecordedAt  time.Time
}

// JourneyTransition is one WorkItem transition, for the audit trail.
type JourneyTransition struct {
	WorkItemID string
	From       string
	To         string
	Actor      string
	Reason     string
	At         time.Time
}

// JourneyLedgerEvent is the one governed business write the END node raised.
type JourneyLedgerEvent struct {
	StreamKey      string
	Sequence       int64
	SchemaRef      string
	Digest         string
	IdempotencyKey string
	OccurredAt     time.Time
	EffectiveAt    time.Time
	RecordedAt     time.Time
}

// JourneyEvent is one entry of the engine-composed chronological timeline:
// intent created, simulated, gate admitted, instance started, work item
// routed/claimed/completed, node completed, ledger fact recorded.
type JourneyEvent struct {
	At     time.Time
	Actor  string
	Kind   string
	Title  string
	Detail string
	Ref    string
}

// JourneyDetail is everything the detail page shows for one journey.
type JourneyDetail struct {
	Summary JourneySummary

	Findings      []JourneyFinding
	PlannedWrites []string

	// Instance, Nodes, WorkItems and Transitions are nil/empty until the
	// journey has been executed.
	Instance    *JourneyInstance
	Nodes       []JourneyNode
	WorkItems   []workitem.WorkItem
	Transitions []JourneyTransition

	// Ledger is nil until the END node's terminal write has been recorded.
	Ledger *JourneyLedgerEvent

	// EvidenceIDs are every capability-gateway and execution evidence
	// identifier the engine recorded for this journey, in order.
	EvidenceIDs []string

	Timeline []JourneyEvent

	// Approver is the principal the approval WorkItem is routed to. The
	// page shows who will decide; the engine decides as that principal.
	Approver string

	// Server-authorized, viewer-relative presentation capabilities. A client
	// must not reconstruct either from roles or principal identifiers.
	DiagnosticsAvailable bool
	CanDecide            bool
}

// Decision is the approver's answer.
type Decision struct {
	Approve bool
	Reason  string
}

// ---------------------------------------------------------------------------
// Interventions (PROMOUX-013)
// ---------------------------------------------------------------------------

// JourneyInterventionKind is the closed set of typed interventions beyond
// Execute and Decide. Both kinds run the same underlying governed capability
// ([JourneyEngine.RequestIntervention] composes [intent.CancelInstance]); the
// kind only selects which stage offers the affordance and what the
// consequence preview says.
type JourneyInterventionKind string

const (
	// JourneyInterventionWithdraw stops a proposal before any approval has
	// been recorded against it.
	JourneyInterventionWithdraw JourneyInterventionKind = "WITHDRAW"
	// JourneyInterventionCancel requests cancellation while the journey is
	// in an eligible wait (for example, waiting on approval or on its
	// effective-date safe point).
	JourneyInterventionCancel JourneyInterventionKind = "CANCEL"
)

// JourneyInterventionOutcome is the shared, closed disposition an
// intervention resolves to (hcmnext.common.v1.InterventionOutcome). It is
// deliberately the same vocabulary a future workflow-level intervention
// endpoint (EP-WF-002) is designed to adopt, rather than each surface
// inventing its own.
type JourneyInterventionOutcome string

const (
	InterventionApplied          JourneyInterventionOutcome = "APPLIED"
	InterventionPendingSafePoint JourneyInterventionOutcome = "PENDING_SAFE_POINT"
	InterventionDenied           JourneyInterventionOutcome = "DENIED"
	InterventionTooLate          JourneyInterventionOutcome = "TOO_LATE"
	InterventionRepairRequired   JourneyInterventionOutcome = "REPAIR_REQUIRED"
)

// EditProposalInput carries the corrected fields for an unstarted or
// not-yet-approved proposal. The worker is not editable: an edit changes the
// promotion being proposed, never who it is for.
type EditProposalInput struct {
	TargetJobCode    string
	TargetGrade      string
	TargetPositionID string
	// ProposedBase is a decimal string in the worker's current currency.
	ProposedBase string
	// EffectiveDate is ISO-8601 (YYYY-MM-DD).
	EffectiveDate  string
	BusinessReason string
}

// JourneyInterventionPreview answers, without mutating anything, whether a
// typed intervention is available right now and what it would likely do. Its
// UnavailableReasonRef never varies by anything other than the journey's own
// durable stage, so its presence discloses no more than the stage itself
// already does.
type JourneyInterventionPreview struct {
	Available            bool
	UnavailableReasonRef string
	ConsequenceSummary   string
	LikelyOutcome        JourneyInterventionOutcome
	// CurrentGovernanceVersion is the journey's current
	// JourneySummary.GovernanceVersion, present whenever Available is true,
	// so a caller can confirm with RequestIntervention without a second
	// read.
	CurrentGovernanceVersion uint64
}

// JourneyInterventionRequest is one typed WITHDRAW or CANCEL request's
// governance envelope: which kind, a reason, the idempotency key the
// retained evidence is bound to, and the expected instance version that
// pins which revision this caller believes they are acting against.
type JourneyInterventionRequest struct {
	Kind                    JourneyInterventionKind
	ExpectedInstanceVersion uint64
	IdempotencyKey          string
	Reason                  string
}

// JourneyInterventionResult is what a WITHDRAW or CANCEL intervention left
// behind: the journey at its resulting stage, the outcome, and the durable
// evidence reference retained for the intervention itself.
type JourneyInterventionResult struct {
	Journey             JourneySummary
	Outcome             JourneyInterventionOutcome
	RetainedEvidenceRef string
}

// ---------------------------------------------------------------------------
// Workforce
// ---------------------------------------------------------------------------

// A journey needs somebody to run. The three types below are the workforce
// half of the port: the population a manager picks from, the form that adds
// to it, and the closed set of placements the simulation can actually
// evaluate.
//
// The population has two sources and says which is which. The corpus workers
// are the fixed regression population compiled into this release; the created
// workers are the durable ones a user typed in. They are listed together
// because to the person picking one they are the same thing -- an employee --
// and the distinction only matters when explaining where a fact came from.

// Worker sources reported on [WorkerSummary.Source].
const (
	// WorkerSourceCorpus is a worker from the release's fixed corpus. It
	// cannot be created, changed or removed through this port.
	WorkerSourceCorpus = "CORPUS"
	// WorkerSourceCreated is a worker somebody created through
	// [JourneyEngine.CreateWorker]. It is a durable, append-only fact.
	WorkerSourceCreated = "CREATED"
)

// WorkerSummary is one employee as the worker list shows them.
//
// It is a listing row, not a worker record: it carries what a manager needs
// to recognise a person and choose them, and nothing that would make it a
// second, ungoverned way to read worker state. The authoritative answer to
// "what is true about this worker" is still people.ExplainWorkerState through
// the capability gateway, which is what [JourneyEngine.Propose] itself reads.
type WorkerSummary struct {
	// WorkerRef is the stable reference the journey accepts: the corpus key
	// for a corpus worker, the created worker's own key otherwise. It is what
	// [ProposalInput.WorkerRef] is filled with.
	WorkerRef string
	// WorkerID is the entity id the governed read names the worker by.
	WorkerID string
	// SubjectRevision is the server-projected revision a canonical promotion
	// proposal must bind through expected_subject_revision.
	SubjectRevision string

	LegalName     string
	PreferredName string
	WorkerNumber  string

	JobCode    string
	JobTitle   string
	Grade      string
	OrgUnit    string
	PositionID string
	Location   string
	PayZone    string

	// BasePay, Currency and BonusTarget are the declared compensation
	// baseline the promotion simulation reads for this worker. They are
	// empty on a corpus worker, whose baseline comes from the ported legacy
	// corpus rather than from a row of its own.
	BasePay     string
	Currency    string
	BonusTarget string

	// HireDate is ISO-8601 (YYYY-MM-DD).
	HireDate string

	// ManagerRef is the stable relationship reference. ProfilePhotoURL is
	// always the display-safe proxy; the retained original is never exposed
	// through this listing surface.
	ManagerRef string
	// ManagerDisposition and ManagerWorkerRef form the authorization-safe
	// reporting-edge projection. ManagerWorkerRef is set exactly for VISIBLE
	// and names a returned Worker's WorkerRef.
	ManagerDisposition string
	ManagerWorkerRef   string
	ProfilePhotoURL    string

	// Source is [WorkerSourceCorpus] or [WorkerSourceCreated].
	Source string

	// CreatedAt is when a created worker was recorded. It is the zero time
	// for a corpus worker, which was not created at any moment this cell
	// witnessed.
	CreatedAt time.Time
}

// WorkerInput is what somebody fills in to add an employee.
//
// Everything the record needs that is not asked for here -- the worker
// number, the employment and assignment identifiers, the lifecycle status,
// the revision stream position, the bitemporal coordinates -- the engine
// derives, because those are identity and evidence rather than choices a form
// gets to make.
type WorkerInput struct {
	LegalName     string
	PreferredName string

	JobCode    string
	Grade      string
	OrgUnit    string
	PositionID string
	Location   string
	PayZone    string

	// BasePay is a decimal string in Currency. Currency empty means the
	// options' own declared currency.
	BasePay  string
	Currency string
	// BonusTarget is a decimal fraction (0.0500 is five percent). Empty means
	// the engine's own default.
	BonusTarget string

	// HireDate is ISO-8601 (YYYY-MM-DD).
	HireDate string
	// ManagerRef is the manager relationship reference. Empty means the
	// engine derives one.
	ManagerRef string
}

// WorkforceOptions is the closed set of placements a created worker may be
// given, derived by the engine from the pay-band catalog and the corpus
// population.
//
// It exists so the refusal and the form agree. A worker placed on a job code,
// grade and pay zone no band covers is a worker whose promotion the rewards
// engine can only answer "no band found" for -- a simulation-time refusal for
// a mistake made at creation time. [JourneyEngine.CreateWorker] refuses that
// placement outright with [ErrJourneyInput] naming the field, and this is the
// same answer stated in advance so a page can offer choices rather than let
// somebody discover the rule by failing.
type WorkforceOptions struct {
	JobCodes  []string
	Grades    []string
	OrgUnits  []string
	PayZones  []string
	Positions []string
	// Placements retains exact catalog combinations. Independent code and
	// grade lists are not sufficient to decide whether their cross-product is
	// a real governed placement.
	Placements []WorkforcePlacementOption
	// PromotionPaths are the immutable job-architecture edges that decide
	// which of those payable placements is a valid next role.
	PromotionPaths []PromotionPathOption
	// Currency is the one currency the catalog is denominated in. A created
	// worker's baseline is carried in it.
	Currency string
}

type WorkforcePlacementOption struct {
	JobCode  string
	Grade    string
	PayZone  string
	Currency string
}

type PromotionPathOption struct {
	PathRef, Revision          string
	SourceProfileRef           string
	SourceJobCode, SourceGrade string
	TargetProfileRef           string
	TargetJobCode, TargetGrade string
	TargetTitle, Kind          string
	MinimumBaseIncrease        string
	MaximumBaseIncrease        string
	CompensationPolicyRef      string
	BenefitRuleRefs            []string
}

// JourneyEngine is the live engine the journey page reads and acts through.
//
// Every method runs on behalf of the principal in ctx (the same admission
// the workspace and the API share) and refuses under the same policy the
// RPC surfaces would. Propose, Execute and Decide are the only writes, and
// each is one of the engine's own governed operations: CreateIntent +
// SimulateIntent, ExecuteIntent behind the P1B authority gate, and the
// WorkItem claim/complete plus the driver's Resume.
type JourneyEngine interface {
	// ListJourneys returns every promotion journey in the caller's tenant,
	// newest first.
	ListJourneys(ctx context.Context) ([]JourneySummary, error)
	// Propose creates and simulates a new promotion intent from the
	// manager's input, and returns it at JourneyStageProposed (or
	// JourneyStageBlocked when the simulation produced no executable plan).
	Propose(ctx context.Context, in ProposalInput) (JourneySummary, error)
	// Inspect returns everything durable about one journey.
	Inspect(ctx context.Context, intentID string) (JourneyDetail, error)
	// Execute runs ExecuteIntent for an approved, re-simulated proposal and
	// returns the journey parked at its approval WorkItem.
	Execute(ctx context.Context, intentID string) (JourneyDetail, error)
	// Decide claims and completes the approval WorkItem as the routed
	// approver with the given decision, resumes the driver, and returns the
	// journey at its resulting stage.
	Decide(ctx context.Context, intentID string, d Decision) (JourneyDetail, error)

	// EditProposal corrects an unstarted or not-yet-approved proposal
	// (PROMOUX-013). It is [intent.SupersedeOriginal] scoped to journeys: the
	// edited fields are re-proposed as an authorized successor intent under
	// a fresh proposal revision, and the original's request state moves to
	// SUPERSEDED without otherwise mutating it -- so any approval recorded
	// against the original's proposal revision is left referring to a
	// revision that can never execute, which is how an edit invalidates
	// material approvals without touching the approval record itself. It
	// returns the successor summary and the original's own intent id. A
	// stale expectedInstanceVersion, or an already-terminal original, is
	// refused.
	EditProposal(ctx context.Context, intentID string, expectedInstanceVersion uint64, idempotencyKey, reason string, in EditProposalInput) (successor JourneySummary, supersededIntentID string, err error)

	// PreviewIntervention answers, for one journey and one typed
	// intervention kind, whether the intervention is available right now,
	// and if not, why -- computed only from the journey's own durable
	// stage. It mutates nothing and runs no capability.
	PreviewIntervention(ctx context.Context, intentID string, kind JourneyInterventionKind) (JourneyInterventionPreview, error)

	// RequestIntervention runs a typed WITHDRAW or CANCEL intervention. It
	// is [intent.CancelInstance] scoped to journeys: both kinds are the same
	// governed capability, reporting the same four dispositions
	// CancelIntent already reports, projected onto
	// [JourneyInterventionOutcome]. Racing this call against a concurrent
	// approval, timer fire or terminal commit resolves to exactly one
	// durable outcome, because the intent record's own optimistic
	// instance-version compare-and-swap is the single serialization point a
	// losing caller's request never gets past.
	RequestIntervention(ctx context.Context, intentID string, req JourneyInterventionRequest) (JourneyInterventionResult, error)

	// ListWorkers returns every employee a journey can be proposed for --
	// the release's corpus population and the tenant's own created one,
	// newest created first and the corpus after -- together with the closed
	// set of placements a new employee may be given.
	//
	// The options travel with the list rather than on a separate call
	// because they are what makes the list actionable: a caller that has one
	// and not the other can render a picker but not a form.
	ListWorkers(ctx context.Context) ([]WorkerSummary, WorkforceOptions, error)
	// CreateWorker records one new employee as a durable, append-only fact
	// and returns it as the list would show it.
	//
	// It is a governed write under the same execution authority
	// [JourneyEngine.Execute] and [JourneyEngine.Decide] run behind:
	// creating the subject of a promotion is a workforce change, and a
	// surface that could add employees without that authority would be a way
	// around the gate rather than a step before it. A caller without it is
	// refused [ErrDenied]; a placement outside [WorkforceOptions], a missing
	// name, a non-positive base pay or a malformed hire date is refused
	// [ErrJourneyInput] naming the field.
	CreateWorker(ctx context.Context, in WorkerInput) (WorkerSummary, error)
}
