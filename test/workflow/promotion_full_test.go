package workflow_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionsteps"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/intervention"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepsapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
	stepstask "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/task"
	stepswait "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

const (
	promotionFullAt      = "2026-09-05T12:00:00Z"
	promotionFullQueue   = "queue:workflow-runtime"
	promotionFullWorkRef = "workload:hcmnext-workflow-runtime"
)

type promotionFullBehavior struct {
	aboveThreshold bool
	validity       []string
	payroll        workflow.Outcome
}

// promotionFullPorts are deterministic test doubles for the ports consumed by
// the real promotionsteps.Runner. The fixture controls only port answers; it
// exercises the shipped node dispatch, fixed OBSERVE vocabulary and TASK
// awaiting behavior end to end through execute.Driver.
type promotionFullPorts struct {
	mu         sync.Mutex
	behavior   promotionFullBehavior
	revalidate int
}

func (p *promotionFullPorts) Snapshot(context.Context, execute.StepRequest) (promotionsteps.Artifact, error) {
	return promotionsteps.Artifact{OutputDigest: digestForPromotionNode(promotionexec.NodeSnapshotWorker)}, nil
}

func (p *promotionFullPorts) SimulateCompensation(context.Context, execute.StepRequest) (promotionsteps.Artifact, error) {
	return promotionsteps.Artifact{OutputDigest: digestForPromotionNode(promotionexec.NodeSimulateCompensation)}, nil
}

func (p *promotionFullPorts) EvaluateBand(context.Context, execute.StepRequest) (promotionsteps.Artifact, error) {
	return promotionsteps.Artifact{OutputDigest: digestForPromotionNode(promotionexec.NodeEvaluateBand)}, nil
}

func (p *promotionFullPorts) RaiseThreshold(context.Context, execute.StepRequest) (promotionsteps.ThresholdResult, error) {
	route := workflow.Outcome("WITHIN_THRESHOLD")
	if p.behavior.aboveThreshold {
		route = workflow.Outcome("ABOVE_THRESHOLD")
	}
	return promotionsteps.ThresholdResult{
		Artifact: promotionsteps.Artifact{OutputDigest: digestForPromotionNode(promotionexec.NodeRaiseThreshold)},
		Route:    route,
	}, nil
}

func (p *promotionFullPorts) Revalidate(context.Context, execute.StepRequest) (promotionsteps.RevalidationResult, error) {
	return promotionsteps.RevalidationResult{
		Artifact:  promotionsteps.Artifact{OutputDigest: digestForPromotionNode(promotionexec.NodeRevalidate)},
		Confirmed: true,
	}, nil
}

func (p *promotionFullPorts) StillValid(context.Context, execute.StepRequest) (promotionsteps.ValidityResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	index := p.revalidate
	if index >= len(p.behavior.validity) {
		index = len(p.behavior.validity) - 1
	}
	if index < 0 {
		index = 0
	}
	status := "VALID"
	if len(p.behavior.validity) > 0 {
		status = p.behavior.validity[index]
	}
	p.revalidate++
	// The scenario table speaks the definition's route keys; the validity
	// port speaks GOVERN-003's requirement vocabulary, which the runner maps
	// back onto those routes (validityRoute in promotionsteps).
	switch status {
	case "VALID":
		status = "CONFIRMED"
	case "BLOCKED":
		status = "BLOCK"
	}
	return promotionsteps.ValidityResult{
		Artifact: promotionsteps.Artifact{OutputDigest: digestForPromotionNode(promotionexec.NodeStillValid)},
		Status:   status,
	}, nil
}

func (p *promotionFullPorts) ExecutePromotion(context.Context, execute.StepRequest, string) (promotionsteps.Artifact, error) {
	return promotionsteps.Artifact{OutputDigest: digestForPromotionNode(promotionexec.NodeExecutePromotion)}, nil
}

func (p *promotionFullPorts) Observe(ctx context.Context, req execute.StepRequest) (promotionsteps.ObservationResult, error) {
	if req.Node.ID == promotionexec.NodeObservePayroll && p.behavior.payroll == "FAIL" {
		// A real observation that did not see the committed change: the
		// runner takes the compiled FAIL edge (through the compensate
		// node), never a port error.
		return promotionsteps.ObservationResult{
			Artifact: promotionsteps.Artifact{OutputDigest: digestForPromotionNode(req.Node.ID)},
			Status:   promotionsteps.ObservationFailed,
		}, nil
	}
	if req.Node.ID == promotionexec.NodeObservePayroll && p.behavior.payroll != "" && p.behavior.payroll != workflow.Outcome("PASS") {
		return promotionsteps.ObservationResult{}, fmt.Errorf("payroll observation %s", p.behavior.payroll)
	}
	nodeID := req.Node.ID
	return promotionsteps.ObservationResult{
		Artifact: promotionsteps.Artifact{OutputDigest: digestForPromotionNode(nodeID)},
		Status:   promotionsteps.ObservationObserved,
	}, nil
}

func (p *promotionFullPorts) Reconcile(context.Context, execute.StepRequest) (promotionsteps.ReconciliationResult, error) {
	return promotionsteps.ReconciliationResult{
		Artifact: promotionsteps.Artifact{OutputDigest: digestForPromotionNode(promotionexec.NodeObserveReconciliation)},
		Status:   promotionsteps.ReconciliationConsistent,
	}, nil
}

func (p *promotionFullPorts) ReleaseHold(context.Context, execute.StepRequest) (promotionsteps.HoldReleaseResult, error) {
	return promotionsteps.HoldReleaseResult{
		Artifact: promotionsteps.Artifact{OutputDigest: digestForPromotionNode(promotionexec.NodeCompensateHold)},
		Status:   "COMPENSATED",
	}, nil
}

type promotionFullTerminalWriter struct {
	delegate *effects.LedgerTerminalWriter
}

type promotionFullRepairRequester struct{}

func (promotionFullRepairRequester) Request(ctx context.Context, tx dbport.Tx, req execute.RepairRequest) error {
	_, _, err := (reconcile.PostgresStore{}).Create(ctx, tx, reconcile.Job{
		TenantID: req.TenantID, JobID: reconcile.JobID(req.TenantID, req.EffectRef, req.PolicyRef),
		EffectRef: req.EffectRef, EffectID: promotionexec.NodeObservePayroll, PolicyRef: req.PolicyRef,
		IntendedRef: req.IntendedRef, RequiredFreshness: observe.FreshnessFresh,
		NextCheckAt: req.RequestedAt, Deadline: req.RequestedAt.Add(24 * time.Hour), Owner: "workflow-runtime",
		SLARef: "sla:promotion-repair", RepairPolicy: req.RepairPolicy, Status: reconcile.StatusPending,
		Version: 1, CreatedAt: req.RequestedAt, UpdatedAt: req.RequestedAt,
	})
	return err
}

func (w *promotionFullTerminalWriter) Write(ctx context.Context, tx dbport.Tx, req execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	if req.TerminalCode != "PROMOTION_COMPLETE" && req.TerminalCode != "PROMOTION_REPAIR_REQUIRED" {
		return idempotency.ResultIdentity{ResultRef: req.TerminalCode}, nil
	}
	return w.delegate.Write(ctx, tx, req)
}

func digestForPromotionNode(nodeID string) string {
	return "sha256:" + fmt.Sprintf("%064x", len(nodeID)+1)
}

type promotionFullWorkItems struct {
}

func promotionApprovalRequirement(requirementID, approver string, decideBy time.Time) (humanwork.ApprovalRequirement, error) {
	switch requirementID {
	case promotionexec.ApprovalFinance:
		return promotionexec.CompileFinanceApprovalRequirement(approver, decideBy)
	case promotionexec.ApprovalManager:
		return promotionexec.CompileManagerApprovalRequirement(approver, decideBy)
	default:
		return humanwork.ApprovalRequirement{}, fmt.Errorf("unknown promotion approval requirement %q", requirementID)
	}
}

