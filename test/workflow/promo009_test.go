package workflow_test

import (
	"context"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	intentapp "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

func TestTodo_PROMO_009(t *testing.T) {
	f := newPromotionFullFixture(t, "promo009-primary", promotionFullBehavior{
		aboveThreshold: true,
		validity:       []string{"VALID"},
		payroll:        "PASS",
	})
	_, parked := runPromotionToWait(t, f)
	got, err := makePromotionScheduler(t, f, f.fireAt).Tick(context.Background())
	if err != nil || got.Fired != 1 || got.Completed != 1 {
		t.Fatalf("promotion scheduler Tick = %+v, err=%v; want one fired and one completed dispatch", got, err)
	}

	assertPromotion009Trace(t, f, parked.Start.InstanceID, promotion009CompleteDimensions())
}

func TestTodo_PROMO_009_Golden(t *testing.T) {
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("promotionexec.Compile: %v", err)
	}

	type nodeExpectation struct {
		id       string
		stepType workflow.StepType
	}
	wantNodes := []nodeExpectation{
		{promotionexec.NodeSnapshotWorker, workflow.StepCapability},
		{promotionexec.NodeSimulateCompensation, workflow.StepCapability},
		{promotionexec.NodeEvaluateBand, workflow.StepCapability},
		{promotionexec.NodeRaiseThreshold, workflow.StepDecision},
		{promotionexec.NodeApproveFinance, workflow.StepApproval},
		{promotionexec.NodeApproveManager, workflow.StepApproval},
		{promotionexec.NodeWaitEffectiveDate, workflow.StepWait},
		{promotionexec.NodeRevalidate, workflow.StepCapability},
		{promotionexec.NodeStillValid, workflow.StepDecision},
		{promotionexec.NodeExecutePromotion, workflow.StepCapability},
		{promotionexec.NodeReapproval, workflow.StepTask},
		{promotionexec.NodeEndBlocked, workflow.StepEnd},
		{promotionexec.NodeObservePayroll, workflow.StepObserve},
		{promotionexec.NodeEndInvalidated, workflow.StepEnd},
		{promotionexec.NodeEndCancelled, workflow.StepEnd},
		{promotionexec.NodeEndRejected, workflow.StepEnd},
		{promotionexec.NodeEndExpired, workflow.StepEnd},
		{promotionexec.NodeObserveAccess, workflow.StepObserve},
		{promotionexec.NodeObserveReconciliation, workflow.StepObserve},
		{promotionexec.NodeEndComplete, workflow.StepEnd},
		{promotionexec.NodeEndRepairPlan, workflow.StepEnd},
	}
	if got := len(plan.Nodes); got != len(wantNodes) {
		t.Fatalf("compiled promotion node count = %d, want %d", got, len(wantNodes))
	}
	for _, want := range wantNodes {
		node, ok := plan.Node(want.id)
		if !ok {
			t.Fatalf("compiled promotion plan is missing node %s", want.id)
		}
		if node.Type != want.stepType {
			t.Errorf("node %s type = %s, want %s", want.id, node.Type, want.stepType)
		}
		if node.Type == workflow.StepCapability && (node.Capability == nil || node.Capability.OperationMode != workflow.ModeExecute) {
			t.Errorf("capability node %s is not bound to EXECUTE", want.id)
		}
	}

	type terminalExpectation struct {
		id, code string
		dims     map[string]string
	}
	wantTerminals := []terminalExpectation{
		{promotionexec.NodeEndComplete, "PROMOTION_COMPLETE", promotion009CompleteDimensions()},
		{promotionexec.NodeEndRepairPlan, "PROMOTION_REPAIR_REQUIRED", map[string]string{
			"RequestState": "APPROVED", "ExecutionState": "REPAIR_REQUIRED", "BusinessState": "UNKNOWN",
			"ConsistencyState": "DEGRADED", "ObligationState": "PENDING",
		}},
		{promotionexec.NodeEndRejected, "PROMOTION_REJECTED", map[string]string{
			"RequestState": "REJECTED", "ExecutionState": "NOT_PLANNED", "BusinessState": "NOT_ACHIEVED",
			"ConsistencyState": "NOT_APPLICABLE", "ObligationState": "NOT_APPLICABLE",
		}},
		{promotionexec.NodeEndInvalidated, "PROMOTION_INVALIDATED", map[string]string{
			"RequestState": "SUPERSEDED", "ExecutionState": "NOT_PLANNED", "BusinessState": "NOT_ACHIEVED",
			"ConsistencyState": "NOT_APPLICABLE", "ObligationState": "NOT_APPLICABLE",
		}},
		{promotionexec.NodeEndExpired, "PROMOTION_EXPIRED", map[string]string{
			"RequestState": "CANCELLED", "ExecutionState": "NOT_PLANNED", "BusinessState": "NOT_ACHIEVED",
			"ConsistencyState": "NOT_APPLICABLE", "ObligationState": "NOT_APPLICABLE",
		}},
		{promotionexec.NodeEndCancelled, "PROMOTION_CANCELLED", map[string]string{
			"RequestState": "CANCELLED", "ExecutionState": "NOT_PLANNED", "BusinessState": "NOT_ACHIEVED",
			"ConsistencyState": "NOT_APPLICABLE", "ObligationState": "NOT_APPLICABLE",
		}},
		{promotionexec.NodeEndBlocked, "PROMOTION_BLOCKED", map[string]string{
			"RequestState": "APPROVED", "ExecutionState": "BLOCKED", "BusinessState": "NOT_ACHIEVED",
			"ConsistencyState": "UNKNOWN", "ObligationState": "PENDING",
		}},
	}
	for _, want := range wantTerminals {
		node, ok := plan.Node(want.id)
		if !ok || node.Terminal == nil {
			t.Fatalf("terminal node %s is absent or has no end profile", want.id)
		}
		if node.Terminal.TerminalCode != want.code {
			t.Errorf("terminal %s code = %q, want %q", want.id, node.Terminal.TerminalCode, want.code)
		}
		if got := terminalDimensionsMap(node.Terminal.Dimensions); !sameStringMap(got, want.dims) {
			t.Errorf("terminal %s dimensions = %#v, want %#v", want.id, got, want.dims)
		}
	}
}

