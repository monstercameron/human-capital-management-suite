package workflow_test

// This file proves the EXECUTE driver and runtime are workflow-agnostic
// (internal/workflow/hireexec's own package doc explains why): the exact same
// internal/workflow/execute.Driver that drives Promotion elsewhere in this
// package also drives an entirely different business process, "New employee
// hire", through APPROVAL, SIGNAL, DECISION, four sequential TASKs, WAIT and
// a governed CAPABILITY commit, with no change to the driver.
//
// Everything here is prefixed "hire" and lives only in this file; it reuses
// this package's existing generic helpers verbatim (insertTenant, appConn,
// inTenantTx, newLedgerAppender, demoControlSnapshots, countRows,
// approvedStartFacts, acceptingSignalVerifier, waitTimerReader and the
// generic promotionApprovalOutcome, which takes the node id as a parameter
// and is not Promotion-specific) rather than redefining any of them.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/hireexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/notifyplan"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepsapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
	stepstask "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/task"
	stepswait "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// hireWaitDataset is the tzdb/calendar release await_start_date's wake
// condition resolves against.
var hireWaitDataset = values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "2026.1"}

const hireBackgroundCheckField = "background_check_result"

// Owner principals for the four sequential provisioning tasks WF-EXT-019
// explains cannot fan out, plus the candidate who submits their own forms.
// The hiring manager (humanwork.PrincipalManager) and HR business partner
// (humanwork.PrincipalHRBP) fixtures are reused as-is.
const (
	hirePrincipalCandidate  = "principal:new-hire-candidate-1"
	hirePrincipalIT         = "principal:it-provisioning"
	hirePrincipalFacilities = "principal:facilities-provisioning"
	hirePrincipalPayroll    = "principal:payroll-ops"
)

// ---------------------------------------------------------------------------
// Version registry: an in-memory single-plan store, exactly the shape
// promotion_full_test.go's own promotionVersionStore is, chosen over
// version.Publish/Activate because that path recompiles the definition under
// the exact same [workflow.Options] the plan was compiled with (including the
// capability registry hireexec.Compile binds internally) to prove the digests
// match, and hireexec keeps that registry unexported.
// ---------------------------------------------------------------------------

type hireVersionStore struct{ plan *workflow.CompiledWorkflow }

func (s hireVersionStore) Put(version.CompiledVersion) error { return nil }
func (s hireVersionStore) GetByDigest(digest string) (version.CompiledVersion, bool, error) {
	if s.plan == nil || digest != s.plan.Digest() {
		return version.CompiledVersion{}, false, nil
	}
	return version.CompiledVersion{WorkflowID: s.plan.WorkflowID, CompiledPlanDigest: digest, Status: version.StatusActive}, true, nil
}
func (s hireVersionStore) GetActiveForWorkflow(workflowID string) (version.CompiledVersion, bool, error) {
	if s.plan == nil || workflowID != s.plan.WorkflowID {
		return version.CompiledVersion{}, false, nil
	}
	return version.CompiledVersion{WorkflowID: workflowID, CompiledPlanDigest: s.plan.Digest(), Status: version.StatusActive}, true, nil
}
func (s hireVersionStore) List(workflowID string) ([]version.CompiledVersion, error) {
	if s.plan == nil || workflowID != s.plan.WorkflowID {
		return nil, nil
	}
	v, _, _ := s.GetActiveForWorkflow(workflowID)
	return []version.CompiledVersion{v}, nil
}

// ---------------------------------------------------------------------------
// Proposal fixture: a real, immutable ProposalRevision naming the candidate's
// target placement and, WF-EXT-004, the background-check verdict the
// evaluate_background_check DECISION reads -- since node inputs are not
// resolved on the durable path, that verdict has to live somewhere the bound
// proposal itself carries, not in a compiled InputMapping.
// ---------------------------------------------------------------------------

func hireNewProposal(t *testing.T, tenant values.TenantId, intentID string, at, startDate time.Time, backgroundCheckResult string) intent.ProposalRevision {
	t.Helper()
	digester, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("protomap.NewDefaultDigester: %v", err)
	}
	// The candidate's own start date is the proposal's effective time --
	// WF-EXT-012's hireTimerFactory reads it straight from here to derive
	// await_start_date's real per-run wake instant.
	interval, err := values.NewOpenInstantInterval(values.NewInstant(startDate))
	if err != nil {
		t.Fatalf("NewOpenInstantInterval: %v", err)
	}
	candidateKey, err := values.NewResourceKey(tenant, "employment", "new-hire-candidate-1")
	if err != nil {
		t.Fatalf("NewResourceKey: %v", err)
	}
	subject := intent.SubjectReference{Kind: "EMPLOYMENT", SubjectID: "employment:new-hire-candidate-1", AuthorityDomain: "PEOPLE"}
	rev, err := intent.NewProposalRevision(intent.ProposalSpec{
		IntentID: intentID, Revision: 1, Tenant: tenant, OrganizationScopeID: humanwork.ScenarioOrganizationScopeID,
		Subjects:      []intent.SubjectReference{subject},
		EffectiveTime: interval, ControlSnapshots: demoControlSnapshots(),
		ProposedState: []intent.StateAssertion{
			{Subject: subject, ResourceKey: candidateKey, FieldPath: "position_id", CanonicalText: "position:software-engineer"},
			{Subject: subject, ResourceKey: candidateKey, FieldPath: hireBackgroundCheckField, CanonicalText: backgroundCheckResult},
		},
		CreatedBy: intent.PrincipalReference{PrincipalID: humanwork.PrincipalRequester, Kind: intent.InitiatorHuman, IdentityAssuranceRef: "assurance:1"},
	}, intent.Definition{
		Ref: intent.Ref{TypeID: "hcmnext.test.new_hire", Version: 1}, Family: intent.FamilyChangeRequest,
	}, digester, nil, func() values.Instant { return values.NewInstant(at) })
	if err != nil {
		t.Fatalf("intent.NewProposalRevision: %v", err)
	}
	return rev
}

// hireProposalStateValue reads the one proposed-state assertion a hire
// StepRunner needs back out, by field path.
func hireProposalStateValue(rev intent.ProposalRevision, fieldPath string) string {
	for _, a := range rev.ProposedState {
		if a.FieldPath == fieldPath {
			return a.CanonicalText
		}
	}
	return ""
}

