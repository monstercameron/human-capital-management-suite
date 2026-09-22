package execute_test

// WF-EXT-004: each node's typed output is stored as an immutable artifact
// bound to its attempt; the run's typed input document is stored once at
// start; the driver resolves WORKFLOW_INPUT, NODE_OUTPUT and CONSTANT
// mappings before dispatch and hands them to the step runner; RECOVERY
// proves a resumed run (a fresh execute.New driver) reads the same
// artifacts; PROPERTY proves a mapping never reads an output from a node
// that does not dominate it.

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepsapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// ---------------------------------------------------------------------------
// The compiled plan: compute (TRANSFORM, start) -> review (APPROVAL, parks)
// -> apply (TRANSFORM whose mappings read compute.score, workflow input
// worker_id, and a CONSTANT) -> end.
// ---------------------------------------------------------------------------

const (
	wfext004WorkflowID  = "hcmnext.workflows.test.wf_ext_004_demo"
	wfext004NodeCompute = "compute"
	wfext004NodeReview  = "review"
	wfext004NodeApply   = "apply"
	wfext004NodeEnd     = "end_applied"
	wfext004NodeReject  = "end_rejected"

	wfext004ApprovalRequirementID = "approval.test.wf_ext_004/v1"
	wfext004TerminalCode          = "WF_EXT_004_APPLIED"
	wfext004OrgScope              = "org:test/wfext004"
)

func wfext004Schema(name string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID: "hcmnext.workflows.test.wfext004." + name + "/v1", Version: 1,
		ProtobufFullName: "hcmnext.workflow.test.wfext004." + name,
	}
}

func wfext004BrandedString(brand string) workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: brand}
}

func wfext004TransformSpec(ref string) *workflow.TransformSpec {
	return &workflow.TransformSpec{
		TransformRef: ref, Version: 1, NormalizationProfile: "profile.test.wfext004/v1",
		OutputTaint: workflow.TaintDerived,
		Limits: workflow.TransformLimits{
			MaxInputBytes: workflow.MaxTransformInputBytes, MaxOutputBytes: workflow.MaxTransformOutputBytes,
			MaxSteps: workflow.MaxTransformSteps,
		},
	}
}

func wfext004Governance(purpose string) workflow.NodeGovernance {
	return workflow.NodeGovernance{
		Purpose: purpose, Classification: "CONFIDENTIAL_HR",
		RevalidationBoundary: workflow.RevalidatePreExecution, DataAccessManifestRef: "data-access.test.wfext004/v1",
	}
}

func wfext004TerminalFields() []workflow.Field {
	return []workflow.Field{
		{Path: "worker_id", Type: wfext004BrandedString("WorkerID")},
		{Path: "terminal_code", Type: workflow.ValueType{Kind: workflow.KindString}},
	}
}

func wfext004Completion(request, execution, business, consistency, obligation string) map[string]string {
	return map[string]string{
		"RequestState": request, "ExecutionState": execution, "BusinessState": business,
		"ConsistencyState": consistency, "ObligationState": obligation,
	}
}