func (promotionFullWorkItems) CreateAndRoute(ctx context.Context, ex workitem.Executor, req execute.WorkItemRequest) (workitem.WorkItem, error) {
	kind := workitem.KindTask
	workType := "task.promotion.reapproval/v1"
	approvalRef := ""
	owner := humanwork.PrincipalHRBP
	var requirement humanwork.ApprovalRequirement
	if req.Continuation.TargetNodeID == promotionexec.NodeApproveFinance || req.Continuation.TargetNodeID == promotionexec.NodeApproveManager {
		kind = workitem.KindApproval
		workType = "approval.promotion/v1"
		if req.Continuation.TargetNodeID == promotionexec.NodeApproveFinance {
			approvalRef, owner = promotionexec.ApprovalFinance, "principal:finance-partner"
		} else {
			approvalRef, owner = promotionexec.ApprovalManager, humanwork.PrincipalManager
		}
		var err error
		requirement, err = promotionApprovalRequirement(approvalRef, owner, req.CreatedAt.Add(72*time.Hour))
		if err != nil {
			return workitem.WorkItem{}, err
		}
	}
	item, err := workitem.NewWorkItem(workitem.NewWorkItemInput{
		TenantID: req.Continuation.TenantID, WorkItemID: req.WorkItemID, Kind: kind,
		WorkType: workType, CorrelationID: req.CorrelationID,
		WorkflowInstanceID: req.Continuation.InstanceID, NodeID: req.Continuation.TargetNodeID,
		ApprovalRequirementRef: approvalRef, ProposalRef: req.Proposal.Revision.MaterialDigest.Digest,
		SubjectRefs: req.SubjectRefs, PolicyRouteRef: "route.promotion.execute/v1",
		Visibility: workitem.VisibilityAssigneeOnly, OrganizationScopeID: "acme/engineering",
		DeadlineAt: req.CreatedAt.Add(72 * time.Hour), CreatedAt: req.CreatedAt,
	})
	if err != nil {
		return workitem.WorkItem{}, err
	}
	store := workitem.Store{}
	created, err := store.Create(ctx, ex, item, workitem.TransitionMeta{
		ActorPrincipalID: "system:workflow-runtime", Reason: "WORKFLOW_WORK_REQUIRED", At: req.CreatedAt,
	})
	if err != nil {
		return workitem.WorkItem{}, err
	}
	assignment := workitem.Assignment{
		Resolution: humanwork.Resolution{
			RequirementID: approvalRef, RequirementRevision: requirement.Revision, RequirementDigest: requirement.Digest(), Outcome: humanwork.OutcomeResolved,
			Candidates: []humanwork.Candidate{{PrincipalID: owner, Via: humanwork.SourceDirect, TermRef: "promotion.fixture"}},
			ResolvedAt: values.NewInstant(req.CreatedAt), EffectiveAt: values.NewInstant(req.CreatedAt),
			DirectoryVersion: "promotion.fixture/v1", ExpressionDigest: "sha256:" + strings.Repeat("1", 64),
			QuorumRequired: 1,
		},
		GovernancePolicyRef: "governance.promotion.fixture/v1", Trigger: workitem.TriggerInitialRouting,
		ChosenOwner: owner,
	}
	if kind == workitem.KindTask {
		assignment.Resolution = humanwork.Resolution{
			RequirementID:       "task.promotion.reapproval/v1",
			RequirementRevision: 1,
			Outcome:             humanwork.OutcomeResolved,
			Candidates:          []humanwork.Candidate{{PrincipalID: owner, Via: humanwork.SourceDirect, TermRef: "promotion.fixture"}},
			ResolvedAt:          values.NewInstant(req.CreatedAt), EffectiveAt: values.NewInstant(req.CreatedAt),
			DirectoryVersion: "promotion.fixture/v1", ExpressionDigest: "sha256:" + strings.Repeat("2", 64),
			RequirementDigest: "sha256:" + strings.Repeat("3", 64), QuorumRequired: 1,
		}
		assignment.GovernancePolicyRef = "governance.promotion.fixture/v1"
		assignment.ChosenOwner = owner
	}
	return store.Route(ctx, ex, created.TenantID, created.WorkItemID, created.ItemVersion, assignment,
		workitem.TransitionMeta{ActorPrincipalID: "system:workflow-runtime", Reason: "ASSIGNMENT_RESOLVED", At: req.CreatedAt})
}

type promotionFullTimerFactory struct {
	scheduler timer.Scheduler
	dataset   values.DatasetVersions
}

func (f promotionFullTimerFactory) CreateTimer(ctx context.Context, ex runtime.Executor, req execute.TimerRequest) (execute.TimerHandle, error) {
	node, err := stepswait.FromCompiled(&req.Node)
	if err != nil {
		return execute.TimerHandle{}, err
	}
	node.WorkflowID, node.WorkflowVersion = req.Plan.WorkflowID, req.Plan.Version
	requirement, err := stepswait.ComputeTimerRequirement(node, f.dataset)
	if err != nil {
		return execute.TimerHandle{}, err
	}
	scheduled, err := f.scheduler.Schedule(ctx, ex, timer.Request{
		TenantID: req.Continuation.TenantID, InstanceID: req.Continuation.InstanceID,
		NodeID: req.Continuation.TargetNodeID, Kind: timer.KindUntil,
		Requirement: requirement, CreatedAt: req.CreatedAt,
		// A WAIT the plan re-enters (re-approval) is a later activation with
		// its own promise; the continuation carries the attempt Advance recorded.
		Attempt: req.Continuation.TargetAttempt,
	})
	if err != nil {
		return execute.TimerHandle{}, err
	}
	return execute.TimerHandle{TimerID: scheduled.Timer.TimerID, NodeID: scheduled.Timer.NodeID,
		Key: scheduled.Timer.Key, FiresAt: scheduled.Timer.FiresAt, Replay: scheduled.Replay}, nil
}

type promotionFullFixture struct {
	t        *testing.T
	db       *pgtest.DB
	tenantID uuid.UUID
	key      string
	at       time.Time
	fireAt   time.Time
	start    runtime.StartRequest
	proposal intent.ProposalRevision
	plan     *workflow.CompiledWorkflow
	versions version.Store
	terminal execute.TerminalWriter
	runner   *promotionsteps.Runner
	behavior promotionFullBehavior
}

func newPromotionFullFixture(t *testing.T, key string, behavior promotionFullBehavior) promotionFullFixture {
	t.Helper()
	at, _ := time.Parse(time.RFC3339, promotionFullAt)
	fireAt := at.Add(24 * time.Hour)
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, key, at)
	versions, plan := publishPromotionFullPlan(t, fireAt)
	proposal := newDemoProposal(t, values.TenantId(key), "intent:"+key, at)
	binding := runtime.ProposalBinding{Revision: proposal, ApprovalRef: "decision:promotion-start"}
	resolver := effects.PolicyResolver{Entries: []effects.PolicyEntry{{
		WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: plan.Digest()}, Plan: plan,
	}}}
	terminal := &promotionFullTerminalWriter{delegate: &effects.LedgerTerminalWriter{Appender: newLedgerAppender(t), ProjectionName: "promotion_full_" + key, SourceRef: "hcmnext:test:promotion-full"}}
	ports := &promotionFullPorts{behavior: behavior}
	runner := promotionsteps.New(promotionsteps.Config{
		SnapshotWorker:        ports,
		SimulateCompensation:  ports,
		EvaluateBand:          ports,
		RaiseThreshold:        ports,
		Revalidate:            ports,
		StillValid:            ports,
		ExecutePromotion:      ports,
		ObservePayroll:        ports,
		ObserveAccess:         ports,
		ObserveReconciliation: ports,
		CompensateHold:        ports,
	})
	return promotionFullFixture{t: t, db: db, tenantID: tenantID, key: key, at: at, fireAt: fireAt,
		proposal: proposal,
		start: runtime.StartRequest{TenantID: tenantID, CellID: "cell-local", StartIdempotencyKey: "start:" + key,
			Resolver: resolver, Versions: versions, Proposal: binding,
			ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: approvedStartFacts(proposal), ExpectedIntentID: proposal.IntentID,
			ExpectedTenant: proposal.Tenant, BusinessSubjectRefs: []string{"employment:promotion-execute-demo-1"},
			ExecutionMode: workflow.ModeExecute, CorrelationID: "corr:" + key, CreatedAt: at},
		plan: plan, versions: versions, terminal: terminal, runner: runner, behavior: behavior}
}