// hireApprovalRequirement compiles the one approval requirement this graph
// declares, mirroring promotionexec's own compileApprovalRequirement (that
// package's helper is promotion-specific and unexported, so it is not reused
// directly, but the shape -- a single named candidate, quorum one, separation
// of duties, a bounded deadline and the three standard invalidators -- is the
// same real humanwork.Compile contract every reference workflow uses).
func hireApprovalRequirement(approver string, decideBy time.Time) (humanwork.ApprovalRequirement, error) {
	if approver == "" {
		return humanwork.ApprovalRequirement{}, fmt.Errorf("test/workflow: hire approval requirement needs an approver principal")
	}
	if decideBy.IsZero() {
		return humanwork.ApprovalRequirement{}, fmt.Errorf("test/workflow: hire approval requirement needs a decision deadline")
	}
	deadline := values.NewInstant(decideBy.UTC().Truncate(time.Second))
	return humanwork.Compile(humanwork.RequirementSpec{
		RequirementID: hireexec.ApprovalHiringManager, Revision: 1, Stage: 1,
		Candidates: humanwork.Named(approver, "policy.new_hire.hiring_manager/v1",
			humanwork.Scope{Kind: humanwork.ScopeOrganization, Ref: humanwork.ScenarioOrganizationScopeID}),
		AuthorityFloor: []string{"hiring_manager"},
		Quorum:         humanwork.Quorum{MinApprovals: 1},
		Deadline:       humanwork.Deadline{DecideBy: deadline, Expiry: deadline},
		Escalation: humanwork.EscalationPolicy{
			OnDeadline: humanwork.EscalationBlock, RuleID: "rule.new_hire.escalation.block/v1",
		},
		Separation: humanwork.SeparationConstraints{
			RequesterMayNotApprove: true, SubjectMayNotApprove: true, RuleID: "rule.new_hire.separation/v1",
		},
		Invalidators: []humanwork.Invalidator{
			{Kind: humanwork.InvalidatorMaterialProposalChange, RuleID: "rule.new_hire.invalidate.material_change/v1"},
			{Kind: humanwork.InvalidatorAuthorityRevoked, RuleID: "rule.new_hire.invalidate.authority_revoked/v1"},
			{Kind: humanwork.InvalidatorDeadlineExpired, RuleID: "rule.new_hire.invalidate.deadline/v1"},
		},
		Source: humanwork.RequirementSource{
			Tier: rules.ApprovalTierStandard, TableID: "new_hire.approval.tier", TableVersion: "1",
			TableDigest: "sha256:new-hire-approval-tier", MatchedRowID: "new_hire.standard",
			GovernancePolicyRef: "governance.new_hire.hiring_manager/v1",
		},
	})
}

// ---------------------------------------------------------------------------
// StepRunner: prepare_hire (TRANSFORM) always succeeds, END nodes assert
// nothing (the pinned plan's own compiled Terminal wins), commit_hire
// (CAPABILITY) performs no side effect of its own -- as endOnlySteps performs
// none for Promotion's demo graph -- and runs as a plain StepRunner rather
// than a TransactionalStepRunner, exactly as promotionsteps.Runner does for
// Promotion's own execute_promotion node (neither implements RunInTx).
// evaluate_background_check (DECISION) is the one node whose route is a real
// business decision: WF-EXT-004 means it reads the verdict off the bound
// proposal's own encoded state rather than a compiled InputMapping.
// ---------------------------------------------------------------------------

type hireSteps struct{}

func (hireSteps) Run(_ context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	switch req.Node.Type {
	case workflow.StepEnd:
		return frontier.NodeOutcome{NodeID: req.Node.ID}, runtime.GovernanceRefs{}, nil
	case workflow.StepTransform:
		return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: workflow.OutcomeSucceeded}, runtime.GovernanceRefs{}, nil
	case workflow.StepCapability:
		return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: workflow.OutcomeSucceeded}, runtime.GovernanceRefs{}, nil
	case workflow.StepDecision:
		route := hireexec.RouteBackgroundCheckClear
		if hireProposalStateValue(req.Proposal.Revision, hireBackgroundCheckField) == hireexec.RouteBackgroundCheckAdverse {
			route = hireexec.RouteBackgroundCheckAdverse
		}
		return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: workflow.Outcome(route)}, runtime.GovernanceRefs{}, nil
	default:
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf(
			"test/workflow: unexpected READY step type %s for node %s", req.Node.Type, req.Node.ID)
	}
}

// ---------------------------------------------------------------------------
// WorkItemFactory: opens the governed APPROVAL through steps/approval.Open,
// and every TASK through workitem.NewWorkItem, exactly as demoWorkItems does
// for Promotion's own demo graph -- ProposalRef must be set by hand because
// steps/task.Open does not accept or set it, and execute's own validateResume
// unconditionally checks it for every resumed WorkItem, APPROVAL or TASK
// alike.
// ---------------------------------------------------------------------------

// hireTaskSpec is the one table naming each TASK node's work type and owner
// principal, shared by hireWorkItems.CreateAndRoute (which routes the work)
// and every test below (which builds the matching stepstask.CompiledTaskNode
// and completes the work as that exact owner).
func hireTaskSpec(nodeID string) (workType, owner string, ok bool) {
	switch nodeID {
	case hireexec.NodeReviewAdverseResult:
		return hireexec.WorkTypeReviewAdverseResult, humanwork.PrincipalHRBP, true
	case hireexec.NodeCollectNewHireForms:
		return hireexec.WorkTypeNewHireForms, hirePrincipalCandidate, true
	case hireexec.NodeProvisionITAccess:
		return hireexec.WorkTypeProvisionITAccess, hirePrincipalIT, true
	case hireexec.NodeProvisionWorkspace:
		return hireexec.WorkTypeProvisionWorkspace, hirePrincipalFacilities, true
	case hireexec.NodeEnrollPayroll:
		return hireexec.WorkTypeEnrollPayroll, hirePrincipalPayroll, true
	default:
		return "", "", false
	}
}

type hireWorkItems struct {
	proposal   intent.ProposalRevision
	managerReq humanwork.ApprovalRequirement
}

