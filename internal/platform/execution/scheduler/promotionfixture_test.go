package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	wfruntime "github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepswait "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// The Promotion instance these tests schedule: a trivial TRANSFORM, a real
// WAIT on a fixed business instant, and one of four terminals.
//
// It is the same executable graph test/workflow's own WF-RUN-002 + WF-RUN-004
// end-to-end uses, rebuilt here because a Go test fixture cannot cross a
// package boundary. Nothing about it is a stub: the plan goes through the real
// compiler and version registry, the wake instant is resolved by
// internal/workflow/steps/wait's pure ComputeTimerRequirement, the promise is a
// workflow_timer row written by internal/workflow/timer through this
// repository's production TimerFactory
// (internal/platform/execution.NewTimerFactory), and the terminal write is
// effects.LedgerTerminalWriter. What is new here is who wakes it: not the test,
// but two scheduler replicas.
const (
	waitWorkflowID = "hcmnext.workflows.test.promotion_wait_scheduled"

	nodeWaitPrepare   = "prepare_promotion"
	nodeWaitEffective = "wait_effective_date"
	nodeWaitApplied   = "end_wait_applied"
	nodeWaitExpired   = "end_wait_expired"
	nodeWaitCancelled = "end_wait_cancelled"
	nodeWaitDegraded  = "end_wait_degraded"

	waitTerminalCode = "PROMOTION_APPLIED_AFTER_WAIT"

	waitZoneID    = "America/New_York"
	waitTzdb      = "2026a"
	waitCalendar  = "us-federal"
	waitCalendarV = "2026.1"
)

// waitDataset is the tzdb and calendar release the wake condition is resolved
// against. It is supplied, never read from the environment.
var waitDataset = values.DatasetVersions{TzdbVersion: waitTzdb, CalendarVersion: waitCalendarV}

func demoSchema(name string) workflow.SchemaRef {
	return workflow.SchemaRef{
		SchemaID: "hcmnext.workflows.test." + name + "/v1", Version: 1,
		ProtobufFullName: "hcmnext.workflow.test." + name,
	}
}

func brandedString(brand string) workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: brand}
}

func demoTerminalFields() []workflow.Field {
	return []workflow.Field{
		{Path: "worker_id", Type: brandedString("WorkerID")},
		{Path: "terminal_code", Type: workflow.ValueType{Kind: workflow.KindString}},
	}
}

func demoCompletion(request, execution, business, consistency, obligation string) map[string]string {
	return map[string]string{
		"RequestState": request, "ExecutionState": execution, "BusinessState": business,
		"ConsistencyState": consistency, "ObligationState": obligation,
	}
}