func TestTodo_PROMO_009_Integration(t *testing.T) {
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	const signingKey = "hcm-next-promo009-integration-signing-key"
	const tenant = string(fixtures.Tenant)
	const approver = "principal:promo009-approver"
	clockAt := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cfg := application.ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: db.URL,
		DevHMACKey: signingKey, Issuer: application.DefaultIssuer, Audience: application.DefaultAudience,
		Tenant: tenant, CellID: "cell-promo009-integration", MaxDeadline: 30 * time.Second,
		Migrate: false, Workspace: true, OTelExporter: application.OTelExporterNone,
		ExecutionAuthority: true, ExecutionAuthorityDigest: "sha256:promo009-execution-authority",
		ExecutionAuthorityRole: "promotion_operator", ExecutionApprover: approver,
		WorkflowPlan: application.WorkflowPlanExecute, TimerTzdbVersion: application.DefaultTimerTzdbVersion,
		TimerCalendarVersion: application.DefaultTimerCalendarVersion,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("promotion integration configuration: %v", err)
	}
	composed, err := application.ComposeServe(context.Background(), application.ServeInput{
		Config: cfg, Pool: pool, Identity: "promo009-integration", Options: application.Options{Now: func() time.Time { return clockAt }},
	})
	if err != nil {
		t.Fatalf("ComposeServe(promotion reference): %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = composed.Stop(ctx)
	})

	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key: []byte(signingKey), Issuer: cfg.Issuer, Audience: cfg.Audience, Now: func() time.Time { return clockAt },
	})
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}
	token, err := verifier.Issue(trust.Claims{
		Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: "principal:promo009-operator", SubjectKind: "human", Tenant: tenant,
		OrganizationScopeID: "org-north-america", Roles: []string{"intent_author", "comp_admin", "promotion_operator"},
		Purposes: []string{"compensation_review"}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
		SessionRef: "session:promo009-integration", IssuedAtUnix: clockAt.Add(-time.Minute).Unix(), ExpiresAtUnix: clockAt.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue integration credential: %v", err)
	}
	principal, err := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: token, Audience: cfg.Audience})
	if err != nil {
		t.Fatalf("verify integration credential: %v", err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)

	journey := composed.Cell().Journey
	proposed, err := journey.Propose(ctx, workspaceProposalForPROMO009())
	if err != nil {
		t.Fatalf("Journey.Propose: %v", err)
	}
	financeWaiting, err := journey.Execute(ctx, proposed.IntentID)
	if err != nil {
		t.Fatalf("Journey.Execute: %v", err)
	}
	if string(financeWaiting.Summary.Stage) != "FINANCE_APPROVAL" {
		t.Fatalf("after execute stage = %s, want FINANCE_APPROVAL", financeWaiting.Summary.Stage)
	}
	// PROMOUX-015: a decision is the caller's own. Each approval is decided
	// by the principal it is routed to -- for this corpus worker, the
	// class-scoped derivations of the configured approver -- and neither
	// credential holds the execution role.
	approverContext := func(derive func(string) (string, error)) context.Context {
		subject, deriveErr := derive(approver)
		if deriveErr != nil {
			t.Fatalf("derive the routed approver: %v", deriveErr)
		}
		approverToken, issueErr := verifier.Issue(trust.Claims{
			Issuer: cfg.Issuer, Audience: cfg.Audience, Subject: subject, SubjectKind: "human", Tenant: tenant,
			OrganizationScopeID: "org-north-america", Roles: []string{"comp_admin"},
			Purposes: []string{"compensation_review"}, AuthenticationMethod: "bearer_token", Assurance: "substantial",
			SessionRef: "session:" + subject, IssuedAtUnix: clockAt.Add(-time.Minute).Unix(), ExpiresAtUnix: clockAt.Add(time.Hour).Unix(),
		})
		if issueErr != nil {
			t.Fatalf("issue routed approver credential: %v", issueErr)
		}
		approverPrincipal, verifyErr := verifier.Verify(context.Background(), trust.Credential{Scheme: "Bearer", Token: approverToken, Audience: cfg.Audience})
		if verifyErr != nil {
			t.Fatalf("verify routed approver credential: %v", verifyErr)
		}
		return trust.WithPrincipal(context.Background(), approverPrincipal)
	}
	managerWaiting, err := journey.Decide(approverContext(promotionexec.FinanceApproverFor), proposed.IntentID, workspaceDecisionApprove("finance approved"))
	if err != nil {
		t.Fatalf("Journey.Decide(finance): %v", err)
	}
	if string(managerWaiting.Summary.Stage) != "MANAGER_APPROVAL" {
		t.Fatalf("after finance stage = %s, want MANAGER_APPROVAL", managerWaiting.Summary.Stage)
	}
	waiting, err := journey.Decide(approverContext(promotionexec.ManagerApproverFor), proposed.IntentID, workspaceDecisionApprove("manager approved"))
	if err != nil {
		t.Fatalf("Journey.Decide(manager): %v", err)
	}
	if string(waiting.Summary.Stage) != "WAITING_EFFECTIVE_DATE" || waiting.Instance.InstanceID == "" {
		t.Fatalf("after manager detail = %+v, want WAITING_EFFECTIVE_DATE with an instance", waiting)
	}

	tenantID := pgstore.TenantID(tenant)
	runner, err := scheduler.New(scheduler.Config{
		DB: pool, Claims: []lease.AcquireRequest{{TenantID: tenantID, Resource: lease.Resource{Kind: lease.ResourceQueue, ID: scheduler.DefaultQueueKey}, Holder: lease.Identity{WorkloadRef: "workload:promo009", InstanceRef: "promo009-integration"}}},
		Leases: lease.Manager{}, Timers: timer.Scheduler{Attempts: runtime.Store{}}, Misfire: schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: time.Hour, MaxCatchUp: 1},
		Dispatcher: scheduler.DispatcherFunc(func(dispatchCtx context.Context, work scheduler.Work) (scheduler.Disposition, error) {
			_, resumeErr := composed.Cell().ResumeFiredTimer(intentapp.WithResumeTenant(dispatchCtx, tenant), work.Row.InstanceID.String(), work.Row.NodeID, work.Row.Attempt)
			if resumeErr != nil {
				return scheduler.DispositionRetry, resumeErr
			}
			return scheduler.DispositionCompleted, nil
		}), Clock: func() time.Time { return clockAt },
	})
	if err != nil {
		t.Fatalf("scheduler.New: %v", err)
	}
	tick, err := runner.Tick(ctx)
	if err != nil || tick.Fired != 1 || tick.Completed != 1 {
		t.Fatalf("scheduler.Tick = %+v, err=%v; want one fired and one completed timer", tick, err)
	}
	completed, err := journey.Inspect(ctx, proposed.IntentID)
	if err != nil {
		t.Fatalf("Journey.Inspect: %v", err)
	}
	if string(completed.Summary.Stage) != "RECORDED" || completed.Ledger == nil {
		t.Fatalf("completed journey = stage %s ledger %+v, want RECORDED with one ledger fact", completed.Summary.Stage, completed.Ledger)
	}

	stored, err := composed.Cell().Service.GetIntent(ctx, &intentsv1.GetIntentRequest{IntentId: proposed.IntentID})
	if err != nil {
		t.Fatalf("internal/intent/app.GetIntent: %v", err)
	}
	lifecycle := stored.GetIntent().GetLifecycle()
	if lifecycle.GetExecution() != intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED || lifecycle.GetBusiness() != intentsv1.BusinessState_BUSINESS_STATE_COMPLETED {
		t.Fatalf("intent lifecycle after terminal = execution %s business %s, want COMMITTED/COMPLETED", lifecycle.GetExecution(), lifecycle.GetBusiness())
	}

	var ledgerCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`, tenantID, effects.StreamKeyFor(completed.Instance.WorkflowID, waiting.Instance.InstanceID)).Scan(&ledgerCount); err != nil {
		t.Fatalf("count integration ledger facts: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("integration ledger facts = %d, want 1", ledgerCount)
	}
}

func TestTodo_PROMO_009_Fault(t *testing.T) {
	f := newPromotionFullFixture(t, "promo009-fault", promotionFullBehavior{
		validity: []string{"VALID"},
		payroll:  "FAIL",
	})
	_, parked := runPromotionToWait(t, f)
	got, err := makePromotionScheduler(t, f, f.fireAt).Tick(context.Background())
	if err != nil || got.Fired != 1 || got.Completed != 1 {
		t.Fatalf("fault scheduler Tick = %+v, err=%v; want one repaired completion", got, err)
	}
	assertPromotion009Trace(t, f, parked.Start.InstanceID, map[string]string{
		"RequestState": "APPROVED", "ExecutionState": "REPAIR_REQUIRED", "BusinessState": "UNKNOWN",
		"ConsistencyState": "DEGRADED", "ObligationState": "PENDING",
	})
	if got := countRows(t, f.db, `SELECT count(*) FROM effect_reconciliation_job WHERE tenant_id = $1`, f.tenantID); got != 1 {
		t.Fatalf("repair jobs = %d, want 1", got)
	}
}

func TestTodo_PROMO_009_Security(t *testing.T) {
	f := newPromotionFullFixture(t, "promo009-security", promotionFullBehavior{
		validity: []string{"VALID"},
		payroll:  "PASS",
	})
	first, err := f.driver(t, f.at, nil).Execute(context.Background(), execute.ExecuteRequest{Start: f.start})
	if err != nil {
		t.Fatalf("initial execute: %v", err)
	}
	if first.Status != execute.StatusParked || len(first.WorkItems) != 1 || first.WorkItems[0].NodeID != promotionexec.NodeApproveManager {
		t.Fatalf("initial reference execution = %+v, want manager approval park", first)
	}
	if got := countRows(t, f.db, `SELECT count(*) FROM work_item WHERE tenant_id = $1 AND workflow_instance_id = $2 AND node_id = $3`, f.tenantID, first.Start.InstanceID, promotionexec.NodeApproveFinance); got != 0 {
		t.Fatalf("finance work items on within-threshold path = %d, want 0", got)
	}
	if got := countRows(t, f.db, `SELECT count(*) FROM workflow_node_execution WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3`, f.tenantID, first.Start.InstanceID, promotionexec.NodeExecutePromotion); got != 0 {
		t.Fatalf("execute capability rows before manager approval = %d, want 0", got)
	}
	if got := countRows(t, f.db, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, f.tenantID); got != 0 {
		t.Fatalf("ledger facts before approval = %d, want 0", got)
	}

	for _, nodeID := range []string{promotionexec.NodeSnapshotWorker, promotionexec.NodeSimulateCompensation, promotionexec.NodeEvaluateBand, promotionexec.NodeRaiseThreshold} {
		if got := countRows(t, f.db, `SELECT count(*) FROM workflow_node_execution WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND status = 'SUCCEEDED'`, f.tenantID, first.Start.InstanceID, nodeID); got != 1 {
			t.Fatalf("pre-approval reference node %s executions = %d, want 1", nodeID, got)
		}
	}
}

func promotion009CompleteDimensions() map[string]string {
	return map[string]string{
		"RequestState": "APPROVED", "ExecutionState": "COMMITTED", "BusinessState": "COMPLETED",
		"ConsistencyState": "CONSISTENT", "ObligationState": "SATISFIED",
	}
}

func assertPromotion009Trace(t *testing.T, f promotionFullFixture, instanceID uuid.UUID, wantDimensions map[string]string) {
	t.Helper()
	instance := loadPromotionInstance(t, f, instanceID)
	status := string(instance.RuntimeStatus)
	if status != "COMPLETED" && status != "REPAIR_REQUIRED" {
		t.Fatalf("promotion runtime status = %s, want COMPLETED or REPAIR_REQUIRED", status)
	}
	gotDimensions := map[string]string{
		"RequestState":     instance.CompletionDimensions.RequestState,
		"ExecutionState":   instance.CompletionDimensions.ExecutionState,
		"BusinessState":    instance.CompletionDimensions.BusinessState,
		"ConsistencyState": instance.CompletionDimensions.ConsistencyState,
		"ObligationState":  instance.CompletionDimensions.ObligationState,
	}
	if !sameStringMap(gotDimensions, wantDimensions) {
		t.Fatalf("promotion completion dimensions = %#v, want %#v", gotDimensions, wantDimensions)
	}

	expected := map[string]bool{
		promotionexec.NodeSnapshotWorker: true, promotionexec.NodeSimulateCompensation: true,
		promotionexec.NodeEvaluateBand: true, promotionexec.NodeRaiseThreshold: true,
		promotionexec.NodeApproveManager: true, promotionexec.NodeWaitEffectiveDate: true,
		promotionexec.NodeRevalidate: true, promotionexec.NodeStillValid: true,
		promotionexec.NodeExecutePromotion: true, promotionexec.NodeObservePayroll: true,
		promotionexec.NodeObserveAccess: true, promotionexec.NodeObserveReconciliation: true,
	}
	if f.behavior.aboveThreshold {
		expected[promotionexec.NodeApproveFinance] = true
	}
	if f.behavior.payroll != "" && f.behavior.payroll != workflow.Outcome("PASS") {
		delete(expected, promotionexec.NodeObserveAccess)
		delete(expected, promotionexec.NodeObserveReconciliation)
	}
	if got := countRows(t, f.db, `SELECT count(*) FROM workflow_node_execution WHERE tenant_id = $1 AND instance_id = $2 AND status = 'SUCCEEDED'`, f.tenantID, instanceID); got != len(expected)+1 {
		t.Fatalf("successful reference node executions = %d, want %d including terminal", got, len(expected)+1)
	}
	rows, err := f.db.Conn.Query(context.Background(), `SELECT node_id, status FROM workflow_node_execution WHERE tenant_id = $1 AND instance_id = $2`, f.tenantID, instanceID)
	if err != nil {
		t.Fatalf("load reference node executions: %v", err)
	}
	defer rows.Close()
	seen := make(map[string]string)
	for rows.Next() {
		var nodeID, nodeStatus string
		if err := rows.Scan(&nodeID, &nodeStatus); err != nil {
			t.Fatalf("scan reference node execution: %v", err)
		}
		seen[nodeID] = nodeStatus
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate reference node executions: %v", err)
	}
	for nodeID := range expected {
		if seen[nodeID] != "SUCCEEDED" {
			t.Errorf("reference node %s status = %q, want SUCCEEDED", nodeID, seen[nodeID])
		}
	}
	terminalNode := promotionexec.NodeEndComplete
	if status == "REPAIR_REQUIRED" {
		terminalNode = promotionexec.NodeEndRepairPlan
	}
	if seen[terminalNode] != "SUCCEEDED" {
		t.Errorf("terminal node %s status = %q, want SUCCEEDED", terminalNode, seen[terminalNode])
	}

	var assertionClass, schemaRef, sourceRef string
	var sequence int64
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT assertion_class, schema_ref, source_ref, sequence FROM ledger_event WHERE tenant_id = $1`, f.tenantID).Scan(&assertionClass, &schemaRef, &sourceRef, &sequence); err != nil {
		t.Fatalf("load promotion ledger fact: %v", err)
	}
	if assertionClass != "TRANSACTION_FACT" || schemaRef != effects.PromotionOutcomeSchema || sourceRef != "hcmnext:test:promotion-full" || sequence != 1 {
		t.Fatalf("promotion ledger fact = class %s schema %s source %s sequence %d, want governed promotion fact", assertionClass, schemaRef, sourceRef, sequence)
	}
}

func sameStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, want := range right {
		if left[key] != want {
			return false
		}
	}
	return true
}

func workspaceProposalForPROMO009() workspace.ProposalInput {
	return workspace.ProposalInput{
		// No TargetPositionID: PROMOUX-004 refuses every position reference
		// no picker issued, and POS-HRBP-301 is not a corpus position.
		WorkerRef: "omar-reyes", TargetJobCode: "OPS-HRBP3", TargetGrade: "P3",
		ProposedBase: "98000.00", EffectiveDate: "2026-06-01", BusinessReason: "promotion_into_senior_hrbp",
	}
}

func workspaceDecisionApprove(reason string) workspace.Decision {
	return workspace.Decision{Approve: true, Reason: reason}
}

// terminalDimensionsMap renders the five lifecycle dimensions the way the
// definition declares them, so the assertion table above reads as the spec.
func terminalDimensionsMap(d lifecycle.Dimensions) map[string]string {
	return map[string]string{
		"RequestState": d.Request.String(), "ExecutionState": d.Execution.String(),
		"BusinessState": d.Business.String(), "ConsistencyState": d.Consistency.String(),
		"ObligationState": d.Obligation.String(),
	}
}