func wfext004TerminalNode(id, code string, status workflow.RuntimeStatus, dims map[string]string, commitReceiptRef string) workflow.Node {
	return workflow.Node{
		ID: id, Type: workflow.StepEnd, Inputs: wfext004TerminalFields(),
		InputMappings: []workflow.Mapping{
			{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
			{Target: "terminal_code", Source: workflow.Source{
				Kind: workflow.SourceConstant, Constant: code, Type: workflow.ValueType{Kind: workflow.KindString},
			}},
		},
		Governance: workflow.NodeGovernance{
			Purpose: "WF_EXT_004_DEMO", Classification: "CONFIDENTIAL_HR",
			RevalidationBoundary: workflow.RevalidatePreClosure, DataAccessManifestRef: "data-access.test.wfext004/v1",
		},
		End: &workflow.EndSpec{TerminalCode: code, RuntimeStatus: status, CompletionMapping: dims, CommitReceiptRef: commitReceiptRef},
	}
}

func wfext004Definition() workflow.Definition {
	return workflow.Definition{
		WorkflowID: wfext004WorkflowID, Version: 1, Name: "WF-EXT-004 demo",
		InputSchema: wfext004Schema("Input"), OutputSchema: wfext004Schema("Result"), VariablesSchema: wfext004Schema("Variables"),
		TenantScope: "test", OrganizationScope: wfext004OrgScope, RiskClass: "MEDIUM",
		DeclaredModes: []workflow.ExecutionMode{workflow.ModeExecute}, TerminalProfile: workflow.TerminalProfileExecute,
		StartNodeID: wfext004NodeCompute,
		Inputs:      []workflow.Field{{Path: "worker_id", Type: wfext004BrandedString("WorkerID")}},
		Outputs:     wfext004TerminalFields(),
		ApprovalRequirements: []workflow.ApprovalRequirement{{
			ID: wfext004ApprovalRequirementID, ResolverExpression: "CurrentManagerOf(worker)",
			Scope: wfext004OrgScope, Quorum: 1, SeparationOfDuties: true,
			EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND",
		}},
		Limits:                workflow.Limits{MaxFanOut: 5, MaxDepth: 3, MaxNodes: 8},
		FailurePolicyRef:      "policy.workflow.failure.test/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.test/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.test/v1",
		Nodes: []workflow.Node{
			{
				ID: wfext004NodeCompute, Type: workflow.StepTransform,
				InputSchema: wfext004Schema("ComputeInput"), OutputSchema: wfext004Schema("ComputeResult"),
				Outputs: []workflow.Field{
					{Path: "score", Type: workflow.ValueType{Kind: workflow.KindDecimal}},
					{Path: "label", Type: workflow.ValueType{Kind: workflow.KindString}},
				},
				Transform:      wfext004TransformSpec("transform.test.wfext004.compute/v1"),
				DeclaredEffect: capability.EffectPure, Governance: wfext004Governance("WF_EXT_004_COMPUTE"),
			},
			{
				ID: wfext004NodeReview, Type: workflow.StepApproval,
				InputSchema: wfext004Schema("ReviewInput"), OutputSchema: wfext004Schema("ReviewResult"),
				Inputs: []workflow.Field{{Path: "worker_id", Type: wfext004BrandedString("WorkerID")}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
				},
				DeclaredEffect: capability.EffectPure,
				Governance: workflow.NodeGovernance{
					Purpose: "WF_EXT_004_REVIEW", Classification: "CONFIDENTIAL_HR",
					ApprovalRequirements: []string{wfext004ApprovalRequirementID},
					RevalidationBoundary: workflow.RevalidatePreExecution, DataAccessManifestRef: "data-access.test.wfext004/v1",
				},
			},
			{
				ID: wfext004NodeApply, Type: workflow.StepTransform,
				InputSchema: wfext004Schema("ApplyInput"), OutputSchema: wfext004Schema("ApplyResult"),
				Inputs: []workflow.Field{
					{Path: "score", Type: workflow.ValueType{Kind: workflow.KindDecimal}},
					{Path: "worker_id", Type: wfext004BrandedString("WorkerID")},
					{Path: "kind", Type: workflow.ValueType{Kind: workflow.KindString}},
				},
				InputMappings: []workflow.Mapping{
					{Target: "score", Source: workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: wfext004NodeCompute, Path: "score"}},
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
					{Target: "kind", Source: workflow.Source{
						Kind: workflow.SourceConstant, Constant: "PROMOTION", Type: workflow.ValueType{Kind: workflow.KindString},
					}},
				},
				Transform:      wfext004TransformSpec("transform.test.wfext004.apply/v1"),
				DeclaredEffect: capability.EffectPure, Governance: wfext004Governance("WF_EXT_004_APPLY"),
			},
			wfext004TerminalNode(wfext004NodeEnd, wfext004TerminalCode, workflow.RuntimeCompleted,
				wfext004Completion("CLOSED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"), "receipt.test.wfext004/v1"),
			wfext004TerminalNode(wfext004NodeReject, "REJECTED", workflow.RuntimeCompleted,
				wfext004Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), ""),
		},
		Edges: []workflow.Edge{
			{From: wfext004NodeCompute, To: wfext004NodeReview, RouteKey: "SUCCEEDED"},
			{From: wfext004NodeCompute, To: wfext004NodeReject, RouteKey: "FAILED"},
			{From: wfext004NodeReview, To: wfext004NodeApply, RouteKey: "APPROVED"},
			{From: wfext004NodeReview, To: wfext004NodeReject, RouteKey: "REJECTED"},
			{From: wfext004NodeReview, To: wfext004NodeReject, RouteKey: "CANCELLED"},
			{From: wfext004NodeReview, To: wfext004NodeReject, RouteKey: "EXPIRED"},
			{From: wfext004NodeReview, To: wfext004NodeReject, RouteKey: "INVALIDATED"},
			{From: wfext004NodeApply, To: wfext004NodeEnd, RouteKey: "SUCCEEDED"},
			{From: wfext004NodeApply, To: wfext004NodeReject, RouteKey: "FAILED"},
		},
	}
}