func demoTerminalNode(id, code string, status workflow.RuntimeStatus, dims map[string]string, commitReceiptRef string) workflow.Node {
	return workflow.Node{
		ID: id, Type: workflow.StepEnd, Inputs: demoTerminalFields(),
		InputMappings: []workflow.Mapping{
			{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
			{Target: "terminal_code", Source: workflow.Source{
				Kind: workflow.SourceConstant, Constant: code, Type: workflow.ValueType{Kind: workflow.KindString},
			}},
		},
		Governance: workflow.NodeGovernance{
			Purpose: "PROMOTION_WAIT_SCHEDULED", Classification: "CONFIDENTIAL_HR",
			RevalidationBoundary:  workflow.RevalidatePreClosure,
			DataAccessManifestRef: "data-access.test.promotion-wait-scheduled/v1",
		},
		End: &workflow.EndSpec{TerminalCode: code, RuntimeStatus: status, CompletionMapping: dims, CommitReceiptRef: commitReceiptRef},
	}
}

// promotionWaitDefinition is the smallest executable graph that parks on a
// durable timer. frontier.Seed always marks the start node READY, so a WAIT may
// not be a driven plan's start node; a trivial TRANSFORM stands in front of it.
func promotionWaitDefinition(fireAt time.Time) workflow.Definition {
	return workflow.Definition{
		WorkflowID: waitWorkflowID, Version: 1, Name: "Promotion wait (scheduler)",
		InputSchema: demoSchema("WaitInput"), OutputSchema: demoSchema("WaitResult"),
		VariablesSchema: demoSchema("WaitVariables"),
		TenantScope:     "test", OrganizationScope: humanwork.ScenarioOrganizationScopeID, RiskClass: "HIGH",
		DeclaredModes: []workflow.ExecutionMode{workflow.ModeExecute}, TerminalProfile: workflow.TerminalProfileExecute,
		StartNodeID: nodeWaitPrepare,
		Inputs: []workflow.Field{
			{Path: "worker_id", Type: brandedString("WorkerID")},
			{Path: "proposal_digest", Type: workflow.ValueType{Kind: workflow.KindString}},
		},
		Outputs:               demoTerminalFields(),
		Limits:                workflow.Limits{MaxFanOut: 5, MaxDepth: 3, MaxNodes: 8},
		FailurePolicyRef:      "policy.workflow.failure.test/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.test/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.test/v1",
		Nodes: []workflow.Node{
			{
				ID: nodeWaitPrepare, Type: workflow.StepTransform,
				InputSchema: demoSchema("WaitPrepareInput"), OutputSchema: demoSchema("WaitPrepareResult"),
				Inputs: []workflow.Field{
					{Path: "worker_id", Type: brandedString("WorkerID")},
					{Path: "proposal_digest", Type: workflow.ValueType{Kind: workflow.KindString}},
				},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
					{Target: "proposal_digest", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "proposal_digest"}},
				},
				Transform: &workflow.TransformSpec{
					TransformRef: "transform.test.promotion-wait-scheduled.prepare/v1", Version: 1,
					NormalizationProfile: "profile.test.promotion-wait-scheduled/v1", OutputTaint: workflow.TaintDerived,
					Limits: workflow.TransformLimits{
						MaxInputBytes: workflow.MaxTransformInputBytes, MaxOutputBytes: workflow.MaxTransformOutputBytes,
						MaxSteps: workflow.MaxTransformSteps,
					},
				},
				DeclaredEffect: capability.EffectPure,
				Governance: workflow.NodeGovernance{
					Purpose: "PROMOTION_WAIT_SCHEDULED", Classification: "CONFIDENTIAL_HR",
					RevalidationBoundary:  workflow.RevalidatePreExecution,
					DataAccessManifestRef: "data-access.test.promotion-wait-scheduled/v1",
				},
			},
			{
				ID: nodeWaitEffective, Type: workflow.StepWait,
				InputSchema: demoSchema("WaitEffectiveInput"), OutputSchema: demoSchema("WaitEffectiveResult"),
				Inputs: []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}},
				InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
				},
				DeclaredEffect: capability.EffectPure,
				Wait: &workflow.WaitSpec{
					WakeKind:              workflow.WaitWakeAtInstant,
					WakeInstant:           fireAt.UTC().Format(time.RFC3339Nano),
					ZoneID:                waitZoneID,
					ZoneTzdbVersion:       waitTzdb,
					CalendarRef:           waitCalendar,
					CalendarVersion:       waitCalendarV,
					ReferenceUpdatePolicy: "PIN",
				},
				FailureRoute: nodeWaitDegraded,
				Governance: workflow.NodeGovernance{
					Purpose: "PROMOTION_EFFECTIVE_DATE", Classification: "CONFIDENTIAL_HR",
					RevalidationBoundary:  workflow.RevalidatePreExecution,
					DataAccessManifestRef: "data-access.test.promotion-wait-scheduled/v1",
				},
			},
			demoTerminalNode(nodeWaitApplied, waitTerminalCode, workflow.RuntimeCompleted,
				demoCompletion("CLOSED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED"),
				"receipt.test.promotion-wait-scheduled/v1"),
			demoTerminalNode(nodeWaitExpired, "EXPIRED", workflow.RuntimeCancelled,
				demoCompletion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), ""),
			demoTerminalNode(nodeWaitCancelled, "CANCELLED", workflow.RuntimeCancelled,
				demoCompletion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), ""),
			demoTerminalNode(nodeWaitDegraded, "TIMER_REVIEW_REQUIRED", workflow.RuntimeCompleted,
				demoCompletion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE"), ""),
		},
		Edges: []workflow.Edge{
			{From: nodeWaitPrepare, To: nodeWaitEffective, RouteKey: "SUCCEEDED"},
			{From: nodeWaitPrepare, To: nodeWaitDegraded, RouteKey: "FAILED"},
			{From: nodeWaitEffective, To: nodeWaitApplied, RouteKey: "SUCCEEDED"},
			{From: nodeWaitEffective, To: nodeWaitExpired, RouteKey: "LATE"},
			{From: nodeWaitEffective, To: nodeWaitCancelled, RouteKey: "CANCELLED"},
		},
	}
}