func (f hireWorkItems) CreateAndRoute(ctx context.Context, ex workitem.Executor, req execute.WorkItemRequest) (workitem.WorkItem, error) {
	store := workitem.Store{}
	if req.Continuation.TargetNodeID == hireexec.NodeApproveOffer {
		item, err := stepsapproval.Open(ctx, ex, store, stepsapproval.OpenInput{
			TenantID: req.Continuation.TenantID, WorkItemID: req.WorkItemID,
			WorkflowInstanceID: req.Continuation.InstanceID, CorrelationID: req.CorrelationID, SubjectRefs: req.SubjectRefs,
			Node: stepsapproval.CompiledApprovalNode{
				WorkflowID: hireexec.WorkflowID, WorkflowVersion: hireexec.Version, NodeID: hireexec.NodeApproveOffer, WorkType: hireexec.ApprovalHiringManager,
				PolicyRouteRef: "route.new_hire.hiring_manager_approval/v1", Visibility: workitem.VisibilityAssigneeOnly,
				OrganizationScopeID: humanwork.ScenarioOrganizationScopeID,
			},
			Requirement: f.managerReq, Proposal: f.proposal, Now: req.CreatedAt,
			Meta: workitem.TransitionMeta{ActorPrincipalID: "system:workflow-runtime", Reason: "WORKFLOW_APPROVAL_REQUIRED", At: req.CreatedAt},
		})
		if err != nil {
			return workitem.WorkItem{}, fmt.Errorf("steps/approval.Open: %w", err)
		}
		assignment := workitem.Assignment{
			Resolution: humanwork.Resolution{
				RequirementID: hireexec.ApprovalHiringManager, RequirementRevision: f.managerReq.Revision, RequirementDigest: f.managerReq.Digest(),
				Outcome:    humanwork.OutcomeResolved,
				Candidates: []humanwork.Candidate{{PrincipalID: humanwork.PrincipalManager, Via: humanwork.SourceDirect, TermRef: "new_hire.fixture"}},
				ResolvedAt: values.NewInstant(req.CreatedAt), EffectiveAt: values.NewInstant(req.CreatedAt),
				DirectoryVersion: "directory.test.new-hire/v1", ExpressionDigest: f.managerReq.ExpressionDigest,
				QuorumRequired: 1,
			},
			GovernancePolicyRef: "governance.new_hire.hiring_manager/v1", Trigger: workitem.TriggerInitialRouting, ChosenOwner: humanwork.PrincipalManager,
		}
		return store.Route(ctx, ex, item.TenantID, item.WorkItemID, item.ItemVersion, assignment,
			workitem.TransitionMeta{ActorPrincipalID: "system:workflow-runtime", Reason: "ASSIGNMENT_RESOLVED", At: req.CreatedAt})
	}

	workType, owner, ok := hireTaskSpec(req.Continuation.TargetNodeID)
	if !ok {
		return workitem.WorkItem{}, fmt.Errorf("test/workflow: no human-work spec bound for node %s", req.Continuation.TargetNodeID)
	}
	draft, err := workitem.NewWorkItem(workitem.NewWorkItemInput{
		TenantID: req.Continuation.TenantID, WorkItemID: req.WorkItemID,
		Kind: workitem.KindTask, WorkType: workType,
		CorrelationID: req.CorrelationID, WorkflowInstanceID: req.Continuation.InstanceID, NodeID: req.Continuation.TargetNodeID,
		ProposalRef:         req.Proposal.Revision.MaterialDigest.Digest,
		SubjectRefs:         req.SubjectRefs,
		PolicyRouteRef:      "route.new_hire." + req.Continuation.TargetNodeID + "/v1",
		Visibility:          workitem.VisibilityAssigneeOnly,
		OrganizationScopeID: humanwork.ScenarioOrganizationScopeID,
		DeadlineAt:          req.CreatedAt.Add(72 * time.Hour),
		CreatedAt:           req.CreatedAt,
	})
	if err != nil {
		return workitem.WorkItem{}, fmt.Errorf("build task work item for %s: %w", req.Continuation.TargetNodeID, err)
	}
	item, err := store.Create(ctx, ex, draft, workitem.TransitionMeta{ActorPrincipalID: "system:workflow-runtime", Reason: "WORKFLOW_TASK_REQUIRED", At: req.CreatedAt})
	if err != nil {
		return workitem.WorkItem{}, fmt.Errorf("create task work item for %s: %w", req.Continuation.TargetNodeID, err)
	}
	resolution := humanwork.Resolution{
		RequirementID: workType, Outcome: humanwork.OutcomeResolved,
		Candidates: []humanwork.Candidate{{PrincipalID: owner, Via: humanwork.SourceDirect, TermRef: "new_hire.fixture"}},
		ResolvedAt: values.NewInstant(req.CreatedAt), EffectiveAt: values.NewInstant(req.CreatedAt),
		DirectoryVersion: "directory.test.new-hire/v1", ExpressionDigest: "expr.test.new-hire." + req.Continuation.TargetNodeID + "/v1",
		QuorumRequired: 1,
	}
	assignment := workitem.Assignment{Resolution: resolution, GovernancePolicyRef: "policy.test.task_direct_assignment/v1", Trigger: workitem.TriggerInitialRouting, ChosenOwner: owner}
	return store.Route(ctx, ex, item.TenantID, item.WorkItemID, item.ItemVersion, assignment,
		workitem.TransitionMeta{ActorPrincipalID: "system:workflow-runtime", Reason: "ASSIGNMENT_RESOLVED", At: req.CreatedAt})
}

func hireTaskNode(plan *workflow.CompiledWorkflow, nodeID, workType string) stepstask.CompiledTaskNode {
	node, _ := plan.Node(nodeID)
	return stepstask.CompiledTaskNode{
		WorkflowID: plan.WorkflowID, WorkflowVersion: plan.Version, NodeID: nodeID, WorkType: workType,
		OutputSchema:        node.OutputSchema,
		FormDefinition:      stepstask.VersionedRef{Ref: "form.new_hire." + nodeID, Version: 1},
		AccessibilityPolicy: stepstask.VersionedRef{Ref: "policy.accessibility.default/v1", Version: 1},
		AccommodationPolicy: stepstask.VersionedRef{Ref: "policy.accommodation.default/v1", Version: 1},
	}
}

// ---------------------------------------------------------------------------
// TerminalWriter: only the authoritative commit's own success terminal
// (TerminalHired) appends a governed ledger fact -- every other exit is a
// routing outcome with nothing to record, exactly as promotion_full_test.go's
// own promotionFullTerminalWriter filters Promotion's terminal codes.
// ---------------------------------------------------------------------------

type hireTerminalWriter struct{ delegate *effects.LedgerTerminalWriter }

func (w hireTerminalWriter) Write(ctx context.Context, tx dbport.Tx, req execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	if req.TerminalCode != hireexec.TerminalHired {
		return idempotency.ResultIdentity{ResultRef: req.TerminalCode}, nil
	}
	return w.delegate.Write(ctx, tx, req)
}

// ---------------------------------------------------------------------------
// TimerFactory: WF-EXT-012. await_start_date's compiled WakeInstant is an
// unused definition-time placeholder; the real per-run wake instant is always
// the bound proposal's own effective time (the candidate's start date).
// hireComputeWaitRequirement is a pure function of (plan, proposal, dataset)
// so both the factory that mints the durable promise and the test that later
// resolves it recompute the identical requirement independently -- neither
// needs to remember anything a driver instance held.
// ---------------------------------------------------------------------------