func wfext004CompilePlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := workflow.Compile(wfext004Definition(), workflow.Options{Phase: workflow.PhaseP1B})
	if err != nil {
		t.Fatalf("compile wfext004 demo definition: %v", err)
	}
	return plan
}

// ---------------------------------------------------------------------------
// StepRunner: compute produces typed outputs; apply records the typed
// inputs it received so the test can assert on them; end asserts nothing.
// ---------------------------------------------------------------------------

type wfext004Steps struct {
	applyCalled bool
	applyInputs map[string]workflow.TypedValue
}

func (s *wfext004Steps) Run(_ context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	switch req.Node.ID {
	case wfext004NodeCompute:
		return frontier.NodeOutcome{
			NodeID: req.Node.ID, Outcome: workflow.OutcomeSucceeded,
			Outputs: &workflow.OutputDocument{Values: []workflow.TypedOutput{
				{Path: "score", Value: workflow.TypedValue{Type: workflow.ValueType{Kind: workflow.KindDecimal}, Text: "87.5000"}},
				{Path: "label", Value: workflow.TypedValue{Type: workflow.ValueType{Kind: workflow.KindString}, Text: "STRONG"}},
			}},
		}, runtime.GovernanceRefs{}, nil
	case wfext004NodeApply:
		s.applyCalled = true
		s.applyInputs = req.Inputs
		return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: workflow.OutcomeSucceeded}, runtime.GovernanceRefs{}, nil
	case wfext004NodeEnd, wfext004NodeReject:
		return frontier.NodeOutcome{NodeID: req.Node.ID}, runtime.GovernanceRefs{}, nil
	default:
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("wfext004: unexpected READY node %s", req.Node.ID)
	}
}

// ---------------------------------------------------------------------------
// WorkItemFactory: opens and routes the review APPROVAL work item through
// steps/approval.Open, exactly as test/workflow's own promotion fixture does.
// ---------------------------------------------------------------------------

type wfext004WorkItems struct {
	proposal    intent.ProposalRevision
	requirement humanwork.ApprovalRequirement
	resolution  humanwork.Resolution
}

func (f wfext004WorkItems) CreateAndRoute(ctx context.Context, ex workitem.Executor, req execute.WorkItemRequest) (workitem.WorkItem, error) {
	if req.Continuation.TargetNodeID != wfext004NodeReview {
		return workitem.WorkItem{}, fmt.Errorf("wfext004: no human-work spec bound for node %s", req.Continuation.TargetNodeID)
	}
	store := workitem.Store{}
	item, err := stepsapproval.Open(ctx, ex, store, stepsapproval.OpenInput{
		TenantID: req.Continuation.TenantID, WorkItemID: req.WorkItemID,
		WorkflowInstanceID: req.Continuation.InstanceID, CorrelationID: req.CorrelationID, SubjectRefs: req.SubjectRefs,
		Node: stepsapproval.CompiledApprovalNode{
			WorkflowID: wfext004WorkflowID, WorkflowVersion: 1, NodeID: wfext004NodeReview, WorkType: wfext004ApprovalRequirementID,
			PolicyRouteRef: "route.test.wfext004_approval/v1", Visibility: workitem.VisibilityAssigneeOnly,
			OrganizationScopeID: wfext004OrgScope,
		},
		Requirement: f.requirement, Proposal: f.proposal, Now: req.CreatedAt,
		Meta: workitem.TransitionMeta{ActorPrincipalID: "system:workflow-runtime", Reason: "WORKFLOW_APPROVAL_REQUIRED", At: req.CreatedAt},
	})
	if err != nil {
		return workitem.WorkItem{}, fmt.Errorf("steps/approval.Open: %w", err)
	}
	assignment := workitem.Assignment{
		Resolution: f.resolution, GovernancePolicyRef: "policy.test.wfext004_approval/v1",
		Trigger: workitem.TriggerInitialRouting, ChosenOwner: f.resolution.Candidates[0].PrincipalID,
	}
	return store.Route(ctx, ex, item.TenantID, item.WorkItemID, item.ItemVersion, assignment,
		workitem.TransitionMeta{ActorPrincipalID: "system:workflow-runtime", Reason: "ASSIGNMENT_RESOLVED", At: req.CreatedAt})
}

