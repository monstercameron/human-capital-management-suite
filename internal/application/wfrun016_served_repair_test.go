package application

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/truststore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
	operationrepair "github.com/monstercameron/human-capital-management-suite/internal/operations/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

const (
	servedRepairTenant   = "wfrun016-served"
	servedRepairOperator = "principal:repair-operator"
	servedRepairAuthor   = "principal:repair-analyst"
	servedRepairApprover = "principal:security-lead"
)

// servedRepairEffect is the one adapter this test swaps into the deployed
// composition: the corrective connector write. Everything else below -- the
// operator gateway, the JIT authority read from the durable trust store, the
// dual control, the preflight simulation, the journaled receipt and the
// durable idempotency record -- is exactly what ComposeServe builds for a
// deployed cell.
type servedRepairEffect struct {
	mu    sync.Mutex
	calls []execute.RepairEffectRequest
	err   error
}

func (s *servedRepairEffect) ExecuteRepairEffect(_ context.Context, req execute.RepairEffectRequest) (execute.RepairEffectResult, error) {
	s.mu.Lock()
	s.calls = append(s.calls, req)
	failure := s.err
	s.mu.Unlock()
	if failure != nil {
		return execute.RepairEffectResult{}, failure
	}
	return execute.RepairEffectResult{EffectKey: req.Step.EffectKey, EffectRef: req.Step.EffectRef, Accepted: true, ResultRef: "payroll:accepted"}, nil
}

func (s *servedRepairEffect) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

type servedRepairObserver struct{}

func (servedRepairObserver) ObserveRepair(context.Context, operationrepair.RepairPlan, execute.RepairEffectResult) (execute.RepairObservation, error) {
	return execute.RepairObservation{Observed: true, Complete: true, Digest: "sha256:served-observed", State: "PAYROLL_EXPECTED"}, nil
}

type servedRepairVerifier struct{}

func (servedRepairVerifier) ReconcileRepair(context.Context, execute.RepairReconciliationRequest) (reconcile.CompletionDecision, error) {
	return reconcile.CompletionDecision{Status: reconcile.CompletionPass, Terminal: true, Route: reconcile.RouteConsistent,
		TerminalContribution: reconcile.CompletionDimension, Reason: "fresh complete observation matches intended and canonical values"}, nil
}

// servedRepairConfig is the deployed serve configuration this test composes:
// the execution authority on, telemetry off, one tenant.
func servedRepairConfig(url string) ServeConfig {
	return ServeConfig{
		GRPCListen: "127.0.0.1:0", HTTPListen: "127.0.0.1:0", DatabaseURL: url,
		DevHMACKey: "wfrun016-served-repair-signing-key", PageCursorKey: integrationPageCursorKey,
		Issuer: DefaultIssuer, Audience: DefaultAudience,
		Tenant: servedRepairTenant, CellID: "cell-wfrun016-served", MaxDeadline: 60 * time.Second,
		OTelExporter:             OTelExporterNone,
		ExecutionAuthority:       true,
		ExecutionAuthorityDigest: "sha256:wfrun016-served",
		ExecutionAuthorityRole:   "promotion_operator",
		ExecutionApprover:        "principal:promotion-approver",
		ExecutionFinancePartner:  LocalDevFinancePartner,
		WorkflowPlan:             WorkflowPlanExecute,
		TimerTzdbVersion:         DefaultTimerTzdbVersion,
		TimerCalendarVersion:     DefaultTimerCalendarVersion,
	}
}

// composeServedRepairCell composes the serve role over pool with the three
// repair adapters swapped in, and returns the cell's governed repair door.
func composeServedRepairCell(t *testing.T, pool *pgxadapter.Pool, url, identity string, effect app.RepairEffectPort) *workflowcontrol.RepairController {
	t.Helper()
	cfg := servedRepairConfig(url)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("configuration: %v", err)
	}
	composed, err := ComposeServe(context.Background(), ServeInput{
		Config: cfg, Pool: pool, Identity: identity,
		Options: Options{}.Apply(WithRepairAdapters(effect, servedRepairObserver{}, servedRepairVerifier{})),
	})
	if err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		_ = composed.Stop(stopCtx)
	})
	repair := composed.Cell().WorkflowRepair
	if repair == nil {
		t.Fatal("the composed serve cell exposes no governed repair door")
	}
	return repair
}