func hireComputeWaitRequirement(plan *workflow.CompiledWorkflow, node workflow.CompiledNode, proposal intent.ProposalRevision, dataset values.DatasetVersions) (stepswait.TimerRequirement, error) {
	compiled, err := stepswait.FromCompiled(&node)
	if err != nil {
		return stepswait.TimerRequirement{}, fmt.Errorf("hire wait: bind compiled node: %w", err)
	}
	compiled.WorkflowID, compiled.WorkflowVersion = plan.WorkflowID, plan.Version
	start, ok := proposal.EffectiveTime.StartInstant()
	if !ok {
		return stepswait.TimerRequirement{}, fmt.Errorf("hire wait: proposal %s carries no effective start instant", proposal.ProposalRevisionID)
	}
	compiled.WakeInstant = start
	return stepswait.ComputeTimerRequirement(compiled, dataset)
}

type hireTimerFactory struct {
	scheduler timer.Scheduler
	dataset   values.DatasetVersions
}

func (f hireTimerFactory) CreateTimer(ctx context.Context, ex runtime.Executor, req execute.TimerRequest) (execute.TimerHandle, error) {
	requirement, err := hireComputeWaitRequirement(req.Plan, req.Node, req.Proposal.Revision, f.dataset)
	if err != nil {
		return execute.TimerHandle{}, err
	}
	scheduled, err := f.scheduler.Schedule(ctx, ex, timer.Request{
		TenantID: req.Continuation.TenantID, InstanceID: req.Continuation.InstanceID,
		NodeID: req.Continuation.TargetNodeID, Kind: timer.KindUntil,
		Requirement: requirement, CreatedAt: req.CreatedAt, Attempt: req.Continuation.TargetAttempt,
	})
	if err != nil {
		return execute.TimerHandle{}, err
	}
	return execute.TimerHandle{TimerID: scheduled.Timer.TimerID, NodeID: scheduled.Timer.NodeID,
		Key: scheduled.Timer.Key, FiresAt: scheduled.Timer.FiresAt, Replay: scheduled.Replay}, nil
}

// ---------------------------------------------------------------------------
// hireFixture: the one plan, proposal and driver factory every test below
// shares. driver builds a brand-new *execute.Driver -- a fresh execute.New,
// nothing cached -- for every call, exactly as promotion_full_test.go's own
// promotionFullFixture.driver does.
// ---------------------------------------------------------------------------

type hireFixture struct {
	db         *pgtest.DB
	tenantID   uuid.UUID
	key        string
	at         time.Time
	startAt    time.Time
	proposal   intent.ProposalRevision
	plan       *workflow.CompiledWorkflow
	versions   version.Store
	start      runtime.StartRequest
	managerReq humanwork.ApprovalRequirement
	terminal   execute.TerminalWriter
	workItems  execute.WorkItemFactory
}

func newHireFixture(t *testing.T, key, backgroundCheckResult string, at time.Time) hireFixture {
	t.Helper()
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, key, at)

	plan, err := hireexec.Compile()
	if err != nil {
		t.Fatalf("hireexec.Compile: %v", err)
	}
	versions := hireVersionStore{plan: plan}

	startAt := at.Add(30 * 24 * time.Hour)
	proposal := hireNewProposal(t, values.TenantId(key), "intent:"+key, at, startAt, backgroundCheckResult)
	binding := runtime.ProposalBinding{Revision: proposal, ApprovalRef: "decision:hiring-manager-approves-offer"}

	managerReq, err := hireApprovalRequirement(humanwork.PrincipalManager, at.Add(72*time.Hour))
	if err != nil {
		t.Fatalf("hireApprovalRequirement: %v", err)
	}

	resolver := effects.PolicyResolver{Entries: []effects.PolicyEntry{{
		WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: plan.Digest()}, Plan: plan,
	}}}
	terminal := execution.WithRequesterStatusTerminal(hireTerminalWriter{delegate: &effects.LedgerTerminalWriter{
		Appender: newLedgerAppender(t), ProjectionName: "hire_execute_" + key, SourceRef: "hcmnext:test:workflow",
	}})
	workItemsFactory := execution.WithWorkNotifications(hireWorkItems{proposal: proposal, managerReq: managerReq})

	start := runtime.StartRequest{
		TenantID: tenantID, CellID: "cell-local", StartIdempotencyKey: "start:" + key,
		Resolver: resolver, Versions: versions, Proposal: binding,
		ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: approvedStartFacts(proposal),
		ExpectedIntentID: proposal.IntentID, ExpectedTenant: proposal.Tenant,
		BusinessSubjectRefs: []string{"employment:new-hire-candidate-1"},
		ExecutionMode:       workflow.ModeExecute, CorrelationID: "corr:" + key, CreatedAt: at,
	}

	return hireFixture{
		db: db, tenantID: tenantID, key: key, at: at, startAt: startAt,
		proposal: proposal, plan: plan, versions: versions, start: start,
		managerReq: managerReq, terminal: terminal, workItems: workItemsFactory,
	}
}

func (f hireFixture) driver(t *testing.T, at time.Time) *execute.Driver {
	t.Helper()
	drv, err := execute.New(execute.Options{
		DB: appConn(t, f.db), Steps: hireSteps{}, WorkItems: f.workItems, Terminal: f.terminal,
		Items: workitem.Store{}, Guard: idempotency.PostgresStore{},
		Retention:   idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock:       func() time.Time { return at },
		Timers:      hireTimerFactory{scheduler: timer.Scheduler{}, dataset: hireWaitDataset},
		TimerReader: waitTimerReader{scheduler: timer.Scheduler{}},
		Signals:     execution.SignalSubscriptions{}, SignalReader: execution.SignalSubscriptions{}, SignalTimeoutReader: execution.SignalSubscriptions{},
	})
	if err != nil {
		t.Fatalf("execute.New: %v", err)
	}
	return drv
}

// ---------------------------------------------------------------------------
// Human-work completion helpers.
// ---------------------------------------------------------------------------