// ---------------------------------------------------------------------------
// Approval fixtures and helpers.
// ---------------------------------------------------------------------------

func wfext004Requirement(at time.Time) humanwork.ApprovalRequirement {
	return humanwork.ApprovalRequirement{
		RequirementID: wfext004ApprovalRequirementID, Revision: 1,
		Quorum: humanwork.Quorum{MinApprovals: 1, RequireDistinctPrincipals: true},
		Deadline: humanwork.Deadline{
			DecideBy: values.NewInstant(at.Add(time.Hour)), Expiry: values.NewInstant(at.Add(2 * time.Hour)),
		},
		ExpressionDigest: "sha256:" + strings.Repeat("c", 64),
		Source: humanwork.RequirementSource{
			TableID: "wfext004.threshold", TableVersion: "1",
			TableDigest: "sha256:" + strings.Repeat("d", 64), MatchedRowID: "manager",
			GovernancePolicyRef: "policy.test.wfext004_approval/v1",
		},
	}
}

func wfext004Resolution(req humanwork.ApprovalRequirement, at time.Time) humanwork.Resolution {
	return humanwork.Resolution{
		RequirementID: req.RequirementID, RequirementRevision: req.Revision,
		Outcome:    humanwork.OutcomeResolved,
		Candidates: []humanwork.Candidate{{PrincipalID: "principal:wfext004-manager", Via: humanwork.SourceDirect, TermRef: "role:manager"}},
		ResolvedAt: values.NewInstant(at), EffectiveAt: values.NewInstant(at),
		DirectoryVersion: "directory.test.wfext004/v1", ExpressionDigest: req.ExpressionDigest,
		RequirementDigest: req.Digest(), QuorumRequired: 1,
	}
}

func wfext004ApprovalDecision(item workitem.WorkItem, req humanwork.ApprovalRequirement, proposal intent.ProposalRevision, at time.Time) intentapproval.ApprovalDecision {
	return intentapproval.ApprovalDecision{
		DecisionID: "decision:wfext004-manager-1",
		Binding: intentapproval.DecisionBinding{
			RequirementID: req.RequirementID, RequirementRevision: req.Revision, RequirementDigest: req.Digest(),
			IntentID: proposal.IntentID, ProposalRevisionID: proposal.ProposalRevisionID, ProposalDigest: proposal.MaterialDigest,
			ControlSnapshots: proposal.ControlSnapshots, TaskVersion: 1,
			RenderedProjectionDigest:   "sha256:" + strings.Repeat("a", 64),
			ResolutionExpressionDigest: req.ExpressionDigest,
		},
		Outcome: intentapproval.OutcomeApproved,
		Approver: intentapproval.ApproverReference{
			PrincipalID: item.CompletedBy, IdentityAssuranceRef: "assurance.mfa_session/v1",
			SessionRef: "session:wfext004", Via: humanwork.SourceDirect,
		},
		AuthorityDecisionRef: "authz:decision:wfext004", Reason: "reason.promotion_supported/v1",
		DecidedAt: values.NewInstant(at),
	}
}

func wfext004ApprovalOutcome(t *testing.T, item workitem.WorkItem, req humanwork.ApprovalRequirement, proposal intent.ProposalRevision, decidedAt, now time.Time) frontier.NodeOutcome {
	t.Helper()
	decision := wfext004ApprovalDecision(item, req, proposal, decidedAt)
	continuation, err := stepsapproval.NewContinuation(item.WorkflowInstanceID, wfext004NodeReview, proposal,
		humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{req}}, []workitem.WorkItem{item})
	if err != nil {
		t.Fatalf("steps/approval.NewContinuation: %v", err)
	}
	resolution, err := stepsapproval.Resolve(continuation, []workitem.WorkItem{item}, []intentapproval.ApprovalDecision{decision},
		values.NewInstant(now), stepsapproval.Event{})
	if err != nil {
		t.Fatalf("steps/approval.Resolve: %v", err)
	}
	return resolution.ToNodeOutcome(wfext004NodeReview)
}