type promotionVersionStore struct{ plan *workflow.CompiledWorkflow }

func (s promotionVersionStore) Put(version.CompiledVersion) error { return nil }
func (s promotionVersionStore) GetByDigest(digest string) (version.CompiledVersion, bool, error) {
	if s.plan == nil || digest != s.plan.Digest() {
		return version.CompiledVersion{}, false, nil
	}
	return version.CompiledVersion{WorkflowID: s.plan.WorkflowID, CompiledPlanDigest: digest, Status: version.StatusActive}, true, nil
}
func (s promotionVersionStore) GetActiveForWorkflow(workflowID string) (version.CompiledVersion, bool, error) {
	if s.plan == nil || workflowID != s.plan.WorkflowID {
		return version.CompiledVersion{}, false, nil
	}
	return version.CompiledVersion{WorkflowID: workflowID, CompiledPlanDigest: s.plan.Digest(), Status: version.StatusActive}, true, nil
}
func (s promotionVersionStore) List(workflowID string) ([]version.CompiledVersion, error) {
	if s.plan == nil || workflowID != s.plan.WorkflowID {
		return nil, nil
	}
	v, _, _ := s.GetActiveForWorkflow(workflowID)
	return []version.CompiledVersion{v}, nil
}

func publishPromotionFullPlan(t *testing.T, fireAt time.Time) (version.Store, *workflow.CompiledWorkflow) {
	t.Helper()
	def := promotionexec.Definition()
	for i := range def.Nodes {
		if def.Nodes[i].ID == promotionexec.NodeWaitEffectiveDate {
			def.Nodes[i].Wait = &workflow.WaitSpec{WakeKind: workflow.WaitWakeAtInstant,
				WakeInstant: fireAt.UTC().Format(time.RFC3339Nano), ZoneID: "America/New_York", ZoneTzdbVersion: "2026a",
				CalendarRef: "us-federal", CalendarVersion: "2026.1", ReferenceUpdatePolicy: "PIN"}
		}
	}
	plan, err := promotionexec.Compile(def)
	if err != nil {
		t.Fatalf("promotionexec.Compile: %v", err)
	}
	return promotionVersionStore{plan: plan}, plan
}

func (f promotionFullFixture) driver(t *testing.T, at time.Time, fence *runtime.Fence) *execute.Driver {
	t.Helper()
	options := execute.Options{DB: appConn(t, f.db), Steps: f.runner, WorkItems: promotionFullWorkItems{}, Terminal: f.terminal, Repair: promotionFullRepairRequester{},
		Items: workitem.Store{}, Guard: idempotency.PostgresStore{}, Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock: func() time.Time { return at }, Timers: promotionFullTimerFactory{scheduler: timer.Scheduler{}, dataset: values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "2026.1"}},
		TimerReader: waitTimerReader{scheduler: timer.Scheduler{}},
		// The acknowledgement gate parks through the production durable
		// subscription adapter, exactly as the served composition does, so a
		// run that reaches it suspends honestly instead of erroring.
		Signals: execution.SignalSubscriptions{}, SignalReader: execution.SignalSubscriptions{},
		SignalTimeoutReader: execution.SignalSubscriptions{}}
	if fence != nil {
		options.Fence, options.FenceVerifier = fence, lease.Fenced{Manager: lease.Manager{}}
	}
	drv, err := execute.New(options)
	if err != nil {
		t.Fatalf("execute.New: %v", err)
	}
	return drv
}

type promotionFullDispatcher struct {
	fixture promotionFullFixture
}

func (d promotionFullDispatcher) Dispatch(ctx context.Context, work scheduler.Work) (scheduler.Disposition, error) {
	var timerID uuid.UUID
	err := d.fixture.db.Conn.QueryRow(ctx, `SELECT timer_id FROM workflow_timer WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND timer_state = 'FIRED' ORDER BY created_at DESC LIMIT 1`,
		d.fixture.tenantID, work.Row.InstanceID, work.Row.NodeID).Scan(&timerID)
	if err != nil {
		return scheduler.DispositionRetry, err
	}
	at := d.fixture.fireAt.Add(time.Minute)
	instanceVersion, err := instanceVersionForPromotion(ctx, d.fixture.db, d.fixture.tenantID, work.Row.InstanceID)
	if err != nil {
		return scheduler.DispositionRetry, err
	}
	requirementNode, ok := d.fixture.plan.Node(promotionexec.NodeWaitEffectiveDate)
	if !ok || requirementNode.Wait == nil {
		return scheduler.DispositionRetry, fmt.Errorf("promotion wait node missing")
	}
	waitNode, err := stepswait.FromCompiled(&requirementNode)
	if err != nil {
		return scheduler.DispositionRetry, err
	}
	waitNode.WorkflowID, waitNode.WorkflowVersion = d.fixture.plan.WorkflowID, d.fixture.plan.Version
	requirement, err := stepswait.ComputeTimerRequirement(waitNode, values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "2026.1"})
	if err != nil {
		return scheduler.DispositionRetry, err
	}
	resolution, err := stepswait.Resolve(requirement, values.NewInstant(at), stepswait.WakeEvent{Kind: stepswait.EventWake})
	if err != nil {
		return scheduler.DispositionRetry, err
	}
	outcome := resolution.ToNodeOutcome(work.Row.NodeID)
	outcome.OutputDigest = "sha256:" + strings.Repeat("e", 64)
	drv := d.fixture.driver(d.fixture.t, at, ptrRuntimeFence(work.Fence.RuntimeFence(at)))
	_, err = drv.ResumeTimer(ctx, execute.ResumeTimerRequest{Start: d.fixture.start, InstanceID: work.Row.InstanceID,
		ExpectedInstanceVersion: instanceVersion, TimerID: timerID, Outcome: outcome, RecordedAt: at})
	if err != nil {
		d.fixture.t.Logf("DISPATCH ResumeTimer: %v", err)
		return scheduler.DispositionRetry, err
	}
	return scheduler.DispositionCompleted, nil
}

func ptrRuntimeFence(f runtime.Fence) *runtime.Fence { return &f }

func instanceVersionForPromotion(ctx context.Context, db *pgtest.DB, tenantID, instanceID uuid.UUID) (int64, error) {
	var version int64
	err := db.Conn.QueryRow(ctx, `SELECT instance_version FROM workflow_instance WHERE tenant_id = $1 AND instance_id = $2`, tenantID, instanceID).Scan(&version)
	return version, err
}

// loadPromotionInstance reads the tenant-scoped runtime row through the same
// runtime store used by the application timer-resume path. A direct query on
// workflow_instance outside app.tenant_id correctly sees no rows under FORCE
// ROW LEVEL SECURITY, even though the executor wrote the row.
func loadPromotionInstance(t *testing.T, f promotionFullFixture, instanceID uuid.UUID) runtime.Instance {
	t.Helper()
	var instance runtime.Instance
	inTenantTx(t, f.db, f.tenantID, func(tx dbport.Tx) error {
		var err error
		instance, err = (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenantID, instanceID)
		return err
	})
	return instance
}