func publishActiveWaitPlan(t *testing.T, fireAt, at time.Time) (*version.Registry, *workflow.CompiledWorkflow, version.CompiledVersion) {
	t.Helper()
	def := promotionWaitDefinition(fireAt)
	plan, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1B})
	if err != nil {
		t.Fatalf("compile the promotion wait definition: %v", err)
	}
	store := version.NewRegistry()
	published, err := version.Publish(store, def, plan, workflow.Options{Phase: workflow.PhaseP1B}, version.PublishMeta{
		SemanticVersion: "1.0.0", PublishedAt: at, PublishedBy: "test:scheduler",
	})
	if err != nil {
		t.Fatalf("version.Publish: %v", err)
	}
	activated, err := version.Activate(store, published.CompiledPlanDigest, version.ActivationEvidence{
		Authorized: true, ApprovedBy: "test:release", Authority: "authority:release",
		ApprovedAt: at, ReviewedPlanDigest: published.CompiledPlanDigest, TestsPassed: true,
	})
	if err != nil {
		t.Fatalf("version.Activate: %v", err)
	}
	return store, plan, activated
}

func demoControlSnapshots() intent.ControlSnapshots {
	return intent.ControlSnapshots{
		CapabilityRegistryDigest: "sha256:capability-registry-fixture", PolicyBundleDigest: "sha256:policy-bundle-fixture",
		LegalContextDigest: "sha256:legal-context-fixture", EntitlementDigest: "sha256:entitlement-fixture",
		ReferenceDataDigest: "sha256:reference-data-fixture", ClassificationTaxonomyDigest: "sha256:classification-taxonomy-fixture",
		DLPDecisionDigest: "sha256:dlp-decision-fixture",
	}
}

// newDemoProposal mints a real, immutable ProposalRevision. runtime.Start
// re-checks its approval and currency itself (WF-RUN-023), so nothing about
// this fixture is a bypass.
func newDemoProposal(t *testing.T, tenant values.TenantId, intentID, subject string, at time.Time) intent.ProposalRevision {
	t.Helper()
	digester, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("protomap.NewDefaultDigester: %v", err)
	}
	interval, err := values.NewOpenInstantInterval(values.NewInstant(at))
	if err != nil {
		t.Fatalf("NewOpenInstantInterval: %v", err)
	}
	placementKey, err := values.NewResourceKey(tenant, "employment", subject)
	if err != nil {
		t.Fatalf("NewResourceKey: %v", err)
	}
	subjectRef := intent.SubjectReference{
		Kind: "EMPLOYMENT", SubjectID: "employment:" + subject, AuthorityDomain: "PEOPLE",
	}
	rev, err := intent.NewProposalRevision(intent.ProposalSpec{
		IntentID: intentID, Revision: 1, Tenant: tenant, OrganizationScopeID: humanwork.ScenarioOrganizationScopeID,
		Subjects:      []intent.SubjectReference{subjectRef},
		EffectiveTime: interval, ControlSnapshots: demoControlSnapshots(),
		ProposedState: []intent.StateAssertion{{
			Subject:       subjectRef,
			ResourceKey:   placementKey,
			FieldPath:     "position_id",
			CanonicalText: "position:senior-engineer",
		}},
		CreatedBy: intent.PrincipalReference{
			PrincipalID: humanwork.PrincipalRequester, Kind: intent.InitiatorHuman, IdentityAssuranceRef: "assurance:1",
		},
	}, intent.Definition{
		Ref: intent.Ref{TypeID: "hcmnext.test.promotion_wait_scheduled", Version: 1}, Family: intent.FamilyChangeRequest,
	}, digester, nil, func() values.Instant { return values.NewInstant(at) })
	if err != nil {
		t.Fatalf("intent.NewProposalRevision: %v", err)
	}
	return rev
}