// ---------------------------------------------------------------------------
// pgtest plumbing.
// ---------------------------------------------------------------------------

func wfext004Tx(t *testing.T, conn dbport.Beginner, tenantID uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	if err := fn(tx); err != nil {
		t.Fatalf("transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// wfext004Fixture is everything a wfext004 driver needs, parked or not.
type wfext004Fixture struct {
	db          *pgtest.DB
	conn        dbport.Beginner
	tenantID    uuid.UUID
	plan        *workflow.CompiledWorkflow
	start       runtime.StartRequest
	execRequest execute.ExecuteRequest
	requirement humanwork.ApprovalRequirement
	resolution  humanwork.Resolution
	proposal    intent.ProposalRevision
	at          time.Time
}

func newWFEXT004Fixture(t *testing.T, key string) wfext004Fixture {
	t.Helper()
	db := pgtest.New(t)
	conn := appConn(t, db)
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', $4)`, tenantID, "wfext004-"+key, "wfext004-"+key, at.Add(-time.Hour))

	plan := wfext004CompilePlan(t)
	intentID, revisionID := "intent:wfext004-"+key, "proposal:wfext004-"+key+":1"
	proposal := intent.ProposalRevision{
		IntentID: intentID, ProposalRevisionID: revisionID, Revision: 1,
		CreatedBy:      intent.PrincipalReference{PrincipalID: "principal:wfext004-initiator", Kind: intent.InitiatorHuman},
		MaterialDigest: digestReference(revisionID, intentID),
		Subjects:       []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "employment:wfext004-" + key, AuthorityDomain: "PEOPLE"}},
	}
	resolver := effects.PolicyResolver{Entries: []effects.PolicyEntry{{
		WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: plan.Digest()}, Plan: plan,
	}}}
	start := runtime.StartRequest{
		TenantID: tenantID, CellID: "cell-local", StartIdempotencyKey: "start:wfext004-" + key,
		Resolver: resolver, Versions: promotionVersionStore{plan: plan},
		Proposal:      runtime.ProposalBinding{Revision: proposal, ApprovalRef: "decision:wfext004-start"},
		ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: approvedStartFacts(proposal),
		ExpectedIntentID: intentID, BusinessSubjectRefs: []string{"employment:wfext004-" + key},
		ExecutionMode: workflow.ModeExecute, CorrelationID: "corr:wfext004-" + key, CreatedAt: at,
	}
	execReq := execute.ExecuteRequest{
		Start: start,
		Inputs: []workflow.TypedOutput{
			{Path: "worker_id", Value: workflow.TypedValue{Type: wfext004BrandedString("WorkerID"), Text: "worker:wfext004-" + key}},
		},
	}
	req := wfext004Requirement(at)
	return wfext004Fixture{
		db: db, conn: conn, tenantID: tenantID, plan: plan, start: start, execRequest: execReq,
		requirement: req, resolution: wfext004Resolution(req, at), proposal: proposal, at: at,
	}
}

func wfext004Driver(t *testing.T, f wfext004Fixture, steps execute.StepRunner) *execute.Driver {
	t.Helper()
	registry, err := datalogger.NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatalf("ledger.NewLedgerEventDigestRegistry: %v", err)
	}
	terminal := &effects.LedgerTerminalWriter{
		Appender: datalogger.NewAppender(registry), ProjectionName: "wfext004_outcome", SourceRef: "hcmnext:test:wfext004",
	}
	drv, err := execute.New(execute.Options{
		DB: f.conn, Steps: steps, WorkItems: wfext004WorkItems{proposal: f.proposal, requirement: f.requirement, resolution: f.resolution},
		Terminal: terminal, Items: workitem.Store{},
		Guard: idempotency.PostgresStore{}, Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock: func() time.Time { return f.at },
	})
	if err != nil {
		t.Fatalf("execute.New: %v", err)
	}
	return drv
}