func countRows(t *testing.T, db *pgtest.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Conn.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

func promotionReapprovalTaskNode(f promotionFullFixture, item workitem.WorkItem) stepstask.CompiledTaskNode {
	node, _ := f.plan.Node(item.NodeID)
	return stepstask.CompiledTaskNode{
		WorkflowID: f.plan.WorkflowID, WorkflowVersion: f.plan.Version, NodeID: item.NodeID, WorkType: item.WorkType,
		OutputSchema:        node.OutputSchema,
		FormDefinition:      stepstask.VersionedRef{Ref: "form.promotion.reapproval/v1", Version: 1},
		AccessibilityPolicy: stepstask.VersionedRef{Ref: "policy.accessibility.default/v1", Version: 1},
		AccommodationPolicy: stepstask.VersionedRef{Ref: "policy.accommodation.default/v1", Version: 1},
	}
}

func completePromotionItem(t *testing.T, f promotionFullFixture, item workitem.WorkItem, principal string, at time.Time, outcome intentapproval.Outcome) workitem.WorkItem {
	t.Helper()
	var completed workitem.WorkItem
	inTenantTx(t, f.db, f.tenantID, func(tx dbport.Tx) error {
		store := workitem.Store{}
		claimed, err := store.Claim(context.Background(), tx, workitem.ClaimInput{TenantID: f.tenantID, WorkItemID: item.WorkItemID,
			ExpectedVersion: item.ItemVersion, ClaimantPrincipalID: principal, ClaimExpiresAt: at.Add(time.Hour), Now: at,
			Meta: workitem.TransitionMeta{ActorPrincipalID: principal, Reason: "PROMOTION_APPROVAL_CLAIMED", At: at}})
		if err != nil {
			return err
		}
		started, err := store.Start(context.Background(), tx, f.tenantID, item.WorkItemID, claimed.ItemVersion, at,
			workitem.TransitionMeta{ActorPrincipalID: principal, Reason: "PROMOTION_APPROVAL_STARTED", At: at})
		if err != nil {
			return err
		}
		switch item.Kind {
		case workitem.KindApproval:
			requirement, requirementErr := promotionApprovalRequirement(item.ApprovalRequirementRef, principal, item.DeadlineAt)
			if requirementErr != nil {
				return requirementErr
			}
			decision := approvalDecisionFor(workitem.WorkItem{CompletedBy: principal}, requirement, f.proposal, at)
			decision.Outcome = outcome
			completed, err = stepsapproval.Complete(context.Background(), tx, store, started, decision, at,
				workitem.TransitionMeta{ActorPrincipalID: principal, Reason: "PROMOTION_APPROVAL_DECIDED", At: at})
			return err
		case workitem.KindTask:
			if started.ClaimID == nil || started.ClaimExpiresAt == nil {
				return fmt.Errorf("promotion task %s has no live claim", started.WorkItemID)
			}
			node := promotionReapprovalTaskNode(f, started)
			var submission stepstask.Submission
			completed, submission, err = stepstask.Submit(context.Background(), tx, store, started, stepstask.SubmitInput{
				Node: node,
				Spec: stepstask.SubmissionSpec{
					CompletedBy: principal, CandidateVia: humanwork.SourceDirect,
					ClaimID: *started.ClaimID, ClaimExpiresAt: values.NewInstant(*started.ClaimExpiresAt), SubmittedAt: values.NewInstant(at),
					OutputSchema: node.OutputSchema, CanonicalPayloadDigest: "sha256:" + strings.Repeat("c", 64),
					FormDefinition: node.FormDefinition, RenderContextDigest: "sha256:" + strings.Repeat("d", 64),
					ValidationEvidenceRef: "evidence.validation.promotion/v1", AccessibilityEvidenceRef: "evidence.accessibility.promotion/v1",
					AccommodationEvidenceRef: "evidence.accommodation.promotion/v1",
				},
				Validator: stepstask.ValidatorFunc(func(stepstask.ValidationRequest) error { return nil }),
				Now:       at, Meta: workitem.TransitionMeta{ActorPrincipalID: principal, Reason: "PROMOTION_TASK_SUBMITTED", At: at},
			})
			if err != nil {
				return err
			}
			continuation, err := stepstask.NewContinuation(completed.WorkflowInstanceID, node, completed)
			if err != nil {
				return err
			}
			resolution, err := stepstask.Resolve(continuation, completed, &submission,
				stepstask.ValidatorFunc(func(stepstask.ValidationRequest) error { return nil }), values.NewInstant(at), stepstask.Event{})
			if err != nil {
				return err
			}
			if resolution.Outcome != stepstask.OutcomeSucceeded {
				return fmt.Errorf("promotion task resolved as %s", resolution.Outcome)
			}
			return nil
		default:
			return fmt.Errorf("unsupported promotion work item kind %s", item.Kind)
		}
	})
	return completed
}

func promotionApprovalOutcome(t *testing.T, item workitem.WorkItem, nodeID string, req humanwork.ApprovalRequirement, proposal intent.ProposalRevision, decidedAt, now time.Time, outcome intentapproval.Outcome) frontier.NodeOutcome {
	t.Helper()
	decision := approvalDecisionFor(item, req, proposal, decidedAt)
	decision.Outcome = outcome
	continuation, err := stepsapproval.NewContinuation(item.WorkflowInstanceID, nodeID, proposal,
		humanwork.RequirementSet{Requirements: []humanwork.ApprovalRequirement{req}}, []workitem.WorkItem{item})
	if err != nil {
		t.Fatalf("steps/approval.NewContinuation: %v", err)
	}
	resolution, err := stepsapproval.Resolve(continuation, []workitem.WorkItem{item}, []intentapproval.ApprovalDecision{decision},
		values.NewInstant(now), stepsapproval.Event{})
	if err != nil {
		t.Fatalf("steps/approval.Resolve: %v", err)
	}
	return resolution.ToNodeOutcome(nodeID)
}

func promotionItem(t *testing.T, f promotionFullFixture, instanceID uuid.UUID, nodeID string) workitem.WorkItem {
	t.Helper()
	var found workitem.WorkItem
	inTenantTx(t, f.db, f.tenantID, func(tx dbport.Tx) error {
		items, err := (workitem.Store{}).ListForInstance(context.Background(), tx, f.tenantID, instanceID)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.NodeID == nodeID && !item.Status.Terminal() {
				found = item
				break
			}
		}
		return nil
	})
	if found.WorkItemID == uuid.Nil {
		t.Fatalf("no open promotion work item for %s", nodeID)
	}
	return found
}

func makePromotionScheduler(t *testing.T, f promotionFullFixture, now time.Time) *scheduler.Scheduler {
	t.Helper()
	claim := lease.AcquireRequest{TenantID: f.tenantID, Resource: lease.Resource{Kind: lease.ResourceQueue, ID: promotionFullQueue}, Holder: lease.Identity{WorkloadRef: promotionFullWorkRef, InstanceRef: "replica:promotion-full"}}
	s, err := scheduler.New(scheduler.Config{DB: appConn(t, f.db), Claims: []lease.AcquireRequest{claim}, Leases: lease.Manager{}, Timers: timer.Scheduler{Attempts: runtime.Store{}},
		Misfire: schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: time.Hour}, Clock: func() time.Time { return now }, BatchSize: 8,
		Dispatcher: promotionFullDispatcher{fixture: f}})
	if err != nil {
		t.Fatalf("scheduler.New: %v", err)
	}
	return s
}