// endOnlySteps runs the plan's two synchronous step types. The WAIT node never
// reaches it: an instance parks on the durable timer instead.
type endOnlySteps struct{}

func (endOnlySteps) Run(_ context.Context, req execute.StepRequest) (frontier.NodeOutcome, wfruntime.GovernanceRefs, error) {
	switch req.Node.Type {
	case workflow.StepEnd:
		return frontier.NodeOutcome{NodeID: req.Node.ID}, wfruntime.GovernanceRefs{}, nil
	case workflow.StepTransform:
		return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: workflow.OutcomeSucceeded}, wfruntime.GovernanceRefs{}, nil
	default:
		return frontier.NodeOutcome{}, wfruntime.GovernanceRefs{}, fmt.Errorf(
			"scheduler fixture: unexpected READY step type %s for node %s", req.Node.Type, req.Node.ID)
	}
}

func newLedgerAppender(t *testing.T) ledgerport.Appender {
	t.Helper()
	registry, err := ledgerport.NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatalf("ledger.NewLedgerEventDigestRegistry: %v", err)
	}
	return ledgerport.NewAppender(registry)
}

// waitTimerReader is the counterpart of the production TimerFactory: it loads
// the durable timer a Driver.ResumeTimer advances from. It lives in this test
// rather than in internal/platform/execution because execute.TimerReader's own
// method signature spells google/uuid.UUID, and
// definitions/architecture/dependency-roles.yaml does not list
// internal/platform among the roots allowed to import that module.
type waitTimerReader struct{ scheduler timer.Scheduler }

func (r waitTimerReader) LoadTimer(ctx context.Context, ex wfruntime.Executor, tenantID, timerID uuid.UUID) (execute.FiredTimer, error) {
	row, err := r.scheduler.Load(ctx, ex, tenantID, timerID)
	if err != nil {
		return execute.FiredTimer{}, err
	}
	return execute.FiredTimer{
		TimerID: row.TimerID, InstanceID: row.InstanceID, NodeID: row.NodeID,
		Key: row.Key, State: row.State, FiresAt: row.FiresAt,
	}, nil
}

// promotionCell is one composed workflow cell: the pinned plan, the durable
// stores and the ports a Driver needs. Every replica in a test builds its own
// over its own connection, exactly as two processes would.
type promotionCell struct {
	db       dbport.Beginner
	plan     *workflow.CompiledWorkflow
	resolver effects.PolicyResolver
	versions *version.Registry
	terminal *effects.LedgerTerminalWriter
	timers   timer.Scheduler
	factory  platformexecution.TimerFactory
	leases   lease.Manager
}

func newPromotionCell(t *testing.T, db dbport.Beginner, plan *workflow.CompiledWorkflow,
	versions *version.Registry, activated version.CompiledVersion,
) promotionCell {
	t.Helper()
	factory, err := platformexecution.NewTimerFactory(platformexecution.TimerFactoryConfig{Dataset: waitDataset})
	if err != nil {
		t.Fatalf("platform execution.NewTimerFactory: %v", err)
	}
	return promotionCell{
		db:   db,
		plan: plan,
		resolver: effects.PolicyResolver{Entries: []effects.PolicyEntry{{
			WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: activated.CompiledPlanDigest}, Plan: plan,
		}}},
		versions: versions,
		terminal: &effects.LedgerTerminalWriter{
			Appender: newLedgerAppender(t), ProjectionName: "workflow_promotion_wait_scheduled",
			SourceRef: "hcmnext:test:scheduler",
		},
		factory: factory,
	}
}