// wfext004ParkAtReview runs Execute and completes the review work item as the
// resolved manager, returning everything a caller needs to Resume from --
// but does not itself call Resume, so the primary and recovery tests can each
// resume through the driver of their choice.
func wfext004ParkAtReview(t *testing.T, f wfext004Fixture, steps execute.StepRunner) (execute.Result, workitem.WorkItem) {
	t.Helper()
	ctx := context.Background()
	drv := wfext004Driver(t, f, steps)
	parked, err := drv.Execute(ctx, f.execRequest)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if parked.Status != execute.StatusParked || len(parked.WorkItems) != 1 {
		t.Fatalf("Execute = %+v, want PARKED with one WorkItem", parked)
	}
	item := parked.WorkItems[0]
	if item.NodeID != wfext004NodeReview {
		t.Fatalf("parked work item = %+v, want the review APPROVAL work item", item)
	}

	store := workitem.Store{}
	var completed workitem.WorkItem
	owner := f.resolution.Candidates[0].PrincipalID
	wfext004Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		claimed, err := store.Claim(ctx, tx, workitem.ClaimInput{
			TenantID: f.tenantID, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
			ClaimantPrincipalID: owner, ClaimExpiresAt: f.at.Add(time.Hour), Now: f.at,
			Meta: workitem.TransitionMeta{ActorPrincipalID: owner, Reason: "APPROVAL_CLAIMED", At: f.at},
		})
		if err != nil {
			return err
		}
		started, err := store.Start(ctx, tx, f.tenantID, item.WorkItemID, claimed.ItemVersion, f.at,
			workitem.TransitionMeta{ActorPrincipalID: owner, Reason: "APPROVAL_STARTED", At: f.at})
		if err != nil {
			return err
		}
		decision := wfext004ApprovalDecision(workitem.WorkItem{CompletedBy: owner}, f.requirement, f.proposal, f.at)
		var cerr error
		completed, cerr = stepsapproval.Complete(ctx, tx, store, started, decision, f.at,
			workitem.TransitionMeta{ActorPrincipalID: owner, Reason: "APPROVAL_DECIDED", At: f.at})
		return cerr
	})
	if completed.Status != workitem.StatusCompleted {
		t.Fatalf("completed review work item status = %s, want COMPLETED", completed.Status)
	}
	return parked, completed
}

// ---------------------------------------------------------------------------
// TestTodo_WF_EXT_004
// ---------------------------------------------------------------------------