func hireCompleteApproval(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, item workitem.WorkItem, req humanwork.ApprovalRequirement, proposal intent.ProposalRevision, principal string, at time.Time, outcome intentapproval.Outcome) workitem.WorkItem {
	t.Helper()
	var completed workitem.WorkItem
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		store := workitem.Store{}
		claimed, err := store.Claim(context.Background(), tx, workitem.ClaimInput{
			TenantID: tenantID, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
			ClaimantPrincipalID: principal, ClaimExpiresAt: at.Add(time.Hour), Now: at,
			Meta: workitem.TransitionMeta{ActorPrincipalID: principal, Reason: "HIRE_APPROVAL_CLAIMED", At: at},
		})
		if err != nil {
			return err
		}
		started, err := store.Start(context.Background(), tx, tenantID, item.WorkItemID, claimed.ItemVersion, at,
			workitem.TransitionMeta{ActorPrincipalID: principal, Reason: "HIRE_APPROVAL_STARTED", At: at})
		if err != nil {
			return err
		}
		decision := approvalDecisionFor(workitem.WorkItem{CompletedBy: principal}, req, proposal, at)
		decision.Outcome = outcome
		var completeErr error
		completed, completeErr = stepsapproval.Complete(context.Background(), tx, store, started, decision, at,
			workitem.TransitionMeta{ActorPrincipalID: principal, Reason: "HIRE_APPROVAL_DECIDED", At: at})
		return completeErr
	})
	return completed
}

func hireSubmitTask(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, node stepstask.CompiledTaskNode, item workitem.WorkItem, principal string, at time.Time) (workitem.WorkItem, stepstask.Submission) {
	t.Helper()
	var completed workitem.WorkItem
	var submission stepstask.Submission
	claimID := uuid.New()
	claimExpires := at.Add(2 * time.Hour)
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		store := workitem.Store{}
		claimed, err := store.Claim(context.Background(), tx, workitem.ClaimInput{
			TenantID: tenantID, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
			ClaimantPrincipalID: principal, ClaimExpiresAt: claimExpires, Now: at,
			Meta: workitem.TransitionMeta{ActorPrincipalID: principal, Reason: "HIRE_TASK_CLAIMED", At: at},
		})
		if err != nil {
			return err
		}
		started, err := store.Start(context.Background(), tx, tenantID, item.WorkItemID, claimed.ItemVersion, at,
			workitem.TransitionMeta{ActorPrincipalID: principal, Reason: "HIRE_TASK_STARTED", At: at})
		if err != nil {
			return err
		}
		claimed.ClaimID = &claimID
		started.ClaimID = &claimID
		var submitErr error
		completed, submission, submitErr = stepstask.Submit(context.Background(), tx, store, started, stepstask.SubmitInput{
			Node: node,
			Spec: stepstask.SubmissionSpec{
				CompletedBy: principal, CandidateVia: humanwork.SourceDirect,
				ClaimID: claimID, ClaimExpiresAt: values.NewInstant(claimExpires), SubmittedAt: values.NewInstant(at),
				OutputSchema: node.OutputSchema, CanonicalPayloadDigest: "sha256:" + strings.Repeat("c", 64),
				FormDefinition: node.FormDefinition, RenderContextDigest: "sha256:" + strings.Repeat("d", 64),
				ValidationEvidenceRef: "evidence.validation.new_hire/v1", AccessibilityEvidenceRef: "evidence.accessibility.new_hire/v1",
				AccommodationEvidenceRef: "evidence.accommodation.new_hire/v1",
			},
			Validator: stepstask.ValidatorFunc(func(stepstask.ValidationRequest) error { return nil }),
			Now:       at, Meta: workitem.TransitionMeta{ActorPrincipalID: principal, Reason: "HIRE_TASK_SUBMITTED", At: at},
		})
		return submitErr
	})
	return completed, submission
}

// hireCancelTask withdraws a TASK work item without submitting it -- HR's
// decision, on an ADVERSE background check, that the hire does not continue.
func hireCancelTask(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, item workitem.WorkItem, principal string, at time.Time) workitem.WorkItem {
	t.Helper()
	var cancelled workitem.WorkItem
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		var err error
		cancelled, err = (workitem.Store{}).Cancel(context.Background(), tx, tenantID, item.WorkItemID, item.ItemVersion,
			workitem.TransitionMeta{ActorPrincipalID: principal, Reason: "HIRE_OFFER_WITHDRAWN", At: at})
		return err
	})
	return cancelled
}

func hireTaskOutcome(t *testing.T, node stepstask.CompiledTaskNode, item workitem.WorkItem, submission *stepstask.Submission, at time.Time) frontier.NodeOutcome {
	t.Helper()
	continuation, err := stepstask.NewContinuation(item.WorkflowInstanceID, node, item)
	if err != nil {
		t.Fatalf("steps/task.NewContinuation: %v", err)
	}
	always := stepstask.ValidatorFunc(func(stepstask.ValidationRequest) error { return nil })
	resolution, err := stepstask.Resolve(continuation, item, submission, always, values.NewInstant(at), stepstask.Event{})
	if err != nil {
		t.Fatalf("steps/task.Resolve: %v", err)
	}
	return resolution.ToNodeOutcome(node.NodeID)
}

// hireDeliverBackgroundCheckSignal receives the background-check provider's
// callback against the run's open await_background_check subscription,
// correlated on the proposal's own intent id (WF-EXT-014), and resumes the
// driver from the matched receipt.
func hireDeliverBackgroundCheckSignal(t *testing.T, f hireFixture, instanceID uuid.UUID, expectedInstanceVersion int64, at time.Time) execute.Result {
	t.Helper()
	ctx := context.Background()
	var sub signals.OpenSubscription
	var receipt signals.Receipt
	inTenantTx(t, f.db, f.tenantID, func(tx dbport.Tx) error {
		var err error
		sub, err = (signals.Store{}).OpenSubscriptionForCorrelation(ctx, tx, f.tenantID, hireexec.NodeAwaitBackgroundCheck, f.proposal.IntentID)
		if err != nil {
			return fmt.Errorf("open background-check wait: %w", err)
		}
		receipt, err = (signals.Store{}).Receive(ctx, tx, signals.ReceiveRequest{
			Signal: stepSignal.Signal{
				Tenant: values.TenantId(f.tenantID.String()), Source: "hcmnext.integrations.background_check",
				EventType: sub.EventType, SchemaRef: sub.ExpectedSchemaRef,
				CorrelationKey: sub.CorrelationKey, CorrelationValue: sub.CorrelationValue,
				IdempotencyKey: "test:background-check:" + f.key,
				Payload:        []byte(`{"intent_id":"` + f.proposal.IntentID + `"}`),
				ReceivedAt:     values.NewInstant(at),
			},
			ReceivedAt: at,
		}, acceptingSignalVerifier{})
		return err
	})
	accepted := false
	for _, disposition := range receipt.Dispositions {
		if disposition.SubscriptionID == sub.ID && disposition.Status == stepSignal.StatusAccepted {
			accepted = true
		}
	}
	if !accepted {
		t.Fatalf("background-check receipt = %+v, want one ACCEPTED disposition for the open wait", receipt)
	}
	result, err := f.driver(t, at).ResumeSignal(ctx, execute.ResumeSignalRequest{
		Start: f.start, InstanceID: instanceID, ExpectedInstanceVersion: expectedInstanceVersion,
		SignalID: receipt.SignalID, SubscriptionID: sub.ID, RecordedAt: at,
	})
	if err != nil {
		t.Fatalf("ResumeSignal: %v", err)
	}
	return result
}