func runPromotionToWait(t *testing.T, f promotionFullFixture) (execute.Result, execute.Result) {
	t.Helper()
	ctx := context.Background()
	first, err := f.driver(t, f.at, nil).Execute(ctx, execute.ExecuteRequest{Start: f.start})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if first.Status != execute.StatusParked || len(first.WorkItems) == 0 {
		t.Fatalf("initial Execute = %+v, want parked human work", first)
	}
	if f.behavior.aboveThreshold {
		finance := completePromotionItem(t, f, first.WorkItems[0], "principal:finance-partner", f.at.Add(time.Minute), intentapproval.OutcomeApproved)
		financeReq, err := promotionApprovalRequirement(finance.ApprovalRequirementRef, finance.CompletedBy, finance.DeadlineAt)
		if err != nil {
			t.Fatalf("finance requirement: %v", err)
		}
		managerResult, err := f.driver(t, f.at.Add(2*time.Minute), nil).Resume(ctx, execute.ResumeRequest{Start: f.start, InstanceID: first.Start.InstanceID,
			ExpectedInstanceVersion: first.InstanceVersion, WorkItemID: finance.WorkItemID, ExpectedWorkItemVersion: finance.ItemVersion,
			Outcome: promotionApprovalOutcome(t, finance, promotionexec.NodeApproveFinance, financeReq, f.proposal, f.at.Add(time.Minute), f.at.Add(2*time.Minute), intentapproval.OutcomeApproved), RecordedAt: f.at.Add(2 * time.Minute)})
		if err != nil {
			t.Fatalf("finance Resume: %v", err)
		}
		manager := completePromotionItem(t, f, managerResult.WorkItems[0], humanwork.PrincipalManager, f.at.Add(3*time.Minute), intentapproval.OutcomeApproved)
		managerReq, err := promotionApprovalRequirement(manager.ApprovalRequirementRef, manager.CompletedBy, manager.DeadlineAt)
		if err != nil {
			t.Fatalf("manager requirement: %v", err)
		}
		final, err := f.driver(t, f.at.Add(4*time.Minute), nil).Resume(ctx, execute.ResumeRequest{Start: f.start, InstanceID: first.Start.InstanceID,
			ExpectedInstanceVersion: managerResult.InstanceVersion, WorkItemID: manager.WorkItemID, ExpectedWorkItemVersion: manager.ItemVersion,
			Outcome: promotionApprovalOutcome(t, manager, promotionexec.NodeApproveManager, managerReq, f.proposal, f.at.Add(3*time.Minute), f.at.Add(4*time.Minute), intentapproval.OutcomeApproved), RecordedAt: f.at.Add(4 * time.Minute)})
		if err != nil {
			t.Fatalf("manager Resume: %v", err)
		}
		// Resume returns the advancement result and deliberately does not copy
		// the immutable StartReceipt. Keep the fixture's returned parked result
		// bound to the actual instance so terminal/work-item assertions do not
		// query uuid.Nil.
		final.Start = first.Start
		return first, final
	}
	manager := completePromotionItem(t, f, first.WorkItems[0], humanwork.PrincipalManager, f.at.Add(time.Minute), intentapproval.OutcomeApproved)
	managerReq, err := promotionApprovalRequirement(manager.ApprovalRequirementRef, manager.CompletedBy, manager.DeadlineAt)
	if err != nil {
		t.Fatalf("manager requirement: %v", err)
	}
	final, err := f.driver(t, f.at.Add(2*time.Minute), nil).Resume(ctx, execute.ResumeRequest{Start: f.start, InstanceID: first.Start.InstanceID,
		ExpectedInstanceVersion: first.InstanceVersion, WorkItemID: manager.WorkItemID, ExpectedWorkItemVersion: manager.ItemVersion,
		Outcome: promotionApprovalOutcome(t, manager, promotionexec.NodeApproveManager, managerReq, f.proposal, f.at.Add(time.Minute), f.at.Add(2*time.Minute), intentapproval.OutcomeApproved), RecordedAt: f.at.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("manager Resume: %v", err)
	}
	final.Start = first.Start
	return first, final
}

// acceptingSignalVerifier admits every signal it sees. The fixture proves
// the driver's park/receive/resume mechanics, not signature trust: the
// served intake binds its own attested verifier, proven where the intake
// lives.
type acceptingSignalVerifier struct{}

func (acceptingSignalVerifier) Verify(stepSignal.Signal) error { return nil }

// acknowledgeParkedPromotion receives the HRIS-recorded acknowledgement
// against the run's open gate and resumes the driver from the matched
// receipt, running the instance to its terminal. It mirrors the served
// Journey.Acknowledge loop at the driver level: the same production
// subscription adapter opened the wait, the same durable store receives the
// signal, and the same driver resumes from the receipt.
func acknowledgeParkedPromotion(t *testing.T, f promotionFullFixture, instanceID uuid.UUID, at time.Time) execute.Result {
	t.Helper()
	ctx := context.Background()
	var sub signals.OpenSubscription
	var receipt signals.Receipt
	inTenantTx(t, f.db, f.tenantID, func(tx dbport.Tx) error {
		var err error
		sub, err = (signals.Store{}).OpenSubscriptionForCorrelation(ctx, tx, f.tenantID, promotionexec.NodeAcknowledgeRelease, "intent:"+f.key)
		if err != nil {
			return fmt.Errorf("open acknowledgement wait: %w", err)
		}
		receipt, err = (signals.Store{}).Receive(ctx, tx, signals.ReceiveRequest{
			Signal: stepSignal.Signal{
				Tenant: values.TenantId(f.tenantID.String()), Source: "hcmnext.integrations.hris",
				EventType: sub.EventType, SchemaRef: sub.ExpectedSchemaRef,
				CorrelationKey: sub.CorrelationKey, CorrelationValue: sub.CorrelationValue,
				IdempotencyKey: "test:ack:" + f.key,
				Payload:        []byte(`{"acknowledged":true,"intent_id":"intent:` + f.key + `"}`),
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
		t.Fatalf("acknowledgement receipt = %+v, want one ACCEPTED disposition for the open wait", receipt)
	}
	version, err := instanceVersionForPromotion(ctx, f.db, f.tenantID, instanceID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.driver(t, at, nil).ResumeSignal(ctx, execute.ResumeSignalRequest{
		Start: f.start, InstanceID: instanceID, ExpectedInstanceVersion: version,
		SignalID: receipt.SignalID, SubscriptionID: sub.ID, RecordedAt: at,
	})
	if err != nil {
		t.Fatalf("ResumeSignal: %v", err)
	}
	if result.Status != execute.StatusComplete {
		t.Fatalf("resume result = %+v, want COMPLETE", result)
	}
	return result
}

// confirmProviderWait receives the provider's confirmation against the run's
// open 1.1.0 provider-confirmation wait on node and resumes the driver from
// the matched receipt. The wait correlates on the proposal revision (the
// outbox effect ids are payroll:<revision> and iam:<revision>) and accepts
// only the provider's own source. The resumed wait always succeeds; the
// observation after it judges the provider's outcome, which these fixture
// ports report locally.
func confirmProviderWait(t *testing.T, f promotionFullFixture, instanceID uuid.UUID, node, source string, at time.Time) execute.Result {
	t.Helper()
	ctx := context.Background()
	var sub signals.OpenSubscription
	var receipt signals.Receipt
	inTenantTx(t, f.db, f.tenantID, func(tx dbport.Tx) error {
		var err error
		sub, err = (signals.Store{}).OpenSubscriptionForCorrelation(ctx, tx, f.tenantID, node, f.proposal.ProposalRevisionID)
		if err != nil {
			return fmt.Errorf("open %s wait: %w", node, err)
		}
		receipt, err = (signals.Store{}).Receive(ctx, tx, signals.ReceiveRequest{
			Signal: stepSignal.Signal{
				Tenant: values.TenantId(f.tenantID.String()), Source: source,
				EventType: sub.EventType, SchemaRef: sub.ExpectedSchemaRef,
				CorrelationKey: sub.CorrelationKey, CorrelationValue: sub.CorrelationValue,
				IdempotencyKey: "test:" + node + ":" + f.key,
				Payload:        []byte(`{"proposal_revision_id":"` + f.proposal.ProposalRevisionID + `"}`),
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
		t.Fatalf("%s receipt = %+v, want one ACCEPTED disposition for the open wait", node, receipt)
	}
	version, err := instanceVersionForPromotion(ctx, f.db, f.tenantID, instanceID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.driver(t, at, nil).ResumeSignal(ctx, execute.ResumeSignalRequest{
		Start: f.start, InstanceID: instanceID, ExpectedInstanceVersion: version,
		SignalID: receipt.SignalID, SubscriptionID: sub.ID, RecordedAt: at,
	})
	if err != nil {
		t.Fatalf("ResumeSignal(%s): %v", node, err)
	}
	return result
}

// confirmProviderWaits drives both 1.1.0 provider-confirmation waits in
// order, payroll then identity, leaving the run parked on the next wait.
func confirmProviderWaits(t *testing.T, f promotionFullFixture, instanceID uuid.UUID, at time.Time) {
	t.Helper()
	for _, wait := range []struct{ node, source string }{
		{promotionexec.NodeAwaitPayrollConfirmation, "hcmnext.integrations.payroll"},
		{promotionexec.NodeAwaitAccessConfirmation, "hcmnext.integrations.iam"},
	} {
		if result := confirmProviderWait(t, f, instanceID, wait.node, wait.source, at); result.Status != execute.StatusParked {
			t.Fatalf("after confirming %s the run = %+v, want it parked on its next wait", wait.node, result)
		}
	}
}

// expireParkedPromotion sweeps the run's open acknowledgement gate past its
// close and resumes the driver from the committed expiry, running the
// instance to its terminal. It mirrors the served scheduler tick at the
// driver level: the same production store expires the wait, and the same
// driver advances TIMED_OUT from the expired row.
func expireParkedPromotion(t *testing.T, f promotionFullFixture, instanceID uuid.UUID, at time.Time) execute.Result {
	t.Helper()
	ctx := context.Background()
	var open signals.OpenSubscription
	inTenantTx(t, f.db, f.tenantID, func(tx dbport.Tx) error {
		var err error
		open, err = (signals.Store{}).OpenSubscriptionForCorrelation(ctx, tx, f.tenantID, promotionexec.NodeAcknowledgeRelease, "intent:"+f.key)
		return err
	})
	var expired []signals.ExpiredSubscription
	inTenantTx(t, f.db, f.tenantID, func(tx dbport.Tx) error {
		var err error
		expired, err = (signals.Store{}).ExpireDue(ctx, tx, f.tenantID, at, 8)
		return err
	})
	if len(expired) != 1 || expired[0].SubscriptionID != open.ID {
		t.Fatalf("expired = %+v, want exactly the acknowledgement wait", expired)
	}
	version, err := instanceVersionForPromotion(ctx, f.db, f.tenantID, instanceID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.driver(t, at, nil).ResumeSignalTimeout(ctx, execute.ResumeSignalTimeoutRequest{
		Start: f.start, InstanceID: instanceID, ExpectedInstanceVersion: version,
		SubscriptionID: open.ID, RecordedAt: at,
	})
	if err != nil {
		t.Fatalf("ResumeSignalTimeout: %v", err)
	}
	if result.Status != execute.StatusComplete {
		t.Fatalf("timeout resume result = %+v, want COMPLETE", result)
	}
	return result
}

func assertPromotionRows(t *testing.T, f promotionFullFixture, instanceID uuid.UUID, status, terminalNode string, ledgerCount int) {
	t.Helper()
	ctx := context.Background()
	got := string(loadPromotionInstance(t, f, instanceID).RuntimeStatus)
	if got != status {
		t.Fatalf("instance status = %q, want %q", got, status)
	}
	var n int
	if err := f.db.Conn.QueryRow(ctx, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, f.tenantID).Scan(&n); err != nil {
		t.Fatalf("ledger count: %v", err)
	}
	if n != ledgerCount {
		t.Fatalf("ledger events = %d, want %d", n, ledgerCount)
	}
	if terminalNode != "" {
		if err := f.db.Conn.QueryRow(ctx, `SELECT count(*) FROM workflow_node_execution WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND status = 'SUCCEEDED'`, f.tenantID, instanceID, terminalNode).Scan(&n); err != nil {
			t.Fatalf("terminal node count: %v", err)
		}
		if n != 1 {
			t.Fatalf("terminal node %s executions = %d, want 1", terminalNode, n)
		}
	}
}

func TestPromotionWorkflowCompletesEndToEnd(t *testing.T) {
	f := newPromotionFullFixture(t, "promotion-full-complete", promotionFullBehavior{aboveThreshold: true, validity: []string{"VALID"}, payroll: "PASS"})
	_, parked := runPromotionToWait(t, f)
	d := makePromotionScheduler(t, f, f.fireAt)
	got, err := d.Tick(context.Background())
	if err != nil || got.Fired != 1 || got.Completed != 1 {
		t.Fatalf("scheduler Tick = %+v, err=%v; want one fired and one completed dispatch", got, err)
	}
	// The effective-date dispatch parks the run on the acknowledgement
	// gate; the received attestation runs it to its COMPLETE terminal.
	confirmProviderWaits(t, f, parked.Start.InstanceID, f.fireAt)
	acknowledgeParkedPromotion(t, f, parked.Start.InstanceID, f.fireAt)
	assertPromotionRows(t, f, parked.Start.InstanceID, "COMPLETED", promotionexec.NodeEndComplete, 1)
	var timerState, readyState string
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT timer_state FROM workflow_timer WHERE tenant_id = $1 AND instance_id = $2`, f.tenantID, parked.Start.InstanceID).Scan(&timerState); err != nil {
		t.Fatal(err)
	}
	if timerState != "FIRED" {
		t.Fatalf("timer state = %s, want FIRED", timerState)
	}
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT ready_state FROM workflow_ready_work WHERE tenant_id = $1 AND instance_id = $2`, f.tenantID, parked.Start.InstanceID).Scan(&readyState); err != nil {
		t.Fatal(err)
	}
	if readyState != runtimestate.ReadyDone {
		t.Fatalf("ready work state = %s, want DONE", readyState)
	}
}

func TestPromotionWorkflowWithinThresholdSkipsFinanceApproval(t *testing.T) {
	f := newPromotionFullFixture(t, "promotion-full-within", promotionFullBehavior{validity: []string{"VALID"}, payroll: "PASS"})
	_, parked := runPromotionToWait(t, f)
	if n := countRows(t, f.db, `SELECT count(*) FROM work_item WHERE tenant_id = $1 AND workflow_instance_id = $2 AND node_id = $3`, f.tenantID, parked.Start.InstanceID, promotionexec.NodeApproveFinance); n != 0 {
		t.Fatalf("finance work items = %d, want 0", n)
	}
	d := makePromotionScheduler(t, f, f.fireAt)
	if got, err := d.Tick(context.Background()); err != nil || got.Completed != 1 {
		t.Fatalf("scheduler Tick = %+v, err=%v", got, err)
	}
	// The run parks on the acknowledgement gate after the effective date;
	// the received attestation runs it to its COMPLETE terminal.
	confirmProviderWaits(t, f, parked.Start.InstanceID, f.fireAt)
	acknowledgeParkedPromotion(t, f, parked.Start.InstanceID, f.fireAt)
	assertPromotionRows(t, f, parked.Start.InstanceID, "COMPLETED", promotionexec.NodeEndComplete, 1)
}

func TestPromotionWorkflowRejectedAtManagerApprovalWritesNoFact(t *testing.T) {
	f := newPromotionFullFixture(t, "promotion-full-rejected", promotionFullBehavior{validity: []string{"VALID"}, payroll: "PASS"})
	first, err := f.driver(t, f.at, nil).Execute(context.Background(), execute.ExecuteRequest{Start: f.start})
	if err != nil {
		t.Fatal(err)
	}
	manager := completePromotionItem(t, f, first.WorkItems[0], humanwork.PrincipalManager, f.at.Add(time.Minute), intentapproval.OutcomeRejected)
	managerReq, err := promotionApprovalRequirement(manager.ApprovalRequirementRef, manager.CompletedBy, manager.DeadlineAt)
	if err != nil {
		t.Fatalf("manager requirement: %v", err)
	}
	result, err := f.driver(t, f.at.Add(2*time.Minute), nil).Resume(context.Background(), execute.ResumeRequest{Start: f.start, InstanceID: first.Start.InstanceID,
		ExpectedInstanceVersion: first.InstanceVersion, WorkItemID: manager.WorkItemID, ExpectedWorkItemVersion: manager.ItemVersion,
		Outcome: promotionApprovalOutcome(t, manager, promotionexec.NodeApproveManager, managerReq, f.proposal, f.at.Add(time.Minute), f.at.Add(2*time.Minute), intentapproval.OutcomeRejected), RecordedAt: f.at.Add(2 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != execute.StatusComplete {
		t.Fatalf("rejection result = %+v, want COMPLETE", result)
	}
	assertPromotionRows(t, f, first.Start.InstanceID, "COMPLETED", promotionexec.NodeEndRejected, 0)
}

func TestPromotionWorkflowRevalidationRequiresReapprovalThenCompletes(t *testing.T) {
	f := newPromotionFullFixture(t, "promotion-full-reapproval", promotionFullBehavior{validity: []string{"REAPPROVAL_REQUIRED", "VALID"}, payroll: "PASS"})
	_, parked := runPromotionToWait(t, f)
	d := makePromotionScheduler(t, f, f.fireAt)
	got, err := d.Tick(context.Background())
	if err != nil || got.Completed != 1 {
		t.Fatalf("first scheduler Tick = %+v, err=%v", got, err)
	}
	item := promotionItem(t, f, parked.Start.InstanceID, promotionexec.NodeReapproval)
	completed := completePromotionItem(t, f, item, humanwork.PrincipalHRBP, f.fireAt.Add(time.Hour), intentapproval.OutcomeApproved)
	versionNow, err := instanceVersionForPromotion(context.Background(), f.db, f.tenantID, parked.Start.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	manager := f.driver(t, f.fireAt.Add(2*time.Hour), nil)
	managerResult, err := manager.Resume(context.Background(), execute.ResumeRequest{Start: f.start, InstanceID: parked.Start.InstanceID,
		ExpectedInstanceVersion: versionNow, WorkItemID: completed.WorkItemID, ExpectedWorkItemVersion: completed.ItemVersion,
		Outcome: frontier.NodeOutcome{NodeID: promotionexec.NodeReapproval, Outcome: workflow.Outcome("SUCCEEDED"), OutputDigest: completed.CompletedOutputDigest}, RecordedAt: f.fireAt.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	managerItem := managerResult.WorkItems[0]
	managerDone := completePromotionItem(t, f, managerItem, humanwork.PrincipalManager, f.fireAt.Add(3*time.Hour), intentapproval.OutcomeApproved)
	managerReq, err := promotionApprovalRequirement(managerDone.ApprovalRequirementRef, managerDone.CompletedBy, managerDone.DeadlineAt)
	if err != nil {
		t.Fatalf("manager requirement: %v", err)
	}
	versionNow, _ = instanceVersionForPromotion(context.Background(), f.db, f.tenantID, parked.Start.InstanceID)
	_, err = f.driver(t, f.fireAt.Add(4*time.Hour), nil).Resume(context.Background(), execute.ResumeRequest{Start: f.start, InstanceID: parked.Start.InstanceID,
		ExpectedInstanceVersion: versionNow, WorkItemID: managerDone.WorkItemID, ExpectedWorkItemVersion: managerDone.ItemVersion,
		Outcome: promotionApprovalOutcome(t, managerDone, promotionexec.NodeApproveManager, managerReq, f.proposal, f.fireAt.Add(3*time.Hour), f.fireAt.Add(4*time.Hour), intentapproval.OutcomeApproved), RecordedAt: f.fireAt.Add(4 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	// The same replica keeps its queue lease across ticks; a second scheduler
	// object at the same instant would be refused the queue as a rival holder.
	if got, err := d.Tick(context.Background()); err != nil || got.Completed != 1 {
		rows, qerr := f.db.SQL.QueryContext(context.Background(), `SELECT node_id, timer_key, timer_state, fires_at FROM workflow_timer WHERE tenant_id = $1 AND instance_id = $2 ORDER BY fires_at`, f.tenantID, parked.Start.InstanceID)
		if qerr == nil {
			for rows.Next() {
				var node, key, state string
				var at time.Time
				_ = rows.Scan(&node, &key, &state, &at)
				t.Logf("timer row: node=%s key=%s state=%s fires_at=%s", node, key, state, at)
			}
			rows.Close()
		}
		t.Fatalf("second scheduler Tick = %+v, err=%v", got, err)
	}
	// The re-approved run parks on the acknowledgement gate after the
	// effective date; the received attestation runs it to COMPLETE.
	confirmProviderWaits(t, f, parked.Start.InstanceID, f.fireAt.Add(5*time.Hour))
	acknowledgeParkedPromotion(t, f, parked.Start.InstanceID, f.fireAt.Add(5*time.Hour))
	assertPromotionRows(t, f, parked.Start.InstanceID, "COMPLETED", promotionexec.NodeEndComplete, 1)
}

func TestPromotionWorkflowRevalidationBlocksEndsBlocked(t *testing.T) {
	f := newPromotionFullFixture(t, "promotion-full-blocked", promotionFullBehavior{validity: []string{"BLOCKED"}, payroll: "PASS"})
	_, parked := runPromotionToWait(t, f)
	if got, err := makePromotionScheduler(t, f, f.fireAt).Tick(context.Background()); err != nil || got.Completed != 1 {
		t.Fatalf("scheduler Tick = %+v, err=%v", got, err)
	}
	assertPromotionRows(t, f, parked.Start.InstanceID, "COMPLETED", promotionexec.NodeEndBlocked, 0)
	var requestState, executionState, businessState string
	dimensions := loadPromotionInstance(t, f, parked.Start.InstanceID).CompletionDimensions
	requestState, executionState, businessState = dimensions.RequestState, dimensions.ExecutionState, dimensions.BusinessState
	if requestState != "APPROVED" || executionState != "BLOCKED" || businessState != "NOT_ACHIEVED" {
		t.Fatalf("blocked completion dimensions = %s/%s/%s, want APPROVED/BLOCKED/NOT_ACHIEVED", requestState, executionState, businessState)
	}
}

func TestPromotionWorkflowDegradedObservationEndsInRepairPlan(t *testing.T) {
	f := newPromotionFullFixture(t, "promotion-full-repair", promotionFullBehavior{validity: []string{"VALID"}, payroll: "FAIL"})
	_, parked := runPromotionToWait(t, f)
	if got, err := makePromotionScheduler(t, f, f.fireAt).Tick(context.Background()); err != nil || got.Completed != 1 {
		t.Fatalf("scheduler Tick = %+v, err=%v", got, err)
	}
	// The committed run parks on the payroll provider's confirmation; the
	// confirmation resumes it into the observation that reports FAIL.
	if result := confirmProviderWait(t, f, parked.Start.InstanceID, promotionexec.NodeAwaitPayrollConfirmation, "hcmnext.integrations.payroll", f.fireAt); result.Status != execute.StatusComplete {
		t.Fatalf("after the payroll confirmation the run = %+v, want it COMPLETE on RepairPlan", result)
	}
	assertPromotionRows(t, f, parked.Start.InstanceID, "REPAIR_REQUIRED", promotionexec.NodeEndRepairPlan, 1)
	if n := countRows(t, f.db, `SELECT count(*) FROM effect_reconciliation_job WHERE tenant_id = $1`, f.tenantID); n != 1 {
		t.Fatalf("repair jobs = %d, want 1", n)
	}
	// The known-bad payroll observation routes through the compensate node
	// (not straight to repair): the bounded correction runs before the
	// RepairPlan terminal, and its COMPENSATED route is on the record.
	if n := countRows(t, f.db, `SELECT count(*) FROM workflow_node_execution WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND status = 'SUCCEEDED'`, f.tenantID, parked.Start.InstanceID, promotionexec.NodeCompensateHold); n != 1 {
		t.Fatalf("compensate node executions = %d, want 1 SUCCEEDED with the COMPENSATED route", n)
	}
}

func TestPromotionWorkflowAcknowledgementTimeoutEndsInRepairPlan(t *testing.T) {
	f := newPromotionFullFixture(t, "promotion-full-expiry", promotionFullBehavior{validity: []string{"VALID"}, payroll: "PASS"})
	_, parked := runPromotionToWait(t, f)
	if got, err := makePromotionScheduler(t, f, f.fireAt).Tick(context.Background()); err != nil || got.Completed != 1 {
		t.Fatalf("scheduler Tick = %+v, err=%v", got, err)
	}
	confirmProviderWaits(t, f, parked.Start.InstanceID, f.fireAt)
	// Capture the wait's signal identity before the sweep so the late
	// acknowledgement below addresses the expired wait itself.
	var eventType, schemaRef, correlationKey, correlationValue string
	inTenantTx(t, f.db, f.tenantID, func(tx dbport.Tx) error {
		open, err := (signals.Store{}).OpenSubscriptionForCorrelation(context.Background(), tx, f.tenantID, promotionexec.NodeAcknowledgeRelease, "intent:"+f.key)
		if err != nil {
			return err
		}
		eventType, schemaRef, correlationKey, correlationValue = open.EventType, open.ExpectedSchemaRef, open.CorrelationKey, open.CorrelationValue
		return nil
	})
	// The close window is fourteen days; the sweep runs a day later. The
	// unacknowledged run takes its declared TIMED_OUT edge to RepairPlan.
	late := f.fireAt.Add(15 * 24 * time.Hour)
	expireParkedPromotion(t, f, parked.Start.InstanceID, late)
	assertPromotionRows(t, f, parked.Start.InstanceID, "REPAIR_REQUIRED", promotionexec.NodeEndRepairPlan, 1)
	// The expired wait is no longer an intake: correlating it is a stage
	// refusal, and a signal that arrives now is refused late.
	inTenantTx(t, f.db, f.tenantID, func(tx dbport.Tx) error {
		if _, err := (signals.Store{}).OpenSubscriptionForCorrelation(context.Background(), tx, f.tenantID, promotionexec.NodeAcknowledgeRelease, "intent:"+f.key); !errors.Is(err, signals.ErrNoOpenSubscription) {
			t.Fatalf("open subscription after expiry = %v, want ErrNoOpenSubscription", err)
		}
		receipt, err := (signals.Store{}).Receive(context.Background(), tx, signals.ReceiveRequest{
			Signal: stepSignal.Signal{
				Tenant: values.TenantId(f.tenantID.String()), Source: "hcmnext.integrations.hris",
				EventType: eventType, SchemaRef: schemaRef,
				CorrelationKey: correlationKey, CorrelationValue: correlationValue,
				IdempotencyKey: "test:ack:late:" + f.key,
				Payload:        []byte(`{"acknowledged":true,"intent_id":"intent:` + f.key + `"}`),
				ReceivedAt:     values.NewInstant(late),
			},
			ReceivedAt: late,
		}, acceptingSignalVerifier{})
		if err != nil {
			return err
		}
		for _, disposition := range receipt.Dispositions {
			if disposition.Status != stepSignal.StatusRefusedLate {
				t.Fatalf("late acknowledgement disposition = %+v, want REFUSED_LATE", disposition)
			}
		}
		if len(receipt.Dispositions) == 0 {
			t.Fatalf("late acknowledgement left no disposition")
		}
		return nil
	})
}

func TestPromotionWorkflowSurvivesRestartDuringApprovalAndWait(t *testing.T) {
	f := newPromotionFullFixture(t, "promotion-full-restart", promotionFullBehavior{validity: []string{"VALID"}, payroll: "PASS"})
	first, _ := runPromotionToWait(t, f)
	// The first driver is intentionally discarded. The next driver is rebuilt
	// from the published plan and the durable tenant/instance/work-item/timer
	// rows, matching a process restart while the workflow is parked.
	if first.Status != execute.StatusParked {
		t.Fatalf("initial result = %+v, want PARKED", first)
	}
	if got, err := makePromotionScheduler(t, f, f.fireAt).Tick(context.Background()); err != nil || got.Completed != 1 {
		t.Fatalf("restart scheduler Tick = %+v, err=%v", got, err)
	}
	// The restarted run parks on the acknowledgement gate; the received
	// attestation runs it to its COMPLETE terminal.
	confirmProviderWaits(t, f, first.Start.InstanceID, f.fireAt)
	acknowledgeParkedPromotion(t, f, first.Start.InstanceID, f.fireAt)
	assertPromotionRows(t, f, first.Start.InstanceID, "COMPLETED", promotionexec.NodeEndComplete, 1)
}

func TestPromotionWorkflowDuplicateTimerFireAndDuplicateResumeAreIdempotent(t *testing.T) {
	f := newPromotionFullFixture(t, "promotion-full-idempotent", promotionFullBehavior{validity: []string{"VALID"}, payroll: "PASS"})
	_, parked := runPromotionToWait(t, f)
	if got, err := makePromotionScheduler(t, f, f.fireAt).Tick(context.Background()); err != nil || got.Fired != 1 {
		t.Fatalf("first scheduler Tick = %+v, err=%v", got, err)
	}
	if got, err := makePromotionScheduler(t, f, f.fireAt.Add(time.Minute)).Tick(context.Background()); err != nil || got.Fired != 0 {
		t.Fatalf("duplicate scheduler Tick = %+v, err=%v", got, err)
	}
	// Both ticks park on the acknowledgement gate; the received attestation
	// runs the once-fired dispatch to its COMPLETE terminal.
	confirmProviderWaits(t, f, parked.Start.InstanceID, f.fireAt.Add(time.Minute))
	acknowledgeParkedPromotion(t, f, parked.Start.InstanceID, f.fireAt.Add(time.Minute))
	assertPromotionRows(t, f, parked.Start.InstanceID, "COMPLETED", promotionexec.NodeEndComplete, 1)
	if n := countRows(t, f.db, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, f.tenantID); n != 1 {
		t.Fatalf("ledger events after duplicate fire/resume = %d, want 1", n)
	}
}

func TestPromotionWorkflowCancelledBeforeApprovalWritesNothing(t *testing.T) {
	f := newPromotionFullFixture(t, "promotion-full-cancelled", promotionFullBehavior{aboveThreshold: true, validity: []string{"VALID"}, payroll: "PASS"})
	first, err := f.driver(t, f.at, nil).Execute(context.Background(), execute.ExecuteRequest{Start: f.start})
	if err != nil {
		t.Fatal(err)
	}
	request := intervention.Request{
		InstanceID: first.Start.InstanceID, ExpectedVersion: first.InstanceVersion, Kind: intervention.Cancel,
		Reason: "promotion withdrawn before approval", EvidenceRefs: []string{"evidence.promotion.withdrawal/v1"},
		RequestedBy: "principal:promotion-requester", RequestedAt: f.at,
	}
	instance := loadPromotionInstance(t, f, first.Start.InstanceID)
	plan, err := intervention.Evaluate(request, intervention.Facts{Plan: f.plan, Instance: instance})
	if err != nil {
		t.Fatalf("intervention.Evaluate(cancel): %v", err)
	}
	if plan.InstanceTo != runtime.InstanceCancelling {
		t.Fatalf("cancel plan = %+v, want the cancellation boundary", plan)
	}
	result, err := f.driver(t, f.at.Add(2*time.Minute), nil).Cancel(context.Background(), execute.CancellationRequest{
		TenantID: f.tenantID, InstanceID: first.Start.InstanceID, ExpectedInstanceVersion: first.InstanceVersion,
		Plan: f.plan, Reason: request.Reason, RequestedBy: request.RequestedBy, RecordedAt: f.at.Add(2 * time.Minute),
	})
	if err != nil || result.Instance.RuntimeStatus != runtime.InstanceCancelled {
		t.Fatalf("cancel result = %+v, err=%v", result, err)
	}
	// Cancellation closes the instance through execute.Cancel rather than
	// routing a durable END node, so no end_cancelled execution row exists.
	assertPromotionRows(t, f, first.Start.InstanceID, "CANCELLED", "", 0)
}