func TestTodo_WF_EXT_004(t *testing.T) {
	ctx := context.Background()
	f := newWFEXT004Fixture(t, "primary")
	steps := &wfext004Steps{}
	parked, completed := wfext004ParkAtReview(t, f, steps)
	drv := wfext004Driver(t, f, steps)

	approvalOut := wfext004ApprovalOutcome(t, completed, f.requirement, f.proposal, f.at, f.at.Add(time.Minute))
	if approvalOut.Outcome != workflow.Outcome("APPROVED") {
		t.Fatalf("approval resolution outcome = %q, want APPROVED", approvalOut.Outcome)
	}
	final, err := drv.Resume(ctx, execute.ResumeRequest{
		Start: f.start, InstanceID: parked.Start.InstanceID, ExpectedInstanceVersion: parked.InstanceVersion,
		WorkItemID: completed.WorkItemID, ExpectedWorkItemVersion: completed.ItemVersion,
		Outcome: approvalOut, RecordedAt: f.at.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if final.Status != execute.StatusComplete {
		t.Fatalf("Resume result = %+v, want COMPLETE", final)
	}

	if !steps.applyCalled {
		t.Fatal("apply was never dispatched")
	}
	if len(steps.applyInputs) != 3 {
		t.Fatalf("apply inputs = %+v, want exactly 3 (score, worker_id, kind)", steps.applyInputs)
	}
	if v := steps.applyInputs["score"]; v.Text != "87.5000" || v.Type.Kind != workflow.KindDecimal {
		t.Errorf("apply score input = %+v, want DECIMAL 87.5000", v)
	}
	if v := steps.applyInputs["worker_id"]; v.Text != "worker:wfext004-primary" {
		t.Errorf("apply worker_id input = %+v, want worker:wfext004-primary", v)
	}
	if v := steps.applyInputs["kind"]; v.Text != "PROMOTION" {
		t.Errorf("apply kind input = %+v, want the constant PROMOTION", v)
	}

	instanceID := parked.Start.InstanceID
	var outputDigest, nodeExecRef string
	if err := f.db.Conn.QueryRow(ctx, `SELECT output_digest FROM workflow_node_output_artifact
		WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND attempt = 1`,
		f.tenantID, instanceID, wfext004NodeCompute).Scan(&outputDigest); err != nil {
		t.Fatalf("read compute output artifact: %v", err)
	}
	if err := f.db.Conn.QueryRow(ctx, `SELECT output_artifact_ref FROM workflow_node_execution
		WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND attempt = 1`,
		f.tenantID, instanceID, wfext004NodeCompute).Scan(&nodeExecRef); err != nil {
		t.Fatalf("read compute node execution: %v", err)
	}
	if outputDigest == "" || outputDigest != nodeExecRef {
		t.Fatalf("compute output artifact digest %q, want it to match the node execution's recorded output ref %q", outputDigest, nodeExecRef)
	}

	var inputRows int
	if err := f.db.Conn.QueryRow(ctx, `SELECT count(*) FROM workflow_input_artifact WHERE tenant_id = $1 AND instance_id = $2`,
		f.tenantID, instanceID).Scan(&inputRows); err != nil {
		t.Fatalf("count workflow input artifact rows: %v", err)
	}
	if inputRows != 1 {
		t.Fatalf("workflow input artifact rows = %d, want 1", inputRows)
	}
}

// ---------------------------------------------------------------------------
// TestTodo_WF_EXT_004_Recovery
// ---------------------------------------------------------------------------

func TestTodo_WF_EXT_004_Recovery(t *testing.T) {
	ctx := context.Background()
	f := newWFEXT004Fixture(t, "recovery")
	parked, completed := wfext004ParkAtReview(t, f, &wfext004Steps{})

	// A brand-new driver, over a brand-new runner value: apply's inputs must
	// still come from the durable artifacts compute recorded during Execute,
	// never from anything the first driver or runner held in memory.
	freshSteps := &wfext004Steps{}
	freshDriver := wfext004Driver(t, f, freshSteps)

	approvalOut := wfext004ApprovalOutcome(t, completed, f.requirement, f.proposal, f.at, f.at.Add(time.Minute))
	final, err := freshDriver.Resume(ctx, execute.ResumeRequest{
		Start: f.start, InstanceID: parked.Start.InstanceID, ExpectedInstanceVersion: parked.InstanceVersion,
		WorkItemID: completed.WorkItemID, ExpectedWorkItemVersion: completed.ItemVersion,
		Outcome: approvalOut, RecordedAt: f.at.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Resume through a fresh driver: %v", err)
	}
	if final.Status != execute.StatusComplete {
		t.Fatalf("Resume result = %+v, want COMPLETE", final)
	}
	if !freshSteps.applyCalled || len(freshSteps.applyInputs) != 3 {
		t.Fatalf("fresh runner's apply inputs = %+v, want exactly 3", freshSteps.applyInputs)
	}
	if v := freshSteps.applyInputs["score"]; v.Text != "87.5000" {
		t.Errorf("recovered apply score input = %+v, want 87.5000 (from the durable artifact, not memory)", v)
	}
	if v := freshSteps.applyInputs["worker_id"]; v.Text != "worker:wfext004-recovery" {
		t.Errorf("recovered apply worker_id input = %+v, want worker:wfext004-recovery", v)
	}

	t.Run("tamper is impossible through SQL", func(t *testing.T) {
		for _, stmt := range []string{
			`UPDATE workflow_node_output_artifact SET artifact = artifact WHERE tenant_id = $1`,
			`DELETE FROM workflow_node_output_artifact WHERE tenant_id = $1`,
			`UPDATE workflow_input_artifact SET artifact = artifact WHERE tenant_id = $1`,
			`DELETE FROM workflow_input_artifact WHERE tenant_id = $1`,
		} {
			if err := f.db.ExecErr(stmt, f.tenantID); err == nil {
				t.Errorf("%q succeeded against append-only evidence", stmt)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// TestTodo_WF_EXT_004_Property
// ---------------------------------------------------------------------------

// TestTodo_WF_EXT_004_Property_CompilerRefusesNonDominatingMapping proves the
// compiler refuses a mapping that reads a node which does not dominate the
// reader: NodeRaiseThreshold's DECISION branches either straight to
// NodeApproveManager (WITHIN_THRESHOLD) or through NodeApproveFinance first
// (ABOVE_THRESHOLD) -- so NodeApproveFinance never dominates NodeApproveManager,
// and a mapping on the latter reading the former's output is refused.
func TestTodo_WF_EXT_004_Property(t *testing.T) {
	t.Run("compiler refuses a mapping that reads a non-dominating branch", func(t *testing.T) {
		def := promotionexec.Definition()
		found := false
		for i := range def.Nodes {
			if def.Nodes[i].ID != promotionexec.NodeApproveManager {
				continue
			}
			found = true
			for j := range def.Nodes[i].InputMappings {
				if def.Nodes[i].InputMappings[j].Target == "worker_id" {
					def.Nodes[i].InputMappings[j].Source = workflow.Source{
						Kind: workflow.SourceNodeOutput, NodeID: promotionexec.NodeApproveFinance, Path: "worker_id",
					}
				}
			}
		}
		if !found {
			t.Fatal("promotionexec.Definition() no longer declares NodeApproveManager; fixture assumption broken")
		}
		plan, err := promotionexec.Compile(def)
		if plan != nil {
			t.Fatalf("expected no plan when a mapping violates dominance, got digest %s", plan.Digest())
		}
		var diags *workflow.Diagnostics
		if !errors.As(err, &diags) || !diags.Has(workflow.CodeSourceNotPredecessor) {
			t.Fatalf("compile error = %v, want *workflow.Diagnostics carrying %s", err, workflow.CodeSourceNotPredecessor)
		}
	})

	t.Run("ResolveMappings never returns a value for a node that has not produced", func(t *testing.T) {
		for _, seed := range []int64{1, 7, 42, 1009, 20260921} {
			seed := seed
			t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
				rng := rand.New(rand.NewSource(seed))
				nodeCount := 2 + rng.Intn(5) // 2..6 nodes
				ids := make([]string, nodeCount)
				for i := range ids {
					ids[i] = fmt.Sprintf("n%d", i)
				}
				// producedUpTo simulates a run that has, so far, produced
				// outputs only for a random prefix of the chain -- so any
				// mapping reading a node at or beyond that boundary is a
				// mapping reading a node that "has not produced".
				producedUpTo := rng.Intn(nodeCount)
				produced := map[string]bool{}
				for i := 0; i < producedUpTo; i++ {
					produced[ids[i]] = true
				}

				// The reader is always the last node; its mapping's source is
				// a random earlier node (which may or may not be produced).
				reader := ids[nodeCount-1]
				sourceIdx := rng.Intn(nodeCount - 1)
				sourceNode := ids[sourceIdx]

				node := workflow.CompiledNode{
					ID: reader,
					Mappings: []workflow.CompiledMapping{{
						Target: "x", TargetType: workflow.ValueType{Kind: workflow.KindString},
						SourceKind: workflow.SourceNodeOutput, SourceNode: sourceNode, SourcePath: "out",
					}},
				}
				src := wfext004FakeSource{
					produced: produced,
					outputs:  map[string]map[string]workflow.TypedValue{sourceNode: {"out": {Type: workflow.ValueType{Kind: workflow.KindString}, Text: "v"}}},
				}
				resolved, err := workflow.ResolveMappings(node, src)
				if produced[sourceNode] {
					if err != nil || resolved["x"].Text != "v" {
						t.Fatalf("seed %d: source %s produced, resolve = %+v, %v, want the value with no error", seed, sourceNode, resolved, err)
					}
					return
				}
				if err == nil {
					t.Fatalf("seed %d: source %s has not produced, but ResolveMappings returned %+v with no error", seed, sourceNode, resolved)
				}
				var merr *workflow.MappingError
				if !errors.As(err, &merr) || merr.Code != workflow.CodeSourceNodeNotRun {
					t.Fatalf("seed %d: error = %v, want *workflow.MappingError %s", seed, err, workflow.CodeSourceNodeNotRun)
				}
			})
		}
	})
}

// wfext004FakeSource is a minimal [workflow.MappingSource] whose NodeOutput
// reports "not run" for exactly the nodes not present in produced -- the
// deterministic-DAG property test's whole fixture.
type wfext004FakeSource struct {
	produced map[string]bool
	outputs  map[string]map[string]workflow.TypedValue
}

func (s wfext004FakeSource) WorkflowInput(string) (workflow.TypedValue, bool) {
	return workflow.TypedValue{}, false
}

func (s wfext004FakeSource) NodeOutput(nodeID, path string) (workflow.TypedValue, bool, bool) {
	if !s.produced[nodeID] {
		return workflow.TypedValue{}, false, false
	}
	v, ok := s.outputs[nodeID][path]
	return v, true, ok
}

func (s wfext004FakeSource) Context(string, string) (workflow.TypedValue, bool) {
	return workflow.TypedValue{}, false
}