// hireFireStartDateTimerAndComplete settles await_start_date's durable
// promise under a lease this call acquires for itself, then resumes the
// driver from the fired timer through to COMPLETE (commit_hire's SUCCEEDED
// route ends the run at end_hired with nothing left to park on).
func hireFireStartDateTimerAndComplete(t *testing.T, f hireFixture, instanceID uuid.UUID, parkedWait execute.Result, at time.Time) execute.Result {
	t.Helper()
	ctx := context.Background()

	holder := lease.Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica:hire-" + f.key}
	queue := lease.Resource{Kind: lease.ResourceQueue, ID: "queue:workflow-runtime"}
	var manager lease.Manager
	var grant lease.Grant
	inTenantTx(t, f.db, f.tenantID, func(tx dbport.Tx) error {
		var err error
		grant, err = manager.Acquire(ctx, tx, lease.AcquireRequest{TenantID: f.tenantID, Resource: queue, Holder: holder, Now: at, TTL: time.Hour})
		return err
	})

	scheduler := timer.Scheduler{}
	var fired timer.FireResult
	inTenantTx(t, f.db, f.tenantID, func(tx dbport.Tx) error {
		var err error
		fired, err = scheduler.Fire(ctx, tx, timer.FireRequest{
			TenantID: f.tenantID, Now: at, Fence: grant.Fence,
			Misfire: schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: time.Hour},
		})
		return err
	})
	if len(fired.Fired) != 1 || fired.Fired[0].Timer.TimerID != parkedWait.Timers[0].TimerID {
		t.Fatalf("fire result = %+v, want the start-date promise settled", fired)
	}

	waitNode, ok := f.plan.Node(hireexec.NodeAwaitStartDate)
	if !ok {
		t.Fatal("plan carries no await_start_date node")
	}
	requirement, err := hireComputeWaitRequirement(f.plan, waitNode, f.proposal, hireWaitDataset)
	if err != nil {
		t.Fatalf("compute wait requirement: %v", err)
	}
	resolution, err := stepswait.Resolve(requirement, values.NewInstant(at), stepswait.WakeEvent{Kind: stepswait.EventWake})
	if err != nil {
		t.Fatalf("wait.Resolve: %v", err)
	}
	if resolution.Outcome != stepswait.OutcomeFired {
		t.Fatalf("wait resolution = %q, want FIRED", resolution.Outcome)
	}
	waitOutcome := resolution.ToNodeOutcome(hireexec.NodeAwaitStartDate)
	waitOutcome.OutputDigest = "sha256:" + requirement.Digest[:32] + requirement.Digest[:32]

	completed, err := f.driver(t, at.Add(time.Minute)).ResumeTimer(ctx, execute.ResumeTimerRequest{
		Start: f.start, InstanceID: instanceID, ExpectedInstanceVersion: parkedWait.InstanceVersion,
		TimerID: parkedWait.Timers[0].TimerID, Outcome: waitOutcome, RecordedAt: at.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("ResumeTimer: %v", err)
	}
	return completed
}

func loadHireInstance(t *testing.T, f hireFixture, instanceID uuid.UUID) runtime.Instance {
	t.Helper()
	var instance runtime.Instance
	inTenantTx(t, f.db, f.tenantID, func(tx dbport.Tx) error {
		var err error
		instance, err = (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenantID, instanceID)
		return err
	})
	return instance
}