// driver builds a Driver whose every advancement is fenced by fence. A nil
// fence builds an unfenced driver, which is how a start runs before its
// instance exists: no lease can name an instance id that Execute has not yet
// derived, so the start presents no fence (as a caller holding no lease does).
func (c promotionCell) driver(t *testing.T, fence *wfruntime.Fence, at time.Time) *execute.Driver {
	t.Helper()
	opts := execute.Options{
		DB: c.db, Steps: endOnlySteps{}, Terminal: c.terminal,
		Timers: c.factory, TimerReader: waitTimerReader{scheduler: c.timers},
		Guard:     idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock:     func() time.Time { return at },
	}
	if fence != nil {
		opts.Fence = fence
		opts.FenceVerifier = lease.Fenced{Manager: c.leases}
	}
	drv, err := execute.New(opts)
	if err != nil {
		t.Fatalf("execute.New: %v", err)
	}
	return drv
}

// startRequest is the immutable start this cell runs.
func (c promotionCell) startRequest(tenantID uuid.UUID, proposal intent.ProposalRevision, subject string, at time.Time) wfruntime.StartRequest {
	return wfruntime.StartRequest{
		TenantID: tenantID, CellID: "cell-local", StartIdempotencyKey: "start:" + subject,
		Resolver: c.resolver, Versions: c.versions,
		Proposal: wfruntime.ProposalBinding{Revision: proposal},
		// WF-RUN-027: approval and supersession are derived from fact ports,
		// never from caller-asserted flags on the binding.
		ProposalFacts: wfruntime.MemoryProposalFacts{},
		ApprovalFacts: wfruntime.MemoryApprovalFacts{
			ByRevisionID: map[string][]wfruntime.ApprovalDecisionFact{
				proposal.ProposalRevisionID: {{
					DecisionID: "decision:hr-partner-approves-start", Outcome: wfruntime.ApprovalOutcomeApproved,
					ProposalDigest: proposal.MaterialDigest.Digest,
				}},
			},
		},
		ExpectedIntentID: proposal.IntentID, ExpectedTenant: proposal.Tenant,
		BusinessSubjectRefs: []string{"employment:" + subject},
		ExecutionMode:       workflow.ModeExecute, CorrelationID: "corr:" + subject, CreatedAt: at,
	}
}

// waitRequirement recomputes the WAIT node's wake requirement exactly as the
// production TimerFactory did, so a dispatcher can address the promise that was
// actually minted rather than one that merely looks similar.
func (c promotionCell) waitRequirement(t *testing.T) stepswait.TimerRequirement {
	t.Helper()
	node, ok := c.plan.Node(nodeWaitEffective)
	if !ok {
		t.Fatalf("pinned plan has no node %s", nodeWaitEffective)
	}
	bound, err := stepswait.FromCompiled(&node)
	if err != nil {
		t.Fatalf("bind the compiled WAIT node: %v", err)
	}
	bound.WorkflowID, bound.WorkflowVersion = c.plan.WorkflowID, c.plan.Version
	requirement, err := stepswait.ComputeTimerRequirement(bound, waitDataset)
	if err != nil {
		t.Fatalf("compute the wake requirement: %v", err)
	}
	return requirement
}

// forConn returns the same pinned cell over an independent connection.
//
// Two scheduler replicas are two processes in production; in one test process
// they must at least be two sessions, because a pgxadapter.Conn is not safe for
// concurrent use. The ledger appender is rebuilt for the same reason -- nothing
// shared between replicas here is shared in production either.
func (c promotionCell) forConn(t *testing.T, db *pgtest.DB) promotionCell {
	t.Helper()
	out := c
	out.db = appConn(t, db)
	out.terminal = &effects.LedgerTerminalWriter{
		Appender:       newLedgerAppender(t),
		ProjectionName: c.terminal.ProjectionName,
		SourceRef:      c.terminal.SourceRef,
	}
	return out
}