func servedRepairPlan() operationrepair.RepairPlan {
	return operationrepair.RepairPlan{
		ID: "repair-served-promotion-1", Digest: "sha256:served-plan-1", FindingDigest: "sha256:served-finding-1",
		ObservationDigest: "sha256:served-observation-1", AuthorityPolicy: "authority.promotion/1",
		MappingVersion: "mapping.payroll/1", CredentialRef: "credential:payroll-1", TargetVersion: "payroll:worker-1@7",
		OriginalSemanticKey: "promotion:worker-1:proposal-1", FailedEffectKey: "effect:payroll-provision",
		Steps: []operationrepair.Step{
			{Ordinal: 1, EffectKey: "effect:payroll-provision", EffectRef: "operation:payroll-1", Target: "payroll:worker-1", ExpectedVersion: "payroll:worker-1@7", MaxAttempts: 2},
		},
	}
}

func servedRepairCommand(tenantID uuid.UUID, key string) workflowcontrol.RepairCommand {
	plan := servedRepairPlan()
	return workflowcontrol.RepairCommand{
		TenantID: tenantID, Tenant: servedRepairTenant, Plan: plan,
		Current: operationrepair.CurrentEvidence{
			PlanDigest: plan.Digest, FindingDigest: plan.FindingDigest, ObservationDigest: plan.ObservationDigest,
			AuthorityPolicy: plan.AuthorityPolicy, MappingVersion: plan.MappingVersion,
			CredentialRef: plan.CredentialRef, TargetVersion: plan.TargetVersion,
		},
		Operator: servedRepairOperator, Author: servedRepairAuthor,
		IdempotencyKey: key, ReasonRef: "INC-REPAIR-SERVED",
	}
}

// grantServedRepairAuthority records the operator's JIT grant in the durable
// trust store. Narrowed to this repair plan, its approver -- whom the grant
// store already requires to differ from the requester -- is the second person
// DATABASE_REPAIR demands.
func grantServedRepairAuthority(t *testing.T, pool *pgxadapter.Pool, tenantID uuid.UUID, grantID string, narrowed bool) {
	t.Helper()
	scope := truststore.JITGrantScope{
		Role: string(jit.RoleIntegrityRepair), TicketRef: "INC-REPAIR-SERVED",
		Justification: "degraded external consistency after a failed payroll effect",
		Capabilities:  []string{string(workflowcontrol.RepairKind)}, Purpose: "repair plan execution",
	}
	if narrowed {
		scope.Fields = []string{workflowcontrol.RepairApprovalField(servedRepairPlan().ID)}
	}
	body, err := json.Marshal(scope)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := truststore.New(pool).PutJITGrant(context.Background(), tenantID, truststore.JITGrantRecord{
		TenantID: tenantID, RowID: uuid.New(), GrantID: grantID, Revision: 1, State: "ACTIVE",
		Requester: servedRepairOperator, Approver: servedRepairApprover, Scope: body,
		NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(2 * time.Hour),
	}); err != nil {
		t.Fatalf("record grant: %v", err)
	}
}