// hireDriveToWait drives a CLEAR-background-check run from Execute through
// the hiring-manager approval, the background-check signal and all four
// sequential provisioning tasks, leaving it parked on await_start_date. Every
// call builds its own fresh driver (hireFixture.driver), so the approval's
// resume and the tasks' resumes already happen on driver instances that share
// no Go-level state with whichever one parked the run there.
func hireDriveToWait(t *testing.T, f hireFixture) (parkedApproval execute.Result, parkedWait execute.Result) {
	t.Helper()
	ctx := context.Background()

	first, err := f.driver(t, f.at).Execute(ctx, execute.ExecuteRequest{Start: f.start})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if first.Status != execute.StatusParked || len(first.WorkItems) != 1 {
		t.Fatalf("Execute = %+v, want PARKED with the hiring-manager approval", first)
	}
	approvalItem := first.WorkItems[0]
	if approvalItem.Kind != workitem.KindApproval || approvalItem.NodeID != hireexec.NodeApproveOffer {
		t.Fatalf("parked work item = %+v, want the offer approval for %s", approvalItem, hireexec.NodeApproveOffer)
	}

	completedApproval := hireCompleteApproval(t, f.db, f.tenantID, approvalItem, f.managerReq, f.proposal, humanwork.PrincipalManager, f.at.Add(time.Minute), intentapproval.OutcomeApproved)
	approvalOut := promotionApprovalOutcome(t, completedApproval, hireexec.NodeApproveOffer, f.managerReq, f.proposal, f.at.Add(time.Minute), f.at.Add(2*time.Minute), intentapproval.OutcomeApproved)
	afterApproval, err := f.driver(t, f.at.Add(2*time.Minute)).Resume(ctx, execute.ResumeRequest{
		Start: f.start, InstanceID: first.Start.InstanceID, ExpectedInstanceVersion: first.InstanceVersion,
		WorkItemID: completedApproval.WorkItemID, ExpectedWorkItemVersion: completedApproval.ItemVersion,
		Outcome: approvalOut, RecordedAt: f.at.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Resume (approval): %v", err)
	}
	if afterApproval.Status != execute.StatusParked {
		t.Fatalf("Resume (approval) = %+v, want PARKED on the background-check signal", afterApproval)
	}

	afterSignal := hireDeliverBackgroundCheckSignal(t, f, first.Start.InstanceID, afterApproval.InstanceVersion, f.at.Add(10*time.Minute))
	if afterSignal.Status != execute.StatusParked || len(afterSignal.WorkItems) != 1 {
		t.Fatalf("ResumeSignal = %+v, want PARKED with one work item", afterSignal)
	}
	if afterSignal.WorkItems[0].NodeID != hireexec.NodeCollectNewHireForms {
		t.Fatalf("parked work item = %+v, want %s", afterSignal.WorkItems[0], hireexec.NodeCollectNewHireForms)
	}

	current := afterSignal
	at := f.at.Add(15 * time.Minute)
	for _, nodeID := range []string{
		hireexec.NodeCollectNewHireForms, hireexec.NodeProvisionITAccess, hireexec.NodeProvisionWorkspace, hireexec.NodeEnrollPayroll,
	} {
		workType, principal, ok := hireTaskSpec(nodeID)
		if !ok {
			t.Fatalf("no task spec for %s", nodeID)
		}
		item := current.WorkItems[0]
		if item.NodeID != nodeID {
			t.Fatalf("parked node = %s, want %s", item.NodeID, nodeID)
		}
		node := hireTaskNode(f.plan, nodeID, workType)
		completed, submission := hireSubmitTask(t, f.db, f.tenantID, node, item, principal, at)
		outcome := hireTaskOutcome(t, node, completed, &submission, at.Add(time.Minute))
		next, err := f.driver(t, at.Add(2*time.Minute)).Resume(ctx, execute.ResumeRequest{
			Start: f.start, InstanceID: first.Start.InstanceID, ExpectedInstanceVersion: current.InstanceVersion,
			WorkItemID: completed.WorkItemID, ExpectedWorkItemVersion: completed.ItemVersion,
			Outcome: outcome, RecordedAt: at.Add(2 * time.Minute),
		})
		if err != nil {
			t.Fatalf("Resume (%s): %v", nodeID, err)
		}
		current = next
		at = at.Add(20 * time.Minute)
	}

	if current.Status != execute.StatusParked || len(current.Timers) != 1 || current.Timers[0].NodeID != hireexec.NodeAwaitStartDate {
		t.Fatalf("after provisioning = %+v, want PARKED on the start-date timer", current)
	}
	current.Start = first.Start
	return first, current
}

// ---------------------------------------------------------------------------
// TestTodo_WF_HIRE_001_Integration
// ---------------------------------------------------------------------------

func TestTodo_WF_HIRE_001_Integration(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	f := newHireFixture(t, "hire-int-1", hireexec.RouteBackgroundCheckClear, at)

	_, parkedWait := hireDriveToWait(t, f)
	completed := hireFireStartDateTimerAndComplete(t, f, parkedWait.Start.InstanceID, parkedWait, f.startAt)
	if completed.Status != execute.StatusComplete {
		t.Fatalf("final result = %+v, want COMPLETE", completed)
	}
	last := completed.Advances[len(completed.Advances)-1]
	if !last.Complete || last.TerminalCode != hireexec.TerminalHired {
		t.Fatalf("terminal advancement = %+v, want %s", last, hireexec.TerminalHired)
	}

	instance := loadHireInstance(t, f, parkedWait.Start.InstanceID)
	if instance.RuntimeStatus != runtime.InstanceCompleted {
		t.Fatalf("instance status = %s, want COMPLETED", instance.RuntimeStatus)
	}

	if n := countRows(t, f.db, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, f.tenantID); n != 1 {
		t.Fatalf("ledger events = %d, want exactly 1", n)
	}
	if n := countRows(t, f.db, `SELECT count(*) FROM work_item WHERE tenant_id = $1 AND workflow_instance_id = $2`,
		f.tenantID, parkedWait.Start.InstanceID); n != 5 {
		t.Fatalf("work items = %d, want exactly 5 (one per human step)", n)
	}

	requester := f.proposal.CreatedBy.PrincipalID
	owners := map[string]string{
		humanwork.PrincipalManager: notifyplan.PurposeApproval,
		hirePrincipalCandidate:     notifyplan.PurposeTask,
		hirePrincipalIT:            notifyplan.PurposeTask,
		hirePrincipalFacilities:    notifyplan.PurposeTask,
		hirePrincipalPayroll:       notifyplan.PurposeTask,
	}
	inTenantTx(t, f.db, f.tenantID, func(tx dbport.Tx) error {
		for owner, purpose := range owners {
			notices, err := (inbox.Store{}).WorkflowNotices(ctx, tx, f.tenantID, owner, 20)
			if err != nil {
				return err
			}
			if len(notices) != 1 || notices[0].Purpose != purpose {
				t.Errorf("notices for %s = %+v, want exactly one %s notice", owner, notices, purpose)
			}
		}
		statuses, err := (inbox.Store{}).WorkflowStatusNotices(ctx, tx, f.tenantID, requester, 20)
		if err != nil {
			return err
		}
		// One "sent for review" per routed step the requester does not own,
		// then one FINISHED, newest first: the requester hears about every
		// hand-off and about the end, and about nothing twice.
		inReview := 0
		for _, status := range statuses[1:] {
			if status.Event == inbox.StatusInReview {
				inReview++
			}
		}
		if len(statuses) != len(owners)+1 || statuses[0].Event != inbox.StatusFinished || inReview != len(owners) {
			t.Errorf("requester statuses = %+v, want %d IN_REVIEW then one FINISHED", statuses, len(owners))
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// TestTodo_WF_HIRE_001_Recovery
// ---------------------------------------------------------------------------

// TestTodo_WF_HIRE_001_Recovery proves the run survives being driven by a new
// execute.Driver at every step, including right after the approval parks and
// right after the WAIT parks: hireDriveToWait and
// hireFireStartDateTimerAndComplete each call hireFixture.driver fresh for
// every single Execute/Resume/ResumeSignal/ResumeTimer, so nothing but the
// durable rows a driver committed ever crosses from one call to the next.
func TestTodo_WF_HIRE_001_Recovery(t *testing.T) {
	at := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	f := newHireFixture(t, "hire-recovery-1", hireexec.RouteBackgroundCheckClear, at)

	_, parkedWait := hireDriveToWait(t, f)
	completed := hireFireStartDateTimerAndComplete(t, f, parkedWait.Start.InstanceID, parkedWait, f.startAt)
	if completed.Status != execute.StatusComplete {
		t.Fatalf("final result = %+v, want COMPLETE", completed)
	}
	instance := loadHireInstance(t, f, parkedWait.Start.InstanceID)
	if instance.RuntimeStatus != runtime.InstanceCompleted {
		t.Fatalf("instance status = %s, want COMPLETED", instance.RuntimeStatus)
	}
}

// ---------------------------------------------------------------------------
// TestTodo_WF_HIRE_001_Conformance
// ---------------------------------------------------------------------------

func TestTodo_WF_HIRE_001_Conformance(t *testing.T) {
	t.Run("hiring manager rejects the offer", func(t *testing.T) {
		ctx := context.Background()
		at := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
		f := newHireFixture(t, "hire-conf-rejected", hireexec.RouteBackgroundCheckClear, at)

		first, err := f.driver(t, f.at).Execute(ctx, execute.ExecuteRequest{Start: f.start})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if first.Status != execute.StatusParked || len(first.WorkItems) != 1 {
			t.Fatalf("Execute = %+v, want PARKED on the offer approval", first)
		}
		approvalItem := first.WorkItems[0]
		rejected := hireCompleteApproval(t, f.db, f.tenantID, approvalItem, f.managerReq, f.proposal, humanwork.PrincipalManager, f.at.Add(time.Minute), intentapproval.OutcomeRejected)
		outcome := promotionApprovalOutcome(t, rejected, hireexec.NodeApproveOffer, f.managerReq, f.proposal, f.at.Add(time.Minute), f.at.Add(2*time.Minute), intentapproval.OutcomeRejected)
		result, err := f.driver(t, f.at.Add(2*time.Minute)).Resume(ctx, execute.ResumeRequest{
			Start: f.start, InstanceID: first.Start.InstanceID, ExpectedInstanceVersion: first.InstanceVersion,
			WorkItemID: rejected.WorkItemID, ExpectedWorkItemVersion: rejected.ItemVersion,
			Outcome: outcome, RecordedAt: f.at.Add(2 * time.Minute),
		})
		if err != nil {
			t.Fatalf("Resume: %v", err)
		}
		if result.Status != execute.StatusComplete {
			t.Fatalf("rejection result = %+v, want COMPLETE", result)
		}
		last := result.Advances[len(result.Advances)-1]
		if !last.Complete || last.TerminalCode != hireexec.TerminalOfferRejected {
			t.Fatalf("terminal advancement = %+v, want %s", last, hireexec.TerminalOfferRejected)
		}
		if n := countRows(t, f.db, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, f.tenantID); n != 0 {
			t.Fatalf("ledger events = %d, want 0", n)
		}
		if n := countRows(t, f.db, `SELECT count(*) FROM work_item WHERE tenant_id = $1 AND workflow_instance_id = $2`,
			f.tenantID, first.Start.InstanceID); n != 1 {
			t.Fatalf("work items = %d, want exactly the one approval (no task ever created)", n)
		}
	})

	t.Run("background check comes back adverse and HR withdraws", func(t *testing.T) {
		ctx := context.Background()
		at := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
		f := newHireFixture(t, "hire-conf-withdrawn", hireexec.RouteBackgroundCheckAdverse, at)

		first, err := f.driver(t, f.at).Execute(ctx, execute.ExecuteRequest{Start: f.start})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if first.Status != execute.StatusParked || len(first.WorkItems) != 1 {
			t.Fatalf("Execute = %+v, want PARKED on the offer approval", first)
		}
		approvalItem := first.WorkItems[0]
		completedApproval := hireCompleteApproval(t, f.db, f.tenantID, approvalItem, f.managerReq, f.proposal, humanwork.PrincipalManager, f.at.Add(time.Minute), intentapproval.OutcomeApproved)
		approvalOut := promotionApprovalOutcome(t, completedApproval, hireexec.NodeApproveOffer, f.managerReq, f.proposal, f.at.Add(time.Minute), f.at.Add(2*time.Minute), intentapproval.OutcomeApproved)
		afterApproval, err := f.driver(t, f.at.Add(2*time.Minute)).Resume(ctx, execute.ResumeRequest{
			Start: f.start, InstanceID: first.Start.InstanceID, ExpectedInstanceVersion: first.InstanceVersion,
			WorkItemID: completedApproval.WorkItemID, ExpectedWorkItemVersion: completedApproval.ItemVersion,
			Outcome: approvalOut, RecordedAt: f.at.Add(2 * time.Minute),
		})
		if err != nil {
			t.Fatalf("Resume (approval): %v", err)
		}
		if afterApproval.Status != execute.StatusParked {
			t.Fatalf("Resume (approval) = %+v, want PARKED on the background-check signal", afterApproval)
		}

		afterSignal := hireDeliverBackgroundCheckSignal(t, f, first.Start.InstanceID, afterApproval.InstanceVersion, f.at.Add(10*time.Minute))
		if afterSignal.Status != execute.StatusParked || len(afterSignal.WorkItems) != 1 {
			t.Fatalf("ResumeSignal = %+v, want PARKED on HR's adverse-result review", afterSignal)
		}
		reviewItem := afterSignal.WorkItems[0]
		if reviewItem.NodeID != hireexec.NodeReviewAdverseResult || reviewItem.Kind != workitem.KindTask {
			t.Fatalf("parked work item = %+v, want the adverse-result review task", reviewItem)
		}

		withdrawnAt := f.at.Add(20 * time.Minute)
		cancelled := hireCancelTask(t, f.db, f.tenantID, reviewItem, humanwork.PrincipalHRBP, withdrawnAt)
		if cancelled.Status != workitem.StatusCancelled {
			t.Fatalf("cancelled review item status = %s, want CANCELLED", cancelled.Status)
		}
		workType, _, _ := hireTaskSpec(hireexec.NodeReviewAdverseResult)
		node := hireTaskNode(f.plan, hireexec.NodeReviewAdverseResult, workType)
		outcome := hireTaskOutcome(t, node, cancelled, nil, withdrawnAt.Add(time.Minute))
		if outcome.Outcome != workflow.Outcome("CANCELLED") {
			t.Fatalf("review outcome = %q, want CANCELLED", outcome.Outcome)
		}
		result, err := f.driver(t, withdrawnAt.Add(2*time.Minute)).Resume(ctx, execute.ResumeRequest{
			Start: f.start, InstanceID: first.Start.InstanceID, ExpectedInstanceVersion: afterSignal.InstanceVersion,
			WorkItemID: cancelled.WorkItemID, ExpectedWorkItemVersion: cancelled.ItemVersion,
			Outcome: outcome, RecordedAt: withdrawnAt.Add(2 * time.Minute),
		})
		if err != nil {
			t.Fatalf("Resume (withdraw): %v", err)
		}
		if result.Status != execute.StatusComplete {
			t.Fatalf("withdrawal result = %+v, want COMPLETE", result)
		}
		last := result.Advances[len(result.Advances)-1]
		if !last.Complete || last.TerminalCode != hireexec.TerminalOfferWithdrawn {
			t.Fatalf("terminal advancement = %+v, want %s", last, hireexec.TerminalOfferWithdrawn)
		}
		if n := countRows(t, f.db, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, f.tenantID); n != 0 {
			t.Fatalf("ledger events = %d, want 0", n)
		}
	})
}