// repairRecordStages reads the durable record straight from the table the
// composed cell wrote to, so the assertion is about PostgreSQL rows rather
// than about the executor's return value.
func repairRecordStages(t *testing.T, pool *pgxadapter.Pool, tenantID uuid.UUID) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT stage FROM workflow_repair_execution_record
		WHERE tenant_id = $1
		ORDER BY CASE stage WHEN 'CLAIMED' THEN 0 WHEN 'EXECUTED' THEN 1 ELSE 2 END`, tenantID)
	if err != nil {
		t.Fatalf("read repair record: %v", err)
	}
	defer rows.Close()
	var stages []string
	for rows.Next() {
		var stage string
		if err := rows.Scan(&stage); err != nil {
			t.Fatal(err)
		}
		stages = append(stages, stage)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return stages
}

// TestTodo_WF_RUN_016_Served is the served proof: a RepairPlan executes
// through the production ComposeServe wiring, not a test-only constructor.
//
// It composes the deployed serve role over embedded PostgreSQL and swaps only
// the three external-system adapters no production adapter exists for yet. The
// governed door, the durable trust store the authority is read from, the
// operator gateway, the dual control, the preflight simulation, the journaled
// receipt and the durable idempotency record are all the composed ones. Then
// the cell is recomposed -- a restart -- over the same database and the same
// plan is submitted under a fresh idempotency key: the durable record, and
// nothing in process memory, is what stops a second redrive.
func TestTodo_WF_RUN_016_Served(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	pool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	effect := &servedRepairEffect{}
	repair := composeServedRepairCell(t, pool, db.URL, "wfrun016-served-a", effect)
	tenantID := pgstore.TenantID(servedRepairTenant)

	// Without a current grant the composed door denies the repair and the
	// provider is never touched.
	denied, err := repair.Execute(ctx, servedRepairCommand(tenantID, "served-repair-no-grant"))
	if err != nil {
		t.Fatalf("ungranted repair: %v", err)
	}
	if denied.Outcome != workflowcontrol.OutcomeDenied || denied.Code != "OPERATOR_AUTHORITY_REQUIRED" {
		t.Fatalf("ungranted repair = %+v, want an authority denial", denied)
	}
	if effect.count() != 0 {
		t.Fatalf("an ungranted repair redrove the effect %d times", effect.count())
	}
	if stages := repairRecordStages(t, pool, tenantID); len(stages) != 0 {
		t.Fatalf("a denied repair claimed the fence: %v", stages)
	}

	// A broad grant carries no per-action second approval, so dual control
	// refuses it.
	grantServedRepairAuthority(t, pool, tenantID, "jit-served-repair-broad", false)
	broad, err := repair.Execute(ctx, servedRepairCommand(tenantID, "served-repair-broad-grant"))
	if err != nil {
		t.Fatalf("broad-grant repair: %v", err)
	}
	if broad.Outcome != workflowcontrol.OutcomeDenied || broad.Code != "OPERATOR_DUAL_CONTROL_REQUIRED" {
		t.Fatalf("broad-grant repair = %+v, want a dual-control denial", broad)
	}
	if effect.count() != 0 {
		t.Fatalf("a repair without dual control redrove the effect %d times", effect.count())
	}

	// A grant narrowed to this plan carries its approver as the second person.
	grantServedRepairAuthority(t, pool, tenantID, "jit-served-repair-narrow", true)
	applied, err := repair.Execute(ctx, servedRepairCommand(tenantID, "served-repair-1"))
	if err != nil {
		t.Fatalf("governed repair: %v", err)
	}
	if applied.Outcome != workflowcontrol.OutcomeApplied || applied.Status != execute.RepairCompleted ||
		!applied.Executed || applied.ConsistencyState != "CONSISTENT" {
		t.Fatalf("governed repair = %+v, want APPLIED/COMPLETED/CONSISTENT", applied)
	}
	if applied.IntentInstanceID == "" || applied.ReceiptDigest == "" || applied.FenceID == "" {
		t.Fatalf("governed repair produced no evidence: %+v", applied)
	}
	if effect.count() != 1 {
		t.Fatalf("effect calls = %d, want exactly the one failed effect", effect.count())
	}
	if call := effect.calls[0]; call.OriginalSemanticKey != servedRepairPlan().OriginalSemanticKey ||
		call.Mode != execute.RepairExecutionMode {
		t.Fatalf("redrive = %+v, want the parent semantic key under REPAIR mode", call)
	}
	stages := repairRecordStages(t, pool, tenantID)
	if len(stages) != 3 || stages[0] != "CLAIMED" || stages[1] != "EXECUTED" || stages[2] != "SETTLED" {
		t.Fatalf("durable record = %v, want CLAIMED, EXECUTED, SETTLED", stages)
	}

	// Restart: a freshly composed cell over the same database, sharing nothing
	// but PostgreSQL. A new idempotency key gets past the receipt journal, so
	// what has to stop the second redrive is the durable record.
	restarted := composeServedRepairCell(t, pool, db.URL, "wfrun016-served-b", effect)
	replayed, err := restarted.Execute(ctx, servedRepairCommand(tenantID, "served-repair-after-restart"))
	if err != nil {
		t.Fatalf("repair after restart: %v", err)
	}
	if replayed.Status != execute.RepairCompleted || !replayed.Executed || replayed.ConsistencyState != "CONSISTENT" {
		t.Fatalf("repair after restart = %+v, want the recorded decision replayed", replayed)
	}
	if effect.count() != 1 {
		t.Fatalf("effect calls across the restart = %d, want the one original redrive", effect.count())
	}
	if after := repairRecordStages(t, pool, tenantID); len(after) != 3 {
		t.Fatalf("durable record after the restart = %v, want the same three append-only rows", after)
	}
}

// TestTodo_WF_RUN_016_ServedFault proves the served composition's failure path:
// a repair whose corrective effect fails is a REPAIR_REQUIRED receipt, the
// claim written before the provider call survives in PostgreSQL, and a later
// attempt on a restarted cell is told the outcome is indeterminate rather than
// being allowed to redrive an external mutation the provider may already hold.
func TestTodo_WF_RUN_016_ServedFault(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	pool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	effect := &servedRepairEffect{err: context.DeadlineExceeded}
	repair := composeServedRepairCell(t, pool, db.URL, "wfrun016-served-fault-a", effect)
	tenantID := pgstore.TenantID(servedRepairTenant)
	grantServedRepairAuthority(t, pool, tenantID, "jit-served-repair-fault", true)

	failed, err := repair.Execute(ctx, servedRepairCommand(tenantID, "served-repair-fault-1"))
	if err != nil {
		t.Fatalf("failed repair: %v", err)
	}
	if failed.Outcome != workflowcontrol.OutcomeRepairRequired || failed.Status != execute.RepairFailed {
		t.Fatalf("failed repair = %+v, want a REPAIR_REQUIRED receipt", failed)
	}
	if failed.ConsistencyState == "CONSISTENT" || failed.Executed {
		t.Fatalf("failed repair claimed progress: %+v", failed)
	}
	if stages := repairRecordStages(t, pool, tenantID); len(stages) != 1 || stages[0] != "CLAIMED" {
		t.Fatalf("record after a failed redrive = %v, want the claim alone", stages)
	}

	// The provider recovers and the cell restarts. The claim still stands, so
	// the effect is never repeated on evidence nobody observed.
	effect.mu.Lock()
	effect.err = nil
	effect.mu.Unlock()
	restarted := composeServedRepairCell(t, pool, db.URL, "wfrun016-served-fault-b", effect)
	retry, err := restarted.Execute(ctx, servedRepairCommand(tenantID, "served-repair-fault-2"))
	if err != nil {
		t.Fatalf("retry after restart: %v", err)
	}
	if retry.Status != execute.RepairIndeterminate || retry.Executed {
		t.Fatalf("retry after an unobserved redrive = %+v, want INDETERMINATE", retry)
	}
	if effect.count() != 1 {
		t.Fatalf("effect calls = %d, want the single unobserved attempt and no repeat", effect.count())
	}
}

// TestServedRepairDefaultsRefuseWithoutAnAdapter proves the production default
// -- a cell composed with no corrective connector adapter, which is every
// deployed cell today -- denies a repair with a reason instead of reporting a
// redrive it never performed.
func TestServedRepairDefaultsRefuseWithoutAnAdapter(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	pool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	cfg := servedRepairConfig(db.URL)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("configuration: %v", err)
	}
	composed, err := ComposeServe(ctx, ServeInput{Config: cfg, Pool: pool, Identity: "wfrun016-served-default"})
	if err != nil {
		t.Fatalf("ComposeServe: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		_ = composed.Stop(stopCtx)
	})
	repair := composed.Cell().WorkflowRepair
	if repair == nil {
		t.Fatal("a default serve cell exposes no governed repair door at all")
	}
	tenantID := pgstore.TenantID(servedRepairTenant)
	grantServedRepairAuthority(t, pool, tenantID, "jit-served-repair-default", true)

	result, err := repair.Execute(ctx, servedRepairCommand(tenantID, "served-repair-default"))
	if err != nil {
		t.Fatalf("default repair: %v", err)
	}
	if result.Outcome != workflowcontrol.OutcomeRepairRequired || result.Status != execute.RepairFailed {
		t.Fatalf("repair without an adapter = %+v, want a REPAIR_REQUIRED receipt, never a claimed success", result)
	}
	if result.ConsistencyState == "CONSISTENT" {
		t.Fatalf("a cell with no corrective adapter claimed consistency: %+v", result)
	}
}
